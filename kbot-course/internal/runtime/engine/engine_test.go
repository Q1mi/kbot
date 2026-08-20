package engine

import (
	"context"
	"fmt"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/Q1mi/kbot/internal/domain"
)

type fakeChatModel struct{}

func (fakeChatModel) Generate(
	context.Context,
	[]*schema.Message,
	...model.Option,
) (*schema.Message, error) {
	return &schema.Message{}, nil
}

func (m fakeChatModel) Stream(
	ctx context.Context, messages []*schema.Message, opts ...model.Option,
) (*schema.StreamReader[*schema.Message], error) {
	response, err := m.Generate(ctx, messages, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{response}), nil
}

type fakePlatform struct {
	conversation     *domain.Conversation
	snapshots        map[string]*AgentSnapshot
	requestedVersion string
}

func (p *fakePlatform) CreateConversationForVersion(
	_ context.Context, workspaceID, agentID, versionID, userID string,
) (*domain.Conversation, error) {
	snapshot, ok := p.snapshots[versionID]
	if !ok || (snapshot.WorkspaceID != "" && snapshot.WorkspaceID != workspaceID) ||
		(snapshot.AgentID != "" && snapshot.AgentID != agentID) {
		return nil, fmt.Errorf("snapshot %s not found", versionID)
	}
	conversation := &domain.Conversation{
		ID: versionID + "-conversation", WorkspaceID: workspaceID, AgentID: agentID,
		AgentVersionID: versionID, UserID: userID,
	}
	p.conversation = conversation
	return conversation, nil
}

func (p *fakePlatform) LoadConversation(
	context.Context,
	string,
) (*domain.Conversation, error) {
	return p.conversation, nil
}

func (p *fakePlatform) GetAgentSnapshotByVersion(
	_ context.Context,
	agentVersionID string,
) (*AgentSnapshot, error) {
	p.requestedVersion = agentVersionID
	snapshot, ok := p.snapshots[agentVersionID]
	if !ok {
		return nil, fmt.Errorf("snapshot %s not found", agentVersionID)
	}
	return snapshot, nil
}

var _ model.BaseChatModel = fakeChatModel{}
var _ Platform = (*fakePlatform)(nil)

func TestResolveSnapshotUsesConversationPinnedVersion(t *testing.T) {
	t.Parallel()

	controlPlane := &fakePlatform{
		conversation: &domain.Conversation{
			ID:             "conversation-1",
			AgentID:        "agent-1",
			AgentVersionID: "agent-version-1",
		},
		snapshots: map[string]*AgentSnapshot{
			"agent-version-1": {
				ID:           "agent-version-1",
				AgentID:      "agent-1",
				SystemPrompt: "version one",
			},
			"agent-version-2": {
				ID:           "agent-version-2",
				AgentID:      "agent-1",
				SystemPrompt: "version two",
			},
		},
	}

	runtime := New(controlPlane, fakeChatModel{})
	snapshot, err := runtime.ResolveSnapshot(context.Background(), "conversation-1")
	if err != nil {
		t.Fatalf("resolve snapshot: %v", err)
	}
	if snapshot.ID != "agent-version-1" {
		t.Fatalf("snapshot ID = %q, want agent-version-1", snapshot.ID)
	}
	if controlPlane.requestedVersion != "agent-version-1" {
		t.Fatalf(
			"requested version = %q, want agent-version-1",
			controlPlane.requestedVersion,
		)
	}
}
