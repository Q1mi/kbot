package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/cloudwego/eino/components/model"

	"github.com/Q1mi/kbot/internal/config"
	"github.com/Q1mi/kbot/internal/demo/lesson21"
	"github.com/Q1mi/kbot/internal/integration"
	"github.com/Q1mi/kbot/internal/integration/lark"
	"github.com/Q1mi/kbot/internal/runtime/llm"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
	cardURL := flag.String("card-url", "http://127.0.0.1:8082/.well-known/agent-card.json", "第 21 课物流 Mock AgentCard 地址（限本机）")
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
	// 第 22 课复用第 21 课已发布的物流助手，并增加飞书消息入口。
	runtime, err := lesson21.NewRuntime(ctx, *cardURL, chatModel)
	if err != nil {
		return err
	}
	dialogue, err := integration.NewDialogue(runtime.Agents, runtime.Engine, lesson21.WorkspaceID, runtime.AgentID, "dev")
	if err != nil {
		return err
	}
	bot, err := lark.NewBot(lark.BotConfig{AppID: cfg.LarkAppID, AppSecret: cfg.LarkAppSecret, TenantKey: cfg.LarkTenantKey}, dialogue, nil)
	if err != nil {
		return err
	}
	log.Printf("lesson=22 workspace=%s agent=%s agent_version=%s", lesson21.WorkspaceID, runtime.AgentID, lesson21.AgentVersionID)
	log.Print("connecting to Feishu; after ready, send 查询订单 CB-1001 的物流信息; /new starts a new conversation")
	err = bot.Run(ctx)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}
