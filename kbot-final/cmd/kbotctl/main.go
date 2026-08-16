// Command kbotctl 是 kbot 的运维 CLI（最小集，设计文档 §4.X-2 / 讲义 §15.10）。
//
// 内部直接用 Go SDK（pkg/sdk/go），给 SDK 一份强约束。不追求覆盖所有 API——常用就够。
//
// 用法：
//
//	kbotctl -url http://localhost:8080 -email admin@example.com -password admin12345 \
//	        agent-chat -agent <id> -input "你好"
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	kbot "github.com/Q1mi/kbot/pkg/sdk/go"
)

func main() {
	var (
		baseURL  = flag.String("url", "http://localhost:8080", "kbot base URL")
		email    = flag.String("email", "", "登录邮箱")
		password = flag.String("password", "", "登录密码")
		token    = flag.String("token", "", "已有 token（与 email/password 二选一）")
		agentID  = flag.String("agent", "", "Agent ID")
		input    = flag.String("input", "", "对话输入")
	)
	flag.Parse()

	cmd := flag.Arg(0)
	if cmd == "" {
		usage()
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	client := kbot.NewClient(*baseURL, *token)
	if *token == "" {
		if *email == "" || *password == "" {
			fatal("需要 -token 或 -email/-password")
		}
		if err := client.Login(ctx, *email, *password); err != nil {
			fatal("登录失败: %v", err)
		}
	}

	switch cmd {
	case "agent-chat":
		if *agentID == "" || *input == "" {
			fatal("agent-chat 需要 -agent 和 -input")
		}
		reply, err := client.Chat(ctx, *agentID, *input)
		if err != nil {
			fatal("对话失败: %v", err)
		}
		fmt.Println(reply)

	case "agent-stream":
		if *agentID == "" || *input == "" {
			fatal("agent-stream 需要 -agent 和 -input")
		}
		ch, err := client.Stream(ctx, *agentID, *input)
		if err != nil {
			fatal("流式失败: %v", err)
		}
		for ev := range ch {
			if ev.Type == "answer_delta" {
				fmt.Print(ev.Text)
			}
		}
		fmt.Println()

	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `kbotctl —— kbot 运维 CLI（最小集）

命令:
  agent-chat    -agent <id> -input <text>    同步对话
  agent-stream  -agent <id> -input <text>    流式对话

全局参数:
  -url, -email, -password 或 -token`)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
