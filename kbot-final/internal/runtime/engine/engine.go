// Package engine 实现带工具调用、Skill 激活、审批续跑与配置快照的 ReAct 运行时。
package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"
	"go.opentelemetry.io/otel/attribute"

	"github.com/Q1mi/kbot/internal/a2ui"
	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/infrastructure/metrics"
	"github.com/Q1mi/kbot/internal/runtime/guard"
	"github.com/Q1mi/kbot/internal/runtime/llm"
	"github.com/Q1mi/kbot/internal/runtime/skillrunner"
	"github.com/Q1mi/kbot/internal/runtime/tooling"
	"github.com/Q1mi/kbot/internal/util"
)

// Generator 抽象"给定消息+工具产出一条助手消息"的能力。*llm.Gateway 满足它；
// 抽成接口便于在不依赖真实 LLM 的情况下单测 ReAct + Skill 循环。
type Generator interface {
	Generate(ctx context.Context, messages []*schema.Message, tools []*schema.ToolInfo) (*schema.Message, error)
}

// Engine Runtime引擎
type Engine struct {
	platform     Platform
	gen          Generator
	tools        *tooling.Registry
	guard        Guarder
	audit        AuditRecorder
	toolAudit    ToolAuditRecorder
	approvals    ApprovalGate
	traceOptions TraceOptions
}

// Platform 平台服务接口
type Platform interface {
	// CreateConversation 新建会话并 pin 当前 env 指向的 AgentVersion（老对话不切版本）。
	CreateConversation(ctx context.Context, agentID, userID string) (*domain.Conversation, error)
	// CreateConversationWithVersion 新建会话并 pin 到指定 AgentVersion（Team 跑成员时按团队快照 pin 的版本）。
	CreateConversationWithVersion(ctx context.Context, agentVersionID, userID string) (*domain.Conversation, error)
	LoadConversation(ctx context.Context, conversationID string) (*domain.Conversation, error)
	// GetAgentSnapshotByVersion 按 pinned 的版本号解析配置快照。
	GetAgentSnapshotByVersion(ctx context.Context, agentVersionID string) (*AgentSnapshot, error)
	LoadConversationMessages(ctx context.Context, conversationID string) ([]*domain.Message, error)
	AppendMessage(ctx context.Context, conversationID string, role, content string) error
}

type conversationTurnCoordinator interface {
	ClaimConversationTurn(context.Context, string, bool) (string, error)
	RenewConversationTurn(context.Context, string, string) error
	CommitConversationTurn(context.Context, string, string, []*domain.Message, string) error
	ReleaseConversationTurn(context.Context, string, string, string) error
}

// AgentSnapshot 是 Runtime 视角的、已解析好的 Agent 配置快照。
type AgentSnapshot struct {
	ID                    string // = AgentVersionID
	AgentID               string
	WorkspaceID           string
	SystemPrompt          string // 已解析（Prompt 中心渲染或字面量）
	PromptVersionID       string
	ModelProfileVersionID string
	GenerationConfig      domain.GenerationConfig
	ExperimentID          string
	ExperimentVariant     string
	ToolVersionIDs        []string           // 挂载工具的【版本 ID】(快照创建时 pin 死,老对话不随工具发新版而变)
	Skills                []skillrunner.Spec // 订阅的技能（用于 L1/L2）
	KBIDs                 []string           // Agent 允许访问的知识库 ID
	AllowNetwork          bool               // 是否允许 REST/MCP/A2A 等出网工具
	MaxSteps              int                // ReAct 步数预算
}

// Guarder 是 Guard 的入站/出站文本钩子（注入检测 / PII 脱敏）。*guard.Guard 满足它。
type Guarder interface {
	OnInput(ctx context.Context, text string) (string, error)
	OnOutput(ctx context.Context, text string) (string, error)
}

// AuditRecorder 记录对话轨迹（*audit.Service 满足它）。
type AuditRecorder interface {
	RecordConversation(ctx context.Context, conversationID, actor, action, detail string)
	RecordSkillTrigger(ctx context.Context, workspaceID, conversationID, skillVersionID, skillName, source string)
}

type ToolAuditRecorder interface {
	BeginToolInvocation(ctx context.Context, workspaceID, conversationID, toolCallID, toolVersionID, args string) (string, error)
	CompleteToolInvocation(ctx context.Context, invocationID, result string, latencyMS int, runErr error) error
	RecordSandboxExecution(ctx context.Context, execution *domain.SandboxExecution) error
}

// NewEngine 创建Runtime引擎。tools 可为 nil（无工具时退化为纯对话）。
func NewEngine(platform Platform, llmGW *llm.Gateway, tools *tooling.Registry) *Engine {
	return &Engine{platform: platform, gen: llmGW, tools: tools}
}

// NewEngineWithGenerator 用自定义 Generator 构造引擎（多 Agent member runner / 测试用）。
func NewEngineWithGenerator(platform Platform, gen Generator, tools *tooling.Registry) *Engine {
	return &Engine{platform: platform, gen: gen, tools: tools}
}

// Chat 同步跑一次对话，收敛流式事件为最终文本（Team member runner / eval target / kbotctl 用）。
func (e *Engine) Chat(ctx context.Context, req ChatStreamRequest) (string, error) {
	ch, err := e.ChatStream(ctx, req)
	if err != nil {
		return "", err
	}
	var answer string
	for ev := range ch {
		switch ev.Type {
		case EventAnswerDelta:
			answer += ev.Text
		case EventError:
			return "", fmt.Errorf("%s", ev.Text)
		}
	}
	return answer, nil
}

