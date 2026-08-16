package tool_test

// Tool Store 契约测试:memory 与 postgres 跑同一组用例(模式见 iam/prompt 的 contract_test)。

import (
	"context"
	"testing"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/platform"
	"github.com/Q1mi/kbot/internal/platform/tool"
	"github.com/Q1mi/kbot/internal/util"
)

func runToolStoreContract(t *testing.T, newStore func(t *testing.T) tool.Store) {
	ws := "ws-default"

	t.Run("ToolCreateGetList", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		tl := &domain.Tool{ID: util.GenerateID(), WorkspaceID: ws, Name: "weather", SourceType: "rest_api", Description: "查天气", CreatedBy: "u1"}
		if err := s.CreateTool(ctx, tl); err != nil {
			t.Fatalf("CreateTool: %v", err)
		}
		got, err := s.GetTool(ctx, tl.ID)
		if err != nil {
			t.Fatalf("GetTool: %v", err)
		}
		if got.Name != "weather" || got.SourceType != "rest_api" || got.Description != "查天气" {
			t.Fatalf("tool mismatch: %+v", got)
		}
		list, err := s.ListTools(ctx, ws)
		if err != nil {
			t.Fatalf("ListTools: %v", err)
		}
		if len(list) != 1 || list[0].Name != "weather" {
			t.Fatalf("ListTools mismatch: %+v", list)
		}
	})

	t.Run("VersionsCurrentAndStatus", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		tl := &domain.Tool{ID: util.GenerateID(), WorkspaceID: ws, Name: "t", SourceType: "rest_api", CreatedBy: "u1"}
		_ = s.CreateTool(ctx, tl)
		v1 := &domain.ToolVersion{ID: util.GenerateID(), ToolID: tl.ID, Version: 1, SchemaJSON: `{"type":"object"}`, EndpointConfig: `{"url":"http://a"}`, AuthConfig: "{}", RetryPolicy: "{}", Status: "draft", CreatedBy: "u1"}
		v2 := &domain.ToolVersion{ID: util.GenerateID(), ToolID: tl.ID, Version: 2, SchemaJSON: `{"type":"object"}`, EndpointConfig: `{"url":"http://b"}`, AuthConfig: "{}", RetryPolicy: "{}", Status: "draft", CreatedBy: "u1"}
		if err := s.CreateToolVersion(ctx, v1); err != nil {
			t.Fatalf("CreateToolVersion v1: %v", err)
		}
		if err := s.CreateToolVersion(ctx, v2); err != nil {
			t.Fatalf("CreateToolVersion v2: %v", err)
		}
		// 当前版本 = 最新(v2)。
		cur, err := s.GetToolCurrentVersion(ctx, tl.ID)
		if err != nil {
			t.Fatalf("GetToolCurrentVersion: %v", err)
		}
		if cur.Version != 2 || cur.EndpointConfig != `{"url":"http://b"}` {
			t.Fatalf("current version mismatch: %+v", cur)
		}
		// 发布 v2:改状态后 GetToolVersion 反映。
		if err := s.UpdateToolVersionStatus(ctx, cur.ID, "published"); err != nil {
			t.Fatalf("UpdateToolVersionStatus: %v", err)
		}
		got, err := s.GetToolVersion(ctx, cur.ID)
		if err != nil {
			t.Fatalf("GetToolVersion: %v", err)
		}
		if got.Status != "published" || got.SchemaJSON != `{"type":"object"}` {
			t.Fatalf("version after status update mismatch: %+v", got)
		}
	})

	t.Run("TestRunsLastSuccessful", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		tl := &domain.Tool{ID: util.GenerateID(), WorkspaceID: ws, Name: "t", SourceType: "rest_api", CreatedBy: "u1"}
		_ = s.CreateTool(ctx, tl)
		version := &domain.ToolVersion{
			ID: util.GenerateID(), ToolID: tl.ID, Version: 1, SchemaJSON: `{}`, EndpointConfig: `{}`,
			AuthConfig: `{}`, RetryPolicy: `{}`, Status: "draft", CreatedBy: "u1",
		}
		if err := s.CreateToolVersion(ctx, version); err != nil {
			t.Fatalf("CreateToolVersion: %v", err)
		}

		if _, err := s.GetToolLastSuccessfulTestRun(ctx, tl.ID); err == nil {
			t.Fatal("expected error when no successful run")
		}
		boom := "boom"
		_ = s.CreateTestRun(ctx, &domain.ToolTestRun{ID: util.GenerateID(), ToolID: tl.ID, ToolVersionID: version.ID, Input: "{}", Output: "", Status: "error", LatencyMs: 5, Error: &boom})
		if err := s.CreateTestRun(ctx, &domain.ToolTestRun{ID: util.GenerateID(), ToolID: tl.ID, ToolVersionID: version.ID, Input: "{}", Output: "ok", Status: "success", LatencyMs: 12}); err != nil {
			t.Fatalf("CreateTestRun: %v", err)
		}
		run, err := s.GetToolLastSuccessfulTestRun(ctx, tl.ID)
		if err != nil {
			t.Fatalf("GetToolLastSuccessfulTestRun: %v", err)
		}
		if run.Status != "success" || run.Output != "ok" || run.LatencyMs != 12 || run.Error != nil {
			t.Fatalf("test run mismatch: %+v", run)
		}
	})

}

func TestMemoryToolStore_Contract(t *testing.T) {
	runToolStoreContract(t, func(t *testing.T) tool.Store {
		return platform.NewMemoryToolStore()
	})
}
