package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"go.opentelemetry.io/otel/attribute"

	courseotel "github.com/Q1mi/kbot/internal/infrastructure/otel"
	"github.com/Q1mi/kbot/internal/platform/approval"
	"github.com/Q1mi/kbot/internal/platform/audit"
	"github.com/Q1mi/kbot/internal/runtime/tooling"
)

const frameworkCheckpointVersion = 1

type approvalInterruptInfo struct {
	ApprovalID, RunID, ToolName, ToolCallID, ToolVersionID, Arguments, ActiveSkillName string
}

type approvalInterruptState struct{ Info approvalInterruptInfo }
type approvalResumeDecision struct{ Approved bool }

type persistedFrameworkCheckpoint struct {
	Version         int    `json:"version"`
	InterruptID     string `json:"interrupt_id"`
	ActiveSkillName string `json:"active_skill_name,omitempty"`
	Data            []byte `json:"data"`
}

func init() {
	schema.RegisterName[approvalInterruptInfo]("course_approval_interrupt_info_v1")
	schema.RegisterName[approvalInterruptState]("course_approval_interrupt_state_v1")
	schema.RegisterName[approvalResumeDecision]("course_approval_resume_decision_v1")
}

type ToolBinding = tooling.Binding

type ToolExecutor interface {
	Execute(ctx context.Context, call tooling.Call) (tooling.Result, error)
}

type ApprovalGate interface {
	Create(ctx context.Context, request approval.Request) (*approval.Request, error)
	SaveCheckpoint(ctx context.Context, workspaceID, requestID string, checkpoint []byte) error
}

type ADKRunner struct {
	model        model.BaseChatModel
	executor     ToolExecutor
	workspaceID  string
	retry        *adk.ModelRetryConfig
	failover     *adk.ModelFailoverConfig[*schema.Message]
	handlers     []adk.ChatModelAgentMiddleware
	authorize    func(name, arguments string) error
	approvals    ApprovalGate
	runID        string
	activeSkill  func() string
	restoreSkill func(string) error
	audit        AuditSink
	actorID      string
}

func NewADKRunner(chatModel model.BaseChatModel, executor ToolExecutor, workspaceID string) *ADKRunner {
	return &ADKRunner{model: chatModel, executor: executor, workspaceID: workspaceID}
}

func (r *ADKRunner) WithModelPolicy(retry *adk.ModelRetryConfig, failover *adk.ModelFailoverConfig[*schema.Message]) *ADKRunner {
	r.retry, r.failover = retry, failover
	return r
}

func (r *ADKRunner) WithHandlers(handlers ...adk.ChatModelAgentMiddleware) *ADKRunner {
	r.handlers = append(r.handlers, handlers...)
	return r
}

func (r *ADKRunner) WithToolAuthorization(authorize func(name, arguments string) error) *ADKRunner {
	r.authorize = authorize
	return r
}

func (r *ADKRunner) WithApprovals(approvals ApprovalGate, runID string) *ADKRunner {
	r.approvals, r.runID = approvals, runID
	return r
}

func (r *ADKRunner) WithSkillState(active func() string, restore func(string) error) *ADKRunner {
	r.activeSkill, r.restoreSkill = active, restore
	return r
}

func (r *ADKRunner) WithAudit(sink AuditSink, actorID string) *ADKRunner {
	r.audit, r.actorID = sink, actorID
	return r
}

type AwaitingApprovalError struct {
	ApprovalID, ToolName, ToolCallID, ToolVersionID string
}

func (e *AwaitingApprovalError) Error() string {
	return "tool call is awaiting approval: " + e.ApprovalID
}

