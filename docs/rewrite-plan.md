# Plan de reescritura — AgentBank Go

## Contexto

El repo viejo (`/root/reference/BancoDeDatosAgente`, commit `95f9a00`) es un
monorepo TypeScript/Node/Postgres de ~30k líneas con backend Fastify, MCP, CLI,
UI vanilla y 19 migraciones. La reescritura conserva la **semántica del
backend** y descarta el frontend; la UI se construirá después contra la API
documentada.

## Decisiones

| Tema | Decisión |
|---|---|
| Lenguaje | Go 1.27.3 |
| HTTP | `net/http` + `ServeMux` con patrones de método |
| Base | SQLite con `modernc.org/sqlite` (Go puro, sin CGO) |
| Migraciones | SQL embebido, forward-only, `schema_migrations` |
| JSON | stdlib, `DisallowUnknownFields`, snake_case |
| IDs | UUIDv4 con `crypto/rand` |
| Auth | bearer token hasheado, un token activo por participante |
| Auditoría | append-only + hash chain por proyecto y `system` |
| Frontend | fuera de alcance; solo `docs/api.md` y `docs/frontend.md` |
| Despliegue | binario + systemd en el Moto, SQLite local |

## Fases

### Fase 0 — Scaffold

- Go module, config por entorno, SQLite, migraciones, health/meta.
- `go test ./...` verde; servidor responde.
- Documentación base y reglas del agente.

### Fase 1 — Identidad y auditoría

- `projects`, `participants`, `memberships`, `tokens`, `sessions`.
- Bootstrap del humano (token inicial), alta de agentes, rotación/revocación.
- `GET /v1/me`, `GET /v1/projects`, `POST /v1/projects`.
- Auditoría: appendEvent transaccional, cadena `system` y por proyecto,
  `GET /v1/audit`, `GET /v1/audit/verify`.
- Tests de autoría, permisos, token revocado y cadena alterada.

### Fase 2 — Tickets y cola — ✅ implementada

- `tickets`, `ticket_edges`, `claims`, `comments`.
- Estados y transiciones; `listo` derivado; ciclos rechazados.
- Desde el hardening, el cierre normal pasa por `en revisión` y veredicto
  estructurado (`POST /v1/tickets/{id}/revision`).
- Claim con lease y heartbeat; `POST /v1/tickets/siguiente` con
  `BEGIN IMMEDIATE` y orden fase→épica→ticket→prioridad→antigüedad.
- `GET /v1/tickets/{id}`, `GET .../timeline`, comentarios, `ticket_avanzar`.
- Tests de concurrencia y de dependencias pendientes.

### Fase 3 — Fases y épicas — ✅ implementada

- `phases`, `epics`, orden y fase efectiva.
- Cierre con pendientes; briefing, digest y tablero.

### Fase 4 — Mensajes y bandeja — ✅ implementada

- `messages`, `message_affects`, `message_refs`, `message_state`.
- `info`/`solicitud`, destino participante/rol/proyecto, opciones y default.
- `GET /v1/me/bandeja`, tomar/resolver/descartar, `afecta` saca de listo.
- Handoff como alias, defaults asumidos, conversión a decisión/ticket y
  corrección cuando la respuesta difiere del default.

### Fase 5 — Documentos y búsqueda — ✅ implementada

- `documents`, `document_versions` con hash y diff.
- Docs por proyecto y globales, `ruta`, `fijado`, sync por hash, glosario.
- Decisiones/ADRs, adjuntos a tickets y búsqueda simple con `LIKE`.

### Fase 6 — Operación y hardening — ✅ implementada

- Import JSON con dry-run, modos y ciclos.
- Outbox/webhooks, long-poll de novedades, idempotencia, rate limit.
- Secret scanner, métricas, barridos, heartbeat, pausa.
- Catálogo de acciones de auditoría, sesión/origen y verificación global.
- `docs/api.md` y `docs/frontend.md` actualizados.

