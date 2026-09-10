package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cloudwego/eino/components/model"

	"github.com/Q1mi/kbot/internal/config"
	"github.com/Q1mi/kbot/internal/demo/lesson21"
	"github.com/Q1mi/kbot/internal/runtime/engine"
	"github.com/Q1mi/kbot/internal/runtime/llm"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
	cardURL := flag.String("card-url", "http://127.0.0.1:8082/.well-known/agent-card.json", "物流 Mock AgentCard 地址（限本机）")
	message := flag.String("message", "查询订单 CB-1001 的物流信息", "A2A 演示问题")
	liveModel := flag.Bool("live-model", false, "使用真实大模型，需要 ARK_API_KEY 或 KBOT_LLM_API_KEY")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	cfg := config.Load()
	var chatModel model.BaseChatModel
	if *liveModel {
		if cfg.LLMAPIKey == "" {
			return fmt.Errorf("-live-model requires ARK_API_KEY or KBOT_LLM_API_KEY")
		}
		gateway, err := llm.NewGateway(cfg)
		if err != nil {
			return err
		}
		chatModel = gateway
		log.Printf("model mode=live model=%s", cfg.LLMModel)
	} else {
		log.Print("model mode=demo (deterministic tool call; no LLM API key required)")
	}
	runtime, err := lesson21.NewRuntime(ctx, *cardURL, chatModel)
	if err != nil {
		return err
	}
	log.Printf("workspace=%s agent=%s agent_version=%s tool_version=%s remote_version=logistics-v1", lesson21.WorkspaceID, runtime.AgentID, lesson21.AgentVersionID, lesson21.ToolVersionID)
	conversation, err := runtime.Agents.CreateConversation(ctx, lesson21.WorkspaceID, runtime.AgentID, "dev", "lesson21-cli")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	fmt.Printf("问题：%s\n", *message)
	return runtime.Engine.ChatStream(ctx, engine.ChatRequest{ConversationID: conversation.ID,
		WorkspaceID: lesson21.WorkspaceID, UserID: "lesson21-cli", Message: *message}, func(event engine.Event) error {
		switch event.Type {
		case "answer_done":
			fmt.Printf("回答：%s\n", event.Text)
		case "run_started", "tool_started", "tool_finished", "run_finished":
			data, _ := json.Marshal(event.Data)
			fmt.Printf("[%s] %s\n", event.Type, data)
		}
		return nil
	})
}
