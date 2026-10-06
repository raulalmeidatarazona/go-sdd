# ADR 0004: Multi-tenancy with a shared database and row-level security

- Status: accepted
- Date: 2026-10-06

## Context

The OMS serves many companies. A missing tenant filter in a single query would be a serious data leak.

## Decision

Shared tables with `tenant_id`, RLS policies with `FORCE`, and a `NOBYPASSRLS` application role. The tenant is set per transaction with `set_config('app.tenant_id', ..., true)` inside `InTenantTx`, the only path to tenant data.

## Consequences

- PostgreSQL enforces isolation, not just coding discipline.
- Every primary key starts with `tenant_id`, which gives good index locality.
- Every access is transactional; cross-tenant analytics need a separate role.
- A very large tenant can move to its own database without domain code changes.
