// Package guard 是 Runtime 链路里的固定环节：注入检测 / 限流 / 配额 / PII 脱敏 /
// 数据出域控制（设计文档 §4.13 / 讲义 §15.4）。
//
// Guard 采用“中间件 + 多个 Rule”结构：每个 Rule 绑定一个 Hook，
// 在请求链的固定时刻被调用，便于集中配置、按 Workspace 灰度开关。
package guard

import (
	"context"
	"fmt"
)

// Hook 是 Guard 在请求链里的注入点位（讲义 §15.4）。
type Hook string

const (
	HookOnInput    Hook = "on_input"     // 入站：脱敏 / 注入检测 / 限流
	HookOnLLMCall  Hook = "on_llm_call"  // 调模型前：分级路由 / token 配额
	HookOnToolCall Hook = "on_tool_call" // 调工具前：白名单 / 敏感操作审批
	HookOnOutput   Hook = "on_output"    // 出站：脱敏 / 出域检查
)

// Decision 是一个 Rule 的判定结果。
type Decision struct {
	Allow   bool
	Reason  string
	Patched any // 经脱敏 / 改写后的载荷（非 nil 时替换原载荷继续）
}

// Allowed 是放行的便捷构造。
func Allowed() Decision { return Decision{Allow: true} }

// Block 是拦截的便捷构造。
func Block(reason string) Decision { return Decision{Allow: false, Reason: reason} }

// Patch 是改写并放行的便捷构造。
func Patch(payload any, reason string) Decision {
	return Decision{Allow: true, Reason: reason, Patched: payload}
}

// Rule 是单条 Guard 规则。
type Rule interface {
	Name() string
	Hook() Hook
	Check(ctx context.Context, payload any) Decision
}

// Recorder 记录 Guard 触发事件（拦截 / 改写），用于 Audit 与注入日志。
type Recorder interface {
	Record(ctx context.Context, ev Event)
}

// Event 是一次 Guard 触发记录。
type Event struct {
	Hook    Hook
	Rule    string
	Allowed bool
	Reason  string
	Sample  string // 原始载荷摘要（注入样本保留原文）
}

// ErrBlocked 表示被 Guard 拦截。
type ErrBlocked struct{ Reason string }

func (e *ErrBlocked) Error() string { return "guard blocked: " + e.Reason }

// Guard 编排：按 Hook 顺序执行规则，任一拦截即终止。
type Guard struct {
	rules    map[Hook][]Rule
	recorder Recorder
	provider RuleProvider
}

// RuleProvider 按工作空间提供运行时可配置规则。
type RuleProvider interface {
	Rules(ctx context.Context, workspaceID string, hook Hook) ([]Rule, error)
}

// New 创建 Guard。
func New(recorder Recorder) *Guard {
	return &Guard{rules: make(map[Hook][]Rule), recorder: recorder}
}

// Add 注册一条规则。
func (g *Guard) Add(r Rule) *Guard {
	g.rules[r.Hook()] = append(g.rules[r.Hook()], r)
	return g
}

// WithRuleProvider 接入数据库驱动的动态规则。
func (g *Guard) WithRuleProvider(provider RuleProvider) *Guard {
	g.provider = provider
	return g
}

// Run 在某 Hook 上跑所有规则。返回（可能被改写过的）载荷或拦截错误。
func (g *Guard) Run(ctx context.Context, h Hook, payload any) (any, error) {
	rules := g.rules[h]
	if g.provider != nil {
		if dynamic, err := g.provider.Rules(ctx, WorkspaceKey(ctx), h); err == nil {
			rules = append(append([]Rule(nil), rules...), dynamic...)
		}
	}
	for _, r := range rules {
		d := r.Check(ctx, payload)
		if g.recorder != nil && (!d.Allow || d.Patched != nil) {
			g.recorder.Record(ctx, Event{
				Hook: h, Rule: r.Name(), Allowed: d.Allow, Reason: d.Reason,
				Sample: summarize(payload),
			})
		}
		if !d.Allow {
			return nil, &ErrBlocked{Reason: d.Reason}
		}
		if d.Patched != nil {
			payload = d.Patched
		}
	}
	return payload, nil
}

type workspaceKeyCtx struct{}

// WithWorkspaceKey 把当前工作空间写入 Guard 上下文。
func WithWorkspaceKey(ctx context.Context, workspaceID string) context.Context {
	if workspaceID == "" {
		return ctx
	}
	return context.WithValue(ctx, workspaceKeyCtx{}, workspaceID)
}

// WorkspaceKey 读取当前 Guard 工作空间。
func WorkspaceKey(ctx context.Context) string {
	value, _ := ctx.Value(workspaceKeyCtx{}).(string)
	return value
}

// RunString 是针对文本载荷的便捷封装（OnInput/OnOutput 常用）。
func (g *Guard) RunString(ctx context.Context, h Hook, text string) (string, error) {
	out, err := g.Run(ctx, h, text)
	if err != nil {
		return "", err
	}
	s, ok := out.(string)
	if !ok {
		return "", fmt.Errorf("guard: expected string payload, got %T", out)
	}
	return s, nil
}

// OnInput 跑入站规则（注入检测 / PII 脱敏）。返回脱敏后文本或拦截错误。
func (g *Guard) OnInput(ctx context.Context, text string) (string, error) {
	return g.RunString(ctx, HookOnInput, text)
}

// OnOutput 跑出站规则（PII 脱敏 / 出域检查）。
func (g *Guard) OnOutput(ctx context.Context, text string) (string, error) {
	return g.RunString(ctx, HookOnOutput, text)
}

// OnLLMCall 执行调用模型前的限流与 token 配额规则。
func (g *Guard) OnLLMCall(ctx context.Context) error {
	_, err := g.Run(ctx, HookOnLLMCall, "")
	return err
}

// OnToolCall 执行调用工具前的限流规则，敏感工具审批由引擎处理。
func (g *Guard) OnToolCall(ctx context.Context, toolName string) error {
	_, err := g.Run(ctx, HookOnToolCall, toolName)
	return err
}

// IsBlocked 报告 err 是否是 Guard 拦截。
func IsBlocked(err error) bool {
	_, ok := err.(*ErrBlocked)
	return ok
}

func summarize(payload any) string {
	if s, ok := payload.(string); ok {
		if len(s) > 500 {
			return s[:500]
		}
		return s
	}
	return fmt.Sprintf("%v", payload)
}
