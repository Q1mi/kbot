package httpapi

import (
	"net/http"

	"github.com/Q1mi/kbot-crossborder/internal/domain"
	"github.com/Q1mi/kbot-crossborder/internal/service"
)

type Service interface {
	GetOrder(id string) (domain.Order, bool)
	Inventory(sku string) []domain.InventoryBalance
	CreateTransfer(req service.TransferRequest) (domain.InventoryTransfer, error)
}

func New(Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}
