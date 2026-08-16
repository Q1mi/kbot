package main

import "testing"

func TestMigrateDatabaseURLUsesRegisteredPGX5Scheme(t *testing.T) {
	got := migrateDatabaseURL("postgres://user:pass@db:5432/kbot?sslmode=disable")
	if got != "pgx5://user:pass@db:5432/kbot?sslmode=disable" {
		t.Fatalf("URL = %q", got)
	}
}
