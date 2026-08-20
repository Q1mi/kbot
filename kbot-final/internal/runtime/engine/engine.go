// Package engine 基于 Eino ADK 实现工具调用、Skill 激活、审批续跑与配置快照运行时。
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
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

type executionPlanner interface {
	PrepareExecution(context.Context) (*llm.ExecutionPlan, error)
}

// Engine Runtime引擎
type Engine struct {
	platform     Platform
	model        model.BaseChatModel // 单元测试/内嵌场景使用；生产环境由 planner 解析 Model Profile
	planner      executionPlanner
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
	return &Engine{platform: platform, planner: llmGW, tools: tools}
}

// NewEngineWithChatModel 用 Eino 标准 ChatModel 构造引擎，供测试和内嵌运行使用。
func NewEngineWithChatModel(platform Platform, chatModel model.BaseChatModel, tools *tooling.Registry) *Engine {
	return &Engine{platform: platform, model: chatModel, tools: tools}
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

	_, _, policies, err := e.buildTools(ctx, snapshot.ToolVersionIDs, snapshot.KBIDs)
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
		answer, interrupts, checkpoint, runErr := e.runWithADK(
			ctx, snapshot, messages, policies, conv.ID, conv.WorkspaceID, em,
		)
		if runErr != nil {
			traceErr = runErr
			em.send(AgentEvent{Type: EventError, Text: runErr.Error()})
			return
		}
		if len(interrupts) > 0 {
			if err := persistApprovalInterrupts(ctx, e.approvals, conv.ID, checkpoint, interrupts); err != nil {
				traceErr = err
				em.send(AgentEvent{Type: EventError, Text: err.Error()})
				return
			}
			emitApprovalEvents(e, em, interrupts, policies)
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

// guardLLMCall 由 ADK ChatModelAgent 的模型调用中间件执行限流和配额检查。
func (e *Engine) guardLLMCall(ctx context.Context) error {
	if g, ok := e.guard.(interface {
		OnLLMCall(context.Context) error
	}); ok {
		return g.OnLLMCall(ctx)
	}
	return nil
}

type toolPolicies struct {
	tools           []einotool.BaseTool
	sensitive       map[string]bool
	requiresNetwork map[string]bool
	kbScoped        map[string]bool
	approvalUI      map[string]a2ui.ApprovalPresentation
	versionIDs      map[string]string
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
		policies.tools = append(policies.tools, bt.InvokableTool(info))
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

// buildMessages 构建历史消息；Agent 的 system instruction 与 Skill 渐进式披露由 Eino ADK 注入。
func buildMessages(_ *AgentSnapshot, history []*domain.Message, userMsg string) []*schema.Message {
	var messages []*schema.Message

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
