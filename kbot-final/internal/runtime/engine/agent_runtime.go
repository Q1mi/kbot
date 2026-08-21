package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/patchtoolcalls"
	einoskill "github.com/cloudwego/eino/adk/middlewares/skill"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"go.opentelemetry.io/otel/attribute"

	"github.com/Q1mi/kbot/internal/a2ui"
	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/infrastructure/metrics"
	"github.com/Q1mi/kbot/internal/runtime/llm"
	"github.com/Q1mi/kbot/internal/runtime/skillrunner"
	"github.com/Q1mi/kbot/internal/runtime/tooling"
	"github.com/Q1mi/kbot/internal/util"
)

const frameworkCheckpointVersion = 1

type approvalInterruptInfo struct {
	ApprovalID     string
	ConversationID string
	ToolName       string
	Arguments      string
	ToolCallID     string
}

type approvalInterruptState struct {
	Info            approvalInterruptInfo
	ActiveSkillName string
}

type approvalResumeDecision struct {
	Approved bool
	Reason   string
}

type persistedFrameworkCheckpoint struct {
	Version     int    `json:"version"`
	InterruptID string `json:"interrupt_id"`
	Data        []byte `json:"data"`
}

func init() {
	schema.RegisterName[approvalInterruptInfo]("kbot_approval_interrupt_info_v1")
	schema.RegisterName[approvalInterruptState]("kbot_approval_interrupt_state_v1")
	schema.RegisterName[approvalResumeDecision]("kbot_approval_resume_decision_v1")
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

type agentRunState struct {
	mu             sync.RWMutex
	engine         *Engine
	conversationID string
	workspaceID    string
	allowNetwork   bool
	agentKBs       map[string]bool
	policies       toolPolicies
	skills         []skillrunner.Spec
	activeSkill    *skillrunner.Spec
	explicitSkill  string
	batchDenied    map[string]string
	emitter        emitter
}

func (s *agentRunState) activateSkill(name string) (skillrunner.Spec, bool) {
	spec, ok := skillrunner.Find(s.skills, name)
	if !ok {
		return skillrunner.Spec{}, false
	}
	s.mu.Lock()
	s.activeSkill = &spec
	s.mu.Unlock()
	return spec, true
}

func (s *agentRunState) activeSkillSnapshot() *skillrunner.Spec {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.activeSkill == nil {
		return nil
	}
	cloned := *s.activeSkill
	return &cloned
}

func (s *agentRunState) toolPolicyError(name, arguments string) error {
	active := s.activeSkillSnapshot()
	if active != nil && !containsString(active.AllowedTools, name) {
		return fmt.Errorf("tool %q not allowed by active skill %q", name, active.Name)
	}
	if s.policies.requiresNetwork[name] && !s.allowNetwork {
		return fmt.Errorf("tool %q requires network access, but the agent version does not allow it", name)
	}
	if !s.policies.kbScoped[name] {
		return nil
	}
	var input struct {
		KBID string `json:"kb_id"`
	}
	if err := json.Unmarshal([]byte(arguments), &input); err != nil || input.KBID == "" {
		return fmt.Errorf("tool %q requires a valid kb_id", name)
	}
	if !s.agentKBs[input.KBID] {
		return fmt.Errorf("knowledge base %q is not attached to the agent", input.KBID)
	}
	if active != nil && !containsString(active.AllowedKBs, input.KBID) {
		return fmt.Errorf("knowledge base %q is not allowed by active skill %q", input.KBID, active.Name)
	}
	return nil
}

func (s *agentRunState) setBatchPolicy(toolCalls []schema.ToolCall) {
	denied := make(map[string]string)
	invalid := false
	for _, call := range toolCalls {
		if call.Function.Name == "skill" {
			continue
		}
		if err := s.toolPolicyError(call.Function.Name, call.Function.Arguments); err != nil {
			invalid = true
			denied[call.ID] = "error: " + err.Error()
		}
	}
	if invalid {
		for _, call := range toolCalls {
			if _, exists := denied[call.ID]; !exists {
				denied[call.ID] = "error: tool call batch rejected because another call violated policy"
			}
		}
	}
	s.mu.Lock()
	s.batchDenied = denied
	s.mu.Unlock()
}

func (s *agentRunState) batchPolicyResult(callID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.batchDenied[callID]
}

type runtimePolicyHandler struct {
	*adk.BaseChatModelAgentMiddleware
	state *agentRunState
}

func (h *runtimePolicyHandler) BeforeModelRewriteState(
	ctx context.Context, state *adk.ChatModelAgentState, modelContext *adk.ModelContext,
) (context.Context, *adk.ChatModelAgentState, error) {
	if err := h.state.engine.guardLLMCall(ctx); err != nil {
		return ctx, nil, err
	}
	active := h.state.activeSkillSnapshot()
	if active == nil {
		return ctx, state, nil
	}
	allowed := make(map[string]bool, len(active.AllowedTools))
	for _, name := range active.AllowedTools {
		allowed[name] = true
	}
	filtered := make([]*schema.ToolInfo, 0, len(state.ToolInfos))
	for _, info := range state.ToolInfos {
		if allowed[info.Name] {
			filtered = append(filtered, info)
		}
	}
	state.ToolInfos = filtered
	return ctx, state, nil
}

func (h *runtimePolicyHandler) AfterModelRewriteState(
	ctx context.Context, state *adk.ChatModelAgentState, modelContext *adk.ModelContext,
) (context.Context, *adk.ChatModelAgentState, error) {
	if len(state.Messages) == 0 {
		return ctx, state, nil
	}
	last := state.Messages[len(state.Messages)-1]
	if last != nil && last.Role == schema.Assistant && len(last.ToolCalls) > 0 {
		h.state.setBatchPolicy(last.ToolCalls)
	}
	return ctx, state, nil
}

func (e *Engine) runWithADK(
	ctx context.Context, snapshot *AgentSnapshot, messages []*schema.Message, policies toolPolicies,
	conversationID, workspaceID string, em emitter,
) (string, []*adk.InterruptCtx, []byte, error) {
	plan, err := e.executionPlan(ctx)
	if err != nil {
		return "", nil, nil, err
	}
	state := &agentRunState{
		engine: e, conversationID: conversationID, workspaceID: workspaceID,
		allowNetwork: snapshot.AllowNetwork, agentKBs: stringSet(snapshot.KBIDs),
		policies: policies, skills: snapshot.Skills, emitter: em,
	}
	state.explicitSkill, _ = explicitSkillFromMessages(messages, snapshot.Skills)
	agent, err := e.newADKAgent(ctx, snapshot, plan, state)
	if err != nil {
		return "", nil, nil, err
	}
	store := newFrameworkCheckpointStore("", nil)
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, EnableStreaming: true, CheckPointStore: store})
	checkpointID := util.GenerateID()
	answer, interrupted, runErr := consumeADKEvents(ctx, runner.Run(ctx, messages, adk.WithCheckPointID(checkpointID)))
	return answer, interrupted, store.Latest(), runErr
}

