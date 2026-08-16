package agentmock

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCleanClaimWorkflowRequestsFraudFeatures(t *testing.T) {
	first := request(t, `{"model":"demo","messages":[{"role":"user","content":"审核理赔案件 CLM-2026-0001 并给出可赔金额"}],"tools":[{"function":{"name":"get_claim"}}]}`)
	if !strings.Contains(first, `"name":"get_claim"`) {
		t.Fatalf("unexpected first response: %s", first)
	}
	last := request(t, `{"model":"demo","messages":[{"role":"user","content":"审核理赔案件 CLM-2026-0001 并给出可赔金额"},{"role":"assistant","tool_calls":[{"function":{"name":"evaluate_coverage"}}]},{"role":"tool","content":"{}"}],"tools":[{"function":{"name":"get_fraud_features"}}]}`)
	if !strings.Contains(last, `"name":"get_fraud_features"`) {
		t.Fatalf("unexpected fraud response: %s", last)
	}
}

func TestHighRiskClaimWorkflowRequestsHold(t *testing.T) {
	response := request(t, `{"model":"demo","messages":[{"role":"user","content":"审核高风险案件 CLM-2026-0002 并冻结赔款"},{"role":"assistant","tool_calls":[{"function":{"name":"get_fraud_features"}}]},{"role":"tool","content":"{}"}],"tools":[{"function":{"name":"hold_claim_payment"}}]}`)
	if !strings.Contains(response, `"name":"hold_claim_payment"`) {
		t.Fatalf("unexpected hold response: %s", response)
	}
}

func TestHighRiskDispositionRequestsInvestigationAfterHold(t *testing.T) {
	response := request(t, `{"model":"demo","messages":[{"role":"user","content":"审核高风险案件 CLM-2026-0002 并处置"},{"role":"assistant","tool_calls":[{"function":{"name":"hold_claim_payment"}}]},{"role":"tool","content":"{}"}],"tools":[{"function":{"name":"open_fraud_investigation"}}]}`)
	if !strings.Contains(response, `"name":"open_fraud_investigation"`) {
		t.Fatalf("unexpected investigation response: %s", response)
	}
}

func TestAnalyzeOnlyTemplateDoesNotRequestPaymentWrite(t *testing.T) {
	response := request(t, `{"model":"demo","messages":[{"role":"user","content":"# 保险理赔审核任务\n\n- 理赔案件号：CLM-2026-0001\n- 执行模式：analyze_only\n\n涉及补件或赔付时，列出拟执行参数。"},{"role":"assistant","tool_calls":[{"function":{"name":"get_fraud_features"}}]},{"role":"tool","content":"{}"}],"tools":[{"function":{"name":"approve_claim"}},{"function":{"name":"hold_claim_payment"}}]}`)
	if strings.Contains(response, `"tool_calls"`) {
		t.Fatalf("analyze-only response requested a write tool: %s", response)
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