// WithGuard 挂上 Guard。
func (e *Engine) WithGuard(g Guarder) *Engine { e.guard = g; return e }

// WithAudit 挂上 Audit。
func (e *Engine) WithAudit(a AuditRecorder) *Engine { e.audit = a; return e }

// WithToolAudit 在 Tool 副作用发生前后写入持久化调用账本。
func (e *Engine) WithToolAudit(a ToolAuditRecorder) *Engine { e.toolAudit = a; return e }

// AgentEvent Agent事件类型
type AgentEvent struct {
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
	Text string `json:"text,omitempty"`
}

// RunFinished 明确区分一次流结束时业务已经完成，或因人工审批而暂停。
type RunFinished struct {
	Status string `json:"status"`
}

const (
	RunStatusCompleted        = "completed"
	RunStatusAwaitingApproval = "awaiting_approval"
)

// 事件类型常量
const (
	EventStarted      = "started"
	EventToolCall     = "tool_call"
	EventToolResult   = "tool_result"
	EventSkillTrigger = "skill_trigger"
	EventA2UI         = "a2ui"
	EventAnswerDelta  = "answer_delta"
	EventAnswerDone   = "answer_done"
	EventError        = "error"
	EventDone         = "done"
)

// ChatStreamRequest 流式聊天请求
type ChatStreamRequest struct {
	AgentID             string         `json:"agent_id"`
	WorkspaceID         string         `json:"workspace_id,omitempty"`
	AgentVersionID      string         `json:"agent_version_id"` // 新会话 pin 到此版本(Team 成员用);空则 pin 当前版本
	AgentEnv            string         `json:"agent_env"`        // dev/staging/prod；空时使用 dev
	ConversationID      string         `json:"conversation_id"`
	Message             string         `json:"message"`
	UserID              string         `json:"user_id"`
	UserPromptVersionID string         `json:"user_prompt_version_id,omitempty"`
	UserPromptVariables map[string]any `json:"user_prompt_variables,omitempty"`
}

// emitA2UI 在协议消息通过服务端白名单校验后逐条发送给客户端。
func (e *Engine) emitA2UI(em emitter, messages []a2ui.Message) bool {
	for _, message := range messages {
		if err := a2ui.ValidateMessage(message); err != nil {
			return false
		}
		if !em.send(AgentEvent{Type: EventA2UI, Data: message}) {
			return false
		}
	}
	return true
}

const defaultMaxSteps = 6

