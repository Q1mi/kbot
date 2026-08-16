package postgres

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestPoolConfigAppliesProductionBounds(t *testing.T) {
	config, err := PoolConfig("postgres://user:pass@localhost:5432/kbot?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if config.MaxConns != 20 || config.MinConns != 2 {
		t.Fatalf("pool bounds: min=%d max=%d", config.MinConns, config.MaxConns)
	}
	if config.MaxConnLifetime != time.Hour || config.HealthCheckPeriod != 30*time.Second {
		t.Fatalf("pool durations are incorrect")
	}
}

func TestCoreMigrationBindsConversationToWorkspaceAgentVersionTuple(t *testing.T) {
	raw, err := os.ReadFile("../../../migrations/000001_core.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	schema := strings.Join(strings.Fields(string(raw)), " ")
	if !strings.Contains(schema, "UNIQUE (id, workspace_id, agent_id)") ||
		!strings.Contains(schema, "FOREIGN KEY (agent_version_id, workspace_id, agent_id) REFERENCES agent_versions (id, workspace_id, agent_id)") {
		t.Fatalf("conversation foreign key is not scoped to agent version identity:\n%s", raw)
	}
}

func TestPoolConfigRejectsInvalidURL(t *testing.T) {
	if _, err := PoolConfig("://bad"); err == nil {
		t.Fatal("expected invalid URL")
	}
}
