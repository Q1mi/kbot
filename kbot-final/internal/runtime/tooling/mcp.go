package tooling

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Q1mi/kbot/internal/platform/tool"
)

const (
	defaultMCPProtocolVersion = "2025-11-25"
	maxMCPMessageBytes        = 4 << 20
)

// mcpExecutor 实现 MCP tools/call 的最小生产可用客户端。
//
// 支持规范定义的两种标准传输:
//   - stdio:每次调用启动一个子进程,完成 initialize → initialized → tools/call。
//   - Streamable HTTP:每次调用建立一个短会话,处理 MCP-Session-Id 与 JSON/SSE 响应。
//
// 当前只声明空 client capabilities,不支持 server 发起的 sampling/elicitation 请求。
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

func newMCPExecutor(client *http.Client, cfg *tool.ToolConfig) Executor {
	transport, _ := cfg.EndpointConfig["transport"].(string)
	command, _ := cfg.EndpointConfig["command"].(string)
	endpoint, _ := cfg.EndpointConfig["url"].(string)
	if endpoint == "" && (transport == "http" || transport == "streamable_http") {
		endpoint = command // 兼容早期把 URL 写在 command 的配置。
	}
	toolName, _ := cfg.EndpointConfig["tool_name"].(string)
	protocolVersion, _ := cfg.EndpointConfig["protocol_version"].(string)
	if protocolVersion == "" {
		protocolVersion = defaultMCPProtocolVersion
	}
	if transport == "" {
		if endpoint != "" {
			transport = "streamable_http"
		} else {
			transport = "stdio"
		}
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	return &mcpExecutor{
		transport:       transport,
		command:         command,
		args:            configStringSlice(cfg.EndpointConfig["args"]),
		env:             configStringMap(cfg.EndpointConfig["env"]),
		endpoint:        endpoint,
		toolName:        toolName,
		protocolVersion: protocolVersion,
		headers:         authHeaders(cfg.AuthConfig),
		client:          client,
	}
}

func (e *mcpExecutor) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if e.toolName == "" {
		return "", errors.New("mcp executor: empty tool_name")
	}
	var input map[string]any
	if len(args) == 0 {
		input = map[string]any{}
	} else if err := json.Unmarshal(args, &input); err != nil {
		return "", fmt.Errorf("mcp executor: parse arguments: %w", err)
	}

	var result json.RawMessage
	var err error
	switch e.transport {
	case "stdio":
		result, err = e.executeStdio(ctx, input)
	case "http", "streamable_http":
		result, err = e.executeHTTP(ctx, input)
	default:
		return "", fmt.Errorf("mcp executor: unsupported transport %q", e.transport)
	}
	if err != nil {
		return "", err
	}
	return formatMCPToolResult(result)
}

type mcpRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type mcpRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *mcpRPCError    `json:"error,omitempty"`
}

type mcpRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type mcpInitializeResult struct {
	ProtocolVersion string `json:"protocolVersion"`
}

func initializeRequest(version string) mcpRPCRequest {
	return mcpRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params: map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "kbot",
				"version": "1.0",
			},
		},
	}
}

func initializedNotification() mcpRPCRequest {
	return mcpRPCRequest{JSONRPC: "2.0", Method: "notifications/initialized"}
}

func callToolRequest(name string, args map[string]any) mcpRPCRequest {
	return mcpRPCRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "tools/call",
		Params: map[string]any{
			"name":      name,
			"arguments": args,
		},
	}
}

// --- stdio transport ---

type mcpStdioConn struct {
	writer *bufio.Writer
	reader *bufio.Scanner
}

