// Package ordering is the composition root of the ordering bounded context.
// Every bounded context exposes the same Module shape so cmd/api and
// cmd/worker wire them identically.
package ordering

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"

	orderv1 "github.com/raulalmeidatarazona/go-sdd/gen/go/oms/order/v1"
	"github.com/raulalmeidatarazona/go-sdd/internal/ordering/application/command"
	"github.com/raulalmeidatarazona/go-sdd/internal/ordering/application/projection"
	"github.com/raulalmeidatarazona/go-sdd/internal/ordering/application/query"
	"github.com/raulalmeidatarazona/go-sdd/internal/ordering/infrastructure/events"
	orderpg "github.com/raulalmeidatarazona/go-sdd/internal/ordering/infrastructure/postgres"
	"github.com/raulalmeidatarazona/go-sdd/internal/ordering/transport/grpcapi"
	"github.com/raulalmeidatarazona/go-sdd/internal/platform/cqrs"
	"github.com/raulalmeidatarazona/go-sdd/internal/platform/inbox"
	"github.com/raulalmeidatarazona/go-sdd/internal/platform/postgres"
	"github.com/raulalmeidatarazona/go-sdd/internal/platform/rabbitmq"
)

const projectorQueue = "oms.ordering.order-projector"

type Module struct {
	db        *postgres.DB
	logger    *slog.Logger
	server    *grpcapi.OrderServer
	projector *projection.OrderProjector
}

func NewModule(db *postgres.DB, logger *slog.Logger) *Module {
	logger = logger.With(slog.String("module", "ordering"))
	repository := orderpg.NewOrderRepository(db)
	readModel := orderpg.NewOrderReadModel(db)
	uow := command.UnitOfWork(db.InTenantTx)
	now := time.Now

	handlers := grpcapi.Handlers{
		PlaceOrder:  cqrs.Observe(cqrs.KindCommand, "PlaceOrder", logger, command.NewPlaceOrderHandler(uow, repository, now)),
		CancelOrder: cqrs.Observe(cqrs.KindCommand, "CancelOrder", logger, command.NewCancelOrderHandler(uow, repository, now)),
		GetOrder:    cqrs.Observe(cqrs.KindQuery, "GetOrder", logger, query.NewGetOrderHandler(readModel)),
		ListOrders:  cqrs.Observe(cqrs.KindQuery, "ListOrders", logger, query.NewListOrdersHandler(readModel)),
	}
	return &Module{
		db:        db,
		logger:    logger,
		server:    grpcapi.NewOrderServer(handlers),
		projector: projection.NewOrderProjector(readModel),
	}
}

// RegisterGRPC exposes the service on the gRPC server.
func (m *Module) RegisterGRPC(s *grpc.Server) {
	orderv1.RegisterOrderServiceServer(s, m.server)
}

// RegisterGateway exposes the same service as REST, transcoded from gRPC.
func (m *Module) RegisterGateway(ctx context.Context, mux *runtime.ServeMux, conn *grpc.ClientConn) error {
	return orderv1.RegisterOrderServiceHandler(ctx, mux, conn)
}

// Subscriptions are the event consumers this context runs in the worker.
func (m *Module) Subscriptions() []rabbitmq.Subscription {
	return []rabbitmq.Subscription{{
		Queue:         projectorQueue,
		RoutingKeys:   []string{"oms.order.v1.*"},
		DeliveryLimit: 5,
		Handle: inbox.Handle(m.db, projectorQueue, func(ctx context.Context, msg rabbitmq.Message) error {
			event, err := events.Decode(msg.Type, msg.Body)
			if err != nil {
				return fmt.Errorf("decode %s: %w", msg.ID, err)
			}
			return m.projector.Project(ctx, projection.Envelope{AggregateVersion: msg.AggregateVersion, Event: event})
		}),
	}}
}
