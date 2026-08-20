package engine

import (
	"context"
	"fmt"
	"testing"

	"github.com/cloudwego/eino/schema"

	"github.com/Q1mi/kbot/internal/domain"
)

type teamPlatform struct {
	snapshots map[string]*AgentSnapshot
}

func (p *teamPlatform) CreateConversation(
	_ context.Context, agentID, userID string,
) (*domain.Conversation, error) {
	return p.conversation(agentID+"-v1", userID)
}

func (p *teamPlatform) CreateConversationWithVersion(
	_ context.Context, versionID, userID string,
) (*domain.Conversation, error) {
	return p.conversation(versionID, userID)
}

func (p *teamPlatform) conversation(versionID, userID string) (*domain.Conversation, error) {
	snapshot := p.snapshots[versionID]
	if snapshot == nil {
		return nil, fmt.Errorf("unknown version %s", versionID)
	}
	return &domain.Conversation{
		ID: versionID + "-conversation", AgentID: snapshot.AgentID, AgentVersionID: versionID,
		WorkspaceID: snapshot.WorkspaceID, UserID: userID, Classification: "internal",
	}, nil
}

func (p *teamPlatform) LoadConversation(context.Context, string) (*domain.Conversation, error) {
	return nil, fmt.Errorf("not implemented")
}

func (p *teamPlatform) GetAgentSnapshotByVersion(
	_ context.Context, versionID string,
) (*AgentSnapshot, error) {
	snapshot := p.snapshots[versionID]
	if snapshot == nil {
		return nil, fmt.Errorf("unknown version %s", versionID)
	}
	cloned := *snapshot
	return &cloned, nil
}

func (*teamPlatform) LoadConversationMessages(context.Context, string) ([]*domain.Message, error) {
	return nil, nil
}

func (*teamPlatform) AppendMessage(context.Context, string, string, string) error { return nil }

func TestRunSupervisorTeamUsesEinoAgentTool(t *testing.T) {
	platform := &teamPlatform{snapshots: map[string]*AgentSnapshot{
		"supervisor-v1": {
			ID: "supervisor-v1", AgentID: "supervisor", WorkspaceID: "w1",
			SystemPrompt: "你是客服主管", MaxSteps: 4,
		},
		"billing-v1": {
			ID: "billing-v1", AgentID: "billing-agent", WorkspaceID: "w1",
			SystemPrompt: "你是账务专家", MaxSteps: 2,
		},
	}}
	chatModel := &scriptedChatModel{replies: []*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{{
			ID:       "delegate-1",
			Function: schema.FunctionCall{Name: "billing", Arguments: `{"request":"核对退款状态"}`},
		}}),
		schema.AssistantMessage("退款状态正常，可原路退回。", nil),
		schema.AssistantMessage("已核对，款项可以原路退回。", nil),
	}}
	engine := NewEngineWithChatModel(platform, chatModel, nil)

	answer, steps, err := engine.RunSupervisorTeam(
		context.Background(),
		TeamMember{AgentID: "supervisor", AgentVersionID: "supervisor-v1", Role: "supervisor"},
		[]TeamMember{{AgentID: "billing-agent", AgentVersionID: "billing-v1", Role: "billing"}},
		"帮我确认退款", "w1", "u1",
	)
	if err != nil {
		t.Fatalf("run supervisor team: %v", err)
	}
	if answer != "已核对，款项可以原路退回。" {
		t.Fatalf("answer = %q", answer)
	}
	if len(steps) != 2 || steps[0].Role != "billing" || steps[1].Role != "supervisor" {
		t.Fatalf("unexpected steps: %+v", steps)
	}
	if steps[0].Input != "核对退款状态" {
		t.Fatalf("worker input = %q", steps[0].Input)
	}
}
