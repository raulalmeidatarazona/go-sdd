# Agent guide

This repository is a Go API boilerplate: gRPC first with REST through grpc-gateway, DDD, CQRS, EDA (outbox/inbox over RabbitMQ), multi-tenant PostgreSQL with RLS, and OpenTelemetry. Read `README.md`, then the `docs/` document that matches the task.

## Rules

1. **Repository language is English.** Code, comments, docs, ADRs, scripts and commit messages are written in English, whatever language the conversation uses.
2. **One shape for every feature.** Each feature follows a recipe in `docs/feature-playbook.md`. If it does not fit, stop and propose an ADR in `docs/adr/` before coding.
3. **Contract first.** Change `api/proto` and run `make proto`. Never edit `gen/` by hand. Do not break `v1` (`buf breaking`).
4. **Dependencies point inwards.** `domain` imports no infrastructure, proto or SQL. Bounded contexts do not import each other.
5. **Tenant data only inside `postgres.DB.InTenantTx`**, with parameterised SQL. Every new table with `tenant_id` gets RLS with `FORCE` and a `GRANT` to `oms_app`.
6. **Messaging.** Events leave only through the outbox, from the repository. Consumers use `inbox.Handle` and version guards.
7. **Errors.** Use `apperr` with a stable code; never return a gRPC `status` from domain or application code.
8. **Handlers.** Register them with `cqrs.Observe` in `module.go`.
9. **Before handing off:** run `make check`. If persistence, messaging or the contract changed, also run `make up && make test-integration`. Never claim something passes without running it.
10. **No secrets** in code, logs, fixtures or commits. `.env` is git-ignored.

## Commands

`make proto` · `make lint` · `make test` · `make check` · `make up` · `make test-integration` · `make smoke` · `make migrate-new NAME=...`
