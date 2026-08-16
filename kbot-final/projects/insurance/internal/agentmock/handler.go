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
	claimPattern        = regexp.MustCompile(`(?i)CLM-[A-Z0-9-]+`)
	underwritingPattern = regexp.MustCompile(`(?i)UW-[A-Z0-9-]+`)
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
		"id": fmt.Sprintf("chatcmpl-insurance-%d", time.Now().UnixNano()), "object": "chat.completion",
		"created": time.Now().Unix(), "model": modelName(req.Model),
		"choices": []map[string]any{{"index": 0, "message": message, "finish_reason": finish}},
		"usage":   map[string]any{"prompt_tokens": 112, "completion_tokens": 36, "total_tokens": 148},
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
		case containsAny(user, "核保", "承保", "UW-"):
			return call(req.Tools, "get_underwriting_case", map[string]any{"case_id": matchOr(underwritingPattern, user, "UW-2026-0001")})
		case containsAny(user, "理赔", "赔付", "欺诈", "CLM-"):
			return call(req.Tools, "get_claim", map[string]any{"claim_id": matchOr(claimPattern, user, "CLM-2026-0001")})
		default:
			return answer("我是保险承保、理赔与反欺诈 Agent，可处理核保评估、责任审核、可赔金额计算和欺诈风险处置。")
		}
	}

	if containsAny(user, "核保", "承保", "UW-") {
		return decideUnderwriting(req, user, lastTool)
	}
	return decideClaim(req, user, lastTool)
}

func decideUnderwriting(req chatRequest, user, lastTool string) (map[string]any, string) {
	caseID := matchOr(underwritingPattern, user, "UW-2026-0001")
	writeAllowed := !isAnalyzeOnly(user)
	switch lastTool {
	case "get_underwriting_case":
		return call(req.Tools, "assess_underwriting", map[string]any{"case_id": caseID})
	case "assess_underwriting":
		if writeAllowed && containsAny(user, "提交", "批准", "执行") {
			return call(req.Tools, "approve_underwriting", map[string]any{
				"case_id": caseID, "decision": "approve", "premium": 4160,
				"reason_codes":    []string{"HIGH_PRIOR_CLAIM_FREQUENCY", "OLDER_VEHICLE"},
				"idempotency_key": "underwriting-" + strings.ToLower(caseID),
			})
		}
		return answer("该案件为高风险核保，建议转人工核保，建议保费为 4160 CNY。")
	case "approve_underwriting":
		return answer("人工核保方案已经批准，最终保费为 4160 CNY。")
	default:
		return answer("核保工具执行完成。")
	}
}

func decideClaim(req chatRequest, user, lastTool string) (map[string]any, string) {
	claimID := matchOr(claimPattern, user, "CLM-2026-0001")
	highRisk := strings.EqualFold(claimID, "CLM-2026-0002") || containsAny(user, "高风险", "欺诈", "冻结", "立案")
	writeAllowed := !isAnalyzeOnly(user)
	switch lastTool {
	case "get_claim":
		return call(req.Tools, "get_policy", map[string]any{"policy_id": "POL-CAR-2026-0001"})
	case "get_policy":
		return call(req.Tools, "evaluate_coverage", map[string]any{"claim_id": claimID})
	case "evaluate_coverage":
		return call(req.Tools, "get_fraud_features", map[string]any{"claim_id": claimID})
	case "get_fraud_features":
		if writeAllowed && highRisk && containsAny(user, "处置", "执行", "冻结", "立案") {
			return call(req.Tools, "hold_claim_payment", map[string]any{
				"claim_id": claimID, "reason": "欺诈风险分 0.92，命中事故时间与重复影像信号",
				"idempotency_key": "hold-" + strings.ToLower(claimID),
			})
		}
		if writeAllowed && !highRisk && containsAny(user, "批准", "执行", "赔付") {
			return call(req.Tools, "approve_claim", map[string]any{
				"claim_id": claimID, "approved_amount": 6300,
				"reason_codes":    []string{"COVERED_COLLISION", "DEDUCTIBLE_APPLIED"},
				"idempotency_key": "approve-" + strings.ToLower(claimID),
			})
		}
		if highRisk {
			return answer("案件不在保险责任期间，欺诈风险分为 0.92，命中事故时间异常与重复影像信号，建议冻结赔款并转人工调查。")
		}
		return answer("案件符合车辆损失险责任，扣除 500 CNY 免赔额后，最高可赔金额为 6300 CNY，欺诈风险较低。")
	case "hold_claim_payment":
		if writeAllowed && containsAny(user, "处置", "立案") {
			return call(req.Tools, "open_fraud_investigation", map[string]any{
				"claim_id": claimID, "reason": "赔款已冻结，命中事故时间与重复影像信号",
				"idempotency_key": "investigation-" + strings.ToLower(claimID),
			})
		}
		return answer("高风险理赔赔款已经冻结，案件已进入人工反欺诈复核队列。")
	case "open_fraud_investigation":
		return answer("欺诈调查案件已经创建，调查人员可查看规则信号与证据。")
	case "approve_claim":
		return answer("理赔决定已经批准，赔付金额为 6300 CNY。")
	case "request_additional_documents":
		return answer("补充材料请求已经发送给理赔申请人。")
	default:
		return answer("理赔工具执行完成。")
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
		return "insurance-agent-mock"
	}
	return value
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
