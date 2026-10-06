# Feature playbook

Every feature has the same shape, in the same order, in the same places. If a change does not fit one of these recipes, it is an architecture decision: write an ADR before coding it.

The recipes use adding **ConfirmOrder** (moving an order from `PLACED` to `CONFIRMED`) as the running example.

## Recipe 1 — New command (write)

1. **Contract** — `api/proto/oms/order/v1/order_service.proto`
   - Add `rpc ConfirmOrder(ConfirmOrderRequest) returns (ConfirmOrderResponse)` with its `google.api.http` annotation. Actions use `post: "/v1/orders/{order_id}:confirm"` and `body: "*"`.
   - Add the Request and Response messages. A command response carries identifiers only, never the resource.
   - If the state change matters outside this context, add an `OrderConfirmed` event to `order_events.proto`.
   - Run `make proto`. `buf lint` and `buf breaking` validate the contract.
2. **Domain** — `internal/ordering/domain/`
   - Add `StatusConfirmed`, the `OrderConfirmed` event whose `EventName()` equals the proto message name, and its errors with a **stable code** in `errors.go`.
   - Add `func (o *Order) Confirm(now time.Time) error`: check invariants first, then change state, then call `o.Record(...)`.
   - Add the aggregate test in `order_test.go`.
3. **Persistence** — if the new state needs columns, create a migration with `make migrate-new NAME=order_confirmation`. Update the `status` `CHECK` constraint and the repository's `Update`. The outbox needs no change: it stores any event.
4. **Codec** — `infrastructure/events/codec.go`: add the new event's `case` to both `Encode` and `Decode`.
5. **Use case** — create `application/command/confirm_order.go` with exactly this shape:
   ```go
   type ConfirmOrder struct{ OrderID string }
   type ConfirmOrderResult struct{}
   type ConfirmOrderHandler struct{ uow UnitOfWork; orders domain.Repository; now func() time.Time }
   func NewConfirmOrderHandler(...) *ConfirmOrderHandler
   func (h *ConfirmOrderHandler) Handle(ctx context.Context, cmd ConfirmOrder) (ConfirmOrderResult, error) {
       // 1. tenant  2. parse into value objects  3. uow{ Get → behaviour → Update }
   }
   ```
   Add its test to `commands_test.go` using the in-memory repository.
6. **Transport** — `transport/grpcapi/order_server.go`: add the field to `Handlers` and the RPC method. The method only maps between proto and the command; no business rules.
7. **Composition** — `module.go`: register it with `cqrs.Observe(cqrs.KindCommand, "ConfirmOrder", logger, command.NewConfirmOrderHandler(uow, repository, now))`. It inherits traces, metrics and logs with no extra code.
8. **Projection** — if the view changes: add the `case` to `projection/order_projector.go`, a method to `projection.Store`, and its implementation with a **version guard** (`WHERE version < $n`) in `order_read_model.go`.
9. **End-to-end tests** — add the case to `test/e2e`, then run `make check` and `make test-integration`.
10. **Docs** — update `docs/api.md` if a convention changes, and add the sample request to `api/http/orders.http`.

## Recipe 2 — New query (read)

1. Contract: `rpc GetX(GetXRequest) returns (GetXResponse)` with `get:`, then `make proto`.
2. If the current view lacks the data, extend the projection (recipe 4) instead of reading the write model.
3. Create `application/query/get_x.go`: a query struct, a handler that receives the `ReadModel` port, and `Handle` that resolves the tenant, validates and reads.
4. Implement the query in `infrastructure/postgres/order_read_model.go` inside `InTenantTx`, always with bind parameters (`$1`), never string concatenation.
5. Add the gRPC method in `grpcapi` and register it in `module.go` with `cqrs.Observe(cqrs.KindQuery, ...)`.
6. Test the handler with a fake `ReadModel` and add the e2e case.

## Recipe 3 — New event consumer

1. Decide who owns the queue: always the context that **reacts**, never the publisher.
2. In that context's `module.go`, add a `rabbitmq.Subscription` with:
   - `Queue: "oms.<context>.<purpose>"`, which must stay stable because renaming it creates a new queue;
   - `RoutingKeys`, with event names or wildcards (`oms.order.v1.*`);
   - `Handle: inbox.Handle(db, <queue>, fn)`. The inbox is mandatory because delivery is *at-least-once*.
3. Make `fn` idempotent and version-guarded. If a message arrives too early, return an error: it is retried and, after `DeliveryLimit` attempts, moved to `<queue>.dlq`.
4. The worker starts it automatically; `cmd/worker` does not change.

## Recipe 4 — Change a projection

1. Create a migration in `ordering_read` with the new columns or table.
2. If the change needs historical data, **rebuild the projection**: empty the table and replay the outbox (runbook in [operations.md](operations.md#rebuild-a-projection)).
3. Every update keeps its version guard.

## Recipe 5 — New bounded context

Copy the structure of `internal/ordering` and rename:

```text
internal/<context>/
  domain/                 aggregate(s), value objects, events, errors, Repository
  application/command/    one file per command
  application/query/      one file per query + read_model.go
  application/projection/ if it has its own read model
  infrastructure/postgres and infrastructure/events
  transport/grpcapi/
  module.go               NewModule, RegisterGRPC, RegisterGateway, Subscriptions
```

Then:

1. Create `api/proto/oms/<context>/v1/` with its service and events.
2. Create a migration with dedicated `<context>` and `<context>_read` schemas, RLS on every table with `tenant_id`, and `GRANT`s to `oms_app`.
3. Add `NewModule` to `internal/app/app.go`. It is the **only** line touched outside the context.
4. A context **never imports** another context's packages. Contexts talk through events or their gRPC API.

## Review checklist

- [ ] The proto contract changed first and `buf breaking` passes.
- [ ] Business rules live in the domain and are tested.
- [ ] The handler follows the shape: tenant → parse → unit of work → result with identifiers.
- [ ] The handler is registered through `cqrs.Observe`.
- [ ] Errors are `apperr` values with a stable, documented code.
- [ ] All SQL runs in `InTenantTx`, with bind parameters, and new tables are under RLS.
- [ ] Events are backward compatible; consumers are idempotent and version-guarded.
- [ ] `make check` and `make test-integration` pass.
