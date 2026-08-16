// Command migrate 用 golang-migrate 跑数据库迁移。
//
// 用法:
//
//	migrate -up                上到最新
//	migrate -down 1            回退 1 步
//	migrate -version           打印当前版本
//
// 连接串从 KBOT_DATABASE_URL 读;迁移文件目录由 -path 指定(默认 ./migrations)。
// 容器内由 docker-compose 的 migrate 服务以 ["-up"] 跑一遍,app/worker 依赖其成功完成。
package main

import (
	"errors"
	"flag"
	"log"
	"os"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // 注册 pgx5:// 数据库驱动
	_ "github.com/golang-migrate/migrate/v4/source/file"     // 注册 file:// 源
)

func main() {
	var (
		up          = flag.Bool("up", false, "迁移到最新版本")
		down        = flag.Int("down", 0, "回退 N 步")
		showVer     = flag.Bool("version", false, "打印当前迁移版本")
		path        = flag.String("path", "migrations", "迁移文件目录")
		databaseEnv = flag.String("database-env", "KBOT_DATABASE_URL", "读取连接串的环境变量名")
	)
	flag.Parse()

	rawURL := os.Getenv(*databaseEnv)
	if rawURL == "" {
		log.Fatalf("缺少数据库连接串:环境变量 %s 为空", *databaseEnv)
	}
	// golang-migrate 的 pgx/v5 驱动注册在 pgx5:// scheme 下;把标准 postgres:// 改写过去。
	dbURL := rawURL
	if strings.HasPrefix(dbURL, "postgres://") {
		dbURL = "pgx5://" + strings.TrimPrefix(dbURL, "postgres://")
	} else if strings.HasPrefix(dbURL, "postgresql://") {
		dbURL = "pgx5://" + strings.TrimPrefix(dbURL, "postgresql://")
	}

	m, err := migrate.New("file://"+*path, dbURL)
	if err != nil {
		log.Fatalf("初始化 migrate 失败:%v", err)
	}
	defer m.Close()

	switch {
	case *showVer:
		v, dirty, err := m.Version()
		if errors.Is(err, migrate.ErrNilVersion) {
			log.Println("当前版本:无(数据库尚未迁移)")
			return
		}
		if err != nil {
			log.Fatalf("读取版本失败:%v", err)
		}
		log.Printf("当前版本:%d(dirty=%v)", v, dirty)

	case *down > 0:
		if err := m.Steps(-*down); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			log.Fatalf("回退失败:%v", err)
		}
		log.Printf("✅ 已回退 %d 步", *down)

	case *up:
		if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			log.Fatalf("迁移失败:%v", err)
		}
		log.Println("✅ 迁移到最新版本完成")

	default:
		flag.Usage()
		os.Exit(2)
	}
}
