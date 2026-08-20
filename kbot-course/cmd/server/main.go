package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/Q1mi/kbot/internal/api"
	"github.com/Q1mi/kbot/internal/config"
	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/platform"
	"github.com/Q1mi/kbot/internal/platform/iam"
	platformtool "github.com/Q1mi/kbot/internal/platform/tool"
	"github.com/Q1mi/kbot/internal/runtime/engine"
	"github.com/Q1mi/kbot/internal/runtime/llm"
)

func main() {
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		log.Fatal(err)
	}
	iamService := iam.New(iam.NewMemoryStore(), cfg.JWTSecret, cfg.JWTIssuer)
	gateway, err := llm.NewGateway(cfg)
	if err != nil {
		log.Fatalf("create LLM gateway: %v", err)
	}
	controlPlane := platform.New()
	controlPlane.PutSnapshot(&engine.AgentSnapshot{ID: "demo-v1", AgentID: "demo", SystemPrompt: "你是 kbot 课堂助手。", MaxSteps: 4})
	controlPlane.PutConversation(&domain.Conversation{ID: "demo-conversation", AgentID: "demo", AgentVersionID: "demo-v1"})
	runtime := engine.New(controlPlane, gateway)
	toolRegistry := platformtool.NewRegistry()

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.NewRouterWithControlPlane(iamService, runtime, api.ControlPlane{Tools: toolRegistry}),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()

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
