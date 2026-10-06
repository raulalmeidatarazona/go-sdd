# Sota, caballo y rey

Cada funcionalidad tiene la misma forma, en el mismo orden y en los mismos sitios. Si un cambio no encaja en una de estas recetas, es una decisión de arquitectura: escribe un ADR antes de programarlo.

Las recetas usan como ejemplo añadir **ConfirmOrder** (pasar un pedido de `PLACED` a `CONFIRMED`).

## Receta 1 — Nuevo comando (escritura)

1. **Contrato** — `api/proto/oms/order/v1/order_service.proto`
   - Añade `rpc ConfirmOrder(ConfirmOrderRequest) returns (ConfirmOrderResponse)` con su anotación `google.api.http`. Para acciones usa `post: "/v1/orders/{order_id}:confirm"` y `body: "*"`.
   - Añade los mensajes Request y Response. La respuesta de un comando solo lleva IDs, nunca el recurso.
   - Si el cambio de estado debe conocerse fuera, añade el evento `OrderConfirmed` en `order_events.proto`.
   - Ejecuta `make proto`. `buf lint` y `buf breaking` validan el contrato.
2. **Dominio** — `internal/ordering/domain/`
   - Añade el estado `StatusConfirmed`, el evento `OrderConfirmed` con su `EventName()` igual al nombre del mensaje proto, y los errores con un **código estable** en `errors.go`.
   - Añade el método `func (o *Order) Confirm(now time.Time) error`. Primero comprueba los invariantes, después cambia el estado y por último llama a `o.Record(...)`.
   - Escribe el test del agregado en `order_test.go`.
3. **Persistencia** — si el estado nuevo necesita columnas, crea la migración con `make migrate-new NAME=order_confirmation`. Ajusta el `CHECK` de `status` y el `Update` del repositorio. El outbox no se toca: guarda cualquier evento.
4. **Codec** — `infrastructure/events/codec.go`: añade el `case` del evento nuevo en `Encode` y en `Decode`.
5. **Caso de uso** — crea `application/command/confirm_order.go` con esta forma exacta:
   ```go
   type ConfirmOrder struct{ OrderID string }
   type ConfirmOrderResult struct{}
   type ConfirmOrderHandler struct{ uow UnitOfWork; orders domain.Repository; now func() time.Time }
   func NewConfirmOrderHandler(...) *ConfirmOrderHandler
   func (h *ConfirmOrderHandler) Handle(ctx context.Context, cmd ConfirmOrder) (ConfirmOrderResult, error) {
       // 1. tenant  2. parsear a value objects  3. uow{ Get → comportamiento → Update }
   }
   ```
   Añade su test en `commands_test.go` usando el repositorio en memoria.
6. **Transporte** — `transport/grpcapi/order_server.go`: añade el campo en `Handlers` y el método RPC. Ese método solo mapea entre proto y el comando, sin reglas de negocio.
7. **Composición** — `module.go`: regístralo con `cqrs.Observe(cqrs.KindCommand, "ConfirmOrder", logger, command.NewConfirmOrderHandler(uow, repository, now))`. Así hereda trazas, métricas y logs sin escribir nada más.
8. **Proyección** — si la vista cambia: añade el `case` en `projection/order_projector.go`, un método en `projection.Store` y su implementación con **guarda de versión** (`WHERE version < $n`) en `order_read_model.go`.
9. **Pruebas end-to-end** — añade el caso en `test/e2e`. Después ejecuta `make check` y `make test-integration`.
10. **Docs** — actualiza `docs/api.md` si cambia alguna convención y añade la petición de ejemplo a `api/http/orders.http`.

## Receta 2 — Nueva query (lectura)

1. Contrato: `rpc GetX(GetXRequest) returns (GetXResponse)` con `get:`, y después `make proto`.
2. Si la vista actual no tiene los datos, extiende la proyección (receta 4) en lugar de leer el modelo de escritura.
3. Crea `application/query/get_x.go` con un struct de query, un handler que reciba el puerto `ReadModel` y `Handle` que resuelva tenant, valide y lea.
4. Implementa la consulta en `infrastructure/postgres/order_read_model.go` dentro de `InTenantTx` y siempre con parámetros (`$1`), nunca con concatenación.
5. Añade el método gRPC en `grpcapi` y regístralo en `module.go` con `cqrs.Observe(cqrs.KindQuery, ...)`.
6. Escribe el test del handler con un `ReadModel` falso y el caso e2e.

## Receta 3 — Nuevo consumidor de eventos

1. Decide quién posee la cola: siempre el contexto que **reacciona**, nunca el que publica.
2. En el `module.go` de ese contexto, añade una `rabbitmq.Subscription` con:
   - `Queue: "oms.<contexto>.<propósito>"`, que es estable porque renombrarla crea otra cola;
   - `RoutingKeys`, con los nombres de evento o comodines (`oms.order.v1.*`);
   - `Handle: inbox.Handle(db, <queue>, fn)`. El inbox es obligatorio, porque la entrega es *at-least-once*.
3. Haz `fn` idempotente y con guarda de versión. Si un mensaje llega antes de tiempo, devuelve error: se reintentará y, tras `DeliveryLimit` intentos, irá a `<queue>.dlq`.
4. El worker lo arranca solo, sin tocar `cmd/worker`.

## Receta 4 — Cambiar la proyección

1. Crea una migración en `ordering_read` con las columnas o la tabla nuevas.
2. Si el cambio necesita datos históricos, **reconstruye la proyección**: vacía la tabla y reprocesa el outbox (runbook en [operations.md](operations.md#reconstruir-una-proyección)).
3. Cada actualización lleva su guarda de versión.

## Receta 5 — Nuevo bounded context

Copia la estructura de `internal/ordering` y cambia los nombres:

```text
internal/<contexto>/
  domain/                 agregado(s), value objects, eventos, errores, Repository
  application/command/    un archivo por comando
  application/query/      un archivo por query + read_model.go
  application/projection/ si tiene modelo de lectura propio
  infrastructure/postgres y infrastructure/events
  transport/grpcapi/
  module.go               NewModule, RegisterGRPC, RegisterGateway, Subscriptions
```

Después:

1. Crea `api/proto/oms/<contexto>/v1/` con su servicio y sus eventos.
2. Crea una migración con esquemas propios `<contexto>` y `<contexto>_read`, con RLS en todas las tablas con `tenant_id` y `GRANT` a `oms_app`.
3. Añade `NewModule` en `internal/app/app.go`. Es la **única** línea que se toca fuera del contexto.
4. Un contexto **no importa** paquetes de otro. Se comunican por eventos o por su API gRPC.

## Checklist de revisión

- [ ] El contrato proto cambió primero y `buf breaking` pasa.
- [ ] Las reglas de negocio están en el dominio y tienen test.
- [ ] El handler sigue la forma: tenant → parsear → uow → resultado con IDs.
- [ ] El handler está registrado con `cqrs.Observe`.
- [ ] Los errores son `apperr` con código estable y documentado.
- [ ] Todo el SQL corre en `InTenantTx`, con parámetros y con las tablas nuevas bajo RLS.
- [ ] Los eventos son compatibles hacia atrás y los consumidores son idempotentes y con guarda de versión.
- [ ] `make check` y `make test-integration` pasan.
