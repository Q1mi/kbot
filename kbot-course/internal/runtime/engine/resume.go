package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Q1mi/kbot/internal/platform/approval"
	"github.com/Q1mi/kbot/internal/platform/audit"
	platformskill "github.com/Q1mi/kbot/internal/platform/skill"
	"github.com/Q1mi/kbot/internal/runtime/skillrunner"
)

func decodeFrameworkCheckpoint(payload []byte) ([]byte, string, string, error) {
	var stored persistedFrameworkCheckpoint
	if err := json.Unmarshal(payload, &stored); err != nil || stored.Version != frameworkCheckpointVersion ||
		len(stored.Data) == 0 || stored.InterruptID == "" {
		return nil, "", "", fmt.Errorf("approval checkpoint is incompatible with Eino ADK runtime v%d", frameworkCheckpointVersion)
	}
	return stored.Data, stored.InterruptID, stored.ActiveSkillName, nil
}

// ResumeApproved 通过 Eino ResumeWithParams 精确恢复被批准的中断地址。
func (e *Engine) ResumeApproved(ctx context.Context, request *approval.Request, checkpoint []byte, emit Emitter) error {
	if request == nil || request.RunID == "" || request.ToolCallID == "" || request.ToolVersionID == "" {
		return fmt.Errorf("complete approval binding is required")
	}
	if e.tools == nil || e.approvals == nil {
		return fmt.Errorf("tool runtime and approval gate are required")
	}
	snapshot, err := e.ResolveSnapshot(ctx, request.RunID)
	if err != nil {
		return err
	}
	if snapshot.WorkspaceID != request.WorkspaceID {
		return fmt.Errorf("approval and conversation workspaces do not match")
	}
	bindings, err := e.tools.Bind(ctx, request.WorkspaceID, snapshot.ToolVersionIDs)
	if err != nil {
		return fmt.Errorf("bind pinned tools: %w", err)
	}
	if !containsToolVersion(bindings, request.ToolVersionID) {
		return fmt.Errorf("approved tool is outside the pinned agent snapshot")
	}
	plan, err := e.executionPlan(ctx, snapshot)
	if err != nil {
		return err
	}
	plan = e.guardExecutionPlan(plan, request.WorkspaceID)
	plan = observeExecutionPlan(plan)
	packages, err := e.resolveSkills(ctx, snapshot)
	if err != nil {
		return err
	}
	frameworkData, interruptID, activeSkillName, err := decodeFrameworkCheckpoint(checkpoint)
	if err != nil {
		return err
	}
	userInput := ""
	if activeSkillName != "" {
		userInput = "/skill " + activeSkillName
	}
	skillRuntime, err := skillrunner.NewRuntime(ctx, packages, bindings, userInput, func(pkg platformskill.Package) error {
		if e.audit != nil && request.DecidedBy != "" {
			_, _ = e.audit.Append(context.WithoutCancel(ctx), audit.Event{
				WorkspaceID: request.WorkspaceID, ActorID: request.DecidedBy, Action: "skill.triggered",
				ResourceID: pkg.Name, Data: map[string]any{"conversation_id": request.RunID, "resumed": true},
			})
		}
		return emitContext(ctx, emit, Event{Type: "skill_trigger", Data: map[string]string{"name": pkg.Name, "resumed": "true"}})
	})
	if err != nil {
		return err
	}
	var authorize func(string, string) error
	runner := NewADKRunner(plan.Model, e.tools, request.WorkspaceID).
		WithModelPolicy(plan.Retry, plan.Failover).
		WithApprovals(e.approvals, request.RunID).
		WithAudit(e.audit, request.DecidedBy)
	if skillRuntime != nil {
		if err := skillRuntime.Restore(activeSkillName); err != nil {
			return err
		}
		authorize = skillRuntime.Authorize
		runner.WithHandlers(skillRuntime.Handlers...).
			WithSkillState(skillRuntime.ActiveName, skillRuntime.Restore)
	}
	runner.WithToolAuthorization(authorize)
	maxSteps := snapshot.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 4
	}
	answer, err := runner.Resume(ctx, request.RunID, frameworkData, interruptID, bindings, maxSteps, emit)
	if err != nil {
		var awaiting *AwaitingApprovalError
		if errors.As(err, &awaiting) {
			if emitErr := emitContext(ctx, emit, Event{Type: "approval_requested", Data: map[string]string{
				"approval_id": awaiting.ApprovalID, "tool_name": awaiting.ToolName,
				"tool_call_id": awaiting.ToolCallID, "tool_version_id": awaiting.ToolVersionID,
			}}); emitErr != nil {
				return emitErr
			}
			return emitContext(ctx, emit, Event{Type: "run_finished", Data: map[string]string{"status": "awaiting_approval"}})
		}
		return fmt.Errorf("resume Eino approval checkpoint: %w", err)
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

func containsToolVersion(bindings []ToolBinding, versionID string) bool {
	for _, binding := range bindings {
		if binding.VersionID == versionID {
			return true
		}
	}
	return false
}
