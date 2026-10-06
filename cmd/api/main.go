// Command api serves the gRPC API and its REST gateway. It writes to
// PostgreSQL only; events leave through the outbox, relayed by cmd/worker.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/raulalmeidatarazona/go-sdd/gen/openapi"
	"github.com/raulalmeidatarazona/go-sdd/internal/app"
	"github.com/raulalmeidatarazona/go-sdd/internal/platform/config"
	"github.com/raulalmeidatarazona/go-sdd/internal/platform/gateway"
	"github.com/raulalmeidatarazona/go-sdd/internal/platform/grpcx"
	"github.com/raulalmeidatarazona/go-sdd/internal/platform/postgres"
	"github.com/raulalmeidatarazona/go-sdd/internal/platform/telemetry"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		app.RunHealthcheck("http://127.0.0.1" + envOr("HTTP_ADDR", ":8080") + "/readyz")
	}
	if err := run(); err != nil {
		slog.Error("api stopped", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load("oms-api")
	if err != nil {
		return err
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

	modules := app.Modules(db, logger)

	grpcServer, health := grpcx.NewServer(logger, grpcx.HeaderTenantResolver)
	for _, m := range modules {
		m.RegisterGRPC(grpcServer)
	}

	conn, err := gateway.DialLocal(loopback(cfg.GRPCAddr))
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	registers := make([]gateway.Register, 0, len(modules))
	for _, m := range modules {
		registers = append(registers, m.RegisterGateway)
	}
	handler, err := gateway.New(ctx, conn, openapi.Spec, map[string]gateway.ReadinessCheck{"postgres": db.Ping}, registers...)
	if err != nil {
		return err
	}
	httpServer := &http.Server{Addr: cfg.HTTPAddr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}

	listener, err := new(net.ListenConfig).Listen(ctx, "tcp", cfg.GRPCAddr)
	if err != nil {
		return err
	}

	group, ctx := errgroup.WithContext(ctx)
	group.Go(func() error {
		logger.Info("grpc listening", slog.String("addr", cfg.GRPCAddr))
		return grpcServer.Serve(listener)
	})
	group.Go(func() error {
		logger.Info("http gateway listening", slog.String("addr", cfg.HTTPAddr))
		return app.ServeHTTP(ctx, httpServer, cfg.ShutdownTimeout)
	})
	group.Go(func() error {
		<-ctx.Done()
		// Fail readiness first so load balancers drain, then stop accepting work.
		health.Shutdown()
		stopped := make(chan struct{})
		go func() { grpcServer.GracefulStop(); close(stopped) }()
		select {
		case <-stopped:
		case <-time.After(cfg.ShutdownTimeout):
			grpcServer.Stop()
		}
		return nil
	})
	health.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

	if err := group.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	logger.Info("api stopped cleanly")
	return nil
}

// loopback turns ":9090" into "localhost:9090" for the in-process gateway client.
func loopback(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || host == "" || host == "0.0.0.0" {
		return "localhost:" + port
	}
	return addr
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
