// Package domain is the ordering model: the Order aggregate, its value objects,
// events and the repository port. It depends on nothing but the shared kernel.
package domain

import (
	"time"
	"unicode/utf8"

	"github.com/raulalmeidatarazona/go-sdd/internal/platform/ddd"
	"github.com/raulalmeidatarazona/go-sdd/internal/platform/tenancy"
)

// Status is the order lifecycle state.
type Status string

const (
	StatusPlaced    Status = "PLACED"
	StatusCancelled Status = "CANCELLED"
)

const (
	maxLines        = 100
	maxReasonLength = 500
)

// Order is the aggregate root of the ordering context. Invariants:
//   - it has 1..100 lines with distinct SKUs, all in the order currency;
//   - total equals the sum of line subtotals;
//   - a cancelled order cannot change again.
type Order struct {
	ddd.AggregateRoot

	id                 OrderID
	tenantID           tenancy.ID
	customerID         CustomerID
	status             Status
	lines              []LineItem
	total              Money
	cancellationReason string
	placedAt           time.Time
	updatedAt          time.Time
}

// Place creates a new order and records OrderPlaced.
func Place(id OrderID, tenantID tenancy.ID, customerID CustomerID, currency Currency, lines []LineItem, now time.Time) (*Order, error) {
	if len(lines) == 0 {
		return nil, ErrNoLines
	}
	if len(lines) > maxLines {
		return nil, ErrTooManyLines
	}
	total, err := NewMoney(0, currency)
	if err != nil {
		return nil, err
	}
	seen := make(map[SKU]bool, len(lines))
	for _, line := range lines {
		if seen[line.SKU()] {
			return nil, ErrDuplicateSKU
		}
		seen[line.SKU()] = true
		if line.UnitPrice().Currency() != currency {
			return nil, ErrInvalidCurrency
		}
		subtotal, err := line.Subtotal()
		if err != nil {
			return nil, err
		}
		if total, err = total.Plus(subtotal); err != nil {
			return nil, err
		}
	}

	now = now.UTC()
	order := &Order{
		id:         id,
		tenantID:   tenantID,
		customerID: customerID,
		status:     StatusPlaced,
		lines:      append([]LineItem(nil), lines...),
		total:      total,
		placedAt:   now,
		updatedAt:  now,
	}
	order.Record(OrderPlaced{
		OrderID:    id,
		CustomerID: customerID,
		Currency:   currency,
		Lines:      order.Lines(),
		Total:      total,
		PlacedAt:   now,
	})
	return order, nil
}

// Cancel moves a placed order to cancelled and records OrderCancelled.
func (o *Order) Cancel(reason string, now time.Time) error {
	if reason == "" || utf8.RuneCountInString(reason) > maxReasonLength {
		return ErrInvalidCancelReason
	}
	if o.status == StatusCancelled {
		return ErrOrderAlreadyCancelled
	}
	now = now.UTC()
	o.status = StatusCancelled
	o.cancellationReason = reason
	o.updatedAt = now
	o.Record(OrderCancelled{OrderID: o.id, Reason: reason, CancelledAt: now})
	return nil
}

func (o *Order) ID() OrderID                { return o.id }
func (o *Order) TenantID() tenancy.ID       { return o.tenantID }
func (o *Order) CustomerID() CustomerID     { return o.customerID }
func (o *Order) Status() Status             { return o.status }
func (o *Order) Total() Money               { return o.total }
func (o *Order) CancellationReason() string { return o.cancellationReason }
func (o *Order) PlacedAt() time.Time        { return o.placedAt }
func (o *Order) UpdatedAt() time.Time       { return o.updatedAt }
func (o *Order) Lines() []LineItem          { return append([]LineItem(nil), o.lines...) }

// Snapshot is the persisted state used to rehydrate an Order. Only repositories use it.
type Snapshot struct {
	ID                 OrderID
	TenantID           tenancy.ID
	CustomerID         CustomerID
	Status             Status
	Lines              []LineItem
	Total              Money
	CancellationReason string
	PlacedAt           time.Time
	UpdatedAt          time.Time
	Version            int64
}

// Rehydrate rebuilds an aggregate from storage without recording events.
func Rehydrate(s Snapshot) *Order {
	order := &Order{
		id:                 s.ID,
		tenantID:           s.TenantID,
		customerID:         s.CustomerID,
		status:             s.Status,
		lines:              s.Lines,
		total:              s.Total,
		cancellationReason: s.CancellationReason,
		placedAt:           s.PlacedAt,
		updatedAt:          s.UpdatedAt,
	}
	order.MarkPersisted(s.Version)
	return order
}
