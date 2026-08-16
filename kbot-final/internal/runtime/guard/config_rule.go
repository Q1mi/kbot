package guard

import (
	"context"
	"fmt"
	"regexp"
)

// ConfigRule 是从 guard_rules 表装配的正则规则。
type ConfigRule struct {
	id     string
	kind   string
	hook   Hook
	action string
	re     *regexp.Regexp
}

func NewConfigRule(id, kind string, hook Hook, pattern, action string) (*ConfigRule, error) {
	if hook != HookOnInput && hook != HookOnOutput && hook != HookOnToolCall && hook != HookOnLLMCall {
		return nil, fmt.Errorf("invalid guard hook: %s", hook)
	}
	if action != "block" && action != "warn" && action != "redact" {
		return nil, fmt.Errorf("invalid guard action: %s", action)
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("compile guard pattern: %w", err)
	}
	return &ConfigRule{id: id, kind: kind, hook: hook, action: action, re: re}, nil
}

func (r *ConfigRule) Name() string { return "config:" + r.kind + ":" + r.id }
func (r *ConfigRule) Hook() Hook   { return r.hook }

func (r *ConfigRule) Check(_ context.Context, payload any) Decision {
	text, ok := payload.(string)
	if !ok || !r.re.MatchString(text) {
		return Allowed()
	}
	reason := "matched configured " + r.kind + " rule"
	switch r.action {
	case "redact":
		return Patch(r.re.ReplaceAllString(text, "[REDACTED]"), reason)
	case "warn":
		return Patch(text, reason)
	default:
		return Block(reason)
	}
}
