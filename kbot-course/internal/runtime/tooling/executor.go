// Package tooling 负责 Runtime 侧的工具解析、校验与调用。
package tooling

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
	jsonschema "github.com/eino-contrib/jsonschema"
	"github.com/xeipuuv/gojsonschema"

	platformtool "github.com/Q1mi/kbot/internal/platform/tool"
)

const maxResponseBytes = 1 << 20

type Registry interface {
	Resolve(ctx context.Context, workspaceID, versionID string) (platformtool.Version, error)
}

type Call struct {
	WorkspaceID    string
	ToolVersionID  string
	Arguments      []byte
	IdempotencyKey string
}

type Result struct {
	StatusCode int
	Body       []byte
}

// SandboxRunner 是 code_execution Tool 依赖的最小执行端口。
// 第 08 课结束时会由独立 HTTP Client 实现。
type SandboxRunner interface {
	Run(ctx context.Context, language, code string) (string, error)
}

type Binding struct {
	Name            string
	VersionID       string
	SourceType      string
	Info            *schema.ToolInfo
	RequiresNetwork bool
	KBScoped        bool
	RestrictKBs     bool
	AllowedKBs      []string
	Sensitive       bool
}

type Executor struct {
	registry     Registry
	client       *http.Client
	allowedHosts map[string]struct{}
	sandbox      SandboxRunner
	sdk          map[string]SDKHandler
}

type SDKHandler func(ctx context.Context, workspaceID string, arguments map[string]any) (Result, error)

func NewExecutor(registry Registry, client *http.Client, allowedHosts ...string) *Executor {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	} else if client.Timeout <= 0 {
		clone := *client
		clone.Timeout = 10 * time.Second
		client = &clone
	}
	allowed := make(map[string]struct{}, len(allowedHosts))
	for _, host := range allowedHosts {
		if normalized := strings.ToLower(strings.TrimSpace(host)); normalized != "" {
			allowed[normalized] = struct{}{}
		}
	}
	executor := &Executor{registry: registry, client: client, allowedHosts: allowed, sdk: make(map[string]SDKHandler)}
	clone := *client
	if transport, ok := clone.Transport.(*http.Transport); ok {
		clone.Transport = executor.secureTransport(transport.Clone())
	} else if clone.Transport == nil {
		clone.Transport = executor.secureTransport(http.DefaultTransport.(*http.Transport).Clone())
	}
	previousRedirect := clone.CheckRedirect
	clone.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if err := executor.validateEndpoint(req.URL); err != nil {
			return err
		}
		if len(via) > 0 && !sameOrigin(via[0].URL, req.URL) {
			return fmt.Errorf("cross-origin tool redirect is blocked")
		}
		if previousRedirect != nil {
			return previousRedirect(req, via)
		}
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		return nil
	}
	executor.client = &clone
	return executor
}

func (e *Executor) WithSandbox(runner SandboxRunner) *Executor {
	e.sandbox = runner
	return e
}

func (e *Executor) RegisterSDK(name string, handler SDKHandler) {
	if strings.TrimSpace(name) != "" && handler != nil {
		e.sdk[name] = handler
	}
}

func (e *Executor) Execute(ctx context.Context, call Call) (Result, error) {
	if e.registry == nil {
		return Result{}, fmt.Errorf("tool registry is required")
	}
	version, err := e.registry.Resolve(ctx, call.WorkspaceID, call.ToolVersionID)
	if err != nil {
		return Result{}, fmt.Errorf("resolve tool: %w", err)
	}
	var arguments map[string]any
	decoder := json.NewDecoder(bytes.NewReader(call.Arguments))
	decoder.UseNumber()
	if err := decoder.Decode(&arguments); err != nil {
		return Result{}, fmt.Errorf("arguments must be a JSON object: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return Result{}, fmt.Errorf("arguments must contain one JSON object")
	}
	if err := validateArguments(version.InputSchema, call.Arguments); err != nil {
		return Result{}, err
	}
	if version.SourceType == "code_execution" {
		if e.sandbox == nil {
			return Result{}, fmt.Errorf("sandbox runner is not configured")
		}
		code, _ := arguments["code"].(string)
		if strings.TrimSpace(code) == "" {
			return Result{}, fmt.Errorf("code_execution requires a non-empty code argument")
		}
		language := strings.TrimSpace(version.Endpoint)
		if language == "" {
			language = "python"
		}
		output, err := e.sandbox.Run(ctx, language, code)
		if err != nil {
			return Result{}, fmt.Errorf("execute code tool: %w", err)
		}
		return Result{StatusCode: http.StatusOK, Body: []byte(output)}, nil
	}
	if version.SourceType == "internal_sdk" {
		handler := e.sdk[version.Endpoint]
		if handler == nil {
			return Result{}, fmt.Errorf("internal SDK tool %q is not registered", version.Endpoint)
		}
		return handler(ctx, call.WorkspaceID, arguments)
	}
	endpoint, err := url.Parse(version.Endpoint)
	if err != nil {
		return Result{}, fmt.Errorf("parse tool endpoint: %w", err)
	}
	if err := e.validateEndpoint(endpoint); err != nil {
		return Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(call.Arguments))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	headers, err := toolAuthHeaders(version.AuthConfig)
	if err != nil {
		return Result{}, err
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	if key := strings.TrimSpace(call.IdempotencyKey); key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	response, err := e.client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("call tool: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return Result{}, fmt.Errorf("read tool response: %w", err)
	}
	if len(body) > maxResponseBytes {
		return Result{}, fmt.Errorf("tool response exceeds %d bytes", maxResponseBytes)
	}
	result := Result{StatusCode: response.StatusCode, Body: body}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return result, fmt.Errorf("tool returned HTTP %d", response.StatusCode)
	}
	return result, nil
}

// secureTransport 在真正拨号前重新解析 allowlist 主机，并直接拨向已校验的 IP。
// 这样 URL 校验、DNS 解析和 TCP 连接属于同一条策略链。
func (e *Executor) secureTransport(transport *http.Transport) *http.Transport {
	transport.Proxy = nil
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		if _, ok := e.allowedHosts[strings.ToLower(strings.TrimSuffix(host, "."))]; !ok {
			return nil, fmt.Errorf("tool dial host %q is not allowlisted", host)
		}
		if ip := net.ParseIP(host); ip != nil {
			return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		}
		addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil || len(addresses) == 0 {
			return nil, fmt.Errorf("resolve tool host %q: %w", host, err)
		}
		var dialErr error
		for _, candidate := range addresses {
			var connection net.Conn
			connection, dialErr = dialer.DialContext(ctx, network, net.JoinHostPort(candidate.IP.String(), port))
			if dialErr == nil {
				return connection, nil
			}
		}
		return nil, fmt.Errorf("dial tool host %q: %w", host, dialErr)
	}
	return transport
}

