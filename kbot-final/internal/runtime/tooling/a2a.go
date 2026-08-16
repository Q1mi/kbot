package tooling

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/Q1mi/kbot/internal/platform/tool"
	"github.com/google/uuid"
)

// a2aExecutor 通过 A2A v1 JSON-RPC binding 调用外部 Agent。
// 从 kbot 视角，外部 Agent 与内部 Tool 是同一种东西——都交给 Engine 循环统一调度。
type a2aExecutor struct {
	client  *http.Client
	cardURL string
	headers map[string]string
}

func newA2AExecutor(client *http.Client, cfg *tool.ToolConfig) Executor {
	cardURL, _ := cfg.EndpointConfig["card_url"].(string)
	if cardURL == "" {
		cardURL, _ = cfg.EndpointConfig["url"].(string)
	}
	return &a2aExecutor{
		client:  client,
		cardURL: cardURL,
		headers: authHeaders(cfg.AuthConfig),
	}
}

// AgentCard 是 A2A v1 的最小能力描述。supportedInterfaces 是按偏好排序的数组。
type AgentCard struct {
	Name                string         `json:"name"`
	SupportedInterfaces []a2aInterface `json:"supportedInterfaces"`
}

type a2aInterface struct {
	URL             string `json:"url"`
	ProtocolBinding string `json:"protocolBinding"`
	ProtocolVersion string `json:"protocolVersion"`
	Tenant          string `json:"tenant,omitempty"`
}

func (e *a2aExecutor) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if e.cardURL == "" {
		return "", fmt.Errorf("a2a tool: empty card_url")
	}

	// 1. 取 AgentCard。
	card, err := e.fetchCard(ctx)
	if err != nil {
		return "", fmt.Errorf("fetch agent card: %w", err)
	}

	// 2. 按 AgentCard 顺序选择第一个 JSONRPC binding。
	iface, err := selectJSONRPCInterface(card.SupportedInterfaces)
	if err != nil {
		return "", err
	}
	if err := validateA2AInterfaceURL(e.cardURL, iface.URL); err != nil {
		return "", err
	}

	// 3. 把普通 Tool 参数归一化成 SendMessageRequest。调用方也可以直接传完整 message 对象。
	params, err := buildSendMessageParams(args, iface.Tenant)
	if err != nil {
		return "", err
	}
	result, err := e.rpcCall(ctx, iface, "SendMessage", params)
	if err != nil {
		return "", err
	}
	return string(result), nil
}

func validateA2AInterfaceURL(cardURL, interfaceURL string) error {
	card, err := url.Parse(cardURL)
	if err != nil || card.Hostname() == "" {
		return fmt.Errorf("invalid A2A card URL")
	}
	endpoint, err := url.Parse(interfaceURL)
	if err != nil || endpoint.Hostname() == "" || endpoint.User != nil ||
		(endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return fmt.Errorf("invalid A2A interface URL")
	}
	if !sameA2AOrigin(card, endpoint) {
		return fmt.Errorf("A2A interface must use the same origin as its AgentCard")
	}
	return nil
}

func sameA2AOrigin(left, right *url.URL) bool {
	return strings.EqualFold(left.Scheme, right.Scheme) &&
		strings.EqualFold(strings.TrimSuffix(left.Hostname(), "."), strings.TrimSuffix(right.Hostname(), ".")) &&
		a2aPort(left) == a2aPort(right)
}

func a2aPort(value *url.URL) string {
	if port := value.Port(); port != "" {
		return port
	}
	if strings.EqualFold(value.Scheme, "https") {
		return "443"
	}
	return "80"
}

func (e *a2aExecutor) fetchCard(ctx context.Context) (*AgentCard, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.cardURL, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range e.headers {
		req.Header.Set(k, v)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := readToolResponse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read agent card: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("card endpoint returned %d", resp.StatusCode)
	}
	var card AgentCard
	if err := json.Unmarshal(body, &card); err != nil {
		return nil, err
	}
	return &card, nil
}

func selectJSONRPCInterface(interfaces []a2aInterface) (a2aInterface, error) {
	for _, iface := range interfaces {
		if strings.EqualFold(iface.ProtocolBinding, "JSONRPC") && iface.URL != "" {
			return iface, nil
		}
	}
	return a2aInterface{}, fmt.Errorf("remote agent has no A2A JSONRPC interface")
}

func buildSendMessageParams(args json.RawMessage, tenant string) (map[string]any, error) {
	var input map[string]any
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("parse a2a args: %w", err)
	}

	// 高级调用方可直接传 A2A Message 对象以及 configuration/metadata。
	if msg, ok := input["message"].(map[string]any); ok {
		if _, exists := msg["messageId"]; !exists {
			msg["messageId"] = uuid.NewString()
		}
		if _, exists := msg["role"]; !exists {
			msg["role"] = "ROLE_USER"
		}
		if tenant != "" {
			input["tenant"] = tenant
		}
		return input, nil
	}

	text := ""
	for _, key := range []string{"message", "text", "prompt", "input"} {
		if value, ok := input[key].(string); ok && value != "" {
			text = value
			break
		}
	}
	if text == "" {
		b, err := json.Marshal(input)
		if err != nil {
			return nil, fmt.Errorf("marshal a2a message: %w", err)
		}
		text = string(b)
	}

	params := map[string]any{
		"message": map[string]any{
			"role":      "ROLE_USER",
			"parts":     []map[string]any{{"text": text}},
			"messageId": uuid.NewString(),
		},
	}
	if tenant != "" {
		params["tenant"] = tenant
	}
	return params, nil
}

type jsonRPCRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      int            `json:"id"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params"`
}

type jsonRPCResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (e *a2aExecutor) rpcCall(ctx context.Context, iface a2aInterface, method string, params map[string]any) (json.RawMessage, error) {
	reqBody, err := json.Marshal(jsonRPCRequest{JSONRPC: "2.0", ID: 1, Method: method, Params: params})
	if err != nil {
		return nil, fmt.Errorf("marshal jsonrpc request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, iface.URL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if iface.ProtocolVersion != "" {
		req.Header.Set("A2A-Version", iface.ProtocolVersion)
	}
	for k, v := range e.headers {
		req.Header.Set(k, v)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := readToolResponse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read jsonrpc response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("a2a endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var rpcResp jsonRPCResponse
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return nil, fmt.Errorf("parse jsonrpc response: %w", err)
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("a2a rpc error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}
	return rpcResp.Result, nil
}
