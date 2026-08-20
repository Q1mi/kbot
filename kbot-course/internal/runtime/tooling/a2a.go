package tooling

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// A2AClient implements the synchronous JSON-RPC binding from A2A v1.0.1.
// It discovers the endpoint from the AgentCard on every call, so interface
// changes do not require rebuilding the local tool version.
type A2AClient struct {
	cardURL      string
	client       *http.Client
	allowedHosts map[string]struct{}
	headers      map[string]string
}

type AgentCard struct {
	Name                string         `json:"name"`
	SupportedInterfaces []A2AInterface `json:"supportedInterfaces"`
}

type A2AInterface struct {
	URL             string `json:"url"`
	ProtocolBinding string `json:"protocolBinding"`
	ProtocolVersion string `json:"protocolVersion"`
	Tenant          string `json:"tenant,omitempty"`
}

type A2AMessage struct {
	Role      string    `json:"role"`
	Parts     []A2APart `json:"parts"`
	MessageID string    `json:"messageId"`
}

type A2APart struct {
	Text string `json:"text"`
}

type A2AResult struct {
	TaskID  string      `json:"task_id,omitempty"`
	Status  string      `json:"status,omitempty"`
	Content string      `json:"content,omitempty"`
	Message *A2AMessage `json:"message,omitempty"`
}

func NewA2AClient(cardURL string, allowedHosts ...string) *A2AClient {
	allowed := make(map[string]struct{}, len(allowedHosts))
	for _, host := range allowedHosts {
		if host = strings.ToLower(strings.TrimSpace(host)); host != "" {
			allowed[host] = struct{}{}
		}
	}
	return &A2AClient{cardURL: cardURL, client: &http.Client{Timeout: 30 * time.Second}, allowedHosts: allowed}
}

func (c *A2AClient) Send(ctx context.Context, agentVersionID, text string) (A2AResult, error) {
	card, err := c.fetchCard(ctx)
	if err != nil {
		return A2AResult{}, fmt.Errorf("fetch AgentCard: %w", err)
	}
	iface, err := selectA2AJSONRPC(card.SupportedInterfaces)
	if err != nil {
		return A2AResult{}, err
	}
	endpoint, err := c.parseEndpoint(iface.URL)
	if err != nil {
		return A2AResult{}, fmt.Errorf("invalid AgentCard interface: %w", err)
	}
	cardEndpoint, _ := c.parseEndpoint(c.cardURL)
	if !sameOrigin(cardEndpoint, endpoint) {
		return A2AResult{}, fmt.Errorf("AgentCard interface must use the card origin")
	}
	params := map[string]any{
		"message": A2AMessage{Role: "ROLE_USER", Parts: []A2APart{{Text: text}}, MessageID: newMessageID()},
	}
	if agentVersionID != "" {
		params["metadata"] = map[string]string{"agent_version_id": agentVersionID}
	}
	if iface.Tenant != "" {
		params["tenant"] = iface.Tenant
	}
	payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": newMessageID(), "method": "SendMessage", "params": params})
	if err != nil {
		return A2AResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return A2AResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if iface.ProtocolVersion != "" {
		req.Header.Set("A2A-Version", iface.ProtocolVersion)
	}
	body, err := c.do(req)
	if err != nil {
		return A2AResult{}, fmt.Errorf("SendMessage: %w", err)
	}
	var envelope struct {
		Result A2AResult `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return A2AResult{}, fmt.Errorf("decode A2A response: %w", err)
	}
	if envelope.Error != nil {
		return A2AResult{}, fmt.Errorf("A2A error %d: %s", envelope.Error.Code, envelope.Error.Message)
	}
	if envelope.Result.Content == "" && envelope.Result.Message != nil {
		var content strings.Builder
		for _, part := range envelope.Result.Message.Parts {
			content.WriteString(part.Text)
		}
		envelope.Result.Content = content.String()
	}
	return envelope.Result, nil
}

func (c *A2AClient) fetchCard(ctx context.Context) (AgentCard, error) {
	endpoint, err := c.parseEndpoint(c.cardURL)
	if err != nil {
		return AgentCard{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return AgentCard{}, err
	}
	body, err := c.do(req)
	if err != nil {
		return AgentCard{}, err
	}
	var card AgentCard
	if err := json.Unmarshal(body, &card); err != nil {
		return AgentCard{}, fmt.Errorf("decode AgentCard: %w", err)
	}
	if strings.TrimSpace(card.Name) == "" {
		return AgentCard{}, fmt.Errorf("AgentCard name is required")
	}
	return card, nil
}

func (c *A2AClient) do(req *http.Request) ([]byte, error) {
	for name, value := range c.headers {
		req.Header.Set(name, value)
	}
	client := c.client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	clone := *client
	previousRedirect := clone.CheckRedirect
	clone.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if _, err := c.parseEndpoint(req.URL.String()); err != nil {
			return err
		}
		if len(via) > 0 && !sameOrigin(via[0].URL, req.URL) {
			return fmt.Errorf("cross-origin A2A redirect is blocked")
		}
		if previousRedirect != nil {
			return previousRedirect(req, via)
		}
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		return nil
	}
	response, err := clone.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxResponseBytes {
		return nil, fmt.Errorf("A2A response exceeds %d bytes", maxResponseBytes)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	return body, nil
}

func (c *A2AClient) parseEndpoint(raw string) (*url.URL, error) {
	endpoint, err := url.Parse(raw)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.User != nil {
		return nil, fmt.Errorf("endpoint must be an absolute HTTP URL without user info")
	}
	host := strings.ToLower(endpoint.Hostname())
	if _, ok := c.allowedHosts[host]; !ok {
		return nil, fmt.Errorf("endpoint host %q is not allowlisted", host)
	}
	return endpoint, nil
}

func selectA2AJSONRPC(interfaces []A2AInterface) (A2AInterface, error) {
	for _, iface := range interfaces {
		if strings.EqualFold(iface.ProtocolBinding, "JSONRPC") && strings.TrimSpace(iface.URL) != "" {
			return iface, nil
		}
	}
	return A2AInterface{}, fmt.Errorf("remote AgentCard has no JSONRPC interface")
}

func newMessageID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return fmt.Sprintf("message-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(value[:])
}