### Fase 7 — CLI y MCP — ✅ implementada

- CLI Go: `init`, `me`, `changes`, `next`, `advance`, `inbox`, `resolve`,
  `project`, `phase`, `epic`, `doc`, `import`, `audit`, `admin`, `worker` y
  `api` como escape hatch.
- MCP por stdio con 75 tools, sin perfiles y `api_request`.
- Worker básico con ejecutor `cmd`/`simulado`, heartbeat y liberación.
- Pendiente: ejecutor `opencode-server` y `--worktree`.

### Fase 8 — UI (fuera de este repo)

- El backend expone OpenAPI (`/openapi.json`, `/swagger`).
- El tablero, la bandeja y la identidad los construye otro equipo contra el
  contrato HTTP. Este repo no implementa UI de producto.

### Fase 9 — Integraciones P2/P3 (pendiente)

- Telegram, contratos entre proyectos, bloqueos/escalado y choque de áreas.

### Fase 10 — Moto

- systemd con `MemoryMax=128M`, `Nice=10`, bind a `127.0.0.1`/ZeroTier.
- Backup SQLite, rotación de logs, healthcheck.
- Benchmark: RSS, CPU, DB, latencia; documentar en `docs/benchmarks.md`.

## Endpoints MVP

| Área | Endpoints |
|---|---|
| Salud | `GET /healthz`, `GET /v1/meta` |
| Identidad | `GET /v1/me`, `GET/POST /v1/projects`, `GET/POST /v1/participants`, `POST /v1/participants/{id}/tokens`, `POST /v1/tokens/{id}/revoke` |
| Membresías | `GET/PUT/DELETE /v1/projects/{id}/members` |
| Tickets | `GET/POST /v1/projects/{id}/tickets`, `GET/PATCH /v1/tickets/{id}`, `POST /v1/tickets/siguiente`, `POST /v1/tickets/{id}/avanzar`, `POST /v1/tickets/{id}/revision`, `POST /v1/tickets/{id}/claims`, `POST /v1/tickets/{id}/claims/release`, `POST/GET /v1/tickets/{id}/comments`, `GET /v1/tickets/{id}/timeline` |
| Dependencias | `POST/DELETE /v1/tickets/{id}/dependencies` |
| Fases/épicas | `GET/POST /v1/projects/{id}/phases`, `PATCH /v1/phases/{id}`, `GET/POST /v1/projects/{id}/epics`, `PATCH /v1/epics/{id}` |
| Mensajes | `POST /v1/messages`, `GET /v1/me/bandeja`, `GET /v1/messages/{id}`, `POST /v1/messages/{id}/resolver` |
| Docs | `GET/POST /v1/projects/{id}/documents`, `GET /v1/documents/{id}`, `GET/POST /v1/documents/{id}/versions`, `GET /v1/search` |
| Auditoría | `GET /v1/audit`, `GET /v1/audit/chains`, `GET /v1/audit/verify` |

Los nombres y campos buscan parecerse a la API vieja; las diferencias se
documentan en `docs/api.md`. Las etapas 1–7 están implementadas (incluido CLI y
MCP); la UI se construye fuera de este repo y Telegram queda para la etapa 9.

## Presupuesto

- Binario ≤ 20 MB; RSS reposo ≤ 30 MB; carga ≤ 64 MB.
- DB ≤ 100 MB para 10k tickets.
- p95 local ≤ 50 ms.
- Sin dependencias fuera del stack permitido.

## Riesgos

- SQLite y concurrencia: un writer; leases y claims deben probarse con
  concurrencia real.
- Auditoría: la cadena debe ser verificable y no romperse por escrituras
  fuera de transacción.
- Paridad: el repo viejo tiene 80+ rutas; el MVP prioriza el ciclo de trabajo.
- El adaptador de OpenCode 2 del repo viejo no está validado contra la versión
  real; la reescritura no lo incluye en el MVP.
