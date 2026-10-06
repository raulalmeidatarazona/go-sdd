// Command worker runs the event-driven side: the outbox relay (PostgreSQL ->
// RabbitMQ) and every module's subscriptions (RabbitMQ -> projections).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/raulalmeidatarazona/go-sdd/internal/app"
	"github.com/raulalmeidatarazona/go-sdd/internal/platform/config"
	"github.com/raulalmeidatarazona/go-sdd/internal/platform/outbox"
	"github.com/raulalmeidatarazona/go-sdd/internal/platform/postgres"
	"github.com/raulalmeidatarazona/go-sdd/internal/platform/rabbitmq"
	"github.com/raulalmeidatarazona/go-sdd/internal/platform/telemetry"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		addr := os.Getenv("WORKER_HTTP_ADDR")
		if addr == "" {
			addr = ":8081"
		}
		app.RunHealthcheck("http://127.0.0.1" + addr + "/readyz")
	}
	if err := run(); err != nil {
		slog.Error("worker stopped", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load("oms-worker")
	if err != nil {
		return err
	}
	if cfg.RabbitMQURL == "" {
		return errors.New("RABBITMQ_URL is required")
	}
	logger, shutdownTelemetry, err := telemetry.Setup(ctx, cfg)
	if err != nil {
		return fmt.Errorf("telemetry: %w", err)
	}
	defer func() {
		flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownTelemetry(flushCtx)
	}()
	slog.SetDefault(logger)

	db, err := postgres.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	bus, err := rabbitmq.Dial(cfg.RabbitMQURL)
	if err != nil {
		return err
	}
	defer func() { _ = bus.Close() }()

	publisher, err := bus.NewPublisher()
	if err != nil {
		return err
	}
	relay, err := outbox.NewRelay(db.Pool(), publisher, logger, cfg.OutboxPollInterval, cfg.OutboxBatchSize)
	if err != nil {
		return err
	}

	group, ctx := errgroup.WithContext(ctx)
	group.Go(func() error { return relay.Run(ctx) })
	for _, m := range app.Modules(db, logger) {
		for _, sub := range m.Subscriptions() {
			group.Go(func() error { return bus.Consume(ctx, logger, sub, cfg.ConsumerPrefetch) })
		}
	}
	group.Go(func() error {
		// Crash-only: a lost broker connection stops the process; the orchestrator restarts it.
		select {
		case <-ctx.Done():
			return nil
		case amqpErr := <-bus.Closed():
			return fmt.Errorf("rabbitmq connection lost: %w", amqpErr)
		}
	})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := db.Ping(r.Context()); err != nil || !bus.Healthy() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	probeServer := &http.Server{Addr: cfg.WorkerHTTPAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	group.Go(func() error { return app.ServeHTTP(ctx, probeServer, cfg.ShutdownTimeout) })

	logger.Info("worker started")
	if err := group.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	logger.Info("worker stopped cleanly")
	return nil
}
