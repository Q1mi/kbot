// Command mockllm 提供课堂专用、确定性的 OpenAI 兼容模型与退款 Tool。
// 它让 Langfuse/A2UI 演示不依赖公网、真实 API Key 或本地大模型。
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Tools    []chatTool    `json:"tools"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type chatTool struct {
	Function struct {
		Name string `json:"name"`
	} `json:"function"`
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		client := &http.Client{Timeout: time.Second}
		resp, err := client.Get("http://127.0.0.1:8090/healthz")
		if err != nil || resp.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		_ = resp.Body.Close()
		return
	}
	addr := ":8090"
	log.Printf("kbot classroom mock LLM listening on %s", addr)
	if err := http.ListenAndServe(addr, newMux()); err != nil {
		log.Fatal(err)
	}
}

func newMux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/v1/chat/completions", chatCompletions)
	mux.HandleFunc("/tools/refund", refundTool)
	return mux
}

func chatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Model == "" {
		req.Model = "kbot-classroom-mock"
	}
	lastRole, prompt := lastMessage(req.Messages)
	choiceMessage := map[string]any{"role": "assistant", "content": mockAnswer(prompt)}
	finishReason := "stop"
	if lastRole != "tool" {
		if toolName := refundToolName(req.Tools, prompt); toolName != "" {
			arguments := refundArgumentsFromPrompt(prompt)
			choiceMessage = map[string]any{
				"role": "assistant", "content": nil,
				"tool_calls": []map[string]any{{
					"id": "call_refund_demo", "type": "function",
					"function": map[string]any{
						"name":      toolName,
						"arguments": arguments,
					},
				}},
			}
			finishReason = "tool_calls"
		}
	} else {
		choiceMessage["content"] = "退款申请已经提交，退款单号 RF-20260725-001，预计 1–3 个工作日原路到账。"
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":      fmt.Sprintf("chatcmpl-kbot-%d", time.Now().UnixNano()),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   req.Model,
		"choices": []map[string]any{{
			"index": 0, "message": choiceMessage, "finish_reason": finishReason,
		}},
		"usage": map[string]any{
			"prompt_tokens": 64, "completion_tokens": 32, "total_tokens": 96,
			"prompt_tokens_details": map[string]any{"cached_tokens": 16},
		},
	})
}

var (
	orderIDPattern = regexp.MustCompile(`(?i)\b(?:KBOT|ORD|ORDER)-[A-Z0-9-]+\b`)
	amountPattern  = regexp.MustCompile(`(?i)(?:退款(?:金额)?|金额|amount)[^0-9]{0,8}([0-9]+(?:\.[0-9]{1,2})?)\s*(?:元|rmb|cny)?`)
	reasonPattern  = regexp.MustCompile(`(?i)(?:原因(?:是|为|[:：])?|reason\s*[:：]?)\s*([^，。；;\n]+)`)
)

func refundArgumentsFromPrompt(prompt string) string {
	arguments := map[string]any{
		"order_id": "KBOT-2026-001",
		"amount":   299,
		"reason":   "课程演示",
	}
	if orderID := orderIDPattern.FindString(prompt); orderID != "" {
		arguments["order_id"] = strings.ToUpper(orderID)
	}
	if match := amountPattern.FindStringSubmatch(prompt); len(match) == 2 {
		if amount, err := strconv.ParseFloat(match[1], 64); err == nil {
			arguments["amount"] = amount
		}
	}
	if match := reasonPattern.FindStringSubmatch(prompt); len(match) == 2 {
		reason := strings.Trim(strings.TrimSpace(match[1]), `"'`)
		if reason != "" {
			arguments["reason"] = reason
		}
	}
	raw, _ := json.Marshal(arguments)
	return string(raw)
}

func refundTool(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var input map[string]any
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "submitted", "refund_id": "RF-20260725-001",
		"order_id": input["order_id"], "amount": input["amount"],
		"eta": "1-3 business days",
	})
}

func lastMessage(messages []chatMessage) (string, string) {
	if len(messages) == 0 {
		return "", ""
	}
	last := messages[len(messages)-1]
	switch content := last.Content.(type) {
	case string:
		return last.Role, content
	default:
		raw, _ := json.Marshal(content)
		return last.Role, string(raw)
	}
}

func refundToolName(tools []chatTool, prompt string) string {
	lower := strings.ToLower(prompt)
	if !strings.Contains(prompt, "退款") && !strings.Contains(prompt, "退掉") && !strings.Contains(lower, "refund") {
		return ""
	}
	for _, tool := range tools {
		name := tool.Function.Name
		if strings.Contains(strings.ToLower(name), "refund") || strings.Contains(name, "退款") {
			return name
		}
	}
	return ""
}

func mockAnswer(prompt string) string {
	switch {
	case strings.Contains(prompt, "注入"):
		return "该请求需要经过安全规则检查。"
	case strings.Contains(prompt, "退款"):
		return "我已检查退款政策。七天内的订单可以申请退款，请确认订单号和退款原因。"
	case strings.Contains(prompt, "能做什么"):
		return "我是企业服务助手，可以回答知识库问题、调用业务工具并处理需要人工审批的操作。"
	default:
		return "这是来自 kbot 课堂 Mock LLM 的确定性回复。"
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
