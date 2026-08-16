package agent_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/platform"
	"github.com/Q1mi/kbot/internal/platform/agent"
	"github.com/Q1mi/kbot/internal/platform/prompt"
	"github.com/Q1mi/kbot/internal/runtime/promptcache"
)

func TestConversationPinsResolvedPromptConfig(t *testing.T) {
	ctx := context.Background()
	promptSvc := prompt.NewService(platform.NewMemoryPromptStore(), promptcache.NewCache(), prompt.NoopPublisher{})
	p, _, err := promptSvc.CreatePrompt(ctx, prompt.CreatePromptRequest{
		WorkspaceID: "w1", Name: "support", Template: "baseline", CreatedBy: "u1",
	})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := promptSvc.CreateVersionConfigured(ctx, p.ID, "candidate", "{}",
		"profile-v2", domain.GenerationConfig{MaxOutputTokens: intPtr(512)}, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := promptSvc.StartRollout(ctx, p.ID, prompt.EnvDev, candidate.ID, 99, "u1"); err != nil {
		t.Fatal(err)
	}

	agentSvc := agent.NewService(platform.NewMemoryAgentStore(), promptSvc, nil, nil)
	ag, err := agentSvc.CreateAgent(ctx, agent.CreateAgentRequest{
		WorkspaceID: "w1", Name: "bot", Template: "custom",
		SystemPromptID: p.ID, PromptEnv: prompt.EnvDev, CreatedBy: "u1",
	})
	if err != nil {
		t.Fatal(err)
	}
	conv, err := agentSvc.CreateConversation(ctx, ag.ID, "candidate-user")
	if err != nil {
		t.Fatal(err)
	}
	var runtime domain.ConversationRuntimeConfig
	if err := json.Unmarshal([]byte(conv.RuntimeConfigJSON), &runtime); err != nil {
		t.Fatal(err)
	}
	if runtime.PromptVersionID == "" || runtime.SystemPrompt == "" {
		t.Fatalf("conversation did not pin prompt config: %+v", runtime)
	}
	first := conv.RuntimeConfigJSON
	loaded, err := agentSvc.LoadConversation(ctx, conv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RuntimeConfigJSON != first {
		t.Fatal("conversation runtime config changed after creation")
	}
}

func TestRecordConversationTraceIDPreservesRuntimeConfig(t *testing.T) {
	ctx := context.Background()
	store := platform.NewMemoryAgentStore()
	service := agent.NewService(store, nil, nil, nil)
	runtimeBefore := domain.ConversationRuntimeConfig{
		SystemPrompt: "课程系统提示词", PromptVersionID: "prompt-v1",
		UserPromptVariables: map[string]any{"order_id": "TTS-1001"},
	}
	raw, err := json.Marshal(runtimeBefore)
	if err != nil {
		t.Fatal(err)
	}
	conversation := &domain.Conversation{
		ID: "conversation-1", WorkspaceID: "w1", UserID: "u1",
		RuntimeConfigJSON: string(raw),
	}
	if err := store.CreateConversation(ctx, conversation); err != nil {
		t.Fatal(err)
	}
	if err := service.RecordConversationTraceID(ctx, conversation.ID, "trace-123"); err != nil {
		t.Fatal(err)
	}
	loaded, err := service.LoadConversation(ctx, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	var runtimeAfter domain.ConversationRuntimeConfig
	if err := json.Unmarshal([]byte(loaded.RuntimeConfigJSON), &runtimeAfter); err != nil {
		t.Fatal(err)
	}
	if runtimeAfter.LatestTraceID != "trace-123" || runtimeAfter.SystemPrompt != runtimeBefore.SystemPrompt ||
		runtimeAfter.PromptVersionID != runtimeBefore.PromptVersionID ||
		!reflect.DeepEqual(runtimeAfter.UserPromptVariables, runtimeBefore.UserPromptVariables) {
		t.Fatalf("runtime config was not preserved: %+v", runtimeAfter)
	}
}

func intPtr(v int) *int { return &v }
