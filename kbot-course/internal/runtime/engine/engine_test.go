package engine

import (
	"context"
	"fmt"
	"testing"

	"github.com/cloudwego/eino/schema"

	"github.com/Q1mi/kbot/internal/domain"
)

type fakeGenerator struct{}

func (fakeGenerator) Generate(
	context.Context,
	[]*schema.Message,
	[]*schema.ToolInfo,
) (*schema.Message, error) {
	return &schema.Message{}, nil
}

type fakePlatform struct {
	conversation     *domain.Conversation
	snapshots        map[string]*AgentSnapshot
	requestedVersion string
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

var _ Generator = fakeGenerator{}
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

	runtime := New(controlPlane, fakeGenerator{})
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
