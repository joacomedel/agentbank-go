# ADR 0001 — Go + SQLite puro, backend sin frontend

## Contexto

AgentBank viejo es TypeScript/Node + Fastify + Postgres + MCP + UI. Corre en una
PC; el objetivo es un backend equivalente que viva en el Moto G20 (3.4 GB RAM,
8 núcleos ARM, eMMC) sin competir con OpenCode.

## Decisión

- Go 1.27.3 con `net/http` y `log/slog`.
- SQLite embebido con `modernc.org/sqlite` (Go puro, sin CGO ni gcc).
- Sin ORM ni framework HTTP.
- Sin frontend: la API se documenta para construir la UI después.
- Migraciones SQL embebidas.
- Un solo binario y un solo archivo de base.

## Consecuencias

- El binario no depende de Node, Postgres ni Docker.
- El consumo de RAM es bajo y el despliegue es simple.
- SQLite limita la concurrencia de escritura; el diseño usa un writer y
  transacciones cortas.
- No hay paridad automática con Postgres: `jsonb`, `text[]`, `tsvector` y
  triggers se reescriben.
- El frontend queda desacoplado y consume un contrato HTTP estable.
- Si el proyecto crece a decenas de miles de escrituras por minuto, se evalúa
  migrar a Postgres; el MVP no lo necesita.
