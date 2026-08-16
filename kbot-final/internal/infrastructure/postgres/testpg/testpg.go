//go:build integration

// Package testpg 为集成测试提供一个真实 Postgres(pgvector)实例 + 已迁移的 schema。
//
// 两种模式:
//   - 若设置了 KBOT_TEST_DATABASE_URL,直接连它(适合本地复用一个常驻容器,迭代快);
//   - 否则用 ory/dockertest 拉起 pgvector/pgvector:pg16 容器,测试结束自动销毁。
//
// 两种模式都会先把 migrations/ 全套跑一遍,保证 schema 与生产一致。
// 仅在 `-tags=integration` 下编译,普通 `go build` / `go test` 不引入 dockertest。
package testpg

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
)

// Start 返回一个连到已迁移 schema 的连接池。t.Cleanup 负责收尾(销毁容器或关池)。
func Start(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if url := os.Getenv("KBOT_TEST_DATABASE_URL"); url != "" {
		return startFromURL(t, url)
	}
	return startFromDocker(t)
}

func startFromURL(t *testing.T, url string) *pgxpool.Pool {
	t.Helper()
	if err := runMigrations(url); err != nil {
		t.Fatalf("testpg: 迁移失败: %v", err)
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("testpg: 连接失败: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func startFromDocker(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := dockertest.NewPool("")
	if err != nil {
		t.Fatalf("testpg: 连不上 Docker(集成测试需要 Docker daemon): %v", err)
	}
	if err := pool.Client.Ping(); err != nil {
		t.Fatalf("testpg: Docker daemon 未就绪: %v", err)
	}

	res, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "pgvector/pgvector",
		Tag:        "pg16",
		Env: []string{
			"POSTGRES_USER=kbot",
			"POSTGRES_PASSWORD=kbot",
			"POSTGRES_DB=kbot",
			"listen_addresses='*'",
		},
	}, func(c *docker.HostConfig) {
		c.AutoRemove = true
		c.RestartPolicy = docker.RestartPolicy{Name: "no"}
	})
	if err != nil {
		t.Fatalf("testpg: 启动容器失败: %v", err)
	}
	t.Cleanup(func() { _ = pool.Purge(res) })

	hostPort := res.GetPort("5432/tcp")
	url := fmt.Sprintf("postgres://kbot:kbot@localhost:%s/kbot?sslmode=disable", hostPort)

	res.Expire(600) // 兜底:10 分钟后强制回收,防止测试 panic 泄漏容器

	var pgpool *pgxpool.Pool
	pool.MaxWait = 60 * time.Second
	if err := pool.Retry(func() error {
		p, err := pgxpool.New(context.Background(), url)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := p.Ping(ctx); err != nil {
			p.Close()
			return err
		}
		pgpool = p
		return nil
	}); err != nil {
		t.Fatalf("testpg: 等待 Postgres 就绪超时: %v", err)
	}
	t.Cleanup(pgpool.Close)

	if err := runMigrations(url); err != nil {
		t.Fatalf("testpg: 迁移失败: %v", err)
	}
	return pgpool
}

// runMigrations 把仓库根 migrations/ 全套应用到目标库。
func runMigrations(rawURL string) error {
	dbURL := rawURL
	if strings.HasPrefix(dbURL, "postgres://") {
		dbURL = "pgx5://" + strings.TrimPrefix(dbURL, "postgres://")
	} else if strings.HasPrefix(dbURL, "postgresql://") {
		dbURL = "pgx5://" + strings.TrimPrefix(dbURL, "postgresql://")
	}
	dir, err := findMigrationsDir()
	if err != nil {
		return err
	}
	m, err := migrate.New("file://"+dir, dbURL)
	if err != nil {
		return err
	}
	defer m.Close()
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return err
	}
	return nil
}

// findMigrationsDir 从当前测试工作目录向上找到含 go.mod 的仓库根,返回其 migrations/ 绝对路径。
func findMigrationsDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for dir := wd; ; {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			mig := filepath.Join(dir, "migrations")
			if _, err := os.Stat(mig); err == nil {
				return mig, nil
			}
			return "", fmt.Errorf("找到 go.mod 于 %s 但无 migrations/ 目录", dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("从 %s 向上未找到 go.mod", wd)
		}
		dir = parent
	}
}
