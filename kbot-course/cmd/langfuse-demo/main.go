// langfuse-demo exercises the same Engine/model/tool instrumentation as the server.
// The default model is deterministic and requires no model account or database.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/Q1mi/kbot/internal/config"
	"github.com/Q1mi/kbot/internal/domain"
	courseotel "github.com/Q1mi/kbot/internal/infrastructure/otel"
	"github.com/Q1mi/kbot/internal/platform/audit"
	"github.com/Q1mi/kbot/internal/runtime/engine"
	"github.com/Q1mi/kbot/internal/runtime/llm"
	"github.com/Q1mi/kbot/internal/runtime/tooling"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func main() {
	realModel := flag.Bool("real", false, "use the configured KBOT_LLM_MODEL and API key")
	flag.Parse()
	if err := run(*realModel); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(realModel bool) (runErr error) {
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" && os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") == "" {
		return errors.New("configure an OTLP endpoint first; see docs/langfuse.md")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	shutdown, err := courseotel.Setup(ctx, "kbot-course-19-demo")
	if err != nil {
		return err
	}
	defer func() {
		flushCtx, flushCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer flushCancel()
		runErr = errors.Join(runErr, shutdown(flushCtx))
	}()
	var chatModel model.BaseChatModel = demoModel{}
	if realModel {
		cfg := config.Load()
		if cfg.LLMAPIKey == "" {
			return errors.New("ARK_API_KEY or KBOT_LLM_API_KEY is required with -real")
		}
		chatModel, err = llm.NewGateway(cfg)
		if err != nil {
			return err
		}
	} else {
		fmt.Println("演示模型：course-demo-model；Token 为模拟数据，费用需配置自定义模型价格。")
	}
	ledger := audit.NewLedger()
	var runtime *engine.Engine
	session := fmt.Sprintf("course-19-%d", time.Now().UnixNano())
	requests := []struct{ version, message string }{
		{"stream-v1", "你好，请介绍退货流程。"},
		{"tools-v1", "请调用 lookup_order 查询 ORD-DEMO 的状态，再给出简短回复。"},
	}
	for _, request := range requests {
		// Each conversation keeps a pinned version; stream success/error share a session.
		runtime = engine.New(demoPlatform{version: request.version}, chatModel).WithTools(demoTools{}).WithAudit(ledger)
		if err := runtime.ChatStream(ctx, engine.ChatRequest{WorkspaceID: "demo-workspace", UserID: "demo-user", ConversationID: session + "-" + request.version, Message: request.message}, printEvent); err != nil {
			return err
		}
	}
	if !realModel {
		// A deterministic provider failure gives students an error trace to inspect.
		runtime = engine.New(demoPlatform{version: "stream-v1"}, demoModel{fail: true}).WithAudit(ledger)
		err := runtime.ChatStream(ctx, engine.ChatRequest{WorkspaceID: "demo-workspace", UserID: "demo-user", ConversationID: session + "-stream-v1", Message: "模拟流式读取失败"}, printEvent)
		if err == nil {
			return errors.New("expected the demonstration stream to fail")
		}
		fmt.Println("已记录预期的模拟错误：", err)
	}
	if err := ledger.Verify(ctx, "demo-workspace"); err != nil {
		return err
	}
	fmt.Println("审计链校验通过。Langfuse Session ID 前缀：", session)
	return nil
}

func printEvent(event engine.Event) error {
	if event.Type == "run_started" {
		fmt.Println("运行：", event.Data)
	}
	if event.Type == "answer_done" {
		fmt.Println("回答：", event.Text)
	}
	return nil
}

type demoPlatform struct{ version string }

func (p demoPlatform) LoadConversation(_ context.Context, id string) (*domain.Conversation, error) {
	return &domain.Conversation{ID: id, WorkspaceID: "demo-workspace", AgentVersionID: p.version}, nil
}
func (p demoPlatform) GetAgentSnapshotByVersion(_ context.Context, version string) (*engine.AgentSnapshot, error) {
	snapshot := &engine.AgentSnapshot{ID: version, WorkspaceID: "demo-workspace", SystemPrompt: "你是课堂客服助手。需要订单信息时使用 lookup_order 工具。", MaxSteps: 4}
	if version == "tools-v1" {
		snapshot.ToolVersionIDs = []string{"lookup-v1"}
	}
	return snapshot, nil
}

type demoTools struct{}

func (demoTools) Bind(context.Context, string, []string) ([]tooling.Binding, error) {
	return []tooling.Binding{{Name: "lookup_order", VersionID: "lookup-v1", Info: &schema.ToolInfo{
		Name: "lookup_order", Desc: "查询演示订单状态", ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"order_id": {Type: schema.String, Required: true},
		}),
	}}}, nil
}
func (demoTools) Execute(context.Context, tooling.Call) (tooling.Result, error) {
	return tooling.Result{StatusCode: 200, Body: []byte(`{"order_id":"ORD-DEMO","status":"delivered","return_window_days":7}`)}, nil
}

type demoModel struct{ fail bool }

func (demoModel) ModelName() string { return "course-demo-model" }
func (demoModel) Generate(_ context.Context, messages []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	answer := schema.AssistantMessage("订单已送达，演示退货期限为 7 天。", nil)
	if len(messages) > 0 && messages[len(messages)-1].Role != schema.Tool {
		answer = schema.AssistantMessage("", []schema.ToolCall{{ID: "demo-call", Type: "function", Function: schema.FunctionCall{Name: "lookup_order", Arguments: `{"order_id":"ORD-DEMO"}`}}})
	}
	answer.ResponseMeta = &schema.ResponseMeta{FinishReason: "stop", Usage: &schema.TokenUsage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120}}
	return answer, nil
}
func (m demoModel) Stream(ctx context.Context, _ []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	reader, writer := schema.Pipe[*schema.Message](0)
	go func() {
		defer writer.Close()
		for _, text := range []string{"你好，", "可以先查询订单，", "再核对退货条件。"} {
			select {
			case <-ctx.Done():
				writer.Send(nil, ctx.Err())
				return
			case <-time.After(40 * time.Millisecond):
			}
			if writer.Send(schema.AssistantMessage(text, nil), nil) {
				return
			}
			if m.fail {
				writer.Send(nil, errors.New("simulated provider read failure"))
				return
			}
		}
		writer.Send(&schema.Message{ResponseMeta: &schema.ResponseMeta{FinishReason: "stop", Usage: &schema.TokenUsage{PromptTokens: 50, CompletionTokens: 12, TotalTokens: 62}}}, nil)
	}()
	return reader, nil
}
