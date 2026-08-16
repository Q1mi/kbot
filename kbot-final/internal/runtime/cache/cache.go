// Package cache 提供 Runtime 的缓存与幂等设施（设计文档 §4.8 / 讲义 §15.5）。
//
// 两类缓存职责独立：Prompt Cache 在 LLM Gateway 出口为稳定前缀透传 cache_control；
// 这里实现 Embedding Cache（省重复 embedding 成本）与
// 幂等键存储（HTTP 重复请求去重）。
package cache

import (
	"bytes"
	"context"
	"sync"
	"time"

	"github.com/Q1mi/kbot/internal/runtime/retriever"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// EmbeddingCache 包装一个 Embedder，对相同文本返回缓存向量（设计 §4.8 的 Embedding Cache）。
type EmbeddingCache struct {
	inner retriever.Embedder
	mu    sync.RWMutex
	store map[string][]float32
	hits  int64
	miss  int64
}

// NewEmbeddingCache 创建嵌入缓存。
func NewEmbeddingCache(inner retriever.Embedder) *EmbeddingCache {
	return &EmbeddingCache{inner: inner, store: make(map[string][]float32)}
}

func (c *EmbeddingCache) Dim() int { return c.inner.Dim() }

// Embed 逐条命中缓存；未命中的批量交给底层一次性计算。
func (c *EmbeddingCache) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	var missTexts []string
	var missIdx []int

	c.mu.RLock()
	for i, t := range texts {
		if v, ok := c.store[t]; ok {
			out[i] = v
		} else {
			missTexts = append(missTexts, t)
			missIdx = append(missIdx, i)
		}
	}
	c.mu.RUnlock()

	c.mu.Lock()
	c.hits += int64(len(texts) - len(missTexts))
	c.miss += int64(len(missTexts))
	c.mu.Unlock()

	if len(missTexts) == 0 {
		return out, nil
	}

	embs, err := c.inner.Embed(ctx, missTexts)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	for j, idx := range missIdx {
		out[idx] = embs[j]
		c.store[missTexts[j]] = embs[j]
	}
	c.mu.Unlock()
	return out, nil
}

// Stats 返回命中/未命中计数（供 metrics / 仪表盘）。
func (c *EmbeddingCache) Stats() (hits, miss int64) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.hits, c.miss
}

// IdemStore 是幂等键存储接口。
type IdemStore interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Set(ctx context.Context, key string, val []byte, ttl time.Duration) error
	SetIfAbsent(ctx context.Context, key string, val []byte, ttl time.Duration) (bool, error)
	Delete(ctx context.Context, key string) error
}

// DistributedLocker 让 HTTP 幂等中间件在多副本下串行同一业务键。
type DistributedLocker interface {
	Lock(ctx context.Context, key string, ttl time.Duration) (release func(), err error)
}

// MemoryIdemStore 是进程内幂等存储（测试 / 单进程）。
type MemoryIdemStore struct {
	mu sync.Mutex
	m  map[string]idemEntry
}

type idemEntry struct {
	value     []byte
	expiresAt time.Time
}

// NewMemoryIdemStore 创建内存幂等存储。
func NewMemoryIdemStore() *MemoryIdemStore {
	return &MemoryIdemStore{m: make(map[string]idemEntry)}
}

func (s *MemoryIdemStore) Get(_ context.Context, key string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.m[key]
	if !ok {
		return nil, false, nil
	}
	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		delete(s.m, key)
		return nil, false, nil
	}
	return bytes.Clone(entry.value), true, nil
}

func (s *MemoryIdemStore) Set(_ context.Context, key string, val []byte, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := idemEntry{value: bytes.Clone(val)}
	if ttl > 0 {
		entry.expiresAt = time.Now().Add(ttl)
	}
	s.m[key] = entry
	return nil
}

func (s *MemoryIdemStore) SetIfAbsent(_ context.Context, key string, val []byte, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry, ok := s.m[key]; ok && (entry.expiresAt.IsZero() || time.Now().Before(entry.expiresAt)) {
		return false, nil
	}
	entry := idemEntry{value: bytes.Clone(val)}
	if ttl > 0 {
		entry.expiresAt = time.Now().Add(ttl)
	}
	s.m[key] = entry
	return true, nil
}

func (s *MemoryIdemStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
	return nil
}

// RedisIdemStore 提供跨进程响应缓存、原子去重和可续期分布式锁。
type RedisIdemStore struct{ client redis.UniversalClient }

func NewRedisIdemStore(client redis.UniversalClient) *RedisIdemStore {
	return &RedisIdemStore{client: client}
}

func (s *RedisIdemStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	value, err := s.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	}
	return value, err == nil, err
}

func (s *RedisIdemStore) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	return s.client.Set(ctx, key, val, ttl).Err()
}

func (s *RedisIdemStore) SetIfAbsent(ctx context.Context, key string, val []byte, ttl time.Duration) (bool, error) {
	return s.client.SetNX(ctx, key, val, ttl).Result()
}

func (s *RedisIdemStore) Delete(ctx context.Context, key string) error {
	return s.client.Del(ctx, key).Err()
}

var unlockScript = redis.NewScript(`
if redis.call("get", KEYS[1]) == ARGV[1] then
  return redis.call("del", KEYS[1])
end
return 0`)

var renewScript = redis.NewScript(`
if redis.call("get", KEYS[1]) == ARGV[1] then
  return redis.call("pexpire", KEYS[1], ARGV[2])
end
return 0`)

func (s *RedisIdemStore) Lock(ctx context.Context, key string, ttl time.Duration) (func(), error) {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	lockKey := "lock:" + key
	token := uuid.NewString()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		acquired, err := s.client.SetNX(ctx, lockKey, token, ttl).Result()
		if err != nil {
			return nil, err
		}
		if acquired {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		renew := time.NewTicker(ttl / 3)
		defer renew.Stop()
		for {
			select {
			case <-stop:
				return
			case <-renew.C:
				_, _ = renewScript.Run(context.Background(), s.client, []string{lockKey}, token, ttl.Milliseconds()).Result()
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			close(stop)
			<-done
			_, _ = unlockScript.Run(context.Background(), s.client, []string{lockKey}, token).Result()
		})
	}, nil
}
