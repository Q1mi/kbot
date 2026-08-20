package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/Q1mi/kbot/internal/domain"
	kotel "github.com/Q1mi/kbot/internal/infrastructure/otel"
	"github.com/Q1mi/kbot/internal/runtime/guard"
	"github.com/Q1mi/kbot/internal/runtime/llm"
	"github.com/Q1mi/kbot/internal/util"
)

// EventAwaitApproval 在敏感工具被拦下等审批时发给客户端,带 approval_id。
const EventAwaitApproval = "await_approval"

// errAwaitApproval 是 Resume 控制流的暂停哨兵：Eino 已生成新 checkpoint，等待下一项审批。
var errAwaitApproval = errors.New("await approval")

// ErrApprovalUnavailable 表示敏感工具缺少可用的审批持久化能力。
var ErrApprovalUnavailable = errors.New("sensitive tool requires approval gate")

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

// Resume 从 checkpoint 续跑一个等审批的会话(worker 在审批通过后调用)。
// Eino Runner 载回完整框架 checkpoint，并只恢复审批记录对应的工具调用。
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
	frameworkCheckpoint, interruptID, err := decodeFrameworkCheckpoint(state)
	if err != nil {
		return "", err
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
	_, _, policies, err := e.buildTools(ctx, snapshot.ToolVersionIDs, snapshot.KBIDs)
	if err != nil {
		return "", fmt.Errorf("build tools: %w", err)
	}

	// Resume 由审批 worker 触发，事件只用于维持与在线运行一致的审计路径。
	eventCh := make(chan AgentEvent, 16)
	done := make(chan struct{})
	go func() {
		for range eventCh {
		}
		close(done)
	}()
	em := emitter{ctx: ctx, ch: eventCh}
	answer, interrupts, nextCheckpoint, runErr := e.resumeWithADK(
		ctx, snapshot, policies, conversationID, conv.WorkspaceID,
		approvalID, interruptID, frameworkCheckpoint, em,
	)
	close(eventCh)
	<-done
	if runErr != nil {
		return "", runErr
	}
	if len(interrupts) > 0 {
		if err := persistApprovalInterrupts(ctx, e.approvals, conversationID, nextCheckpoint, interrupts); err != nil {
			return "", err
		}
		if err := e.commitConversationTurn(ctx, turns, conversationID, turnToken, nil, "awaiting_approval"); err != nil {
			return "", fmt.Errorf("save awaiting approval state: %w", err)
		}
		turnFinalized = true
		return "", errAwaitApproval
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
