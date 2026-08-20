package team

import (
	"context"
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
