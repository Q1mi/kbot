package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/schema"
	"go.opentelemetry.io/otel/attribute"

	courseotel "github.com/Q1mi/kbot/internal/infrastructure/otel"
	"github.com/Q1mi/kbot/internal/platform/approval"
	"github.com/Q1mi/kbot/internal/platform/audit"
	"github.com/Q1mi/kbot/internal/runtime/tooling"
)

type ToolBinding = tooling.Binding

type ToolExecutor interface {
	Execute(ctx context.Context, call tooling.Call) (tooling.Result, error)
}

type ReActRunner struct {
	gen         Generator
	executor    ToolExecutor
	workspaceID string
	approvals   ApprovalCreator
	runID       string
	audit       AuditSink
	actorID     string
}

func (r *ReActRunner) WithAudit(sink AuditSink, actorID string) *ReActRunner {
	r.audit, r.actorID = sink, actorID
	return r
}

func (r *ReActRunner) WithApprovals(approvals ApprovalCreator, runID string) *ReActRunner {
	r.approvals, r.runID = approvals, runID
	return r
}

type AwaitingApprovalError struct {
	ApprovalID    string
	ToolName      string
	ToolCallID    string
	ToolVersionID string
	Arguments     map[string]any
}

type reactCheckpoint struct {
	History        []*schema.Message `json:"history"`
	ToolVersionIDs []string          `json:"tool_version_ids"`
	MaxSteps       int               `json:"max_steps"`
}

func (e *AwaitingApprovalError) Error() string {
	return "tool call is awaiting approval: " + e.ApprovalID
}

func NewReActRunner(gen Generator, executor ToolExecutor, workspaceID string) *ReActRunner {
	return &ReActRunner{gen: gen, executor: executor, workspaceID: workspaceID}
}

func (r *ReActRunner) Run(ctx context.Context, messages []*schema.Message, bindings []ToolBinding, maxSteps int, emit Emitter) (*schema.Message, error) {
	if r.gen == nil || r.executor == nil {
		return nil, fmt.Errorf("generator and tool executor are required")
	}
	if maxSteps <= 0 {
		return nil, fmt.Errorf("max steps must be positive")
	}
	byName := make(map[string]ToolBinding, len(bindings))
	tools := make([]*schema.ToolInfo, 0, len(bindings))
	for _, binding := range bindings {
		byName[binding.Name] = binding
		tools = append(tools, binding.Info)
	}
	history := append([]*schema.Message(nil), messages...)
	for step := 1; step <= maxSteps; step++ {
		assistant, err := r.gen.Generate(ctx, history, tools)
		if err != nil {
			return nil, fmt.Errorf("generate step %d: %w", step, err)
		}
		history = append(history, assistant)
		if len(assistant.ToolCalls) == 0 {
			return assistant, nil
		}
		for _, call := range assistant.ToolCalls {
			binding, ok := byName[call.Function.Name]
			if !ok {
				history = append(history, schema.ToolMessage(fmt.Sprintf(`{"error":"tool %s is not available"}`, call.Function.Name), call.ID))
				continue
			}
			if err := validateBindingCall(binding, []byte(call.Function.Arguments)); err != nil {
				history = append(history, schema.ToolMessage(fmt.Sprintf(`{"error":%q}`, err.Error()), call.ID))
				continue
			}
			if binding.Sensitive {
				if r.approvals == nil {
					return nil, fmt.Errorf("sensitive tool %s requires an approval service", binding.Name)
				}
				versionIDs := make([]string, 0, len(bindings))
				for _, active := range bindings {
					versionIDs = append(versionIDs, active.VersionID)
				}
				checkpoint, marshalErr := json.Marshal(reactCheckpoint{History: history, ToolVersionIDs: versionIDs, MaxSteps: maxSteps})
				if marshalErr != nil {
					return nil, fmt.Errorf("encode approval checkpoint: %w", marshalErr)
				}
				created, createErr := r.approvals.Create(ctx, approval.Request{
					WorkspaceID: r.workspaceID, RunID: r.runID, ToolCallID: call.ID, ToolVersionID: binding.VersionID,
					Arguments: []byte(call.Function.Arguments), Checkpoint: checkpoint,
				})
				if createErr != nil {
					return nil, fmt.Errorf("create approval: %w", createErr)
				}
				var arguments map[string]any
				_ = json.Unmarshal([]byte(call.Function.Arguments), &arguments)
				return nil, &AwaitingApprovalError{ApprovalID: created.ID, ToolName: binding.Name, ToolCallID: call.ID, ToolVersionID: binding.VersionID, Arguments: arguments}
			}
			if emit != nil {
				if err := emitContext(ctx, emit, Event{Type: "tool_started", Data: map[string]any{"name": binding.Name, "call_id": call.ID}}); err != nil {
					return nil, err
				}
			}
			operation := "tool.execute"
			if binding.SourceType == "code_execution" {
				operation = "sandbox.execute"
			}
			toolContext, finishTool := courseotel.StartOperation(ctx, operation,
				attribute.String("gen_ai.tool.name", binding.Name),
				attribute.String("kbot.tool.version.id", binding.VersionID),
				attribute.String("kbot.tool.call.id", call.ID),
			)
			result, err := r.executor.Execute(toolContext, tooling.Call{
				WorkspaceID: r.workspaceID, ToolVersionID: binding.VersionID,
				Arguments: []byte(call.Function.Arguments), IdempotencyKey: "react:" + call.ID,
			})
			finishTool(err)
			r.appendExecutionAudit(ctx, operation, binding, call.ID, result, err)
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				history = append(history, schema.ToolMessage(fmt.Sprintf(`{"error":%q}`, err.Error()), call.ID))
				if emit != nil {
					if emitErr := emitContext(ctx, emit, Event{Type: "tool_finished", Data: map[string]any{"name": binding.Name, "call_id": call.ID, "error": err.Error()}}); emitErr != nil {
						return nil, emitErr
					}
				}
				continue
			}
			history = append(history, schema.ToolMessage(string(result.Body), call.ID))
			if emit != nil {
				if err := emitContext(ctx, emit, Event{Type: "tool_finished", Data: map[string]any{"name": binding.Name, "call_id": call.ID, "status": result.StatusCode}}); err != nil {
					return nil, err
				}
			}
		}
	}
	return nil, fmt.Errorf("ReAct loop exceeded max steps %d", maxSteps)
}

func (r *ReActRunner) appendExecutionAudit(ctx context.Context, action string, binding ToolBinding, callID string, result tooling.Result, executionErr error) {
	if r.audit == nil || r.actorID == "" {
		return
	}
	data := map[string]any{"conversation_id": r.runID, "tool_call_id": callID, "status_code": result.StatusCode}
	if executionErr != nil {
		data["failed"] = true
	}
	_, _ = r.audit.Append(context.WithoutCancel(ctx), audit.Event{
		WorkspaceID: r.workspaceID, ActorID: r.actorID, Action: action,
		ResourceID: binding.VersionID, Data: data,
	})
}

func validateBindingCall(binding ToolBinding, arguments []byte) error {
	if !binding.KBScoped || !binding.RestrictKBs {
		return nil
	}
	var input struct {
		KBID string `json:"kb_id"`
	}
	if err := json.Unmarshal(arguments, &input); err != nil {
		return fmt.Errorf("decode knowledge base arguments: %w", err)
	}
	for _, allowed := range binding.AllowedKBs {
		if input.KBID == allowed {
			return nil
		}
	}
	return fmt.Errorf("knowledge base %q is outside the active skill allowlist", input.KBID)
}