func (e *mcpExecutor) executeStdio(ctx context.Context, args map[string]any) (json.RawMessage, error) {
	command, commandArgs := e.command, append([]string(nil), e.args...)
	if command == "" {
		return nil, errors.New("mcp stdio: empty command")
	}
	// 兼容早期 "command": "npx -y server" 写法；新配置应把参数放进 args 数组。
	if len(commandArgs) == 0 {
		fields := strings.Fields(command)
		if len(fields) > 1 {
			command, commandArgs = fields[0], fields[1:]
		}
	}

	cmd := exec.CommandContext(ctx, command, commandArgs...)
	if len(e.env) > 0 {
		cmd.Env = append([]string(nil), os.Environ()...)
		for k, v := range e.env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp stdio stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp stdio stdout: %w", err)
	}
	stderr := &limitedStringWriter{limit: 32 << 10}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp stdio start %q: %w", command, err)
	}
	defer func() {
		_ = stdin.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64<<10), maxMCPMessageBytes)
	conn := &mcpStdioConn{writer: bufio.NewWriter(stdin), reader: scanner}

	initResp, err := conn.call(initializeRequest(e.protocolVersion))
	if err != nil {
		return nil, withMCPStderr(err, stderr.String())
	}
	var initialized mcpInitializeResult
	if err := json.Unmarshal(initResp, &initialized); err != nil {
		return nil, fmt.Errorf("mcp stdio decode initialize result: %w", err)
	}
	if initialized.ProtocolVersion == "" {
		return nil, errors.New("mcp stdio: initialize response missing protocolVersion")
	}
	if err := conn.notify(initializedNotification()); err != nil {
		return nil, withMCPStderr(err, stderr.String())
	}

	result, err := conn.call(callToolRequest(e.toolName, args))
	if err != nil {
		return nil, withMCPStderr(err, stderr.String())
	}
	return result, nil
}

func (c *mcpStdioConn) call(req mcpRPCRequest) (json.RawMessage, error) {
	if err := c.write(req); err != nil {
		return nil, err
	}
	wantID := fmt.Sprint(req.ID)
	for c.reader.Scan() {
		line := bytes.TrimSpace(c.reader.Bytes())
		if len(line) == 0 {
			continue
		}
		var msg mcpRPCResponse
		if err := json.Unmarshal(line, &msg); err != nil {
			return nil, fmt.Errorf("mcp stdio decode response: %w", err)
		}
		if msg.Method != "" && len(msg.ID) > 0 {
			// 本客户端不声明 sampling/elicitation 能力；对意外 server request 明确回错。
			_ = c.write(map[string]any{
				"jsonrpc": "2.0",
				"id":      json.RawMessage(msg.ID),
				"error": map[string]any{
					"code":    -32601,
					"message": "kbot MCP client does not support server-initiated requests",
				},
			})
			continue
		}
		if strings.Trim(string(msg.ID), `"`) != wantID {
			continue // 忽略通知和其它并发消息。
		}
		if msg.Error != nil {
			return nil, fmt.Errorf("mcp rpc error %d: %s", msg.Error.Code, msg.Error.Message)
		}
		return msg.Result, nil
	}
	if err := c.reader.Err(); err != nil {
		return nil, fmt.Errorf("mcp stdio read: %w", err)
	}
	return nil, io.EOF
}

func (c *mcpStdioConn) notify(req mcpRPCRequest) error {
	return c.write(req)
}

func (c *mcpStdioConn) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := c.writer.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("mcp stdio write: %w", err)
	}
	if err := c.writer.Flush(); err != nil {
		return fmt.Errorf("mcp stdio flush: %w", err)
	}
	return nil
}

// --- Streamable HTTP transport ---

type mcpHTTPSession struct {
	id              string
	protocolVersion string
}

func (e *mcpExecutor) executeHTTP(ctx context.Context, args map[string]any) (json.RawMessage, error) {
	if e.endpoint == "" {
		return nil, errors.New("mcp streamable HTTP: empty url")
	}
	session := mcpHTTPSession{protocolVersion: e.protocolVersion}

	initResp, sessionID, err := e.httpRequest(ctx, initializeRequest(e.protocolVersion), session)
	if err != nil {
		return nil, err
	}
	session.id = sessionID
	var initialized mcpInitializeResult
	if err := json.Unmarshal(initResp, &initialized); err != nil {
		return nil, fmt.Errorf("mcp http decode initialize result: %w", err)
	}
	if initialized.ProtocolVersion == "" {
		return nil, errors.New("mcp http: initialize response missing protocolVersion")
	}
	session.protocolVersion = initialized.ProtocolVersion

	if _, _, err := e.httpRequest(ctx, initializedNotification(), session); err != nil {
		return nil, err
	}
	defer e.closeHTTPSession(session)

	result, _, err := e.httpRequest(ctx, callToolRequest(e.toolName, args), session)
	return result, err
}

