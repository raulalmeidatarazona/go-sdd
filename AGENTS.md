# Guía para agentes

Este repositorio es un boilerplate de APIs Go: gRPC first con REST por grpc-gateway, DDD, CQRS, EDA (outbox/inbox con RabbitMQ), PostgreSQL multi-tenant con RLS y OpenTelemetry. Lee `README.md` y, según la tarea, el documento de `docs/` que corresponda.

## Reglas

1. **Sota, caballo y rey.** Toda funcionalidad sigue una receta de `docs/sota-caballo-rey.md`. Si no encaja, para y propón un ADR en `docs/adr/` antes de programar.
2. **El contrato va primero.** Cambia `api/proto` y ejecuta `make proto`. Nunca edites `gen/` a mano. No rompas `v1` (`buf breaking`).
3. **Dependencias hacia dentro.** `domain` no importa infraestructura, proto ni SQL. Los bounded contexts no se importan entre sí.
4. **Datos de tenant solo dentro de `postgres.DB.InTenantTx`**, con SQL parametrizado. Toda tabla nueva con `tenant_id` lleva RLS con `FORCE` y `GRANT` a `oms_app`.
5. **Mensajería.** Los eventos salen solo por el outbox, desde el repositorio. Los consumidores usan `inbox.Handle` y guardas de versión.
6. **Errores.** Usa `apperr` con un código estable; nunca devuelvas `status` gRPC desde dominio ni aplicación.
7. **Handlers.** Regístralos con `cqrs.Observe` en `module.go`.
8. **Antes de entregar:** `make check`. Si tocaste persistencia, mensajería o el contrato, también `make up && make test-integration`. No digas que algo pasa sin haberlo ejecutado.
9. **Sin secretos** en código, logs, fixtures ni commits. `.env` está en `.gitignore`.

## Comandos

`make proto` · `make lint` · `make test` · `make check` · `make up` · `make test-integration` · `make smoke` · `make migrate-new NAME=...`
