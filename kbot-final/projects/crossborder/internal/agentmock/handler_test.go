package agentmock

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOrderWorkflowRequestsInventoryTransfer(t *testing.T) {
	first := request(t, `{"model":"demo","messages":[{"role":"user","content":"诊断并执行调拨订单 TTS-20260801-1001"}],"tools":[{"function":{"name":"get_order"}}]}`)
	if !strings.Contains(first, `"name":"get_order"`) {
		t.Fatalf("unexpected first response: %s", first)
	}
	last := request(t, `{"model":"demo","messages":[{"role":"user","content":"诊断并执行调拨订单 TTS-20260801-1001"},{"role":"assistant","tool_calls":[{"function":{"name":"get_shipping_options"}}]},{"role":"tool","content":"[]"}],"tools":[{"function":{"name":"create_inventory_transfer"}}]}`)
	if !strings.Contains(last, `"name":"create_inventory_transfer"`) {
		t.Fatalf("unexpected transfer response: %s", last)
	}
}

func TestAnalyzeOnlyTemplateDoesNotRequestWriteTool(t *testing.T) {
	response := request(t, `{"model":"demo","messages":[{"role":"user","content":"# 跨境商品运营诊断任务\n\n- 订单号：TTS-20260801-1001\n- 执行模式：analyze_only\n\n涉及调拨或退款时，严格遵循执行模式。"},{"role":"assistant","tool_calls":[{"function":{"name":"get_shipping_options"}}]},{"role":"tool","content":"[]"}],"tools":[{"function":{"name":"create_inventory_transfer"}}]}`)
	if strings.Contains(response, `"tool_calls"`) {
		t.Fatalf("analyze-only response requested a write tool: %s", response)
	}
	if !strings.Contains(response, "建议创建 2 件跨仓调拨") {
		t.Fatalf("unexpected analyze-only response: %s", response)
	}
}

func request(t *testing.T, body string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	Handler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}
