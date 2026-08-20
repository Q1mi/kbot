package engine

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/cloudwego/eino/adk"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

type checkpointGate struct {
	mu      sync.Mutex
	state   []byte
	states  map[string][]byte
	pending []string
}

func (g *checkpointGate) CreatePending(_ context.Context, id, _, _, _, _ string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.pending = append(g.pending, id)
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
func (g *checkpointGate) SaveCheckpoint(_ context.Context, id, _ string, state []byte) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.state = append([]byte(nil), state...)
	if g.states == nil {
		g.states = make(map[string][]byte)
	}
	g.states[id] = append([]byte(nil), state...)
	return nil
}
func (g *checkpointGate) CheckpointForApproval(_ context.Context, id, _ string) ([]byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if state := g.states[id]; len(state) > 0 {
		return append([]byte(nil), state...), nil
	}
	return append([]byte(nil), g.state...), nil
}

type approvalTestTool struct {
	name  string
	calls *int
}

func (t *approvalTestTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: t.name}, nil
}

func (t *approvalTestTool) InvokableRun(context.Context, string, ...einotool.Option) (string, error) {
	*t.calls++
	return t.name + " completed", nil
}

func TestApprovalResumeTargetsOneSensitiveToolAtATime(t *testing.T) {
	gate := &checkpointGate{}
	model := &scriptedChatModel{replies: []*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{
			{ID: "call-one", Function: schema.FunctionCall{Name: "write_one", Arguments: `{}`}},
			{ID: "call-two", Function: schema.FunctionCall{Name: "write_two", Arguments: `{}`}},
		}),
		schema.AssistantMessage("全部完成", nil),
	}}
	engine := (&Engine{model: model}).WithApprovals(gate)
	executions := 0
	policies := newToolPolicies()
	policies.tools = []einotool.BaseTool{
		&approvalTestTool{name: "write_one", calls: &executions},
		&approvalTestTool{name: "write_two", calls: &executions},
	}
	policies.sensitive["write_one"] = true
	policies.sensitive["write_two"] = true
	snapshot := &AgentSnapshot{MaxSteps: 4}
	events := make(chan AgentEvent, 32)
	em := emitter{ctx: context.Background(), ch: events}

	_, interrupts, checkpoint, err := engine.runWithADK(
		context.Background(), snapshot, []*schema.Message{schema.UserMessage("执行两个写操作")},
		policies, "conversation-1", "workspace-1", em,
	)
	if err != nil || len(interrupts) == 0 || executions != 0 {
		t.Fatalf("initial run: interrupts=%d executions=%d err=%v", len(interrupts), executions, err)
	}
	if err := persistApprovalInterrupts(context.Background(), gate, "conversation-1", checkpoint, interrupts); err != nil {
		t.Fatal(err)
	}
	firstInfo, ok := firstApprovalInfo(interrupts)
	if !ok {
		t.Fatal("first approval info missing")
	}
	firstStored, _ := gate.CheckpointForApproval(context.Background(), firstInfo.ApprovalID, "conversation-1")
	firstCheckpoint, firstAddress, err := decodeFrameworkCheckpoint(firstStored)
	if err != nil {
		t.Fatal(err)
	}

	_, interrupts, checkpoint, err = engine.resumeWithADK(
		context.Background(), snapshot, policies, "conversation-1", "workspace-1",
		firstInfo.ApprovalID, firstAddress, firstCheckpoint, em,
	)
	if err != nil || len(interrupts) == 0 || executions != 1 {
		t.Fatalf("first resume: interrupts=%d executions=%d err=%v", len(interrupts), executions, err)
	}
	if err := persistApprovalInterrupts(context.Background(), gate, "conversation-1", checkpoint, interrupts); err != nil {
		t.Fatal(err)
	}
	secondInfo, ok := firstApprovalInfo(interrupts)
	if !ok || secondInfo.ToolName != "write_two" {
		t.Fatalf("second approval info=%+v ok=%v", secondInfo, ok)
	}
	secondStored, _ := gate.CheckpointForApproval(context.Background(), secondInfo.ApprovalID, "conversation-1")
	secondCheckpoint, secondAddress, err := decodeFrameworkCheckpoint(secondStored)
	if err != nil {
		t.Fatal(err)
	}

	answer, interrupts, _, err := engine.resumeWithADK(
		context.Background(), snapshot, policies, "conversation-1", "workspace-1",
		secondInfo.ApprovalID, secondAddress, secondCheckpoint, em,
	)
	if err != nil || len(interrupts) != 0 || executions != 2 || answer != "全部完成" {
		t.Fatalf("second resume: answer=%q interrupts=%d executions=%d err=%v", answer, len(interrupts), executions, err)
	}
}

func firstApprovalInfo(contexts []*adk.InterruptCtx) (approvalInterruptInfo, bool) {
	for _, interrupt := range contexts {
		if interrupt != nil && interrupt.IsRootCause {
			if info, ok := approvalInfoFromAny(interrupt.Info); ok {
				return info, true
			}
		}
	}
	return approvalInterruptInfo{}, false
}

func TestSensitiveToolRequiresApprovalGate(t *testing.T) {
	e := &Engine{}
	state := &agentRunState{conversationID: "conversation-1"}
	err := e.interruptForApproval(context.Background(), state, &compose.ToolInput{
		Name: "refund_order", CallID: "call-1", Arguments: `{"order_id":"1"}`,
	})
	if !errors.Is(err, ErrApprovalUnavailable) {
		t.Fatalf("error=%v want=%v", err, ErrApprovalUnavailable)
	}
}

func TestApprovalCheckpointPreservesFrameworkInterruptAddress(t *testing.T) {
	gate := &checkpointGate{}
	info := approvalInterruptInfo{
		ApprovalID: "approval-1", ConversationID: "conversation-1",
		ToolName: "refund_order", Arguments: `{}`, ToolCallID: "call-1",
	}
	rawCheckpoint := []byte("eino-checkpoint")
	err := persistApprovalInterrupts(context.Background(), gate, "conversation-1", rawCheckpoint, []*adk.InterruptCtx{{
		ID: "agent:kbot;tool:call-1", Info: info, IsRootCause: true,
	}})
	if err != nil {
		t.Fatalf("persist checkpoint: %v", err)
	}
	decoded, interruptID, err := decodeFrameworkCheckpoint(gate.state)
	if err != nil {
		t.Fatalf("decode framework checkpoint: %v", err)
	}
	if string(decoded) != string(rawCheckpoint) || interruptID != "agent:kbot;tool:call-1" {
		t.Fatalf("decoded=%q interrupt_id=%q", decoded, interruptID)
	}
}
