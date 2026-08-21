package engine

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/patchtoolcalls"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"go.opentelemetry.io/otel/attribute"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/infrastructure/metrics"
	"github.com/Q1mi/kbot/internal/runtime/guard"
	"github.com/Q1mi/kbot/internal/runtime/llm"
	"github.com/Q1mi/kbot/internal/util"
)

// TeamMember 是 Supervisor 运行时使用的固定成员版本。
type TeamMember struct {
	AgentID        string
	AgentVersionID string
	Role           string
}

// TeamStep 记录 Eino AgentTool 对成员 Agent 的一次调用。
type TeamStep struct {
	Role    string
	AgentID string
	Input   string
	Output  string
}

// RunSupervisorTeam 使用 Eino 推荐的 ChatModelAgent + AgentTool 模式运行主管团队。
// 主管模型可按需调用一个或多个成员，成员仍通过标准 Kbot Runtime 执行并继承版本固定、
// Guard、审计、工具审批与计量能力。
func (e *Engine) RunSupervisorTeam(
	ctx context.Context, supervisor TeamMember, workers []TeamMember, input, workspaceID, userID string,
) (answer string, steps []TeamStep, retErr error) {
	if strings.TrimSpace(input) == "" {
		return "", nil, fmt.Errorf("team input is required")
	}
	if len(workers) == 0 {
		return "", nil, fmt.Errorf("supervisor team has no workers")
	}

	conv, snapshot, err := e.prepareTeamSupervisor(ctx, supervisor, workspaceID, userID)
	if err != nil {
		return "", nil, err
	}
	turns, _ := e.platform.(conversationTurnCoordinator)
	turnToken := ""
	turnFinalized := false
	if turns != nil {
		turnToken, err = turns.ClaimConversationTurn(ctx, conv.ID, false)
		if err != nil {
			return "", nil, fmt.Errorf("claim supervisor conversation turn: %w", err)
		}
	}
	runCtx, cancelRun := context.WithCancel(ctx)
	ctx = runCtx
	turnStop := make(chan struct{})
	turnDone := make(chan error, 1)
	if turns != nil {
		go e.renewConversationTurn(ctx, turns, conv.ID, turnToken, turnStop, turnDone, cancelRun)
	}
	ctx = llm.WithClassification(ctx, conv.Classification)
	ctx = llm.WithInvocationConfig(ctx, llm.InvocationConfig{
		WorkspaceID: conv.WorkspaceID, AgentID: conv.AgentID, UserID: conv.UserID,
		Environment:           conversationEnvironment(conv),
		PromptVersionID:       snapshot.PromptVersionID,
		ModelProfileVersionID: snapshot.ModelProfileVersionID,
		GenerationConfig:      snapshot.GenerationConfig,
		ExperimentID:          snapshot.ExperimentID,
		ExperimentVariant:     snapshot.ExperimentVariant,
	})
	ctx = guard.WithRateKey(ctx, userID)
	ctx = guard.WithWorkspaceKey(ctx, conv.WorkspaceID)
	traceRequest := ChatStreamRequest{
		AgentID: supervisor.AgentID, AgentVersionID: conv.AgentVersionID,
		ConversationID: conv.ID, WorkspaceID: conv.WorkspaceID, Message: input, UserID: userID,
	}
	ctx, runSpan, parentSpan, started := e.startChatTrace(
		ctx, traceRequest, conv.ID, conv.WorkspaceID, conv.AgentID, conv.AgentVersionID,
		snapshot.PromptVersionID, snapshot.ModelProfileVersionID, snapshot.ExperimentVariant,
	)
	defer func() {
		if turns != nil {
			close(turnStop)
			if renewErr := <-turnDone; renewErr != nil && retErr == nil {
				retErr = fmt.Errorf("renew supervisor conversation turn: %w", renewErr)
			}
			if !turnFinalized {
				releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				_ = turns.ReleaseConversationTurn(releaseCtx, conv.ID, turnToken, "active")
				cancel()
			}
		}
		cancelRun()
		finishChatTrace(runSpan, parentSpan, answer, retErr, e.traceOptions.CaptureContent)
	}()
	if recorder, ok := e.platform.(interface {
		RecordConversationTraceID(context.Context, string, string) error
	}); ok {
		if err := recorder.RecordConversationTraceID(ctx, conv.ID, started.TraceID); err != nil {
			return "", nil, fmt.Errorf("record supervisor conversation trace: %w", err)
		}
	}

	userMessage := input
	if e.guard != nil {
		guardCtx, guardSpan := startOperationSpan(ctx, "guard.input",
			attribute.String("guard.hook", "on_input"),
			attribute.String("team.role", supervisor.Role),
		)
		if e.traceOptions.CaptureContent {
			guardSpan.SetAttributes(attribute.String("langfuse.observation.input", userMessage))
		}
		patched, guardErr := e.guard.OnInput(guardCtx, userMessage)
		finishOperationSpan(guardCtx, guardSpan, patched, guardErr)
		if guardErr != nil {
			metrics.GuardBlocks.WithLabelValues("on_input").Inc()
			metrics.InjectionsBlocked.Inc()
			e.recordAudit(ctx, conv.ID, userID, "team_guard_blocked", guardErr.Error())
			return "", nil, guardErr
		}
		userMessage = patched
	}

	plan, err := e.executionPlan(ctx)
	if err != nil {
		return "", nil, err
	}
	recorder := &teamStepRecorder{}
	agentTools := make([]einotool.BaseTool, 0, len(workers))
	usedNames := make(map[string]int, len(workers))
	for _, worker := range workers {
		name := uniqueTeamToolName(worker.Role, usedNames)
		agent := &runtimeMemberAgent{
			name: name, description: fmt.Sprintf("调用 %s 专家 Agent 处理适合该角色的子任务", worker.Role),
			member: worker, engine: e, workspaceID: workspaceID, userID: userID, recorder: recorder,
		}
		agentTools = append(agentTools, adk.NewAgentTool(ctx, agent))
	}

	patchHandler, err := patchtoolcalls.New(ctx, nil)
	if err != nil {
		return "", nil, fmt.Errorf("create team tool-call middleware: %w", err)
	}
	policyState := &agentRunState{engine: e, workspaceID: workspaceID, conversationID: conv.ID}
	instruction := strings.TrimSpace(snapshot.SystemPrompt) + `

你是团队主管。根据用户目标拆分任务，调用最合适的专家工具。可以按需调用多个专家；收到专家结果后进行核对、综合并直接输出最终答案。`
	coordinator, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name: "kbot_team_supervisor", Description: "Kbot team supervisor", Instruction: instruction,
		Model: plan.Model,
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{
			Tools:               agentTools,
			ExecuteSequentially: true,
		}},
		MaxIterations:       maxStepsOf(snapshot),
		Handlers:            []adk.ChatModelAgentMiddleware{patchHandler, &runtimePolicyHandler{state: policyState}},
		ModelRetryConfig:    plan.Retry,
		ModelFailoverConfig: plan.Failover,
	})
	if err != nil {
		return "", nil, fmt.Errorf("create team supervisor: %w", err)
	}

	iterator := adk.NewRunner(ctx, adk.RunnerConfig{Agent: coordinator, EnableStreaming: true}).Query(ctx, userMessage)
	answer, interrupts, err := consumeADKEvents(ctx, iterator)
	if err != nil {
		var approvalErr *AwaitingApprovalError
		if errors.As(err, &approvalErr) {
			e.recordAudit(ctx, conv.ID, userID, "team_await_approval", err.Error())
		} else {
			e.recordAudit(ctx, conv.ID, userID, "team_run_failed", err.Error())
		}
		return "", recorder.snapshot(), err
	}
	if len(interrupts) > 0 {
		err = fmt.Errorf("team execution is awaiting approval")
		e.recordAudit(ctx, conv.ID, userID, "team_await_approval", err.Error())
		return "", recorder.snapshot(), err
	}
	if e.guard != nil {
		guardCtx, guardSpan := startOperationSpan(ctx, "guard.output",
			attribute.String("guard.hook", "on_output"),
			attribute.String("team.role", supervisor.Role),
		)
		if e.traceOptions.CaptureContent {
			guardSpan.SetAttributes(attribute.String("langfuse.observation.input", answer))
		}
		patched, guardErr := e.guard.OnOutput(guardCtx, answer)
		finishOperationSpan(guardCtx, guardSpan, patched, guardErr)
		if guardErr != nil {
			metrics.GuardBlocks.WithLabelValues("on_output").Inc()
			e.recordAudit(ctx, conv.ID, userID, "team_guard_blocked", guardErr.Error())
			return "", recorder.snapshot(), guardErr
		}
		answer = patched
	}
	if err := e.commitConversationTurn(ctx, turns, conv.ID, turnToken, []*domain.Message{
		{ID: util.GenerateID(), ConversationID: conv.ID, Role: "user", Content: userMessage},
		{ID: util.GenerateID(), ConversationID: conv.ID, Role: "assistant", Content: answer},
	}, "active"); err != nil {
		return "", recorder.snapshot(), fmt.Errorf("save supervisor conversation turn: %w", err)
	}
	turnFinalized = true
	e.recordAudit(ctx, conv.ID, userID, "team_turn", answer)
	steps = recorder.snapshot()
	steps = append(steps, TeamStep{Role: supervisor.Role, AgentID: supervisor.AgentID, Input: userMessage, Output: answer})
	return answer, steps, nil
}