func (e *mcpExecutor) httpRequest(ctx context.Context, rpc mcpRPCRequest, session mcpHTTPSession) (json.RawMessage, string, error) {
	body, err := json.Marshal(rpc)
	if err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if session.id != "" {
		req.Header.Set("MCP-Session-Id", session.id)
	}
	// initialize 自己携带版本；规范要求其后的 HTTP 请求再带版本 Header。
	if rpc.Method != "initialize" && session.protocolVersion != "" {
		req.Header.Set("MCP-Protocol-Version", session.protocolVersion)
	}
	for k, v := range e.headers {
		req.Header.Set(k, v)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("mcp http request: %w", err)
	}
	defer resp.Body.Close()
	sessionID := resp.Header.Get("MCP-Session-Id")
	if sessionID == "" {
		sessionID = session.id
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, maxMCPMessageBytes))
		return nil, sessionID, fmt.Errorf("mcp http returned %d: %s", resp.StatusCode, string(b))
	}
	if rpc.ID == 0 {
		// Notification 正常返回 202 且无 body。
		return nil, sessionID, nil
	}

	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	var rpcResp mcpRPCResponse
	switch {
	case strings.Contains(contentType, "text/event-stream"):
		rpcResp, err = readMCPSSE(resp.Body, rpc.ID)
	default:
		err = decodeLimitedJSON(resp.Body, &rpcResp)
	}
	if err != nil {
		return nil, sessionID, err
	}
	if rpcResp.Error != nil {
		return nil, sessionID, fmt.Errorf("mcp rpc error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}
	return rpcResp.Result, sessionID, nil
}

func (e *mcpExecutor) closeHTTPSession(session mcpHTTPSession) {
	if session.id == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, e.endpoint, nil)
	if err != nil {
		return
	}
	req.Header.Set("MCP-Session-Id", session.id)
	req.Header.Set("MCP-Protocol-Version", session.protocolVersion)
	for k, v := range e.headers {
		req.Header.Set(k, v)
	}
	resp, err := e.client.Do(req)
	if err == nil {
		_ = resp.Body.Close()
	}
}

func readMCPSSE(r io.Reader, wantID int) (mcpRPCResponse, error) {
	scanner := bufio.NewScanner(io.LimitReader(r, maxMCPMessageBytes))
	scanner.Buffer(make([]byte, 64<<10), maxMCPMessageBytes)
	var data strings.Builder
	parseEvent := func() (mcpRPCResponse, bool, error) {
		if data.Len() == 0 {
			return mcpRPCResponse{}, false, nil
		}
		raw := strings.TrimSpace(data.String())
		data.Reset()
		if raw == "" {
			return mcpRPCResponse{}, false, nil
		}
		var msg mcpRPCResponse
		if err := json.Unmarshal([]byte(raw), &msg); err != nil {
			return mcpRPCResponse{}, false, fmt.Errorf("mcp http decode SSE data: %w", err)
		}
		return msg, strings.Trim(string(msg.ID), `"`) == fmt.Sprint(wantID), nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			msg, matched, err := parseEvent()
			if err != nil {
				return mcpRPCResponse{}, err
			}
			if matched {
				return msg, nil
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return mcpRPCResponse{}, fmt.Errorf("mcp http read SSE: %w", err)
	}
	if msg, matched, err := parseEvent(); err != nil {
		return mcpRPCResponse{}, err
	} else if matched {
		return msg, nil
	}
	return mcpRPCResponse{}, io.EOF
}

func decodeLimitedJSON(r io.Reader, v any) error {
	dec := json.NewDecoder(io.LimitReader(r, maxMCPMessageBytes))
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("mcp http decode response: %w", err)
	}
	return nil
}

// --- result/config helpers ---

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
	var parts []string
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

func configStringSlice(v any) []string {
	raw, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func configStringMap(v any) map[string]string {
	raw, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	out := make(map[string]string, len(raw))
	for k, item := range raw {
		if s, ok := item.(string); ok {
			out[k] = s
		}
	}
	return out
}

type limitedStringWriter struct {
	buf   strings.Builder
	limit int
}

func (w *limitedStringWriter) Write(p []byte) (int, error) {
	remaining := w.limit - w.buf.Len()
	if remaining > 0 {
		if len(p) > remaining {
			_, _ = w.buf.Write(p[:remaining])
		} else {
			_, _ = w.buf.Write(p)
		}
	}
	return len(p), nil
}

func (w *limitedStringWriter) String() string { return w.buf.String() }

func withMCPStderr(err error, stderr string) error {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return err
	}
	return fmt.Errorf("%w (stderr: %s)", err, stderr)
}
