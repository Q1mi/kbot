package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cloudwego/eino/schema"
	"go.opentelemetry.io/otel/attribute"

	"github.com/Q1mi/kbot/internal/a2ui"
	"github.com/Q1mi/kbot/internal/domain"
	kotel "github.com/Q1mi/kbot/internal/infrastructure/otel"
	"github.com/Q1mi/kbot/internal/runtime/guard"
	"github.com/Q1mi/kbot/internal/runtime/llm"
	"github.com/Q1mi/kbot/internal/runtime/skillrunner"
	"github.com/Q1mi/kbot/internal/util"
)

// EventAwaitApproval 在敏感工具被拦下等审批时发给客户端,带 approval_id。
const EventAwaitApproval = "await_approval"

// errAwaitApproval 是 runLoop 内部的"暂停"哨兵:已写 pending approval + checkpoint,等人审批。
var errAwaitApproval = errors.New("await approval")

// ErrApprovalUnavailable 表示敏感工具缺少可用的审批持久化能力。
var ErrApprovalUnavailable = errors.New("sensitive tool requires approval gate")

type approvalCheckpoint struct {
	Messages        []*schema.Message `json:"messages"`
	ActiveSkillName string            `json:"active_skill_name,omitempty"`
}

// ApprovalGate 是 engine 用来"暂停-续跑"的最小持久化接口(approval.Store 适配后满足)。
type ApprovalGate interface {
	CreatePending(ctx context.Context, id, workspaceID, conversationID, action, payload string) error
	SaveCheckpoint(ctx context.Context, approvalID, conversationID string, state []byte) error
	CheckpointForApproval(ctx context.Context, approvalID, conversationID string) ([]byte, error)
	BeginExecution(ctx context.Context, approvalID, conversationID string) (string, error)
	RenewExecution(ctx context.Context, approvalID, token string) error
	CompleteExecution(ctx context.Context, approvalID, token string) error
	FailExecution(ctx context.Context, approvalID, token, message string) error
}

// WithApprovals 挂上审批门。敏感工具缺少审批门时会安全失败。
func (e *Engine) WithApprovals(g ApprovalGate) *Engine { e.approvals = g; return e }

// pauseIfSensitive 在本批包含敏感工具时写入 pending approval 与 checkpoint，随后通知客户端暂停。
func (e *Engine) pauseIfSensitive(ctx context.Context, ls *loopState, resp *schema.Message, em emitter) (bool, error) {
	for _, tc := range resp.ToolCalls {
		if !ls.policies.sensitive[tc.Function.Name] {
			continue
		}
		if e.approvals == nil {
			return false, ErrApprovalUnavailable
		}
		apprID := util.GenerateID()
		approvalCtx, approvalSpan := startOperationSpan(ctx, "approval.pause",
			attribute.String("approval.id", apprID),
			attribute.String("gen_ai.tool.name", tc.Function.Name),
			attribute.String("approval.status", "pending"),
		)
		if e.traceOptions.CaptureContent {
			approvalSpan.SetAttributes(attribute.String("langfuse.observation.input", tc.Function.Arguments))
		}
		if err := e.approvals.CreatePending(approvalCtx, apprID, ls.workspaceID, ls.convID, tc.Function.Name, tc.Function.Arguments); err != nil {
			finishOperationSpan(approvalCtx, approvalSpan, "failed", err)
			return false, fmt.Errorf("create pending approval: %w", err)
		}
		// checkpoint 当前 messages(已含本轮 assistant 的 tool-call 决策),续跑时不重新生成。
		checkpoint := approvalCheckpoint{Messages: ls.messages}
		if ls.activeSkill != nil {
			checkpoint.ActiveSkillName = ls.activeSkill.Name
		}
		state, err := json.Marshal(checkpoint)
		if err != nil {
			finishOperationSpan(approvalCtx, approvalSpan, "failed", err)
			return false, fmt.Errorf("marshal approval checkpoint: %w", err)
		}
		if err := e.approvals.SaveCheckpoint(approvalCtx, apprID, ls.convID, state); err != nil {
			finishOperationSpan(approvalCtx, approvalSpan, "failed", err)
			return false, fmt.Errorf("save approval checkpoint: %w", err)
		}
		finishOperationSpan(approvalCtx, approvalSpan, "pending", nil)
		// pending + checkpoint 已落库,即便客户端已断开也照常暂停;send 失败无所谓。
		em.send(AgentEvent{Type: EventAwaitApproval, Text: apprID, Data: tc.Function.Name})
		if messages, err := a2ui.ApprovalSurfaceWithPresentation(
			apprID, ls.convID, tc.Function.Name, tc.Function.Arguments,
			ls.policies.approvalUI[tc.Function.Name],
		); err == nil {
			e.emitA2UI(em, messages)
		}
		return true, nil
	}
	return false, nil
}

