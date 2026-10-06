// Package tenancy carries the tenant through a request. Every data access is
// scoped to the tenant in the context; PostgreSQL row-level security enforces it.
package tenancy

import (
	"context"

	"github.com/google/uuid"

	"github.com/raulalmeidatarazona/go-sdd/internal/platform/apperr"
)

// ID identifies a tenant (a company using the OMS).
type ID string

var (
	ErrMissing = apperr.New(apperr.KindUnauthenticated, "tenant_missing", "tenant is required")
	ErrInvalid = apperr.New(apperr.KindUnauthenticated, "tenant_invalid", "tenant id must be a UUID")
)

// Parse validates a tenant identifier.
func Parse(raw string) (ID, error) {
	if raw == "" {
		return "", ErrMissing
	}
	parsed, err := uuid.Parse(raw)
	if err != nil {
		return "", ErrInvalid
	}
	return ID(parsed.String()), nil
}

type contextKey struct{}

func WithTenant(ctx context.Context, id ID) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

// FromContext returns the tenant or ErrMissing. Code paths that touch tenant
// data must call it instead of assuming a default tenant.
func FromContext(ctx context.Context) (ID, error) {
	id, ok := ctx.Value(contextKey{}).(ID)
	if !ok || id == "" {
		return "", ErrMissing
	}
	return id, nil
}
