package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Q1mi/kbot-crossborder/internal/agentmock"
	"github.com/Q1mi/kbot-crossborder/internal/service"
)

func New(svc *service.Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", agentmock.Handler)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "project": "crossborder"})
	})
	mux.HandleFunc("POST /tools/get_order", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			OrderID string `json:"order_id"`
		}
		if !decode(w, r, &in) {
			return
		}
		out, err := svc.GetOrder(in.OrderID)
		respond(w, out, err)
	})
	mux.HandleFunc("POST /tools/get_inventory", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			SKU string `json:"sku"`
		}
		if !decode(w, r, &in) {
			return
		}
		respond(w, map[string]any{"sku": in.SKU, "balances": svc.Inventory(in.SKU)}, nil)
	})
	mux.HandleFunc("POST /tools/get_shipping_options", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			OrderID string `json:"order_id"`
		}
		if !decode(w, r, &in) {
			return
		}
		out, err := svc.ShippingOptions(in.OrderID)
		respond(w, out, err)
	})
	mux.HandleFunc("POST /tools/get_statement", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			StatementID string `json:"statement_id"`
		}
		if !decode(w, r, &in) {
			return
		}
		out, err := svc.GetStatement(in.StatementID)
		respond(w, out, err)
	})
	mux.HandleFunc("POST /tools/create_inventory_transfer", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			SKU      string `json:"sku"`
			From     string `json:"from_warehouse"`
			To       string `json:"to_warehouse"`
			Quantity int    `json:"quantity"`
			Key      string `json:"idempotency_key"`
			DryRun   bool   `json:"dry_run"`
		}
		if !decode(w, r, &in) {
			return
		}
		out, err := svc.CreateTransfer(service.TransferRequest{SKU: in.SKU, FromWarehouse: in.From, ToWarehouse: in.To, Quantity: in.Quantity, IdempotencyKey: in.Key, DryRun: in.DryRun})
		respond(w, out, err)
	})
	mux.HandleFunc("POST /tools/approve_refund", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			OrderID string  `json:"order_id"`
			Amount  float64 `json:"amount"`
			Reason  string  `json:"reason"`
			Key     string  `json:"idempotency_key"`
			DryRun  bool    `json:"dry_run"`
		}
		if !decode(w, r, &in) {
			return
		}
		out, err := svc.ApproveRefund(service.RefundRequest{OrderID: in.OrderID, Amount: in.Amount, Reason: in.Reason, IdempotencyKey: in.Key, DryRun: in.DryRun})
		respond(w, out, err)
	})
	mux.HandleFunc("POST /tools/create_reconciliation_case", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			StatementID string `json:"statement_id"`
			Reason      string `json:"reason"`
			Key         string `json:"idempotency_key"`
			DryRun      bool   `json:"dry_run"`
		}
		if !decode(w, r, &in) {
			return
		}
		out, err := svc.CreateReconciliation(service.ReconciliationRequest{StatementID: in.StatementID, Reason: in.Reason, IdempotencyKey: in.Key, DryRun: in.DryRun})
		respond(w, out, err)
	})
	return mux
}

func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	if err := json.NewDecoder(r.Body).Decode(value); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return false
	}
	return true
}

func respond(w http.ResponseWriter, value any, err error) {
	if err == nil {
		writeJSON(w, http.StatusOK, value)
		return
	}
	status := http.StatusUnprocessableEntity
	if errors.Is(err, service.ErrNotFound) {
		status = http.StatusNotFound
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