// ChatStream 启动工具循环与 Skill L1/L2 激活的流式对话。
func (e *Engine) ChatStream(ctx context.Context, req ChatStreamRequest) (<-chan AgentEvent, error) {
	if strings.TrimSpace(req.Message) == "" && req.UserPromptVariables == nil {
		return nil, fmt.Errorf("message or user_prompt_variables is required")
	}
	// 取/建会话；新会话 pin 当前 AgentVersion。
	var conv *domain.Conversation
	var err error
	if req.ConversationID != "" {
		conv, err = e.platform.LoadConversation(ctx, req.ConversationID)
	} else if req.AgentVersionID != "" {
		// Team 成员:pin 到团队快照里钉死的成员 agent 版本(而非该成员当前版本)。
		conv, err = e.platform.CreateConversationWithVersion(ctx, req.AgentVersionID, req.UserID)
	} else if req.AgentEnv != "" && req.AgentEnv != "dev" {
		platformWithEnv, ok := e.platform.(interface {
			CreateConversationInEnv(context.Context, string, string, string) (*domain.Conversation, error)
		})
		if !ok {
			return nil, fmt.Errorf("platform does not support agent environments")
		}
		conv, err = platformWithEnv.CreateConversationInEnv(ctx, req.AgentID, req.AgentEnv, req.UserID)
	} else {
		conv, err = e.platform.CreateConversation(ctx, req.AgentID, req.UserID)
	}
	if err != nil {
		return nil, fmt.Errorf("resolve conversation: %w", err)
	}
	if conv.UserID != req.UserID {
		return nil, fmt.Errorf("conversation not found")
	}
	if req.WorkspaceID != "" && conv.WorkspaceID != req.WorkspaceID {
		return nil, fmt.Errorf("conversation not found")
	}
	if req.AgentID != "" && conv.AgentID != req.AgentID {
		return nil, fmt.Errorf("conversation not found")
	}

	// 用会话上 pin 的固定版本解析快照，运行中不跟随 env 指针变化。
	snapshot, err := e.platform.GetAgentSnapshotByVersion(ctx, conv.AgentVersionID)
	if err != nil {
		return nil, fmt.Errorf("get agent snapshot: %w", err)
	}
	if req.WorkspaceID != "" && snapshot.WorkspaceID != req.WorkspaceID {
		return nil, fmt.Errorf("agent not found")
	}
	if err := applyConversationRuntimeConfig(snapshot, conv.RuntimeConfigJSON); err != nil {
		return nil, err
	}
	turns, _ := e.platform.(conversationTurnCoordinator)
	turnToken := ""
	if turns != nil {
		turnToken, err = turns.ClaimConversationTurn(ctx, conv.ID, false)
		if err != nil {
			return nil, fmt.Errorf("claim conversation turn: %w", err)
		}
	}
	releaseClaim := func() {
		if turns == nil || turnToken == "" {
			return
		}
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = turns.ReleaseConversationTurn(releaseCtx, conv.ID, turnToken, "active")
	}

	allInfos, execByName, policies, err := e.buildTools(ctx, snapshot.ToolVersionIDs, snapshot.KBIDs)
	if err != nil {
		releaseClaim()
		return nil, fmt.Errorf("build tools: %w", err)
	}

	history, err := e.platform.LoadConversationMessages(ctx, conv.ID)
	if err != nil {
		releaseClaim()
		return nil, fmt.Errorf("load conversation messages: %w", err)
	}
	if req.UserPromptVariables != nil {
		if req.ConversationID != "" || len(history) > 0 {
			releaseClaim()
			return nil, fmt.Errorf("user prompt template variables are only allowed on the first conversation turn")
		}
		preparer, ok := e.platform.(interface {
			PrepareUserMessage(context.Context, string, string, string, string, string, map[string]any) (string, string, error)
		})
		if !ok {
			releaseClaim()
			return nil, fmt.Errorf("platform does not support user prompt templates")
		}
		rendered, versionID, err := preparer.PrepareUserMessage(
			ctx, conv.ID, conv.AgentVersionID, req.UserID, req.Message, req.UserPromptVersionID, req.UserPromptVariables,
		)
		if err != nil {
			releaseClaim()
			return nil, err
		}
		if strings.TrimSpace(rendered) == "" {
			releaseClaim()
			return nil, fmt.Errorf("rendered user prompt is empty")
		}
		req.Message = rendered
		req.UserPromptVersionID = versionID
	}

	// 会话数据分级驱动 LLM Provider 路由。
	ctx = llm.WithClassification(ctx, conv.Classification)
	ctx = llm.WithInvocationConfig(ctx, llm.InvocationConfig{
		WorkspaceID: conv.WorkspaceID, AgentID: conv.AgentID, UserID: conv.UserID,
		Environment:           conversationEnvironment(conv),
		PromptVersionID:       snapshot.PromptVersionID,
		ModelProfileVersionID: snapshot.ModelProfileVersionID,
		GenerationConfig:      snapshot.GenerationConfig, ExperimentID: snapshot.ExperimentID,
		ExperimentVariant: snapshot.ExperimentVariant,
	})
	// Guard 使用用户维度的限流键。
	ctx = guard.WithRateKey(ctx, req.UserID)
	ctx = guard.WithWorkspaceKey(ctx, conv.WorkspaceID)
	ctx, runSpan, parentSpan, started := e.startChatTrace(
		ctx, req, conv.ID, conv.WorkspaceID, conv.AgentID, conv.AgentVersionID,
		snapshot.PromptVersionID, snapshot.ModelProfileVersionID, snapshot.ExperimentVariant,
	)
	if recorder, ok := e.platform.(interface {
		RecordConversationTraceID(context.Context, string, string) error
	}); ok {
		if err := recorder.RecordConversationTraceID(ctx, conv.ID, started.TraceID); err != nil {
			finishChatTrace(runSpan, parentSpan, "", err, e.traceOptions.CaptureContent)
			releaseClaim()
			return nil, fmt.Errorf("record conversation trace: %w", err)
		}
	}

	eventCh := make(chan AgentEvent, 16)
	go func() {
		defer close(eventCh)
		turnFinalized := false
		releaseStatus := "active"
		turnStop := make(chan struct{})
		turnDone := make(chan error, 1)
		runCtx, cancelRun := context.WithCancel(ctx)
		if turns != nil {
			go e.renewConversationTurn(runCtx, turns, conv.ID, turnToken, turnStop, turnDone, cancelRun)
		}
		defer func() {
			if turns != nil {
				close(turnStop)
				<-turnDone
				if !turnFinalized {
					releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(runCtx), 5*time.Second)
					_ = turns.ReleaseConversationTurn(releaseCtx, conv.ID, turnToken, releaseStatus)
					cancel()
				}
			}
			cancelRun()
		}()
		ctx := runCtx
		var traceOutput string
		var traceErr error
		defer func() {
			finishChatTrace(runSpan, parentSpan, traceOutput, traceErr, e.traceOptions.CaptureContent)
		}()
		// em.send 永远尊重 ctx 取消:客户端断开后(SSE/WS 请求 ctx cancel),后台 goroutine
		// 满缓冲 channel 会及时退出，避免每个中途断开的对话泄漏一个 goroutine。
		em := emitter{ctx: ctx, ch: eventCh}
		if !em.send(AgentEvent{Type: EventStarted, Data: started}) {
			traceErr = ctx.Err()
			return
		}
		metrics.ChatRequests.Inc()

		// Guard OnInput：注入检测 + 入站 PII 脱敏（讲义 §15.4）。
		userMsg := req.Message
		if e.guard != nil {
			guardCtx, guardSpan := startOperationSpan(ctx, "guard.input",
				attribute.String("guard.hook", "on_input"),
			)
			if e.traceOptions.CaptureContent {
				guardSpan.SetAttributes(attribute.String("langfuse.observation.input", userMsg))
			}
			patched, gErr := e.guard.OnInput(guardCtx, userMsg)
			finishOperationSpan(guardCtx, guardSpan, patched, gErr)
			if gErr != nil {
				traceErr = gErr
				metrics.GuardBlocks.WithLabelValues("on_input").Inc()
				metrics.InjectionsBlocked.Inc()
				e.recordAudit(ctx, conv.ID, req.UserID, "guard_blocked", gErr.Error())
				em.send(AgentEvent{Type: EventError, Text: gErr.Error()})
				return
			}
			userMsg = patched
		}

		messages := buildMessages(snapshot, history, userMsg)
		ls := &loopState{
			convID: conv.ID, workspaceID: conv.WorkspaceID, messages: messages, allInfos: allInfos, execByName: execByName,
			policies: policies, skills: snapshot.Skills, agentKBs: stringSet(snapshot.KBIDs),
			allowNetwork: snapshot.AllowNetwork, maxSteps: maxStepsOf(snapshot),
		}
		// disable-model-invocation 的 Skill 只接受用户显式标签触发。
		if name, ok := skillrunner.DetectExplicit(userMsg, snapshot.Skills); ok {
			e.activateSkill(ctx, ls, name, false, em)
		}
		answer, runErr := e.runLoop(ctx, ls, em)
		if errors.Is(runErr, errAwaitApproval) {
			releaseStatus = "awaiting_approval"
			if err := e.commitConversationTurn(ctx, turns, conv.ID, turnToken, []*domain.Message{{
				ID: util.GenerateID(), ConversationID: conv.ID, Role: "user", Content: userMsg,
			}}, "awaiting_approval"); err != nil {
				traceErr = err
				em.send(AgentEvent{Type: EventError, Text: fmt.Sprintf("save user message: %v", err)})
				return
			}
			turnFinalized = true
			e.recordAudit(ctx, conv.ID, req.UserID, "await_approval", "")
			em.send(AgentEvent{Type: EventDone, Data: RunFinished{Status: RunStatusAwaitingApproval}})
			traceOutput = "awaiting approval"
			return
		}
		if runErr != nil {
			traceErr = runErr
			em.send(AgentEvent{Type: EventError, Text: runErr.Error()})
			return
		}

		// Guard OnOutput：出站 PII 脱敏 / 出域检查（在 emit 给客户端之前）。
		if e.guard != nil {
			guardCtx, guardSpan := startOperationSpan(ctx, "guard.output",
				attribute.String("guard.hook", "on_output"),
			)
			if e.traceOptions.CaptureContent {
				guardSpan.SetAttributes(attribute.String("langfuse.observation.input", answer))
			}
			patched, gErr := e.guard.OnOutput(guardCtx, answer)
			finishOperationSpan(guardCtx, guardSpan, patched, gErr)
			if gErr != nil {
				traceErr = gErr
				metrics.GuardBlocks.WithLabelValues("on_output").Inc()
				e.recordAudit(ctx, conv.ID, req.UserID, "guard_blocked", gErr.Error())
				em.send(AgentEvent{Type: EventError, Text: gErr.Error()})
				return
			}
			answer = patched
		}

		if err := e.commitConversationTurn(ctx, turns, conv.ID, turnToken, []*domain.Message{
			{ID: util.GenerateID(), ConversationID: conv.ID, Role: "user", Content: userMsg},
			{ID: util.GenerateID(), ConversationID: conv.ID, Role: "assistant", Content: answer},
		}, "active"); err != nil {
			traceErr = err
			em.send(AgentEvent{Type: EventError, Text: fmt.Sprintf("save conversation turn: %v", err)})
			return
		}
		turnFinalized = true
		if answer != "" {
			if !em.send(AgentEvent{Type: EventAnswerDelta, Text: answer}) {
				traceErr = ctx.Err()
				return
			}
		}
		if !em.send(AgentEvent{Type: EventAnswerDone}) {
			traceErr = ctx.Err()
			return
		}
		// Audit：留对话轨迹（飞行记录仪，§15.6）。
		e.recordAudit(ctx, conv.ID, req.UserID, "chat_turn", answer)
		traceOutput = answer
		em.send(AgentEvent{Type: EventDone, Data: RunFinished{Status: RunStatusCompleted}})
	}()

	return eventCh, nil
}

