# Architecture

## Layers and the dependency rule

Hexagonal architecture per bounded context. Dependencies only point inwards:

```text
transport/grpcapi ─┐
infrastructure/* ──┼──► application/{command,query,projection} ──► domain ──► platform/{ddd,apperr,tenancy}
module.go ─────────┘
```

| Layer | Contains | Must not |
|---|---|---|
| `domain` | Aggregates, value objects, events, errors and ports (`Repository`) | Import SQL, gRPC, proto or RabbitMQ |
| `application` | One handler per use case; read and projection ports | Know Postgres or proto; choose HTTP or gRPC codes |
| `infrastructure` | Adapters: Postgres and the protobuf event codec | Contain business rules |
| `transport` | Mapping proto ↔ commands and queries | Contain business rules or touch the database |
| `module.go` | Composition: builds adapters, handlers and subscriptions | — |
| `internal/platform` | Cross-cutting infrastructure with no business logic | Import any bounded context |

Bounded contexts do not import each other. They communicate through integration events (protobuf over RabbitMQ) or their gRPC API.

## Command flow

```mermaid
sequenceDiagram
  participant Client
  participant GW as grpc-gateway (:8080)
  participant S as gRPC (:9090)
  participant H as PlaceOrderHandler
  participant DB as PostgreSQL
  Client->>GW: POST /v1/orders + X-Tenant-Id
  GW->>S: PlaceOrder (metadata x-tenant-id)
  S->>S: interceptors: recovery → tenant → errors
  S->>H: cqrs.Observe (span + metric + log)
  H->>DB: BEGIN; set_config('app.tenant_id')
  H->>DB: idempotency lookup
  H->>DB: INSERT orders, order_lines, outbox
  H->>DB: COMMIT
  S-->>Client: { orderId }
```

## Event flow

```mermaid
sequenceDiagram
  participant Relay as Outbox relay (worker)
  participant DB as PostgreSQL
  participant MQ as RabbitMQ
  participant P as Projector (worker)
  Relay->>DB: SELECT ... FOR UPDATE SKIP LOCKED
  Relay->>MQ: publish (routing key = oms.order.v1.OrderPlaced), await confirm
  Relay->>DB: UPDATE published_at; COMMIT
  MQ->>P: deliver (headers: tenant, version, traceparent)
  P->>DB: BEGIN (tenant); INSERT inbox ON CONFLICT DO NOTHING
  P->>DB: version-guarded view upsert; COMMIT
  P->>MQ: ack (or nack → retry → DLQ)
```

The trace is **a single trace** end to end: the `traceparent` is stored in the outbox and travels in the AMQP headers.

## Processes

| Process | Responsibility | Scaling |
|---|---|---|
| `oms-api` | gRPC + REST; writes to Postgres only | Horizontal, stateless |
| `oms-worker` | Outbox relay + consumers | Horizontal; the relay shares work with `SKIP LOCKED` and quorum queues share messages |

The API does not depend on RabbitMQ: if the broker is down, orders are still accepted and their events wait in the outbox. The worker is *crash-only*: if it loses the broker it exits and Docker or Kubernetes restarts it.

## Error model

Domain and application code return `apperr.Error{Kind, Code, Message}`. A single interceptor (`grpcx.ToStatus`) maps `Kind` to a gRPC code and attaches `google.rpc.ErrorInfo{reason: Code, domain: "oms"}`. grpc-gateway maps the gRPC code to HTTP. Internal errors never expose their message.

| Kind | gRPC | HTTP |
|---|---|---|
| Invalid | INVALID_ARGUMENT | 400 |
| Unauthenticated | UNAUTHENTICATED | 401 |
| NotFound | NOT_FOUND | 404 |
| Conflict | ALREADY_EXISTS | 409 |
| Aborted | ABORTED | 409 |
| FailedPrecondition | FAILED_PRECONDITION | 400 |
| Internal | INTERNAL | 500 |

## Decisions

The rationale for each choice lives in the [ADRs](adr/).
