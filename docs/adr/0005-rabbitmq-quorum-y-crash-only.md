# ADR 0005: RabbitMQ con colas quorum, DLQ y cliente crash-only

- Estado: aceptada
- Fecha: 2026-10-06

## Contexto

Necesitamos colas duraderas y replicadas, reintentos acotados y un manejo de desconexiones simple y correcto.

## Decisión

Exchange topic `oms.events` con routing key igual al nombre del evento. Cada suscripción declara una cola *quorum* con `x-delivery-limit` y *dead-letter* hacia `<cola>.dlq`. Si se pierde la conexión, el proceso termina y el orquestador lo reinicia, en lugar de implementar reconexión en caliente.

## Consecuencias

- Los mensajes venenosos no bloquean la cola y quedan inspeccionables.
- El código de reconexión, frecuente fuente de bugs, desaparece.
- Un reinicio del broker reinicia también el worker; es aceptable porque el outbox retiene los eventos.
- Los reintentos son inmediatos; para *backoff* exponencial habría que añadir colas de espera con TTL.
