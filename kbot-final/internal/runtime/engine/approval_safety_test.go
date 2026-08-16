package engine

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Q1mi/kbot/internal/runtime/skillrunner"
	"github.com/cloudwego/eino/schema"
)

type checkpointGate struct{ state []byte }

func (g *checkpointGate) CreatePending(context.Context, string, string, string, string, string) error {
	return nil
}
func (g *checkpointGate) BeginExecution(context.Context, string, string) (string, error) {
	return "token", nil
}
func (g *checkpointGate) RenewExecution(context.Context, string, string) error { return nil }
func (g *checkpointGate) CompleteExecution(context.Context, string, string) error {
	return nil
}
func (g *checkpointGate) FailExecution(context.Context, string, string, string) error {
	return nil
}
func (g *checkpointGate) SaveCheckpoint(_ context.Context, _, _ string, state []byte) error {
	g.state = append([]byte(nil), state...)
	return nil
}
func (g *checkpointGate) CheckpointForApproval(context.Context, string, string) ([]byte, error) {
	return g.state, nil
}

func TestSensitiveToolRequiresApprovalGate(t *testing.T) {
	e := &Engine{}
	ls := &loopState{
		convID: "conversation-1",
		policies: toolPolicies{
			sensitive: map[string]bool{"refund_order": true},
		},
	}
	resp := &schema.Message{ToolCalls: []schema.ToolCall{{
		ID:       "call-1",
		Function: schema.FunctionCall{Name: "refund_order", Arguments: `{"order_id":"1"}`},
	}}}
	paused, err := e.pauseIfSensitive(context.Background(), ls, resp, emitter{})
	if paused {
		t.Fatal("request should not enter a resumable paused state without persistence")
	}
	if !errors.Is(err, ErrApprovalUnavailable) {
		t.Fatalf("error=%v want=%v", err, ErrApprovalUnavailable)
	}
}

func TestApprovalCheckpointPreservesActiveSkill(t *testing.T) {
	gate := &checkpointGate{}
	e := &Engine{approvals: gate}
	active := &skillrunner.Spec{Name: "refund-order"}
	ls := &loopState{
		convID: "conversation-1", activeSkill: active,
		messages: []*schema.Message{schema.UserMessage("refund")},
		policies: toolPolicies{sensitive: map[string]bool{"refund_order": true}},
	}
	resp := &schema.Message{ToolCalls: []schema.ToolCall{{
		ID: "call-1", Function: schema.FunctionCall{Name: "refund_order", Arguments: `{}`},
	}}}
	events := make(chan AgentEvent, 4)
	paused, err := e.pauseIfSensitive(context.Background(), ls, resp, emitter{ctx: context.Background(), ch: events})
	if err != nil || !paused {
		t.Fatalf("pause: paused=%v err=%v", paused, err)
	}
	var checkpoint approvalCheckpoint
	if err := json.Unmarshal(gate.state, &checkpoint); err != nil {
		t.Fatalf("decode checkpoint: %v", err)
	}
	if checkpoint.ActiveSkillName != "refund-order" || len(checkpoint.Messages) != 1 {
		t.Fatalf("active skill missing from checkpoint: %+v", checkpoint)
	}
}
