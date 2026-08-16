package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Q1mi/kbot-insurance/internal/service"
)

func TestFraudFeatureTool(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/tools/get_fraud_features", strings.NewReader(`{"claim_id":"CLM-2026-0002"}`))
	rec := httptest.NewRecorder()
	New(service.NewSeeded()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "DUPLICATE_DAMAGE_IMAGE") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
