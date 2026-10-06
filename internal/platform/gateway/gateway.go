// Package gateway exposes the gRPC services as REST/JSON through grpc-gateway.
// The HTTP routes come from google.api.http annotations in the .proto files,
// so REST is a projection of the gRPC contract, never a separate API.
package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/raulalmeidatarazona/go-sdd/internal/platform/grpcx"
)

// Register adds one service's REST handlers to the mux.
type Register func(ctx context.Context, mux *runtime.ServeMux, conn *grpc.ClientConn) error

// ReadinessCheck reports whether a dependency is usable.
type ReadinessCheck func(ctx context.Context) error

// forwardedHeaders are copied from HTTP into gRPC metadata.
var forwardedHeaders = map[string]bool{
	grpcx.TenantMetadataKey: true,
	"x-request-id":          true,
}

// New builds the HTTP handler: REST routes, /healthz, /readyz and /openapi.json.
func New(ctx context.Context, grpcConn *grpc.ClientConn, openAPI []byte, readiness map[string]ReadinessCheck, registers ...Register) (http.Handler, error) {
	mux := runtime.NewServeMux(
		runtime.WithIncomingHeaderMatcher(func(key string) (string, bool) {
			lower := strings.ToLower(key)
			if forwardedHeaders[lower] {
				return lower, true
			}
			return runtime.DefaultHeaderMatcher(key)
		}),
		runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{
			MarshalOptions:   protojson.MarshalOptions{EmitUnpopulated: true},
			UnmarshalOptions: protojson.UnmarshalOptions{DiscardUnknown: false},
		}),
	)
	for _, register := range registers {
		if err := register(ctx, mux, grpcConn); err != nil {
			return nil, err
		}
	}

	root := http.NewServeMux()
	root.Handle("/", mux)
	root.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	root.HandleFunc("GET /readyz", readyHandler(readiness))
	root.HandleFunc("GET /openapi.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		_, _ = w.Write(openAPI)
	})
	return otelhttp.NewHandler(root, "http",
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string { return r.Method + " " + r.URL.Path }),
		otelhttp.WithFilter(func(r *http.Request) bool { return r.URL.Path != "/healthz" && r.URL.Path != "/readyz" }),
	), nil
}

// DialLocal connects the gateway to the in-process gRPC server.
func DialLocal(target string) (*grpc.ClientConn, error) {
	return grpc.NewClient(target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
}

func readyHandler(checks map[string]ReadinessCheck) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		result := map[string]string{}
		healthy := true
		for name, check := range checks {
			if err := check(ctx); err != nil {
				result[name] = err.Error()
				healthy = false
				continue
			}
			result[name] = "ok"
		}
		w.Header().Set("Content-Type", "application/json")
		if !healthy {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(result)
	}
}
