// Package prompt 提供 Apollo 风格的 Prompt 管理中心（设计文档 §4.2 / 讲义 §14.4）。
//
// 核心 pattern：版本 immutable（只 insert 不 update）+ 环境是指针（prompt_envs）+
// 客户端缓存（promptcache）+ Pub/Sub 推送失效 + A/B 灰度。发布/回滚都是改指针。
package prompt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"time"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/runtime/promptcache"
	"github.com/Q1mi/kbot/internal/util"
)

// 环境常量。
const (
	EnvDev     = "dev"
	EnvStaging = "staging"
	EnvProd    = "prod"
)

// Publisher 把"某 prompt@env 指针变了"广播出去（Redis Pub/Sub）。
type Publisher interface {
	Publish(ctx context.Context, channel, message string) error
}

// NoopPublisher 是无 Redis 时的空实现（测试 / 单进程）。
type NoopPublisher struct{}

func (NoopPublisher) Publish(context.Context, string, string) error { return nil }

// InvalidateChannel 返回某 prompt@env 的失效频道名。
func InvalidateChannel(promptID, env string) string {
	return fmt.Sprintf("prompt:%s:%s:invalidate", promptID, env)
}

// Store Prompt 存储接口。
type Store interface {
	CreatePrompt(ctx context.Context, p *domain.Prompt) error
	DeletePrompt(ctx context.Context, promptID string) error
	GetPrompt(ctx context.Context, promptID string) (*domain.Prompt, error)
	ListPrompts(ctx context.Context, workspaceID string) ([]*domain.Prompt, error)

	CreatePromptVersion(ctx context.Context, v *domain.PromptVersion) error
	GetPromptVersion(ctx context.Context, versionID string) (*domain.PromptVersion, error)
	GetPromptVersionByNumber(ctx context.Context, promptID string, version int) (*domain.PromptVersion, error)
	ListPromptVersions(ctx context.Context, promptID string) ([]*domain.PromptVersion, error)

	SetEnvBinding(ctx context.Context, promptID, env, versionID string) error
	GetEnvBinding(ctx context.Context, promptID, env string) (string, error) // 返回 versionID

	GetActiveExperiment(ctx context.Context, promptID, env string) (*domain.PromptExperiment, error)
	UpsertExperiment(ctx context.Context, exp *domain.PromptExperiment) error
	AppendRolloutEvent(ctx context.Context, event *domain.PromptRolloutEvent) error
	CompleteRollout(ctx context.Context, exp *domain.PromptExperiment, event *domain.PromptRolloutEvent) error
}

type ModelProfileValidator interface {
	ValidateProfileVersion(ctx context.Context, workspaceID, versionID string) error
}

// Service Prompt 服务。
type Service struct {
	store  Store
	cache  *promptcache.Cache
	pub    Publisher
	models ModelProfileValidator
}

// NewService 创建 Prompt 服务。cache/pub 可由调用方共享（Runtime 与 Platform 同一份 cache）。
func NewService(store Store, cache *promptcache.Cache, pub Publisher) *Service {
	if cache == nil {
		cache = promptcache.NewCache()
	}
	if pub == nil {
		pub = NoopPublisher{}
	}
	return &Service{store: store, cache: cache, pub: pub}
}

func (s *Service) WithModelProfiles(models ModelProfileValidator) *Service {
	s.models = models
	return s
}

// Cache 暴露底层缓存（供 Runtime subscriber 重拉时写入）。
func (s *Service) Cache() *promptcache.Cache { return s.cache }

// CreatePromptRequest 创建 Prompt 请求。
type CreatePromptRequest struct {
	WorkspaceID           string                  `json:"workspace_id"`
	Name                  string                  `json:"name"`
	Category              string                  `json:"category"`
	Template              string                  `json:"template"`
	VariablesSchema       string                  `json:"variables_schema"`
	ModelProfileVersionID string                  `json:"model_profile_version_id"`
	GenerationConfig      domain.GenerationConfig `json:"generation_config"`
	CreatedBy             string                  `json:"created_by"`
}

