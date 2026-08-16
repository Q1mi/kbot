package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/schema"
	"go.opentelemetry.io/otel/attribute"

	courseotel "github.com/Q1mi/kbot/internal/infrastructure/otel"
	"github.com/Q1mi/kbot/internal/platform/approval"
	"github.com/Q1mi/kbot/internal/platform/audit"
	"github.com/Q1mi/kbot/internal/runtime/tooling"
)

// ResumeApproved executes the exact approved ToolCall and continues generation from its checkpoint.
func (e *Engine) ResumeApproved(ctx context.Context, request *approval.Request, checkpoint []byte, emit Emitter) error {
	if request == nil || request.RunID == "" || request.ToolCallID == "" || request.ToolVersionID == "" {
		return fmt.Errorf("complete approval binding is required")
	}
	if e.tools == nil {
		return fmt.Errorf("tool runtime is required")
	}
	snapshot, err := e.ResolveSnapshot(ctx, request.RunID)
	if err != nil {
		return err
	}
	if snapshot.WorkspaceID != request.WorkspaceID {
		return fmt.Errorf("approval and conversation workspaces do not match")
	}
	var state reactCheckpoint
	if err := json.Unmarshal(checkpoint, &state); err != nil {
		return fmt.Errorf("decode approval checkpoint: %w", err)
	}
	if len(state.History) == 0 || len(state.ToolVersionIDs) == 0 {
		return fmt.Errorf("approval checkpoint has no active runtime policy")
	}
	approvedToolIsActive := false
	for _, versionID := range state.ToolVersionIDs {
		if versionID == request.ToolVersionID {
			approvedToolIsActive = true
			break
		}
	}
	if !approvedToolIsActive {
		return fmt.Errorf("approved tool is outside the checkpoint policy")
	}
	if err := emitContext(ctx, emit, Event{Type: "tool_started", Data: map[string]any{"call_id": request.ToolCallID, "resumed": true}}); err != nil {
		return err
	}
	toolContext, finishTool := courseotel.StartOperation(ctx, "tool.execute.approved",
		attribute.String("kbot.tool.version.id", request.ToolVersionID),
		attribute.String("kbot.tool.call.id", request.ToolCallID),
	)
	result, err := e.tools.Execute(toolContext, tooling.Call{
		WorkspaceID: request.WorkspaceID, ToolVersionID: request.ToolVersionID,
		Arguments: request.Arguments, IdempotencyKey: "approval:" + request.ID,
	})
	finishTool(err)
	if e.audit != nil && request.DecidedBy != "" {
		data := map[string]any{"conversation_id": request.RunID, "tool_call_id": request.ToolCallID, "status_code": result.StatusCode}
		if err != nil {
			data["failed"] = true
		}
		_, _ = e.audit.Append(context.WithoutCancel(ctx), audit.Event{
			WorkspaceID: request.WorkspaceID, ActorID: request.DecidedBy, Action: "tool.execute.approved",
			ResourceID: request.ToolVersionID, Data: data,
		})
	}
	if err != nil {
		return fmt.Errorf("execute approved tool: %w", err)
	}
	state.History = append(state.History, schema.ToolMessage(string(result.Body), request.ToolCallID))
	if err := emitContext(ctx, emit, Event{Type: "tool_finished", Data: map[string]any{"call_id": request.ToolCallID, "status": result.StatusCode, "resumed": true}}); err != nil {
		return err
	}
	bindings, err := e.tools.Bind(ctx, request.WorkspaceID, state.ToolVersionIDs)
	if err != nil {
		return fmt.Errorf("bind pinned tools: %w", err)
	}
	maxSteps := state.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 4
	}
	generator := e.gen
	if snapshot.ModelProfileVersionID != "" {
		if e.profiles == nil {
			return fmt.Errorf("model profile resolver is required by the pinned snapshot")
		}
		profile, err := e.profiles.Resolve(ctx, request.WorkspaceID, snapshot.ModelProfileVersionID)
		if err != nil {
			return fmt.Errorf("resolve pinned model profile: %w", err)
		}
		routed, ok := e.gen.(RoutedGenerator)
		if !ok {
			return fmt.Errorf("generator does not support pinned model profiles")
		}
		generator = pinnedProfileGenerator{next: routed, profile: profile}
	}
	if e.guard != nil {
		generator = guardedGenerator{next: generator, guard: e.guard, workspaceID: request.WorkspaceID}
	}
	generator = observedGenerator{next: generator}
	answer, err := NewReActRunner(generator, e.tools, request.WorkspaceID).
		WithApprovals(e.approvals, request.RunID).
		WithAudit(e.audit, request.DecidedBy).
		Run(ctx, state.History, bindings, maxSteps, emit)
	if err != nil {
		return fmt.Errorf("continue approved run: %w", err)
	}
	if e.guard != nil {
		decision, guardErr := e.guard.Evaluate(ctx, request.WorkspaceID, "on_output", answer.Content)
		if guardErr != nil {
			return fmt.Errorf("evaluate resumed output guard: %w", guardErr)
		}
		if !decision.Allowed {
			return fmt.Errorf("resumed output blocked by guard")
		}
		answer.Content = decision.SanitizedText
	}
	if history, ok := e.platform.(ConversationMessageStore); ok {
		if err := history.AppendMessage(ctx, request.WorkspaceID, request.RunID, "assistant", answer.Content); err != nil {
			return fmt.Errorf("persist resumed assistant message: %w", err)
		}
	}
	for _, delta := range answerDeltas(answer.Content, 8) {
		if err := emitContext(ctx, emit, Event{Type: "answer_delta", Text: delta}); err != nil {
			return err
		}
	}
	if err := emitContext(ctx, emit, Event{Type: "answer_done", Text: answer.Content}); err != nil {
		return err
	}
	return emitContext(ctx, emit, Event{Type: "run_finished", Data: map[string]string{"status": "completed"}})
}
