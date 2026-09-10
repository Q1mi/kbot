// 第 22 课起点：在课堂中实现 Bot、Dialogue，并将物流 Runtime 接入飞书。
package main

import (
	"flag"
	"log"
)

func main() {
	flag.String("card-url", "http://127.0.0.1:8082/.well-known/agent-card.json", "第 21 课物流 Mock AgentCard 地址")
	flag.Bool("live-model", false, "接入完成后可启用真实模型")
	flag.Parse()
	log.Print("lesson22-start: 按 docs/lesson-22-feishu-kbot.md 实现 Dialogue、lark.Bot 与本命令装配；完整示例见 22-end")
}
