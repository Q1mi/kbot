package engine

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/patchtoolcalls"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/runtime/guard"
	"github.com/Q1mi/kbot/internal/runtime/llm"
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
) (string, []TeamStep, error) {
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
	ctx = guard.WithWorkspaceKey(ctx, workspaceID)

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

	iterator := adk.NewRunner(ctx, adk.RunnerConfig{Agent: coordinator, EnableStreaming: true}).Query(ctx, input)
	answer, interrupts, err := consumeADKEvents(ctx, iterator)
	if err != nil {
		return "", recorder.snapshot(), err
	}
	if len(interrupts) > 0 {
		return "", recorder.snapshot(), fmt.Errorf("team member execution is awaiting approval; run the member conversation directly to approve it")
	}
	steps := recorder.snapshot()
	steps = append(steps, TeamStep{Role: supervisor.Role, AgentID: supervisor.AgentID, Input: input, Output: answer})
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
