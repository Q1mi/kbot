// Package skill 负责解析和版本化 Agent Skill。
package skill

import "errors"

var ErrNotImplemented = errors.New("skill parser is implemented in 13-end")

type Package struct {
	Name         string
	Description  string
	Instructions string
	AllowedTools []string
	MaxSteps     int
}

func ParseSkillMD([]byte) (Package, error) { return Package{}, ErrNotImplemented }
