// Package team 实现确定性的 Pipeline 编排（设计文档 §4.9 / 讲义 §15.8）。
//
// Supervisor 使用 Eino ADK ChatModelAgent + AgentTool，装配入口位于 runtime/engine。
// Pipeline 需要严格的顺序与输入传递，因此保留这里的确定性工作流。
package team

import (
	"context"
	"fmt"
)

// Mode 协作模式。
type Mode string

const (
	ModeSupervisor Mode = "supervisor" // 主管路由到不同 worker
	ModePipeline   Mode = "pipeline"   // 流水线：上一个输出喂下一个
)

// Member 是 Team 的一个成员。
type Member struct {
	AgentID        string `json:"agent_id"`
	AgentVersionID string `json:"agent_version_id,omitempty"` // pin 死的成员 agent 版本(空则退化为当前版本,如 ad-hoc 调试)
	Role           string `json:"role"`                       // 角色名，Supervisor 用它路由
}

// Step 记录一次成员调用，用于 trace / 成本归因（每子 Agent 单独入账，§4.9）。
type Step struct {
	Role    string `json:"role"`
	AgentID string `json:"agent_id"`
	Input   string `json:"input"`
	Output  string `json:"output"`
}

// MemberRunner 跑一个成员 Agent：给定成员(含 pin 的版本)与输入，返回最终文本。
// 传整个 Member 而非裸 agentID,是为了让运行端能按 AgentVersionID pin 版本(讲义 §14.6)。
type MemberRunner func(ctx context.Context, m Member, input string) (string, error)

// RunPipeline 顺序跑各成员，上一个的输出作为下一个的输入。
func RunPipeline(ctx context.Context, members []Member, input string, run MemberRunner) (string, []Step, error) {
	if len(members) == 0 {
		return "", nil, fmt.Errorf("pipeline has no members")
	}
	var steps []Step
	cur := input
	for _, m := range members {
		out, err := run(ctx, m, cur)
		if err != nil {
			return "", steps, fmt.Errorf("pipeline member %s (%s): %w", m.Role, m.AgentID, err)
		}
		steps = append(steps, Step{Role: m.Role, AgentID: m.AgentID, Input: cur, Output: out})
		cur = out
	}
	return cur, steps, nil
}
