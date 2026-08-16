package guard

import (
	"context"
	"regexp"
)

// PIIRule 对文本做 PII 脱敏（输入/输出双向，设计文档 §4.13）。匹配到的敏感片段
// 用占位符替换，返回 Patched 让链路继续用脱敏后的文本。
type PIIRule struct {
	hook     Hook
	patterns []piiPattern
}

type piiPattern struct {
	re    *regexp.Regexp
	label string
}

// 常见 PII：邮箱、手机号、身份证、银行卡。规则可在 Admin Console 扩展（PiiPolicy）。
var defaultPIIPatterns = []piiPattern{
	{regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`), "[EMAIL]"},
	{regexp.MustCompile(`\b1[3-9]\d{9}\b`), "[PHONE]"},    // 中国大陆手机号
	{regexp.MustCompile(`\b\d{17}[\dXx]\b`), "[ID_CARD]"}, // 18 位身份证
	{regexp.MustCompile(`\b\d{16,19}\b`), "[CARD]"},       // 银行卡号
}

// NewPIIRule 创建 PII 脱敏规则，绑定到给定 hook（OnInput 或 OnOutput）。
func NewPIIRule(hook Hook) *PIIRule {
	return &PIIRule{hook: hook, patterns: defaultPIIPatterns}
}

func (r *PIIRule) Name() string { return "pii_mask" }
func (r *PIIRule) Hook() Hook   { return r.hook }

func (r *PIIRule) Check(_ context.Context, payload any) Decision {
	text, ok := payload.(string)
	if !ok {
		return Allowed()
	}
	masked := text
	changed := false
	for _, p := range r.patterns {
		if p.re.MatchString(masked) {
			masked = p.re.ReplaceAllString(masked, p.label)
			changed = true
		}
	}
	if !changed {
		return Allowed()
	}
	return Patch(masked, "PII 脱敏")
}

// Mask 暴露纯函数版脱敏，便于复用与测试。
func Mask(text string) string {
	for _, p := range defaultPIIPatterns {
		text = p.re.ReplaceAllString(text, p.label)
	}
	return text
}
