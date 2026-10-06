package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/raulalmeidatarazona/go-sdd/internal/ordering/application/projection"
	"github.com/raulalmeidatarazona/go-sdd/internal/ordering/application/query"
	"github.com/raulalmeidatarazona/go-sdd/internal/ordering/domain"
	"github.com/raulalmeidatarazona/go-sdd/internal/platform/postgres"
	"github.com/raulalmeidatarazona/go-sdd/internal/platform/tenancy"
)

// OrderReadModel serves queries and is updated by the projector.
type OrderReadModel struct {
	db *postgres.DB
}

func NewOrderReadModel(db *postgres.DB) *OrderReadModel {
	return &OrderReadModel{db: db}
}

var (
	_ query.ReadModel  = (*OrderReadModel)(nil)
	_ projection.Store = (*OrderReadModel)(nil)
)

const viewColumns = `id::text, customer_id, status, currency, lines, total_minor, COALESCE(cancellation_reason, ''), placed_at, updated_at, version`

func scanView(row pgx.Row) (query.OrderView, error) {
	var (
		v     query.OrderView
		lines []byte
	)
	if err := row.Scan(&v.ID, &v.CustomerID, &v.Status, &v.Currency, &lines, &v.TotalMinor, &v.CancellationReason, &v.PlacedAt, &v.UpdatedAt, &v.Version); err != nil {
		return v, err
	}
	if err := json.Unmarshal(lines, &v.Lines); err != nil {
		return v, fmt.Errorf("decode lines: %w", err)
	}
	return v, nil
}

func (m *OrderReadModel) Get(ctx context.Context, id string) (query.OrderView, error) {
	var view query.OrderView
	err := m.db.InTenantTx(ctx, func(ctx context.Context) error {
		var err error
		view, err = scanView(postgres.Tx(ctx).QueryRow(ctx, `SELECT `+viewColumns+` FROM ordering_read.order_views WHERE id = $1`, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrOrderNotFound
		}
		return err
	})
	return view, err
}

func (m *OrderReadModel) List(ctx context.Context, f query.ListFilter) ([]query.OrderView, error) {
	var (
		where []string
		args  []any
	)
	// arg appends a bind parameter and returns its placeholder; values are never interpolated.
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if f.Status != "" {
		where = append(where, "status = "+arg(f.Status))
	}
	if f.CustomerID != "" {
		where = append(where, "customer_id = "+arg(f.CustomerID))
	}
	if f.After != nil {
		where = append(where, fmt.Sprintf("(placed_at, id) < (%s, %s::uuid)", arg(f.After.PlacedAt), arg(f.After.ID)))
	}
	sql := `SELECT ` + viewColumns + ` FROM ordering_read.order_views`
	if len(where) > 0 {
		sql += " WHERE " + strings.Join(where, " AND ")
	}
	sql += " ORDER BY placed_at DESC, id DESC LIMIT " + arg(f.Limit)

	var views []query.OrderView
	err := m.db.InTenantTx(ctx, func(ctx context.Context) error {
		rows, err := postgres.Tx(ctx).Query(ctx, sql, args...)
		if err != nil {
			return fmt.Errorf("list orders: %w", err)
		}
		views, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (query.OrderView, error) { return scanView(row) })
		return err
	})
	return views, err
}

// InsertPlaced creates the view; replays of the same event are ignored.
func (m *OrderReadModel) InsertPlaced(ctx context.Context, v query.OrderView) error {
	return m.db.InTenantTx(ctx, func(ctx context.Context) error {
		tenant, err := tenancy.FromContext(ctx)
		if err != nil {
			return err
		}
		lines, err := json.Marshal(v.Lines)
		if err != nil {
			return err
		}
		_, err = postgres.Tx(ctx).Exec(ctx, `
			INSERT INTO ordering_read.order_views
				(tenant_id, id, customer_id, status, currency, lines, item_count, total_minor, placed_at, updated_at, version)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			ON CONFLICT (tenant_id, id) DO NOTHING`,
			string(tenant), v.ID, v.CustomerID, v.Status, v.Currency, lines, len(v.Lines), v.TotalMinor, v.PlacedAt, v.UpdatedAt, v.Version)
		if err != nil {
			return fmt.Errorf("insert order view: %w", err)
		}
		return nil
	})
}

// ApplyCancelled updates the view only if the event is newer than the view.
func (m *OrderReadModel) ApplyCancelled(ctx context.Context, e domain.OrderCancelled, version int64) error {
	return m.db.InTenantTx(ctx, func(ctx context.Context) error {
		tx := postgres.Tx(ctx)
		tag, err := tx.Exec(ctx, `
			UPDATE ordering_read.order_views
			SET status = $1, cancellation_reason = $2, updated_at = $3, version = $4
			WHERE id = $5 AND version < $4`,
			string(domain.StatusCancelled), e.Reason, e.CancelledAt, version, string(e.OrderID))
		if err != nil {
			return fmt.Errorf("apply cancelled: %w", err)
		}
		if tag.RowsAffected() > 0 {
			return nil
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM ordering_read.order_views WHERE id = $1)`, string(e.OrderID)).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return projection.ErrViewMissing
		}
		return nil // already at or past this version
	})
}
