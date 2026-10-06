# ADR 0006: SQL explícito con pgx, sin ORM

- Estado: aceptada
- Fecha: 2026-10-06

## Contexto

El modelo de persistencia incluye RLS, bloqueo optimista, `SKIP LOCKED` y paginación por keyset, que los ORM ocultan o dificultan.

## Decisión

Repositorios con SQL parametrizado escrito a mano sobre pgx v5, instrumentado con otelpgx. Migraciones con golang-migrate en SQL plano.

## Consecuencias

- Control total del plan de consulta y de las transacciones; las consultas aparecen tal cual en las trazas.
- Algo más de código de mapeo. Si crece, `sqlc` (ya compatible con este estilo) puede generarlo sin cambiar la arquitectura.
