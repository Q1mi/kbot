// Package llm 提供LLM网关实现
package llm

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/Q1mi/kbot/internal/config"
	"github.com/Q1mi/kbot/internal/domain"
	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// ErrLocalModelRequired 表示 secret 数据缺少可用的本地模型路由。
var ErrLocalModelRequired = errors.New("secret classification requires a local model")

// provider 是一路可路由的 Provider(云 / 本地)。
type provider struct {
	model                      model.ToolCallingChatModel
	system                     string // gen_ai.system:openai-compatible / ollama
	modelID                    string // gen_ai.request.model
	providerID                 string
	deploymentID               string
	inputPricePerMillion       float64
	outputPricePerMillion      float64
	cachedInputPricePerMillion float64
}

// ResolvedDeployment 是模型控制面解析出的可调用部署。
type ResolvedDeployment struct {
	ID                         string
	ProviderID                 string
	ProviderKind               string
	BaseURL                    string
	APIKey                     string
	Model                      string
	TimeoutMS                  int
	MaxRetries                 int
	InputPricePerMillion       float64
	OutputPricePerMillion      float64
	CachedInputPricePerMillion float64
}

type ResolvedModelProfile struct {
	VersionID         string
	ClassificationMax string
	Deployments       []ResolvedDeployment
}

type ProfileResolver interface {
	ResolveProfile(ctx context.Context, versionID string) (*ResolvedModelProfile, error)
}

var ErrProjectQuotaExceeded = errors.New("project model quota exceeded")

// ProjectQuotaRequest 是调用模型前的保守额度预留。Token 和成本在成功返回后按真实 usage 结算。
type ProjectQuotaRequest struct {
	WorkspaceID, Env, ModelProfileVersionID, DeploymentID string
	ReservedTokens                                        int
	ReservedCost                                          float64
}

type ProjectQuotaEnforcer interface {
	ReserveProjectUsage(context.Context, ProjectQuotaRequest) (string, error)
	FinalizeProjectUsage(context.Context, string, int, float64, bool) error
}

// Gateway LLM网关
//

// 全局兼容路径按数据分级在云端 Provider 与本地 Ollama 之间路由。
// classification == secret 时只允许本地模型;其余分级使用云模型(见 routeFor)。
// 把 Eino 的 ToolCallingChatModel 收敛在一处,上层只依赖稳定方法(不直接碰 Eino 0.x 的 API)。
type Gateway struct {
	cloud     provider
	local     *provider     // nil 表示未配置本地 Provider
	sink      ModelCallSink // 调用计量落库(model_call_logs);默认 NopSink
	profiles  ProfileResolver
	quota     ProjectQuotaEnforcer
	models    sync.Map // profile/deployment/generation config -> provider
	endpoints interface {
		ValidateURL(context.Context, string) error
		HTTPClient(time.Duration) *http.Client
	}
}

// NewGateway 创建 LLM 网关。全局云模型是旧配置的可选回退；
// 新配置由 Model Profile 在调用时动态解析。
func NewGateway(cfg config.Config) (*Gateway, error) {
	g := &Gateway{
		sink: NopSink{},
	}
	if cfg.LLMAPIKey != "" {
		cloudM, err := openai.NewChatModel(context.Background(), &openai.ChatModelConfig{
			APIKey: cfg.LLMAPIKey, BaseURL: cfg.LLMBaseURL, Model: cfg.LLMModel,
		})
		if err != nil {
			return nil, fmt.Errorf("create cloud chat model: %w", err)
		}
		g.cloud = provider{model: cloudM, system: "openai-compatible", modelID: cfg.LLMModel}
	}
	if cfg.OllamaBaseURL != "" {
		localM, lerr := openai.NewChatModel(context.Background(), &openai.ChatModelConfig{
			APIKey:  "ollama", // Ollama 的 OpenAI 兼容端点忽略密钥,占位即可
			BaseURL: cfg.OllamaBaseURL,
			Model:   cfg.OllamaModel,
		})
		if lerr != nil {
			return nil, fmt.Errorf("create local(ollama) chat model: %w", lerr)
		}
		g.local = &provider{model: localM, system: "ollama", modelID: cfg.OllamaModel}
	}
	return g, nil
}

// routeFor 按数据分级选 Provider:secret 只允许本地模型,其余分级使用云模型。
func (g *Gateway) routeFor(classification string) (provider, error) {
	if classification == "secret" {
		if g.local == nil {
			return provider{}, ErrLocalModelRequired
		}
		return *g.local, nil
	}
	return g.cloud, nil
}

// WithCallSink 注入调用计量落库器(db != nil 时为 PgModelCallSink)。返回自身便于链式。
func (g *Gateway) WithCallSink(sink ModelCallSink) *Gateway {
	if sink != nil {
		g.sink = sink
	}
	return g
}

func (g *Gateway) WithProfileResolver(resolver ProfileResolver) *Gateway {
	g.profiles = resolver
	return g
}

func (g *Gateway) WithProjectQuota(enforcer ProjectQuotaEnforcer) *Gateway {
	g.quota = enforcer
	return g
}

