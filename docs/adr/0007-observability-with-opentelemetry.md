# ADR 0007: OpenTelemetry with a collector as the single telemetry exit

- Status: accepted
- Date: 2026-10-06

## Context

We want correlated traces, metrics and logs without tying the code to a vendor.

## Decision

Services export all three signals over OTLP to an OpenTelemetry Collector, which forwards them to Jaeger, Prometheus and Loki. Trace context propagates over gRPC, HTTP and AMQP (through the outbox).

## Consequences

- Changing backends is a configuration change.
- A request and its asynchronous effects form a single trace.
- The collector is one more component to run; in production it is deployed as an agent or a highly available gateway.
