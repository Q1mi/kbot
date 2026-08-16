package service

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Q1mi/kbot-crossborder/internal/domain"
)

var (
	ErrNotFound          = errors.New("business resource not found")
	ErrInvalidTransition = errors.New("invalid business state transition")
	ErrInsufficientStock = errors.New("insufficient inventory")
	ErrIdempotencyKey    = errors.New("idempotency_key is required")
)

type Service struct {
	mu              sync.RWMutex
	orders          map[string]domain.Order
	inventory       map[string]domain.InventoryBalance
	statements      map[string]domain.SettlementStatement
	transfers       map[string]domain.InventoryTransfer
	refunds         map[string]domain.Refund
	reconciliations map[string]domain.ReconciliationCase
	idempotency     map[string]any
}

func NewSeeded() *Service {
	shipBy := time.Now().UTC().Add(8 * time.Hour).Truncate(time.Second)
	return &Service{
		orders: map[string]domain.Order{
			"TTS-20260801-1001": {
				ID: "TTS-20260801-1001", ShopID: "SHOP-US-001", Market: "US",
				Currency: "USD", Amount: 129.99, Status: domain.OrderAwaitingShipment,
				FulfillmentWH: "WH-CN-SZ", ShipBy: shipBy, CancellationOpen: true,
				Items: []domain.OrderItem{{SKU: "SKU-BLACK-M-01", Quantity: 1, Price: 129.99}},
			},
		},
		inventory: map[string]domain.InventoryBalance{
			inventoryKey("WH-CN-SZ", "SKU-BLACK-M-01"):  {WarehouseID: "WH-CN-SZ", SKU: "SKU-BLACK-M-01", Available: 0},
			inventoryKey("WH-US-LAX", "SKU-BLACK-M-01"): {WarehouseID: "WH-US-LAX", SKU: "SKU-BLACK-M-01", Available: 18},
		},
		statements: map[string]domain.SettlementStatement{
			"STMT-2026-31": {ID: "STMT-2026-31", ExpectedAmount: 118.47, PaidAmount: 106.95, Currency: "USD", Status: "difference_detected"},
		},
		transfers: make(map[string]domain.InventoryTransfer), refunds: make(map[string]domain.Refund),
		reconciliations: make(map[string]domain.ReconciliationCase), idempotency: make(map[string]any),
	}
}

func (s *Service) GetOrder(id string) (domain.Order, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	order, ok := s.orders[id]
	if !ok {
		return domain.Order{}, ErrNotFound
	}
	return order, nil
}

func (s *Service) Inventory(sku string) []domain.InventoryBalance {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []domain.InventoryBalance
	for _, balance := range s.inventory {
		if balance.SKU == sku {
			result = append(result, balance)
		}
	}
	return result
}

func (s *Service) ShippingOptions(orderID string) ([]domain.ShippingOption, error) {
	if _, err := s.GetOrder(orderID); err != nil {
		return nil, err
	}
	return []domain.ShippingOption{
		{Provider: "4PX", Service: "US Priority", Cost: 12.80, Currency: "USD", DeliveryDays: 7, SLAEligible: true},
		{Provider: "UPS", Service: "Ground", Cost: 9.40, Currency: "USD", DeliveryDays: 4, SLAEligible: true},
	}, nil
}

