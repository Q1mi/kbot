// Package skill 提供 Skills 编辑器后端（设计文档 §4.4 / 讲义 §14.5）。
package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/util"
)

// maxBodyLen 是 SKILL.md body 的字符上限，防止 L2 注入时把上下文撑爆
// （讲义 §14.5 / 思考题 2：太长的 Skill 应拆分或用子 Agent）。
const maxBodyLen = 8000

// Spec 是 Runtime 需要的技能视图（解耦存储；engine/skillrunner 消费它）。
type Spec struct {
	VersionID              string
	WorkspaceID            string
	Name                   string
	Description            string
	Body                   string
	AllowedTools           []string
	AllowedKBs             []string
	DisableModelInvocation bool
	RequiresNetwork        bool
	Status                 string
}

// ToolChecker 用于发布前校验 allowed-tools 都存在（可为 nil 跳过校验）。
type ToolChecker interface {
	ToolExistsByName(ctx context.Context, workspaceID, name string) (bool, error)
}

// KBChecker 用于发布前校验 allowed-kbs 引用的知识库属于当前工作空间。
type KBChecker interface {
	KBExists(ctx context.Context, workspaceID, kbID string) (bool, error)
}

// Store 技能存储接口。
type Store interface {
	CreateSkill(ctx context.Context, s *domain.Skill) error
	GetSkill(ctx context.Context, skillID string) (*domain.Skill, error)
	ListSkills(ctx context.Context, workspaceID string) ([]*domain.Skill, error)

	CreateSkillVersion(ctx context.Context, v *domain.SkillVersion) error
	GetSkillVersion(ctx context.Context, versionID string) (*domain.SkillVersion, error)
	ListSkillVersions(ctx context.Context, skillID string) ([]*domain.SkillVersion, error)
	UpdateSkillVersionStatus(ctx context.Context, versionID, status string) error

	CreateSubscription(ctx context.Context, sub *domain.SkillSubscription) error
	ListSubscriptionsForAgent(ctx context.Context, agentID string) ([]*domain.SkillSubscription, error)
}

type skillBundleStore interface {
	CreateSkillWithVersion(context.Context, *domain.Skill, *domain.SkillVersion) error
}

// Service 技能服务。
type Service struct {
	store     Store
	checker   ToolChecker
	kbChecker KBChecker
}

// NewService 创建技能服务。checker 可为 nil。
func NewService(store Store, checker ToolChecker) *Service {
	return &Service{store: store, checker: checker}
}

// WithKBChecker 开启 allowed-kbs 发布门禁。
func (s *Service) WithKBChecker(checker KBChecker) *Service {
	s.kbChecker = checker
	return s
}

// CreateSkillRequest 创建技能请求。
type CreateSkillRequest struct {
	WorkspaceID string `json:"workspace_id"`
	Category    string `json:"category"`
	SkillMD     string `json:"skill_md"` // 完整 SKILL.md 文本
	CreatedBy   string `json:"created_by"`
}