func (g *Gateway) WithEndpointPolicy(policy interface {
	ValidateURL(context.Context, string) error
	HTTPClient(time.Duration) *http.Client
}) *Gateway {
	g.endpoints = policy
	return g
}

// ChatRequest 聊天请求。
type ChatRequest struct {
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

// ChatMessage 聊天消息
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatResponse 聊天响应
type ChatResponse struct {
	Content string `json:"content"`
}

// Chat 发起一次纯文本聊天（无工具）。
func (g *Gateway) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	messages := make([]*schema.Message, len(req.Messages))
	for i, msg := range req.Messages {
		messages[i] = &schema.Message{
			Role:    schema.RoleType(msg.Role),
			Content: msg.Content,
		}
	}

	p, err := g.routeFor(classificationFromContext(ctx))
	if err != nil {
		return nil, err
	}
	if p.model == nil {
		return nil, fmt.Errorf("no model profile configured and KBOT_LLM_API_KEY fallback is empty")
	}
	resp, err := p.model.Generate(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("generate response: %w", err)
	}

	return &ChatResponse{Content: resp.Content}, nil
}

// Generate 用给定的工具集发起一次生成，返回完整的助手消息（可能带 ToolCalls）。
// tools 非空时绑定工具供模型按需调用。
func (g *Gateway) Generate(ctx context.Context, messages []*schema.Message, tools []*schema.ToolInfo) (*schema.Message, error) {
	classification := classificationFromContext(ctx)
	inv := invocationFromContext(ctx)
	providers, err := g.providersFor(ctx, inv, classification)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for _, candidate := range providers {
		attempts := candidate.retries + 1
		for attempt := 0; attempt < attempts; attempt++ {
			resp, callErr := g.generateWithProvider(ctx, candidate.provider, messages, tools, inv, classification)
			if callErr == nil {
				return resp, nil
			}
			lastErr = callErr
		}
	}
	return nil, fmt.Errorf("generate response: %w", lastErr)
}

type providerCandidate struct {
	provider provider
	retries  int
}

func (g *Gateway) providersFor(ctx context.Context, inv InvocationConfig, classification string) ([]providerCandidate, error) {
	if inv.ModelProfileVersionID == "" || g.profiles == nil {
		p, err := g.routeFor(classification)
		if err != nil {
			return nil, err
		}
		if p.model == nil {
			return nil, fmt.Errorf("no model profile configured and global LLM fallback is disabled")
		}
		return []providerCandidate{{provider: p}}, nil
	}
	profile, err := g.profiles.ResolveProfile(ctx, inv.ModelProfileVersionID)
	if err != nil {
		return nil, fmt.Errorf("resolve model profile: %w", err)
	}
	if classificationRank(classification) > classificationRank(profile.ClassificationMax) {
		return nil, fmt.Errorf("model profile %s cannot process classification %s", profile.VersionID, classification)
	}
	out := make([]providerCandidate, 0, len(profile.Deployments))
	for _, d := range profile.Deployments {
		p, err := g.dynamicProvider(ctx, profile.VersionID, d, inv.GenerationConfig)
		if err != nil {
			return nil, err
		}
		out = append(out, providerCandidate{provider: p, retries: d.MaxRetries})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("model profile %s has no deployments", profile.VersionID)
	}
	return out, nil
}

