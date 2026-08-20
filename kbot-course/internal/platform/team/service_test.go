package team

import (
	"context"
	"testing"
)

type fakeResolver struct{ current map[string]string }

func (r fakeResolver) ResolveVersion(_ context.Context, _, agentID, environment string) (string, error) {
	return r.current[agentID+":"+environment], nil
}

func TestTeamVersionPinsAgentVersions(t *testing.T) {
	service := NewService(fakeResolver{current: map[string]string{"writer:dev": "writer-v1", "reviewer:dev": "reviewer-v2"}})
	item, version, err := service.Create(context.Background(), "ws-1", "content", "pipeline", []Member{{AgentID: "writer", Role: "write"}, {AgentID: "reviewer", Role: "review"}})
	if err != nil {
		t.Fatal(err)
	}
	if version.Members[0].AgentVersionID != "writer-v1" || version.Members[1].AgentVersionID != "reviewer-v2" {
		t.Fatalf("members=%+v", version.Members)
	}
	spec, err := service.RunSpec(t.Context(), "ws-1", item.ID, "dev")
	if err != nil || spec.Members[0].AgentVersionID != "writer-v1" {
		t.Fatalf("spec=%+v err=%v", spec, err)
	}
}