// CreateSkill 解析 SKILL.md，创建技能 + v1（draft）。
func (s *Service) CreateSkill(ctx context.Context, req CreateSkillRequest) (*domain.Skill, *domain.SkillVersion, error) {
	fm, body, err := ParseSkill(req.SkillMD)
	if err != nil {
		return nil, nil, err
	}
	if fm.Name == "" {
		return nil, nil, fmt.Errorf("skill name 不能为空")
	}

	sk := &domain.Skill{
		ID:          util.GenerateID(),
		WorkspaceID: req.WorkspaceID,
		Name:        fm.Name,
		Category:    req.Category,
		CreatedBy:   req.CreatedBy,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	fmJSON, err := json.Marshal(fm)
	if err != nil {
		return nil, nil, err
	}
	v := &domain.SkillVersion{
		ID: util.GenerateID(), SkillID: sk.ID, Version: 1, FrontmatterJSON: string(fmJSON), BodyMD: body,
		Status: "draft", CreatedBy: req.CreatedBy, CreatedAt: time.Now(),
	}
	if bundle, ok := s.store.(skillBundleStore); ok {
		if err := bundle.CreateSkillWithVersion(ctx, sk, v); err != nil {
			return nil, nil, fmt.Errorf("create skill and version: %w", err)
		}
		return sk, v, nil
	}
	if err := s.store.CreateSkill(ctx, sk); err != nil {
		return nil, nil, fmt.Errorf("create skill: %w", err)
	}
	if err := s.store.CreateSkillVersion(ctx, v); err != nil {
		return nil, nil, fmt.Errorf("create skill version: %w", err)
	}
	return sk, v, nil
}

// CreateVersion 从新的 SKILL.md 文本新增一个 immutable 版本。
func (s *Service) CreateVersion(ctx context.Context, skillID, skillMD, createdBy string) (*domain.SkillVersion, error) {
	fm, body, err := ParseSkill(skillMD)
	if err != nil {
		return nil, err
	}
	return s.createVersion(ctx, skillID, fm, body, createdBy)
}

func (s *Service) createVersion(ctx context.Context, skillID string, fm Frontmatter, body, createdBy string) (*domain.SkillVersion, error) {
	fmJSON, err := json.Marshal(fm)
	if err != nil {
		return nil, err
	}
	existing, err := s.store.ListSkillVersions(ctx, skillID)
	if err != nil {
		return nil, fmt.Errorf("list skill versions: %w", err)
	}
	v := &domain.SkillVersion{
		ID:              util.GenerateID(),
		SkillID:         skillID,
		Version:         len(existing) + 1,
		FrontmatterJSON: string(fmJSON),
		BodyMD:          body,
		Status:          "draft",
		CreatedBy:       createdBy,
		CreatedAt:       time.Now(),
	}
	if err := s.store.CreateSkillVersion(ctx, v); err != nil {
		return nil, fmt.Errorf("create skill version: %w", err)
	}
	return v, nil
}

// Publish 发布技能版本。强制校验（讲义 §14.5）：name/description 非空、body 不超长、
// allowed-tools 都在 Tool Registry 里存在（若配置了 checker）。
func (s *Service) Publish(ctx context.Context, versionID string) error {
	v, err := s.store.GetSkillVersion(ctx, versionID)
	if err != nil {
		return fmt.Errorf("get skill version: %w", err)
	}
	fm, err := frontmatterOf(v)
	if err != nil {
		return err
	}
	if fm.Name == "" || fm.Description == "" {
		return fmt.Errorf("发布要求 name 与 description 非空")
	}
	if utf8.RuneCountInString(v.BodyMD) > maxBodyLen {
		return fmt.Errorf("SKILL.md body 超过 %d 字符上限", maxBodyLen)
	}
	if s.checker != nil {
		sk, err := s.store.GetSkill(ctx, v.SkillID)
		if err != nil {
			return err
		}
		for _, name := range fm.AllowedTools {
			ok, err := s.checker.ToolExistsByName(ctx, sk.WorkspaceID, name)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("allowed-tools 引用了不存在的工具: %s", name)
			}
		}
	}
	if s.kbChecker != nil {
		sk, err := s.store.GetSkill(ctx, v.SkillID)
		if err != nil {
			return err
		}
		for _, kbID := range fm.AllowedKBs {
			ok, err := s.kbChecker.KBExists(ctx, sk.WorkspaceID, kbID)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("allowed-kbs 引用了不存在或越权的知识库: %s", kbID)
			}
		}
	}
	return s.store.UpdateSkillVersionStatus(ctx, versionID, "published")
}

// Subscribe 把某技能版本订阅到一个 Agent。
func (s *Service) Subscribe(ctx context.Context, skillID, versionID, agentID string) error {
	sk, err := s.store.GetSkill(ctx, skillID)
	if err != nil {
		return fmt.Errorf("get skill: %w", err)
	}
	v, err := s.store.GetSkillVersion(ctx, versionID)
	if err != nil {
		return fmt.Errorf("get skill version: %w", err)
	}
	if v.SkillID != skillID {
		return fmt.Errorf("skill version does not belong to skill")
	}
	if v.Status != "published" {
		return fmt.Errorf("skill version must be published")
	}
	existing, err := s.store.ListSubscriptionsForAgent(ctx, agentID)
	if err != nil {
		return fmt.Errorf("list subscriptions: %w", err)
	}
	for _, sub := range existing {
		if sub.SkillID == skillID && sub.VersionID == versionID {
			return nil
		}
	}
	sub := &domain.SkillSubscription{
		SkillID:     skillID,
		VersionID:   versionID,
		AgentID:     agentID,
		WorkspaceID: sk.WorkspaceID,
		CreatedAt:   time.Now(),
	}
	return s.store.CreateSubscription(ctx, sub)
}

