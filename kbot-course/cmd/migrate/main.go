// Command migrate applies the course PostgreSQL migrations.
package main

import (
	"errors"
	"flag"
	"log"
	"os"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func main() {
	up := flag.Bool("up", false, "apply all pending migrations")
	down := flag.Int("down", 0, "roll back N migrations")
	showVersion := flag.Bool("version", false, "print current migration version")
	path := flag.String("path", "migrations", "migration directory")
	flag.Parse()

	databaseURL := os.Getenv("KBOT_DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("KBOT_DATABASE_URL is required")
	}
	databaseURL = migrateDatabaseURL(databaseURL)
	runner, err := migrate.New("file://"+*path, databaseURL)
	if err != nil {
		log.Fatalf("initialize migrations: %v", err)
	}
	defer runner.Close()

	switch {
	case *showVersion:
		version, dirty, err := runner.Version()
		if errors.Is(err, migrate.ErrNilVersion) {
			log.Print("migration version: none")
			return
		}
		if err != nil {
			log.Fatalf("read migration version: %v", err)
		}
		log.Printf("migration version: %d dirty=%v", version, dirty)
	case *down > 0:
		if err := runner.Steps(-*down); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			log.Fatalf("roll back migrations: %v", err)
		}
	case *up:
		if err := runner.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			log.Fatalf("apply migrations: %v", err)
		}
	default:
		flag.Usage()
		os.Exit(2)
	}
}

func migrateDatabaseURL(raw string) string {
	if strings.HasPrefix(raw, "postgres://") {
		return "pgx5://" + strings.TrimPrefix(raw, "postgres://")
	}
	if strings.HasPrefix(raw, "postgresql://") {
		return "pgx5://" + strings.TrimPrefix(raw, "postgresql://")
	}
	return raw
}
