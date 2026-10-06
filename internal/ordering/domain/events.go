package domain

import "time"

// Event names equal the protobuf message names in order_events.proto; they are
// the routing keys on the bus.
const (
	EventOrderPlaced    = "oms.order.v1.OrderPlaced"
	EventOrderCancelled = "oms.order.v1.OrderCancelled"
)

type OrderPlaced struct {
	OrderID    OrderID
	CustomerID CustomerID
	Currency   Currency
	Lines      []LineItem
	Total      Money
	PlacedAt   time.Time
}

func (OrderPlaced) EventName() string { return EventOrderPlaced }

type OrderCancelled struct {
	OrderID     OrderID
	Reason      string
	CancelledAt time.Time
}

func (OrderCancelled) EventName() string { return EventOrderCancelled }
