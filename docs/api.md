# API conventions

## gRPC first

- The contract lives in `api/proto/oms/<context>/v1/*.proto` and is checked with `buf lint` (STANDARD style) and `buf breaking` (file-level compatibility).
- REST is **never hand-written**: each RPC declares its route with `google.api.http` and grpc-gateway transcodes it. The OpenAPI document (`/openapi.json`) is generated too.
- Naming follows the Google API Improvement Proposals: plural resources (`/v1/orders`), actions with a `:verb` suffix (`/v1/orders/{id}:cancel`), lowerCamelCase JSON.
- 64-bit integers are serialised in JSON as **strings** (`"totalMinor": "8498"`), as proto3 JSON mandates, so JavaScript clients do not lose precision.

## Headers

| Header | Required | Purpose |
|---|---|---|
| `X-Tenant-Id` | Yes | Tenant UUID. In gRPC it is the `x-tenant-id` metadata. See [multitenancy.md](multitenancy.md) |
| `X-Request-Id` | No | Forwarded to gRPC for correlation |
| `traceparent` | No | W3C Trace Context; when present, the trace continues |

## Commands and queries

- **Commands** (`PlaceOrder`, `CancelOrder`) return identifiers only.
- **Queries** (`GetOrder`, `ListOrders`) read the read model, which is **eventually consistent**. Right after a command, a `GET` may return 404 or an older version for a few milliseconds. Clients that need read-your-writes retry while comparing the `version` field.

## Idempotency

`PlaceOrder` requires an `idempotencyKey` (up to 128 characters), unique per tenant:

- the same key with the same body returns the same `orderId` (a safe retry);
- the same key with a different body returns `409 ALREADY_EXISTS`, `reason: idempotency_key_reused`.

`CancelOrder` is idempotent in effect: repeating it returns `FAILED_PRECONDITION` (`order_already_cancelled`) and changes nothing.

## Pagination

`ListOrders` uses cursor pagination (keyset on `placed_at, id`), which stays stable while new orders arrive:

- `pageSize`: defaults to 50, maximum 200;
- `pageToken`: opaque; pass the previous response's `nextPageToken`;
- an empty `nextPageToken` means there are no more pages.

## Errors

The `google.rpc.Status` format, identical over gRPC and REST:

```json
{
  "code": 3,
  "message": "currency must be an ISO 4217 code such as EUR",
  "details": [{
    "@type": "type.googleapis.com/google.rpc.ErrorInfo",
    "reason": "invalid_currency",
    "domain": "oms"
  }]
}
```

Clients branch on `reason`, which is stable and part of the contract, never on `message`. The codes are defined in `internal/*/domain/errors.go`.

## Versioning

- Compatible changes (new fields, RPCs or enum values) go into `v1`.
- Breaking changes create a `v2` package that coexists with `v1` until `v1` is retired. `buf breaking` stops `v1` from breaking by accident.
- Field numbers are never reused; removed fields are marked `reserved`.
