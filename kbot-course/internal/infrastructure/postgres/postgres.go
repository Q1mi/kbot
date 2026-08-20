// Package postgres 提供控制面持久化的连接与事务边界。
package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotImplemented = errors.New("PostgreSQL persistence is implemented in 15-end")

func Open(context.Context, string) (*pgxpool.Pool, error) { return nil, ErrNotImplemented }
