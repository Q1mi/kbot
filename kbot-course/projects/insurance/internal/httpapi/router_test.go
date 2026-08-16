package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Q1mi/kbot-course/insurance/internal/claim"
	"github.com/Q1mi/kbot-course/insurance/internal/simulator"
)

func TestInsuranceToolEndpointsAndIdempotency(t *testing.T) {
	handler := New(simulator.NewSeeded())
	policyRequest := httptest.NewRequest(http.MethodPost, "/tools/get-policy", bytes.NewBufferString(`{"policy_id":"POL-1001"}`))
	policyResponse := httptest.NewRecorder()
	handler.ServeHTTP(policyResponse, policyRequest)
	if policyResponse.Code != http.StatusOK {
		t.Fatalf("policy status = %d", policyResponse.Code)
	}
	decision := claim.Decision{Eligible: true, Action: "manual_review", RequiresApproval: true, RiskScore: 0.2, Evidence: []string{"policy:POL-1001"}}
	body, _ := json.Marshal(map[string]any{"claim_id": "C-1", "decision": decision})
	for range 2 {
		request := httptest.NewRequest(http.MethodPost, "/tools/submit-claim-decision", bytes.NewReader(body))
		request.Header.Set("Idempotency-Key", "approval-1")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("decision status = %d, body = %s", response.Code, response.Body.String())
		}
	}
}
