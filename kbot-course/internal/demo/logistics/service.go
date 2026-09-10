// Package logistics 提供第 21 课独立运行的 A2A 物流服务。
package logistics

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

const Version = "logistics-v1"

type Shipment struct {
	OrderID        string `json:"order_id"`
	TrackingNumber string `json:"tracking_number"`
	Status         string `json:"status"`
	Location       string `json:"location"`
	Detail         string `json:"detail"`
}

var shipments = map[string]Shipment{
	"CB-1001": {"CB-1001", "KB20260910001", "清关中", "洛杉矶海关", "申报资料正在审核，请等待承运商确认放行。"},
	"CB-1002": {"CB-1002", "KB20260910002", "运输中", "上海转运中心", "包裹已离开转运中心，预计 2 天内到达目的地。"},
	"CB-1003": {"CB-1003", "KB20260910003", "已签收", "深圳南山", "包裹已由收件人签收。"},
}

var orderPattern = regexp.MustCompile(`(?i)CB-[0-9]+`)

// NewHandler 只实现本课需要的 AgentCard 和 SendMessage 文本响应。
// publicURL 由启动参数固定，避免从请求 Host 构造协议地址。
func NewHandler(publicURL string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /.well-known/agent-card.json", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"name": "Kbot 物流 Mock Agent", "description": "查询 CB-1001、CB-1002、CB-1003 的演示物流信息", "version": Version,
			"supportedInterfaces": []map[string]string{{"url": strings.TrimRight(publicURL, "/") + "/a2a", "protocolBinding": "JSONRPC", "protocolVersion": "1.0"}},
			"capabilities":        map[string]bool{"streaming": false, "pushNotifications": false},
			"defaultInputModes":   []string{"text/plain"}, "defaultOutputModes": []string{"text/plain"},
			"skills": []map[string]any{{"id": "shipment_lookup", "name": "物流查询", "description": "按订单号查询物流状态", "tags": []string{"logistics"}}},
		})
	})
	mux.HandleFunc("POST /a2a", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Method  string          `json:"method"`
			Params  struct {
				Message struct {
					Role      string `json:"role"`
					MessageID string `json:"messageId"`
					Parts     []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"message"`
				Metadata map[string]string `json:"metadata"`
			} `json:"params"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&request); err != nil {
			rpcError(w, nil, -32700, "invalid JSON")
			return
		}
		if request.JSONRPC != "2.0" || len(request.ID) == 0 {
			rpcError(w, request.ID, -32600, "JSON-RPC 2.0 request id is required")
			return
		}
		if request.Method != "SendMessage" {
			rpcError(w, request.ID, -32601, "this demo supports SendMessage")
			return
		}
		if r.Header.Get("A2A-Version") != "1.0" || request.Params.Message.Role != "ROLE_USER" || request.Params.Message.MessageID == "" {
			rpcError(w, request.ID, -32602, "A2A-Version 1.0, ROLE_USER and messageId are required")
			return
		}
		if request.Params.Metadata["agent_version_id"] != Version {
			rpcError(w, request.ID, -32602, "agent_version_id must be "+Version)
			return
		}
		var parts []string
		for _, part := range request.Params.Message.Parts {
			parts = append(parts, part.Text)
		}
		orderID := strings.ToUpper(orderPattern.FindString(strings.Join(parts, " ")))
		text := "请提供演示订单号：CB-1001、CB-1002 或 CB-1003。"
		if shipment, ok := shipments[orderID]; ok {
			text = fmt.Sprintf("【演示物流数据】订单 %s，运单 %s：%s。当前位置：%s。%s", shipment.OrderID, shipment.TrackingNumber, shipment.Status, shipment.Location, shipment.Detail)
		} else if orderID != "" {
			text = "【演示物流数据】未找到订单 " + orderID + "，可查询 CB-1001、CB-1002、CB-1003。"
		}
		writeJSON(w, map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{
			"message": map[string]any{"role": "ROLE_AGENT", "messageId": "reply-" + request.Params.Message.MessageID,
				"contextId": "logistics-" + request.Params.Message.MessageID, "parts": []map[string]string{{"text": text}},
				"metadata": map[string]string{"executed_agent_version_id": Version}},
		}})
	})
	return mux
}

func rpcError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	writeJSON(w, map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
