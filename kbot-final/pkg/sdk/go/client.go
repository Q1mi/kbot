// Package kbot 是 kbot 平台的 Go SDK（设计文档 §4.X-2 / 讲义 §15.10）。
//
// 提供 idiomatic 的入口：登录拿 token、创建 Agent、对话（同步 / 流式）。
// kbotctl 内部直接用本 SDK——给 SDK 一份强约束：自己用得起来，外部也用得起来。
package kbot

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client 是 kbot API 客户端。
type Client struct {
	baseURL     string
	apiKey      string // JWT 或 API Key
	workspaceID string
	http        *http.Client
}

// Option 配置 Client。
type Option func(*Client)

// WithWorkspace 设置默认 workspace。
func WithWorkspace(id string) Option { return func(c *Client) { c.workspaceID = id } }

// WithHTTPClient 自定义底层 http.Client。
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// NewClient 创建客户端。
func NewClient(baseURL, apiKey string, opts ...Option) *Client {
	c := &Client{
		baseURL:     strings.TrimRight(baseURL, "/"),
		apiKey:      apiKey,
		workspaceID: "default",
		http:        &http.Client{Timeout: 60 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Login 用账密登录，成功后把返回的 token 设为后续请求凭证。
func (c *Client) Login(ctx context.Context, email, password string) error {
	var out struct {
		Token string `json:"token"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/v1/auth/login",
		map[string]string{"email": email, "password": password}, &out); err != nil {
		return err
	}
	c.apiKey = out.Token
	return nil
}

// CreateAgentRequest 创建 Agent 的请求体。
type CreateAgentRequest struct {
	Name           string   `json:"name"`
	Template       string   `json:"template,omitempty"`
	SystemPrompt   string   `json:"system_prompt,omitempty"`
	SystemPromptID string   `json:"system_prompt_id,omitempty"`
	UserPromptID   string   `json:"user_prompt_id,omitempty"`
	PromptEnv      string   `json:"prompt_env,omitempty"`
	ToolIDs        []string `json:"tool_ids,omitempty"`
	SkillIDs       []string `json:"skill_version_ids,omitempty"`
}

// Agent 是创建返回的 Agent。
type Agent struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// CreateAgent 创建一个 Agent。
func (c *Client) CreateAgent(ctx context.Context, req CreateAgentRequest) (*Agent, error) {
	var a Agent
	if err := c.do(ctx, http.MethodPost, "/api/v1/agents", req, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

// UserPromptInputSpec 描述 Agent 首轮任务模板及其变量 Schema。
type UserPromptInputSpec struct {
	Enabled         bool   `json:"enabled"`
	PromptID        string `json:"prompt_id,omitempty"`
	PromptName      string `json:"prompt_name,omitempty"`
	PromptEnv       string `json:"prompt_env,omitempty"`
	PromptVersionID string `json:"prompt_version_id,omitempty"`
	PromptVersion   int    `json:"prompt_version,omitempty"`
	VariablesSchema string `json:"variables_schema,omitempty"`
}

// GetUserPromptInputSpec 获取 Agent 当前环境的首轮任务模板输入规范。
func (c *Client) GetUserPromptInputSpec(ctx context.Context, agentID, agentEnv string) (*UserPromptInputSpec, error) {
	if agentEnv == "" {
		agentEnv = "dev"
	}
	var spec UserPromptInputSpec
	path := "/api/v1/agents/" + agentID + "/input-schema?agent_env=" + url.QueryEscape(agentEnv)
	if err := c.do(ctx, http.MethodGet, path, nil, &spec); err != nil {
		return nil, err
	}
	return &spec, nil
}

// Chat 同步对话，返回最终回答。
func (c *Client) Chat(ctx context.Context, agentID, message string) (string, error) {
	var out struct {
		Content string `json:"content"`
	}
	err := c.do(ctx, http.MethodPost, "/api/v1/agents/"+agentID+"/chat",
		map[string]string{"message": message}, &out)
	return out.Content, err
}

// ChatWithUserPrompt 使用 Agent 绑定的 User Prompt Template 渲染首轮业务任务。
func (c *Client) ChatWithUserPrompt(
	ctx context.Context,
	agentID, promptVersionID string,
	variables map[string]any,
	additionalInstruction string,
) (string, error) {
	var out struct {
		Content string `json:"content"`
	}
	err := c.do(ctx, http.MethodPost, "/api/v1/agents/"+agentID+"/chat", map[string]any{
		"message": additionalInstruction, "user_prompt_version_id": promptVersionID,
		"user_prompt_variables": variables,
	}, &out)
	return out.Content, err
}

// Event 是流式事件。
type Event struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Stream 流式对话，把 SSE 事件包成 <-chan Event。
func (c *Client) Stream(ctx context.Context, agentID, message string) (<-chan Event, error) {
	body, err := json.Marshal(map[string]string{"message": message})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/stream/agents/"+agentID+"/chat", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)
	streamClient := *c.http
	streamClient.Timeout = 0
	resp, err := streamClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		resp.Body.Close()
		return nil, fmt.Errorf("stream returned %d", resp.StatusCode)
	}

	out := make(chan Event)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			line := sc.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var ev Event
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev); err != nil {
				continue
			}
			select {
			case out <- ev:
			case <-ctx.Done():
				return
			}
		}
		if err := sc.Err(); err != nil && ctx.Err() == nil {
			select {
			case out <- Event{Type: "error", Text: err.Error()}:
			case <-ctx.Done():
			}
		}
	}()
	return out, nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	req.Header.Set("X-Workspace-ID", c.workspaceID)
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body *bytes.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	} else {
		body = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	c.setHeaders(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Body)
		return fmt.Errorf("kbot api %s %s: %d %s", method, path, resp.StatusCode, buf.String())
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}
