# ADR 0003: Transactional outbox and inbox for reliable messaging

- Status: accepted
- Date: 2026-10-06

## Context

Writing to the database and publishing to RabbitMQ in two steps yields state without an event (publish fails) or an event without state (commit fails).

## Decision

The repository inserts events into `messaging.outbox` in the same transaction as the aggregate. A relay publishes them with *publisher confirms* and sets `published_at`. Each consumer records the `message_id` in `messaging.inbox` in the same transaction as its side effect.

## Consequences

- At-least-once delivery with effectively exactly-once processing per consumer.
- The API does not depend on the broker to accept writes.
- Extra latency from polling (250 ms by default) and tables that need purging.
- Rejected for now: CDC with Debezium, more powerful but heavier infrastructure.
