package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/Q1mi/kbot/internal/api"
	"github.com/Q1mi/kbot/internal/config"
	courseotel "github.com/Q1mi/kbot/internal/infrastructure/otel"
	postgresinfra "github.com/Q1mi/kbot/internal/infrastructure/postgres"
	"github.com/Q1mi/kbot/internal/integration"
	"github.com/Q1mi/kbot/internal/integration/lark"
	"github.com/Q1mi/kbot/internal/integration/replay"
	"github.com/Q1mi/kbot/internal/integration/webhook"
	"github.com/Q1mi/kbot/internal/platform/agent"
	"github.com/Q1mi/kbot/internal/platform/approval"
	"github.com/Q1mi/kbot/internal/platform/audit"
	platformeval "github.com/Q1mi/kbot/internal/platform/eval"
	"github.com/Q1mi/kbot/internal/platform/iam"
	"github.com/Q1mi/kbot/internal/platform/kb"
	"github.com/Q1mi/kbot/internal/platform/modelconfig"
	"github.com/Q1mi/kbot/internal/platform/prompt"
	"github.com/Q1mi/kbot/internal/platform/skill"
	platformteam "github.com/Q1mi/kbot/internal/platform/team"
	platformtool "github.com/Q1mi/kbot/internal/platform/tool"
	"github.com/Q1mi/kbot/internal/runtime/engine"
	"github.com/Q1mi/kbot/internal/runtime/guard"
	"github.com/Q1mi/kbot/internal/runtime/llm"
	"github.com/Q1mi/kbot/internal/runtime/retriever"
	"github.com/Q1mi/kbot/internal/runtime/sandbox"
	"github.com/Q1mi/kbot/internal/runtime/tooling"
)

func main() {
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		log.Fatal(err)
	}
	shutdownTracing, err := courseotel.Setup(context.Background(), "kbot-course")
	if err != nil {
		log.Fatalf("configure tracing: %v", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTracing(shutdownCtx); err != nil {
			log.Printf("shutdown tracing: %v", err)
		}
	}()
	databaseContext, databaseCancel := context.WithTimeout(context.Background(), 10*time.Second)
	pool, err := postgresinfra.Open(databaseContext, cfg.DatabaseURL)
	databaseCancel()
	if err != nil {
		log.Fatalf("connect PostgreSQL: %v", err)
	}
	defer pool.Close()
	iamService := iam.New(iam.NewPostgresStore(pool), cfg.JWTSecret, cfg.JWTIssuer)
	if cfg.BootstrapEmail != "" {
		if _, err := iamService.EnsureRegistered(context.Background(), cfg.BootstrapEmail, cfg.BootstrapPassword, cfg.BootstrapName); err != nil {
			log.Fatalf("bootstrap course administrator: %v", err)
		}
		log.Printf("bootstrapped course administrator %s", cfg.BootstrapEmail)
	}
	gateway, err := llm.NewGateway(cfg)
	if err != nil {
		log.Fatalf("create LLM gateway: %v", err)
	}
	agents := agent.NewPostgresService(pool)
	teams := platformteam.NewPostgresService(agents, pool)
	runtime := engine.New(agents, gateway)
	toolRegistry := platformtool.NewRegistry()
	knowledgeBases := kb.NewService()
	knowledgeSearch := retriever.NewKnowledgeSearch(knowledgeBases)
	prompts := prompt.NewService()
	profiles := modelconfig.NewRegistry([]byte(cfg.JWTSecret))
	skills := skill.NewService()
	approvals := approval.NewPostgresService(pool)
	guards := guard.NewService(guard.NewPipeline(guard.MaxLengthRule{MaxRunes: 8000}, guard.InjectionRule{}, guard.PIIRule{}))
	auditLedger := audit.NewPostgresLedger(pool)
	evaluator, evalData := platformeval.NewService(), platformeval.NewPostgresCatalog(pool)
	sandboxClient, err := sandbox.NewClient(cfg.SandboxRunnerURL, cfg.SandboxRunnerToken)
	if err != nil {
		log.Fatalf("create sandbox runner client: %v", err)
	}
	toolExecutor := tooling.NewExecutor(toolRegistry, nil, cfg.ToolAllowedHosts...).WithSandbox(sandboxClient)
	toolExecutor.RegisterSDK("search_knowledge_base", func(ctx context.Context, workspaceID string, arguments map[string]any) (tooling.Result, error) {
		kbID, _ := arguments["kb_id"].(string)
		query, _ := arguments["query"].(string)
		mode, _ := arguments["mode"].(string)
		topK := 5
		if number, ok := arguments["top_k"].(json.Number); ok {
			if value, parseErr := number.Int64(); parseErr == nil {
				topK = int(value)
			}
		}
		results, searchErr := knowledgeSearch.Search(ctx, workspaceID, kbID, query, mode, topK)
		if searchErr != nil {
			return tooling.Result{}, searchErr
		}
		body, marshalErr := json.Marshal(results)
		return tooling.Result{StatusCode: http.StatusOK, Body: body}, marshalErr
	})
	runtime.WithTools(toolExecutor).WithRuntimeConfig(prompts, profiles).WithSkills(skills).WithApprovals(approvals).WithGuard(guards).WithAudit(auditLedger)
	approvalWorker := engine.NewApprovalWorker(approvals, runtime, "course-server-worker")
	replayGuard := replay.NewPostgres(pool, replay.DefaultWindow)
	channelConsumer := integration.NewRuntimeConsumer(agents, runtime, cfg.ChannelWorkspaceID, cfg.ChannelAgentID, cfg.ChannelAgentEnv, func(source, eventID, answer string) {
		log.Printf("channel answer source=%s event_id=%s answer=%q", source, eventID, answer)
	})

	server := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: api.NewRouterWithControlPlane(iamService, runtime, api.ControlPlane{
			Agents: agents, Approvals: approvals, Audit: auditLedger, Tools: toolRegistry, ToolExecutor: toolExecutor, KBs: knowledgeBases, Search: knowledgeSearch,
			Prompts: prompts, Profiles: profiles, Skills: skills, Guard: guards,
			Evaluator: evaluator, EvalData: evalData, Teams: teams,
			Webhook:        webhook.NewHandlerWithReplay(cfg.WebhookSecret, replayGuard, channelConsumer.Callback("webhook")),
			Lark:           lark.NewHandlerWithReplay(cfg.LarkEncryptKey, replayGuard, channelConsumer.Callback("lark")),
			ApprovalWorker: approvalWorker,
			Readiness:      sandboxClient.Check,
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()
	go func() {
		if err := approvalWorker.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("approval worker stopped: %v", err)
		}
	}()

	go func() {
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cancel()

		_ = server.Shutdown(shutdownCtx)
	}()

	log.Printf("server listening on %s", cfg.HTTPAddr)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
