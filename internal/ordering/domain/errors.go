package domain

import "github.com/raulalmeidatarazona/go-sdd/internal/platform/apperr"

// Error codes are part of the public API (google.rpc.ErrorInfo.reason). Never rename them.
var (
	ErrInvalidOrderID          = apperr.Invalid("invalid_order_id", "order id must be a UUID")
	ErrInvalidCustomerID       = apperr.Invalid("invalid_customer_id", "customer id is required and must be at most 64 characters")
	ErrInvalidCurrency         = apperr.Invalid("invalid_currency", "currency must be an ISO 4217 code such as EUR")
	ErrInvalidAmount           = apperr.Invalid("invalid_amount", "amounts must be non-negative and within range")
	ErrInvalidSKU              = apperr.Invalid("invalid_sku", "sku must be 1-64 characters of A-Z, 0-9, '-' or '_'")
	ErrInvalidQuantity         = apperr.Invalid("invalid_quantity", "quantity must be between 1 and 10000")
	ErrNoLines                 = apperr.Invalid("order_without_lines", "an order needs at least one line")
	ErrTooManyLines            = apperr.Invalid("too_many_lines", "an order accepts at most 100 lines")
	ErrDuplicateSKU            = apperr.Invalid("duplicate_sku", "each sku may appear only once per order")
	ErrInvalidCancelReason     = apperr.Invalid("invalid_cancellation_reason", "cancellation reason is required and must be at most 500 characters")
	ErrInvalidIdempotencyKey   = apperr.Invalid("invalid_idempotency_key", "idempotency key is required and must be at most 128 characters")
	ErrOrderNotFound           = apperr.New(apperr.KindNotFound, "order_not_found", "order not found")
	ErrOrderAlreadyCancelled   = apperr.New(apperr.KindFailedPrecondition, "order_already_cancelled", "order is already cancelled")
	ErrIdempotencyKeyReused    = apperr.New(apperr.KindConflict, "idempotency_key_reused", "idempotency key was already used with a different request")
	ErrConcurrentModification  = apperr.New(apperr.KindAborted, "concurrent_modification", "order was modified concurrently; retry")
	ErrDuplicateIdempotencyKey = apperr.New(apperr.KindConflict, "duplicate_idempotency_key", "idempotency key already stored")
)
