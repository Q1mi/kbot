// Package team implements deterministic Supervisor and Pipeline orchestration.
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
type Router func(supervisorOutput string) (nextRole, finalAnswer string, done bool)

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

func RunSupervisor(ctx context.Context, supervisor Member, workers []Member, input string, run MemberRunner, route Router, maxSteps int) (string, []Step, error) {
	if len(workers) == 0 || run == nil || route == nil {
		return "", nil, fmt.Errorf("supervisor, workers, runner and router are required")
	}
	if maxSteps <= 0 {
		maxSteps = 8
	}
	byRole := make(map[string]Member, len(workers))
	for _, worker := range workers {
		byRole[worker.Role] = worker
	}
	steps := make([]Step, 0, maxSteps)
	supervisorInput := input
	for range maxSteps {
		output, err := run(ctx, supervisor, supervisorInput)
		if err != nil {
			return "", steps, fmt.Errorf("supervisor: %w", err)
		}
		steps = append(steps, Step{Role: supervisor.Role, AgentID: supervisor.AgentID, Input: supervisorInput, Output: output})
		nextRole, final, done := route(output)
		if done {
			return final, steps, nil
		}
		worker, exists := byRole[nextRole]
		if !exists {
			return "", steps, fmt.Errorf("supervisor routed to unknown role %q", nextRole)
		}
		workerOutput, err := run(ctx, worker, input)
		if err != nil {
			return "", steps, fmt.Errorf("worker %s: %w", worker.Role, err)
		}
		steps = append(steps, Step{Role: worker.Role, AgentID: worker.AgentID, Input: input, Output: workerOutput})
		supervisorInput = "worker[" + worker.Role + "] returned: " + workerOutput
	}
	return "", steps, fmt.Errorf("supervisor reached max steps %d", maxSteps)
}

func RouteDirective(output string) (string, string, bool) {
	output = strings.TrimSpace(output)
	if strings.HasPrefix(output, "ROUTE:") {
		return strings.TrimSpace(strings.TrimPrefix(output, "ROUTE:")), "", false
	}
	if strings.HasPrefix(output, "DONE:") {
		return "", strings.TrimSpace(strings.TrimPrefix(output, "DONE:")), true
	}
	return "", output, true
}

// Coordinator keeps the focused dispatch API introduced in the lesson start.
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
