package engine

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/cloudwego/eino/schema"

	"github.com/Q1mi/kbot/internal/runtime/llm"
)

type ChatRequest struct {
	ConversationID string `json:"conversation_id"`
	Message        string `json:"message"`
	WorkspaceID    string `json:"-"`
}

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
	plan, err := e.executionPlan(ctx, snapshot)
	if err != nil {
		return err
	}
	systemPrompt := snapshot.SystemPrompt
	if snapshot.PromptVersionID != "" {
		if e.prompts == nil {
			return fmt.Errorf("prompt resolver is required by the pinned snapshot")
		}
		systemPrompt, err = e.prompts.Render(ctx, snapshot.WorkspaceID, snapshot.PromptVersionID, map[string]string{})
		if err != nil {
			return fmt.Errorf("render pinned system prompt: %w", err)
		}
	}
	if err := emitContext(ctx, emit, Event{Type: "run_started", Data: map[string]string{
		"conversation_id": req.ConversationID, "agent_version_id": snapshot.ID,
	}}); err != nil {
		return err
	}
	messages := []*schema.Message{schema.SystemMessage(systemPrompt), schema.UserMessage(req.Message)}
	answer, streamed, err := e.runPlan(ctx, snapshot, plan, messages, emit)
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

func (e *Engine) executionPlan(ctx context.Context, snapshot *AgentSnapshot) (*llm.ExecutionPlan, error) {
	if snapshot.ModelProfileVersionID == "" {
		if e.model == nil {
			return nil, fmt.Errorf("chat model is required")
		}
		return &llm.ExecutionPlan{Model: e.model}, nil
	}
	if e.profiles == nil || e.planner == nil {
		return nil, fmt.Errorf("model profile resolver and execution planner are required by the pinned snapshot")
	}
	profile, err := e.profiles.Resolve(ctx, snapshot.WorkspaceID, snapshot.ModelProfileVersionID)
	if err != nil {
		return nil, fmt.Errorf("resolve pinned model profile: %w", err)
	}
	plan, err := e.planner.PrepareExecution(ctx, profile)
	if err != nil {
		return nil, fmt.Errorf("prepare model execution: %w", err)
	}
	return plan, nil
}

func (e *Engine) runPlan(
	ctx context.Context, snapshot *AgentSnapshot, plan *llm.ExecutionPlan,
	messages []*schema.Message, emit Emitter,
) (*schema.Message, bool, error) {
	if plan == nil || plan.Model == nil {
		return nil, false, fmt.Errorf("execution plan model is required")
	}
	var bindings []ToolBinding
	if len(snapshot.ToolVersionIDs) > 0 {
		if e.tools == nil {
			return nil, false, fmt.Errorf("tool runtime is required by the pinned agent snapshot")
		}
		var err error
		bindings, err = e.tools.Bind(ctx, snapshot.WorkspaceID, snapshot.ToolVersionIDs)
		if err != nil {
			return nil, false, fmt.Errorf("bind pinned tools: %w", err)
		}
	}
	if len(bindings) > 0 || plan.Retry != nil || plan.Failover != nil {
		answer, err := NewADKRunner(plan.Model, e.tools, snapshot.WorkspaceID).
			WithModelPolicy(plan.Retry, plan.Failover).
			Run(ctx, messages, bindings, snapshot.MaxSteps, emit)
		return answer, false, err
	}
	stream, err := plan.Model.Stream(ctx, messages)
	if err != nil {
		return nil, false, err
	}
	defer stream.Close()
	var chunks []*schema.Message
	streamed := false
	for {
		chunk, recvErr := stream.Recv()
		if recvErr == io.EOF {
			break
		}
		if recvErr != nil {
			return nil, streamed, recvErr
		}
		chunks = append(chunks, chunk)
		if chunk != nil && chunk.Content != "" {
			if err := emitContext(ctx, emit, Event{Type: "answer_delta", Text: chunk.Content}); err != nil {
				return nil, streamed, err
			}
			streamed = true
		}
	}
	answer, err := schema.ConcatMessages(chunks)
	return answer, streamed, err
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
