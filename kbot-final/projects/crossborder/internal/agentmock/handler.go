package agentmock

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Tools    []chatTool    `json:"tools"`
}

type chatMessage struct {
	Role      string     `json:"role"`
	Content   any        `json:"content"`
	ToolCalls []toolCall `json:"tool_calls"`
}

type toolCall struct {
	Function struct {
		Name string `json:"name"`
	} `json:"function"`
}

type chatTool struct {
	Function struct {
		Name string `json:"name"`
	} `json:"function"`
}

var (
	orderPattern     = regexp.MustCompile(`(?i)TTS-[A-Z0-9-]+`)
	statementPattern = regexp.MustCompile(`(?i)STMT-[A-Z0-9-]+`)
)

func Handler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	message, finish := decide(req)
	writeJSON(w, http.StatusOK, map[string]any{
		"id": fmt.Sprintf("chatcmpl-crossborder-%d", time.Now().UnixNano()), "object": "chat.completion",
		"created": time.Now().Unix(), "model": modelName(req.Model),
		"choices": []map[string]any{{"index": 0, "message": message, "finish_reason": finish}},
		"usage":   map[string]any{"prompt_tokens": 96, "completion_tokens": 32, "total_tokens": 128},
	})
}

func decide(req chatRequest) (map[string]any, string) {
	user := latestUser(req.Messages)
	lastRole := ""
	if len(req.Messages) > 0 {
		lastRole = req.Messages[len(req.Messages)-1].Role
	}
	lastTool := latestAssistantTool(req.Messages)
	if lastRole != "tool" {
		switch {
		case containsAny(user, "账单", "结算", "对账", "statement"):
			return call(req.Tools, "get_statement", map[string]any{"statement_id": matchOr(statementPattern, user, "STMT-2026-31")})
		case containsAny(user, "订单", "履约", "库存", "退款", "TTS-"):
			return call(req.Tools, "get_order", map[string]any{"order_id": matchOr(orderPattern, user, "TTS-20260801-1001")})
		default:
			return answer("我是跨境电商运营与供应链协同 Agent，可处理订单履约、库存调拨、物流方案、退款和结算对账。")
		}
	}

	orderID := matchOr(orderPattern, user, "TTS-20260801-1001")
	writeAllowed := !isAnalyzeOnly(user)
	switch lastTool {
	case "get_order":
		if writeAllowed && containsAny(user, "退款", "取消") && containsAny(user, "执行", "批准", "提交") {
			return call(req.Tools, "approve_refund", map[string]any{"order_id": orderID, "amount": 129.99, "reason": "买家取消未发货订单", "idempotency_key": "refund-" + strings.ToLower(orderID)})
		}
		return call(req.Tools, "get_inventory", map[string]any{"sku": "SKU-BLACK-M-01"})
	case "get_inventory":
		return call(req.Tools, "get_shipping_options", map[string]any{"order_id": orderID})
	case "get_shipping_options":
		if writeAllowed && containsAny(user, "执行", "调拨", "创建") {
			return call(req.Tools, "create_inventory_transfer", map[string]any{"sku": "SKU-BLACK-M-01", "from_warehouse": "WH-US-LAX", "to_warehouse": "WH-CN-SZ", "quantity": 2, "idempotency_key": "transfer-" + strings.ToLower(orderID)})
		}
		return answer("订单履约仓库存不足，洛杉矶仓库存可用且存在满足 SLA 的物流渠道。建议创建 2 件跨仓调拨，并由运营人员审批。")
	case "create_inventory_transfer":
		return answer("库存调拨已经创建，后续可跟踪调拨状态和订单最晚发货时间。")
	case "approve_refund":
		return answer("订单退款已经批准并进入处理流程。")
	case "get_statement":
		if writeAllowed && containsAny(user, "提交", "创建", "申诉") {
			return call(req.Tools, "create_reconciliation_case", map[string]any{"statement_id": matchOr(statementPattern, user, "STMT-2026-31"), "reason": "平台结算少付 11.52 USD", "idempotency_key": "reconcile-stmt-2026-31"})
		}
		return answer("账单应结 118.47 USD，实结 106.95 USD，差异为 11.52 USD，建议财务复核后提交申诉。")
	case "create_reconciliation_case":
		return answer("结算差异申诉单已经创建，差异金额为 11.52 USD。")
	default:
		return answer("业务工具执行完成。")
	}
}

func call(tools []chatTool, name string, arguments map[string]any) (map[string]any, string) {
	if !hasTool(tools, name) {
		return answer("当前 Agent 未挂载所需工具 " + name + "。")
	}
	raw, _ := json.Marshal(arguments)
	return map[string]any{"role": "assistant", "content": nil, "tool_calls": []map[string]any{{"id": "call_" + name, "type": "function", "function": map[string]any{"name": name, "arguments": string(raw)}}}}, "tool_calls"
}

func answer(content string) (map[string]any, string) {
	return map[string]any{"role": "assistant", "content": content}, "stop"
}
func hasTool(tools []chatTool, name string) bool {
	for _, tool := range tools {
		if tool.Function.Name == name {
			return true
		}
	}
	return false
}
func latestAssistantTool(messages []chatMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" && len(messages[i].ToolCalls) > 0 {
			return messages[i].ToolCalls[len(messages[i].ToolCalls)-1].Function.Name
		}
	}
	return ""
}
func latestUser(messages []chatMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			if value, ok := messages[i].Content.(string); ok {
				return value
			}
		}
	}
	return ""
}
func containsAny(value string, terms ...string) bool {
	lower := strings.ToLower(value)
	for _, term := range terms {
		if strings.Contains(lower, strings.ToLower(term)) {
			return true
		}
	}
	return false
}

func isAnalyzeOnly(value string) bool {
	return containsAny(value, "执行模式：analyze_only", "执行模式: analyze_only", `"execution_mode":"analyze_only"`)
}
func matchOr(pattern *regexp.Regexp, value, fallback string) string {
	if found := pattern.FindString(value); found != "" {
		return strings.ToUpper(found)
	}
	return fallback
}
func modelName(value string) string {
	if value == "" {
		return "crossborder-agent-mock"
	}
	return value
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
