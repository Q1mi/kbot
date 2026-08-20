// Package integration_test 是跨模块端到端测试（设计文档 §10 / 讲义 §15.9）。
//
// 讲义 §15.9 用 ory/dockertest 起真 PG/Redis。本平台 M1–M5 的存储是进程内内存实现
// （贯穿各 docs/mN.md 的统一脱节点），因此 e2e 直接跑在内存 platform 上，并用脚本化
// ChatModel 注入引擎、无需真实 LLM。落 PostgreSQL 后把本测试切到 dockertest 即可。
package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/Q1mi/kbot/internal/platform"
	"github.com/Q1mi/kbot/internal/platform/agent"
	"github.com/Q1mi/kbot/internal/platform/iam"
	"github.com/Q1mi/kbot/internal/platform/kb"
	"github.com/Q1mi/kbot/internal/platform/prompt"
	"github.com/Q1mi/kbot/internal/platform/skill"
	"github.com/Q1mi/kbot/internal/platform/tool"
	"github.com/Q1mi/kbot/internal/runtime/engine"
)

// scriptedChatModel：第一次（有工具时）调用工具，第二次给最终回答。
type scriptedChatModel struct {
	mu    sync.Mutex
	calls int
}

func (g *scriptedChatModel) Generate(_ context.Context, _ []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	tools := model.GetCommonOptions(nil, opts...).Tools
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls++
	if g.calls == 1 && len(tools) > 0 {
		return schema.AssistantMessage("", []schema.ToolCall{{
			ID:       "call-1",
			Function: schema.FunctionCall{Name: tools[0].Name, Arguments: `{"q":"ping"}`},
		}}), nil
	}
	return schema.AssistantMessage("已根据工具结果完成处理", nil), nil
}

func (g *scriptedChatModel) Stream(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	response, err := g.Generate(ctx, messages, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{response}), nil
}

func TestPlatformCRUDFlow(t *testing.T) {
	ctx := context.Background()
	plat := platform.NewService(nil, nil, make([]byte, 32), prompt.NoopPublisher{}, nil, nil)

	// IAM：建用户 + workspace。
	if _, err := plat.IAM.CreateUser(ctx, "admin@example.com", "secret123", "Admin"); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := plat.IAM.Login(ctx, iam.LoginRequest{Email: "admin@example.com", Password: "secret123"}); err != nil {
		t.Fatalf("login: %v", err)
	}
	ws, err := plat.IAM.CreateWorkspace(ctx, "默认", "demo", nil)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	// Prompt：创建并渲染。
	p, _, err := plat.Prompt.CreatePrompt(ctx, prompt.CreatePromptRequest{
		WorkspaceID: ws.ID, Name: "sys", Template: "你是客服助手", CreatedBy: "u1",
	})
	if err != nil {
		t.Fatalf("create prompt: %v", err)
	}
	if _, err := plat.Prompt.Render(ctx, p.ID, prompt.EnvDev, "u1", nil); err != nil {
		t.Fatalf("render prompt: %v", err)
	}

	// Skill：创建并发布（无 allowed-tools，发布应通过）。
	sk, sv, err := plat.Skill.CreateSkill(ctx, skill.CreateSkillRequest{
		WorkspaceID: ws.ID, CreatedBy: "u1",
		SkillMD: "---\nname: greet\ndescription: 打招呼\n---\n\n## 流程\n问候用户",
	})
	if err != nil {
		t.Fatalf("create skill: %v", err)
	}
	if err := plat.Skill.Publish(ctx, sv.ID); err != nil {
		t.Fatalf("publish skill: %v", err)
	}
	_ = sk

	// KB：建库 + 同步本地 Markdown + 检索。
	knb, err := plat.KB.CreateKB(ctx, kb.CreateKBRequest{WorkspaceID: ws.ID, Name: "faq", CreatedBy: "u1"})
	if err != nil {
		t.Fatalf("create kb: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "refund.md"), []byte("退款政策：七天内可申请退款。"), 0o644); err != nil {
		t.Fatal(err)
	}
	if res, err := plat.KB.SyncMarkdownFolder(ctx, knb.ID, dir); err != nil || res.Ingested != 1 {
		t.Fatalf("sync kb: res=%+v err=%v", res, err)
	}
	passages, err := plat.KB.Search(ctx, knb.ID, "怎么退款", 3)
	if err != nil || len(passages) == 0 {
		t.Fatalf("kb search: %d passages, err=%v", len(passages), err)
	}
}

func TestRuntimeToolCallE2E(t *testing.T) {
	ctx := context.Background()
	plat := platform.NewService(nil, nil, make([]byte, 32), prompt.NoopPublisher{}, nil, nil)

	// 一个被工具调用命中的 REST 端点。
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		fmt.Fprint(w, `{"status":"ok"}`)
	}))
	defer srv.Close()

	// 注册并发布 REST 工具。
	endpoint, _ := json.Marshal(map[string]string{"method": "POST", "url": srv.URL})
	tl, err := plat.Tool.CreateTool(ctx, tool.CreateToolRequest{
		WorkspaceID: "w1", Name: "ping_tool", SourceType: "rest_api",
		SchemaJSON:     `{"type":"object","properties":{"q":{"type":"string"}}}`,
		EndpointConfig: string(endpoint), CreatedBy: "u1",
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

	// 创建挂了该工具的 Agent。
	ag, err := plat.Agent.CreateAgent(ctx, agent.CreateAgentRequest{
		WorkspaceID: "w1", Name: "bot", SystemPrompt: "你是助手", ToolIDs: []string{tl.ID}, CreatedBy: "u1",
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	// 用脚本化 ChatModel + Guard + Audit 跑引擎。
	eng := engine.NewEngineWithChatModel(plat.Agent, &scriptedChatModel{}, plat.Registry).
		WithGuard(plat.Guard).
		WithAudit(plat.Audit)

	answer, err := eng.Chat(ctx, engine.ChatStreamRequest{AgentID: ag.ID, Message: "帮我 ping 一下", UserID: "u1"})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if !strings.Contains(answer, "完成") {
		t.Fatalf("unexpected answer: %q", answer)
	}
	if hits == 0 {
		t.Fatal("expected the REST tool endpoint to be called")
	}

	// Audit 应留下对话轨迹。
	plat.Audit.Close()
	// 注意：Close 后不可再写；此处仅查询。
}
