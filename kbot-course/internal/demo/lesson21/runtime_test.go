package lesson21_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Q1mi/kbot/internal/demo/lesson21"
	"github.com/Q1mi/kbot/internal/demo/logistics"
	"github.com/Q1mi/kbot/internal/runtime/engine"
	"github.com/Q1mi/kbot/internal/runtime/tooling"
)

func logisticsServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewUnstartedServer(nil)
	server.Config.Handler = logistics.NewHandler("http://" + server.Listener.Addr().String())
	server.Start()
	t.Cleanup(server.Close)
	return server
}

func TestRuntimeCallsExternalLogisticsThroughA2A(t *testing.T) {
	server := logisticsServer(t)
	ctx := context.Background()
	runtime, err := lesson21.NewRuntime(ctx, server.URL+"/.well-known/agent-card.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := runtime.Agents.CreateConversation(ctx, lesson21.WorkspaceID, runtime.AgentID, "dev", "student")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ message, want string }{
		{"查询 CB-1001", "清关中"}, {"查询 CB-1002", "运输中"}, {"查询 CB-1003", "已签收"}, {"查询 CB-9999", "未找到订单"},
	} {
		var answer string
		var events []string
		err = runtime.Engine.ChatStream(ctx, engine.ChatRequest{ConversationID: conversation.ID, WorkspaceID: lesson21.WorkspaceID, UserID: "student", Message: test.message}, func(event engine.Event) error {
			events = append(events, event.Type)
			if event.Type == "answer_done" {
				answer = event.Text
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(answer, test.want) || !strings.Contains(answer, "演示物流数据") || !strings.Contains(strings.Join(events, ","), "tool_started,tool_finished") {
			t.Fatalf("message=%s answer=%q events=%v", test.message, answer, events)
		}
	}
	stored, err := runtime.Agents.ListMessages(ctx, lesson21.WorkspaceID, conversation.ID)
	if err != nil || len(stored) != 8 {
		t.Fatalf("history=%v err=%v", stored, err)
	}
}

func TestLogisticsRejectsUnsupportedRemoteVersion(t *testing.T) {
	server := logisticsServer(t)
	client := tooling.NewA2AClient(server.URL+"/.well-known/agent-card.json", "127.0.0.1")
	for _, version := range []string{"", "logistics-v2"} {
		_, err := client.Send(context.Background(), version, "CB-1001")
		if err == nil || !strings.Contains(err.Error(), "agent_version_id must be logistics-v1") {
			t.Fatalf("version=%q err=%v", version, err)
		}
	}
}
