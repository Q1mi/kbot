package integration_test

// 人在环审批闭环 E2E：敏感工具触发暂停，批准后从 checkpoint 续跑完成。
// 跑在内存 platform + 脚本化 ChatModel上,无需真实 LLM。复用 e2e_test.go 里的 scriptedChatModel
//(第 1 次有工具时调工具、第 2 次给最终回答)。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Q1mi/kbot/internal/platform"
	"github.com/Q1mi/kbot/internal/platform/agent"
	"github.com/Q1mi/kbot/internal/platform/approval"
	"github.com/Q1mi/kbot/internal/platform/prompt"
	"github.com/Q1mi/kbot/internal/platform/tool"
	"github.com/Q1mi/kbot/internal/runtime/engine"
)

func TestSensitiveToolApprovalE2E(t *testing.T) {
	ctx := context.Background()
	plat := platform.NewService(nil, nil, make([]byte, 32), prompt.NoopPublisher{}, nil, nil)

	// 一个被工具命中的 REST 端点(用 hits 验证"批准后才执行")。
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		fmt.Fprint(w, `{"status":"refunded"}`)
	}))
	defer srv.Close()

	// 注册并发布一个**敏感** REST 工具 refund_money。
	endpoint, _ := json.Marshal(map[string]string{"method": "POST", "url": srv.URL})
	tl, err := plat.Tool.CreateTool(ctx, tool.CreateToolRequest{
		WorkspaceID: "w1", Name: "refund_money", SourceType: "rest_api",
		SchemaJSON:     `{"type":"object","properties":{"q":{"type":"string"}}}`,
		EndpointConfig: string(endpoint), Sensitive: true, CreatedBy: "u1",
	})
	if err != nil {
		t.Fatalf("create tool: %v", err)
	}
	if _, err := plat.Tool.RecordTestRun(ctx, tl.ID, "{}", "ok", "success", 5, nil); err != nil {
		t.Fatal(err)
	}
	if err := plat.Tool.PublishTool(ctx, tl.ID); err != nil {
		t.Fatalf("publish tool: %v", err)
	}

	ag, err := plat.Agent.CreateAgent(ctx, agent.CreateAgentRequest{
		WorkspaceID: "w1", Name: "bot", SystemPrompt: "你是助手", ToolIDs: []string{tl.ID}, CreatedBy: "u1",
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	eng := engine.NewEngineWithChatModel(plat.Agent, &scriptedChatModel{}, plat.Registry).
		WithGuard(plat.Guard).WithAudit(plat.Audit).
		WithApprovals(plat.ApprovalGate())

	// 1) 对话:敏感工具应被拦下,发出 await_approval,工具尚未执行。
	ch, err := eng.ChatStream(ctx, engine.ChatStreamRequest{AgentID: ag.ID, Message: "给我退款", UserID: "u1"})
	if err != nil {
		t.Fatalf("chat stream: %v", err)
	}
	var awaited string
	for ev := range ch {
		if ev.Type == engine.EventAwaitApproval {
			awaited = ev.Text
		}
	}
	if awaited == "" {
		t.Fatal("expected EventAwaitApproval, got none")
	}
	if hits != 0 {
		t.Fatalf("sensitive tool must NOT execute before approval, hits=%d", hits)
	}

	// 2) 待审批队列里有一条;取出会话 ID。
	pend, err := plat.Approvals.ListPending(ctx, "w1")
	if err != nil || len(pend) != 1 {
		t.Fatalf("ListPending: %v len=%d", err, len(pend))
	}
	if pend[0].Action != "refund_money" {
		t.Fatalf("pending action mismatch: %q", pend[0].Action)
	}
	convID := pend[0].ConversationID
	if _, err := eng.Resume(ctx, convID, pend[0].ID); err == nil {
		t.Fatal("pending approval must not be resumable")
	}
	if hits != 0 {
		t.Fatalf("unapproved resume must not execute the tool, hits=%d", hits)
	}

	// 3) 批准。
	if _, err := plat.Approvals.ResolvePending(ctx, pend[0].ID, "w1", approval.StatusApproved, "admin-1"); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	// 4) 续跑:从 checkpoint 接着执行被批准的工具 → 给最终回答。
	answer, err := eng.Resume(ctx, convID, pend[0].ID)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if !strings.Contains(answer, "完成") {
		t.Fatalf("unexpected resumed answer: %q", answer)
	}
	if hits != 1 {
		t.Fatalf("sensitive tool must execute exactly once on resume, hits=%d", hits)
	}
}
