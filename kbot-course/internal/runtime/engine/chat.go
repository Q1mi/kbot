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
	WorkspaceID    string `json:"-"`
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
	if req.WorkspaceID != "" && snapshot.WorkspaceID != "" && req.WorkspaceID != snapshot.WorkspaceID {
		return fmt.Errorf("conversation is outside the active workspace")
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
	var answer *schema.Message
	streamed := false
	if len(snapshot.ToolVersionIDs) > 0 {
		if e.tools == nil {
			return fmt.Errorf("tool runtime is required by the pinned agent snapshot")
		}
		bindings, bindErr := e.tools.Bind(ctx, snapshot.WorkspaceID, snapshot.ToolVersionIDs)
		if bindErr != nil {
			return fmt.Errorf("bind pinned tools: %w", bindErr)
		}
		answer, err = NewADKRunner(e.model, e.tools, snapshot.WorkspaceID).Run(ctx, messages, bindings, snapshot.MaxSteps, emit)
	} else {
		if e.model == nil {
			return fmt.Errorf("chat model is required")
		}
		stream, streamErr := e.model.Stream(ctx, messages)
		if streamErr != nil {
			err = streamErr
		} else {
			defer stream.Close()
			var chunks []*schema.Message
			for {
				chunk, recvErr := stream.Recv()
				if recvErr == io.EOF {
					break
				}
				if recvErr != nil {
					err = recvErr
					break
				}
				chunks = append(chunks, chunk)
				if chunk != nil && chunk.Content != "" {
					if emitErr := emitContext(ctx, emit, Event{Type: "answer_delta", Text: chunk.Content}); emitErr != nil {
						err = emitErr
						break
					}
					streamed = true
				}
			}
			if err == nil {
				answer, err = schema.ConcatMessages(chunks)
			}
		}
	}
	if err != nil {
		_ = emitContext(ctx, emit, Event{Type: "error", Data: map[string]string{"message": err.Error()}})
		return fmt.Errorf("generate: %w", err)
	}
	if !streamed {
		for _, delta := range answerDeltas(answer.Content, 8) {
			if err := emitContext(ctx, emit, Event{Type: "answer_delta", Text: delta}); err != nil {
				return err
			}
		}
	}
	if err := emitContext(ctx, emit, Event{Type: "answer_done", Text: answer.Content}); err != nil {
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
