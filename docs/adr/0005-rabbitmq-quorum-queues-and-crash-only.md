# ADR 0005: RabbitMQ quorum queues, DLQs and a crash-only client

- Status: accepted
- Date: 2026-10-06

## Context

We need durable, replicated queues, bounded retries, and simple, correct handling of disconnects.

## Decision

A topic exchange `oms.events` whose routing key is the event name. Each subscription declares a *quorum* queue with `x-delivery-limit` and *dead-lettering* to `<queue>.dlq`. When the connection is lost, the process exits and the orchestrator restarts it, instead of implementing in-process reconnection.

## Consequences

- Poison messages do not block the queue and remain inspectable.
- Reconnection code, a common source of bugs, disappears.
- A broker restart also restarts the worker; acceptable because the outbox retains the events.
- Retries are immediate; exponential backoff would need TTL-based wait queues.
