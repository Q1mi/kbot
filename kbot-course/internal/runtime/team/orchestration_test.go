package team

import (
	"context"
	"testing"
)

func TestPipelinePassesOutputToNextPinnedMember(t *testing.T) {
	members := []Member{{AgentID: "writer", AgentVersionID: "writer-v1", Role: "write"}, {AgentID: "reviewer", AgentVersionID: "reviewer-v2", Role: "review"}}
	final, steps, err := RunPipeline(context.Background(), members, "draft", func(_ context.Context, member Member, input string) (string, error) {
		return input + ":" + member.AgentVersionID, nil
	})
	if err != nil || final != "draft:writer-v1:reviewer-v2" || len(steps) != 2 {
		t.Fatalf("final=%q steps=%+v err=%v", final, steps, err)
	}
}
