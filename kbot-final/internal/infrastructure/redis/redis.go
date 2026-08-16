// Package redis 提供 Redis 客户端管理
package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// MustOpen 创建Redis客户端，失败时panic
func MustOpen(ctx context.Context, redisURL string) *redis.Client {
	client, err := Open(ctx, redisURL)
	if err != nil {
		panic(fmt.Sprintf("failed to connect to redis: %v", err))
	}
	return client
}

// Open 创建Redis客户端
func Open(ctx context.Context, redisURL string) (*redis.Client, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis URL: %w", err)
	}

	client := redis.NewClient(opt)

	// 测试连接
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	return client, nil
}
