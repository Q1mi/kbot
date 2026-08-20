// Package llm 提供LLM网关实现
package llm

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/Q1mi/kbot/internal/config"
	"github.com/Q1mi/kbot/internal/domain"
	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"
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

// ExecutionPlan 把一次 Agent 运行所需的主模型、重试策略和部署故障转移策略交给 Eino ADK。
// 模型调用的配额、计量和链路追踪仍由 Gateway 包装器统一完成。
type ExecutionPlan struct {
	Model    model.BaseChatModel
	Retry    *adk.ModelRetryConfig
	Failover *adk.ModelFailoverConfig[*schema.Message]
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

// PrepareExecution 按当前调用上下文解析 Model Profile，并生成 Eino ADK 原生执行配置。
// 同一 Profile 中各部署的 MaxRetries 统一取最大值，作为本次 Agent 运行的模型重试预算。
func (g *Gateway) PrepareExecution(ctx context.Context) (*ExecutionPlan, error) {
	classification := classificationFromContext(ctx)
	inv := invocationFromContext(ctx)
	candidates, err := g.providersFor(ctx, inv, classification)
	if err != nil {
		return nil, err
	}

	models := make([]model.BaseChatModel, 0, len(candidates))
	maxRetries := 0
	for _, candidate := range candidates {
		models = append(models, &managedModel{
			gateway: g, provider: candidate.provider, invocation: inv, classification: classification,
		})
		if candidate.retries > maxRetries {
			maxRetries = candidate.retries
		}
	}
	plan := &ExecutionPlan{Model: models[0]}
	if maxRetries > 0 {
		plan.Retry = &adk.ModelRetryConfig{MaxRetries: maxRetries}
	}
	if len(models) > 1 {
		plan.Failover = &adk.ModelFailoverConfig[*schema.Message]{
			MaxRetries: uint(len(models) - 1),
			ShouldFailover: func(_ context.Context, _ *schema.Message, callErr error) bool {
				return callErr != nil
			},
			GetFailoverModel: func(_ context.Context, failover *adk.FailoverContext[*schema.Message]) (model.BaseChatModel, []*schema.Message, error) {
				index := int(failover.FailoverAttempt)
				if index <= 0 || index >= len(models) {
					return nil, nil, fmt.Errorf("model failover attempt %d is out of range", index)
				}
				return models[index], nil, nil
			},
		}
	}
	return plan, nil
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

// managedModel 是 Gateway 的治理包装器。ADK 负责 ReAct、重试和故障转移；
// 此包装器只处理单次 Provider 调用的配额、追踪、成本和审计。
type managedModel struct {
	gateway        *Gateway
	provider       provider
	invocation     InvocationConfig
	classification string
}

func (m *managedModel) Generate(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	reservationID, err := m.reserve(ctx, messages)
	if err != nil {
		return nil, err
	}
	var response *schema.Message
	startedAt := time.Now()
	result, callErr := withSpan(ctx, m.provider.system, m.provider.modelID, func(ctx context.Context) (callResult, error) {
		generated, err := m.provider.model.Generate(ctx, messages, opts...)
		if err != nil {
			return callResult{input: marshalJSON(messages)}, err
		}
		response = generated
		return modelCallResult(messages, generated), nil
	})
	m.finish(ctx, reservationID, result, startedAt, callErr)
	if callErr != nil {
		return nil, callErr
	}
	return response, nil
}

func (m *managedModel) Stream(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	reservationID, err := m.reserve(ctx, messages)
	if err != nil {
		return nil, err
	}
	inputStream, err := m.provider.model.Stream(ctx, messages, opts...)
	if err != nil {
		m.finish(ctx, reservationID, callResult{input: marshalJSON(messages)}, time.Now(), err)
		return nil, err
	}
	output, writer := schema.Pipe[*schema.Message](1)
	startedAt := time.Now()
	go func() {
		defer writer.Close()
		defer inputStream.Close()
		chunks := make([]*schema.Message, 0, 8)
		result, callErr := withSpan(ctx, m.provider.system, m.provider.modelID, func(context.Context) (callResult, error) {
			for {
				chunk, recvErr := inputStream.Recv()
				if errors.Is(recvErr, io.EOF) {
					break
				}
				if recvErr != nil {
					return callResult{input: marshalJSON(messages)}, recvErr
				}
				chunks = append(chunks, chunk)
				if writer.Send(chunk, nil) {
					return callResult{input: marshalJSON(messages)}, context.Canceled
				}
			}
			merged, concatErr := schema.ConcatMessages(chunks)
			if concatErr != nil {
				return callResult{input: marshalJSON(messages)}, concatErr
			}
			return modelCallResult(messages, merged), nil
		})
		m.finish(context.WithoutCancel(ctx), reservationID, result, startedAt, callErr)
		if callErr != nil {
			writer.Send(nil, callErr)
		}
	}()
	return output, nil
}

func (m *managedModel) reserve(ctx context.Context, messages []*schema.Message) (string, error) {
	if m.gateway.quota == nil || m.invocation.WorkspaceID == "" || m.invocation.ModelProfileVersionID == "" {
		return "", nil
	}
	estimatedInput := estimateMessageTokens(messages)
	estimatedOutput := 1024
	if limit := m.invocation.GenerationConfig.MaxOutputTokens; limit != nil && *limit > 0 {
		estimatedOutput = *limit
	}
	env := m.invocation.Environment
	if env == "" {
		env = "dev"
	}
	return m.gateway.quota.ReserveProjectUsage(ctx, ProjectQuotaRequest{
		WorkspaceID: m.invocation.WorkspaceID, Env: env,
		ModelProfileVersionID: m.invocation.ModelProfileVersionID, DeploymentID: m.provider.deploymentID,
		ReservedTokens: estimatedInput + estimatedOutput,
		ReservedCost:   tokenCost(m.provider, estimatedInput, estimatedOutput, 0),
	})
}

func (m *managedModel) finish(ctx context.Context, reservationID string, result callResult, startedAt time.Time, callErr error) {
	actualCost := tokenCost(m.provider, result.inputTokens, result.outputTokens, result.cachedTokens)
	if reservationID != "" {
		if err := m.gateway.quota.FinalizeProjectUsage(
			ctx, reservationID, result.inputTokens+result.outputTokens, actualCost, callErr == nil,
		); err != nil {
			log.Printf("finalize project model quota: reservation_id=%s error=%v", reservationID, err)
		}
	}
	status := "ok"
	if callErr != nil {
		status = "error"
	}
	m.gateway.sink.Record(ctx, CallUsage{
		Provider: m.provider.system, ProviderID: m.provider.providerID, DeploymentID: m.provider.deploymentID,
		Model: m.provider.modelID, InputTokens: result.inputTokens, OutputTokens: result.outputTokens,
		CachedTokens: result.cachedTokens, LatencyMs: int(time.Since(startedAt).Milliseconds()),
		Status: status, Classification: m.classification, WorkspaceID: m.invocation.WorkspaceID,
		AgentID: m.invocation.AgentID, UserID: m.invocation.UserID, PromptVersionID: m.invocation.PromptVersionID,
		ModelProfileVersionID: m.invocation.ModelProfileVersionID, ExperimentID: m.invocation.ExperimentID,
		ExperimentVariant: m.invocation.ExperimentVariant, Cost: actualCost,
	})
}

func modelCallResult(messages []*schema.Message, response *schema.Message) callResult {
	result := callResult{input: marshalJSON(messages), output: marshalJSON(response)}
	if response == nil || response.ResponseMeta == nil {
		return result
	}
	result.finishReason = response.ResponseMeta.FinishReason
	if response.ResponseMeta.Usage != nil {
		result.inputTokens = response.ResponseMeta.Usage.PromptTokens
		result.outputTokens = response.ResponseMeta.Usage.CompletionTokens
		result.cachedTokens = response.ResponseMeta.Usage.PromptTokenDetails.CachedTokens
	}
	return result
}

func marshalJSON(value any) string {
	raw, _ := json.Marshal(value)
	return string(raw)
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