func (r *ADKRunner) Run(
	ctx context.Context, messages []*schema.Message, bindings []ToolBinding, maxSteps int, emit Emitter,
) (*schema.Message, error) {
	agent, err := r.newAgent(ctx, bindings, maxSteps, emit)
	if err != nil {
		return nil, err
	}
	store := &frameworkCheckpointStore{}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, CheckPointStore: store})
	iterator := runner.Run(ctx, messages, adk.WithCheckPointID(r.runID))
	answer, interrupted, err := consumeADKEvents(iterator)
	if err != nil {
		return nil, err
	}
	if len(interrupted) > 0 {
		return nil, r.persistInterrupts(ctx, store.Latest(), interrupted)
	}
	if answer == nil {
		return nil, fmt.Errorf("Eino ADK run finished without a final answer")
	}
	return answer, nil
}

func (r *ADKRunner) Resume(
	ctx context.Context, checkpointID string, checkpoint []byte, interruptID string,
	bindings []ToolBinding, maxSteps int, emit Emitter,
) (*schema.Message, error) {
	agent, err := r.newAgent(ctx, bindings, maxSteps, emit)
	if err != nil {
		return nil, err
	}
	store := newFrameworkCheckpointStore(checkpointID, checkpoint)
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, CheckPointStore: store})
	iterator, err := runner.ResumeWithParams(ctx, checkpointID, &adk.ResumeParams{Targets: map[string]any{
		interruptID: approvalResumeDecision{Approved: true},
	}})
	if err != nil {
		return nil, err
	}
	answer, interrupted, err := consumeADKEvents(iterator)
	if err != nil {
		return nil, err
	}
	if len(interrupted) > 0 {
		return nil, r.persistInterrupts(ctx, store.Latest(), interrupted)
	}
	if answer == nil {
		return nil, fmt.Errorf("Eino ADK resume finished without a final answer")
	}
	return answer, nil
}

func (r *ADKRunner) newAgent(ctx context.Context, bindings []ToolBinding, maxSteps int, emit Emitter) (*adk.ChatModelAgent, error) {
	if r.model == nil || (len(bindings) > 0 && r.executor == nil) {
		return nil, fmt.Errorf("chat model and required tool executor are required")
	}
	if maxSteps <= 0 {
		return nil, fmt.Errorf("max steps must be positive")
	}
	tools := make([]einotool.BaseTool, 0, len(bindings))
	byName := make(map[string]ToolBinding, len(bindings))
	for _, binding := range bindings {
		byName[binding.Name] = binding
		tools = append(tools, &bindingTool{
			binding: binding, executor: r.executor, workspaceID: r.workspaceID,
			audit: r.audit, actorID: r.actorID, runID: r.runID,
		})
	}
	return adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name: "course_agent", Description: "kbot course agent", Model: r.model,
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{
			Tools: tools, ExecuteSequentially: true,
			ToolCallMiddlewares: []compose.ToolMiddleware{toolEventMiddleware(emit, r.authorize, r.approvalInterrupt(byName))},
		}},
		MaxIterations:       maxSteps,
		Handlers:            r.handlers,
		ModelRetryConfig:    r.retry,
		ModelFailoverConfig: r.failover,
	})
}

func (r *ADKRunner) approvalInterrupt(bindings map[string]ToolBinding) func(context.Context, *compose.ToolInput) error {
	return func(ctx context.Context, input *compose.ToolInput) error {
		binding, ok := bindings[input.Name]
		if !ok || !binding.Sensitive {
			return nil
		}
		if r.approvals == nil {
			return fmt.Errorf("sensitive tool %s requires an approval service", binding.Name)
		}
		wasInterrupted, hasState, stored := einotool.GetInterruptState[approvalInterruptState](ctx)
		if !wasInterrupted {
			created, err := r.approvals.Create(ctx, approval.Request{
				WorkspaceID: r.workspaceID, RunID: r.runID, ToolCallID: input.CallID,
				ToolVersionID: binding.VersionID, Arguments: []byte(input.Arguments),
			})
			if err != nil {
				return fmt.Errorf("create approval: %w", err)
			}
			info := approvalInterruptInfo{
				ApprovalID: created.ID, RunID: r.runID, ToolName: input.Name, ToolCallID: input.CallID,
				ToolVersionID: binding.VersionID, Arguments: input.Arguments,
			}
			if r.activeSkill != nil {
				info.ActiveSkillName = r.activeSkill()
			}
			return einotool.StatefulInterrupt(ctx, info, approvalInterruptState{Info: info})
		}
		if !hasState {
			return fmt.Errorf("approval interrupt state is missing")
		}
		if stored.Info.ActiveSkillName != "" && r.restoreSkill != nil {
			if err := r.restoreSkill(stored.Info.ActiveSkillName); err != nil {
				return err
			}
		}
		isTarget, hasData, decision := einotool.GetResumeContext[approvalResumeDecision](ctx)
		if !isTarget {
			return einotool.StatefulInterrupt(ctx, stored.Info, stored)
		}
		if !hasData || !decision.Approved {
			return fmt.Errorf("approval was not granted")
		}
		return nil
	}
}