// CreatePrompt 创建 Prompt 并产生 v1，默认绑定到 dev 环境。
func (s *Service) CreatePrompt(ctx context.Context, req CreatePromptRequest) (*domain.Prompt, *domain.PromptVersion, error) {
	// Prompt 主记录、v1 和 dev 指针组成一个逻辑原子操作。先完成所有
	// 无副作用校验，避免模板或模型配置错误留下无版本 Prompt。
	if _, err := promptcache.Compile("", req.Template, req.VariablesSchema); err != nil {
		return nil, nil, fmt.Errorf("compile: %w", err)
	}
	if s.models != nil {
		if err := s.models.ValidateProfileVersion(ctx, req.WorkspaceID, req.ModelProfileVersionID); err != nil {
			return nil, nil, fmt.Errorf("validate model profile: %w", err)
		}
	}

	p := &domain.Prompt{
		ID:          util.GenerateID(),
		WorkspaceID: req.WorkspaceID,
		Name:        req.Name,
		Category:    req.Category,
		CreatedBy:   req.CreatedBy,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := s.store.CreatePrompt(ctx, p); err != nil {
		return nil, nil, fmt.Errorf("create prompt: %w", err)
	}
	v, err := s.CreateVersionConfigured(ctx, p.ID, req.Template, req.VariablesSchema,
		req.ModelProfileVersionID, req.GenerationConfig, req.CreatedBy)
	if err != nil {
		return nil, nil, s.rollbackNewPrompt(ctx, p.ID, err)
	}
	// 新 Prompt 默认在 dev 生效。
	if err := s.Promote(ctx, p.ID, EnvDev, v.ID); err != nil {
		return nil, nil, s.rollbackNewPrompt(ctx, p.ID, err)
	}
	return p, v, nil
}

func (s *Service) rollbackNewPrompt(ctx context.Context, promptID string, cause error) error {
	if err := s.store.DeletePrompt(ctx, promptID); err != nil {
		return fmt.Errorf("%w; rollback new prompt: %v", cause, err)
	}
	return cause
}

// CreateVersion 新增一个 immutable 版本（不改任何 env 指针）。
func (s *Service) CreateVersion(ctx context.Context, promptID, tmpl, schema, createdBy string) (*domain.PromptVersion, error) {
	return s.CreateVersionConfigured(ctx, promptID, tmpl, schema, "", domain.GenerationConfig{}, createdBy)
}

// CreateVersionConfigured 把 Prompt、模型 Profile 版本与生成参数作为一个原子不可变版本保存。
func (s *Service) CreateVersionConfigured(
	ctx context.Context,
	promptID, tmpl, schema, modelProfileVersionID string,
	generationConfig domain.GenerationConfig,
	createdBy string,
) (*domain.PromptVersion, error) {
	// 编译校验：模板语法、变量 schema 合法才允许保存。
	if _, err := promptcache.Compile("", tmpl, schema); err != nil {
		return nil, fmt.Errorf("compile: %w", err)
	}
	p, err := s.store.GetPrompt(ctx, promptID)
	if err != nil {
		return nil, fmt.Errorf("get prompt: %w", err)
	}
	if s.models != nil {
		if err := s.models.ValidateProfileVersion(ctx, p.WorkspaceID, modelProfileVersionID); err != nil {
			return nil, fmt.Errorf("validate model profile: %w", err)
		}
	}

	existing, err := s.store.ListPromptVersions(ctx, promptID)
	if err != nil {
		return nil, fmt.Errorf("list versions: %w", err)
	}
	v := &domain.PromptVersion{
		ID:                    util.GenerateID(),
		PromptID:              promptID,
		Version:               len(existing) + 1,
		Template:              tmpl,
		VariablesSchema:       orEmptyObj(schema),
		ModelProfileVersionID: modelProfileVersionID,
		GenerationConfig:      generationConfig,
		Hash:                  hashTemplate(tmpl),
		TokenEstimate:         promptcache.EstimateTokens(tmpl),
		CreatedBy:             createdBy,
		CreatedAt:             time.Now(),
	}
	if err := s.store.CreatePromptVersion(ctx, v); err != nil {
		return nil, fmt.Errorf("create version: %w", err)
	}
	return v, nil
}

// Promote 把 env 指针指向某版本（dev→staging→prod 晋升 / 首次发布都走这里）。
// 顺序：改指针 → 重编译并写本地缓存（in-process 立刻生效）→ Pub/Sub 广播（跨进程）。
func (s *Service) Promote(ctx context.Context, promptID, env, versionID string) error {
	v, err := s.store.GetPromptVersion(ctx, versionID)
	if err != nil {
		return fmt.Errorf("get version: %w", err)
	}
	if v.PromptID != promptID {
		return fmt.Errorf("version %s does not belong to prompt %s", versionID, promptID)
	}
	if err := s.store.SetEnvBinding(ctx, promptID, env, versionID); err != nil {
		return fmt.Errorf("set env binding: %w", err)
	}

	// 重编译并更新本地缓存（讲义 §14.4：事务成功后才广播；这里指针写成功后）。
	comp, err := promptcache.Compile(v.ID, v.Template, v.VariablesSchema)
	if err != nil {
		return fmt.Errorf("compile on promote: %w", err)
	}
	s.cache.Put(promptID, env, comp)

	// 广播失效，供其它 Runtime 进程异步重拉。
	_ = s.pub.Publish(ctx, InvalidateChannel(promptID, env), versionID)
	return nil
}

// Rollback 回滚 = 把 env 指针指回旧版本（与 Promote 同机制）。
func (s *Service) Rollback(ctx context.Context, promptID, env, versionID string) error {
	return s.Promote(ctx, promptID, env, versionID)
}

// RefreshCache 按当前 env 指针重新编译并写入本地缓存。供 Pub/Sub 订阅端收到失效
// 通知后异步重拉（讲义 §14.4 的订阅侧）。
func (s *Service) RefreshCache(ctx context.Context, promptID, env string) error {
	versionID, err := s.store.GetEnvBinding(ctx, promptID, env)
	if err != nil {
		return err
	}
	v, err := s.store.GetPromptVersion(ctx, versionID)
	if err != nil {
		return err
	}
	comp, err := promptcache.Compile(v.ID, v.Template, v.VariablesSchema)
	if err != nil {
		return err
	}
	s.cache.Put(promptID, env, comp)
	return nil
}

// ResolveVersion 解析某 (prompt, env, user) 实际应使用的版本：优先 A/B 实验分流，
// 否则走 env 指针（讲义 §14.4）。
func (s *Service) ResolveVersion(ctx context.Context, promptID, env, userID string) (string, error) {
	exp, err := s.store.GetActiveExperiment(ctx, promptID, env)
	if err != nil {
		return "", fmt.Errorf("get active experiment: %w", err)
	}
	if versionID := selectExperimentVersion(exp, userID); versionID != "" {
		return versionID, nil
	}
	return s.store.GetEnvBinding(ctx, promptID, env)
}

func selectExperimentVersion(exp *domain.PromptExperiment, userID string) string {
	if exp == nil || len(exp.Variants) == 0 {
		return ""
	}
	bucket := int(crc32.ChecksumIEEE([]byte(userID+exp.ID)) % 100)
	cumulative := 0
	for _, variant := range exp.Variants {
		cumulative += variant.Traffic
		if bucket < cumulative {
			return variant.VersionID
		}
	}
	return ""
}

type ResolvedConfig struct {
	VersionID             string
	Rendered              string
	ModelProfileVersionID string
	GenerationConfig      domain.GenerationConfig
	ExperimentID          string
	ExperimentVariant     string
}

func (s *Service) ResolveConfig(ctx context.Context, promptID, env, userID string, vars map[string]any) (*ResolvedConfig, error) {
	baselineID, err := s.store.GetEnvBinding(ctx, promptID, env)
	if err != nil {
		return nil, err
	}
	versionID := baselineID
	var experimentID, variant string
	exp, err := s.store.GetActiveExperiment(ctx, promptID, env)
	if err != nil {
		return nil, fmt.Errorf("get active experiment: %w", err)
	}
	if exp != nil && exp.CandidateVersionID != "" && exp.TrafficPercent > 0 {
		bucket := int(crc32.ChecksumIEEE([]byte(userID+exp.ID)) % 100)
		experimentID = exp.ID
		variant = "baseline"
		if bucket < exp.TrafficPercent {
			versionID = exp.CandidateVersionID
			variant = "candidate"
		}
	} else if exp != nil && len(exp.Variants) > 0 {
		if selected := selectExperimentVersion(exp, userID); selected != "" {
			versionID = selected
		}
		experimentID = exp.ID
		variant = "variant"
	}
	return s.ResolveConfigByVersion(ctx, versionID, vars, experimentID, variant)
}

func (s *Service) ResolveConfigByVersion(ctx context.Context, versionID string, vars map[string]any, experimentID, variant string) (*ResolvedConfig, error) {
	v, err := s.store.GetPromptVersion(ctx, versionID)
	if err != nil {
		return nil, err
	}
	rendered, err := s.RenderByVersion(ctx, versionID, vars)
	if err != nil {
		return nil, err
	}
	return &ResolvedConfig{
		VersionID: v.ID, Rendered: rendered, ModelProfileVersionID: v.ModelProfileVersionID,
		GenerationConfig: v.GenerationConfig, ExperimentID: experimentID, ExperimentVariant: variant,
	}, nil
}

// Render 解析版本 → 取编译产物（缓存未命中则即时编译）→ 渲染。
func (s *Service) Render(ctx context.Context, promptID, env, userID string, vars map[string]any) (string, error) {
	versionID, err := s.ResolveVersion(ctx, promptID, env, userID)
	if err != nil {
		return "", fmt.Errorf("resolve version: %w", err)
	}
	// 无实验且缓存命中：走本地缓存。
	if comp, ok := s.cache.Get(promptID, env); ok && comp.VersionID == versionID {
		return comp.Render(ctx, vars)
	}
	return s.RenderByVersion(ctx, versionID, vars)
}

// RenderByVersion 按具体版本号渲染（供 Agent 快照里 pinned 的版本使用，老对话不切版）。
func (s *Service) RenderByVersion(ctx context.Context, versionID string, vars map[string]any) (string, error) {
	v, err := s.store.GetPromptVersion(ctx, versionID)
	if err != nil {
		return "", fmt.Errorf("get version: %w", err)
	}
	comp, err := promptcache.Compile(v.ID, v.Template, v.VariablesSchema)
	if err != nil {
		return "", err
	}
	return comp.Render(ctx, vars)
}

// ListPrompts / ListVersions 透传。
func (s *Service) ListPrompts(ctx context.Context, ws string) ([]*domain.Prompt, error) {
	return s.store.ListPrompts(ctx, ws)
}

// GetPrompt 返回 Prompt 元数据，供 Agent Builder 校验 system/user 用途与工作空间。
func (s *Service) GetPrompt(ctx context.Context, promptID string) (*domain.Prompt, error) {
	return s.store.GetPrompt(ctx, promptID)
}

// EnsurePromptWorkspace 校验 Prompt 属于当前 Workspace。
func (s *Service) EnsurePromptWorkspace(ctx context.Context, promptID, workspaceID string) error {
	p, err := s.store.GetPrompt(ctx, promptID)
	if err != nil || workspaceID == "" || p.WorkspaceID != workspaceID {
		return fmt.Errorf("prompt not found")
	}
	return nil
}

// GetVersion 返回指定不可变 Prompt 版本。
func (s *Service) GetVersion(ctx context.Context, versionID string) (*domain.PromptVersion, error) {
	return s.store.GetPromptVersion(ctx, versionID)
}

func (s *Service) ListVersions(ctx context.Context, promptID string) ([]*domain.PromptVersion, error) {
	return s.store.ListPromptVersions(ctx, promptID)
}

// StartExperiment 配置并启动一个 A/B 实验。
func (s *Service) StartExperiment(ctx context.Context, promptID, env string, variants []domain.ExperimentVariant) (*domain.PromptExperiment, error) {
	total := 0
	for _, v := range variants {
		if _, err := s.store.GetPromptVersion(ctx, v.VersionID); err != nil {
			return nil, fmt.Errorf("variant version %s: %w", v.VersionID, err)
		}
		total += v.Traffic
	}
	if total > 100 {
		return nil, fmt.Errorf("variant traffic sums to %d (>100)", total)
	}
	exp := &domain.PromptExperiment{
		ID:        util.GenerateID(),
		PromptID:  promptID,
		Env:       env,
		Variants:  variants,
		Status:    "active",
		StartedAt: time.Now(),
	}
	if err := s.store.UpsertExperiment(ctx, exp); err != nil {
		return nil, err
	}
	return exp, nil
}

// StartRollout 启动“当前环境基线 vs 候选版本”的渐进式发布。
func (s *Service) StartRollout(ctx context.Context, promptID, env, candidateID string, traffic int, actor string) (*domain.PromptExperiment, error) {
	if traffic <= 0 || traffic >= 100 {
		return nil, fmt.Errorf("traffic_percent must be between 1 and 99")
	}
	baselineID, err := s.store.GetEnvBinding(ctx, promptID, env)
	if err != nil {
		return nil, err
	}
	candidate, err := s.store.GetPromptVersion(ctx, candidateID)
	if err != nil {
		return nil, err
	}
	if candidate.PromptID != promptID || candidateID == baselineID {
		return nil, fmt.Errorf("candidate must be another version of this prompt")
	}
	exp := &domain.PromptExperiment{
		ID: util.GenerateID(), PromptID: promptID, Env: env,
		BaselineVersionID: baselineID, CandidateVersionID: candidateID, TrafficPercent: traffic,
		Variants: []domain.ExperimentVariant{{VersionID: candidateID, Traffic: traffic}},
		Status:   "active", StartedAt: time.Now(),
	}
	if err := s.store.UpsertExperiment(ctx, exp); err != nil {
		return nil, err
	}
	_ = s.recordRollout(ctx, exp.ID, "started", 0, traffic, actor, "")
	return exp, nil
}

func (s *Service) UpdateRolloutTraffic(ctx context.Context, promptID, env string, traffic int, actor string) (*domain.PromptExperiment, error) {
	if traffic <= 0 || traffic >= 100 {
		return nil, fmt.Errorf("traffic_percent must be between 1 and 99")
	}
	exp, err := s.store.GetActiveExperiment(ctx, promptID, env)
	if err != nil || exp == nil {
		return nil, fmt.Errorf("active rollout not found")
	}
	from := exp.TrafficPercent
	exp.TrafficPercent = traffic
	exp.Variants = []domain.ExperimentVariant{{VersionID: exp.CandidateVersionID, Traffic: traffic}}
	if err := s.store.UpsertExperiment(ctx, exp); err != nil {
		return nil, err
	}
	_ = s.recordRollout(ctx, exp.ID, "traffic_changed", from, traffic, actor, "")
	return exp, nil
}

func (s *Service) CompleteRollout(ctx context.Context, promptID, env, actor string) error {
	exp, err := s.store.GetActiveExperiment(ctx, promptID, env)
	if err != nil || exp == nil {
		return fmt.Errorf("active rollout not found")
	}
	candidate, err := s.store.GetPromptVersion(ctx, exp.CandidateVersionID)
	if err != nil {
		return fmt.Errorf("get candidate version: %w", err)
	}
	if candidate.PromptID != promptID {
		return fmt.Errorf("candidate version does not belong to prompt")
	}
	compiled, err := promptcache.Compile(candidate.ID, candidate.Template, candidate.VariablesSchema)
	if err != nil {
		return fmt.Errorf("compile candidate: %w", err)
	}
	now := time.Now()
	previousTraffic := exp.TrafficPercent
	exp.Status = "completed"
	exp.TrafficPercent = 100
	exp.CompletedAt = &now
	event := &domain.PromptRolloutEvent{
		ID: util.GenerateID(), ExperimentID: exp.ID, Action: "completed",
		FromTraffic: previousTraffic, ToTraffic: 100, Actor: actor,
		Detail: "candidate became baseline", CreatedAt: now,
	}
	if err := s.store.CompleteRollout(ctx, exp, event); err != nil {
		return fmt.Errorf("complete rollout transaction: %w", err)
	}
	s.cache.Put(promptID, env, compiled)
	_ = s.pub.Publish(ctx, InvalidateChannel(promptID, env), candidate.ID)
	return nil
}

func (s *Service) RollbackRollout(ctx context.Context, promptID, env, actor string) error {
	exp, err := s.store.GetActiveExperiment(ctx, promptID, env)
	if err != nil || exp == nil {
		return fmt.Errorf("active rollout not found")
	}
	now := time.Now()
	exp.Status = "rolled_back"
	exp.TrafficPercent = 0
	exp.CompletedAt = &now
	if err := s.store.UpsertExperiment(ctx, exp); err != nil {
		return err
	}
	return s.recordRollout(ctx, exp.ID, "rolled_back", 0, 0, actor, "")
}

func (s *Service) recordRollout(ctx context.Context, experimentID, action string, from, to int, actor, detail string) error {
	return s.store.AppendRolloutEvent(ctx, &domain.PromptRolloutEvent{
		ID: util.GenerateID(), ExperimentID: experimentID, Action: action,
		FromTraffic: from, ToTraffic: to, Actor: actor, Detail: detail, CreatedAt: time.Now(),
	})
}

// Diff 返回两个版本模板的 unified 行级 diff（设计文档 §4.2 的语义级 Diff，
// 讲义选 sergi/go-diff；这里用无依赖的 LCS 行 diff，见 diff.go）。
func (s *Service) Diff(ctx context.Context, promptID string, fromVer, toVer int) (string, error) {
	from, err := s.store.GetPromptVersionByNumber(ctx, promptID, fromVer)
	if err != nil {
		return "", fmt.Errorf("get from version: %w", err)
	}
	to, err := s.store.GetPromptVersionByNumber(ctx, promptID, toVer)
	if err != nil {
		return "", fmt.Errorf("get to version: %w", err)
	}
	return UnifiedDiff(from.Template, to.Template), nil
}

func hashTemplate(tmpl string) string {
	h := sha256.Sum256([]byte(tmpl))
	return hex.EncodeToString(h[:])
}

func orEmptyObj(s string) string {
	if s == "" {
		return "{}"
	}
	return s
}
