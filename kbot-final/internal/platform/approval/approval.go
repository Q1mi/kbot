// Package approval 持久化人在环审批与会话 checkpoint。
//
// 敏感工具调用前:engine 写一条 pending approval + 存当前会话 checkpoint(messages 快照),
// 暂停并返回 EventAwaitApproval;审批人点准后从 checkpoint 续跑(LLM 不重新生成 tool-call 决策)。
// 本包负责持久化；engine 的暂停/续跑、A2UI action 与 worker 恢复已在运行路径中完成装配。
package approval

import (
	"context"
	"errors"
	"time"
)

var ErrAlreadyResolved = errors.New("approval is no longer pending")
var ErrExecutionUnavailable = errors.New("approval execution is already claimed or unavailable")

// Status 取值。
const (
	StatusPending  = "pending"
	StatusApproved = "approved"
	StatusRejected = "rejected"
)

// Approval 是一条审批记录。
type Approval struct {
	ID                   string     `json:"id"`
	WorkspaceID          string     `json:"workspace_id"`
	ConversationID       string     `json:"conversation_id"`
	Action               string     `json:"action"`  // 触发审批的工具名
	Payload              string     `json:"payload"` // 工具入参(JSON)
	Status               string     `json:"status"`  // pending / approved / rejected
	ApproverID           string     `json:"approver_id,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	ResolvedAt           *time.Time `json:"resolved_at,omitempty"`
	ExecutionStatus      string     `json:"execution_status"`
	ExecutionStartedAt   *time.Time `json:"execution_started_at,omitempty"`
	ExecutionCompletedAt *time.Time `json:"execution_completed_at,omitempty"`
	ExecutionError       string     `json:"execution_error,omitempty"`
	ExecutionLeaseUntil  *time.Time `json:"execution_lease_until,omitempty"`
	ExecutionAttempts    int        `json:"execution_attempts"`
	ExecutionToken       string     `json:"-"`
}

// Store 是审批 + checkpoint 的持久化接口(memory 与 postgres 双实现)。
type Store interface {
	CreatePending(ctx context.Context, a *Approval) error
	Get(ctx context.Context, id string) (*Approval, error)
	ListPending(ctx context.Context, workspaceID string) ([]*Approval, error)
	ListByConversation(ctx context.Context, conversationID string) ([]*Approval, error)
	ResolvePending(ctx context.Context, id, workspaceID, status, approverID string) (*Approval, error)
	BeginExecution(ctx context.Context, id, conversationID string) (string, error)
	RenewExecution(ctx context.Context, id, token string) error
	CompleteExecution(ctx context.Context, id, token string) error
	FailExecution(ctx context.Context, id, token, message string) error
	ListReadyResumes(ctx context.Context, limit int32) ([]*Approval, error)

	SaveCheckpoint(ctx context.Context, approvalID, conversationID string, state []byte) error
	CheckpointForApproval(ctx context.Context, approvalID, conversationID string) ([]byte, error)
}