func (e *Engine) resumeWithADK(
	ctx context.Context, snapshot *AgentSnapshot, policies toolPolicies, conversationID, workspaceID,
	approvalID, interruptID string, checkpoint []byte, em emitter,
) (string, []*adk.InterruptCtx, []byte, error) {
	plan, err := e.executionPlan(ctx)
	if err != nil {
		return "", nil, nil, err
	}
	state := &agentRunState{
		engine: e, conversationID: conversationID, workspaceID: workspaceID,
		allowNetwork: snapshot.AllowNetwork, agentKBs: stringSet(snapshot.KBIDs),
		policies: policies, skills: snapshot.Skills, emitter: em,
	}
	agent, err := e.newADKAgent(ctx, snapshot, plan, state)
	if err != nil {
		return "", nil, nil, err
	}
	store := newFrameworkCheckpointStore(approvalID, checkpoint)
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, EnableStreaming: true, CheckPointStore: store})
	iterator, err := runner.ResumeWithParams(ctx, approvalID, &adk.ResumeParams{Targets: map[string]any{
		interruptID: approvalResumeDecision{Approved: true},
	}})
	if err != nil {
		return "", nil, nil, err
	}
	answer, interrupted, runErr := consumeADKEvents(ctx, iterator)
	return answer, interrupted, store.Latest(), runErr
}

func (e *Engine) executionPlan(ctx context.Context) (*llm.ExecutionPlan, error) {
	if e.planner != nil {
		return e.planner.PrepareExecution(ctx)
	}
	if e.model != nil {
		return &llm.ExecutionPlan{Model: e.model}, nil
	}
	return nil, fmt.Errorf("chat model is not configured")
}

