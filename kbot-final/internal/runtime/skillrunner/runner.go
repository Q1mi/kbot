// Package skillrunner 实现 Skill 的 L1 元数据注入与 <USE_SKILL> 元约定下的 L2 注入
// （设计文档 §4.4 / 讲义 §14.5）。
//
// 本包只处理"渐进式披露"的纯逻辑（拼元提示词、解析标记、决定工具范围），不依赖具体
// 存储或 LLM——技能以 Spec 形式传入，便于单测与复用。
package skillrunner

import (
	"regexp"
	"strings"
)

// Spec 是 Runtime 视角的技能（与 platform/skill.Spec 字段对应，避免跨层耦合）。
type Spec struct {
	VersionID              string
	Name                   string
	Description            string
	Body                   string
	AllowedTools           []string
	AllowedKBs             []string
	DisableModelInvocation bool
	RequiresNetwork        bool
}

// useSkillRE 匹配 <USE_SKILL>technical_name</USE_SKILL>（讲义 §14.5）。
var useSkillRE = regexp.MustCompile(`<USE_SKILL>([a-zA-Z0-9_\-]+)</USE_SKILL>`)

// BuildL1 把所有可用技能的 name+description 拼成元提示词，加进 system prompt。
// 模型据此知道"有哪些技能、何时用、怎么触发"。无技能时返回空串。
func BuildL1(specs []Spec) string {
	if len(specs) == 0 {
		return ""
	}
	s := "你可以使用以下技能。需要触发某个技能时，在回复里单独一行输出 " +
		"<USE_SKILL>技能名</USE_SKILL>（必须严格按此格式，不要混在自然语言里），" +
		"系统会注入该技能的完整流程，你下一轮按流程办事。可用技能：\n"
	visible := 0
	for _, sp := range specs {
		if sp.DisableModelInvocation {
			continue
		}
		s += "- " + sp.Name + "：" + sp.Description + "\n"
		visible++
	}
	if visible == 0 {
		return ""
	}
	return s
}

// Detect 从模型回复里解析 <USE_SKILL> 标记，返回技能名。
func Detect(text string) (name string, ok bool) {
	m := useSkillRE.FindStringSubmatch(text)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// DetectExplicit 识别用户显式调用：XML 标签、`/skill name` 或 `/name`。
// 这为 disable-model-invocation 提供可操作的人工入口。
func DetectExplicit(text string, specs []Spec) (name string, ok bool) {
	if name, ok := Detect(text); ok {
		return name, true
	}
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return "", false
	}
	if fields[0] == "/skill" && len(fields) >= 2 {
		name = fields[1]
	} else if strings.HasPrefix(fields[0], "/") {
		name = strings.TrimPrefix(fields[0], "/")
	}
	if name == "" {
		return "", false
	}
	_, ok = Find(specs, name)
	return name, ok
}

// L2Message 返回某技能 L2 注入用的 system 消息内容（body 作为新的 system 上下文）。
func L2Message(spec Spec) string {
	return "[激活技能 " + spec.Name + "]\n\n" + spec.Body
}

// Find 在 specs 里按名查找技能。
func Find(specs []Spec, name string) (Spec, bool) {
	for _, sp := range specs {
		if sp.Name == name {
			return sp, true
		}
	}
	return Spec{}, false
}
