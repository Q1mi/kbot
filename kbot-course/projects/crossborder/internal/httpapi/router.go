package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/Q1mi/kbot-crossborder/internal/domain"
	"github.com/Q1mi/kbot-crossborder/internal/service"
)

type Service interface {
	GetOrder(id string) (domain.Order, bool)
	Inventory(sku string) []domain.InventoryBalance
	CreateTransfer(req service.TransferRequest) (domain.InventoryTransfer, error)
}

func New(svc Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /api/orders/{orderID}", func(w http.ResponseWriter, r *http.Request) {
		order, ok := svc.GetOrder(strings.TrimSpace(r.PathValue("orderID")))
		if !ok {
			writeError(w, http.StatusNotFound, "order_not_found")
			return
		}
		writeJSON(w, http.StatusOK, order)
	})
	mux.HandleFunc("GET /api/inventory", func(w http.ResponseWriter, r *http.Request) {
		sku := strings.TrimSpace(r.URL.Query().Get("sku"))
		if sku == "" {
			writeError(w, http.StatusBadRequest, "sku_is_required")
			return
		}
		writeJSON(w, http.StatusOK, svc.Inventory(sku))
	})
	mux.HandleFunc("POST /api/transfers", func(w http.ResponseWriter, r *http.Request) {
		var req service.TransferRequest
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json")
			return
		}
		transfer, err := svc.CreateTransfer(req)
		if err != nil {
			status := http.StatusConflict
			if errors.Is(err, service.ErrIdempotencyKey) || errors.Is(err, service.ErrInvalidTransition) {
				status = http.StatusBadRequest
			}
			writeError(w, status, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, transfer)
	})
	return mux
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}
