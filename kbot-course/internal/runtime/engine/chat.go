package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/schema"

	"github.com/Q1mi/kbot/internal/a2ui"
	courseotel "github.com/Q1mi/kbot/internal/infrastructure/otel"
	"github.com/Q1mi/kbot/internal/platform/audit"
	"github.com/Q1mi/kbot/internal/platform/modelconfig"
	"github.com/Q1mi/kbot/internal/platform/skill"
	"github.com/Q1mi/kbot/internal/runtime/skillrunner"
	"go.opentelemetry.io/otel/attribute"
)

// ChatRequest 是一次运行请求；ConversationID 用于多轮会话续接。
type ChatRequest struct {
	ConversationID   string `json:"conversation_id"`
	Message          string `json:"message"`
	WorkspaceID      string `json:"-"`
	UserID           string `json:"-"`
	AgentEnvironment string `json:"agent_env,omitempty"`
}

// Event 是 Runtime 对传输层公开的稳定事件信封。
type Event struct {
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
	Text string `json:"text,omitempty"`
}

type Emitter func(Event) error

func (e *Engine) ChatStream(ctx context.Context, req ChatRequest, emit Emitter) (runErr error) {
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
	generator := e.gen
	ctx, finishTrace := courseotel.StartRun(ctx, courseotel.RunContext{
		WorkspaceID: snapshot.WorkspaceID, AgentVersionID: snapshot.ID,
		ConversationID: req.ConversationID, UserID: req.UserID,
	})
	defer func() { finishTrace(runErr) }()
	runStatus := "failed"
	defer func() {
		if e.audit == nil || req.UserID == "" || snapshot.WorkspaceID == "" {
			return
		}
		_, _ = e.audit.Append(context.WithoutCancel(ctx), audit.Event{
			WorkspaceID: snapshot.WorkspaceID, ActorID: req.UserID, Action: "agent.run." + runStatus,
			ResourceID: req.ConversationID, Data: map[string]any{"agent_version_id": snapshot.ID},
		})
	}()
	if snapshot.ModelProfileVersionID != "" {
		if e.profiles == nil {
			return fmt.Errorf("model profile resolver is required by the pinned snapshot")
		}
		profile, resolveErr := e.profiles.Resolve(ctx, snapshot.WorkspaceID, snapshot.ModelProfileVersionID)
		if resolveErr != nil {
			return fmt.Errorf("resolve pinned model profile: %w", resolveErr)
		}
		routed, ok := e.gen.(RoutedGenerator)
		if !ok {
			return fmt.Errorf("generator does not support pinned model profiles")
		}
		generator = pinnedProfileGenerator{next: routed, profile: profile}
	}
	if generator == nil {
		return fmt.Errorf("generator is required")
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
	input := req.Message
	if e.guard != nil {
		decision, guardErr := e.guard.Evaluate(ctx, snapshot.WorkspaceID, "on_input", input)
		if guardErr != nil {
			return fmt.Errorf("evaluate input guard: %w", guardErr)
		}
		if err := emitContext(ctx, emit, Event{Type: "guard_decision", Data: decision}); err != nil {
			return err
		}
		if !decision.Allowed {
			if err := emitContext(ctx, emit, Event{Type: "guard_blocked", Data: map[string]any{"hook": "on_input", "reasons": decision.Reasons}}); err != nil {
				return err
			}
			runStatus = "blocked"
			return emitContext(ctx, emit, Event{Type: "run_finished", Data: map[string]string{"status": runStatus}})
		}
		input = decision.SanitizedText
		generator = guardedGenerator{next: generator, guard: e.guard, workspaceID: snapshot.WorkspaceID}
	}
	var bindings []ToolBinding
	if len(snapshot.ToolVersionIDs) > 0 {
		if e.tools == nil {
			return fmt.Errorf("tool runtime is required by the pinned agent snapshot")
		}
		bindings, err = e.tools.Bind(ctx, snapshot.WorkspaceID, snapshot.ToolVersionIDs)
		if err != nil {
			return fmt.Errorf("bind pinned tools: %w", err)
		}
	}
	maxSteps := snapshot.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 4
	}
	if len(snapshot.SkillVersionIDs) > 0 {
		if e.skills == nil {
			return fmt.Errorf("skill resolver is required by the pinned agent snapshot")
		}
		packages := make([]skill.Package, 0, len(snapshot.SkillVersionIDs))
		for _, versionID := range snapshot.SkillVersionIDs {
			version, resolveErr := e.skills.Resolve(ctx, snapshot.WorkspaceID, versionID)
			if resolveErr != nil {
				return fmt.Errorf("resolve pinned skill %s: %w", versionID, resolveErr)
			}
			packages = append(packages, version.Package)
		}
		if active, ok := skillrunner.Select(input, packages); ok {
			applied, applyErr := skillrunner.Apply(systemPrompt, active, bindings)
			if applyErr != nil {
				return fmt.Errorf("apply skill %s: %w", active.Name, applyErr)
			}
			systemPrompt, bindings, maxSteps = applied.SystemPrompt, applied.Tools, applied.MaxSteps
			if e.audit != nil && req.UserID != "" {
				_, _ = e.audit.Append(context.WithoutCancel(ctx), audit.Event{
					WorkspaceID: snapshot.WorkspaceID, ActorID: req.UserID, Action: "skill.triggered",
					ResourceID: active.Name, Data: map[string]any{"conversation_id": req.ConversationID},
				})
			}
		} else {
			systemPrompt += skillrunner.L1(packages)
		}
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
		if historyErr := history.AppendMessage(ctx, snapshot.WorkspaceID, req.ConversationID, "user", input); historyErr != nil {
			return fmt.Errorf("persist user message: %w", historyErr)
		}
	}
	messages = append(messages, schema.UserMessage(input))
	generator = observedGenerator{next: generator}
	var answer *schema.Message
	if len(bindings) > 0 {
		answer, err = NewReActRunner(generator, e.tools, snapshot.WorkspaceID).
			WithApprovals(e.approvals, req.ConversationID).
			WithAudit(e.audit, req.UserID).
			Run(ctx, messages, bindings, maxSteps, emit)
	} else {
		answer, err = generator.Generate(ctx, messages, nil)
	}
	if err != nil {
		var awaiting *AwaitingApprovalError
		if errors.As(err, &awaiting) {
			for _, message := range a2ui.ApprovalMessages(awaiting.ApprovalID, req.ConversationID, awaiting.ToolName, awaiting.Arguments) {
				if validateErr := a2ui.Validate(message); validateErr != nil {
					return fmt.Errorf("validate approval A2UI: %w", validateErr)
				}
				if emitErr := emitContext(ctx, emit, Event{Type: "a2ui", Data: message}); emitErr != nil {
					return emitErr
				}
			}
			if emitErr := emitContext(ctx, emit, Event{Type: "approval_requested", Data: map[string]string{
				"approval_id": awaiting.ApprovalID, "tool_name": awaiting.ToolName,
				"tool_call_id": awaiting.ToolCallID, "tool_version_id": awaiting.ToolVersionID,
			}}); emitErr != nil {
				return emitErr
			}
			runStatus = "awaiting_approval"
			return emitContext(ctx, emit, Event{Type: "run_finished", Data: map[string]string{"status": runStatus}})
		}
		_ = emitContext(ctx, emit, Event{Type: "error", Data: map[string]string{"message": err.Error()}})
		return fmt.Errorf("generate: %w", err)
	}
	if e.guard != nil {
		decision, guardErr := e.guard.Evaluate(ctx, snapshot.WorkspaceID, "on_output", answer.Content)
		if guardErr != nil {
			return fmt.Errorf("evaluate output guard: %w", guardErr)
		}
		if !decision.Allowed {
			if err := emitContext(ctx, emit, Event{Type: "guard_blocked", Data: map[string]any{"hook": "on_output", "reasons": decision.Reasons}}); err != nil {
				return err
			}
			runStatus = "blocked"
			return emitContext(ctx, emit, Event{Type: "run_finished", Data: map[string]string{"status": runStatus}})
		}
		answer.Content = decision.SanitizedText
	}
	if history, ok := e.platform.(ConversationMessageStore); ok {
		if historyErr := history.AppendMessage(ctx, snapshot.WorkspaceID, req.ConversationID, "assistant", answer.Content); historyErr != nil {
			return fmt.Errorf("persist assistant message: %w", historyErr)
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
	if err := emitContext(ctx, emit, Event{Type: "run_finished", Data: map[string]string{"status": "completed"}}); err != nil {
		return err
	}
	runStatus = "completed"
	return nil
}

type pinnedProfileGenerator struct {
	next    RoutedGenerator
	profile modelconfig.ProfileVersion
}

type guardedGenerator struct {
	next        Generator
	guard       RuntimeGuard
	workspaceID string
}

type observedGenerator struct{ next Generator }

func (g observedGenerator) Generate(ctx context.Context, messages []*schema.Message, tools []*schema.ToolInfo) (answer *schema.Message, err error) {
	ctx, finish := courseotel.StartOperation(ctx, "llm.generate",
		attribute.String("gen_ai.operation.name", "chat"),
		attribute.Int("gen_ai.request.tool_count", len(tools)),
	)
	defer func() { finish(err) }()
	return g.next.Generate(ctx, messages, tools)
}

func (g guardedGenerator) Generate(ctx context.Context, messages []*schema.Message, tools []*schema.ToolInfo) (*schema.Message, error) {
	decision, err := g.guard.Evaluate(ctx, g.workspaceID, "on_llm_call", "")
	if err != nil {
		return nil, fmt.Errorf("evaluate LLM quota: %w", err)
	}
	if !decision.Allowed {
		return nil, fmt.Errorf("LLM call blocked by guard: %s", strings.Join(decision.Reasons, ","))
	}
	return g.next.Generate(ctx, messages, tools)
}

func (g pinnedProfileGenerator) Generate(ctx context.Context, messages []*schema.Message, tools []*schema.ToolInfo) (*schema.Message, error) {
	return g.next.GenerateWithProfile(ctx, g.profile, messages, tools)
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
