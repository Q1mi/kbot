//go:build integration

package prompt_test

// PG 版 Prompt Store 契约测试。需 Docker(或 KBOT_TEST_DATABASE_URL)。

import (
	"context"
	"testing"

	"github.com/Q1mi/kbot/internal/domain"
	pgstore "github.com/Q1mi/kbot/internal/infrastructure/postgres/sqlc"
	"github.com/Q1mi/kbot/internal/infrastructure/postgres/testpg"
	"github.com/Q1mi/kbot/internal/platform/prompt"
	"github.com/Q1mi/kbot/internal/runtime/promptcache"
)

func TestPostgresPromptStore_Contract(t *testing.T) {
	pool := testpg.Start(t)
	runPromptStoreContract(t, func(t *testing.T) prompt.Store {
		if _, err := pool.Exec(context.Background(),
			`TRUNCATE prompts, prompt_versions, prompt_envs, prompt_experiments CASCADE`); err != nil {
			t.Fatalf("truncate: %v", err)
		}
		return prompt.NewPostgresStore(pool, pgstore.New(pool))
	})
}

// TestPostgresPromptService_PersistsVersionConfig 防止基础版本行回填时把
// PromptVersion 上尚未写入 prompt_version_configs 的模型参数清空。
func TestPostgresPromptService_PersistsVersionConfig(t *testing.T) {
	pool := testpg.Start(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`TRUNCATE prompt_version_configs, prompt_envs, prompt_versions, prompts CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	store := prompt.NewPostgresStore(pool, pgstore.New(pool))
	svc := prompt.NewService(store, promptcache.NewCache(), prompt.NoopPublisher{})
	temperature := float32(0.2)
	p, created, err := svc.CreatePrompt(ctx, prompt.CreatePromptRequest{
		WorkspaceID: "ws-config", Name: "configured", Template: "hello", CreatedBy: "u1",
		GenerationConfig: domain.GenerationConfig{Temperature: &temperature},
	})
	if err != nil {
		t.Fatalf("CreatePrompt: %v", err)
	}
	if created.GenerationConfig.Temperature == nil || *created.GenerationConfig.Temperature != temperature {
		t.Fatalf("returned version lost generation config: %+v", created.GenerationConfig)
	}
	loaded, err := store.GetPromptVersionByNumber(ctx, p.ID, 1)
	if err != nil {
		t.Fatalf("GetPromptVersionByNumber: %v", err)
	}
	if loaded.GenerationConfig.Temperature == nil || *loaded.GenerationConfig.Temperature != temperature {
		t.Fatalf("persisted version lost generation config: %+v", loaded.GenerationConfig)
	}
}

// TestPostgresPromptService_PromoteFlow 是服务层回归:CreatePrompt 内部会 CreateVersion + Promote,
// Promote 校验 version.PromptID == promptID。PG 把 ID 规范化为 canonical UUID,若 store 不回写 canonical ID,
// Service 仍持 32-hex,归属校验会失败(docker compose 实测发现的 400)。本测试守住这条。
func TestPostgresPromptService_PromoteFlow(t *testing.T) {
	pool := testpg.Start(t)
	if _, err := pool.Exec(context.Background(),
		`TRUNCATE prompts, prompt_versions, prompt_envs, prompt_experiments CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	svc := prompt.NewService(prompt.NewPostgresStore(pool, pgstore.New(pool)), promptcache.NewCache(), prompt.NoopPublisher{})
	ctx := context.Background()
	p, v, err := svc.CreatePrompt(ctx, prompt.CreatePromptRequest{
		WorkspaceID: "ws-default", Name: "sys", Template: "你好 {{.name}}", CreatedBy: "u1",
	})
	if err != nil {
		t.Fatalf("CreatePrompt(含 CreateVersion+Promote): %v", err)
	}
	// dev 环境应已绑定到 v1,可渲染。
	if _, err := svc.Render(ctx, p.ID, prompt.EnvDev, "u1", map[string]any{"name": "世界"}); err != nil {
		t.Fatalf("Render after promote: %v", err)
	}
	_ = v
}

func TestPostgresPromptService_CompleteRolloutTransaction(t *testing.T) {
	pool := testpg.Start(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		TRUNCATE prompt_rollout_events, prompt_experiments, prompt_envs,
		  prompt_version_configs, prompt_versions, prompts CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	svc := prompt.NewService(
		prompt.NewPostgresStore(pool, pgstore.New(pool)),
		promptcache.NewCache(),
		prompt.NoopPublisher{},
	)
	p, _, err := svc.CreatePrompt(ctx, prompt.CreatePromptRequest{
		WorkspaceID: "ws-rollout", Name: "rollout", Template: "baseline", CreatedBy: "u1",
	})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := svc.CreateVersion(ctx, p.ID, "candidate", "{}", "u1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.StartRollout(ctx, p.ID, prompt.EnvDev, candidate.ID, 20, "u1"); err != nil {
		t.Fatal(err)
	}
	if err := svc.CompleteRollout(ctx, p.ID, prompt.EnvDev, "u1"); err != nil {
		t.Fatal(err)
	}

	var boundVersion, status, action string
	var traffic, fromTraffic, toTraffic int
	err = pool.QueryRow(ctx, `
		SELECT pe.version_id::text,px.status,px.traffic_percent,re.action,
		       re.from_traffic,re.to_traffic
		FROM prompt_envs pe
		JOIN prompt_experiments px ON px.prompt_id=pe.prompt_id AND px.env=pe.env
		JOIN prompt_rollout_events re ON re.experiment_id=px.id
		WHERE pe.prompt_id=$1 AND pe.env=$2 AND re.action='completed'`,
		p.ID, prompt.EnvDev).
		Scan(&boundVersion, &status, &traffic, &action, &fromTraffic, &toTraffic)
	if err != nil {
		t.Fatal(err)
	}
	if boundVersion != candidate.ID || status != "completed" || traffic != 100 ||
		action != "completed" || fromTraffic != 20 || toTraffic != 100 {
		t.Fatalf("unexpected completed rollout state: version=%s status=%s traffic=%d event=%s %d→%d",
			boundVersion, status, traffic, action, fromTraffic, toTraffic)
	}
}
