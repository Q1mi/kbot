package guard

import "sync"

// QuotaLedger 是周期总量配额（每月 token / 调用次数 / 成本上限）。与限流不同：
// 限流是单位时间速率，配额是周期总量（讲义 §15.4）。Guard 只做扣减与读取，
// 上调是产品策略（走审批）。这里用进程内实现；生产以 DB 持久化。
type QuotaLedger struct {
	mu     sync.Mutex
	limits map[string]int64 // key -> 上限
	used   map[string]int64 // key -> 已用
}

// NewQuotaLedger 创建配额账本。
func NewQuotaLedger() *QuotaLedger {
	return &QuotaLedger{
		limits: make(map[string]int64),
		used:   make(map[string]int64),
	}
}

// SetLimit 设置某 key（如 "workspace:w1:tokens"）的周期上限。
func (q *QuotaLedger) SetLimit(key string, limit int64) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.limits[key] = limit
}

// Consume 扣减 amount。返回是否成功与剩余额度。
// 未设上限的 key 视为不限额（始终成功）。超额时不扣减。
func (q *QuotaLedger) Consume(key string, amount int64) (ok bool, remaining int64) {
	q.mu.Lock()
	defer q.mu.Unlock()
	limit, has := q.limits[key]
	if !has {
		return true, -1 // -1 表示不限额
	}
	if q.used[key]+amount > limit {
		return false, limit - q.used[key]
	}
	q.used[key] += amount
	return true, limit - q.used[key]
}
