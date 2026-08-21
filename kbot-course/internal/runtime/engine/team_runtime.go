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

	runtimeteam "github.com/Q1mi/kbot/internal/runtime/team"
)

// RunSupervisorTeam 使用 Eino ChatModelAgent + AgentTool 运行主管团队。
// 每个成员继续由 Kbot 标准运行时执行，从而沿用固定版本、工具审批、Guard 与审计能力。
func (e *Engine) RunSupervisorTeam(
	ctx context.Context,
	supervisor runtimeteam.Member,
	workers []runtimeteam.Member,
	input string,
	run runtimeteam.MemberRunner,
) (string, []runtimeteam.Step, error) {
	if strings.TrimSpace(supervisor.AgentVersionID) == "" {
		return "", nil, fmt.Errorf("supervisor agent version is required")
	}
	if len(workers) == 0 || run == nil {
		return "", nil, fmt.Errorf("supervisor workers and runner are required")
	}
	snapshot, err := e.platform.GetAgentSnapshotByVersion(ctx, supervisor.AgentVersionID)
	if err != nil {
		return "", nil, fmt.Errorf("get supervisor snapshot: %w", err)
	}
	plan, err := e.executionPlan(ctx, snapshot)
	if err != nil {
		return "", nil, err
	}
	teamInput := input
	if e.guard != nil {
		decision, guardErr := e.guard.Evaluate(ctx, snapshot.WorkspaceID, "on_input", teamInput)
		if guardErr != nil {
			return "", nil, fmt.Errorf("evaluate team input guard: %w", guardErr)
		}
		if !decision.Allowed {
			return "", nil, fmt.Errorf("team input blocked by guard: %s", strings.Join(decision.Reasons, ", "))
		}
		teamInput = decision.SanitizedText
		plan = e.guardExecutionPlan(plan, snapshot.WorkspaceID)
	}
	plan = observeExecutionPlan(plan)

	recorder := &teamStepRecorder{}
	tools := make([]einotool.BaseTool, 0, len(workers))
	usedNames := make(map[string]int, len(workers))
	for _, worker := range workers {
		name := uniqueTeamToolName(worker.Role, usedNames)
		agent := &teamMemberAgent{
			name: name, description: fmt.Sprintf("调用 %s 专家 Agent 处理适合该角色的子任务", worker.Role),
			member: worker, run: run, recorder: recorder,
		}
		tools = append(tools, adk.NewAgentTool(ctx, agent))
	}
	patchHandler, err := patchtoolcalls.New(ctx, nil)
	if err != nil {
		return "", nil, fmt.Errorf("create team tool-call middleware: %w", err)
	}
	instruction := strings.TrimSpace(snapshot.SystemPrompt) + `

你是团队主管。根据用户目标拆分任务，调用最合适的专家工具。可以按需调用多个专家；收到专家结果后进行核对、综合并直接输出最终答案。`
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name: "kbot_team_supervisor", Description: "Kbot team supervisor", Instruction: instruction,
		Model: plan.Model,
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{
			Tools: tools, ExecuteSequentially: true,
		}},
		MaxIterations:       maxTeamSteps(snapshot.MaxSteps),
		Handlers:            []adk.ChatModelAgentMiddleware{patchHandler},
		ModelRetryConfig:    plan.Retry,
		ModelFailoverConfig: plan.Failover,
	})
	if err != nil {
		return "", nil, fmt.Errorf("create team supervisor: %w", err)
	}
	iterator := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, EnableStreaming: true}).Query(ctx, teamInput)
	answer, interrupts, err := consumeADKEvents(iterator)
	if err != nil {
		return "", recorder.snapshot(), err
	}
	if len(interrupts) > 0 {
		return "", recorder.snapshot(), fmt.Errorf("team member execution is awaiting approval")
	}
	if answer == nil || strings.TrimSpace(answer.Content) == "" {
		return "", recorder.snapshot(), fmt.Errorf("team supervisor produced no final answer")
	}
	if e.guard != nil {
		decision, guardErr := e.guard.Evaluate(ctx, snapshot.WorkspaceID, "on_output", answer.Content)
		if guardErr != nil {
			return "", recorder.snapshot(), fmt.Errorf("evaluate team output guard: %w", guardErr)
		}
		if !decision.Allowed {
			return "", recorder.snapshot(), fmt.Errorf("team output blocked by guard: %s", strings.Join(decision.Reasons, ", "))
		}
		answer.Content = decision.SanitizedText
	}
	steps := recorder.snapshot()
	steps = append(steps, runtimeteam.Step{
		Role: supervisor.Role, AgentID: supervisor.AgentID, Input: teamInput, Output: answer.Content,
	})
	return answer.Content, steps, nil
}

type teamMemberAgent struct {
	name, description string
	member            runtimeteam.Member
	run               runtimeteam.MemberRunner
	recorder          *teamStepRecorder
}

func (a *teamMemberAgent) Name(context.Context) string        { return a.name }
func (a *teamMemberAgent) Description(context.Context) string { return a.description }

func (a *teamMemberAgent) Run(
	ctx context.Context, input *adk.AgentInput, _ ...adk.AgentRunOption,
) *adk.AsyncIterator[*adk.AgentEvent] {
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		defer generator.Close()
		request := lastTeamMessage(input.Messages)
		output, err := a.run(ctx, a.member, request)
		if err != nil {
			generator.Send(&adk.AgentEvent{Err: err})
			return
		}
		a.recorder.append(runtimeteam.Step{
			Role: a.member.Role, AgentID: a.member.AgentID, Input: request, Output: output,
		})
		generator.Send(adk.EventFromMessage(schema.AssistantMessage(output, nil), nil, schema.Assistant, ""))
	}()
	return iterator
}

type teamStepRecorder struct {
	mu    sync.Mutex
	steps []runtimeteam.Step
}

func (r *teamStepRecorder) append(step runtimeteam.Step) {
	r.mu.Lock()
	r.steps = append(r.steps, step)
	r.mu.Unlock()
}

func (r *teamStepRecorder) snapshot() []runtimeteam.Step {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]runtimeteam.Step(nil), r.steps...)
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
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index] != nil && strings.TrimSpace(messages[index].Content) != "" {
			return messages[index].Content
		}
	}
	return ""
}

func maxTeamSteps(value int) int {
	if value <= 0 {
		return 8
	}
	return value
}
