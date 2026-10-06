# Operations

## Local development

| Mode | Commands | When |
|---|---|---|
| Everything in Docker | `make up`, `make logs`, `make down` | Exercising the whole system, demos |
| Binaries on the host | `make infra`, then `make run-api` and `make run-worker` | Developing with a debugger or fast reloads |

`make reset` deletes the volumes (Postgres and RabbitMQ) and starts from scratch.

If a host port is taken, change its mapping in `compose.yaml`. Grafana's port is already configurable with `GRAFANA_PORT` in `.env`.

## Configuration

Everything is configured through environment variables (12-factor):

| Variable | Default | Process |
|---|---|---|
| `DATABASE_URL` | — (required) | api, worker |
| `RABBITMQ_URL` | — (required by the worker) | worker |
| `GRPC_ADDR` / `HTTP_ADDR` | `:9090` / `:8080` | api |
| `WORKER_HTTP_ADDR` | `:8081` | worker (`/healthz`, `/readyz`) |
| `LOG_LEVEL` | `info` | both |
| `ENVIRONMENT`, `SERVICE_VERSION` | `local`, `dev` | both (telemetry attributes) |
| `SHUTDOWN_TIMEOUT` | `20s` | both |
| `OUTBOX_POLL_INTERVAL` / `OUTBOX_BATCH_SIZE` | `250ms` / `100` | worker |
| `CONSUMER_PREFETCH` | `20` | worker |
| `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_SERVICE_NAME`, `OTEL_*` | — | both (OpenTelemetry standard) |

## Health and shutdown

- `GET /healthz` (alive) and `GET /readyz` (dependencies ready). In the distroless container, `oms-api healthcheck` and `oms-worker healthcheck` run the probe.
- On SIGTERM the API marks gRPC health as `NOT_SERVING`, stops accepting requests and waits for in-flight ones (`GracefulStop`) up to `SHUTDOWN_TIMEOUT`. The worker finishes the current message; unacknowledged ones return to the queue.

## Migrations

- They live in `migrations/NNNNNN_name.{up,down}.sql` (golang-migrate) and are created with `make migrate-new NAME=...`.
- Compose's `migrate` service applies them as `oms_owner`. In production they run as a pre-deployment job, never from the application at start-up.
- Use *expand/contract* for zero downtime: first add (nullable column or new table), deploy code that writes both, backfill, and drop the old shape in a later migration.
- Every new table with `tenant_id` gets `ENABLE` + `FORCE ROW LEVEL SECURITY`, its policy and its `GRANT` to `oms_app`.

## Runbooks

### Replay the DLQ

1. Find the cause in the worker logs (the failing `message_id`) and deploy the fix.
2. Enable shovel once: `docker compose exec rabbitmq rabbitmq-plugins enable rabbitmq_shovel rabbitmq_shovel_management`.
3. At http://localhost:15672 → Queues → `<queue>.dlq` → **Move messages** to `<queue>`.
4. The inbox prevents duplicates if some messages had already been applied.

### Rebuild a projection

Needed when the view's shape changes or it gets corrupted. Requires the outbox to keep its history.

```sql
BEGIN;
TRUNCATE ordering_read.order_views;
DELETE FROM messaging.inbox WHERE consumer = 'oms.ordering.order-projector';
UPDATE messaging.outbox SET published_at = NULL WHERE aggregate_type = 'order';
COMMIT;
```

The relay republishes everything. Other consumers ignore the duplicates thanks to their own inbox, and the projector rebuilds the view. Meanwhile, queries return incomplete data: do it in a maintenance window, or build a parallel table and swap names at the end.

### The worker restarts in a loop

Check `docker compose logs worker`. `rabbitmq connection lost` is expected when the broker restarts; the process recovers on its own. If it persists, check `RABBITMQ_URL` and the broker's health.
