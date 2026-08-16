package eval

import (
	"context"
	"strings"
	"testing"
)

func TestDeterministicJudges(t *testing.T) {
	ctx := context.Background()

	if jr := (ContainsJudge{}).Score(ctx, "退款 七天", "您可在七天内申请退款"); jr.Score != 1 {
		t.Fatalf("contains: expected full match, got %v (%s)", jr.Score, jr.Reason)
	}
	if jr := (ContainsJudge{}).Score(ctx, "退款 发票", "只提到退款"); jr.Score == 1 {
		t.Fatal("contains: expected partial (<1)")
	}
	if jr := (ExactJudge{}).Score(ctx, "ok", " ok "); jr.Score != 1 {
		t.Fatal("exact: expected trim-equal match")
	}
	if jr := (RegexJudge{}).Score(ctx, `^\d{4}$`, "2024"); jr.Score != 1 {
		t.Fatal("regex: expected match")
	}
}

func TestLLMJudgeParsesAndClampsJSONScore(t *testing.T) {
	judge := LLMJudge{Tier: "full", AgentID: "judge-agent", Runner: func(_ context.Context, prompt string) (string, error) {
		if !strings.Contains(prompt, "expected") || !strings.Contains(prompt, "actual") {
			t.Fatalf("judge prompt missing comparison payload: %s", prompt)
		}
		return "```json\n{\"score\":1.2,\"reason\":\"语义一致\"}\n```", nil
	}}
	result := judge.Score(context.Background(), "允许退款", "可以申请退款")
	if result.Score != 1 || result.Reason != "语义一致" || judge.Kind() != "llm-full:judge-agent" {
		t.Fatalf("unexpected LLM judge result: %+v kind=%s", result, judge.Kind())
	}
}

func buildDataset(t *testing.T) (*Service, string) {
	t.Helper()
	svc := NewService(NewMemoryStore())
	ctx := context.Background()
	d, err := svc.CreateDataset(ctx, "w1", "golden-refund", "agent")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = svc.AddCase(ctx, d.ID, "怎么退款", "退款", "")
	_, _ = svc.AddCase(ctx, d.ID, "多久到账", "工作日", "")
	_, _ = svc.AddCase(ctx, d.ID, "发票怎么开", "发票", "")
	return svc, d.ID
}

func TestRunPasses(t *testing.T) {
	svc, dsID := buildDataset(t)
	// stub target：回答里恰好包含期望关键词。
	target := func(_ context.Context, input string) (string, error) {
		switch {
		case strings.Contains(input, "退款"):
			return "您可在七天内申请退款", nil
		case strings.Contains(input, "到账"):
			return "3 个工作日到账", nil
		default:
			return "可开具增值税发票", nil
		}
	}
	res, err := svc.Run(context.Background(), RunRequest{
		DatasetID: dsID, TargetID: "agent:x", Judge: ContainsJudge{}, Threshold: 0.85,
	}, target)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed || res.PassRate != 1 {
		t.Fatalf("expected pass rate 1.0 passed, got %+v", res)
	}
}

func TestRunFailsBelowThreshold(t *testing.T) {
	svc, dsID := buildDataset(t)
	// stub target：总是答错。
	target := func(_ context.Context, _ string) (string, error) {
		return "我不知道", nil
	}
	res, err := svc.Run(context.Background(), RunRequest{
		DatasetID: dsID, TargetID: "agent:x", Judge: ContainsJudge{}, Threshold: 0.85,
	}, target)
	if err != nil {
		t.Fatal(err)
	}
	if res.Passed {
		t.Fatalf("expected failure below threshold, got %+v", res)
	}
}

// TestEvalGate 是 CI 门禁的离线版（确定性 Judge，无需 LLM）：通过率低于阈值即失败 = 阻断合并。
func TestEvalGate(t *testing.T) {
	svc, dsID := buildDataset(t)
	target := func(_ context.Context, input string) (string, error) {
		switch {
		case strings.Contains(input, "退款"):
			return "七天内可退款", nil
		case strings.Contains(input, "到账"):
			return "工作日内到账", nil
		default:
			return "可开发票", nil
		}
	}
	res, err := svc.Run(context.Background(), RunRequest{
		DatasetID: dsID, TargetID: "agent:refund-bot", Judge: ContainsJudge{}, Threshold: 0.85,
	}, target)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed {
		t.Fatalf("eval gate failed: pass rate %.2f < 0.85", res.PassRate)
	}
}
