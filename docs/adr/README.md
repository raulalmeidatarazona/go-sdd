# Architecture Decision Records

Una decisión por fichero, numeración correlativa y nunca se reescribe: si cambia, se crea otro ADR que la sustituye.

- [0001](0001-grpc-first-rest-por-gateway.md) gRPC first y REST generado con grpc-gateway
- [0002](0002-cqrs-con-consistencia-eventual.md) CQRS con modelo de lectura proyectado por eventos
- [0003](0003-transactional-outbox-e-inbox.md) Transactional outbox e inbox para mensajería fiable
- [0004](0004-multi-tenancy-con-rls.md) Multi-tenancy con base de datos compartida y row-level security
- [0005](0005-rabbitmq-quorum-y-crash-only.md) RabbitMQ con colas quorum, DLQ y cliente crash-only
- [0006](0006-sql-explicito-con-pgx.md) SQL explícito con pgx, sin ORM
- [0007](0007-observabilidad-con-opentelemetry.md) OpenTelemetry y un collector como única salida de telemetría

Plantilla: copia cualquier ADR y mantén las secciones Contexto, Decisión y Consecuencias.