func (e *Engine) newADKAgent(
	ctx context.Context, snapshot *AgentSnapshot, plan *llm.ExecutionPlan, state *agentRunState,
) (*adk.ChatModelAgent, error) {
	instruction := snapshot.SystemPrompt
	if state.explicitSkill != "" {
		instruction += fmt.Sprintf("\n\n用户已显式选择 Skill %q。先调用 skill 工具加载该 Skill，再处理用户请求。", state.explicitSkill)
	}
	handlers := make([]adk.ChatModelAgentMiddleware, 0, 3)
	visibleSkills := make([]skillrunner.Spec, 0, len(snapshot.Skills))
	for _, spec := range snapshot.Skills {
		if !spec.DisableModelInvocation {
			visibleSkills = append(visibleSkills, spec)
		}
	}
	if explicitName := state.explicitSkill; explicitName != "" {
		if spec, found := skillrunner.Find(snapshot.Skills, explicitName); found && !containsSkill(visibleSkills, explicitName) {
			visibleSkills = append(visibleSkills, spec)
		}
	}
	if len(visibleSkills) > 0 {
		skillHandler, err := einoskill.NewMiddleware(ctx, &einoskill.Config{
			Backend:    skillrunner.NewBackend(visibleSkills),
			UseChinese: true,
			BuildContent: func(ctx context.Context, loaded einoskill.Skill, _ string) (string, error) {
				spec, ok := skillrunner.Find(state.skills, loaded.Name)
				if !ok {
					return "", fmt.Errorf("skill %q is not part of the pinned agent version", loaded.Name)
				}
				if spec.RequiresNetwork && !state.allowNetwork {
					return fmt.Sprintf("技能 %q 需要网络访问，当前 Agent 版本未授权网络。", spec.Name), nil
				}
				state.activateSkill(spec.Name)
				metrics.SkillTriggers.Inc()
				state.emitter.send(AgentEvent{Type: EventSkillTrigger, Text: spec.Name})
				if e.audit != nil {
					source := "model"
					if state.explicitSkill == spec.Name {
						source = "user"
					}
					e.audit.RecordSkillTrigger(ctx, state.workspaceID, state.conversationID, spec.VersionID, spec.Name, source)
				}
				return skillrunner.L2Message(spec), nil
			},
		})
		if err != nil {
			return nil, fmt.Errorf("create skill middleware: %w", err)
		}
		handlers = append(handlers, skillHandler)
	}
	patchHandler, err := patchtoolcalls.New(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("create tool-call patch middleware: %w", err)
	}
	handlers = append(handlers, patchHandler, &runtimePolicyHandler{state: state})

	return adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name: "kbot", Description: "Kbot enterprise agent", Instruction: instruction,
		Model: plan.Model,
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{
			Tools: policiesTools(state.policies), ExecuteSequentially: true,
			UnknownToolsHandler: func(_ context.Context, name, _ string) (string, error) {
				return fmt.Sprintf("error: tool %q is not available", name), nil
			},
			ToolCallMiddlewares: []compose.ToolMiddleware{e.runtimeToolMiddleware(state)},
		},
		},
		MaxIterations:       maxStepsOf(snapshot),
		Handlers:            handlers,
		ModelRetryConfig:    plan.Retry,
		ModelFailoverConfig: plan.Failover,
	})
}

func (e *Engine) runtimeToolMiddleware(state *agentRunState) compose.ToolMiddleware {
	return compose.ToolMiddleware{Invokable: func(next compose.InvokableToolEndpoint) compose.InvokableToolEndpoint {
		return func(ctx context.Context, input *compose.ToolInput) (*compose.ToolOutput, error) {
			if input.Name == "skill" {
				output, err := next(ctx, input)
				if err != nil {
					if _, interrupt := compose.IsInterruptRerunError(err); interrupt {
						return nil, err
					}
					return &compose.ToolOutput{Result: "error: " + err.Error()}, nil
				}
				return output, nil
			}
			metrics.ToolCalls.WithLabelValues(input.Name).Inc()
			if !state.emitter.send(AgentEvent{Type: EventToolCall, Text: input.Name, Data: json.RawMessage(input.Arguments)}) {
				return nil, state.emitter.ctx.Err()
			}
			if guarder, ok := e.guard.(interface {
				OnToolCall(context.Context, string) error
			}); ok {
				if err := guarder.OnToolCall(ctx, input.Name); err != nil {
					return nil, err
				}
			}
			if result := state.batchPolicyResult(input.CallID); result != "" {
				state.emitter.send(AgentEvent{Type: EventToolResult, Text: result, Data: input.Name})
				return &compose.ToolOutput{Result: result}, nil
			}
			if err := state.toolPolicyError(input.Name, input.Arguments); err != nil {
				result := "error: " + err.Error()
				state.emitter.send(AgentEvent{Type: EventToolResult, Text: result, Data: input.Name})
				return &compose.ToolOutput{Result: result}, nil
			}
			if state.policies.sensitive[input.Name] {
				if err := e.interruptForApproval(ctx, state, input); err != nil {
					return nil, err
				}
			}
			return e.executeGovernedTool(ctx, state, input, next)
		}
	}}
}

