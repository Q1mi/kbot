package platform

import (
	"context"
	"fmt"

	"github.com/Q1mi/kbot/internal/platform/approval"
	"github.com/Q1mi/kbot/internal/runtime/engine"
)

// engineApprovalGate 把 approval.Store 适配成 engine.ApprovalGate
// (engine 用基本类型,store 用 *approval.Approval)。
type engineApprovalGate struct{ store approval.Store }

func (g engineApprovalGate) CreatePending(ctx context.Context, id, workspaceID, conversationID, action, payload string) error {
	return g.store.CreatePending(ctx, &approval.Approval{
		ID:             id,
		WorkspaceID:    workspaceID,
		ConversationID: conversationID,
		Action:         action,
		Payload:        payload,
	})
}

func (g engineApprovalGate) SaveCheckpoint(ctx context.Context, approvalID, conversationID string, state []byte) error {
	return g.store.SaveCheckpoint(ctx, approvalID, conversationID, state)
}

func (g engineApprovalGate) CheckpointForApproval(ctx context.Context, approvalID, conversationID string) ([]byte, error) {
	appr, err := g.store.Get(ctx, approvalID)
	if err != nil {
		return nil, err
	}
	if appr.Status != approval.StatusApproved {
		return nil, fmt.Errorf("approval %s is %s", approvalID, appr.Status)
	}
	return g.store.CheckpointForApproval(ctx, approvalID, conversationID)
}

func (g engineApprovalGate) BeginExecution(ctx context.Context, approvalID, conversationID string) (string, error) {
	return g.store.BeginExecution(ctx, approvalID, conversationID)
}

func (g engineApprovalGate) RenewExecution(ctx context.Context, approvalID, token string) error {
	return g.store.RenewExecution(ctx, approvalID, token)
}

func (g engineApprovalGate) CompleteExecution(ctx context.Context, approvalID, token string) error {
	return g.store.CompleteExecution(ctx, approvalID, token)
}

func (g engineApprovalGate) FailExecution(ctx context.Context, approvalID, token, message string) error {
	return g.store.FailExecution(ctx, approvalID, token, message)
}

// ApprovalGate 返回可挂到 engine 的审批门。
func (s *Service) ApprovalGate() engine.ApprovalGate {
	return engineApprovalGate{store: s.Approvals}
}
