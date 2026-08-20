package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

type JudgeResult struct {
	Score  float64 `json:"score"`
	Reason string  `json:"reason"`
}

type Judge interface {
	Kind() string
	Score(ctx context.Context, expected, actual string) JudgeResult
}

type ContainsJudge struct{}

func (ContainsJudge) Kind() string { return "deterministic" }

func (ContainsJudge) Score(_ context.Context, expected, actual string) JudgeResult {
	if expected == "" || strings.Contains(strings.ToLower(actual), strings.ToLower(expected)) {
		return JudgeResult{Score: 1, Reason: "expected content matched"}
	}
	return JudgeResult{Score: 0, Reason: "expected content missing"}
}

type LLMJudge struct {
	Tier   string
	Runner func(context.Context, string) (string, error)
}

func (j LLMJudge) Kind() string {
	if j.Tier == "light" {
		return "llm-light"
	}
	return "llm-full"
}

func (j LLMJudge) Score(ctx context.Context, expected, actual string) JudgeResult {
	if j.Runner == nil {
		return JudgeResult{Reason: "LLM judge runner is required"}
	}
	prompt := fmt.Sprintf("Evaluate semantic correctness. Return JSON only: {\"score\":0..1,\"reason\":\"...\"}.\nexpected: %s\nactual: %s", expected, actual)
	raw, err := j.Runner(ctx, prompt)
	if err != nil {
		return JudgeResult{Reason: "LLM judge error: " + err.Error()}
	}
	start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if start < 0 || end < start {
		return JudgeResult{Reason: "LLM judge returned invalid JSON"}
	}
	var result JudgeResult
	if err := json.Unmarshal([]byte(raw[start:end+1]), &result); err != nil || math.IsNaN(result.Score) || math.IsInf(result.Score, 0) {
		return JudgeResult{Reason: "LLM judge returned invalid score"}
	}
	result.Score = math.Max(0, math.Min(1, result.Score))
	return result
}
