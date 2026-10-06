// Package postgres implements the ordering ports on PostgreSQL. Write model in
// schema "ordering", read model in schema "ordering_read". Every method runs in
// the tenant transaction, so row-level security applies.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/raulalmeidatarazona/go-sdd/internal/ordering/domain"
	"github.com/raulalmeidatarazona/go-sdd/internal/ordering/infrastructure/events"
	"github.com/raulalmeidatarazona/go-sdd/internal/platform/outbox"
	"github.com/raulalmeidatarazona/go-sdd/internal/platform/postgres"
	"github.com/raulalmeidatarazona/go-sdd/internal/platform/tenancy"
)

const aggregateType = "order"

type OrderRepository struct {
	db *postgres.DB
}

func NewOrderRepository(db *postgres.DB) *OrderRepository {
	return &OrderRepository{db: db}
}

var _ domain.Repository = (*OrderRepository)(nil)

func (r *OrderRepository) Get(ctx context.Context, id domain.OrderID) (*domain.Order, error) {
	var order *domain.Order
	err := r.db.InTenantTx(ctx, func(ctx context.Context) error {
		tx := postgres.Tx(ctx)
		var (
			s          domain.Snapshot
			tenantID   string
			currency   string
			totalMinor int64
			reason     *string
		)
		err := tx.QueryRow(ctx, `
			SELECT tenant_id::text, customer_id, status, currency, total_minor, cancellation_reason, placed_at, updated_at, version
			FROM ordering.orders WHERE id = $1`, string(id)).
			Scan(&tenantID, &s.CustomerID, &s.Status, &currency, &totalMinor, &reason, &s.PlacedAt, &s.UpdatedAt, &s.Version)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrOrderNotFound
		}
		if err != nil {
			return fmt.Errorf("load order: %w", err)
		}
		s.ID = id
		s.TenantID = tenancy.ID(tenantID)
		if reason != nil {
			s.CancellationReason = *reason
		}
		cur := domain.Currency(currency)
		if s.Total, err = domain.NewMoney(totalMinor, cur); err != nil {
			return err
		}
		if s.Lines, err = loadLines(ctx, tx, id, cur); err != nil {
			return err
		}
		order = domain.Rehydrate(s)
		return nil
	})
	return order, err
}

func loadLines(ctx context.Context, tx pgx.Tx, id domain.OrderID, currency domain.Currency) ([]domain.LineItem, error) {
	rows, err := tx.Query(ctx, `
		SELECT sku, quantity, unit_price_minor FROM ordering.order_lines
		WHERE order_id = $1 ORDER BY line_no`, string(id))
	if err != nil {
		return nil, fmt.Errorf("load lines: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.LineItem, error) {
		var (
			sku      string
			quantity int
			price    int64
		)
		if err := row.Scan(&sku, &quantity, &price); err != nil {
			return domain.LineItem{}, err
		}
		money, err := domain.NewMoney(price, currency)
		if err != nil {
			return domain.LineItem{}, err
		}
		return domain.NewLineItem(domain.SKU(sku), quantity, money)
	})
}

func (r *OrderRepository) FindByIdempotencyKey(ctx context.Context, key domain.IdempotencyKey) (domain.OrderID, string, bool, error) {
	var (
		id          string
		fingerprint string
		found       bool
	)
	err := r.db.InTenantTx(ctx, func(ctx context.Context) error {
		err := postgres.Tx(ctx).QueryRow(ctx, `
			SELECT id::text, request_fingerprint FROM ordering.orders WHERE idempotency_key = $1`, string(key)).
			Scan(&id, &fingerprint)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("find by idempotency key: %w", err)
		}
		found = true
		return nil
	})
	return domain.OrderID(id), fingerprint, found, err
}

func (r *OrderRepository) Add(ctx context.Context, order *domain.Order, key domain.IdempotencyKey, fingerprint string) error {
	return r.db.InTenantTx(ctx, func(ctx context.Context) error {
		tx := postgres.Tx(ctx)
		const version = 1
		_, err := tx.Exec(ctx, `
			INSERT INTO ordering.orders
				(tenant_id, id, customer_id, status, currency, total_minor, idempotency_key, request_fingerprint, version, placed_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
			string(order.TenantID()), string(order.ID()), string(order.CustomerID()), string(order.Status()),
			string(order.Total().Currency()), order.Total().AmountMinor(), string(key), fingerprint, version,
			order.PlacedAt(), order.UpdatedAt())
		if postgres.IsUniqueViolation(err, "orders_tenant_id_idempotency_key_key") {
			return domain.ErrDuplicateIdempotencyKey.WithCause(err)
		}
		if err != nil {
			return fmt.Errorf("insert order: %w", err)
		}
		batch := &pgx.Batch{}
		for i, line := range order.Lines() {
			batch.Queue(`
				INSERT INTO ordering.order_lines (tenant_id, order_id, line_no, sku, quantity, unit_price_minor)
				VALUES ($1, $2, $3, $4, $5, $6)`,
				string(order.TenantID()), string(order.ID()), i+1, string(line.SKU()), line.Quantity(), line.UnitPrice().AmountMinor())
		}
		if err := tx.SendBatch(ctx, batch).Close(); err != nil {
			return fmt.Errorf("insert lines: %w", err)
		}
		return r.persistEvents(ctx, order, version)
	})
}

func (r *OrderRepository) Update(ctx context.Context, order *domain.Order) error {
	return r.db.InTenantTx(ctx, func(ctx context.Context) error {
		next := order.Version() + 1
		tag, err := postgres.Tx(ctx).Exec(ctx, `
			UPDATE ordering.orders
			SET status = $1, cancellation_reason = NULLIF($2, ''), updated_at = $3, version = $4
			WHERE id = $5 AND version = $6`,
			string(order.Status()), order.CancellationReason(), order.UpdatedAt(), next, string(order.ID()), order.Version())
		if err != nil {
			return fmt.Errorf("update order: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrConcurrentModification
		}
		return r.persistEvents(ctx, order, next)
	})
}

// persistEvents writes pending events to the outbox in the same transaction.
func (r *OrderRepository) persistEvents(ctx context.Context, order *domain.Order, version int64) error {
	pending := order.Events()
	messages := make([]outbox.Message, 0, len(pending))
	for _, event := range pending {
		payload, err := events.Encode(event)
		if err != nil {
			return err
		}
		messages = append(messages, outbox.Message{
			EventType:        event.EventName(),
			AggregateType:    aggregateType,
			AggregateID:      string(order.ID()),
			AggregateVersion: version,
			Payload:          payload,
			OccurredAt:       order.UpdatedAt(),
		})
	}
	if err := outbox.Add(ctx, messages...); err != nil {
		return err
	}
	order.MarkPersisted(version)
	return nil
}
