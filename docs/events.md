# Eventos y mensajería

## Garantías

| Garantía | Mecanismo |
|---|---|
| No hay estado sin evento ni evento sin estado | **Transactional outbox**: el evento se inserta en `messaging.outbox` en la misma transacción que el agregado |
| El broker no pierde mensajes aceptados | *Publisher confirms*, mensajes persistentes y colas *quorum* (replicadas) |
| Cada consumidor procesa cada mensaje una sola vez | **Inbox**: `INSERT ... ON CONFLICT DO NOTHING` en la misma transacción que el efecto |
| Los mensajes venenosos no bloquean la cola | `x-delivery-limit` (5 por defecto) y después *dead-letter* a `<cola>.dlq` |
| Los eventos viejos o repetidos no pisan datos nuevos | Guarda de versión en proyecciones: `WHERE version < $evento` |

La entrega es **at-least-once**. Cualquier consumidor nuevo debe usar `inbox.Handle`.

## Topología

```text
exchange oms.events (topic, durable)
  routing key = nombre completo del mensaje proto, p. ej. oms.order.v1.OrderPlaced
  └── oms.ordering.order-projector   (quorum, bind oms.order.v1.*)
exchange oms.events.dlx (direct)
  └── oms.ordering.order-projector.dlq (quorum)
```

Cada suscripción declara su propia cola y su DLQ al arrancar (`rabbitmq.Consume`). Los exchanges los declara el cliente al conectar.

## Formato del mensaje

| Propiedad AMQP | Valor |
|---|---|
| `message_id` | UUIDv7 del mensaje (clave del inbox) |
| `type` | `oms.order.v1.OrderPlaced` |
| `content_type` | `application/x-protobuf` |
| `delivery_mode` | 2 (persistente) |
| header `x-tenant-id` | tenant del agregado |
| header `x-aggregate-id` / `x-aggregate-version` | para ordenar y deduplicar |
| headers `traceparent`, `tracestate` | contexto de traza W3C |

El cuerpo es el mensaje protobuf de `order_events.proto`. Los eventos evolucionan con las mismas reglas que la API: añadir campos sí, renumerar o cambiar tipos no.

## Orden

- El relay publica en orden de `seq` y se detiene en el primer fallo, así que con **un** relay el orden por agregado se conserva.
- Con varios relays, o con reintentos, puede llegar `OrderCancelled` antes que `OrderPlaced`. El proyector devuelve `ErrViewMissing`, el mensaje se reintenta y, cuando la vista exista, se aplica. La guarda de versión descarta lo que llegue tarde.

## Reintentos y DLQ

1. Si el handler falla, se hace `nack(requeue=true)`.
2. RabbitMQ cuenta las entregas (`x-delivery-count`) y, al superar `x-delivery-limit`, mueve el mensaje a `<cola>.dlq`.
3. Una DLQ con mensajes es una alerta: revisa el panel "RabbitMQ queues" en Grafana. Para reprocesar, sigue el runbook de [operations.md](operations.md#reprocesar-la-dlq).

## Limpieza

Las filas publicadas del outbox y las antiguas del inbox crecen sin límite. En producción, programa una purga periódica, por ejemplo con `pg_cron`:

```sql
DELETE FROM messaging.outbox WHERE published_at < now() - interval '7 days';
DELETE FROM messaging.inbox  WHERE processed_at < now() - interval '30 days';
```

Conserva el inbox más tiempo que la ventana máxima de reentrega posible.
