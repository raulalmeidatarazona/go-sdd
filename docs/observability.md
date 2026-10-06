# Observability

## Architecture

```text
oms-api ──┐  OTLP/gRPC   ┌─► Jaeger     (traces)   :16686
          ├────────────► │ OTel Collector ─► Prometheus (metrics) :9091
oms-worker┘    :4317     └─► Loki       (logs)     via Grafana :3300
RabbitMQ ── /metrics/detailed ──► Prometheus
```

Services only speak OTLP. Switching backends (Tempo, Mimir, a SaaS vendor) means editing `deploy/otel-collector/config.yaml`, not the code. Without `OTEL_EXPORTER_OTLP_ENDPOINT`, services only write JSON logs to stdout.

## Signals

### Traces

Automatic instrumentation:

- **HTTP**: `otelhttp` (span `POST /v1/orders`; `/healthz` and `/readyz` are excluded);
- **gRPC**: `otelgrpc` on the server and on the gateway's client;
- **PostgreSQL**: `otelpgx`, one span per statement (`BEGIN`, `INSERT`, `COMMIT`...);
- **Use cases**: `cqrs.Observe` (spans `command PlaceOrder`, `query GetOrder`);
- **Messaging**: the `traceparent` is stored in the outbox and travels over AMQP. The consumer opens a `CONSUMER` span, so **a request and all its asynchronous effects are one trace** in Jaeger.

### Metrics

| Metric (Prometheus) | Type | Labels | Source |
|---|---|---|---|
| `rpc_server_call_duration_seconds` | histogram | `rpc_method`, `rpc_response_status_code` | otelgrpc |
| `http_server_request_duration_seconds` | histogram | `http_response_status_code` | otelhttp |
| `oms_cqrs_handler_duration_seconds` | histogram | `kind`, `handler`, `outcome` (`ok`/`rejected`/`internal_error`) | `cqrs.Observe` |
| `oms_outbox_pending` | gauge | — | relay |
| `oms_outbox_published_total` | counter | — | relay |
| `db_sql_*`, `pgxpool_*` | various | — | otelpgx |
| `rabbitmq_detailed_queue_messages_ready` | gauge | `queue` | RabbitMQ |

Every metric carries `service_name`, `service_version` and `deployment_environment`.

### Logs

`log/slog` as JSON to stdout and, in parallel, OTLP to Loki. Every line logged with a context includes `trace_id` and `span_id`. In Grafana a log line links to its trace and a trace links to its logs (the datasources are pre-provisioned).

## Dashboard

Grafana → **OMS** folder → **OMS overview**: rate and p95 per RPC, non-OK responses, outcome per use case, outbox backlog, messages per queue (DLQs included) and logs.

## Where to look when...

| Symptom | Where |
|---|---|
| Clients get 5xx | "gRPC non-OK" panel → Loki `{service_name="oms-api"} \|= "internal error"` → open the trace by `trace_id` |
| A placed order does not show up in `GET` | `oms_outbox_pending` (is the relay publishing?) → projector queue → DLQ → worker logs |
| The DLQ has messages | Worker logs with `message handling failed` and the `message_id`; fix, then [replay](operations.md#replay-the-dlq) |
| High latency | p95 per RPC → slow trace in Jaeger → dominant SQL span |

## Suggested starting SLOs

| SLI | Target |
|---|---|
| Command availability (non-5xx responses) | 99.9 % monthly |
| `PlaceOrder` p95 latency | < 200 ms |
| Projection lag (pending outbox > 0 for more than 30 s) | < 0.1 % of the time |
| Messages in a DLQ | 0; alert on the first message |
