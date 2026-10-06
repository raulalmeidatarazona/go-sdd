# ADR 0001: gRPC first y REST generado con grpc-gateway

- Estado: aceptada
- Fecha: 2026-10-06

## Contexto

Necesitamos un contrato fuerte entre servicios y, a la vez, una API REST/JSON cómoda para clientes externos. Mantener dos APIs a mano diverge con el tiempo.

## Decisión

Los `.proto` son la única fuente de verdad. REST se obtiene con anotaciones `google.api.http` y grpc-gateway; el OpenAPI también se genera. buf valida estilo y compatibilidad.

## Consecuencias

- Una sola definición para tipos, validación de compatibilidad y documentación.
- REST sigue las convenciones de Google AIP, no las de cada equipo.
- Los enteros de 64 bits viajan como string en JSON; hay que documentarlo para los clientes.
- Rutas REST muy personalizadas (subida de ficheros, streaming SSE) requieren handlers HTTP adicionales fuera del gateway.
