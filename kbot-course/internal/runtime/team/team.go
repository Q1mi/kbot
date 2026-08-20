// Package team 通过固定 Agent 版本组织多 Agent 协作。
//
// Pipeline 需要严格的执行顺序，因此在本包保留确定性流程；Supervisor 由
// runtime/engine 使用 Eino ADK ChatModelAgent + AgentTool 装配。
package team

import (
	"context"
	"fmt"
	"strings"
)

type Mode string

const (
	ModeSupervisor Mode = "supervisor"
	ModePipeline   Mode = "pipeline"
)

type Member struct {
	Name           string `json:"name,omitempty"`
	AgentID        string `json:"agent_id,omitempty"`
	AgentVersionID string `json:"agent_version_id"`
	Role           string `json:"role,omitempty"`
}

type Step struct {
	Role    string `json:"role"`
	AgentID string `json:"agent_id"`
	Input   string `json:"input"`
	Output  string `json:"output"`
}

type MemberRunner func(context.Context, Member, string) (string, error)

func RunPipeline(ctx context.Context, members []Member, input string, run MemberRunner) (string, []Step, error) {
	if len(members) == 0 || run == nil {
		return "", nil, fmt.Errorf("pipeline members and runner are required")
	}
	current := input
	steps := make([]Step, 0, len(members))
	for _, member := range members {
		output, err := run(ctx, member, current)
		if err != nil {
			return "", steps, fmt.Errorf("pipeline member %s: %w", member.Role, err)
		}
		steps = append(steps, Step{Role: member.Role, AgentID: member.AgentID, Input: current, Output: output})
		current = output
	}
	return current, steps, nil
}

// Coordinator 保留本课 start 引入的定向分发接口，适合调试单个固定版本成员。
type Worker interface {
	Run(context.Context, string, string) (string, error)
}

type Coordinator struct {
	members map[string]Member
	worker  Worker
}

func NewCoordinator(members []Member, worker Worker) (*Coordinator, error) {
	if len(members) == 0 || worker == nil {
		return nil, fmt.Errorf("members and worker are required")
	}
	coordinator := &Coordinator{members: make(map[string]Member, len(members)), worker: worker}
	for _, member := range members {
		if strings.TrimSpace(member.Name) == "" || strings.TrimSpace(member.AgentVersionID) == "" {
			return nil, fmt.Errorf("member name and agent version are required")
		}
		if _, exists := coordinator.members[member.Name]; exists {
			return nil, fmt.Errorf("duplicate member %q", member.Name)
		}
		coordinator.members[member.Name] = member
	}
	return coordinator, nil
}

func (c *Coordinator) Dispatch(ctx context.Context, memberName, task string) (string, error) {
	member, ok := c.members[memberName]
	if !ok {
		return "", fmt.Errorf("team member %q not found", memberName)
	}
	answer, err := c.worker.Run(ctx, member.AgentVersionID, task)
	if err != nil {
		return "", fmt.Errorf("member %s: %w", memberName, err)
	}
	return answer, nil
}