func (e *Engine) commitConversationTurn(
	ctx context.Context, turns conversationTurnCoordinator, conversationID, token string,
	messages []*domain.Message, nextStatus string,
) error {
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if turns != nil {
		return turns.CommitConversationTurn(commitCtx, conversationID, token, messages, nextStatus)
	}
	for _, message := range messages {
		if err := e.platform.AppendMessage(commitCtx, conversationID, message.Role, message.Content); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) renewConversationTurn(
	ctx context.Context, turns conversationTurnCoordinator, conversationID, token string,
	stop <-chan struct{}, done chan<- error, cancel context.CancelFunc,
) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			done <- nil
			return
		case <-ctx.Done():
			done <- nil
			return
		case <-ticker.C:
			if err := turns.RenewConversationTurn(context.WithoutCancel(ctx), conversationID, token); err != nil {
				cancel()
				done <- err
				return
			}
		}
	}
}

func conversationEnvironment(conversation *domain.Conversation) string {
	if conversation != nil && conversation.RuntimeConfigJSON != "" {
		var config domain.ConversationRuntimeConfig
		if json.Unmarshal([]byte(conversation.RuntimeConfigJSON), &config) == nil && config.Environment != "" {
			return config.Environment
		}
	}
	return "dev"
}

func applyConversationRuntimeConfig(snapshot *AgentSnapshot, raw string) error {
	if raw == "" {
		return nil
	}
	var runtimeConfig domain.ConversationRuntimeConfig
	if err := json.Unmarshal([]byte(raw), &runtimeConfig); err != nil {
		return fmt.Errorf("decode conversation runtime config: %w", err)
	}
	if runtimeConfig.SystemPrompt != "" {
		snapshot.SystemPrompt = runtimeConfig.SystemPrompt
	}
	snapshot.PromptVersionID = runtimeConfig.PromptVersionID
	snapshot.ModelProfileVersionID = runtimeConfig.ModelProfileVersionID
	snapshot.GenerationConfig = runtimeConfig.GenerationConfig
	snapshot.ExperimentID = runtimeConfig.ExperimentID
	snapshot.ExperimentVariant = runtimeConfig.ExperimentVariant
	return nil
}

