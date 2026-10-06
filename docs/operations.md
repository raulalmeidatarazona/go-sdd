# Operación

## Desarrollo local

| Modo | Comandos | Cuándo |
|---|---|---|
| Todo en Docker | `make up`, `make logs`, `make down` | Probar el sistema completo o hacer demos |
| Binarios en el host | `make infra`, y luego `make run-api` y `make run-worker` | Desarrollar con depurador o recarga rápida |

`make reset` borra los volúmenes (Postgres y RabbitMQ) y empieza de cero.

Si un puerto del host está ocupado, cambia su mapeo en `compose.yaml`. Grafana ya es configurable con `GRAFANA_PORT` en `.env`.

## Configuración

Todo se configura por variables de entorno (12-factor):

| Variable | Por defecto | Proceso |
|---|---|---|
| `DATABASE_URL` | — (obligatoria) | api, worker |
| `RABBITMQ_URL` | — (obligatoria en el worker) | worker |
| `GRPC_ADDR` / `HTTP_ADDR` | `:9090` / `:8080` | api |
| `WORKER_HTTP_ADDR` | `:8081` | worker (`/healthz`, `/readyz`) |
| `LOG_LEVEL` | `info` | ambos |
| `ENVIRONMENT`, `SERVICE_VERSION` | `local`, `dev` | ambos (atributos de telemetría) |
| `SHUTDOWN_TIMEOUT` | `20s` | ambos |
| `OUTBOX_POLL_INTERVAL` / `OUTBOX_BATCH_SIZE` | `250ms` / `100` | worker |
| `CONSUMER_PREFETCH` | `20` | worker |
| `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_SERVICE_NAME`, `OTEL_*` | — | ambos (estándar OpenTelemetry) |

## Salud y apagado

- `GET /healthz` (vivo) y `GET /readyz` (dependencias listas). En el contenedor distroless, `oms-api healthcheck` y `oms-worker healthcheck` hacen la sonda.
- Al recibir SIGTERM, la API marca el health gRPC como `NOT_SERVING`, deja de aceptar peticiones y espera a las que están en curso (`GracefulStop`) hasta `SHUTDOWN_TIMEOUT`. El worker termina el mensaje actual; los no confirmados vuelven a la cola.

## Migraciones

- Están en `migrations/NNNNNN_nombre.{up,down}.sql` (golang-migrate) y se crean con `make migrate-new NAME=...`.
- Las aplica el servicio `migrate` de Compose como `oms_owner`. En producción, es un *job* previo al despliegue, nunca la propia aplicación al arrancar.
- Patrón *expand/contract* para no tener tiempo de parada: primero se añade (columna nullable o tabla nueva), se despliega el código que escribe en ambos sitios, se rellenan los datos y, en una migración posterior, se elimina lo viejo.
- Cada tabla nueva con `tenant_id` lleva `ENABLE` + `FORCE ROW LEVEL SECURITY`, su política y su `GRANT` a `oms_app`.

## Runbooks

### Reprocesar la DLQ

1. Identifica la causa en los logs del worker (`message_id` del fallo) y despliega la corrección.
2. Activa shovel una sola vez: `docker compose exec rabbitmq rabbitmq-plugins enable rabbitmq_shovel rabbitmq_shovel_management`.
3. En http://localhost:15672 → Queues → `<cola>.dlq` → **Move messages** hacia `<cola>`.
4. El inbox evita duplicados si algún mensaje ya se había aplicado.

### Reconstruir una proyección

Se necesita cuando cambia la forma de la vista o se corrompe. Requiere que el outbox conserve el histórico.

```sql
BEGIN;
TRUNCATE ordering_read.order_views;
DELETE FROM messaging.inbox WHERE consumer = 'oms.ordering.order-projector';
UPDATE messaging.outbox SET published_at = NULL WHERE aggregate_type = 'order';
COMMIT;
```

El relay vuelve a publicar todo. Los demás consumidores ignoran los duplicados gracias a su propio inbox, y el proyector reconstruye la vista. Mientras dura, las queries devuelven datos incompletos: hazlo en una ventana de mantenimiento o en una tabla paralela con cambio de nombre al final.

### El worker se reinicia en bucle

Revisa `docker compose logs worker`. `rabbitmq connection lost` es esperable si el broker se reinicia; el proceso vuelve solo. Si persiste, revisa `RABBITMQ_URL` y la salud del broker.
