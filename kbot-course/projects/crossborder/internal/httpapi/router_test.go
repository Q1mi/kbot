package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Q1mi/kbot-crossborder/internal/domain"
	"github.com/Q1mi/kbot-crossborder/internal/service"
)

func TestCreateTransferEndpoint(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(New(service.NewSeeded()))
	t.Cleanup(server.Close)

	body := map[string]any{
		"sku": "SKU-BLACK-M-01", "from_warehouse": "WH-US-LAX",
		"to_warehouse": "WH-CN-SZ", "quantity": 2,
		"idempotency_key": "lesson-03",
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Post(server.URL+"/api/transfers", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusCreated)
	}
	var transfer domain.InventoryTransfer
	if err := json.NewDecoder(response.Body).Decode(&transfer); err != nil {
		t.Fatal(err)
	}
	if transfer.ID == "" || transfer.Status != "created" {
		t.Fatalf("unexpected transfer: %+v", transfer)
	}
}

func TestCreateTransferRejectsUnknownField(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodPost, "/api/transfers", bytes.NewBufferString(`{"unknown":true}`))
	response := httptest.NewRecorder()
	New(service.NewSeeded()).ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}
