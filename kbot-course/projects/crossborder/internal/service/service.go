package service

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Q1mi/kbot-crossborder/internal/domain"
)

var (
	ErrNotFound            = errors.New("business resource not found")
	ErrInvalidTransition   = errors.New("invalid business state transition")
	ErrInsufficientStock   = errors.New("insufficient inventory")
	ErrIdempotencyKey      = errors.New("idempotency_key is required")
	ErrIdempotencyConflict = errors.New("idempotency key was already used for a different request")
)

type TransferRequest struct {
	SKU            string `json:"sku"`
	FromWarehouse  string `json:"from_warehouse"`
	ToWarehouse    string `json:"to_warehouse"`
	IdempotencyKey string `json:"idempotency_key"`
	Quantity       int    `json:"quantity"`
	DryRun         bool   `json:"dry_run"`
}

type Service struct {
	mu          sync.RWMutex
	orders      map[string]domain.Order
	inventory   map[string]domain.InventoryBalance
	transfers   map[string]domain.InventoryTransfer
	idempotency map[string]idempotencyRecord
}

type idempotencyRecord struct {
	fingerprint [32]byte
	transfer    domain.InventoryTransfer
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
		idempotency: make(map[string]idempotencyRecord),
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

func (s *Service) CreateTransfer(req TransferRequest) (domain.InventoryTransfer, error) {
	if req.IdempotencyKey == "" {
		return domain.InventoryTransfer{}, ErrIdempotencyKey
	}
	if req.Quantity <= 0 || req.FromWarehouse == req.ToWarehouse {
		return domain.InventoryTransfer{}, ErrInvalidTransition
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	fingerprint := transferFingerprint(req)
	if cached, ok := s.idempotency[req.IdempotencyKey]; ok {
		if cached.fingerprint != fingerprint {
			return domain.InventoryTransfer{}, ErrIdempotencyConflict
		}
		return cached.transfer, nil
	}

	fromKey := inventoryKey(req.FromWarehouse, req.SKU)
	toKey := inventoryKey(req.ToWarehouse, req.SKU)
	from, ok := s.inventory[fromKey]
	if !ok {
		return domain.InventoryTransfer{}, ErrNotFound
	}
	if from.Available < req.Quantity {
		return domain.InventoryTransfer{}, ErrInsufficientStock
	}

	if req.DryRun {
		transfer := domain.InventoryTransfer{
			SKU: req.SKU, FromWarehouse: req.FromWarehouse,
			ToWarehouse: req.ToWarehouse, Quantity: req.Quantity,
			Status: "validated",
		}
		s.idempotency[req.IdempotencyKey] = idempotencyRecord{fingerprint: fingerprint, transfer: transfer}
		return transfer, nil
	}

	to := s.inventory[toKey]
	to.WarehouseID = req.ToWarehouse
	to.SKU = req.SKU
	from.Available -= req.Quantity
	to.Available += req.Quantity
	s.inventory[fromKey] = from
	s.inventory[toKey] = to

	transfer := domain.InventoryTransfer{
		ID:  fmt.Sprintf("TR-%04d", len(s.transfers)+1),
		SKU: req.SKU, FromWarehouse: req.FromWarehouse,
		ToWarehouse: req.ToWarehouse, Quantity: req.Quantity,
		Status: "created", CreatedAt: time.Now().UTC(),
	}
	s.transfers[transfer.ID] = transfer
	s.idempotency[req.IdempotencyKey] = idempotencyRecord{fingerprint: fingerprint, transfer: transfer}
	return transfer, nil
}

func transferFingerprint(req TransferRequest) [32]byte {
	return sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%t", req.SKU, req.FromWarehouse, req.ToWarehouse, req.Quantity, req.DryRun)))
}

func inventoryKey(warehouse, sku string) string {
	return warehouse + "|" + sku
}
