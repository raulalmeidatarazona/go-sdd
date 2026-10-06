package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/raulalmeidatarazona/go-sdd/internal/platform/rabbitmq"
)

// Publisher is the subset of the bus the relay needs.
type Publisher interface {
	Publish(ctx context.Context, m rabbitmq.Message) error
}

// Relay moves committed outbox rows to the broker. Rows are claimed with
// FOR UPDATE SKIP LOCKED, so several relays can run without double work; a
// crash between publish and commit republishes, which the inbox absorbs.
type Relay struct {
	pool      *pgxpool.Pool
	publisher Publisher
	logger    *slog.Logger
	interval  time.Duration
	batchSize int
	published metric.Int64Counter
}

func NewRelay(pool *pgxpool.Pool, publisher Publisher, logger *slog.Logger, interval time.Duration, batchSize int) (*Relay, error) {
	meter := otel.Meter("github.com/raulalmeidatarazona/go-sdd/internal/platform/outbox")
	published, err := meter.Int64Counter("oms.outbox.published", metric.WithDescription("Outbox messages published to the broker"))
	if err != nil {
		return nil, err
	}
	pending, err := meter.Int64ObservableGauge("oms.outbox.pending", metric.WithDescription("Outbox messages waiting to be published"))
	if err != nil {
		return nil, err
	}
	_, err = meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		var n int64
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM messaging.outbox WHERE published_at IS NULL`).Scan(&n); err != nil {
			return err
		}
		o.ObserveInt64(pending, n)
		return nil
	}, pending)
	if err != nil {
		return nil, err
	}
	return &Relay{pool: pool, publisher: publisher, logger: logger, interval: interval, batchSize: batchSize, published: published}, nil
}

// Run polls until ctx is cancelled. A full batch triggers an immediate next poll.
func (r *Relay) Run(ctx context.Context) error {
	r.logger.Info("outbox relay started", slog.Duration("interval", r.interval))
	for {
		n, err := r.publishBatch(ctx)
		if err != nil && ctx.Err() == nil {
			r.logger.ErrorContext(ctx, "outbox relay batch failed", slog.Any("error", err))
		}
		if n == r.batchSize && err == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(r.interval):
		}
	}
}

func (r *Relay) publishBatch(ctx context.Context) (int, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	rows, err := tx.Query(ctx, `
		SELECT seq, message_id::text, tenant_id::text, event_type, aggregate_id::text, aggregate_version, payload, headers, occurred_at
		FROM messaging.outbox
		WHERE published_at IS NULL
		ORDER BY seq
		LIMIT $1
		FOR UPDATE SKIP LOCKED`, r.batchSize)
	if err != nil {
		return 0, err
	}
	type row struct {
		seq     int64
		message rabbitmq.Message
	}
	batch, err := pgx.CollectRows(rows, func(rs pgx.CollectableRow) (row, error) {
		var out row
		var headers []byte
		err := rs.Scan(&out.seq, &out.message.ID, &out.message.TenantID, &out.message.Type, &out.message.AggregateID,
			&out.message.AggregateVersion, &out.message.Body, &headers, &out.message.Timestamp)
		if err != nil {
			return out, err
		}
		if err := json.Unmarshal(headers, &out.message.TraceHeaders); err != nil {
			return out, fmt.Errorf("decode headers of %d: %w", out.seq, err)
		}
		return out, nil
	})
	if err != nil || len(batch) == 0 {
		return 0, err
	}

	published := make([]int64, 0, len(batch))
	for _, b := range batch {
		if err := r.publisher.Publish(ctx, b.message); err != nil {
			// Stop at the first failure to keep per-aggregate order; mark what succeeded.
			r.logger.ErrorContext(ctx, "publish failed", slog.String("message_id", b.message.ID), slog.Any("error", err))
			break
		}
		published = append(published, b.seq)
	}
	if len(published) > 0 {
		if _, err := tx.Exec(ctx, `UPDATE messaging.outbox SET published_at = now() WHERE seq = ANY($1)`, published); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	r.published.Add(ctx, int64(len(published)))
	return len(published), nil
}
