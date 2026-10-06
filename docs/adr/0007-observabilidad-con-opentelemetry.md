# ADR 0007: OpenTelemetry y un collector como única salida de telemetría

- Estado: aceptada
- Fecha: 2026-10-06

## Contexto

Queremos trazas, métricas y logs correlacionados sin atar el código a un proveedor.

## Decisión

Los servicios exportan las tres señales por OTLP a un OpenTelemetry Collector, que las envía a Jaeger, Prometheus y Loki. El contexto de traza se propaga por gRPC, HTTP y AMQP (a través del outbox).

## Consecuencias

- Cambiar de backend es un cambio de configuración.
- Una petición y sus efectos asíncronos forman una sola traza.
- El collector es una pieza más que operar; en producción se despliega como agente o *gateway* con alta disponibilidad.