func (e *Executor) Bind(ctx context.Context, workspaceID string, versionIDs []string) ([]Binding, error) {
	bindings := make([]Binding, 0, len(versionIDs))
	for _, versionID := range versionIDs {
		version, err := e.registry.Resolve(ctx, workspaceID, versionID)
		if err != nil {
			return nil, fmt.Errorf("resolve tool binding %s: %w", versionID, err)
		}
		params := &jsonschema.Schema{}
		if err := json.Unmarshal(version.InputSchema, params); err != nil {
			return nil, fmt.Errorf("decode schema for tool %s: %w", version.Name, err)
		}
		bindings = append(bindings, Binding{
			Name: version.Name, VersionID: version.ID,
			SourceType:      version.SourceType,
			Info:            &schema.ToolInfo{Name: version.Name, Desc: version.Description, ParamsOneOf: schema.NewParamsOneOfByJSONSchema(params)},
			RequiresNetwork: version.SourceType == "rest_api" || version.SourceType == "mcp_server" || version.SourceType == "a2a" || version.SourceType == "",
			KBScoped:        version.SourceType == "internal_sdk" && version.Endpoint == "search_knowledge_base",
			Sensitive:       version.Sensitive,
		})
	}
	return bindings, nil
}

func (e *Executor) validateEndpoint(endpoint *url.URL) error {
	if endpoint == nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Host == "" || endpoint.User != nil {
		return fmt.Errorf("tool endpoint must be an absolute HTTP URL without user info")
	}
	host := strings.ToLower(endpoint.Hostname())
	if _, ok := e.allowedHosts[host]; !ok {
		return fmt.Errorf("tool endpoint host %q is not allowlisted", host)
	}
	return nil
}

func validateArguments(schemaJSON, arguments []byte) error {
	compiled, err := gojsonschema.NewSchema(gojsonschema.NewBytesLoader(schemaJSON))
	if err != nil {
		return fmt.Errorf("compile stored tool schema: %w", err)
	}
	result, err := compiled.Validate(gojsonschema.NewBytesLoader(arguments))
	if err != nil {
		return fmt.Errorf("validate tool arguments: %w", err)
	}
	if result.Valid() {
		return nil
	}
	problems := make([]string, 0, len(result.Errors()))
	for _, problem := range result.Errors() {
		problems = append(problems, problem.String())
	}
	return fmt.Errorf("tool arguments violate schema: %s", strings.Join(problems, "; "))
}

func toolAuthHeaders(raw string) (map[string]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var config struct {
		Headers map[string]string `json:"headers"`
	}
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return nil, fmt.Errorf("invalid tool auth config: %w", err)
	}
	for name := range config.Headers {
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "", "host", "content-length", "connection", "transfer-encoding":
			return nil, fmt.Errorf("tool auth header %q is not allowed", name)
		}
	}
	return config.Headers, nil
}

func sameOrigin(left, right *url.URL) bool {
	return strings.EqualFold(left.Scheme, right.Scheme) &&
		strings.EqualFold(strings.TrimSuffix(left.Hostname(), "."), strings.TrimSuffix(right.Hostname(), ".")) &&
		effectivePort(left) == effectivePort(right)
}

func effectivePort(value *url.URL) string {
	if port := value.Port(); port != "" {
		return port
	}
	if strings.EqualFold(value.Scheme, "https") {
		return "443"
	}
	return "80"
}
