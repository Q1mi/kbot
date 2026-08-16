package tooling

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
)

const MCPProtocolVersion = "2025-11-25"

type MCPTransport interface {
	Request(context.Context, string, any) (json.RawMessage, error)
}

type HTTPMCPTransport struct {
	endpoint string
	client   *http.Client
	headers  http.Header
	sequence atomic.Uint64
}

func NewHTTPMCPTransport(endpoint string, client *http.Client) *HTTPMCPTransport {
	if client == nil {
		client = http.DefaultClient
	}
	return &HTTPMCPTransport{endpoint: endpoint, client: client}
}

func (t *HTTPMCPTransport) WithHeaders(headers map[string]string) *HTTPMCPTransport {
	t.headers = make(http.Header, len(headers))
	for name, value := range headers {
		t.headers.Set(name, value)
	}
	return t
}

func (t *HTTPMCPTransport) Request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := t.sequence.Add(1)
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	responseBody, err := t.post(ctx, body)
	if err != nil {
		return nil, err
	}
	var response struct {
		ID     uint64          `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(responseBody, &response); err != nil || response.ID != id {
		return nil, fmt.Errorf("invalid MCP JSON-RPC response")
	}
	if response.Error != nil {
		return nil, fmt.Errorf("MCP error %d: %s", response.Error.Code, response.Error.Message)
	}
	return response.Result, nil
}

func (t *HTTPMCPTransport) Notify(ctx context.Context, method string, params any) error {
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	if err != nil {
		return err
	}
	_, err = t.post(ctx, body)
	return err
}

func (t *HTTPMCPTransport) post(ctx context.Context, body []byte) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	for name, values := range t.headers {
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}
	response, err := t.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(payload) > maxResponseBytes {
		return nil, fmt.Errorf("MCP response exceeds %d bytes", maxResponseBytes)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("MCP HTTP %d", response.StatusCode)
	}
	return payload, nil
}

type MCPNotifier interface {
	Notify(context.Context, string, any) error
}

type MCPClient struct {
	transport MCPTransport
	mu        sync.Mutex
	ready     bool
}

func NewMCPClient(transport MCPTransport) *MCPClient { return &MCPClient{transport: transport} }

func (c *MCPClient) Initialize(ctx context.Context) error {
	if c == nil || c.transport == nil {
		return fmt.Errorf("MCP transport is required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ready {
		return nil
	}
	raw, err := c.transport.Request(ctx, "initialize", map[string]any{
		"protocolVersion": MCPProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "kbot-course", "version": "1.0"},
	})
	if err != nil {
		return fmt.Errorf("MCP initialize: %w", err)
	}
	var result struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(raw, &result); err != nil || result.ProtocolVersion != MCPProtocolVersion {
		return fmt.Errorf("MCP protocol negotiation failed: server=%q", result.ProtocolVersion)
	}
	notifier, ok := c.transport.(MCPNotifier)
	if !ok {
		return fmt.Errorf("MCP transport cannot send initialized notification")
	}
	if err := notifier.Notify(ctx, "notifications/initialized", map[string]any{}); err != nil {
		return fmt.Errorf("MCP initialized notification: %w", err)
	}
	c.ready = true
	return nil
}

func (c *MCPClient) ListTools(ctx context.Context) (json.RawMessage, error) {
	if err := c.Initialize(ctx); err != nil {
		return nil, err
	}
	result, err := c.transport.Request(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("MCP tools/list: %w", err)
	}
	if !json.Valid(result) {
		return nil, fmt.Errorf("MCP returned invalid tools/list JSON")
	}
	return append(json.RawMessage(nil), result...), nil
}

func (c *MCPClient) CallTool(ctx context.Context, name string, arguments map[string]any) (json.RawMessage, error) {
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("MCP tool name is required")
	}
	if err := c.Initialize(ctx); err != nil {
		return nil, err
	}
	result, err := c.transport.Request(ctx, "tools/call", map[string]any{"name": name, "arguments": arguments})
	if err != nil {
		return nil, fmt.Errorf("MCP tools/call: %w", err)
	}
	if !json.Valid(result) {
		return nil, fmt.Errorf("MCP returned invalid JSON")
	}
	return append(json.RawMessage(nil), result...), nil
}
