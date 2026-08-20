package service

import (
	"errors"
	"sync"

	"github.com/Q1mi/kbot-crossborder/internal/domain"
)

var ErrNotImplemented = errors.New("inventory transfer is not implemented")

type TransferRequest struct {
	SKU            string
	FromWarehouse  string
	ToWarehouse    string
	IdempotencyKey string
	Quantity       int
	DryRun         bool
}

type Service struct {
	mu          sync.RWMutex
	orders      map[string]domain.Order
	inventory   map[string]domain.InventoryBalance
	transfers   map[string]domain.InventoryTransfer
	idempotency map[string]domain.InventoryTransfer
}

func NewSeeded() *Service {
	return &Service{
		orders: map[string]domain.Order{
			"TTS-20260801-1001": {
				ID: "TTS-20260801-1001", Market: "US", Currency: "USD",
				Amount: 129.99, Status: domain.OrderAwaitingShipment,
				FulfillmentWH: "WH-CN-SZ", CancellationOpen: true,
				Items: []domain.OrderItem{{SKU: "SKU-BLACK-M-01", Quantity: 1, Price: 129.99}},
			},
		},
		inventory: map[string]domain.InventoryBalance{
			inventoryKey("WH-CN-SZ", "SKU-BLACK-M-01"): {
				WarehouseID: "WH-CN-SZ", SKU: "SKU-BLACK-M-01", Available: 0,
			},
			inventoryKey("WH-US-LAX", "SKU-BLACK-M-01"): {
				WarehouseID: "WH-US-LAX", SKU: "SKU-BLACK-M-01", Available: 18,
			},
		},
		transfers:   make(map[string]domain.InventoryTransfer),
		idempotency: make(map[string]domain.InventoryTransfer),
	}
}

func (s *Service) GetOrder(id string) (domain.Order, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	order, ok := s.orders[id]
	return order, ok
}

func (s *Service) Inventory(sku string) []domain.InventoryBalance {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]domain.InventoryBalance, 0, len(s.inventory))
	for _, balance := range s.inventory {
		if balance.SKU == sku {
			result = append(result, balance)
		}
	}
	return result
}

func (s *Service) CreateTransfer(TransferRequest) (domain.InventoryTransfer, error) {
	return domain.InventoryTransfer{}, ErrNotImplemented
}

func inventoryKey(warehouse, sku string) string {
	return warehouse + "|" + sku
}
