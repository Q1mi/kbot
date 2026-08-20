// Package guard 在模型调用前后执行可组合安全规则。
package guard

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

type Decision struct {
	Allowed        bool     `json:"allowed"`
	SanitizedText  string   `json:"sanitized_text"`
	Classification string   `json:"classification"`
	Reasons        []string `json:"reasons,omitempty"`
}

type Rule interface {
	Evaluate(context.Context, string) (Decision, error)
}
type Pipeline struct{ rules []Rule }

func NewPipeline(rules ...Rule) *Pipeline { return &Pipeline{rules: append([]Rule(nil), rules...)} }

func (p *Pipeline) Evaluate(ctx context.Context, text string) (Decision, error) {
	decision := Decision{Allowed: true, SanitizedText: text, Classification: "public"}
	for _, rule := range p.rules {
		if err := ctx.Err(); err != nil {
			return Decision{}, err
		}
		result, err := rule.Evaluate(ctx, decision.SanitizedText)
		if err != nil {
			return Decision{}, fmt.Errorf("evaluate guard rule: %w", err)
		}
		if result.SanitizedText != "" {
			decision.SanitizedText = result.SanitizedText
		}
		if classificationRank(result.Classification) > classificationRank(decision.Classification) {
			decision.Classification = result.Classification
		}
		decision.Reasons = append(decision.Reasons, result.Reasons...)
		if !result.Allowed {
			decision.Allowed = false
			return decision, nil
		}
	}
	return decision, nil
}

type InjectionRule struct{}

func (InjectionRule) Evaluate(_ context.Context, text string) (Decision, error) {
	lower := strings.ToLower(text)
	patterns := []string{"ignore previous instructions", "reveal system prompt", "忽略之前的指令", "泄露系统提示词"}
	for _, pattern := range patterns {
		if strings.Contains(lower, pattern) {
			return Decision{Allowed: false, SanitizedText: text, Classification: "internal", Reasons: []string{"prompt_injection"}}, nil
		}
	}
	return Decision{Allowed: true, SanitizedText: text, Classification: "public"}, nil
}

type PIIRule struct{}

var emailPattern = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
var phonePattern = regexp.MustCompile(`\b1[3-9][0-9]{9}\b`)

func (PIIRule) Evaluate(_ context.Context, text string) (Decision, error) {
	redacted := emailPattern.ReplaceAllString(text, "[EMAIL]")
	redacted = phonePattern.ReplaceAllString(redacted, "[PHONE]")
	if redacted != text {
		return Decision{Allowed: true, SanitizedText: redacted, Classification: "confidential", Reasons: []string{"pii_redacted"}}, nil
	}
	return Decision{Allowed: true, SanitizedText: text, Classification: "public"}, nil
}

type MaxLengthRule struct{ MaxRunes int }

func (r MaxLengthRule) Evaluate(_ context.Context, text string) (Decision, error) {
	if r.MaxRunes <= 0 {
		return Decision{}, fmt.Errorf("positive max length is required")
	}
	if len([]rune(text)) > r.MaxRunes {
		return Decision{Allowed: false, SanitizedText: text, Classification: "public", Reasons: []string{"input_too_long"}}, nil
	}
	return Decision{Allowed: true, SanitizedText: text, Classification: "public"}, nil
}

func classificationRank(value string) int {
	switch value {
	case "secret":
		return 3
	case "confidential":
		return 2
	case "internal":
		return 1
	default:
		return 0
	}
}
