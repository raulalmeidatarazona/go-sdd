# Architecture Decision Records

One decision per file, numbered sequentially and never rewritten: when a decision changes, a new ADR supersedes it.

- [0001](0001-grpc-first-rest-via-gateway.md) gRPC first, REST generated with grpc-gateway
- [0002](0002-cqrs-with-eventual-consistency.md) CQRS with an event-projected read model
- [0003](0003-transactional-outbox-and-inbox.md) Transactional outbox and inbox for reliable messaging
- [0004](0004-multi-tenancy-with-rls.md) Multi-tenancy with a shared database and row-level security
- [0005](0005-rabbitmq-quorum-queues-and-crash-only.md) RabbitMQ quorum queues, DLQs and a crash-only client
- [0006](0006-explicit-sql-with-pgx.md) Explicit SQL with pgx, no ORM
- [0007](0007-observability-with-opentelemetry.md) OpenTelemetry with a collector as the single telemetry exit

Template: copy any ADR and keep the Context, Decision and Consequences sections.
