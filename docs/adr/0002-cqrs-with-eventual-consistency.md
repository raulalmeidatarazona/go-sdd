# ADR 0002: CQRS with an event-projected read model

- Status: accepted
- Date: 2026-10-06

## Context

Reads (filtered, paginated listings) and writes (aggregate invariants) need different shapes and scale differently.

## Decision

Commands write the normalised write model (`ordering.*`) and return identifiers only. Queries read denormalised views (`ordering_read.*`) maintained by a projector consuming events from the bus.

## Consequences

- Each side is optimised and scaled independently; new views can be built by replaying events.
- Reads are eventually consistent (milliseconds under normal conditions); clients must tolerate it (`version` field).
- More moving parts: outbox, relay, inbox and projector. A context that does not need distinct reads may read its write model through the same `ReadModel` port.
