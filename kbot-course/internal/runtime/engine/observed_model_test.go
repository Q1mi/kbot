package engine

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	courseotel "github.com/Q1mi/kbot/internal/infrastructure/otel"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	globalotel "go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func traceRecorder(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	t.Setenv("KBOT_TRACE_CONTENT", "true")
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := globalotel.GetTracerProvider()
	globalotel.SetTracerProvider(provider)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()); globalotel.SetTracerProvider(previous) })
	return recorder
}

func spanAttrs(span sdktrace.ReadOnlySpan) map[string]attribute.Value {
	attrs := map[string]attribute.Value{}
	for _, item := range span.Attributes() {
		attrs[string(item.Key)] = item.Value
	}
	return attrs
}

func startTestRun(t *testing.T) (context.Context, func(error)) {
	t.Helper()
	return courseotel.StartRun(t.Context(), courseotel.RunContext{WorkspaceID: "ws", UserID: "u", ConversationID: "c", AgentVersionID: "v", Environment: "test"})
}

type traceTestModel struct {
	reader *schema.StreamReader[*schema.Message]
	answer *schema.Message
	err    error
}

func (m traceTestModel) ModelName() string { return "test-model" }
func (m traceTestModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return m.answer, m.err
}
func (m traceTestModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return m.reader, m.err
}

func traceUsage() *schema.ResponseMeta {
	return &schema.ResponseMeta{FinishReason: "stop", Usage: &schema.TokenUsage{
		PromptTokens: 100, CompletionTokens: 30, TotalTokens: 130,
		PromptTokenDetails:      schema.PromptTokenDetails{CachedTokens: 60},
		CompletionTokensDetails: schema.CompletionTokensDetails{ReasoningTokens: 10},
	}}
}

func TestObservedGenerateCapturesModelPayloadAndExclusiveUsage(t *testing.T) {
	recorder := traceRecorder(t)
	ctx, end := startTestRun(t)
	answer := schema.AssistantMessage("result", nil)
	answer.ResponseMeta = traceUsage()
	m := observedChatModel{next: guardedChatModel{next: traceTestModel{answer: answer}}}
	// Test the name forwarding through a guard separately; avoid invoking a nil guard.
	if m.next.(guardedChatModel).ModelName() != "test-model" {
		t.Fatal("guard lost model identity")
	}
	m.next = traceTestModel{answer: answer}
	result, err := m.Generate(ctx, []*schema.Message{schema.UserMessage("question")}, model.WithModel("override"), model.WithTemperature(0.5), model.WithMaxTokens(200))
	if err != nil || result != answer {
		t.Fatal("tracing changed model result")
	}
	end(nil)
	spans := recorder.Ended()
	if len(spans) != 2 {
		t.Fatalf("spans=%d", len(spans))
	}
	child, root := spans[0], spans[1]
	if child.Parent().SpanID() != root.SpanContext().SpanID() || child.SpanContext().TraceID() != root.SpanContext().TraceID() {
		t.Fatal("broken parent context")
	}
	for _, span := range spans {
		a := spanAttrs(span)
		for key, want := range map[string]string{"langfuse.user.id": "u", "langfuse.session.id": "c", "langfuse.version": "v", "langfuse.trace.metadata.workspace_id": "ws", "langfuse.environment": "test"} {
			if a[key].AsString() != want {
				t.Errorf("%s: %s missing %s", span.Name(), key, want)
			}
		}
	}
	a := spanAttrs(child)
	if a["langfuse.observation.type"].AsString() != "generation" || a["langfuse.observation.model.name"].AsString() != "override" {
		t.Fatal("missing generation identity")
	}
	if !strings.Contains(a["langfuse.observation.input"].AsString(), "question") || !strings.Contains(a["langfuse.observation.output"].AsString(), "result") {
		t.Fatal("missing model content")
	}
	var params map[string]float64
	if json.Unmarshal([]byte(a["langfuse.observation.model.parameters"].AsString()), &params) != nil || params["temperature"] != 0.5 || params["max_tokens"] != 200 {
		t.Fatal("missing model parameters")
	}
	assertUsage(t, a)
}

func assertUsage(t *testing.T, attrs map[string]attribute.Value) {
	t.Helper()
	var usage map[string]int
	if err := json.Unmarshal([]byte(attrs["langfuse.observation.usage_details"].AsString()), &usage); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]int{"input": 40, "input_cached_tokens": 60, "output": 20, "output_reasoning_tokens": 10} {
		if usage[key] != want {
			t.Errorf("usage[%s]=%d want %d", key, usage[key], want)
		}
	}
}

