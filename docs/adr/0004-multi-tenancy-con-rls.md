# ADR 0004: Multi-tenancy con base de datos compartida y row-level security

- Estado: aceptada
- Fecha: 2026-10-06

## Contexto

El OMS sirve a muchas empresas. Un fallo de filtrado por tenant en una sola consulta sería una fuga de datos grave.

## Decisión

Tablas compartidas con `tenant_id`, políticas RLS con `FORCE` y un rol de aplicación `NOBYPASSRLS`. El tenant se fija por transacción con `set_config('app.tenant_id', ..., true)` dentro de `InTenantTx`, la única vía de acceso a datos de tenant.

## Consecuencias

- El aislamiento lo impone PostgreSQL, no solo la disciplina del código.
- Todas las claves primarias empiezan por `tenant_id`, lo que da buena localidad en los índices.
- Cada acceso es transaccional; las consultas analíticas entre tenants requieren un rol distinto.
- Un tenant muy grande puede moverse a su propia base de datos sin cambiar el código de dominio.
