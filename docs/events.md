# Events and messaging

## Guarantees

| Guarantee | Mechanism |
|---|---|
| No state without its event, no event without its state | **Transactional outbox**: the event is inserted into `messaging.outbox` in the same transaction as the aggregate |
| The broker does not lose accepted messages | *Publisher confirms*, persistent messages and replicated *quorum* queues |
| Each consumer processes each message once | **Inbox**: `INSERT ... ON CONFLICT DO NOTHING` in the same transaction as the side effect |
| Poison messages do not block the queue | `x-delivery-limit` (5 by default), then *dead-lettering* to `<queue>.dlq` |
| Old or repeated events do not overwrite newer data | Version guard in projections: `WHERE version < $event` |

Delivery is **at-least-once**. Every new consumer must use `inbox.Handle`.

## Topology

```text
exchange oms.events (topic, durable)
  routing key = fully qualified proto message name, e.g. oms.order.v1.OrderPlaced
  └── oms.ordering.order-projector     (quorum, bound to oms.order.v1.*)
exchange oms.events.dlx (direct)
  └── oms.ordering.order-projector.dlq (quorum)
```

Each subscription declares its own queue and DLQ on start-up (`rabbitmq.Consume`). The client declares the exchanges when it connects.

## Message format

| AMQP property | Value |
|---|---|
| `message_id` | Message UUIDv7 (the inbox key) |
| `type` | `oms.order.v1.OrderPlaced` |
| `content_type` | `application/x-protobuf` |
| `delivery_mode` | 2 (persistent) |
| header `x-tenant-id` | the aggregate's tenant |
| headers `x-aggregate-id` / `x-aggregate-version` | ordering and deduplication |
| headers `traceparent`, `tracestate` | W3C trace context |

The body is the protobuf message from `order_events.proto`. Events evolve under the same rules as the API: adding fields is fine; renumbering or changing types is not.

## Ordering

- The relay publishes in `seq` order and stops at the first failure, so with **one** relay per-aggregate order is preserved.
- With several relays, or with retries, `OrderCancelled` can arrive before `OrderPlaced`. The projector returns `ErrViewMissing`, the message is retried, and it is applied once the view exists. The version guard discards anything that arrives late.

## Retries and the DLQ

1. When the handler fails, the message is `nack`ed with `requeue=true`.
2. RabbitMQ counts deliveries (`x-delivery-count`) and, once `x-delivery-limit` is exceeded, moves the message to `<queue>.dlq`.
3. A DLQ with messages is an alert: check the "RabbitMQ queues" panel in Grafana. To replay, follow the runbook in [operations.md](operations.md#replay-the-dlq).

## Housekeeping

Published outbox rows and old inbox rows grow without bound. In production, schedule a periodic purge, for example with `pg_cron`:

```sql
DELETE FROM messaging.outbox WHERE published_at < now() - interval '7 days';
DELETE FROM messaging.inbox  WHERE processed_at < now() - interval '30 days';
```

Keep the inbox longer than the longest possible redelivery window.
