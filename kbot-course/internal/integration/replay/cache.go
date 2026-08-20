// Package replay provides the small, in-memory replay window used by signed
// inbound channel adapters. A distributed deployment can replace it with Redis
// while keeping the handler contract unchanged.
package replay

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const DefaultWindow = 5 * time.Minute

type Cache struct {
	mu     sync.Mutex
	seen   map[string]time.Time
	window time.Duration
	now    func() time.Time
}

type Guard interface {
	Accept(timestamp, key string) (duplicate bool, err error)
}

func New(window time.Duration) *Cache {
	if window <= 0 {
		window = DefaultWindow
	}
	return &Cache{seen: make(map[string]time.Time), window: window, now: time.Now}
}

type PostgresCache struct {
	pool   *pgxpool.Pool
	window time.Duration
	now    func() time.Time
}

func NewPostgres(pool *pgxpool.Pool, window time.Duration) *PostgresCache {
	if window <= 0 {
		window = DefaultWindow
	}
	return &PostgresCache{pool: pool, window: window, now: time.Now}
}

func (c *PostgresCache) Accept(timestamp, key string) (bool, error) {
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || key == "" || c.pool == nil {
		return false, fmt.Errorf("timestamp, replay key and store are required")
	}
	now := c.now()
	requestTime := time.Unix(seconds, 0)
	if requestTime.Before(now.Add(-c.window)) || requestTime.After(now.Add(c.window)) {
		return false, fmt.Errorf("request timestamp is outside the replay window")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `DELETE FROM integration_replays WHERE replay_key=$1 AND expires_at <= $2`, key, now); err != nil {
		return false, err
	}
	command, err := tx.Exec(ctx, `INSERT INTO integration_replays (replay_key,expires_at) VALUES ($1,$2) ON CONFLICT DO NOTHING`, key, now.Add(c.window))
	if err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return command.RowsAffected() == 0, nil
}

// Accept validates a Unix-seconds timestamp and records the signed request key.
// duplicate is true only when the timestamp is fresh but the key was seen.
func (c *Cache) Accept(timestamp, key string) (duplicate bool, err error) {
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || key == "" {
		return false, fmt.Errorf("timestamp and replay key are required")
	}
	now := c.now()
	requestTime := time.Unix(seconds, 0)
	if requestTime.Before(now.Add(-c.window)) || requestTime.After(now.Add(c.window)) {
		return false, fmt.Errorf("request timestamp is outside the replay window")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for item, expiresAt := range c.seen {
		if !expiresAt.After(now) {
			delete(c.seen, item)
		}
	}
	if expiresAt, exists := c.seen[key]; exists && expiresAt.After(now) {
		return true, nil
	}
	c.seen[key] = now.Add(c.window)
	return false, nil
}
