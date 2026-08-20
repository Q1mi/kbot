// Package eval 提供 Agent 版本发布前的离线回归评测。
package eval

import (
	"context"
	"fmt"
	"math"
	"strings"
)

type Case struct {
	ID, Input, ExpectedContains string
	Forbidden                   []string
	ExpectedTools               []string
}
type Output struct {
	Content string
	Tools   []string
}
type Agent interface {
	Run(context.Context, string) (Output, error)
}
type CaseResult struct {
	CaseID  string
	Passed  bool
	Score   float64
	Reasons []string
}
type Report struct {
	Results  []CaseResult
	PassRate float64
}
type Service struct{}

func NewService() *Service { return &Service{} }

func (s *Service) Run(ctx context.Context, cases []Case, agent Agent) (Report, error) {
	return s.RunWithJudge(ctx, cases, agent, ContainsJudge{})
}

func (s *Service) RunWithJudge(ctx context.Context, cases []Case, agent Agent, judge Judge) (Report, error) {
	if len(cases) == 0 || agent == nil || judge == nil {
		return Report{}, fmt.Errorf("cases, agent and judge are required")
	}
	seen := make(map[string]struct{}, len(cases))
	report := Report{Results: make([]CaseResult, 0, len(cases))}
	passed := 0
	for _, testCase := range cases {
		if err := ctx.Err(); err != nil {
			return Report{}, err
		}
		if testCase.ID == "" {
			return Report{}, fmt.Errorf("case id is required")
		}
		if _, exists := seen[testCase.ID]; exists {
			return Report{}, fmt.Errorf("duplicate case id %s", testCase.ID)
		}
		seen[testCase.ID] = struct{}{}
		result := CaseResult{CaseID: testCase.ID, Passed: true, Score: 1}
		output, err := agent.Run(ctx, testCase.Input)
		if err != nil {
			result.Passed = false
			result.Score = 0
			result.Reasons = append(result.Reasons, "run_error: "+err.Error())
		} else {
			scoredCase := testCase
			if judge.Kind() != "deterministic" {
				scoredCase.ExpectedContains = ""
			}
			scoreCase(scoredCase, output, &result)
			judged := judge.Score(ctx, testCase.ExpectedContains, output.Content)
			result.Score = judged.Score
			if judged.Score < 0.7 {
				result.Passed = false
				result.Reasons = append(result.Reasons, judge.Kind()+": "+judged.Reason)
			}
		}
		if result.Passed {
			passed++
		}
		report.Results = append(report.Results, result)
	}
	report.PassRate = float64(passed) / float64(len(cases))
	return report, nil
}

func scoreCase(testCase Case, output Output, result *CaseResult) {
	lower := strings.ToLower(output.Content)
	if testCase.ExpectedContains != "" && !strings.Contains(lower, strings.ToLower(testCase.ExpectedContains)) {
		result.Passed = false
		result.Reasons = append(result.Reasons, "expected content missing")
	}
	for _, forbidden := range testCase.Forbidden {
		if strings.Contains(lower, strings.ToLower(forbidden)) {
			result.Passed = false
			result.Reasons = append(result.Reasons, "forbidden content: "+forbidden)
		}
	}
	called := make(map[string]struct{}, len(output.Tools))
	for _, name := range output.Tools {
		called[name] = struct{}{}
	}
	for _, expected := range testCase.ExpectedTools {
		if _, ok := called[expected]; !ok {
			result.Passed = false
			result.Reasons = append(result.Reasons, "expected tool missing: "+expected)
		}
	}
}

func Gate(report Report, minimumPassRate float64) error {
	if math.IsNaN(minimumPassRate) || math.IsInf(minimumPassRate, 0) || minimumPassRate < 0 || minimumPassRate > 1 {
		return fmt.Errorf("minimum pass rate must be within [0,1]")
	}
	if math.IsNaN(report.PassRate) || math.IsInf(report.PassRate, 0) || report.PassRate < 0 || report.PassRate > 1 {
		return fmt.Errorf("report pass rate must be finite and within [0,1]")
	}
	if report.PassRate < minimumPassRate {
		return fmt.Errorf("evaluation gate failed: pass rate %.2f is below %.2f", report.PassRate, minimumPassRate)
	}
	return nil
}
