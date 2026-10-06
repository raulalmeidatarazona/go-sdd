package domain

import "context"

// Repository is the write-side persistence port. Implementations store the
// aggregate and its pending events atomically (transactional outbox) and are
// scoped to the tenant in ctx.
type Repository interface {
	// Get returns ErrOrderNotFound when the order does not exist for the tenant.
	Get(ctx context.Context, id OrderID) (*Order, error)
	// FindByIdempotencyKey returns the order id and request fingerprint stored
	// with key, or found=false.
	FindByIdempotencyKey(ctx context.Context, key IdempotencyKey) (id OrderID, fingerprint string, found bool, err error)
	// Add inserts a new order. Returns ErrDuplicateIdempotencyKey on a race.
	Add(ctx context.Context, order *Order, key IdempotencyKey, fingerprint string) error
	// Update persists changes with optimistic locking; returns ErrConcurrentModification.
	Update(ctx context.Context, order *Order) error
}
