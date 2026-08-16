package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
)

// JudgeResult 单次评判结果。
type JudgeResult struct {
	Score  float64 // 0..1
	Reason string
}

// Judge 评判一个实际输出相对期望的得分。
type Judge interface {
	Kind() string // deterministic / light / full
	Score(ctx context.Context, expected, actual string) JudgeResult
}

// LLMJudge 用一个独立 Agent 评判语义正确性，适合关键词规则覆盖不到的开放式答案。
type LLMJudge struct {
	Runner  Target
	Tier    string
	AgentID string
}

func (j LLMJudge) Kind() string {
	if j.Tier == "light" {
		return "llm-light:" + j.AgentID
	}
	return "llm-full:" + j.AgentID
}

func (j LLMJudge) Score(ctx context.Context, expected, actual string) JudgeResult {
	if j.Runner == nil {
		return JudgeResult{Score: 0, Reason: "llm judge runner is not configured"}
	}
	expectedJSON, _ := json.Marshal(expected)
	actualJSON, _ := json.Marshal(actual)
	prompt := fmt.Sprintf(`你是严格的 AI 输出评测 Judge。请比较 expected 与 actual 的语义正确性、完整性和事实一致性。
仅输出 JSON：{"score":0到1之间的数字,"reason":"简短中文理由"}。
expected=%s
actual=%s`, expectedJSON, actualJSON)
	out, err := j.Runner(ctx, prompt)
	if err != nil {
		return JudgeResult{Score: 0, Reason: "llm judge error: " + err.Error()}
	}
	start, end := strings.Index(out, "{"), strings.LastIndex(out, "}")
	if start < 0 || end < start {
		return JudgeResult{Score: 0, Reason: "llm judge returned invalid JSON"}
	}
	var result struct {
		Score  float64 `json:"score"`
		Reason string  `json:"reason"`
	}
	if err := json.Unmarshal([]byte(out[start:end+1]), &result); err != nil {
		return JudgeResult{Score: 0, Reason: "llm judge returned invalid JSON: " + err.Error()}
	}
	if math.IsNaN(result.Score) || math.IsInf(result.Score, 0) {
		result.Score = 0
	}
	if result.Score < 0 {
		result.Score = 0
	}
	if result.Score > 1 {
		result.Score = 1
	}
	if strings.TrimSpace(result.Reason) == "" {
		result.Reason = "llm semantic judge"
	}
	return JudgeResult{Score: result.Score, Reason: result.Reason}
}

// --- 第一层：确定性 Judge（零成本，§15.3 三层 Judge 池的底层）---

// ContainsJudge 期望是若干关键词（空白/逗号分隔），actual 全部包含才算通过。
type ContainsJudge struct{}

func (ContainsJudge) Kind() string { return "deterministic" }
func (ContainsJudge) Score(_ context.Context, expected, actual string) JudgeResult {
	kws := splitKeywords(expected)
	if len(kws) == 0 {
		return JudgeResult{Score: 1, Reason: "no keywords"}
	}
	hit := 0
	for _, k := range kws {
		if strings.Contains(actual, k) {
			hit++
		}
	}
	score := float64(hit) / float64(len(kws))
	reason := "matched " + itoa(hit) + "/" + itoa(len(kws)) + " keywords"
	return JudgeResult{Score: score, Reason: reason}
}

// ExactJudge 完全相等（去首尾空白）。
type ExactJudge struct{}

func (ExactJudge) Kind() string { return "deterministic" }
func (ExactJudge) Score(_ context.Context, expected, actual string) JudgeResult {
	if strings.TrimSpace(expected) == strings.TrimSpace(actual) {
		return JudgeResult{Score: 1, Reason: "exact match"}
	}
	return JudgeResult{Score: 0, Reason: "not exact"}
}

// RegexJudge 期望是正则，actual 命中即通过。
type RegexJudge struct{}

func (RegexJudge) Kind() string { return "deterministic" }
func (RegexJudge) Score(_ context.Context, expected, actual string) JudgeResult {
	re, err := regexp.Compile(expected)
	if err != nil {
		return JudgeResult{Score: 0, Reason: "bad regex: " + err.Error()}
	}
	if re.MatchString(actual) {
		return JudgeResult{Score: 1, Reason: "regex matched"}
	}
	return JudgeResult{Score: 0, Reason: "regex not matched"}
}

// JudgeByMethod 按 method 名取确定性 Judge。
func JudgeByMethod(method string) Judge {
	switch method {
	case "exact":
		return ExactJudge{}
	case "regex":
		return RegexJudge{}
	default:
		return ContainsJudge{}
	}
}

func splitKeywords(s string) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '，' || r == ' ' || r == '\n' || r == '\t'
	})
	var out []string
	for _, f := range fields {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
