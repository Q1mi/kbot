package engine

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"

	courseotel "github.com/Q1mi/kbot/internal/infrastructure/otel"
	"github.com/Q1mi/kbot/internal/runtime/llm"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type observedChatModel struct{ next model.BaseChatModel }

func (m observedChatModel) start(ctx context.Context, name string, messages []*schema.Message, opts ...model.Option) (context.Context, func(error)) {
	options := model.GetCommonOptions(nil, opts...)
	modelName := llm.ModelName(m.next)
	if options.Model != nil {
		modelName = *options.Model
	}
	parameters := map[string]any{}
	if options.Temperature != nil {
		parameters["temperature"] = *options.Temperature
	}
	if options.TopP != nil {
		parameters["top_p"] = *options.TopP
	}
	if options.MaxTokens != nil {
		parameters["max_tokens"] = *options.MaxTokens
	}
	if options.ToolChoice != nil {
		parameters["tool_choice"] = *options.ToolChoice
	}
	raw, _ := json.Marshal(parameters)
	attrs := []attribute.KeyValue{
		attribute.String("langfuse.observation.type", "generation"),
		attribute.String("gen_ai.operation.name", "chat"),
		attribute.String("langfuse.observation.model.parameters", string(raw)),
	}
	if modelName != "" {
		attrs = append(attrs, attribute.String("langfuse.observation.model.name", modelName), attribute.String("gen_ai.request.model", modelName))
	}
	ctx, finish := courseotel.StartOperation(ctx, name, attrs...)
	if courseotel.CaptureContent() && trace.SpanFromContext(ctx).IsRecording() {
		courseotel.RecordInput(ctx, traceMessages(messages))
	}
	return ctx, finish
}

func (m observedChatModel) Generate(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	ctx, finish := m.start(ctx, "llm.generate", messages, opts...)
	answer, err := m.next.Generate(ctx, messages, opts...)
	if answer != nil {
		if courseotel.CaptureContent() && trace.SpanFromContext(ctx).IsRecording() {
			courseotel.RecordOutput(ctx, traceMessages([]*schema.Message{answer}))
		}
		recordResponseMeta(ctx, answer.ResponseMeta)
	}
	finish(err)
	return answer, err
}

func (m observedChatModel) Stream(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	ctx, finish := m.start(ctx, "llm.stream", messages, opts...)
	stream, err := m.next.Stream(ctx, messages, opts...)
	if err != nil {
		finish(err)
		return nil, err
	}
	if stream == nil {
		err = errors.New("model returned a nil stream")
		finish(err)
		return nil, err
	}
	state := &streamObservation{ctx: ctx, finish: finish, capture: courseotel.CaptureContent() && trace.SpanFromContext(ctx).IsRecording()}
	// Eino's converter has EOF/error hooks but no Close hook. The owning Agent Run
	// synchronously finishes abandoned observations before ending its root span.
	courseotel.OnRunFinish(ctx, func(runErr error) {
		if runErr == nil {
			runErr = errors.New("model stream was not fully consumed")
		}
		state.end(runErr)
	})
	stop := context.AfterFunc(ctx, func() { state.end(ctx.Err()) })
	state.mu.Lock()
	state.stop = stop
	if state.done {
		stop()
	}
	state.mu.Unlock()
	return schema.StreamReaderWithConvert(stream, func(chunk *schema.Message) (*schema.Message, error) {
		state.add(chunk)
		return chunk, nil
	}, schema.WithErrWrapper(func(err error) error {
		state.end(err)
		return err
	}), schema.WithOnEOF(func() (any, error) {
		state.end(ctx.Err())
		return nil, io.EOF
	})), nil
}

type streamObservation struct {
	mu                              sync.Mutex
	ctx                             context.Context
	finish                          func(error)
	stop                            func() bool
	done, first, capture, truncated bool
	bytes                           int
	chunks                          []*schema.Message
}

func (s *streamObservation) add(chunk *schema.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done || chunk == nil {
		return
	}
	// Ignore role-only and usage-only chunks when measuring first generated content.
	if !s.first && (chunk.Content != "" || chunk.ReasoningContent != "" || len(chunk.ToolCalls) > 0 || len(chunk.AssistantGenMultiContent) > 0) {
		s.first = true
		trace.SpanFromContext(s.ctx).SetAttributes(attribute.String("langfuse.observation.completion_start_time", time.Now().UTC().Format(time.RFC3339Nano)))
	}
	// Providers send cumulative usage snapshots; overwrite rather than add them.
	recordResponseMeta(s.ctx, chunk.ResponseMeta)
	if !s.capture || s.truncated {
		return
	}
	message := traceMessages([]*schema.Message{chunk})[0]
	raw, err := json.Marshal(message)
	if err != nil {
		return
	}
	if s.bytes+len(raw) > courseotel.ContentLimit {
		s.truncated = true
		return
	}
	s.bytes += len(raw)
	s.chunks = append(s.chunks, message)
}

func (s *streamObservation) end(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return
	}
	s.done = true
	if s.stop != nil {
		s.stop()
	}
	if s.capture {
		if len(s.chunks) > 0 {
			answer, concatErr := schema.ConcatMessages(s.chunks)
			if concatErr == nil {
				courseotel.RecordOutput(s.ctx, traceMessages([]*schema.Message{answer}))
			} else {
				// Observation formatting must not alter the stream received by the caller.
				courseotel.RecordOutput(s.ctx, s.chunks)
			}
		}
		if s.truncated {
			if len(s.chunks) == 0 {
				courseotel.RecordOutput(s.ctx, map[string]bool{"truncated": true})
			}
			trace.SpanFromContext(s.ctx).SetAttributes(attribute.Bool("kbot.trace.output.truncated", true))
		}
	}
	s.chunks = nil
	s.finish(err)
}

// Keep text and tool calls; omit provider extras, binary media and reasoning text.
func traceMessages(messages []*schema.Message) []*schema.Message {
	result := make([]*schema.Message, 0, len(messages))
	for _, message := range messages {
		if message == nil {
			continue
		}
		calls := append([]schema.ToolCall(nil), message.ToolCalls...)
		result = append(result, &schema.Message{
			Role: message.Role, Content: message.Content, Name: message.Name,
			ToolCalls: calls, ToolCallID: message.ToolCallID, ToolName: message.ToolName,
		})
	}
	return result
}

func recordResponseMeta(ctx context.Context, meta *schema.ResponseMeta) {
	if meta == nil {
		return
	}
	span := trace.SpanFromContext(ctx)
	if meta.FinishReason != "" {
		span.SetAttributes(attribute.String("gen_ai.response.finish_reason", meta.FinishReason))
	}
	if meta.Usage == nil {
		return
	}
	u := meta.Usage
	input, output := max(0, u.PromptTokens), max(0, u.CompletionTokens)
	cached := min(input, max(0, u.PromptTokenDetails.CachedTokens))
	reasoning := min(output, max(0, u.CompletionTokensDetails.ReasoningTokens))
	// Langfuse usage buckets are exclusive; provider totals include these details.
	usage := map[string]int{"input": input - cached, "output": output - reasoning}
	if cached > 0 {
		usage["input_cached_tokens"] = cached
	}
	if reasoning > 0 {
		usage["output_reasoning_tokens"] = reasoning
	}
	raw, _ := json.Marshal(usage)
	span.SetAttributes(
		attribute.String("langfuse.observation.usage_details", string(raw)),
		attribute.Int("gen_ai.usage.input_tokens", input),
		attribute.Int("gen_ai.usage.output_tokens", output),
	)
}
