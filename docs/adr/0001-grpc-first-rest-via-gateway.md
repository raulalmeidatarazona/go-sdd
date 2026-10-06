# ADR 0001: gRPC first, REST generated with grpc-gateway

- Status: accepted
- Date: 2026-10-06

## Context

We need a strong contract between services and, at the same time, a convenient REST/JSON API for external clients. Two hand-maintained APIs drift apart over time.

## Decision

The `.proto` files are the single source of truth. REST comes from `google.api.http` annotations through grpc-gateway, and the OpenAPI document is generated as well. buf checks style and compatibility.

## Consequences

- One definition for types, compatibility checks and documentation.
- REST follows Google AIP conventions instead of per-team habits.
- 64-bit integers travel as JSON strings; clients must be told.
- Highly custom REST routes (file uploads, SSE streaming) need extra HTTP handlers outside the gateway.