// emitter 把 Agent 事件写进 channel,但 select 上 ctx.Done() 兜底:
// 一旦请求 ctx 被取消(客户端断开),send 立刻返回 false 而非阻塞,调用方据此停工退出。
type emitter struct {
	ctx context.Context
	ch  chan<- AgentEvent
}

// send 投递一个事件;返回 false 表示 ctx 已取消(应停止后续工作并退出 goroutine)。
func (em emitter) send(ev AgentEvent) bool {
	select {
	case em.ch <- ev:
		return true
	case <-em.ctx.Done():
		return false
	}
}

func (e *Engine) recordAudit(ctx context.Context, convID, actor, action, detail string) {
	if e.audit != nil {
		e.audit.RecordConversation(ctx, convID, actor, action, detail)
	}
}

// guardLLMCall / guardToolCalls 通过可选接口执行限流和配额检查。
func (e *Engine) guardLLMCall(ctx context.Context) error {
	if g, ok := e.guard.(interface {
		OnLLMCall(context.Context) error
	}); ok {
		return g.OnLLMCall(ctx)
	}
	return nil
}

func (e *Engine) guardToolCalls(ctx context.Context, resp *schema.Message) error {
	g, ok := e.guard.(interface {
		OnToolCall(context.Context, string) error
	})
	if !ok {
		return nil
	}
	for _, tc := range resp.ToolCalls {
		if err := g.OnToolCall(ctx, tc.Function.Name); err != nil {
			return err
		}
	}
	return nil
}

// loopState 是 ReAct 循环的可变状态。
type loopState struct {
	convID       string
	workspaceID  string
	messages     []*schema.Message
	allInfos     []*schema.ToolInfo
	execByName   map[string]tooling.Executor
	policies     toolPolicies
	skills       []skillrunner.Spec
	activeSkill  *skillrunner.Spec
	activeInfos  []*schema.ToolInfo // nil 表示全量工具；非 nil 表示被 Skill 限定的范围
	agentKBs     map[string]bool
	activeKBs    map[string]bool
	allowNetwork bool
	skillsOff    bool // 白名单回退后不再尝试 Skill 注入
	skillFails   int
	maxSteps     int
}

type toolPolicies struct {
	sensitive       map[string]bool
	requiresNetwork map[string]bool
	kbScoped        map[string]bool
	approvalUI      map[string]a2ui.ApprovalPresentation
	versionIDs      map[string]string
}

// runLoop 跑 ReAct + Skill 循环。
func (e *Engine) runLoop(ctx context.Context, ls *loopState, em emitter) (string, error) {
	steps := ls.maxSteps
	if steps <= 0 {
		steps = defaultMaxSteps
	}
	for step := 0; step < steps; step++ {
		// 客户端断开 / 超时即止,别再白白调模型与工具。
		if err := ctx.Err(); err != nil {
			return "", err
		}

		infos := ls.allInfos
		if ls.activeInfos != nil {
			infos = ls.activeInfos
		}

		// 调用模型前执行限流与配额检查。
		if err := e.guardLLMCall(ctx); err != nil {
			return "", err
		}
		if err := validateToolCallResponses(ls.messages); err != nil {
			return "", fmt.Errorf("invalid model message sequence: %w", err)
		}
		resp, err := e.gen.Generate(ctx, ls.messages, infos)
		if err != nil {
			return "", err
		}
		ls.messages = append(ls.messages, resp)

		// 1) 有工具调用：先过限流 + 审批门(敏感工具暂停),再执行回喂。
		if len(resp.ToolCalls) > 0 {
			if e.rejectUnauthorizedToolCalls(ls, resp, em) {
				continue
			}
			if err := e.guardToolCalls(ctx, resp); err != nil {
				return "", err
			}
			paused, err := e.pauseIfSensitive(ctx, ls, resp, em)
			if err != nil {
				return "", err
			}
			if paused {
				return "", errAwaitApproval
			}
			if err := e.executeToolBatch(ctx, ls, resp, em); err != nil {
				return "", err
			}
			continue
		}

		// 2) 无工具调用：检查是否触发了 Skill。
		if !ls.skillsOff && len(ls.skills) > 0 {
			if name, ok := skillrunner.Detect(resp.Content); ok {
				if handled := e.activateSkill(ctx, ls, name, true, em); handled {
					continue
				}
			}
		}

		// 3) 最终回答。延后到 OnOutput 脱敏之后再 emit（见 ChatStream）。
		return resp.Content, nil
	}
	return "", fmt.Errorf("reached max steps (%d) without final answer", steps)
}

