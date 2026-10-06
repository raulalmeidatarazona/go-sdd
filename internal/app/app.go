// Package app lists the bounded contexts of the service. Adding a context means
// implementing Module and appending it to Modules; nothing else changes.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"

	"github.com/raulalmeidatarazona/go-sdd/internal/ordering"
	"github.com/raulalmeidatarazona/go-sdd/internal/platform/postgres"
	"github.com/raulalmeidatarazona/go-sdd/internal/platform/rabbitmq"
)

// Module is the contract every bounded context fulfils.
type Module interface {
	RegisterGRPC(s *grpc.Server)
	RegisterGateway(ctx context.Context, mux *runtime.ServeMux, conn *grpc.ClientConn) error
	Subscriptions() []rabbitmq.Subscription
}

// Modules builds every bounded context.
func Modules(db *postgres.DB, logger *slog.Logger) []Module {
	return []Module{
		ordering.NewModule(db, logger),
	}
}

// RunHealthcheck implements the "healthcheck" subcommand used by Docker,
// since the distroless image has no shell or curl.
func RunHealthcheck(url string) {
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url) //nolint:noctx // one-shot CLI probe
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "unhealthy:", resp.Status)
		os.Exit(1)
	}
	os.Exit(0)
}

// ServeHTTP runs srv until ctx ends, then shuts it down gracefully.
func ServeHTTP(ctx context.Context, srv *http.Server, timeout time.Duration) error {
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
		if err := <-errCh; !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
