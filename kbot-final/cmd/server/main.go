// Command server 是 kbot 平台的入口：读配置、装配依赖、起 HTTP/WS、优雅退出。

package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/Q1mi/kbot/internal/api"
	v1 "github.com/Q1mi/kbot/internal/api/v1"
	"github.com/Q1mi/kbot/internal/config"
	"github.com/Q1mi/kbot/internal/infrastructure/jobs"
	"github.com/Q1mi/kbot/internal/infrastructure/objstore"
	"github.com/Q1mi/kbot/internal/infrastructure/otel"
	"github.com/Q1mi/kbot/internal/infrastructure/postgres"
	redisinfra "github.com/Q1mi/kbot/internal/infrastructure/redis"
	"github.com/Q1mi/kbot/internal/platform"
	"github.com/Q1mi/kbot/internal/platform/coursepreset"
	"github.com/Q1mi/kbot/internal/platform/maintenance"
	"github.com/Q1mi/kbot/internal/platform/modelconfig"
	"github.com/Q1mi/kbot/internal/platform/tool"
	"github.com/Q1mi/kbot/internal/runtime/cache"
	"github.com/Q1mi/kbot/internal/runtime/engine"
	"github.com/Q1mi/kbot/internal/runtime/llm"
	"github.com/Q1mi/kbot/internal/runtime/promptcache"
	"github.com/Q1mi/kbot/internal/runtime/retriever"
	"github.com/Q1mi/kbot/internal/runtime/sandbox"
)