// executeToolBatch 执行一批工具调用并把结果回喂 messages(不做审批暂停;暂停由 pauseIfSensitive 负责)。
// 返回非 nil 错误表示 ctx 已取消(客户端断开/超时),调用方应停止续跑。
func (e *Engine) executeToolBatch(ctx context.Context, ls *loopState, resp *schema.Message, em emitter) error {
	for _, tc := range resp.ToolCalls {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := tc.Function.Name
		metrics.ToolCalls.WithLabelValues(name).Inc()
		if !em.send(AgentEvent{Type: EventToolCall, Text: name, Data: json.RawMessage(tc.Function.Arguments)}) {
			return em.ctx.Err()
		}

		toolCtx, toolSpan := startOperationSpan(ctx, "tool.execute",
			attribute.String("gen_ai.tool.name", name),
			attribute.String("tool.call.id", tc.ID),
		)
		if e.traceOptions.CaptureContent {
			toolSpan.SetAttributes(attribute.String("langfuse.observation.input", tc.Function.Arguments))
		}
		result := ""
		var toolErr error
		invocationID := ""
		startedAt := time.Now()
		if e.toolAudit != nil {
			var auditErr error
			invocationID, auditErr = e.toolAudit.BeginToolInvocation(
				context.WithoutCancel(ctx), ls.workspaceID, ls.convID, tc.ID,
				ls.policies.versionIDs[name], tc.Function.Arguments,
			)
			if auditErr != nil {
				finishOperationSpan(toolCtx, toolSpan, "", auditErr)
				return auditErr
			}
			toolCtx = tooling.WithSandboxObserver(toolCtx, func(sandboxResult tooling.SandboxResult, runErr error) error {
				status := "success"
				if sandboxResult.TimedOut {
					status = "timeout"
				} else if runErr != nil {
					status = "error"
				}
				return e.toolAudit.RecordSandboxExecution(context.WithoutCancel(ctx), &domain.SandboxExecution{
					WorkspaceID: ls.workspaceID, ConversationID: ls.convID, InvocationID: invocationID,
					ToolVersionID: ls.policies.versionIDs[name], ToolCallID: tc.ID,
					ExecutionID: sandboxResult.ExecutionID, Language: sandboxResult.Language,
					ContainerID: sandboxResult.ContainerName, ExitCode: sandboxResult.ExitCode,
					Stdout: sandboxResult.Stdout, Stderr: sandboxResult.Stderr, DurationMS: sandboxResult.DurationMS,
					TimedOut: sandboxResult.TimedOut, OutputTruncated: sandboxResult.OutputTruncated, Status: status,
				})
			})
		}
		exec, ok := ls.execByName[name]
		if !ok {
			result = fmt.Sprintf("error: tool %q not available", name)
			toolErr = fmt.Errorf("tool %q not available", name)
		} else if policyErr := ls.toolPolicyError(name, tc.Function.Arguments); policyErr != nil {
			// Skill 激活期间，超出 allowed-tools 的工具被拒（纵深防御，讲义 §14.5）。
			result = "error: " + policyErr.Error()
			toolErr = policyErr
		} else {
			out, execErr := exec.Execute(toolCtx, json.RawMessage(tc.Function.Arguments))
			if execErr != nil {
				result = "error: " + execErr.Error()
				toolErr = execErr
			} else {
				result = out
			}
		}
		if e.toolAudit != nil {
			if auditErr := e.toolAudit.CompleteToolInvocation(
				context.WithoutCancel(ctx), invocationID, result,
				int(time.Since(startedAt).Milliseconds()), toolErr,
			); auditErr != nil {
				finishOperationSpan(toolCtx, toolSpan, result, auditErr)
				return auditErr
			}
		}
		finishOperationSpan(toolCtx, toolSpan, result, toolErr)
		if !em.send(AgentEvent{Type: EventToolResult, Text: result, Data: name}) {
			return em.ctx.Err()
		}
		ls.messages = append(ls.messages, schema.ToolMessage(result, tc.ID))
	}
	return nil
}

// activateSkill 处理 <USE_SKILL> 触发；返回 true 表示已处理（应继续循环）。
// 三层兜底之"白名单回退"：连续两次触发了不存在的技能则关闭 Skill 注入。
func (e *Engine) activateSkill(ctx context.Context, ls *loopState, name string, modelInitiated bool, em emitter) bool {
	spec, ok := skillrunner.Find(ls.skills, name)
	if !ok {
		ls.skillFails++
		if ls.skillFails >= 2 {
			ls.skillsOff = true
			return false // 回退：把这轮内容当最终回答处理
		}
		// 自愈：提示模型重试。
		ls.messages = append(ls.messages, schema.UserMessage(
			fmt.Sprintf("技能 %q 不存在，请从可用技能里选择，或直接回答。", name)))
		return true
	}
	if modelInitiated && spec.DisableModelInvocation {
		ls.messages = append(ls.messages, schema.UserMessage(
			fmt.Sprintf("技能 %q 仅允许用户显式触发，请直接回答或选择其他可自动调用的技能。", name)))
		return true
	}
	if spec.RequiresNetwork && !ls.allowNetwork {
		ls.messages = append(ls.messages, schema.UserMessage(
			fmt.Sprintf("技能 %q 需要网络访问，但当前 Agent 版本未授权网络，请说明无法执行。", name)))
		return true
	}

	metrics.SkillTriggers.Inc()
	em.send(AgentEvent{Type: EventSkillTrigger, Text: spec.Name})
	// L2 注入：body 作为新的 system 消息进上下文。
	ls.messages = append(ls.messages, schema.SystemMessage(skillrunner.L2Message(spec)))
	// 工具范围临时切到该 Skill 的 allowed-tools。
	ls.activeInfos = filterInfos(ls.allInfos, spec.AllowedTools)
	ls.activeSkill = &spec
	ls.activeKBs = stringSet(spec.AllowedKBs)
	if e.audit != nil {
		source := "user"
		if modelInitiated {
			source = "model"
		}
		e.audit.RecordSkillTrigger(ctx, ls.workspaceID, ls.convID, spec.VersionID, spec.Name, source)
	}
	return true
}