type TransferRequest struct {
	SKU, FromWarehouse, ToWarehouse, IdempotencyKey string
	Quantity                                        int
	DryRun                                          bool
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
	if cached, ok := s.idempotency["transfer:"+req.IdempotencyKey]; ok {
		return cached.(domain.InventoryTransfer), nil
	}
	fromKey, toKey := inventoryKey(req.FromWarehouse, req.SKU), inventoryKey(req.ToWarehouse, req.SKU)
	from, ok := s.inventory[fromKey]
	if !ok {
		return domain.InventoryTransfer{}, ErrNotFound
	}
	if from.Available < req.Quantity {
		return domain.InventoryTransfer{}, ErrInsufficientStock
	}
	if req.DryRun {
		return domain.InventoryTransfer{SKU: req.SKU, FromWarehouse: req.FromWarehouse, ToWarehouse: req.ToWarehouse, Quantity: req.Quantity, Status: "validated"}, nil
	}
	to := s.inventory[toKey]
	to.WarehouseID, to.SKU = req.ToWarehouse, req.SKU
	from.Available -= req.Quantity
	to.Available += req.Quantity
	s.inventory[fromKey], s.inventory[toKey] = from, to
	transfer := domain.InventoryTransfer{ID: fmt.Sprintf("TR-%04d", len(s.transfers)+1), SKU: req.SKU, FromWarehouse: req.FromWarehouse, ToWarehouse: req.ToWarehouse, Quantity: req.Quantity, Status: "created", CreatedAt: time.Now().UTC()}
	s.transfers[transfer.ID] = transfer
	s.idempotency["transfer:"+req.IdempotencyKey] = transfer
	return transfer, nil
}

type RefundRequest struct {
	OrderID, Reason, IdempotencyKey string
	Amount                          float64
	DryRun                          bool
}

func (s *Service) ApproveRefund(req RefundRequest) (domain.Refund, error) {
	if req.IdempotencyKey == "" {
		return domain.Refund{}, ErrIdempotencyKey
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if cached, ok := s.idempotency["refund:"+req.IdempotencyKey]; ok {
		return cached.(domain.Refund), nil
	}
	order, ok := s.orders[req.OrderID]
	if !ok {
		return domain.Refund{}, ErrNotFound
	}
	if order.Status != domain.OrderAwaitingShipment || req.Amount <= 0 || req.Amount > order.Amount {
		return domain.Refund{}, ErrInvalidTransition
	}
	if req.DryRun {
		return domain.Refund{OrderID: order.ID, Amount: req.Amount, Currency: order.Currency, Reason: req.Reason, Status: "validated"}, nil
	}
	order.Status = domain.OrderCancelled
	s.orders[order.ID] = order
	refund := domain.Refund{ID: fmt.Sprintf("RF-%04d", len(s.refunds)+1), OrderID: order.ID, Amount: req.Amount, Currency: order.Currency, Reason: req.Reason, Status: "approved", CreatedAt: time.Now().UTC()}
	s.refunds[refund.ID] = refund
	s.idempotency["refund:"+req.IdempotencyKey] = refund
	return refund, nil
}

func (s *Service) GetStatement(id string) (domain.SettlementStatement, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	statement, ok := s.statements[id]
	if !ok {
		return domain.SettlementStatement{}, ErrNotFound
	}
	return statement, nil
}

type ReconciliationRequest struct {
	StatementID, Reason, IdempotencyKey string
	DryRun                              bool
}

func (s *Service) CreateReconciliation(req ReconciliationRequest) (domain.ReconciliationCase, error) {
	if req.IdempotencyKey == "" {
		return domain.ReconciliationCase{}, ErrIdempotencyKey
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if cached, ok := s.idempotency["reconciliation:"+req.IdempotencyKey]; ok {
		return cached.(domain.ReconciliationCase), nil
	}
	statement, ok := s.statements[req.StatementID]
	if !ok {
		return domain.ReconciliationCase{}, ErrNotFound
	}
	if statement.Status != "difference_detected" {
		return domain.ReconciliationCase{}, ErrInvalidTransition
	}
	if req.DryRun {
		return domain.ReconciliationCase{StatementID: statement.ID, Difference: statement.ExpectedAmount - statement.PaidAmount, Reason: req.Reason, Status: "validated"}, nil
	}
	item := domain.ReconciliationCase{ID: fmt.Sprintf("RC-%04d", len(s.reconciliations)+1), StatementID: statement.ID, Difference: statement.ExpectedAmount - statement.PaidAmount, Reason: req.Reason, Status: "submitted", CreatedAt: time.Now().UTC()}
	s.reconciliations[item.ID] = item
	s.idempotency["reconciliation:"+req.IdempotencyKey] = item
	return item, nil
}

func inventoryKey(warehouse, sku string) string { return warehouse + "|" + sku }
