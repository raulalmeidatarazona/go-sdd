// Package grpcx builds the gRPC server with the interceptor chain every service
// shares: recovery, tenant resolution and error mapping.
package grpcx

import (
	"context"
	"errors"
	"log/slog"
	"runtime/debug"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	"github.com/raulalmeidatarazona/go-sdd/internal/platform/apperr"
	"github.com/raulalmeidatarazona/go-sdd/internal/platform/tenancy"
)

// TenantMetadataKey is the gRPC metadata key (and HTTP header) carrying the tenant.
const TenantMetadataKey = "x-tenant-id"

// ErrorDomain is reported in google.rpc.ErrorInfo so clients can switch on Reason.
const ErrorDomain = "oms"

// TenantResolver extracts the tenant of an incoming call. The default reads a
// header and is meant for trusted networks; production deployments plug in a
// resolver that reads a verified JWT claim (see docs/multitenancy.md).
type TenantResolver func(ctx context.Context) (tenancy.ID, error)

// HeaderTenantResolver trusts the x-tenant-id metadata.
func HeaderTenantResolver(ctx context.Context) (tenancy.ID, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	values := md.Get(TenantMetadataKey)
	if len(values) == 0 {
		return "", tenancy.ErrMissing
	}
	return tenancy.Parse(values[0])
}

// NewServer returns a server with health and reflection registered.
func NewServer(logger *slog.Logger, resolve TenantResolver) (*grpc.Server, *health.Server) {
	server := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.ChainUnaryInterceptor(
			recoveryInterceptor(logger),
			tenantInterceptor(resolve),
			errorInterceptor(logger),
		),
	)
	healthServer := health.NewServer()
	healthpb.RegisterHealthServer(server, healthServer)
	reflection.Register(server)
	return server, healthServer
}

// Methods outside business services (health, reflection) do not need a tenant.
func requiresTenant(fullMethod string) bool {
	return !strings.HasPrefix(fullMethod, "/grpc.health.") && !strings.HasPrefix(fullMethod, "/grpc.reflection.")
}

func tenantInterceptor(resolve TenantResolver) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if !requiresTenant(info.FullMethod) {
			return handler(ctx, req)
		}
		tenant, err := resolve(ctx)
		if err != nil {
			return nil, ToStatus(err)
		}
		return handler(tenancy.WithTenant(ctx, tenant), req)
	}
}

func errorInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		resp, err := handler(ctx, req)
		if err == nil {
			return resp, nil
		}
		if _, ok := status.FromError(err); ok {
			return resp, err
		}
		if apperr.KindOf(err) == apperr.KindInternal {
			logger.ErrorContext(ctx, "internal error", slog.String("method", info.FullMethod), slog.Any("error", err))
		}
		return resp, ToStatus(err)
	}
}

func recoveryInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				logger.ErrorContext(ctx, "panic recovered", slog.String("method", info.FullMethod), slog.Any("panic", r), slog.String("stack", string(debug.Stack())))
				err = status.Error(codes.Internal, "internal error")
			}
		}()
		return handler(ctx, req)
	}
}

// ToStatus maps an application error to a gRPC status. Internal errors never
// leak their message to clients.
func ToStatus(err error) error {
	var appErr *apperr.Error
	if !errors.As(err, &appErr) {
		return status.Error(codes.Internal, "internal error")
	}
	code := codes.Internal
	switch appErr.Kind {
	case apperr.KindInvalid:
		code = codes.InvalidArgument
	case apperr.KindNotFound:
		code = codes.NotFound
	case apperr.KindConflict:
		code = codes.AlreadyExists
	case apperr.KindFailedPrecondition:
		code = codes.FailedPrecondition
	case apperr.KindAborted:
		code = codes.Aborted
	case apperr.KindUnauthenticated:
		code = codes.Unauthenticated
	case apperr.KindInternal:
		return status.Error(codes.Internal, "internal error")
	}
	st, detailErr := status.New(code, appErr.Message).WithDetails(&errdetails.ErrorInfo{Reason: appErr.Code, Domain: ErrorDomain})
	if detailErr != nil {
		return status.Error(code, appErr.Message)
	}
	return st.Err()
}