func (ls *loopState) toolAllowed(name string) bool {
	if ls.activeInfos == nil {
		return true
	}
	for _, info := range ls.activeInfos {
		if info.Name == name {
			return true
		}
	}
	return false
}

func (ls *loopState) toolPolicyError(name, arguments string) error {
	if !ls.toolAllowed(name) {
		return fmt.Errorf("tool %q not allowed by active skill", name)
	}
	if ls.policies.requiresNetwork[name] && !ls.allowNetwork {
		return fmt.Errorf("tool %q requires network access, but the agent version does not allow it", name)
	}
	if !ls.policies.kbScoped[name] {
		return nil
	}
	var in struct {
		KBID string `json:"kb_id"`
	}
	if err := json.Unmarshal([]byte(arguments), &in); err != nil || in.KBID == "" {
		return fmt.Errorf("tool %q requires a valid kb_id", name)
	}
	if !ls.agentKBs[in.KBID] {
		return fmt.Errorf("knowledge base %q is not attached to the agent", in.KBID)
	}
	if ls.activeSkill != nil && !ls.activeKBs[in.KBID] {
		return fmt.Errorf("knowledge base %q is not allowed by active skill %q", in.KBID, ls.activeSkill.Name)
	}
	return nil
}

// rejectUnauthorizedToolCalls 在审批和实际执行前做 fail-closed 策略校验。
func (e *Engine) rejectUnauthorizedToolCalls(ls *loopState, resp *schema.Message, em emitter) bool {
	denied := make(map[string]string)
	for _, tc := range resp.ToolCalls {
		if _, ok := ls.execByName[tc.Function.Name]; !ok {
			continue
		}
		if err := ls.toolPolicyError(tc.Function.Name, tc.Function.Arguments); err != nil {
			denied[tc.ID] = "error: " + err.Error()
		}
	}
	if len(denied) == 0 {
		return false
	}

	// OpenAI 兼容协议要求 assistant 返回的每个 tool_call_id 都紧跟一条
	// tool 消息。整批中任一调用越权时采用全批拒绝，既避免部分副作用，
	// 也保证并行 Tool Call 的消息序列完整。
	const batchRejected = "error: tool batch rejected because another call violated agent policy"
	for _, tc := range resp.ToolCalls {
		result := denied[tc.ID]
		if result == "" {
			result = batchRejected
		}
		em.send(AgentEvent{Type: EventToolCall, Text: tc.Function.Name, Data: json.RawMessage(tc.Function.Arguments)})
		em.send(AgentEvent{Type: EventToolResult, Text: result, Data: tc.Function.Name})
		ls.messages = append(ls.messages, schema.ToolMessage(result, tc.ID))
	}
	return true
}

// validateToolCallResponses 在请求模型前校验 OpenAI 兼容消息约束：一条
// assistant tool_calls 后必须为每个调用提供且只提供一条关联 tool 消息。
func validateToolCallResponses(messages []*schema.Message) error {
	pending := make(map[string]struct{})
	order := make([]string, 0)
	pendingIDs := func() []string {
		ids := make([]string, 0, len(pending))
		for _, id := range order {
			if _, ok := pending[id]; ok {
				ids = append(ids, id)
			}
		}
		return ids
	}

	for index, message := range messages {
		if len(pending) > 0 {
			if message.Role != schema.Tool {
				return fmt.Errorf("missing tool responses for %s before message %d", strings.Join(pendingIDs(), ", "), index)
			}
			if _, ok := pending[message.ToolCallID]; !ok {
				return fmt.Errorf("unexpected or duplicate tool response %q at message %d", message.ToolCallID, index)
			}
			delete(pending, message.ToolCallID)
			continue
		}
		if message.Role != schema.Assistant || len(message.ToolCalls) == 0 {
			continue
		}
		for _, call := range message.ToolCalls {
			if call.ID == "" {
				return fmt.Errorf("assistant tool call at message %d has an empty id", index)
			}
			if _, exists := pending[call.ID]; exists {
				return fmt.Errorf("assistant tool call id %q is duplicated at message %d", call.ID, index)
			}
			pending[call.ID] = struct{}{}
			order = append(order, call.ID)
		}
	}
	if len(pending) > 0 {
		return fmt.Errorf("missing tool responses for %s", strings.Join(pendingIDs(), ", "))
	}
	return nil
}

func filterInfos(all []*schema.ToolInfo, allowedNames []string) []*schema.ToolInfo {
	allow := map[string]bool{}
	for _, n := range allowedNames {
		allow[n] = true
	}
	out := make([]*schema.ToolInfo, 0, len(allowedNames))
	for _, info := range all {
		if allow[info.Name] {
			out = append(out, info)
		}
	}
	return out
}

