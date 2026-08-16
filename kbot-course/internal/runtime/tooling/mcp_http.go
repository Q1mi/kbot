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
)

// MCPHTTPTransport is the Streamable HTTP subset used by the course runtime.
// The lifecycle and session headers match MCP 2025-11-25.
type MCPHTTPTransport struct {
	client   *http.Client
	endpoint string
	mu       sync.Mutex
	nextID   int
	session  string
	headers  map[string]string
}

func NewMCPHTTPTransport(client *http.Client, endpoint string) *MCPHTTPTransport {
	return &MCPHTTPTransport{client: client, endpoint: endpoint}
}

func (t *MCPHTTPTransport) WithHeaders(headers map[string]string) *MCPHTTPTransport {
	t.headers = headers
	return t
}

func (t *MCPHTTPTransport) Request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	t.mu.Lock()
	t.nextID++
	id := t.nextID
	t.mu.Unlock()
	return t.send(ctx, map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}, true)
}

func (t *MCPHTTPTransport) Notify(ctx context.Context, method string, params any) error {
	_, err := t.send(ctx, map[string]any{"jsonrpc": "2.0", "method": method, "params": params}, false)
	return err
}

func (t *MCPHTTPTransport) send(ctx context.Context, payload map[string]any, expectsResult bool) (json.RawMessage, error) {
	if t.client == nil {
		return nil, fmt.Errorf("MCP HTTP client is required")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", MCPProtocolVersion)
	for name, value := range t.headers {
		req.Header.Set(name, value)
	}
	t.mu.Lock()
	session := t.session
	t.mu.Unlock()
	if session != "" {
		req.Header.Set("MCP-Session-Id", session)
	}
	response, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(responseBody) > maxResponseBytes {
		return nil, fmt.Errorf("MCP response exceeds %d bytes", maxResponseBytes)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("MCP endpoint returned HTTP %d", response.StatusCode)
	}
	if strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream") {
		var data []byte
		for _, line := range bytes.Split(responseBody, []byte("\n")) {
			line = bytes.TrimSpace(line)
			if bytes.HasPrefix(line, []byte("data:")) {
				data = append(data, bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))...)
			}
		}
		if len(data) == 0 {
			return nil, fmt.Errorf("MCP SSE response has no data event")
		}
		responseBody = data
	}
	if newSession := response.Header.Get("MCP-Session-Id"); newSession != "" {
		t.mu.Lock()
		t.session = newSession
		t.mu.Unlock()
	}
	if !expectsResult {
		return json.RawMessage(`{}`), nil
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		return nil, fmt.Errorf("decode MCP response: %w", err)
	}
	if envelope.Error != nil {
		return nil, fmt.Errorf("MCP error %d: %s", envelope.Error.Code, envelope.Error.Message)
	}
	if len(envelope.Result) == 0 {
		return nil, fmt.Errorf("MCP response is missing result")
	}
	return envelope.Result, nil
}