func (e *Engine) interruptForApproval(ctx context.Context, state *agentRunState, input *compose.ToolInput) error {
	if e.approvals == nil {
		return ErrApprovalUnavailable
	}
	wasInterrupted, hasState, stored := einotool.GetInterruptState[approvalInterruptState](ctx)
	if !wasInterrupted {
		approvalID := util.GenerateID()
		info := approvalInterruptInfo{
			ApprovalID: approvalID, ConversationID: state.conversationID,
			ToolName: input.Name, Arguments: input.Arguments, ToolCallID: input.CallID,
		}
		if err := e.approvals.CreatePending(ctx, approvalID, state.workspaceID, state.conversationID, input.Name, input.Arguments); err != nil {
			return fmt.Errorf("create pending approval: %w", err)
		}
		interruptState := approvalInterruptState{Info: info}
		if active := state.activeSkillSnapshot(); active != nil {
			interruptState.ActiveSkillName = active.Name
		}
		return einotool.StatefulInterrupt(ctx, info, interruptState)
	}
	if !hasState {
		return fmt.Errorf("approval interrupt state is missing")
	}
	if stored.ActiveSkillName != "" {
		state.activateSkill(stored.ActiveSkillName)
	}
	isTarget, hasData, decision := einotool.GetResumeContext[approvalResumeDecision](ctx)
	if !isTarget {
		return einotool.StatefulInterrupt(ctx, stored.Info, stored)
	}
	if !hasData || !decision.Approved {
		reason := decision.Reason
		if reason == "" {
			reason = "approval was not granted"
		}
		return fmt.Errorf("%s", reason)
	}
	return nil
}

func (e *Engine) executeGovernedTool(
	ctx context.Context, state *agentRunState, input *compose.ToolInput, next compose.InvokableToolEndpoint,
) (*compose.ToolOutput, error) {
	toolCtx, span := startOperationSpan(ctx, "tool.execute",
		attribute.String("gen_ai.tool.name", input.Name), attribute.String("tool.call.id", input.CallID),
	)
	if e.traceOptions.CaptureContent {
		span.SetAttributes(attribute.String("langfuse.observation.input", input.Arguments))
	}
	invocationID := ""
	startedAt := time.Now()
	if e.toolAudit != nil {
		var err error
		invocationID, err = e.toolAudit.BeginToolInvocation(
			context.WithoutCancel(ctx), state.workspaceID, state.conversationID, input.CallID,
			state.policies.versionIDs[input.Name], input.Arguments,
		)
		if err != nil {
			finishOperationSpan(toolCtx, span, "", err)
			return nil, err
		}
		toolCtx = tooling.WithSandboxObserver(toolCtx, func(result tooling.SandboxResult, runErr error) error {
			status := "success"
			if result.TimedOut {
				status = "timeout"
			} else if runErr != nil {
				status = "error"
			}
			return e.toolAudit.RecordSandboxExecution(context.WithoutCancel(ctx), &domain.SandboxExecution{
				WorkspaceID: state.workspaceID, ConversationID: state.conversationID, InvocationID: invocationID,
				ToolVersionID: state.policies.versionIDs[input.Name], ToolCallID: input.CallID,
				ExecutionID: result.ExecutionID, Language: result.Language, ContainerID: result.ContainerName,
				ExitCode: result.ExitCode, Stdout: result.Stdout, Stderr: result.Stderr, DurationMS: result.DurationMS,
				TimedOut: result.TimedOut, OutputTruncated: result.OutputTruncated, Status: status,
			})
		})
	}
	output, runErr := next(toolCtx, input)
	if runErr != nil {
		if _, interrupt := compose.IsInterruptRerunError(runErr); interrupt {
			return nil, runErr
		}
		output = &compose.ToolOutput{Result: "error: " + runErr.Error()}
	}
	if output == nil {
		output = &compose.ToolOutput{}
	}
	if e.toolAudit != nil {
		if err := e.toolAudit.CompleteToolInvocation(
			context.WithoutCancel(ctx), invocationID, output.Result,
			int(time.Since(startedAt).Milliseconds()), runErr,
		); err != nil {
			finishOperationSpan(toolCtx, span, output.Result, err)
			return nil, err
		}
	}
	finishOperationSpan(toolCtx, span, output.Result, runErr)
	state.emitter.send(AgentEvent{Type: EventToolResult, Text: output.Result, Data: input.Name})
	return output, nil
}

