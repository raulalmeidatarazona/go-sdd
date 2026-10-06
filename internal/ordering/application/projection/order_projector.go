// Package projection keeps the read model in sync with domain events. It is
// the "Q" side's writer in CQRS and runs in the worker process.
package projection

import (
	"context"
	"errors"
	"fmt"

	"github.com/raulalmeidatarazona/go-sdd/internal/ordering/application/query"
	"github.com/raulalmeidatarazona/go-sdd/internal/ordering/domain"
	"github.com/raulalmeidatarazona/go-sdd/internal/platform/ddd"
)

// ErrViewMissing means an update arrived before the view was created. The
// consumer retries it; after the delivery limit it lands in the DLQ.
var ErrViewMissing = errors.New("order view not projected yet")

// Store writes the read model. Updates must be version-guarded so replays and
// out-of-date messages are no-ops.
type Store interface {
	InsertPlaced(ctx context.Context, view query.OrderView) error
	// ApplyCancelled returns ErrViewMissing if the view does not exist.
	ApplyCancelled(ctx context.Context, event domain.OrderCancelled, version int64) error
}

// Envelope is a decoded event plus the metadata needed to project it.
type Envelope struct {
	AggregateVersion int64
	Event            ddd.Event
}

type OrderProjector struct {
	store Store
}

func NewOrderProjector(store Store) *OrderProjector {
	return &OrderProjector{store: store}
}

func (p *OrderProjector) Project(ctx context.Context, env Envelope) error {
	switch event := env.Event.(type) {
	case domain.OrderPlaced:
		return p.store.InsertPlaced(ctx, placedView(event, env.AggregateVersion))
	case domain.OrderCancelled:
		return p.store.ApplyCancelled(ctx, event, env.AggregateVersion)
	default:
		return fmt.Errorf("projector: unsupported event %T", env.Event)
	}
}

func placedView(e domain.OrderPlaced, version int64) query.OrderView {
	lines := make([]query.OrderViewLine, 0, len(e.Lines))
	for _, l := range e.Lines {
		lines = append(lines, query.OrderViewLine{SKU: string(l.SKU()), Quantity: l.Quantity(), UnitPriceMinor: l.UnitPrice().AmountMinor()})
	}
	return query.OrderView{
		ID:         string(e.OrderID),
		CustomerID: string(e.CustomerID),
		Status:     string(domain.StatusPlaced),
		Currency:   string(e.Currency),
		Lines:      lines,
		TotalMinor: e.Total.AmountMinor(),
		PlacedAt:   e.PlacedAt,
		UpdatedAt:  e.PlacedAt,
		Version:    version,
	}
}
