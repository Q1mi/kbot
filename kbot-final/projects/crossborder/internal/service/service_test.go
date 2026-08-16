package service

import (
	"errors"
	"math"
	"testing"

	"github.com/Q1mi/kbot-crossborder/internal/domain"
)

func TestInventoryTransferIsValidatedAndIdempotent(t *testing.T) {
	svc := NewSeeded()
	req := TransferRequest{SKU: "SKU-BLACK-M-01", FromWarehouse: "WH-US-LAX", ToWarehouse: "WH-CN-SZ", Quantity: 2, IdempotencyKey: "transfer-1"}
	first, err := svc.CreateTransfer(req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.CreateTransfer(req)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("idempotent replay created %q and %q", first.ID, second.ID)
	}
	balances := svc.Inventory(req.SKU)
	available := map[string]int{}
	for _, balance := range balances {
		available[balance.WarehouseID] = balance.Available
	}
	if available["WH-US-LAX"] != 16 || available["WH-CN-SZ"] != 2 {
		t.Fatalf("unexpected balances: %#v", available)
	}
}

func TestRefundEnforcesOrderStateAndAmount(t *testing.T) {
	svc := NewSeeded()
	_, err := svc.ApproveRefund(RefundRequest{OrderID: "TTS-20260801-1001", Amount: 999, IdempotencyKey: "refund-invalid"})
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("want invalid transition, got %v", err)
	}
	refund, err := svc.ApproveRefund(RefundRequest{OrderID: "TTS-20260801-1001", Amount: 129.99, Reason: "buyer cancellation", IdempotencyKey: "refund-1"})
	if err != nil {
		t.Fatal(err)
	}
	if refund.Status != "approved" {
		t.Fatalf("unexpected refund: %#v", refund)
	}
	order, _ := svc.GetOrder("TTS-20260801-1001")
	if order.Status != domain.OrderCancelled {
		t.Fatalf("unexpected order status %q", order.Status)
	}
}

func TestReconciliationUsesStatementDifference(t *testing.T) {
	svc := NewSeeded()
	item, err := svc.CreateReconciliation(ReconciliationRequest{StatementID: "STMT-2026-31", Reason: "platform shipping fee duplicated", IdempotencyKey: "rc-1"})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(item.Difference-11.52) > 0.0001 {
		t.Fatalf("want 11.52, got %.2f", item.Difference)
	}
}