func (g *Gateway) dynamicProvider(ctx context.Context, profileID string, d ResolvedDeployment, cfg domain.GenerationConfig) (provider, error) {
	if g.endpoints != nil {
		if err := g.endpoints.ValidateURL(ctx, d.BaseURL); err != nil {
			return provider{}, fmt.Errorf("validate model deployment %s endpoint: %w", d.ID, err)
		}
	}
	cacheInput, _ := json.Marshal(struct {
		Deployment ResolvedDeployment
		Generation domain.GenerationConfig
	}{Deployment: d, Generation: cfg})
	cacheHash := sha256.Sum256(cacheInput)
	key := fmt.Sprintf("%s:%x", profileID, cacheHash)
	if cached, ok := g.models.Load(key); ok {
		return cached.(provider), nil
	}
	timeout := time.Duration(d.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	modelConfig := &openai.ChatModelConfig{
		APIKey: d.APIKey, BaseURL: d.BaseURL, Model: d.Model, Timeout: timeout,
		MaxCompletionTokens: cfg.MaxOutputTokens, Temperature: cfg.Temperature,
		TopP: cfg.TopP, Stop: cfg.Stop, Seed: cfg.Seed,
	}
	if g.endpoints != nil {
		modelConfig.HTTPClient = g.endpoints.HTTPClient(timeout)
	}
	m, err := openai.NewChatModel(ctx, modelConfig)
	if err != nil {
		return provider{}, fmt.Errorf("create model deployment %s: %w", d.ID, err)
	}
	p := provider{
		model: m, system: d.ProviderKind, modelID: d.Model,
		providerID: d.ProviderID, deploymentID: d.ID,
		inputPricePerMillion: d.InputPricePerMillion, outputPricePerMillion: d.OutputPricePerMillion,
		cachedInputPricePerMillion: d.CachedInputPricePerMillion,
	}
	g.models.Store(key, p)
	return p, nil
}

func (g *Gateway) generateWithProvider(
	ctx context.Context,
	p provider,
	messages []*schema.Message,
	tools []*schema.ToolInfo,
	inv InvocationConfig,
	classification string,
) (*schema.Message, error) {
	m := p.model
	if len(tools) > 0 {
		bound, err := p.model.WithTools(tools)
		if err != nil {
			return nil, fmt.Errorf("bind tools: %w", err)
		}
		m = bound
	}
	reservationID := ""
	if g.quota != nil && inv.WorkspaceID != "" && inv.ModelProfileVersionID != "" {
		estimatedInput := estimateMessageTokens(messages)
		estimatedOutput := 1024
		if inv.GenerationConfig.MaxOutputTokens != nil && *inv.GenerationConfig.MaxOutputTokens > 0 {
			estimatedOutput = *inv.GenerationConfig.MaxOutputTokens
		}
		estimatedCost := tokenCost(p, estimatedInput, estimatedOutput, 0)
		env := inv.Environment
		if env == "" {
			env = "dev"
		}
		var reserveErr error
		reservationID, reserveErr = g.quota.ReserveProjectUsage(ctx, ProjectQuotaRequest{
			WorkspaceID: inv.WorkspaceID, Env: env, ModelProfileVersionID: inv.ModelProfileVersionID,
			DeploymentID: p.deploymentID, ReservedTokens: estimatedInput + estimatedOutput,
			ReservedCost: estimatedCost,
		})
		if reserveErr != nil {
			return nil, reserveErr
		}
	}
	var resp *schema.Message
	start := time.Now()
	res, err := withSpan(ctx, p.system, p.modelID, func(ctx context.Context) (callResult, error) {
		r, err := m.Generate(ctx, messages)
		if err != nil {
			return callResult{}, err
		}
		resp = r
		cr := callResult{}
		if raw, marshalErr := json.Marshal(messages); marshalErr == nil {
			cr.input = string(raw)
		}
		if raw, marshalErr := json.Marshal(r); marshalErr == nil {
			cr.output = string(raw)
		}
		if r.ResponseMeta != nil && r.ResponseMeta.Usage != nil {
			cr.inputTokens = r.ResponseMeta.Usage.PromptTokens
			cr.outputTokens = r.ResponseMeta.Usage.CompletionTokens
			cr.cachedTokens = r.ResponseMeta.Usage.PromptTokenDetails.CachedTokens
		}
		if r.ResponseMeta != nil {
			cr.finishReason = r.ResponseMeta.FinishReason
		}
		return cr, nil
	})
	status := "ok"
	if err != nil {
		status = "error"
	}
	actualCost := tokenCost(p, res.inputTokens, res.outputTokens, res.cachedTokens)
	if reservationID != "" {
		if finalizeErr := g.quota.FinalizeProjectUsage(
			ctx, reservationID, res.inputTokens+res.outputTokens, actualCost, err == nil,
		); finalizeErr != nil {
			// Provider 调用已经发生，账本保留预留值；继续返回原结果可避免上层重放计费调用。
			log.Printf("finalize project model quota: reservation_id=%s error=%v", reservationID, finalizeErr)
		}
	}
	g.sink.Record(ctx, CallUsage{
		Provider: p.system, ProviderID: p.providerID, DeploymentID: p.deploymentID,
		Model: p.modelID, InputTokens: res.inputTokens, OutputTokens: res.outputTokens,
		CachedTokens: res.cachedTokens, LatencyMs: int(time.Since(start).Milliseconds()),
		Status: status, Classification: classification, WorkspaceID: inv.WorkspaceID,
		AgentID: inv.AgentID, UserID: inv.UserID, PromptVersionID: inv.PromptVersionID,
		ModelProfileVersionID: inv.ModelProfileVersionID, ExperimentID: inv.ExperimentID,
		ExperimentVariant: inv.ExperimentVariant,
		Cost:              actualCost,
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func estimateMessageTokens(messages []*schema.Message) int {
	raw, err := json.Marshal(messages)
	if err != nil || len(raw) == 0 {
		return 1
	}
	// 各模型 tokenizer 不同；字节/2 对中英文混合输入保留更充足的调用前配额，最终以 Provider usage 结算。
	tokens := (len(raw) + 1) / 2
	if tokens < 1 {
		return 1
	}
	return tokens
}

func tokenCost(p provider, inputTokens, outputTokens, cachedTokens int) float64 {
	uncached := inputTokens - cachedTokens
	if uncached < 0 {
		uncached = 0
	}
	return (float64(uncached)*p.inputPricePerMillion +
		float64(cachedTokens)*p.cachedInputPricePerMillion +
		float64(outputTokens)*p.outputPricePerMillion) / 1_000_000
}

func classificationRank(v string) int {
	switch v {
	case "secret":
		return 3
	case "confidential":
		return 2
	case "internal":
		return 1
	default:
		return 0
	}
}
