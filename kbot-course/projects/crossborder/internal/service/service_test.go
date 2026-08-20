package service

import (
	"errors"
	"testing"
)

func TestCreateTransferIsIdempotent(t *testing.T) {
	t.Parallel()

	svc := NewSeeded()
	req := TransferRequest{
		SKU: "SKU-BLACK-M-01", FromWarehouse: "WH-US-LAX",
		ToWarehouse: "WH-CN-SZ", Quantity: 2, IdempotencyKey: "lesson-02",
	}

	first, err := svc.CreateTransfer(req)
	if err != nil {
		t.Fatalf("first transfer: %v", err)
	}
	second, err := svc.CreateTransfer(req)
	if err != nil {
		t.Fatalf("second transfer: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("idempotent transfer IDs differ: %q and %q", first.ID, second.ID)
	}

	balances := svc.Inventory(req.SKU)
	want := map[string]int{"WH-US-LAX": 16, "WH-CN-SZ": 2}
	for _, balance := range balances {
		if got, ok := want[balance.WarehouseID]; ok && balance.Available != got {
			t.Fatalf("%s available = %d, want %d", balance.WarehouseID, balance.Available, got)
		}
	}
}

func TestCreateTransferDryRunDoesNotChangeInventory(t *testing.T) {
	t.Parallel()

	svc := NewSeeded()
	result, err := svc.CreateTransfer(TransferRequest{
		SKU: "SKU-BLACK-M-01", FromWarehouse: "WH-US-LAX",
		ToWarehouse: "WH-CN-SZ", Quantity: 3,
		IdempotencyKey: "lesson-02-dry-run", DryRun: true,
	})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if result.Status != "validated" {
		t.Fatalf("status = %q, want validated", result.Status)
	}

	for _, balance := range svc.Inventory("SKU-BLACK-M-01") {
		if balance.WarehouseID == "WH-US-LAX" && balance.Available != 18 {
			t.Fatalf("source inventory changed during dry run: %d", balance.Available)
		}
		if balance.WarehouseID == "WH-CN-SZ" && balance.Available != 0 {
			t.Fatalf("target inventory changed during dry run: %d", balance.Available)
		}
	}
}

func TestCreateTransferRejectsInsufficientInventory(t *testing.T) {
	t.Parallel()

	_, err := NewSeeded().CreateTransfer(TransferRequest{
		SKU: "SKU-BLACK-M-01", FromWarehouse: "WH-CN-SZ",
		ToWarehouse: "WH-US-LAX", Quantity: 1, IdempotencyKey: "lesson-02-empty",
	})
	if !errors.Is(err, ErrInsufficientStock) {
		t.Fatalf("error = %v, want ErrInsufficientStock", err)
	}
}

func TestCreateTransferBindsIdempotencyKeyToRequest(t *testing.T) {
	t.Parallel()

	svc := NewSeeded()
	first := TransferRequest{
		SKU: "SKU-BLACK-M-01", FromWarehouse: "WH-US-LAX",
		ToWarehouse: "WH-CN-SZ", Quantity: 2, IdempotencyKey: "lesson-02-conflict",
	}
	if _, err := svc.CreateTransfer(first); err != nil {
		t.Fatalf("first transfer: %v", err)
	}
	changed := first
	changed.Quantity = 3
	if _, err := svc.CreateTransfer(changed); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed request error = %v, want ErrIdempotencyConflict", err)
	}

	for _, balance := range svc.Inventory(first.SKU) {
		if balance.WarehouseID == "WH-US-LAX" && balance.Available != 16 {
			t.Fatalf("conflicting retry changed source inventory: %d", balance.Available)
		}
	}
}
