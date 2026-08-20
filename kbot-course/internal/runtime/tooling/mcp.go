package tooling

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	einomcp "github.com/cloudwego/eino-ext/components/tool/mcp"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	markmcp "github.com/mark3labs/mcp-go/mcp"
)

// callMCPTool 把 MCP 生命周期、协议传输、工具发现与 tools/call 交给官方
// MCP SDK 和 Eino MCP Tool 组件。Kbot 继续负责 Registry 固定版本、安全元数据与 SSRF 策略。
func callMCPTool(
	ctx context.Context,
	endpoint string,
	toolName string,
	arguments map[string]any,
	headers map[string]string,
	httpClient *http.Client,
) (json.RawMessage, error) {
	if strings.TrimSpace(endpoint) == "" || strings.TrimSpace(toolName) == "" {
		return nil, fmt.Errorf("MCP endpoint and tool name are required")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	mcpClient, err := client.NewStreamableHttpClient(
		endpoint,
		transport.WithHTTPBasicClient(httpClient),
		transport.WithHTTPHeaders(headers),
	)
	if err != nil {
		return nil, fmt.Errorf("create MCP streamable HTTP client: %w", err)
	}
	if err := mcpClient.Start(ctx); err != nil {
		return nil, fmt.Errorf("start MCP streamable HTTP client: %w", err)
	}
	defer mcpClient.Close()

	request := markmcp.InitializeRequest{}
	request.Params.ProtocolVersion = markmcp.LATEST_PROTOCOL_VERSION
	request.Params.ClientInfo = markmcp.Implementation{Name: "kbot-course", Version: "1.0"}
	if _, err := mcpClient.Initialize(ctx, request); err != nil {
		return nil, fmt.Errorf("initialize MCP client: %w", err)
	}
	tools, err := einomcp.GetTools(ctx, &einomcp.Config{
		Cli: mcpClient, ToolNameList: []string{toolName}, CustomHeaders: headers,
	})
	if err != nil {
		return nil, fmt.Errorf("discover MCP tool %q: %w", toolName, err)
	}
	if len(tools) != 1 {
		return nil, fmt.Errorf("MCP tool %q was not advertised by the server", toolName)
	}
	invokable, ok := tools[0].(einotool.InvokableTool)
	if !ok {
		return nil, fmt.Errorf("MCP tool %q is not invokable", toolName)
	}
	payload, err := json.Marshal(arguments)
	if err != nil {
		return nil, fmt.Errorf("encode MCP arguments: %w", err)
	}
	result, err := invokable.InvokableRun(ctx, string(payload))
	if err != nil {
		return nil, fmt.Errorf("call MCP tool %q: %w", toolName, err)
	}
	if !json.Valid([]byte(result)) {
		return nil, fmt.Errorf("MCP tool %q returned invalid JSON", toolName)
	}
	return json.RawMessage(result), nil
}