func (e *Engine) prepareTeamSupervisor(
	ctx context.Context, supervisor TeamMember, workspaceID, userID string,
) (*domain.Conversation, *AgentSnapshot, error) {
	var (
		conv *domain.Conversation
		err  error
	)
	if supervisor.AgentVersionID != "" {
		conv, err = e.platform.CreateConversationWithVersion(ctx, supervisor.AgentVersionID, userID)
	} else {
		conv, err = e.platform.CreateConversation(ctx, supervisor.AgentID, userID)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("resolve supervisor: %w", err)
	}
	if workspaceID != "" && conv.WorkspaceID != workspaceID {
		return nil, nil, fmt.Errorf("supervisor agent not found")
	}
	snapshot, err := e.platform.GetAgentSnapshotByVersion(ctx, conv.AgentVersionID)
	if err != nil {
		return nil, nil, fmt.Errorf("get supervisor snapshot: %w", err)
	}
	if err := applyConversationRuntimeConfig(snapshot, conv.RuntimeConfigJSON); err != nil {
		return nil, nil, err
	}
	return conv, snapshot, nil
}

type runtimeMemberAgent struct {
	name, description   string
	member              TeamMember
	engine              *Engine
	workspaceID, userID string
	recorder            *teamStepRecorder
}

func (a *runtimeMemberAgent) Name(context.Context) string        { return a.name }
func (a *runtimeMemberAgent) Description(context.Context) string { return a.description }

