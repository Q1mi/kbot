package agent_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Q1mi/kbot/internal/platform"
	"github.com/Q1mi/kbot/internal/platform/agent"
	"github.com/Q1mi/kbot/internal/platform/skill"
	"github.com/Q1mi/kbot/internal/platform/tool"
)

func boolValue(value bool) *bool { return &value }

type testKBResolver struct{}

func (testKBResolver) KBExists(_ context.Context, workspaceID, kbID string) (bool, error) {
	return workspaceID == "w1" && kbID == "kb-refund", nil
}

func TestAgentVersionConfigPinsDependenciesAndPromotes(t *testing.T) {
	ctx := context.Background()
	agentStore := platform.NewMemoryAgentStore()
	toolService := tool.NewService(platform.NewMemoryToolStore())
	skillService := skill.NewService(platform.NewMemorySkillStore(), toolService)
	agentService := agent.NewService(agentStore, nil, skillService, toolService).WithKBResolver(testKBResolver{})

	refundTool, err := toolService.CreateTool(ctx, tool.CreateToolRequest{
		WorkspaceID: "w1", Name: "refund_order", SourceType: "rest_api",
		EndpointConfig: `{"url":"https://example.com/refund"}`, CreatedBy: "u1",
	})
	if err != nil {
		t.Fatalf("create tool: %v", err)
	}
	_, skillVersion, err := skillService.CreateSkill(ctx, skill.CreateSkillRequest{
		WorkspaceID: "w1", CreatedBy: "u1", SkillMD: `---
name: refund-order
description: 处理退款
allowed-tools: [refund_order]
allowed-kbs: [kb-refund]
requires_network: true
---
执行退款流程`,
	})
	if err != nil {
		t.Fatalf("create skill: %v", err)
	}
	if err := skillService.Publish(ctx, skillVersion.ID); err != nil {
		t.Fatalf("publish skill: %v", err)
	}

	_, err = agentService.CreateAgent(ctx, agent.CreateAgentRequest{
		WorkspaceID: "w1", Name: "blocked", Template: "customer_support",
		ToolIDs: []string{refundTool.ID}, SkillVersionIDs: []string{skillVersion.ID},
		KBIDs: []string{"kb-refund"}, AllowNetwork: boolValue(false), CreatedBy: "u1",
	})
	if err == nil || !strings.Contains(err.Error(), "requires network") {
		t.Fatalf("expected network policy validation, got %v", err)
	}

	ag, err := agentService.CreateAgent(ctx, agent.CreateAgentRequest{
		WorkspaceID: "w1", Name: "refund", Template: "customer_support",
		ToolIDs: []string{refundTool.ID}, SkillVersionIDs: []string{skillVersion.ID},
		KBIDs: []string{"kb-refund"}, AllowNetwork: boolValue(true), MaxSteps: 8, CreatedBy: "u1",
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	v1, err := agentStore.GetAgentCurrentVersion(ctx, ag.ID, "dev")
	if err != nil {
		t.Fatalf("get v1: %v", err)
	}
	snapshot, err := agentService.GetAgentSnapshotByVersion(ctx, v1.ID)
	if err != nil {
		t.Fatalf("get snapshot: %v", err)
	}
	if !snapshot.AllowNetwork || len(snapshot.Skills) != 1 || len(snapshot.KBIDs) != 1 {
		t.Fatalf("snapshot constraints missing: %+v", snapshot)
	}

	v2, err := agentService.CreateAgentVersion(ctx, ag.ID, "w1", agent.AgentVersionConfig{
		SystemPrompt: "只回答公开问题", AllowNetwork: boolValue(false), MaxSteps: 4,
	}, "u2")
	if err != nil {
		t.Fatalf("create v2: %v", err)
	}
	if v2.Version != 2 || v2.Config.AllowNetwork == nil || *v2.Config.AllowNetwork {
		t.Fatalf("unexpected v2: %+v", v2)
	}
	if err := agentService.PromoteAgentVersion(ctx, ag.ID, "w1", "prod", v1.ID); err != nil {
		t.Fatalf("promote v1: %v", err)
	}
	prod, err := agentStore.GetAgentCurrentVersion(ctx, ag.ID, "prod")
	if err != nil || prod.ID != v1.ID {
		t.Fatalf("prod version mismatch: %+v err=%v", prod, err)
	}
	conversation, err := agentService.CreateConversationInEnv(ctx, ag.ID, "prod", "user-1")
	if err != nil || conversation.AgentVersionID != v1.ID {
		t.Fatalf("prod conversation must pin v1: %+v err=%v", conversation, err)
	}
}
