package engine

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/cloudwego/eino/schema"
)

// ChatRequest 是一次运行请求；ConversationID 用于多轮会话续接。
type ChatRequest struct {
	ConversationID string `json:"conversation_id"`
	Message        string `json:"message"`
}

// Event 是 Runtime 对传输层公开的稳定事件信封。
type Event struct {
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
	Text string `json:"text,omitempty"`
}

type Emitter func(Event) error

func (e *Engine) ChatStream(ctx context.Context, req ChatRequest, emit Emitter) error {
	if strings.TrimSpace(req.ConversationID) == "" || strings.TrimSpace(req.Message) == "" {
		return fmt.Errorf("conversation_id and message are required")
	}
	if emit == nil {
		return fmt.Errorf("emitter is required")
	}
	snapshot, err := e.ResolveSnapshot(ctx, req.ConversationID)
	if err != nil {
		return err
	}
	if err := emitContext(ctx, emit, Event{Type: "run_started", Data: map[string]string{
		"conversation_id":  req.ConversationID,
		"agent_version_id": snapshot.ID,
	}}); err != nil {
		return err
	}
	messages := []*schema.Message{
		schema.SystemMessage(snapshot.SystemPrompt),
		schema.UserMessage(req.Message),
	}
	if e.model == nil {
		return fmt.Errorf("chat model is required")
	}
	stream, err := e.model.Stream(ctx, messages)
	if err != nil {
		_ = emitContext(ctx, emit, Event{Type: "error", Data: map[string]string{"message": err.Error()}})
		return fmt.Errorf("stream: %w", err)
	}
	defer stream.Close()
	var answer strings.Builder
	for {
		chunk, recvErr := stream.Recv()
		if recvErr == io.EOF {
			break
		}
		if recvErr != nil {
			return fmt.Errorf("receive stream: %w", recvErr)
		}
		if chunk == nil || chunk.Content == "" {
			continue
		}
		answer.WriteString(chunk.Content)
		if err := emitContext(ctx, emit, Event{Type: "answer_delta", Text: chunk.Content}); err != nil {
			return err
		}
	}
	if err := emitContext(ctx, emit, Event{Type: "answer_done", Text: answer.String()}); err != nil {
		return err
	}
	return emitContext(ctx, emit, Event{Type: "run_finished", Data: map[string]string{"status": "completed"}})
}

func answerDeltas(answer string, maxRunes int) []string {
	if maxRunes <= 0 {
		maxRunes = 8
	}
	runes := []rune(answer)
	if len(runes) == 0 {
		return nil
	}
	deltas := make([]string, 0, (len(runes)+maxRunes-1)/maxRunes)
	for start := 0; start < len(runes); start += maxRunes {
		end := min(start+maxRunes, len(runes))
		deltas = append(deltas, string(runes[start:end]))
	}
	return deltas
}

func emitContext(ctx context.Context, emit Emitter, event Event) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return emit(event)
	}
}
