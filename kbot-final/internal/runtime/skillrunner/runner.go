// Package skillrunner 把平台固定版本的 Skill 快照适配到 Eino Skill middleware。
package skillrunner

import (
	"context"
	"fmt"
	"strings"

	einolskill "github.com/cloudwego/eino/adk/middlewares/skill"
)

// Backend 把平台快照中的 Skill Spec 适配为 Eino v0.9.15 的渐进式披露后端。
// specs 已在会话创建时 pin 到固定版本，因此一次运行内保持不可变。
type Backend struct {
	specs []Spec
}

func NewBackend(specs []Spec) *Backend {
	cloned := append([]Spec(nil), specs...)
	return &Backend{specs: cloned}
}

func (b *Backend) List(context.Context) ([]einolskill.FrontMatter, error) {
	out := make([]einolskill.FrontMatter, 0, len(b.specs))
	for _, spec := range b.specs {
		out = append(out, einolskill.FrontMatter{Name: spec.Name, Description: spec.Description})
	}
	return out, nil
}

func (b *Backend) Get(_ context.Context, name string) (einolskill.Skill, error) {
	spec, ok := Find(b.specs, name)
	if !ok {
		return einolskill.Skill{}, fmt.Errorf("skill %q is not available", name)
	}
	return einolskill.Skill{
		FrontMatter: einolskill.FrontMatter{Name: spec.Name, Description: spec.Description},
		Content:     spec.Body,
	}, nil
}

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

// DetectExplicit 识别用户显式调用：`/skill name` 或 `/name`。
// 这为 disable-model-invocation 提供可操作的人工入口。
func DetectExplicit(text string, specs []Spec) (name string, ok bool) {
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
