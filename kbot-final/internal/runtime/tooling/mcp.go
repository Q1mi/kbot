package tooling

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	einomcp "github.com/cloudwego/eino-ext/components/tool/mcp"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	markmcp "github.com/mark3labs/mcp-go/mcp"

	platformtool "github.com/Q1mi/kbot/internal/platform/tool"
)

// mcpExecutor 保留 Kbot Tool Registry 的固定版本和安全元数据，并把 MCP 生命周期、
// 传输、工具发现与 tools/call 交给官方 MCP SDK 和 Eino MCP Tool 组件。
type mcpExecutor struct {
	transport       string
	command         string
	args            []string
	env             map[string]string
	endpoint        string
	toolName        string
	protocolVersion string
	headers         map[string]string
	client          *http.Client
}

func newMCPExecutor(httpClient *http.Client, cfg *platformtool.ToolConfig) Executor {
	transportName, _ := cfg.EndpointConfig["transport"].(string)
	command, _ := cfg.EndpointConfig["command"].(string)
	endpoint, _ := cfg.EndpointConfig["url"].(string)
	if endpoint == "" && (transportName == "http" || transportName == "streamable_http") {
		endpoint = command
	}
	toolName, _ := cfg.EndpointConfig["tool_name"].(string)
	protocolVersion, _ := cfg.EndpointConfig["protocol_version"].(string)
	if protocolVersion == "" {
		protocolVersion = markmcp.LATEST_PROTOCOL_VERSION
	}
	if transportName == "" {
		if endpoint != "" {
			transportName = "streamable_http"
		} else {
			transportName = "stdio"
		}
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &mcpExecutor{
		transport: transportName, command: command,
		args: configStringSlice(cfg.EndpointConfig["args"]), env: configStringMap(cfg.EndpointConfig["env"]),
		endpoint: endpoint, toolName: toolName, protocolVersion: protocolVersion,
		headers: authHeaders(cfg.AuthConfig), client: httpClient,
	}
}

func (e *mcpExecutor) Execute(ctx context.Context, arguments json.RawMessage) (string, error) {
	if e.toolName == "" {
		return "", errors.New("mcp executor: empty tool_name")
	}
	mcpClient, err := e.newClient(ctx)
	if err != nil {
		return "", err
	}
	defer mcpClient.Close()

	request := markmcp.InitializeRequest{}
	request.Params.ProtocolVersion = e.protocolVersion
	request.Params.ClientInfo = markmcp.Implementation{Name: "kbot", Version: "1.0"}
	if _, err := mcpClient.Initialize(ctx, request); err != nil {
		return "", fmt.Errorf("initialize MCP client: %w", err)
	}
	tools, err := einomcp.GetTools(ctx, &einomcp.Config{
		Cli: mcpClient, ToolNameList: []string{e.toolName}, CustomHeaders: e.headers,
	})
	if err != nil {
		return "", fmt.Errorf("discover MCP tool %q: %w", e.toolName, err)
	}
	if len(tools) != 1 {
		return "", fmt.Errorf("MCP tool %q was not advertised by the server", e.toolName)
	}
	invokable, ok := tools[0].(einotool.InvokableTool)
	if !ok {
		return "", fmt.Errorf("MCP tool %q is not invokable", e.toolName)
	}
	result, err := invokable.InvokableRun(ctx, string(arguments))
	if err != nil {
		return "", err
	}
	return formatMCPToolResult(json.RawMessage(result))
}

func (e *mcpExecutor) newClient(ctx context.Context) (*client.Client, error) {
	switch e.transport {
	case "stdio":
		command := e.command
		args := append([]string(nil), e.args...)
		if command == "" {
			return nil, errors.New("mcp stdio: empty command")
		}
		if len(args) == 0 {
			fields := strings.Fields(command)
			if len(fields) > 1 {
				command, args = fields[0], fields[1:]
			}
		}
		return client.NewStdioMCPClient(command, environmentEntries(e.env), args...)
	case "http", "streamable_http":
		if e.endpoint == "" {
			return nil, errors.New("mcp streamable HTTP: empty url")
		}
		mcpClient, err := client.NewStreamableHttpClient(
			e.endpoint,
			transport.WithHTTPBasicClient(e.client),
			transport.WithHTTPHeaders(e.headers),
		)
		if err != nil {
			return nil, fmt.Errorf("create MCP streamable HTTP client: %w", err)
		}
		if err := mcpClient.Start(ctx); err != nil {
			return nil, fmt.Errorf("start MCP streamable HTTP client: %w", err)
		}
		return mcpClient, nil
	default:
		return nil, fmt.Errorf("mcp executor: unsupported transport %q", e.transport)
	}
}

type mcpToolResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	} `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
	IsError           bool            `json:"isError,omitempty"`
}

func formatMCPToolResult(raw json.RawMessage) (string, error) {
	var result mcpToolResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("mcp decode tool result: %w", err)
	}
	parts := make([]string, 0, len(result.Content)+1)
	for _, content := range result.Content {
		if content.Type == "text" && content.Text != "" {
			parts = append(parts, content.Text)
		}
	}
	if len(result.StructuredContent) > 0 && string(result.StructuredContent) != "null" {
		parts = append(parts, string(result.StructuredContent))
	}
	if len(parts) == 0 {
		parts = append(parts, string(raw))
	}
	output := strings.Join(parts, "\n")
	if result.IsError {
		return "", fmt.Errorf("mcp tool error: %s", output)
	}
	return output, nil
}

func configStringSlice(value any) []string {
	raw, ok := value.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

func configStringMap(value any) map[string]string {
	raw, ok := value.(map[string]interface{})
	if !ok {
		return nil
	}
	out := make(map[string]string, len(raw))
	for key, item := range raw {
		if text, ok := item.(string); ok {
			out[key] = text
		}
	}
	return out
}

func environmentEntries(values map[string]string) []string {
	out := make([]string, 0, len(values))
	for key, value := range values {
		out = append(out, key+"="+value)
	}
	return out
}