func (r *ADKRunner) persistInterrupts(ctx context.Context, checkpoint []byte, interrupts []*adk.InterruptCtx) error {
	if len(checkpoint) == 0 {
		return fmt.Errorf("eino did not produce an approval checkpoint")
	}
	for _, interrupt := range interrupts {
		if interrupt == nil || !interrupt.IsRootCause {
			continue
		}
		info, ok := approvalInfo(interrupt.Info)
		if !ok {
			continue
		}
		payload, err := json.Marshal(persistedFrameworkCheckpoint{
			Version: frameworkCheckpointVersion, InterruptID: interrupt.ID,
			ActiveSkillName: info.ActiveSkillName, Data: checkpoint,
		})
		if err != nil {
			return err
		}
		if err := r.approvals.SaveCheckpoint(ctx, r.workspaceID, info.ApprovalID, payload); err != nil {
			return err
		}
		return &AwaitingApprovalError{
			ApprovalID: info.ApprovalID, ToolName: info.ToolName,
			ToolCallID: info.ToolCallID, ToolVersionID: info.ToolVersionID,
		}
	}
	return fmt.Errorf("approval interruption has no root cause")
}

func approvalInfo(value any) (approvalInterruptInfo, bool) {
	switch info := value.(type) {
	case approvalInterruptInfo:
		return info, true
	case *approvalInterruptInfo:
		if info != nil {
			return *info, true
		}
	}
	return approvalInterruptInfo{}, false
}

func consumeADKEvents(iterator *adk.AsyncIterator[*adk.AgentEvent]) (*schema.Message, []*adk.InterruptCtx, error) {
	var answer *schema.Message
	var interrupted []*adk.InterruptCtx
	for {
		event, ok := iterator.Next()
		if !ok {
			break
		}
		if event == nil {
			continue
		}
		if event.Err != nil {
			return nil, nil, event.Err
		}
		if event.Action != nil && event.Action.Interrupted != nil {
			interrupted = append(interrupted, event.Action.Interrupted.InterruptContexts...)
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}
		message, err := event.Output.MessageOutput.GetMessage()
		if err != nil {
			return nil, nil, err
		}
		if message != nil && message.Role == schema.Assistant && len(message.ToolCalls) == 0 {
			answer = message
		}
	}
	return answer, interrupted, nil
}

type frameworkCheckpointStore struct {
	mu      sync.RWMutex
	initial map[string][]byte
	latest  []byte
}

func newFrameworkCheckpointStore(id string, data []byte) *frameworkCheckpointStore {
	store := &frameworkCheckpointStore{}
	if id != "" && len(data) > 0 {
		store.initial = map[string][]byte{id: append([]byte(nil), data...)}
	}
	return store
}

func (s *frameworkCheckpointStore) Get(_ context.Context, id string) ([]byte, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, ok := s.initial[id]
	return append([]byte(nil), data...), ok, nil
}

func (s *frameworkCheckpointStore) Set(_ context.Context, _ string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latest = append(s.latest[:0], data...)
	return nil
}

