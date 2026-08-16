package tooling

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/Q1mi/kbot/internal/platform/tool"
)

// restExecutor 把 Tool 的入参 JSON 作为请求体打到配置的 endpoint（§14.2 的 REST executor）。
type restExecutor struct {
	client   *http.Client
	method   string
	endpoint string
	headers  map[string]string
}

func newRESTExecutor(client *http.Client, cfg *tool.ToolConfig) Executor {
	method, _ := cfg.EndpointConfig["method"].(string)
	if method == "" {
		method = http.MethodPost
	}
	endpoint, _ := cfg.EndpointConfig["url"].(string)

	headers := map[string]string{"Content-Type": "application/json"}
	for k, v := range authHeaders(cfg.AuthConfig) {
		headers[k] = v
	}

	return &restExecutor{
		client:   client,
		method:   method,
		endpoint: endpoint,
		headers:  headers,
	}
}

func (e *restExecutor) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if e.endpoint == "" {
		return "", fmt.Errorf("rest tool: empty endpoint url")
	}

	var body io.Reader
	if e.method == http.MethodGet || e.method == http.MethodDelete {
		body = nil
	} else {
		body = bytes.NewReader(args)
	}

	req, err := http.NewRequestWithContext(ctx, e.method, e.endpoint, body)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	for k, v := range e.headers {
		req.Header.Set(k, v)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	out, err := readToolResponse(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("rest tool returned %d: %s", resp.StatusCode, string(out))
	}

	return string(out), nil
}
