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
	postgresinfra "github.com/Q1mi/kbot/internal/infrastructure/postgres"
	"github.com/Q1mi/kbot/internal/platform/agent"
	"github.com/Q1mi/kbot/internal/platform/approval"
	"github.com/Q1mi/kbot/internal/platform/iam"
	"github.com/Q1mi/kbot/internal/platform/kb"
	"github.com/Q1mi/kbot/internal/platform/modelconfig"
	"github.com/Q1mi/kbot/internal/platform/prompt"
	"github.com/Q1mi/kbot/internal/platform/skill"
	platformtool "github.com/Q1mi/kbot/internal/platform/tool"
	"github.com/Q1mi/kbot/internal/runtime/engine"
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
	databaseContext, databaseCancel := context.WithTimeout(context.Background(), 10*time.Second)
	pool, err := postgresinfra.Open(databaseContext, cfg.DatabaseURL)
	databaseCancel()
	if err != nil {
		log.Fatalf("connect PostgreSQL: %v", err)
	}
	defer pool.Close()
	iamService := iam.New(iam.NewPostgresStore(pool), cfg.JWTSecret, cfg.JWTIssuer)
	gateway, err := llm.NewGateway(cfg)
	if err != nil {
		log.Fatalf("create LLM gateway: %v", err)
	}
	agents := agent.NewPostgresService(pool)
	runtime := engine.New(agents, gateway)
	toolRegistry := platformtool.NewRegistry()
	knowledgeBases := kb.NewService()
	knowledgeSearch := retriever.NewKnowledgeSearch(knowledgeBases)
	prompts := prompt.NewService()
	profiles := modelconfig.NewRegistry([]byte(cfg.JWTSecret))
	skills := skill.NewService()
	approvals := approval.NewPostgresService(pool)
	sandboxClient, err := sandbox.NewClient(cfg.SandboxRunnerURL, cfg.SandboxRunnerToken)
	if err != nil {
		log.Fatalf("create sandbox runner client: %v", err)
	}
	toolExecutor := tooling.NewExecutor(toolRegistry, nil, "crossborder-sim", "localhost", "127.0.0.1").WithSandbox(sandboxClient)
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
	runtime.WithTools(toolExecutor).WithRuntimeConfig(prompts, profiles).WithSkills(skills).WithApprovals(approvals)
	approvalWorker := engine.NewApprovalWorker(approvals, runtime, "course-server-worker")

	server := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: api.NewRouterWithControlPlane(iamService, runtime, api.ControlPlane{
			Agents: agents, Approvals: approvals, Tools: toolRegistry, KBs: knowledgeBases, Search: knowledgeSearch,
			Prompts: prompts, Profiles: profiles, Skills: skills, ApprovalWorker: approvalWorker,
		}),
		ReadHeaderTimeout: 5 * time.Second,
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
