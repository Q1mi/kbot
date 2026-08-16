package promptcache

import (
	"context"
	"log"
	"strings"

	"github.com/redis/go-redis/v9"
)

// Refresher 在收到失效通知后重新拉取并编译某 prompt@env（由 Platform 侧提供）。
type Refresher func(ctx context.Context, promptID, env string) error

// Subscriber 订阅 "prompt:*:invalidate" 频道，收到通知后异步重拉编译产物
// （设计文档 §4.2 / 讲义 §14.4 的订阅侧）。
type Subscriber struct {
	rds     *redis.Client
	refresh Refresher
}

// NewSubscriber 创建订阅器。
func NewSubscriber(rds *redis.Client, refresh Refresher) *Subscriber {
	return &Subscriber{rds: rds, refresh: refresh}
}

// Run 阻塞订阅，直到 ctx 取消或出错。一般在独立 goroutine 里跑。
func (s *Subscriber) Run(ctx context.Context) error {
	pubsub := s.rds.PSubscribe(ctx, "prompt:*:invalidate")
	defer pubsub.Close()

	ch := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-ch:
			if !ok {
				return nil
			}
			promptID, env, parsed := parseChannel(msg.Channel)
			if !parsed {
				continue
			}
			// 异步重拉，单个失败不影响其它通知。
			go func() {
				if err := s.refresh(ctx, promptID, env); err != nil {
					log.Printf("promptcache refresh failed for %s@%s: %v", promptID, env, err)
				}
			}()
		}
	}
}

// parseChannel 解析 "prompt:{id}:{env}:invalidate"。
func parseChannel(channel string) (promptID, env string, ok bool) {
	parts := strings.Split(channel, ":")
	if len(parts) != 4 || parts[0] != "prompt" || parts[3] != "invalidate" {
		return "", "", false
	}
	return parts[1], parts[2], true
}