// GetSpec 把某技能版本解析为 Runtime 用的 Spec。
func (s *Service) GetSpec(ctx context.Context, versionID string) (*Spec, error) {
	v, err := s.store.GetSkillVersion(ctx, versionID)
	if err != nil {
		return nil, fmt.Errorf("get skill version: %w", err)
	}
	fm, err := frontmatterOf(v)
	if err != nil {
		return nil, err
	}
	sk, err := s.store.GetSkill(ctx, v.SkillID)
	if err != nil {
		return nil, fmt.Errorf("get skill: %w", err)
	}
	return &Spec{
		VersionID:   v.ID,
		WorkspaceID: sk.WorkspaceID,
		Name:        fm.Name, Description: fm.Description, Body: v.BodyMD,
		AllowedTools: fm.AllowedTools, AllowedKBs: fm.AllowedKBs,
		DisableModelInvocation: fm.DisableModelInvocation,
		RequiresNetwork:        fm.RequiresNetwork, Status: v.Status,
	}, nil
}

// SpecsForAgent 返回某 Agent 订阅的所有技能 Spec（供 L1 元数据注入）。
func (s *Service) SpecsForAgent(ctx context.Context, agentID string) ([]*Spec, error) {
	subs, err := s.store.ListSubscriptionsForAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}
	var specs []*Spec
	for _, sub := range subs {
		spec, err := s.GetSpec(ctx, sub.VersionID)
		if err != nil {
			return nil, fmt.Errorf("get subscribed skill version %s: %w", sub.VersionID, err)
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

// ListSkills 透传。
func (s *Service) ListSkills(ctx context.Context, ws string) ([]*domain.Skill, error) {
	return s.store.ListSkills(ctx, ws)
}

// EnsureSkillWorkspace 校验 Skill 属于当前 Workspace。
func (s *Service) EnsureSkillWorkspace(ctx context.Context, skillID, workspaceID string) error {
	sk, err := s.store.GetSkill(ctx, skillID)
	if err != nil || workspaceID == "" || sk.WorkspaceID != workspaceID {
		return fmt.Errorf("skill not found")
	}
	return nil
}

// ValidateVersion 校验版本属于 URL 指定的 Skill 和当前 Workspace。
func (s *Service) ValidateVersion(ctx context.Context, skillID, versionID, workspaceID string, requirePublished bool) error {
	if err := s.EnsureSkillWorkspace(ctx, skillID, workspaceID); err != nil {
		return err
	}
	v, err := s.store.GetSkillVersion(ctx, versionID)
	if err != nil || v.SkillID != skillID {
		return fmt.Errorf("skill version not found")
	}
	if requirePublished && v.Status != "published" {
		return fmt.Errorf("skill version must be published")
	}
	return nil
}

// ListVersions 返回指定 Skill 的全部不可变版本。
func (s *Service) ListVersions(ctx context.Context, skillID, workspaceID string) ([]*domain.SkillVersion, error) {
	sk, err := s.store.GetSkill(ctx, skillID)
	if err != nil {
		return nil, fmt.Errorf("get skill: %w", err)
	}
	if workspaceID != "" && sk.WorkspaceID != workspaceID {
		return nil, fmt.Errorf("skill does not belong to current workspace")
	}
	return s.store.ListSkillVersions(ctx, skillID)
}

func frontmatterOf(v *domain.SkillVersion) (Frontmatter, error) {
	var fm Frontmatter
	if err := json.Unmarshal([]byte(v.FrontmatterJSON), &fm); err != nil {
		return Frontmatter{}, fmt.Errorf("unmarshal frontmatter: %w", err)
	}
	return fm, nil
}
