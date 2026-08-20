package engine

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"github.com/Q1mi/kbot/internal/runtime/tooling"
)

type ToolBinding = tooling.Binding

type ToolExecutor interface {
	Execute(ctx context.Context, call tooling.Call) (tooling.Result, error)
}

type ADKRunner struct {
	model       model.BaseChatModel
	executor    ToolExecutor
	workspaceID string
}

func NewADKRunner(chatModel model.BaseChatModel, executor ToolExecutor, workspaceID string) *ADKRunner {
	return &ADKRunner{model: chatModel, executor: executor, workspaceID: workspaceID}
}

func (r *ADKRunner) Run(
	ctx context.Context, messages []*schema.Message, bindings []ToolBinding, maxSteps int, emit Emitter,
) (*schema.Message, error) {
	if r.model == nil || r.executor == nil {
		return nil, fmt.Errorf("chat model and tool executor are required")
	}
	if maxSteps <= 0 {
		return nil, fmt.Errorf("max steps must be positive")
	}
	tools := make([]einotool.BaseTool, 0, len(bindings))
	for _, binding := range bindings {
		tools = append(tools, &bindingTool{binding: binding, executor: r.executor, workspaceID: r.workspaceID})
	}
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name: "course_agent", Description: "kbot course agent", Model: r.model,
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{
			Tools: tools, ExecuteSequentially: true,
			ToolCallMiddlewares: []compose.ToolMiddleware{toolEventMiddleware(emit)},
		}},
		MaxIterations: maxSteps,
	})
	if err != nil {
		return nil, fmt.Errorf("create Eino ChatModelAgent: %w", err)
	}
	iterator := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent}).Run(ctx, messages)
	var answer *schema.Message
	for {
		event, ok := iterator.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			return nil, event.Err
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}
		message, err := event.Output.MessageOutput.GetMessage()
		if err != nil {
			return nil, err
		}
		if message != nil && message.Role == schema.Assistant && len(message.ToolCalls) == 0 {
			answer = message
		}
	}
	if answer == nil {
		return nil, fmt.Errorf("Eino ADK run finished without a final answer")
	}
	return answer, nil
}

type toolCallIDKey struct{}

type bindingTool struct {
	binding     ToolBinding
	executor    ToolExecutor
	workspaceID string
}

func (t *bindingTool) Info(context.Context) (*schema.ToolInfo, error) {
	if t.binding.Info == nil {
		return nil, fmt.Errorf("tool %s has no schema", t.binding.Name)
	}
	info := *t.binding.Info
	info.Name = t.binding.Name
	return &info, nil
}

func (t *bindingTool) InvokableRun(ctx context.Context, arguments string, _ ...einotool.Option) (string, error) {
	callID, _ := ctx.Value(toolCallIDKey{}).(string)
	result, err := t.executor.Execute(ctx, tooling.Call{
		WorkspaceID: t.workspaceID, ToolVersionID: t.binding.VersionID,
		Arguments: []byte(arguments), IdempotencyKey: "react:" + callID,
	})
	if err != nil {
		return "", err
	}
	return string(result.Body), nil
}

func toolEventMiddleware(emit Emitter) compose.ToolMiddleware {
	return compose.ToolMiddleware{Invokable: func(next compose.InvokableToolEndpoint) compose.InvokableToolEndpoint {
		return func(ctx context.Context, input *compose.ToolInput) (*compose.ToolOutput, error) {
			if emit != nil {
				if err := emitContext(ctx, emit, Event{Type: "tool_started", Data: map[string]any{"name": input.Name, "call_id": input.CallID}}); err != nil {
					return nil, err
				}
			}
			output, err := next(context.WithValue(ctx, toolCallIDKey{}, input.CallID), input)
			if err != nil {
				output, err = &compose.ToolOutput{Result: fmt.Sprintf(`{"error":%q}`, err.Error())}, nil
			}
			if emit != nil {
				if emitErr := emitContext(ctx, emit, Event{Type: "tool_finished", Data: map[string]any{"name": input.Name, "call_id": input.CallID}}); emitErr != nil {
					return nil, emitErr
				}
			}
			return output, err
		}
	}}
}
