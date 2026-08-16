// Package team 实现多 Agent 编排（Supervisor / Pipeline，设计文档 §4.9 / 讲义 §15.8）。
//
// 本包只做"编排逻辑"——成员 Agent 怎么跑由 MemberRunner 注入（生产里包 engine.ChatStream，
// 测试里注入 stub）。这样编排策略可离线单测，且不与 LLM 耦合。
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

// Router 解析 Supervisor 的输出，决定下一步交给哪个 worker（按 Role），或结束。
// done=true 时 finalAnswer 为最终回答。
type Router func(supervisorOutput string) (nextRole string, finalAnswer string, done bool)

// RunSupervisor 跑 Supervisor 模式：主管看输入决定路由 → worker 处理 → 主管综合 →
// 再决定下一步或结束。maxSteps 防止无限往返。
func RunSupervisor(ctx context.Context, supervisor Member, workers []Member, input string, run MemberRunner, route Router, maxSteps int) (string, []Step, error) {
	if maxSteps <= 0 {
		maxSteps = 8
	}
	byRole := map[string]Member{}
	for _, w := range workers {
		byRole[w.Role] = w
	}

	var steps []Step
	supervisorInput := input
	for i := 0; i < maxSteps; i++ {
		supOut, err := run(ctx, supervisor, supervisorInput)
		if err != nil {
			return "", steps, fmt.Errorf("supervisor: %w", err)
		}
		steps = append(steps, Step{Role: "supervisor", AgentID: supervisor.AgentID, Input: supervisorInput, Output: supOut})

		nextRole, final, done := route(supOut)
		if done {
			return final, steps, nil
		}
		w, ok := byRole[nextRole]
		if !ok {
			return "", steps, fmt.Errorf("supervisor routed to unknown worker role %q", nextRole)
		}
		workerOut, err := run(ctx, w, input)
		if err != nil {
			return "", steps, fmt.Errorf("worker %s: %w", w.Role, err)
		}
		steps = append(steps, Step{Role: w.Role, AgentID: w.AgentID, Input: input, Output: workerOut})
		// 把 worker 结果回喂给主管，让它综合后再决定。
		supervisorInput = "worker[" + w.Role + "] 返回：" + workerOut
	}
	return "", steps, fmt.Errorf("supervisor reached max steps (%d) without finishing", maxSteps)
}
