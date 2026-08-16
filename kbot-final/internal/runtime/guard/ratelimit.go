package guard

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/time/rate"
)

// RateLimiter 是进程内多维令牌桶（按 user/workspace/api_key/tool_id 等 key 限）。
// 进程内桶挡瞬时尖峰；跨进程总量用 RedisBucket（讲义 §15.4：两层互补）。
type RateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
	rps      rate.Limit
	burst    int
}

// NewRateLimiter 创建限流器：每秒 rps 个、突发 burst 个。
func NewRateLimiter(rps float64, burst int) *RateLimiter {
	return &RateLimiter{
		limiters: make(map[string]*rate.Limiter),
		rps:      rate.Limit(rps),
		burst:    burst,
	}
}

// Allow 报告 key 是否还有令牌。
func (l *RateLimiter) Allow(key string) bool {
	l.mu.Lock()
	lim, ok := l.limiters[key]
	if !ok {
		lim = rate.NewLimiter(l.rps, l.burst)
		l.limiters[key] = lim
	}
	l.mu.Unlock()
	return lim.Allow()
}

// RedisBucket 是跨进程共享的简单速率限制（INCR + EXPIRE，讲义 §15.4）。
type RedisBucket struct {
	rds *redis.Client
}

// NewRedisBucket 创建 Redis 计数桶。
func NewRedisBucket(rds *redis.Client) *RedisBucket {
	return &RedisBucket{rds: rds}
}

// Allow 在 window 内对 key 计数，超过 limit 返回 false。
func (b *RedisBucket) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	n, err := b.rds.Incr(ctx, "rl:"+key).Result()
	if err != nil {
		return false, err
	}
	if n == 1 {
		if err := b.rds.Expire(ctx, "rl:"+key, window).Err(); err != nil {
			return false, err
		}
	}
	return int(n) <= limit, nil
}

// Limiter 是限流后端抽象:Redis(跨进程)或内存(单测 / 无 Redis 兜底)。
type Limiter interface {
	// Allow 报告 key 在窗口内是否还有额度。后端故障应返回 error(调用方 fail-open)。
	Allow(ctx context.Context, key string) (bool, error)
}

// NewLimiter:rds 非 nil → Redis 固定窗口;否则内存固定窗口。limit 为窗口内最大请求数。
func NewLimiter(rds *redis.Client, limit int, window time.Duration) Limiter {
	if rds != nil {
		return &redisLimiter{bucket: NewRedisBucket(rds), limit: limit, window: window}
	}
	return newMemoryLimiter(limit, window)
}

type redisLimiter struct {
	bucket *RedisBucket
	limit  int
	window time.Duration
}

func (l *redisLimiter) Allow(ctx context.Context, key string) (bool, error) {
	return l.bucket.Allow(ctx, key, l.limit, l.window)
}

// memoryLimiter 是进程内固定窗口计数器(无 Redis 时兜底,亦便于单测确定性)。
type memoryLimiter struct {
	mu     sync.Mutex
	counts map[string]*winCount
	limit  int
	window time.Duration
	now    func() time.Time
}

type winCount struct {
	n       int
	resetAt time.Time
}

func newMemoryLimiter(limit int, window time.Duration) *memoryLimiter {
	return &memoryLimiter{counts: map[string]*winCount{}, limit: limit, window: window, now: time.Now}
}

func (l *memoryLimiter) Allow(_ context.Context, key string) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	c := l.counts[key]
	if c == nil || now.After(c.resetAt) {
		c = &winCount{resetAt: now.Add(l.window)}
		l.counts[key] = c
	}
	c.n++
	return c.n <= l.limit, nil
}

// RateLimitRule 是绑定在某 hook 上的限流规则,按 ctx 里的身份(user/api_key)+ prefix 计数。
type RateLimitRule struct {
	hook    Hook
	limiter Limiter
	prefix  string // 维度前缀:input / llm / tool
}

// NewRateLimitRule 创建限流规则。
func NewRateLimitRule(hook Hook, prefix string, limiter Limiter) *RateLimitRule {
	return &RateLimitRule{hook: hook, limiter: limiter, prefix: prefix}
}

func (r *RateLimitRule) Name() string { return "rate_limit:" + r.prefix }
func (r *RateLimitRule) Hook() Hook   { return r.hook }

func (r *RateLimitRule) Check(ctx context.Context, _ any) Decision {
	key := r.prefix + ":" + rateKeyFromContext(ctx)
	ok, err := r.limiter.Allow(ctx, key)
	if err != nil {
		return Allowed() // 限流后端故障 fail-open:不因 Redis 抖动掐断对话
	}
	if !ok {
		return Block("rate limit exceeded for " + r.prefix)
	}
	return Allowed()
}

// 限流身份(user_id / api_key)随 context 传入。
type rateKeyCtx struct{}

// WithRateKey 把限流身份写入 context(引擎从 req.UserID 设)。
func WithRateKey(ctx context.Context, key string) context.Context {
	if key == "" {
		return ctx
	}
	return context.WithValue(ctx, rateKeyCtx{}, key)
}

func rateKeyFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(rateKeyCtx{}).(string); ok {
		return v
	}
	return "anon"
}
