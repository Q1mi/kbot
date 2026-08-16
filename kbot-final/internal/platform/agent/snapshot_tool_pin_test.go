package agent_test

// 「不可变快照」的教学用回归测试。
//
// 课程核心概念(架构综述 / 讲义 §14.6):控制面与数据面通过 immutable 快照解耦——
// 老对话 pin 住它创建时的 AgentVersion,之后改配置(这里是给工具发新版本)只影响新对话。
//
// 本测试坐实:给一个工具发布新版本后,在它【之前】创建的 Agent 快照仍执行旧版本。
// 修复前快照里存的是工具"注册 ID",运行时用 GetToolCurrentVersion 实时取当前版本,
// 老对话会"偷偷换脑子"——这条用例就会失败。

import (
	"context"
	"testing"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/platform"
	"github.com/Q1mi/kbot/internal/platform/agent"
	"github.com/Q1mi/kbot/internal/platform/tool"
	"github.com/Q1mi/kbot/internal/runtime/tooling"
	"github.com/Q1mi/kbot/internal/util"
)

func TestSnapshotPinsToolVersion(t *testing.T) {
	ctx := context.Background()

	toolStore := platform.NewMemoryToolStore()
	agentStore := platform.NewMemoryAgentStore()
	toolSvc := tool.NewService(toolStore)
	// prompt/skills 此用例用不到 → nil;tools 传 toolSvc(实现 agent.ToolResolver)。
	agentSvc := agent.NewService(agentStore, nil, nil, toolSvc)

	// 1) 建工具,初始版本 v1:REST 指向 /v1,参数为 q。
	tl, err := toolSvc.CreateTool(ctx, tool.CreateToolRequest{
		WorkspaceID:    "w1",
		Name:           "weather",
		SourceType:     "rest_api",
		EndpointConfig: `{"url":"https://api.example.com/v1"}`,
		SchemaJSON:     `{"type":"object","properties":{"q":{"type":"string"}}}`,
		CreatedBy:      "u1",
	})
	if err != nil {
		t.Fatalf("CreateTool: %v", err)
	}
	v1, err := toolStore.GetToolCurrentVersion(ctx, tl.ID)
	if err != nil {
		t.Fatalf("get v1: %v", err)
	}

	// 2) 建 Agent —— 此刻快照必须 pin 死 v1 的【版本 ID】。
	ag, err := agentSvc.CreateAgent(ctx, agent.CreateAgentRequest{
		WorkspaceID:  "w1",
		Name:         "bot",
		SystemPrompt: "你是助手",
		ToolIDs:      []string{tl.ID}, // 传的是工具注册 ID;pin 在 CreateAgent 内部完成
		CreatedBy:    "u1",
	})
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	oldVer, err := agentStore.GetAgentCurrentVersion(ctx, ag.ID, "dev")
	if err != nil {
		t.Fatalf("get agent version: %v", err)
	}

	// 3) 给工具发新版本 v2:REST 指向 /v2,参数改名 query,并成为 current。
	v2 := &domain.ToolVersion{
		ID:             util.GenerateID(),
		ToolID:         tl.ID,
		Version:        2,
		Status:         "published",
		EndpointConfig: `{"url":"https://api.example.com/v2"}`,
		SchemaJSON:     `{"type":"object","properties":{"query":{"type":"string"}}}`,
		CreatedBy:      "u1",
	}
	if err := toolStore.CreateToolVersion(ctx, v2); err != nil {
		t.Fatalf("create v2: %v", err)
	}
	if cur, _ := toolStore.GetToolCurrentVersion(ctx, tl.ID); cur.ID != v2.ID {
		t.Fatalf("precondition failed: current should be v2, got %s", cur.ID)
	}

	// 4) 解析老 Agent 的快照 —— 必须仍指向 v1,而非漂移到 v2。
	snap, err := agentSvc.GetAgentSnapshotByVersion(ctx, oldVer.ID)
	if err != nil {
		t.Fatalf("GetAgentSnapshotByVersion: %v", err)
	}
	if len(snap.ToolVersionIDs) != 1 {
		t.Fatalf("expected 1 pinned tool version, got %d", len(snap.ToolVersionIDs))
	}
	if snap.ToolVersionIDs[0] != v1.ID {
		t.Fatalf("快照漂移:pinned=%s 应为 v1=%s(v2=%s)", snap.ToolVersionIDs[0], v1.ID, v2.ID)
	}

	// 5) 配置层坐实:pin 的版本是 v1(/v1 + 参数 q);current 才是 v2(/v2 + 参数 query)。
	pinnedCfg, _, err := toolSvc.GetToolConfigByVersion(ctx, snap.ToolVersionIDs[0])
	if err != nil {
		t.Fatalf("GetToolConfigByVersion: %v", err)
	}
	if got := pinnedCfg.EndpointConfig["url"]; got != "https://api.example.com/v1" {
		t.Fatalf("pinned 工具应命中 /v1,实际 %v", got)
	}
	if props, _ := pinnedCfg.Schema["properties"].(map[string]any); props["q"] == nil {
		t.Fatalf("pinned 工具 schema 应含 v1 的参数 q,实际 %v", pinnedCfg.Schema)
	}

	currentCfg, err := toolSvc.GetToolConfig(ctx, tl.ID) // 取 current = v2
	if err != nil {
		t.Fatalf("GetToolConfig: %v", err)
	}
	if got := currentCfg.EndpointConfig["url"]; got != "https://api.example.com/v2" {
		t.Fatalf("current 工具应命中 /v2,实际 %v", got)
	}

	// 6) Registry 层同样成立:BuildByVersion(pinned) 走 v1,Build(toolID) 走 current=v2。
	reg := tooling.NewRegistry(toolSvc, nil)
	if _, err := reg.BuildByVersion(ctx, snap.ToolVersionIDs[0]); err != nil {
		t.Fatalf("BuildByVersion(pinned v1): %v", err)
	}
	if _, err := reg.Build(ctx, tl.ID); err != nil {
		t.Fatalf("Build(current v2): %v", err)
	}
}
