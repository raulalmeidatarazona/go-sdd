# Convenciones de la API

## gRPC first

- El contrato está en `api/proto/oms/<contexto>/v1/*.proto` y se valida con `buf lint` (estilo STANDARD) y `buf breaking` (compatibilidad a nivel de fichero).
- REST **no se escribe a mano**: cada RPC declara su ruta con `google.api.http` y grpc-gateway la transcodifica. El OpenAPI (`/openapi.json`) también se genera.
- Los nombres siguen las Google API Improvement Proposals: recursos en plural (`/v1/orders`), acciones con sufijo `:verbo` (`/v1/orders/{id}:cancel`) y JSON en lowerCamelCase.
- Los enteros de 64 bits se serializan en JSON como **string** (`"totalMinor": "8498"`), como dicta proto3 JSON. Los clientes JavaScript no pierden precisión.

## Cabeceras

| Cabecera | Obligatoria | Uso |
|---|---|---|
| `X-Tenant-Id` | Sí | UUID del tenant. En gRPC es la metadata `x-tenant-id`. Ver [multitenancy.md](multitenancy.md) |
| `X-Request-Id` | No | Se propaga a gRPC para correlación |
| `traceparent` | No | W3C Trace Context; si llega, la traza continúa |

## Comandos y queries

- Los **comandos** (`PlaceOrder`, `CancelOrder`) devuelven solo identificadores.
- Las **queries** (`GetOrder`, `ListOrders`) leen el modelo de lectura, que es **eventualmente consistente**. Justo después de un comando, un `GET` puede devolver 404 o una versión anterior durante unos milisegundos. Los clientes que necesiten leer su propia escritura reintentan comparando el campo `version`.

## Idempotencia

`PlaceOrder` exige `idempotencyKey` (hasta 128 caracteres), única por tenant:

- con la misma clave y el mismo cuerpo devuelve el mismo `orderId` (reintento seguro);
- con la misma clave y otro cuerpo responde `409 ALREADY_EXISTS`, `reason: idempotency_key_reused`.

`CancelOrder` es naturalmente idempotente en efecto: repetirlo devuelve `FAILED_PRECONDITION` (`order_already_cancelled`) sin cambiar nada.

## Paginación

`ListOrders` usa paginación por cursor (keyset sobre `placed_at, id`), estable aunque entren pedidos nuevos:

- `pageSize`: por defecto 50, máximo 200;
- `pageToken`: opaco; se pasa el `nextPageToken` de la respuesta anterior;
- un `nextPageToken` vacío indica que no hay más páginas.

## Errores

Formato `google.rpc.Status`, igual en gRPC y en REST:

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

Los clientes deciden por `reason`, que es estable y forma parte del contrato, nunca por `message`. Los códigos están en `internal/*/domain/errors.go`.

## Versionado

- Cambios compatibles (añadir campos, RPCs o valores de enum) se hacen en `v1`.
- Cambios incompatibles crean un paquete `v2` que convive con `v1` hasta retirarlo. `buf breaking` impide romper `v1` por accidente.
- Nunca se reutilizan números de campo: se marcan con `reserved`.