func (s *frameworkCheckpointStore) Latest() []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]byte(nil), s.latest...)
}

type toolCallIDKey struct{}

type bindingTool struct {
	binding     ToolBinding
	executor    ToolExecutor
	workspaceID string
	audit       AuditSink
	actorID     string
	runID       string
}

func (t *bindingTool) Info(context.Context) (*schema.ToolInfo, error) {
	if t.binding.Info == nil {
		return nil, fmt.Errorf("tool %s has no schema", t.binding.Name)
	}
	info := *t.binding.Info
	info.Name = t.binding.Name
	return &info, nil
}

func (t *bindingTool) InvokableRun(ctx context.Context, arguments string, _ ...einotool.Option) (string, error) {
	callID, _ := ctx.Value(toolCallIDKey{}).(string)
	operation := "tool.execute"
	if t.binding.SourceType == "code_execution" {
		operation = "sandbox.execute"
	}
	toolContext, finish := courseotel.StartOperation(ctx, operation,
		attribute.String("gen_ai.tool.name", t.binding.Name),
		attribute.String("kbot.tool.version.id", t.binding.VersionID),
		attribute.String("kbot.tool.call.id", callID),
	)
	result, err := t.executor.Execute(toolContext, tooling.Call{
		WorkspaceID: t.workspaceID, ToolVersionID: t.binding.VersionID,
		Arguments: []byte(arguments), IdempotencyKey: "react:" + callID,
	})
	finish(err)
	if t.audit != nil && t.actorID != "" {
		data := map[string]any{"conversation_id": t.runID, "tool_call_id": callID, "status_code": result.StatusCode}
		if err != nil {
			data["failed"] = true
		}
		_, _ = t.audit.Append(context.WithoutCancel(ctx), audit.Event{
			WorkspaceID: t.workspaceID, ActorID: t.actorID, Action: operation,
			ResourceID: t.binding.VersionID, Data: data,
		})
	}
	if err != nil {
		return "", err
	}
	return string(result.Body), nil
}

func toolEventMiddleware(
	emit Emitter,
	authorize func(name, arguments string) error,
	interrupt func(context.Context, *compose.ToolInput) error,
) compose.ToolMiddleware {
	return compose.ToolMiddleware{Invokable: func(next compose.InvokableToolEndpoint) compose.InvokableToolEndpoint {
		return func(ctx context.Context, input *compose.ToolInput) (*compose.ToolOutput, error) {
			if emit != nil {
				if err := emitContext(ctx, emit, Event{Type: "tool_started", Data: map[string]any{"name": input.Name, "call_id": input.CallID}}); err != nil {
					return nil, err
				}
			}
			var output *compose.ToolOutput
			var err error
			if authorizeErr := authorizeTool(authorize, input); authorizeErr != nil {
				output = &compose.ToolOutput{Result: fmt.Sprintf(`{"error":%q}`, authorizeErr.Error())}
			} else if interruptErr := interrupt(ctx, input); interruptErr != nil {
				return nil, interruptErr
			} else {
				output, err = next(context.WithValue(ctx, toolCallIDKey{}, input.CallID), input)
			}
			if err != nil {
				if _, isInterrupt := compose.IsInterruptRerunError(err); isInterrupt {
					return nil, err
				}
				output, err = &compose.ToolOutput{Result: fmt.Sprintf(`{"error":%q}`, err.Error())}, nil
			}
			if emit != nil {
				if emitErr := emitContext(ctx, emit, Event{Type: "tool_finished", Data: map[string]any{"name": input.Name, "call_id": input.CallID}}); emitErr != nil {
					return nil, emitErr
				}
			}
			return output, err
		}
	}}
}

func authorizeTool(authorize func(name, arguments string) error, input *compose.ToolInput) error {
	if authorize == nil {
		return nil
	}
	return authorize(input.Name, input.Arguments)
}