func TestObservedStreamEndsAtEOFAndRetainsUsageAfterContentLimit(t *testing.T) {
	recorder := traceRecorder(t)
	ctx, end := startTestRun(t)
	reader, writer := schema.Pipe[*schema.Message](0)
	stream, err := (observedChatModel{next: traceTestModel{reader: reader}}).Stream(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if len(recorder.Ended()) != 0 {
		t.Fatal("span ended before consumption")
	}
	before := time.Now()
	go func() {
		defer writer.Close()
		writer.Send(schema.AssistantMessage("hello", nil), nil)
		writer.Send(schema.AssistantMessage(strings.Repeat("x", courseotel.ContentLimit), nil), nil)
		usage := &schema.Message{ResponseMeta: traceUsage()}
		writer.Send(usage, nil)
		writer.Send(usage, nil) // Repeated cumulative usage must not double the count.
	}()
	for {
		_, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	end(nil)
	spans := recorder.Ended()
	if len(spans) != 2 || spans[0].EndTime().Before(before) {
		t.Fatal("incorrect stream lifecycle")
	}
	a := spanAttrs(spans[0])
	assertUsage(t, a)
	if a["langfuse.observation.completion_start_time"].AsString() == "" || !a["kbot.trace.output.truncated"].AsBool() {
		t.Fatal("missing streaming metadata")
	}
	if !strings.Contains(a["langfuse.observation.output"].AsString(), "hello") {
		t.Fatal("missing partial output")
	}
}

func TestObservedStreamFailureAndEarlyClose(t *testing.T) {
	for _, scenario := range []string{"start_error", "read_error", "early_close", "cancel", "empty"} {
		t.Run(scenario, func(t *testing.T) {
			recorder := traceRecorder(t)
			base, cancel := context.WithCancel(t.Context())
			defer cancel()
			ctx, end := courseotel.StartRun(base, courseotel.RunContext{})
			reader, writer := schema.Pipe[*schema.Message](2)
			failure := errors.New("provider unavailable")
			m := traceTestModel{reader: reader}
			if scenario == "start_error" {
				m.err = failure
			}
			writer.Send(schema.AssistantMessage("partial", nil), nil)
			if scenario == "read_error" {
				writer.Send(nil, failure)
			}
			writer.Close()
			if scenario == "empty" {
				reader.Close()
				m.reader = schema.StreamReaderFromArray([]*schema.Message{})
			}
			stream, err := (observedChatModel{next: m}).Stream(ctx, nil)
			if scenario == "start_error" {
				reader.Close()
				if !errors.Is(err, failure) {
					t.Fatal("lost start error")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if scenario == "cancel" {
					cancel()
				}
				if scenario == "read_error" || scenario == "empty" {
					for {
						_, err = stream.Recv()
						if err != nil {
							break
						}
					}
					if scenario == "read_error" && !errors.Is(err, failure) {
						t.Fatal("lost read error")
					}
				}
				stream.Close()
			}
			end(base.Err()) // Flush unfinished stream observations before the root.
			spans := recorder.Ended()
			if len(spans) != 2 {
				t.Fatalf("spans=%d", len(spans))
			}
			want := codes.Error
			if scenario == "empty" {
				want = codes.Ok
			}
			if spans[0].Status().Code != want {
				t.Fatalf("status=%v", spans[0].Status())
			}
		})
	}
}

func TestToolTraceCapturesArgumentsResultAndContext(t *testing.T) {
	recorder := traceRecorder(t)
	ctx, end := startTestRun(t)
	tool := &bindingTool{binding: refundBinding(), executor: &fakeToolExecutor{}, workspaceID: "ws"}
	result, err := tool.InvokableRun(ctx, `{"order_id":"ORD-1","token":"private-token"}`)
	if err != nil || !strings.Contains(result, "submitted") {
		t.Fatal("tool execution changed")
	}
	end(nil)
	spans := recorder.Ended()
	if len(spans) != 2 {
		t.Fatalf("spans=%d", len(spans))
	}
	a := spanAttrs(spans[0])
	if a["langfuse.observation.type"].AsString() != "tool" || a["langfuse.session.id"].AsString() != "c" {
		t.Fatal("missing tool context")
	}
	if strings.Contains(a["langfuse.observation.input"].AsString(), "private-token") || !strings.Contains(a["langfuse.observation.output"].AsString(), "submitted") {
		t.Fatal("incorrect tool content")
	}
}
