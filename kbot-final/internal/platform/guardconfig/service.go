// Package guardconfig 提供工作空间级 Guard 规则与周期配额配置。
package guardconfig

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Q1mi/kbot/internal/runtime/guard"
	"github.com/Q1mi/kbot/internal/util"
)

type Rule struct {
	ID             string `json:"id"`
	Kind           string `json:"kind"`
	Hook           string `json:"hook"`
	PatternOrModel string `json:"pattern_or_model"`
	Action         string `json:"action"`
	Enabled        bool   `json:"enabled"`
	WorkspaceID    string `json:"workspace_id"`
}

type Quota struct {
	Dimension string `json:"dimension"`
	Period    string `json:"period"`
	Used      int64  `json:"used"`
	Limit     int64  `json:"limit"`
}

type QuotaView struct {
	Metric string `json:"metric"`
	Period string `json:"period"`
	Used   int64  `json:"used"`
	Limit  int64  `json:"limit"`
}

type Store interface {
	CreateRule(context.Context, *Rule) error
	GetRule(context.Context, string) (*Rule, error)
	ListRules(context.Context, string) ([]*Rule, error)
	UpdateRule(context.Context, *Rule) error
	SetQuota(context.Context, *Quota) error
	ListQuotas(context.Context, string, string) ([]*Quota, error)
	ConsumeQuota(context.Context, string, string, int64) (*Quota, bool, error)
}

type Service struct {
	store Store
	now   func() time.Time
}

func NewService(store Store) *Service { return &Service{store: store, now: time.Now} }

func (s *Service) ListRules(ctx context.Context, workspaceID string) ([]*Rule, error) {
	return s.store.ListRules(ctx, workspaceID)
}

func (s *Service) CreateRule(ctx context.Context, workspaceID string, input Rule) (*Rule, error) {
	input.ID = util.GenerateID()
	input.WorkspaceID = workspaceID
	if err := validateRule(&input); err != nil {
		return nil, err
	}
	if err := s.store.CreateRule(ctx, &input); err != nil {
		return nil, err
	}
	return &input, nil
}

func (s *Service) UpdateRule(ctx context.Context, workspaceID, ruleID string, input Rule) (*Rule, error) {
	existing, err := s.store.GetRule(ctx, ruleID)
	if err != nil || existing.WorkspaceID != workspaceID {
		return nil, fmt.Errorf("guard rule not found")
	}
	input.ID = ruleID
	input.WorkspaceID = workspaceID
	if err := validateRule(&input); err != nil {
		return nil, err
	}
	if err := s.store.UpdateRule(ctx, &input); err != nil {
		return nil, err
	}
	return &input, nil
}

func validateRule(rule *Rule) error {
	if strings.TrimSpace(rule.Kind) == "" {
		return fmt.Errorf("guard rule kind is required")
	}
	if strings.TrimSpace(rule.PatternOrModel) == "" {
		return fmt.Errorf("guard rule pattern is required")
	}
	if rule.Action == "redact" && rule.Hook != string(guard.HookOnInput) && rule.Hook != string(guard.HookOnOutput) {
		return fmt.Errorf("redact action is only valid for on_input or on_output")
	}
	_, err := guard.NewConfigRule(rule.ID, rule.Kind, guard.Hook(rule.Hook), rule.PatternOrModel, rule.Action)
	return err
}

// Rules 实现 guard.RuleProvider；存储故障由 Guard 按 fail-open 处理。
func (s *Service) Rules(ctx context.Context, workspaceID string, hook guard.Hook) ([]guard.Rule, error) {
	if workspaceID == "" {
		return nil, nil
	}
	configured, err := s.store.ListRules(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]guard.Rule, 0, len(configured))
	for _, rule := range configured {
		if !rule.Enabled || guard.Hook(rule.Hook) != hook {
			continue
		}
		compiled, err := guard.NewConfigRule(rule.ID, rule.Kind, hook, rule.PatternOrModel, rule.Action)
		if err == nil {
			out = append(out, compiled)
		}
	}
	return out, nil
}

func (s *Service) SetQuota(ctx context.Context, workspaceID, metric string, limit int64) (*QuotaView, error) {
	if metric != "llm_calls" {
		return nil, fmt.Errorf("unsupported quota metric: %s", metric)
	}
	if limit < 0 {
		return nil, fmt.Errorf("quota limit cannot be negative")
	}
	quota := &Quota{Dimension: quotaDimension(workspaceID, metric), Period: s.period(), Limit: limit}
	if err := s.store.SetQuota(ctx, quota); err != nil {
		return nil, err
	}
	return quotaView(quota), nil
}

func (s *Service) ListQuotas(ctx context.Context, workspaceID string) ([]*QuotaView, error) {
	quotas, err := s.store.ListQuotas(ctx, workspaceID, s.period())
	if err != nil {
		return nil, err
	}
	out := make([]*QuotaView, 0, len(quotas))
	for _, quota := range quotas {
		out = append(out, quotaView(quota))
	}
	return out, nil
}

// Consume 实现 guard.QuotaConsumer。没有配置记录时额度不限。
func (s *Service) Consume(ctx context.Context, workspaceID, metric string, amount int64) (bool, int64, error) {
	quota, consumed, err := s.store.ConsumeQuota(ctx, quotaDimension(workspaceID, metric), s.period(), amount)
	if err != nil {
		return false, 0, err
	}
	if quota == nil {
		return true, -1, nil
	}
	remaining := quota.Limit - quota.Used
	if quota.Limit == 0 {
		remaining = -1
	}
	return consumed, remaining, nil
}

func (s *Service) period() string { return s.now().UTC().Format("2006-01") }

func quotaDimension(workspaceID, metric string) string {
	return "workspace:" + workspaceID + ":" + metric
}

func quotaView(quota *Quota) *QuotaView {
	parts := strings.Split(quota.Dimension, ":")
	metric := parts[len(parts)-1]
	return &QuotaView{Metric: metric, Period: quota.Period, Used: quota.Used, Limit: quota.Limit}
}
