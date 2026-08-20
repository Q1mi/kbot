// Package postgres 提供控制面持久化的连接与事务边界。
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func PoolConfig(databaseURL string) (*pgxpool.Config, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database URL: %w", err)
	}
	config.MaxConns = 20
	config.MinConns = 2
	config.MaxConnLifetime = time.Hour
	config.MaxConnIdleTime = 15 * time.Minute
	config.HealthCheckPeriod = 30 * time.Second
	return config, nil
}

func Open(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	config, err := PoolConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

func WithTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	if pool == nil || fn == nil {
		return fmt.Errorf("pool and transaction function are required")
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// AppendTurn 在一个事务中写入用户消息与助手消息，避免出现半轮会话。
func AppendTurn(ctx context.Context, pool *pgxpool.Pool, conversationID, userContent, assistantContent string) error {
	return WithTx(ctx, pool, func(tx pgx.Tx) error {
		for _, message := range []struct{ role, content string }{{"user", userContent}, {"assistant", assistantContent}} {
			if _, err := tx.Exec(ctx, `INSERT INTO messages (id, conversation_id, role, content) VALUES (gen_random_uuid()::text, $1, $2, $3)`, conversationID, message.role, message.content); err != nil {
				return fmt.Errorf("insert %s message: %w", message.role, err)
			}
		}
		return nil
	})
}