// buildTools 把快照里 pin 死的【工具版本 ID】解析成模型可见的 ToolInfo 与按名索引的执行器。
func (e *Engine) buildTools(ctx context.Context, toolVersionIDs, kbIDs []string) ([]*schema.ToolInfo, map[string]tooling.Executor, toolPolicies, error) {
	if len(toolVersionIDs) == 0 || e.tools == nil {
		return nil, map[string]tooling.Executor{}, newToolPolicies(), nil
	}
	infos := make([]*schema.ToolInfo, 0, len(toolVersionIDs))
	execByName := make(map[string]tooling.Executor, len(toolVersionIDs))
	policies := newToolPolicies()
	for _, id := range toolVersionIDs {
		bt, err := e.tools.BuildByVersion(ctx, id)
		if err != nil {
			return nil, nil, toolPolicies{}, fmt.Errorf("build tool version %s: %w", id, err)
		}
		info := bt.Info
		if bt.KBScoped {
			info, err = scopeKnowledgeBaseToolInfo(info, kbIDs)
			if err != nil {
				return nil, nil, toolPolicies{}, fmt.Errorf("scope knowledge base tool %s: %w", bt.Name, err)
			}
		}
		infos = append(infos, info)
		if _, duplicate := execByName[bt.Name]; duplicate {
			return nil, nil, toolPolicies{}, fmt.Errorf("duplicate tool name %q in agent snapshot", bt.Name)
		}
		execByName[bt.Name] = bt.Executor
		policies.versionIDs[bt.Name] = id
		policies.sensitive[bt.Name] = bt.Sensitive
		policies.requiresNetwork[bt.Name] = bt.RequiresNetwork
		policies.kbScoped[bt.Name] = bt.KBScoped
		policies.approvalUI[bt.Name] = bt.ApprovalUI
	}
	return infos, execByName, policies, nil
}

// ApprovalPresentations 返回 Agent 固定版本中各 Tool 的审批卡片展示配置。
// 历史会话依靠同一份不可变 Tool Version 重建与实时运行一致的业务卡片。
func (e *Engine) ApprovalPresentations(
	ctx context.Context, agentVersionID string,
) (map[string]a2ui.ApprovalPresentation, error) {
	snapshot, err := e.platform.GetAgentSnapshotByVersion(ctx, agentVersionID)
	if err != nil {
		return nil, fmt.Errorf("get agent snapshot: %w", err)
	}
	_, _, policies, err := e.buildTools(ctx, snapshot.ToolVersionIDs, snapshot.KBIDs)
	if err != nil {
		return nil, err
	}
	presentations := make(map[string]a2ui.ApprovalPresentation, len(policies.approvalUI))
	for name, presentation := range policies.approvalUI {
		presentations[name] = presentation
	}
	return presentations, nil
}

// scopeKnowledgeBaseToolInfo 把 Agent 快照允许的 KB ID 固化进本次模型调用的
// Tool Schema。模型可以直接选择有效 ID，运行时 allowlist 仍负责最终校验。
func scopeKnowledgeBaseToolInfo(info *schema.ToolInfo, kbIDs []string) (*schema.ToolInfo, error) {
	if info == nil || info.ParamsOneOf == nil || len(kbIDs) == 0 {
		return info, nil
	}
	params, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil {
		return nil, err
	}
	// ParamsOneOf 可能被 Registry 复用，先深拷贝再加入会话级 allowlist。
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	clonedSchema := &jsonschema.Schema{}
	if err := json.Unmarshal(raw, clonedSchema); err != nil {
		return nil, err
	}
	if clonedSchema.Properties == nil {
		return nil, fmt.Errorf("tool schema has no properties")
	}
	kbProperty, ok := clonedSchema.Properties.Get("kb_id")
	if !ok || kbProperty == nil {
		return nil, fmt.Errorf("tool schema has no kb_id property")
	}
	kbProperty.Enum = make([]any, 0, len(kbIDs))
	for _, id := range kbIDs {
		kbProperty.Enum = append(kbProperty.Enum, id)
	}
	kbProperty.Default = kbIDs[0]
	kbProperty.Description = strings.TrimSpace(kbProperty.Description +
		" Allowed knowledge base IDs: " + strings.Join(kbIDs, ", "))

	clonedInfo := *info
	clonedInfo.ParamsOneOf = schema.NewParamsOneOfByJSONSchema(clonedSchema)
	return &clonedInfo, nil
}

func newToolPolicies() toolPolicies {
	return toolPolicies{
		sensitive: make(map[string]bool), requiresNetwork: make(map[string]bool),
		kbScoped: make(map[string]bool), approvalUI: make(map[string]a2ui.ApprovalPresentation),
		versionIDs: make(map[string]string),
	}
}

func stringSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}

// buildMessages 构建 schema.Message 列表（含 L1 技能元数据注入）。
func buildMessages(snapshot *AgentSnapshot, history []*domain.Message, userMsg string) []*schema.Message {
	var messages []*schema.Message

	system := snapshot.SystemPrompt
	if l1 := skillrunner.BuildL1(snapshot.Skills); l1 != "" {
		if system != "" {
			system += "\n\n"
		}
		system += l1
	}
	if system != "" {
		messages = append(messages, schema.SystemMessage(system))
	}

	// 最多取最近 10 条历史。
	start := 0
	if len(history) > 10 {
		start = len(history) - 10
	}
	for i := start; i < len(history); i++ {
		msg := history[i]
		switch msg.Role {
		case "user":
			messages = append(messages, schema.UserMessage(msg.Content))
		case "assistant":
			messages = append(messages, schema.AssistantMessage(msg.Content, nil))
		}
	}

	messages = append(messages, schema.UserMessage(userMsg))
	return messages
}

func maxStepsOf(s *AgentSnapshot) int {
	if s.MaxSteps > 0 {
		return s.MaxSteps
	}
	return defaultMaxSteps
}
