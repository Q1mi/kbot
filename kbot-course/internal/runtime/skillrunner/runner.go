// Package skillrunner 将已发布 Skill 应用到一次运行快照。
package skillrunner

import (
	"fmt"
	"strings"

	"github.com/Q1mi/kbot/internal/platform/skill"
	"github.com/Q1mi/kbot/internal/runtime/tooling"
)

type Applied struct {
	SystemPrompt string
	Tools        []tooling.Binding
	MaxSteps     int
}

// Apply 同时收敛提示词、工具权限和最大步数，避免 Skill 获得未声明能力。
func Apply(basePrompt string, pkg skill.Package, available []tooling.Binding) (Applied, error) {
	allowed := make(map[string]struct{}, len(pkg.AllowedTools))
	for _, name := range pkg.AllowedTools {
		allowed[name] = struct{}{}
	}
	byName := make(map[string]tooling.Binding, len(available))
	for _, binding := range available {
		byName[binding.Name] = binding
	}
	tools := make([]tooling.Binding, 0, len(allowed))
	for _, name := range pkg.AllowedTools {
		binding, ok := byName[name]
		if !ok {
			return Applied{}, fmt.Errorf("skill requires unavailable tool %q", name)
		}
		if binding.RequiresNetwork && !pkg.RequiresNetwork {
			return Applied{}, fmt.Errorf("skill must declare requires_network for tool %q", name)
		}
		if binding.KBScoped {
			binding.RestrictKBs = true
			binding.AllowedKBs = append([]string(nil), pkg.AllowedKBs...)
		}
		tools = append(tools, binding)
	}
	prompt := strings.TrimSpace(basePrompt) + "\n\n## Skill: " + pkg.Name + "\n" + pkg.Instructions
	return Applied{SystemPrompt: prompt, Tools: tools, MaxSteps: pkg.MaxSteps}, nil
}

func L1(packages []skill.Package) string {
	if len(packages) == 0 {
		return ""
	}
	var lines []string
	for _, pkg := range packages {
		if pkg.DisableModelInvocation {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", pkg.Name, pkg.Description))
	}
	if len(lines) == 0 {
		return ""
	}
	return "\n\n## Available skills\n" + strings.Join(lines, "\n") + "\nUse /skill <name> to activate one skill."
}

func Select(input string, packages []skill.Package) (skill.Package, bool) {
	normalized := strings.ToLower(strings.TrimSpace(input))
	for _, pkg := range packages {
		explicit := strings.HasPrefix(normalized, "/skill "+strings.ToLower(pkg.Name))
		if explicit || (!pkg.DisableModelInvocation && strings.Contains(normalized, strings.ToLower(pkg.Name))) {
			return pkg, true
		}
	}
	return skill.Package{}, false
}
