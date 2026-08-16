package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Q1mi/kbot-crossborder/internal/service"
)

func TestGetOrderTool(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/tools/get_order", strings.NewReader(`{"order_id":"TTS-20260801-1001"}`))
	rec := httptest.NewRecorder()
	New(service.NewSeeded()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "awaiting_shipment") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
