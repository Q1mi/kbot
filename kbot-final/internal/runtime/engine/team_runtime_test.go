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
	appended  []string
	traceID   string
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

func (p *teamPlatform) AppendMessage(_ context.Context, conversationID, role, content string) error {
	p.appended = append(p.appended, conversationID+":"+role+":"+content)
	return nil
}

func (p *teamPlatform) RecordConversationTraceID(_ context.Context, _ string, traceID string) error {
	p.traceID = traceID
	return nil
}

type teamGuard struct {
	input  string
	output string
}

func (g *teamGuard) OnInput(_ context.Context, text string) (string, error) {
	g.input = text
	return "已脱敏的团队请求", nil
}

func (g *teamGuard) OnOutput(_ context.Context, text string) (string, error) {
	g.output = text
	return "已检查的团队答复", nil
}

type teamAudit struct {
	actions []string
}

func (a *teamAudit) RecordConversation(_ context.Context, _, _, action, _ string) {
	a.actions = append(a.actions, action)
}

func (*teamAudit) RecordSkillTrigger(context.Context, string, string, string, string, string) {}

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

func TestRunSupervisorTeamAppliesGuardAndPersistsConversation(t *testing.T) {
	platform := &teamPlatform{snapshots: map[string]*AgentSnapshot{
		"supervisor-v1": {
			ID: "supervisor-v1", AgentID: "supervisor", WorkspaceID: "w1",
			SystemPrompt: "你是客服主管", MaxSteps: 2,
		},
		"worker-v1": {
			ID: "worker-v1", AgentID: "worker", WorkspaceID: "w1", MaxSteps: 2,
		},
	}}
	chatModel := &scriptedChatModel{replies: []*schema.Message{
		schema.AssistantMessage("模型生成的团队答复", nil),
	}}
	guard := &teamGuard{}
	audit := &teamAudit{}
	engine := NewEngineWithChatModel(platform, chatModel, nil).WithGuard(guard).WithAudit(audit)

	answer, steps, err := engine.RunSupervisorTeam(
		context.Background(),
		TeamMember{AgentID: "supervisor", AgentVersionID: "supervisor-v1", Role: "supervisor"},
		[]TeamMember{{AgentID: "worker", AgentVersionID: "worker-v1", Role: "worker"}},
		"包含敏感信息的团队请求", "w1", "u1",
	)
	if err != nil {
		t.Fatalf("run supervisor team: %v", err)
	}
	if answer != "已检查的团队答复" {
		t.Fatalf("answer = %q", answer)
	}
	if guard.input != "包含敏感信息的团队请求" || guard.output != "模型生成的团队答复" {
		t.Fatalf("unexpected guard inputs: input=%q output=%q", guard.input, guard.output)
	}
	if len(platform.appended) != 2 ||
		platform.appended[0] != "supervisor-v1-conversation:user:已脱敏的团队请求" ||
		platform.appended[1] != "supervisor-v1-conversation:assistant:已检查的团队答复" {
		t.Fatalf("unexpected conversation messages: %v", platform.appended)
	}
	if platform.traceID == "" {
		t.Fatal("supervisor conversation trace id was not recorded")
	}
	if len(audit.actions) != 1 || audit.actions[0] != "team_turn" {
		t.Fatalf("unexpected audit actions: %v", audit.actions)
	}
	if len(steps) != 1 || steps[0].Input != "已脱敏的团队请求" || steps[0].Output != answer {
		t.Fatalf("unexpected team steps: %+v", steps)
	}
	foundSanitizedInput := false
	for _, received := range chatModel.received {
		for _, message := range received {
			if message.Role == schema.User && message.Content == "已脱敏的团队请求" {
				foundSanitizedInput = true
			}
		}
	}
	if !foundSanitizedInput {
		t.Fatalf("supervisor model did not receive sanitized input: %+v", chatModel.received)
	}
}
