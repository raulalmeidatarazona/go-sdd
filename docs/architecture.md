# Arquitectura

## Capas y regla de dependencias

Arquitectura hexagonal por bounded context. Las dependencias solo apuntan hacia dentro:

```text
transport/grpcapi ─┐
infrastructure/* ──┼──► application/{command,query,projection} ──► domain ──► platform/{ddd,apperr,tenancy}
module.go ─────────┘
```

| Capa | Contiene | No puede |
|---|---|---|
| `domain` | Agregados, value objects, eventos, errores y puertos (`Repository`) | Importar SQL, gRPC, proto ni RabbitMQ |
| `application` | Un handler por caso de uso y puertos de lectura y proyección | Conocer Postgres ni proto; decidir códigos HTTP o gRPC |
| `infrastructure` | Adaptadores: Postgres y codec protobuf de eventos | Contener reglas de negocio |
| `transport` | Mapeo proto ↔ comandos y queries | Contener reglas de negocio ni acceder a la base de datos |
| `module.go` | Composición: crea adaptadores, handlers y suscripciones | — |
| `internal/platform` | Infraestructura transversal sin lógica de negocio | Importar ningún bounded context |

Los bounded contexts no se importan entre sí. Se comunican por eventos de integración (proto en RabbitMQ) o por su API gRPC.

## Flujo de un comando

```mermaid
sequenceDiagram
  participant Cliente
  participant GW as grpc-gateway (:8080)
  participant S as gRPC (:9090)
  participant H as PlaceOrderHandler
  participant DB as PostgreSQL
  Cliente->>GW: POST /v1/orders + X-Tenant-Id
  GW->>S: PlaceOrder (metadata x-tenant-id)
  S->>S: interceptores: recovery → tenant → errores
  S->>H: cqrs.Observe (span + métrica + log)
  H->>DB: BEGIN; set_config('app.tenant_id')
  H->>DB: idempotency lookup
  H->>DB: INSERT orders, order_lines, outbox
  H->>DB: COMMIT
  S-->>Cliente: { orderId }
```

## Flujo de un evento

```mermaid
sequenceDiagram
  participant Relay as Outbox relay (worker)
  participant DB as PostgreSQL
  participant MQ as RabbitMQ
  participant P as Projector (worker)
  Relay->>DB: SELECT ... FOR UPDATE SKIP LOCKED
  Relay->>MQ: publish (routing key = oms.order.v1.OrderPlaced), espera confirm
  Relay->>DB: UPDATE published_at; COMMIT
  MQ->>P: entrega (headers: tenant, versión, traceparent)
  P->>DB: BEGIN (tenant); INSERT inbox ON CONFLICT DO NOTHING
  P->>DB: upsert de la vista con guarda de versión; COMMIT
  P->>MQ: ack (o nack → reintento → DLQ)
```

La traza es **una sola** de punta a punta: el `traceparent` se guarda en el outbox y viaja en las cabeceras AMQP.

## Procesos

| Proceso | Responsabilidad | Escala |
|---|---|---|
| `oms-api` | gRPC + REST; solo escribe en Postgres | Horizontal, sin estado |
| `oms-worker` | Relay del outbox + consumidores | Horizontal; el relay reparte trabajo con `SKIP LOCKED` y las colas *quorum* reparten mensajes |

La API no depende de RabbitMQ: si el broker cae, se siguen aceptando pedidos y los eventos esperan en el outbox. El worker es *crash-only*: si pierde el broker, termina y Docker o Kubernetes lo reinicia.

## Modelo de errores

El dominio y la aplicación devuelven `apperr.Error{Kind, Code, Message}`. Un único interceptor (`grpcx.ToStatus`) traduce `Kind` a código gRPC y añade `google.rpc.ErrorInfo{reason: Code, domain: "oms"}`. grpc-gateway traduce el código gRPC a HTTP. Los errores internos nunca exponen su mensaje.

| Kind | gRPC | HTTP |
|---|---|---|
| Invalid | INVALID_ARGUMENT | 400 |
| Unauthenticated | UNAUTHENTICATED | 401 |
| NotFound | NOT_FOUND | 404 |
| Conflict | ALREADY_EXISTS | 409 |
| Aborted | ABORTED | 409 |
| FailedPrecondition | FAILED_PRECONDITION | 400 |
| Internal | INTERNAL | 500 |

## Decisiones

Las razones de cada elección están en los [ADRs](adr/).