func consumeADKEvents(ctx context.Context, iterator *adk.AsyncIterator[*adk.AgentEvent]) (
	string, []*adk.InterruptCtx, error,
) {
	answer := ""
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
			return "", nil, event.Err
		}
		if event.Action != nil && event.Action.Interrupted != nil {
			interrupted = append(interrupted, event.Action.Interrupted.InterruptContexts...)
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}
		message, err := materializeMessage(ctx, event.Output.MessageOutput)
		if err != nil {
			return "", nil, err
		}
		if message != nil && message.Role == schema.Assistant && len(message.ToolCalls) == 0 && message.Content != "" {
			answer = message.Content
		}
	}
	return answer, interrupted, nil
}

func materializeMessage(ctx context.Context, variant *adk.MessageVariant) (*schema.Message, error) {
	if !variant.IsStreaming {
		return variant.Message, nil
	}
	chunks := make([]*schema.Message, 0, 8)
	defer variant.MessageStream.Close()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		chunk, err := variant.MessageStream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, chunk)
	}
	if len(chunks) == 0 {
		return nil, nil
	}
	return schema.ConcatMessages(chunks)
}

func persistApprovalInterrupts(
	ctx context.Context, gate ApprovalGate, conversationID string, checkpoint []byte, contexts []*adk.InterruptCtx,
) error {
	if len(contexts) == 0 {
		return nil
	}
	if len(checkpoint) == 0 {
		return fmt.Errorf("eino did not produce an approval checkpoint")
	}
	for _, interrupt := range contexts {
		if interrupt == nil || !interrupt.IsRootCause {
			continue
		}
		info, ok := approvalInfoFromAny(interrupt.Info)
		if !ok {
			continue
		}
		payload, err := json.Marshal(persistedFrameworkCheckpoint{
			Version: frameworkCheckpointVersion, InterruptID: interrupt.ID, Data: checkpoint,
		})
		if err != nil {
			return err
		}
		if err := gate.SaveCheckpoint(ctx, info.ApprovalID, conversationID, payload); err != nil {
			return fmt.Errorf("save approval checkpoint %s: %w", info.ApprovalID, err)
		}
	}
	return nil
}

func decodeFrameworkCheckpoint(payload []byte) ([]byte, string, error) {
	var stored persistedFrameworkCheckpoint
	if err := json.Unmarshal(payload, &stored); err != nil || stored.Version != frameworkCheckpointVersion || len(stored.Data) == 0 || stored.InterruptID == "" {
		return nil, "", fmt.Errorf("approval checkpoint is incompatible with Eino ADK runtime v%d", frameworkCheckpointVersion)
	}
	return stored.Data, stored.InterruptID, nil
}

func emitApprovalEvents(e *Engine, em emitter, contexts []*adk.InterruptCtx, policies toolPolicies) {
	for _, interrupt := range contexts {
		if interrupt == nil || !interrupt.IsRootCause {
			continue
		}
		info, ok := approvalInfoFromAny(interrupt.Info)
		if !ok {
			continue
		}
		em.send(AgentEvent{Type: EventAwaitApproval, Text: info.ApprovalID, Data: info.ToolName})
		messages, err := a2ui.ApprovalSurfaceWithPresentation(
			info.ApprovalID, info.ConversationID, info.ToolName, info.Arguments,
			policies.approvalUI[info.ToolName],
		)
		if err == nil {
			e.emitA2UI(em, messages)
		}
	}
}

func approvalInfoFromAny(value any) (approvalInterruptInfo, bool) {
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

func policiesTools(policies toolPolicies) []einotool.BaseTool {
	return append([]einotool.BaseTool(nil), policies.tools...)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsSkill(skills []skillrunner.Spec, name string) bool {
	for _, spec := range skills {
		if spec.Name == name {
			return true
		}
	}
	return false
}

func explicitSkillFromMessages(messages []*schema.Message, specs []skillrunner.Spec) (string, bool) {
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index] != nil && messages[index].Role == schema.User {
			return skillrunner.DetectExplicit(messages[index].Content, specs)
		}
	}
	return "", false
}
