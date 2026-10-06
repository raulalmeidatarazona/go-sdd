// Package inbox makes consumers idempotent: a message is processed at most once
// per consumer because the claim and the side effects commit in one transaction.
package inbox

import (
	"context"
	"fmt"

	"github.com/raulalmeidatarazona/go-sdd/internal/platform/postgres"
	"github.com/raulalmeidatarazona/go-sdd/internal/platform/rabbitmq"
	"github.com/raulalmeidatarazona/go-sdd/internal/platform/tenancy"
)

// Handle wraps a message handler so that it runs inside a tenant transaction
// and skips messages this consumer already processed.
func Handle(db *postgres.DB, consumer string, next func(ctx context.Context, m rabbitmq.Message) error) func(context.Context, rabbitmq.Message) error {
	return func(ctx context.Context, m rabbitmq.Message) error {
		tenant, err := tenancy.Parse(m.TenantID)
		if err != nil {
			return fmt.Errorf("message %s: %w", m.ID, err)
		}
		ctx = tenancy.WithTenant(ctx, tenant)
		return db.InTenantTx(ctx, func(ctx context.Context) error {
			tag, err := postgres.Tx(ctx).Exec(ctx, `
				INSERT INTO messaging.inbox (consumer, message_id, tenant_id)
				VALUES ($1, $2, $3)
				ON CONFLICT DO NOTHING`, consumer, m.ID, string(tenant))
			if err != nil {
				return fmt.Errorf("claim message %s: %w", m.ID, err)
			}
			if tag.RowsAffected() == 0 {
				return nil // duplicate delivery
			}
			return next(ctx, m)
		})
	}
}
