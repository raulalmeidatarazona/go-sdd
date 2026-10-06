# ADR 0006: Explicit SQL with pgx, no ORM

- Status: accepted
- Date: 2026-10-06

## Context

The persistence model relies on RLS, optimistic locking, `SKIP LOCKED` and keyset pagination, which ORMs hide or make awkward.

## Decision

Repositories use hand-written, parameterised SQL on pgx v5, instrumented with otelpgx. Migrations are plain SQL run by golang-migrate.

## Consequences

- Full control over query plans and transactions; queries appear verbatim in traces.
- Some mapping boilerplate. If it grows, `sqlc` (already compatible with this style) can generate it without changing the architecture.
