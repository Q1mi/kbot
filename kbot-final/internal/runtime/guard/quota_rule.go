package guard

import "context"

// QuotaConsumer 是持久化周期配额的最小接口。
type QuotaConsumer interface {
	Consume(ctx context.Context, workspaceID, metric string, amount int64) (allowed bool, remaining int64, err error)
}

// QuotaRule 在 LLM 调用前扣减工作空间的调用次数配额。
type QuotaRule struct {
	metric   string
	consumer QuotaConsumer
}

func NewQuotaRule(metric string, consumer QuotaConsumer) *QuotaRule {
	return &QuotaRule{metric: metric, consumer: consumer}
}

func (r *QuotaRule) Name() string { return "quota:" + r.metric }
func (r *QuotaRule) Hook() Hook   { return HookOnLLMCall }

func (r *QuotaRule) Check(ctx context.Context, _ any) Decision {
	workspaceID := WorkspaceKey(ctx)
	if workspaceID == "" || r.consumer == nil {
		return Allowed()
	}
	allowed, _, err := r.consumer.Consume(ctx, workspaceID, r.metric, 1)
	if err != nil {
		return Allowed()
	}
	if !allowed {
		return Block("workspace quota exceeded for " + r.metric)
	}
	return Allowed()
}
