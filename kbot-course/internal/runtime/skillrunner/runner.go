// Package skillrunner 将已发布 Skill 应用到一次运行快照。
package skillrunner

import (
	"errors"

	"github.com/Q1mi/kbot/internal/platform/skill"
	"github.com/Q1mi/kbot/internal/runtime/engine"
)

var ErrNotImplemented = errors.New("skill policy is implemented in 13-end")

type Applied struct {
	SystemPrompt string
	Tools        []engine.ToolBinding
	MaxSteps     int
}

func Apply(string, skill.Package, []engine.ToolBinding) (Applied, error) {
	return Applied{}, ErrNotImplemented
}
