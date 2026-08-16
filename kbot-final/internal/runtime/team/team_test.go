package team

import (
	"context"
	"strings"
	"testing"
)

func TestRunPipeline(t *testing.T) {
	members := []Member{
		{AgentID: "summarize", Role: "summarizer"},
		{AgentID: "translate", Role: "translator"},
	}
	// 模拟：summarizer 加前缀，translator 再加前缀，验证链式传递。
	run := func(_ context.Context, m Member, input string) (string, error) {
		return m.AgentID + "(" + input + ")", nil
	}
	out, steps, err := RunPipeline(context.Background(), members, "原文", run)
	if err != nil {
		t.Fatal(err)
	}
	if out != "translate(summarize(原文))" {
		t.Fatalf("unexpected pipeline output: %s", out)
	}
	if len(steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(steps))
	}
	// 第二步的输入应是第一步的输出。
	if steps[1].Input != steps[0].Output {
		t.Fatalf("pipeline did not chain: step1.out=%q step2.in=%q", steps[0].Output, steps[1].Input)
	}
}

func TestRunPipelineEmpty(t *testing.T) {
	_, _, err := RunPipeline(context.Background(), nil, "x", func(context.Context, Member, string) (string, error) { return "", nil })
	if err == nil {
		t.Fatal("expected error for empty pipeline")
	}
}

func TestRunSupervisor(t *testing.T) {
	supervisor := Member{AgentID: "sup", Role: "supervisor"}
	workers := []Member{
		{AgentID: "billing", Role: "billing"},
		{AgentID: "tech", Role: "tech"},
	}

	// supervisor 第一次路由到 billing，拿到结果后结束。
	calls := 0
	run := func(_ context.Context, m Member, input string) (string, error) {
		agentID := m.AgentID
		if agentID == "sup" {
			calls++
			if calls == 1 {
				return "ROUTE:billing", nil
			}
			return "DONE:已处理完毕", nil
		}
		return agentID + "-done", nil
	}
	route := func(out string) (string, string, bool) {
		if strings.HasPrefix(out, "DONE:") {
			return "", strings.TrimPrefix(out, "DONE:"), true
		}
		if strings.HasPrefix(out, "ROUTE:") {
			return strings.TrimPrefix(out, "ROUTE:"), "", false
		}
		return "", "", false
	}

	final, steps, err := RunSupervisor(context.Background(), supervisor, workers, "我要退款", run, route, 8)
	if err != nil {
		t.Fatal(err)
	}
	if final != "已处理完毕" {
		t.Fatalf("unexpected final: %q", final)
	}
	// supervisor → billing worker → supervisor(done) = 3 steps
	if len(steps) != 3 {
		t.Fatalf("expected 3 steps, got %d: %+v", len(steps), steps)
	}
	if steps[1].Role != "billing" {
		t.Fatalf("expected billing worker at step 2, got %s", steps[1].Role)
	}
}

func TestRunSupervisorUnknownRole(t *testing.T) {
	run := func(_ context.Context, _ Member, _ string) (string, error) { return "ROUTE:ghost", nil }
	route := func(out string) (string, string, bool) { return "ghost", "", false }
	_, _, err := RunSupervisor(context.Background(),
		Member{AgentID: "sup", Role: "supervisor"}, nil, "x", run, route, 4)
	if err == nil {
		t.Fatal("expected error for unknown worker role")
	}
}
