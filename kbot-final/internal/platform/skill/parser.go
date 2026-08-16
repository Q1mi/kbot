package skill

import (
	"errors"
	"strings"

	"gopkg.in/yaml.v3"
)

// 解析错误。
var (
	ErrNoFrontmatter = errors.New("SKILL.md 缺少 YAML frontmatter（需以 \"---\\n\" 开头）")
	ErrMalformed     = errors.New("SKILL.md frontmatter 格式错误（缺少结束的 \"---\"）")
)

// Frontmatter 是 SKILL.md 的 YAML 头（设计文档 §4.4 / 讲义 §14.5）。
type Frontmatter struct {
	Name                   string   `yaml:"name" json:"name"`
	Description            string   `yaml:"description" json:"description"`
	AllowedTools           []string `yaml:"allowed-tools" json:"allowed_tools"`
	AllowedKBs             []string `yaml:"allowed-kbs" json:"allowed_kbs"`
	DisableModelInvocation bool     `yaml:"disable-model-invocation" json:"disable_model_invocation"`
	RequiresNetwork        bool     `yaml:"requires_network,omitempty" json:"requires_network,omitempty"`
}

// ParseSkill 把 "---\n<yaml>\n---\n<markdown>" 拆成 frontmatter 与 body。
func ParseSkill(raw string) (Frontmatter, string, error) {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	if !strings.HasPrefix(raw, "---\n") {
		return Frontmatter{}, "", ErrNoFrontmatter
	}
	parts := strings.SplitN(raw[4:], "\n---\n", 2)
	if len(parts) != 2 {
		return Frontmatter{}, "", ErrMalformed
	}
	var fm Frontmatter
	if err := yaml.Unmarshal([]byte(parts[0]), &fm); err != nil {
		return Frontmatter{}, "", err
	}
	return fm, strings.TrimLeft(parts[1], "\n"), nil
}