func (a *runtimeMemberAgent) Run(
	ctx context.Context, input *adk.AgentInput, _ ...adk.AgentRunOption,
) *adk.AsyncIterator[*adk.AgentEvent] {
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		defer generator.Close()
		request := lastTeamMessage(input.Messages)
		output, err := a.engine.Chat(ctx, ChatStreamRequest{
			AgentID: a.member.AgentID, AgentVersionID: a.member.AgentVersionID,
			WorkspaceID: a.workspaceID, Message: request, UserID: a.userID,
		})
		if err != nil {
			generator.Send(&adk.AgentEvent{Err: err})
			return
		}
		a.recorder.append(TeamStep{Role: a.member.Role, AgentID: a.member.AgentID, Input: request, Output: output})
		generator.Send(adk.EventFromMessage(schema.AssistantMessage(output, nil), nil, schema.Assistant, ""))
	}()
	return iterator
}

type teamStepRecorder struct {
	mu    sync.Mutex
	steps []TeamStep
}

func (r *teamStepRecorder) append(step TeamStep) {
	r.mu.Lock()
	r.steps = append(r.steps, step)
	r.mu.Unlock()
}

func (r *teamStepRecorder) snapshot() []TeamStep {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]TeamStep(nil), r.steps...)
}

var invalidTeamToolChar = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

func uniqueTeamToolName(role string, used map[string]int) string {
	base := strings.Trim(invalidTeamToolChar.ReplaceAllString(strings.ToLower(strings.TrimSpace(role)), "_"), "_")
	if base == "" {
		base = "worker"
	}
	used[base]++
	if used[base] == 1 {
		return base
	}
	return fmt.Sprintf("%s_%d", base, used[base])
}

func lastTeamMessage(messages []*schema.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i] != nil && strings.TrimSpace(messages[i].Content) != "" {
			return messages[i].Content
		}
	}
	return ""
}
