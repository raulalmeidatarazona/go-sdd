# ADR 0003: Transactional outbox e inbox para mensajería fiable

- Estado: aceptada
- Fecha: 2026-10-06

## Contexto

Escribir en la base de datos y publicar en RabbitMQ en dos pasos produce estado sin evento (si falla la publicación) o evento sin estado (si falla el commit).

## Decisión

El repositorio inserta los eventos en `messaging.outbox` en la misma transacción que el agregado. Un relay los publica con *publisher confirms* y marca `published_at`. Cada consumidor registra el `message_id` en `messaging.inbox` en la misma transacción que su efecto.

## Consecuencias

- Entrega *at-least-once* con procesamiento efectivo *exactly-once* por consumidor.
- La API no depende del broker para aceptar escrituras.
- Latencia adicional del sondeo (250 ms por defecto) y tablas que hay que purgar.
- Alternativa descartada por ahora: CDC con Debezium, más potente pero con más infraestructura.