// @title           kbot API
// @version         1.0
// @description     kbot 企业级 AI Agent 教学平台 REST API，由 swaggo/swag 从 handler 注解生成。
// @BasePath        /api/v1
// @securityDefinitions.apikey  BearerAuth
// @in              header
// @name            Authorization
func main() {
	cfg := config.Load()
	cfg.MustValidate() // 必填配置启动时就校验（快速失败）

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 基础设施装配
	log.Println("Initializing infrastructure...")

	// 1. OpenTelemetry
	otelCleanup := otel.MustInit(ctx, otel.Config{
		Endpoint: cfg.OTLPEndpoint, Headers: cfg.OTLPHeaders,
		ServiceName: "kbot-server", ServiceVersion: cfg.ServiceVersion,
		Environment: cfg.Environment, SampleRatio: cfg.OTELSampleRatio,
	})
	defer otelCleanup()

	// 2. 数据库
	db := postgres.MustOpen(ctx, cfg.DatabaseURL)
	defer db.Close()

	// 2.1 确保审计/计量的月分区存在(上月/本月/下月)。
	// 写入方(server)自己兜底,不依赖 worker 是否在跑——否则只起 server 时,
	// 审计写入会静默落进 audit_logs_default 永不归档(留证闭环断裂)。worker 的 cron 仍负责长期滚动。
	if err := maintenance.NewService(db, nil, cfg.AuditArchiveAfterMonths).
		EnsureUpcomingPartitions(ctx, time.Now().UTC()); err != nil {
		log.Printf("ensure audit partitions at startup: %v", err)
	}

	// 3. Redis
	rds := redisinfra.MustOpen(ctx, cfg.RedisURL)
	defer rds.Close()

	// 4. 任务队列客户端
	jobsClient := jobs.NewClient(rds)
	defer jobsClient.Close()

	// 平台服务装配（Prompt 中心用 Redis Pub/Sub 广播失效）
	log.Println("Initializing platform services...")
	publisher := redisinfra.NewPublisher(rds)
	// local 嵌入器供离线环境使用，openai 模式调用配置的 /embeddings 端点。
	embedder, err := retriever.NewEmbedder(cfg.EmbedderKind, cfg.EmbedderDim, cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.EmbedderModel)
	must(err)
	// jobsClient 作为 KB ingest 的异步投递器:SyncMarkdownFolder → 入队 → worker 落 kb_chunks。
	sandboxClient, err := sandbox.NewClientWithTimeout(cfg.SandboxRunnerURL, cfg.SandboxRunnerToken, cfg.SandboxRunnerTimeout)
	must(err)
	platformService := platform.NewServiceWithSandboxRunner(db, rds, cfg.JWTKeyBytes(), publisher, embedder, jobsClient,
		sandboxClient, []byte(cfg.CredentialEncryptionKey))
	endpointPolicy := tool.NewEndpointPolicy(cfg.ToolAllowedHosts, cfg.ToolAllowPrivateNetwork)
	platformService.Tool.ConfigureEndpointPolicy(endpointPolicy)
	platformService.ModelConfig.ConfigureEndpointPolicy(endpointPolicy)
	platformService.KB.ConfigureMarkdownAllowedRoots(cfg.KBMarkdownAllowedRoots)
	if err := platformService.Tool.MigrateLegacyCredentials(ctx); err != nil {
		log.Fatalf("migrate legacy tool credentials: %v", err)
	}

	// 首启自动 seed admin:让 `make up && open localhost:8080` 直接能登录(make seed 沦为可选)
	if cfg.AutoseedAdmin {
		if err := platformService.IAM.EnsureSeedAdmin(ctx, cfg.AutoseedAdminEmail, cfg.AutoseedAdminPassword); err != nil {
			log.Printf("autoseed admin: %v", err)
		} else {
			log.Printf("✅ admin ready: %s", cfg.AutoseedAdminEmail)
		}
		if err := platformService.IAM.EnsureSeedWorkspaces(ctx); err != nil {
			log.Printf("autoseed workspaces: %v", err)
		} else {
			log.Printf("✅ default business workspaces ready")
		}
		if err := platformService.IAM.EnsureSeedWorkspaceOwners(ctx, cfg.AutoseedAdminEmail); err != nil {
			log.Printf("autoseed workspace owners: %v", err)
		}
		if cfg.LLMAPIKey != "" {
			if err := ensureDefaultModelConfigs(ctx, platformService, cfg); err != nil {
				log.Printf("autoseed model configs: %v", err)
			} else {
				log.Printf("✅ default workspace model configs ready")
			}
		}
		created, err := coursepreset.EnsurePrompts(
			ctx,
			platformService.IAM,
			platformService.ModelConfig,
			platformService.Prompt,
		)
		if err != nil {
			log.Printf("autoseed course prompts: %v", err)
		} else {
			log.Printf("✅ course prompt presets ready: %d created", created)
		}
		if cfg.AutoseedCourseAssets {
			counts, err := coursepreset.EnsureBusinessAssets(
				ctx,
				platformService.IAM,
				platformService.Prompt,
				platformService.KB,
				platformService.Tool,
				platformService.Skill,
				platformService.Agent,
				platformService.Registry,
				coursepreset.DefaultBusinessAssetOptions(),
			)
			if err != nil {
				log.Printf("autoseed course business assets: %v", err)
			} else {
				log.Printf("✅ course business assets ready: kbs=%d documents=%d tools=%d skills=%d skill_versions=%d agents=%d agent_versions=%d created",
					counts.KnowledgeBases, counts.Documents, counts.Tools, counts.Skills,
					counts.SkillVersions, counts.Agents, counts.AgentVersions)
			}
		}
	}

	// 启动 Prompt 缓存订阅器：收到失效通知后异步重拉编译产物（Apollo 风格推送）
	promptSub := promptcache.NewSubscriber(rds, platformService.Prompt.RefreshCache)
	go func() {
		if err := promptSub.Run(ctx); err != nil && ctx.Err() == nil {
			log.Printf("prompt subscriber stopped: %v", err)
		}
	}()

	// Runtime装配
	log.Println("Initializing runtime...")
	llmGateway, err := llm.NewGateway(cfg)
	must(err)
	llmGateway.WithProfileResolver(platformService.ModelConfig)
	llmGateway.WithProjectQuota(platformService.ModelConfig)
	// 模型调用计量包含 Prompt Cache 命中量。
	if db != nil {
		llmGateway.WithCallSink(llm.NewPgModelCallSink(db))
	}
	llmGateway.WithEndpointPolicy(endpointPolicy)

	runtime := engine.NewEngine(platformService.Agent, llmGateway, platformService.Registry).
		WithGuard(platformService.Guard).
		WithAudit(platformService.Audit).
		WithToolAudit(platformService.Tool).
		WithApprovals(platformService.ApprovalGate()).
		WithTracing(engine.TraceOptions{CaptureContent: cfg.OTELCaptureContent})
	defer platformService.Audit.Close()

	// 对象存储未配置或不可用时，审计导出返回 503。
	var exporter *objstore.Client
	if cfg.S3Endpoint != "" {
		if exporter, err = objstore.New(ctx, cfg.S3Endpoint, cfg.S3AccessKey, cfg.S3SecretKey, cfg.S3Bucket); err != nil {
			log.Printf("objstore 不可用,/audit/exports 将返回 503: %v", err)
			exporter = nil
		}
	}

	// HTTP服务装配
	log.Println("Initializing HTTP server...")
	var auditExporter v1.ObjectExporter
	if exporter != nil {
		auditExporter = exporter
	}
	handler := api.NewHandler(platformService, runtime, jobsClient, cfg.WebhookSecret, auditExporter).
		SetAllowedOrigins(cfg.CORSAllowedOrigins).
		SetIdempotencyStore(cache.NewRedisIdemStore(rds)).
		SetLark(api.LarkConfig{
			VerifyToken:     cfg.LarkVerifyToken,
			EncryptKey:      cfg.LarkEncryptKey,
			AgentID:         cfg.LarkAgentID,
			OutboundEnabled: cfg.LarkAppID != "" && cfg.LarkAppSecret != "",
		}).
		SetObservability(api.ObservabilityConfig{
			OTLPEndpoint: cfg.OTLPEndpoint, LangfuseUIURL: cfg.LangfuseUIURL,
			LangfuseProjectID: cfg.LangfuseProjectID,
		}).
		SetReadiness(
			api.ReadinessCheck{Name: "postgres", Check: db.Ping},
			api.ReadinessCheck{Name: "redis", Check: func(ctx context.Context) error { return rds.Ping(ctx).Err() }},
			api.ReadinessCheck{Name: "sandbox-runner", Check: sandboxClient.Check},
		)

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		// SSE/WS 是长连接，因此不设置整体 WriteTimeout。
	}

	// 启动HTTP服务
	go func() {
		log.Printf("kbot server listening on %s", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	// 等待停止信号
	<-ctx.Done()
	log.Println("shutting down...")

	// 优雅关闭
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)

	log.Println("server stopped")
}

