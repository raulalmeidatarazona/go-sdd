// Package outbox implements the transactional outbox: events are written in the
// same transaction as the aggregate, then a relay publishes them to RabbitMQ.
// Delivery is at-least-once; consumers deduplicate with the inbox.
package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/raulalmeidatarazona/go-sdd/internal/platform/postgres"
	"github.com/raulalmeidatarazona/go-sdd/internal/platform/tenancy"
)

// Message is an encoded integration event ready to be stored.
type Message struct {
	EventType        string // fully qualified proto name; also the routing key
	AggregateType    string
	AggregateID      string
	AggregateVersion int64
	Payload          []byte // protobuf wire format
	OccurredAt       time.Time
}

// Add stores messages inside the current tenant transaction, capturing the
// trace context so consumers continue the same trace.
func Add(ctx context.Context, messages ...Message) error {
	if len(messages) == 0 {
		return nil
	}
	tenant, err := tenancy.FromContext(ctx)
	if err != nil {
		return err
	}
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	headers, err := json.Marshal(carrier)
	if err != nil {
		return fmt.Errorf("encode trace headers: %w", err)
	}

	tx := postgres.Tx(ctx)
	for _, m := range messages {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO messaging.outbox
				(message_id, tenant_id, event_type, aggregate_type, aggregate_id, aggregate_version, payload, headers, occurred_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			id, string(tenant), m.EventType, m.AggregateType, m.AggregateID, m.AggregateVersion, m.Payload, headers, m.OccurredAt)
		if err != nil {
			return fmt.Errorf("insert outbox message: %w", err)
		}
	}
	return nil
}