// Resume 从 checkpoint 续跑一个等审批的会话(worker 在审批通过后调用)。
// 载回 messages 快照 → 重建工具 → 执行那批被批准的工具 → 续跑 runLoop(LLM 不重新生成 tool-call 决策)。
func (e *Engine) Resume(ctx context.Context, conversationID, approvalID string) (result string, retErr error) {
	ctx, resumeSpan := startOperationSpan(ctx, "approval.resume",
		attribute.String("conversation.id", conversationID),
		attribute.String("approval.id", approvalID),
		attribute.String("approval.status", "approved"),
	)
	defer func() { finishOperationSpan(ctx, resumeSpan, result, retErr) }()
	if e.approvals == nil {
		return "", fmt.Errorf("resume: 未挂审批门")
	}
	executionToken, err := e.approvals.BeginExecution(ctx, approvalID, conversationID)
	if err != nil {
		return "", fmt.Errorf("claim approval execution: %w", err)
	}
	defer func() {
		if retErr == nil || errors.Is(retErr, errAwaitApproval) {
			if err := e.approvals.CompleteExecution(context.WithoutCancel(ctx), approvalID, executionToken); err != nil && retErr == nil {
				retErr = fmt.Errorf("complete approval execution: %w", err)
			}
			return
		}
		_ = e.approvals.FailExecution(context.WithoutCancel(ctx), approvalID, executionToken, retErr.Error())
	}()
	runCtx, cancelRun := context.WithCancel(ctx)
	ctx = runCtx
	heartbeatStop := make(chan struct{})
	heartbeatDone := make(chan error, 1)
	go e.renewApprovalLease(ctx, approvalID, executionToken, heartbeatStop, heartbeatDone, cancelRun)
	defer func() {
		close(heartbeatStop)
		if heartbeatErr := <-heartbeatDone; heartbeatErr != nil && retErr == nil {
			retErr = fmt.Errorf("renew approval execution lease: %w", heartbeatErr)
		}
		cancelRun()
	}()
	turns, _ := e.platform.(conversationTurnCoordinator)
	turnToken := ""
	turnFinalized := false
	if turns != nil {
		turnToken, err = turns.ClaimConversationTurn(ctx, conversationID, true)
		if err != nil {
			return "", fmt.Errorf("claim conversation turn: %w", err)
		}
		turnCtx, cancelTurn := context.WithCancel(ctx)
		ctx = turnCtx
		turnStop := make(chan struct{})
		turnDone := make(chan error, 1)
		go e.renewConversationTurn(ctx, turns, conversationID, turnToken, turnStop, turnDone, cancelTurn)
		defer func() {
			close(turnStop)
			if heartbeatErr := <-turnDone; heartbeatErr != nil && retErr == nil {
				retErr = fmt.Errorf("renew conversation turn lease: %w", heartbeatErr)
			}
			if !turnFinalized {
				_ = turns.ReleaseConversationTurn(context.WithoutCancel(ctx), conversationID, turnToken, "awaiting_approval")
			}
			cancelTurn()
		}()
	}
	state, err := e.approvals.CheckpointForApproval(ctx, approvalID, conversationID)
	if err != nil {
		return "", fmt.Errorf("load checkpoint: %w", err)
	}
	var checkpoint approvalCheckpoint
	if err := json.Unmarshal(state, &checkpoint); err != nil || len(checkpoint.Messages) == 0 {
		// 兼容 migration 14 之前只保存 messages 数组的 checkpoint。
		if legacyErr := json.Unmarshal(state, &checkpoint.Messages); legacyErr != nil {
			return "", fmt.Errorf("unmarshal checkpoint: %w", legacyErr)
		}
	}
	messages := checkpoint.Messages
	if len(messages) == 0 {
		return "", fmt.Errorf("checkpoint 为空")
	}

	conv, err := e.platform.LoadConversation(ctx, conversationID)
	if err != nil {
		return "", fmt.Errorf("load conversation: %w", err)
	}
	ctx = kotel.WithTraceContext(ctx, kotel.TraceContext{
		TraceName: "agent-resume", UserID: conv.UserID, SessionID: conversationID,
		Version: conv.AgentVersionID, Tags: []string{"kbot", "approval-resume"},
		Metadata: map[string]string{
			"workspace_id": conv.WorkspaceID, "agent_id": conv.AgentID,
			"agent_version_id": conv.AgentVersionID,
		},
		CaptureContent: e.traceOptions.CaptureContent,
	})
	resumeSpan.SetAttributes(kotel.LangfuseAttributes(ctx)...)
	ctx = llm.WithClassification(ctx, conv.Classification)
	ctx = guard.WithRateKey(ctx, conv.UserID)
	ctx = guard.WithWorkspaceKey(ctx, conv.WorkspaceID)
	snapshot, err := e.platform.GetAgentSnapshotByVersion(ctx, conv.AgentVersionID)
	if err != nil {
		return "", fmt.Errorf("get agent snapshot: %w", err)
	}
	if err := applyConversationRuntimeConfig(snapshot, conv.RuntimeConfigJSON); err != nil {
		return "", err
	}
	ctx = llm.WithInvocationConfig(ctx, llm.InvocationConfig{
		WorkspaceID: conv.WorkspaceID, AgentID: conv.AgentID, UserID: conv.UserID,
		Environment:           conversationEnvironment(conv),
		PromptVersionID:       snapshot.PromptVersionID,
		ModelProfileVersionID: snapshot.ModelProfileVersionID,
		GenerationConfig:      snapshot.GenerationConfig, ExperimentID: snapshot.ExperimentID,
		ExperimentVariant: snapshot.ExperimentVariant,
	})
	allInfos, execByName, policies, err := e.buildTools(ctx, snapshot.ToolVersionIDs, snapshot.KBIDs)
	if err != nil {
		return "", fmt.Errorf("build tools: %w", err)
	}
	ls := &loopState{
		convID: conversationID, workspaceID: conv.WorkspaceID, messages: messages, allInfos: allInfos, execByName: execByName,
		policies: policies, skills: snapshot.Skills, agentKBs: stringSet(snapshot.KBIDs),
		allowNetwork: snapshot.AllowNetwork, maxSteps: maxStepsOf(snapshot),
	}
	if checkpoint.ActiveSkillName != "" {
		if spec, ok := skillrunner.Find(snapshot.Skills, checkpoint.ActiveSkillName); ok {
			ls.activeSkill = &spec
			ls.activeInfos = filterInfos(allInfos, spec.AllowedTools)
			ls.activeKBs = stringSet(spec.AllowedKBs)
		}
	}

	// 事件 drain(Resume 非流式,只取最终文本)。drain goroutine 一直读到 close,
	// 故这里的 send 不会阻塞;emitter 仍带 ctx,取消时也能让 runLoop/executeToolBatch 提前收尾。
	eventCh := make(chan AgentEvent, 16)
	done := make(chan struct{})
	go func() {
		for range eventCh {
		}
		close(done)
	}()
	em := emitter{ctx: ctx, ch: eventCh}

	// 执行被批准的那批工具(checkpoint 最后一条 assistant 的 tool_calls),不再暂停。
	last := messages[len(messages)-1]
	if len(last.ToolCalls) > 0 {
		if e.rejectUnauthorizedToolCalls(ls, last, em) {
			close(eventCh)
			<-done
			return "", fmt.Errorf("approved tool call no longer satisfies agent policy")
		}
		if err := e.executeToolBatch(ctx, ls, last, em); err != nil {
			close(eventCh)
			<-done
			return "", err
		}
	}

	answer, runErr := e.runLoop(ctx, ls, em)
	close(eventCh)
	<-done
	if errors.Is(runErr, errAwaitApproval) {
		// 续跑里又撞上敏感工具:再次暂停(已写新的 pending+checkpoint)。
		if err := e.commitConversationTurn(ctx, turns, conversationID, turnToken, nil, "awaiting_approval"); err != nil {
			return "", fmt.Errorf("save awaiting approval state: %w", err)
		}
		turnFinalized = true
		return "", errAwaitApproval
	}
	if runErr != nil {
		return "", runErr
	}

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
			e.recordAudit(ctx, conversationID, "system", "guard_blocked", gErr.Error())
			return "", gErr
		}
		answer = patched
	}
	if err := e.commitConversationTurn(ctx, turns, conversationID, turnToken, []*domain.Message{{
		ID: util.GenerateID(), ConversationID: conversationID, Role: "assistant", Content: answer,
	}}, "active"); err != nil {
		return answer, fmt.Errorf("save assistant message: %w", err)
	}
	turnFinalized = true
	e.recordAudit(ctx, conversationID, "system", "resumed", answer)
	return answer, nil
}

func (e *Engine) renewApprovalLease(
	ctx context.Context, approvalID, token string, stop <-chan struct{}, done chan<- error, cancel context.CancelFunc,
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
			if err := e.approvals.RenewExecution(context.WithoutCancel(ctx), approvalID, token); err != nil {
				cancel()
				done <- err
				return
			}
		}
	}
}
