# ADR 0002: CQRS con modelo de lectura proyectado por eventos

- Estado: aceptada
- Fecha: 2026-10-06

## Contexto

Las lecturas (listados filtrados y paginados) y las escrituras (invariantes de agregado) tienen necesidades distintas de forma y de escala.

## Decisión

Los comandos escriben el modelo normalizado (`ordering.*`) y devuelven solo IDs. Las queries leen vistas desnormalizadas (`ordering_read.*`) que mantiene un proyector consumiendo los eventos del bus.

## Consecuencias

- Cada lado se optimiza y escala por separado; se pueden añadir vistas nuevas reprocesando eventos.
- Las lecturas son eventualmente consistentes (milisegundos en condiciones normales); los clientes deben tolerarlo (campo `version`).
- Hay más piezas: outbox, relay, inbox y proyector. Si un contexto no necesita lecturas distintas, puede leer el modelo de escritura con el mismo puerto `ReadModel`.
