// Package postgres provides the connection pool and the tenant-scoped unit of
// work. All tenant data access goes through DB.InTenantTx so row-level security
// policies see app.tenant_id; there is no API to query tenant tables without it.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/raulalmeidatarazona/go-sdd/internal/platform/tenancy"
)

type DB struct {
	pool *pgxpool.Pool
}

// Connect opens an instrumented pool and verifies connectivity.
func Connect(ctx context.Context, url string) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.ConnConfig.Tracer = otelpgx.NewTracer()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	if err := otelpgx.RecordStats(pool); err != nil {
		pool.Close()
		return nil, fmt.Errorf("record pool stats: %w", err)
	}
	return &DB{pool: pool}, nil
}

func (db *DB) Close() { db.pool.Close() }

func (db *DB) Ping(ctx context.Context) error { return db.pool.Ping(ctx) }

// Pool exposes the raw pool for cross-tenant infrastructure (outbox relay).
// Never use it for tenant data.
func (db *DB) Pool() *pgxpool.Pool { return db.pool }

type txKey struct{}

// InTenantTx runs fn in a transaction bound to the tenant in ctx. Nested calls
// join the outer transaction, so a command handler, its repository and the
// outbox write commit atomically.
func (db *DB) InTenantTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if _, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return fn(ctx)
	}
	tenant, err := tenancy.FromContext(ctx)
	if err != nil {
		return err
	}
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", string(tenant)); err != nil {
		return fmt.Errorf("set tenant: %w", err)
	}
	if err := fn(context.WithValue(ctx, txKey{}, tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// Tx returns the transaction opened by InTenantTx. Repositories call it inside
// InTenantTx; calling it elsewhere is a programming error.
func Tx(ctx context.Context) pgx.Tx {
	tx, ok := ctx.Value(txKey{}).(pgx.Tx)
	if !ok {
		panic("postgres.Tx called outside InTenantTx")
	}
	return tx
}

// IsUniqueViolation reports whether err violates the named unique constraint.
func IsUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}