func ensureDefaultModelConfigs(ctx context.Context, services *platform.Service, cfg config.Config) error {
	workspaces, err := services.IAM.ListWorkspaces(ctx, 200, 0)
	if err != nil {
		return err
	}
	workspaceByName := make(map[string]string, len(workspaces))
	for _, workspace := range workspaces {
		workspaceByName[workspace.Name] = workspace.ID
	}
	seeds := []struct {
		workspaceName string
		providerName  string
		profiles      []modelconfig.SeedProfile
	}{
		{
			workspaceName: "跨境电商运营平台",
			providerName:  "电商专用模型账号",
			profiles: []modelconfig.SeedProfile{
				{Name: "商品运营 Profile", Description: "商品信息生成、翻译与运营分析", DeploymentName: "商品运营模型", ClassificationMax: "internal"},
				{Name: "供应链协同 Profile", Description: "订单、库存、物流与结算协同", DeploymentName: "供应链协同模型", ClassificationMax: "confidential"},
			},
		},
		{
			workspaceName: "保险理赔与反欺诈平台",
			providerName:  "保险专用模型账号",
			profiles: []modelconfig.SeedProfile{
				{Name: "理赔审核 Profile", Description: "保单责任、材料完整性与理赔结论辅助审核", DeploymentName: "理赔审核模型", ClassificationMax: "confidential"},
				{Name: "反欺诈分析 Profile", Description: "欺诈特征汇总、风险解释与调查建议", DeploymentName: "反欺诈分析模型", ClassificationMax: "confidential"},
			},
		},
	}
	for _, seed := range seeds {
		workspaceID := workspaceByName[seed.workspaceName]
		if workspaceID == "" {
			return fmt.Errorf("default workspace %q not found", seed.workspaceName)
		}
		baseURL, modelName := cfg.LLMBaseURL, cfg.LLMModel
		if cfg.AutoseedCourseAssets {
			switch seed.workspaceName {
			case "跨境电商运营平台":
				baseURL, modelName = "http://crossborder-sim:8091/v1", "crossborder-agent-mock"
			case "保险理赔与反欺诈平台":
				baseURL, modelName = "http://insurance-sim:8092/v1", "insurance-agent-mock"
			}
		}
		if err := services.ModelConfig.EnsureSeedWorkspaceConfig(ctx, modelconfig.SeedWorkspaceConfig{
			WorkspaceID: workspaceID, ProviderName: seed.providerName,
			ProviderKind: "openai-compatible", BaseURL: baseURL, APIKey: cfg.LLMAPIKey,
			ModelName: modelName, Region: cfg.Environment, CreatedBy: "system", Profiles: seed.profiles,
		}); err != nil {
			return fmt.Errorf("seed model config for %q: %w", seed.workspaceName, err)
		}
	}
	return nil
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
