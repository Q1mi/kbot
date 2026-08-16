package guard

import (
	"context"
	"regexp"
	"strings"
)

// InjectionRule 检测确定性 Prompt Injection 模式。
type InjectionRule struct {
	patterns []*regexp.Regexp
}

// 经典的低级注入特征。
var defaultInjectionPatterns = []string{
	`(?i)ignore\s+(all\s+)?previous\s+instructions`,
	`(?i)ignore\s+the\s+above`,
	`(?i)disregard\s+(all\s+)?(previous|prior)\s+instructions`,
	`(?i)you\s+are\s+now\s+`,
	`(?i)forget\s+(everything|all\s+previous)`,
	`(?i)reveal\s+(your\s+)?(system\s+prompt|instructions)`,
	`(?i)output\s+(your\s+)?system\s+prompt`,
	`(?i)<\|?\s*system\s*\|?>`,
	`(?i)忽略(之前|以上|上面)(的)?(所有)?(指令|指示|提示)`,
	`(?i)(显示|输出|告诉我)(你的)?(系统提示词|system prompt)`,
}

// NewInjectionRule 创建注入检测规则。
func NewInjectionRule() *InjectionRule {
	r := &InjectionRule{}
	for _, p := range defaultInjectionPatterns {
		r.patterns = append(r.patterns, regexp.MustCompile(p))
	}
	return r
}

func (r *InjectionRule) Name() string { return "prompt_injection" }
func (r *InjectionRule) Hook() Hook   { return HookOnInput }

func (r *InjectionRule) Check(_ context.Context, payload any) Decision {
	text, ok := payload.(string)
	if !ok {
		return Allowed()
	}
	for _, re := range r.patterns {
		if re.MatchString(text) {
			return Block("检测到疑似 Prompt 注入：" + firstMatch(re, text))
		}
	}
	return Allowed()
}

func firstMatch(re *regexp.Regexp, text string) string {
	m := re.FindString(text)
	return strings.TrimSpace(m)
}
