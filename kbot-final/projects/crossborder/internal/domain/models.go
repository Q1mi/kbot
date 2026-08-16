package domain

import "time"

const (
	OrderAwaitingShipment = "awaiting_shipment"
	OrderPartiallyShipped = "partially_shipped"
	OrderCancelled        = "cancelled"
)

type OrderItem struct {
	SKU      string  `json:"sku"`
	Quantity int     `json:"quantity"`
	Price    float64 `json:"price"`
}

type Order struct {
	ID               string      `json:"id"`
	ShopID           string      `json:"shop_id"`
	Market           string      `json:"market"`
	Currency         string      `json:"currency"`
	Amount           float64     `json:"amount"`
	Status           string      `json:"status"`
	FulfillmentWH    string      `json:"fulfillment_warehouse"`
	ShipBy           time.Time   `json:"ship_by"`
	Items            []OrderItem `json:"items"`
	CancellationOpen bool        `json:"cancellation_open"`
}

type InventoryBalance struct {
	WarehouseID string `json:"warehouse_id"`
	SKU         string `json:"sku"`
	Available   int    `json:"available"`
	Reserved    int    `json:"reserved"`
}

type ShippingOption struct {
	Provider     string  `json:"provider"`
	Service      string  `json:"service"`
	Cost         float64 `json:"cost"`
	Currency     string  `json:"currency"`
	DeliveryDays int     `json:"delivery_days"`
	SLAEligible  bool    `json:"sla_eligible"`
}

type InventoryTransfer struct {
	ID            string    `json:"id"`
	SKU           string    `json:"sku"`
	FromWarehouse string    `json:"from_warehouse"`
	ToWarehouse   string    `json:"to_warehouse"`
	Quantity      int       `json:"quantity"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
}

type Refund struct {
	ID        string    `json:"id"`
	OrderID   string    `json:"order_id"`
	Amount    float64   `json:"amount"`
	Currency  string    `json:"currency"`
	Reason    string    `json:"reason"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type SettlementStatement struct {
	ID             string  `json:"id"`
	ExpectedAmount float64 `json:"expected_amount"`
	PaidAmount     float64 `json:"paid_amount"`
	Currency       string  `json:"currency"`
	Status         string  `json:"status"`
}

type ReconciliationCase struct {
	ID          string    `json:"id"`
	StatementID string    `json:"statement_id"`
	Difference  float64   `json:"difference"`
	Reason      string    `json:"reason"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}
