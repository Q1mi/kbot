package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/cloudwego/eino/schema"

	platformskill "github.com/Q1mi/kbot/internal/platform/skill"
	"github.com/Q1mi/kbot/internal/runtime/llm"
	"github.com/Q1mi/kbot/internal/runtime/skillrunner"
)

type ChatRequest struct {
	ConversationID   string `json:"conversation_id"`
	Message          string `json:"message"`
	WorkspaceID      string `json:"-"`
	AgentEnvironment string `json:"agent_env,omitempty"`
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
	packages, err := e.resolveSkills(ctx, snapshot)
	if err != nil {
		return err
	}
	if err := emitContext(ctx, emit, Event{Type: "run_started", Data: map[string]string{
		"conversation_id": req.ConversationID, "agent_version_id": snapshot.ID,
	}}); err != nil {
		return err
	}
	messages := []*schema.Message{schema.SystemMessage(systemPrompt)}
	if history, ok := e.platform.(ConversationMessageStore); ok {
		stored, historyErr := history.ListMessages(ctx, snapshot.WorkspaceID, req.ConversationID)
		if historyErr != nil {
			return fmt.Errorf("load conversation history: %w", historyErr)
		}
		for _, message := range stored {
			switch message.Role {
			case "user":
				messages = append(messages, schema.UserMessage(message.Content))
			case "assistant":
				messages = append(messages, schema.AssistantMessage(message.Content, nil))
			}
		}
		if historyErr := history.AppendMessage(ctx, snapshot.WorkspaceID, req.ConversationID, "user", req.Message); historyErr != nil {
			return fmt.Errorf("persist user message: %w", historyErr)
		}
	}
	messages = append(messages, schema.UserMessage(req.Message))
	answer, streamed, err := e.runPlan(ctx, req.ConversationID, snapshot, plan, packages, messages, emit)
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
		_ = emitContext(ctx, emit, Event{Type: "error", Data: map[string]string{"message": err.Error()}})
		return fmt.Errorf("generate: %w", err)
	}
	if history, ok := e.platform.(ConversationMessageStore); ok {
		if historyErr := history.AppendMessage(ctx, snapshot.WorkspaceID, req.ConversationID, "assistant", answer.Content); historyErr != nil {
			return fmt.Errorf("persist assistant message: %w", historyErr)
		}
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

func (e *Engine) resolveSkills(ctx context.Context, snapshot *AgentSnapshot) ([]platformskill.Package, error) {
	if len(snapshot.SkillVersionIDs) == 0 {
		return nil, nil
	}
	if e.skills == nil {
		return nil, fmt.Errorf("skill resolver is required by the pinned snapshot")
	}
	packages := make([]platformskill.Package, 0, len(snapshot.SkillVersionIDs))
	for _, versionID := range snapshot.SkillVersionIDs {
		version, err := e.skills.Resolve(ctx, snapshot.WorkspaceID, versionID)
		if err != nil {
			return nil, fmt.Errorf("resolve pinned skill %s: %w", versionID, err)
		}
		packages = append(packages, version.Package)
	}
	return packages, nil
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
	ctx context.Context, conversationID string, snapshot *AgentSnapshot, plan *llm.ExecutionPlan,
	packages []platformskill.Package, messages []*schema.Message, emit Emitter,
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
	skillRuntime, err := skillrunner.NewRuntime(ctx, packages, bindings, messages[len(messages)-1].Content, func(pkg platformskill.Package) error {
		return emitContext(ctx, emit, Event{Type: "skill_trigger", Data: map[string]string{"name": pkg.Name}})
	})
	if err != nil {
		return nil, false, err
	}
	if skillRuntime != nil && skillRuntime.ExplicitName != "" {
		messages[0].Content += fmt.Sprintf("\n\n用户已显式选择 Skill %q；请先调用 skill 工具加载完整说明。", skillRuntime.ExplicitName)
	}
	if len(bindings) > 0 || plan.Retry != nil || plan.Failover != nil || skillRuntime != nil {
		maxSteps := snapshot.MaxSteps
		if maxSteps <= 0 {
			maxSteps = 4
		}
		var authorize func(string, string) error
		if skillRuntime != nil {
			authorize = skillRuntime.Authorize
		}
		runner := NewADKRunner(plan.Model, e.tools, snapshot.WorkspaceID).
			WithModelPolicy(plan.Retry, plan.Failover).
			WithToolAuthorization(authorize).
			WithApprovals(e.approvals, conversationID)
		if skillRuntime != nil {
			runner.WithHandlers(skillRuntime.Handlers...)
		}
		answer, err := runner.Run(ctx, messages, bindings, maxSteps, emit)
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
