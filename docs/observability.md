# Observabilidad

## Arquitectura

```text
oms-api ─┐  OTLP/gRPC   ┌─► Jaeger      (trazas)     :16686
         ├────────────► │ OTel Collector ─► Prometheus (métricas) :9091
oms-worker┘   :4317     └─► Loki        (logs)       vía Grafana :3300
RabbitMQ ── /metrics/detailed ──► Prometheus
```

Los servicios solo hablan OTLP. Para cambiar de backend (Tempo, Mimir o un proveedor SaaS) se toca `deploy/otel-collector/config.yaml`, no el código. Sin `OTEL_EXPORTER_OTLP_ENDPOINT`, los servicios solo escriben logs JSON en stdout.

## Señales

### Trazas

Instrumentación automática:

- **HTTP**: `otelhttp` (span `POST /v1/orders`; `/healthz` y `/readyz` se excluyen);
- **gRPC**: `otelgrpc` en servidor y en el cliente del gateway;
- **PostgreSQL**: `otelpgx`, con un span por consulta (`BEGIN`, `INSERT`, `COMMIT`...);
- **Casos de uso**: `cqrs.Observe` (span `command PlaceOrder`, `query GetOrder`);
- **Mensajería**: el `traceparent` se guarda en el outbox y viaja por AMQP. El consumidor crea un span `CONSUMER`, así que **una petición y todos sus efectos asíncronos son una sola traza** en Jaeger.

### Métricas

| Métrica (Prometheus) | Tipo | Etiquetas | Origen |
|---|---|---|---|
| `rpc_server_call_duration_seconds` | histograma | `rpc_method`, `rpc_response_status_code` | otelgrpc |
| `http_server_request_duration_seconds` | histograma | `http_response_status_code` | otelhttp |
| `oms_cqrs_handler_duration_seconds` | histograma | `kind`, `handler`, `outcome` (`ok`/`rejected`/`internal_error`) | `cqrs.Observe` |
| `oms_outbox_pending` | gauge | — | relay |
| `oms_outbox_published_total` | counter | — | relay |
| `db_sql_*`, `pgxpool_*` | varios | — | otelpgx |
| `rabbitmq_detailed_queue_messages_ready` | gauge | `queue` | RabbitMQ |

Todas llevan `service_name`, `service_version` y `deployment_environment`.

### Logs

`log/slog` en JSON hacia stdout y, en paralelo, OTLP hacia Loki. Cada línea con contexto incluye `trace_id` y `span_id`. En Grafana, un log enlaza con su traza y una traza con sus logs (datasources ya provisionados).

## Dashboard

Grafana → carpeta **OMS** → **OMS overview**: tasa y p95 por RPC, respuestas no OK, resultado de cada caso de uso, backlog del outbox, mensajes por cola (incluidas DLQ) y logs.

## Qué mirar cuando...

| Síntoma | Dónde |
|---|---|
| El cliente recibe 5xx | Panel "gRPC non-OK" → Loki `{service_name="oms-api"} \|= "internal error"` → abrir la traza por `trace_id` |
| Un pedido creado no aparece en `GET` | `oms_outbox_pending` (¿el relay publica?) → cola del proyector → DLQ → logs del worker |
| La DLQ tiene mensajes | Logs del worker con `message handling failed` y el `message_id`; corregir y [reprocesar](operations.md#reprocesar-la-dlq) |
| Latencia alta | p95 por RPC → traza lenta en Jaeger → span SQL dominante |

## SLOs sugeridos para empezar

| SLI | Objetivo |
|---|---|
| Disponibilidad de comandos (respuestas no 5xx) | 99.9 % mensual |
| Latencia p95 de `PlaceOrder` | < 200 ms |
| Retraso de proyección (outbox pendiente > 0 durante más de 30 s) | < 0.1 % del tiempo |
| Mensajes en DLQ | 0; alerta con el primer mensaje |
