# Mapa desde el repo TypeScript

Repo viejo: `/root/reference/BancoDeDatosAgente` (`95f9a00`).

## Paquetes

| Viejo | Nuevo | Notas |
|---|---|---|
| `packages/shared/src/domain.ts` | `internal/domain` | enums, estados, transiciones |
| `packages/shared/src/audit.ts` | `internal/audit` | canonicalización y hash |
| `packages/shared/src/documentos.ts` | `internal/docs` | diff/hash |
| `packages/shared/src/secretScanner.ts` | `internal/security` | escaneo de secretos |
| `packages/server/src/app.ts` | `internal/httpapi` | router y middleware |
| `packages/server/src/routes/*` | `internal/httpapi` | handlers por área |
| `packages/server/src/services/*` | `internal/service` | lógica de dominio |
| `packages/server/src/auth/*` | `internal/auth` | tokens, sesiones, permisos |
| `packages/server/migrations/*` | `internal/db/migrations` | reescritas para SQLite |
| `packages/cli` | `cmd/agentbank` (fase 6) | subcomandos mínimos |
| `packages/mcp` | deferred | MCP posterior |
| `packages/server/src/ui` | **no se porta** | solo `docs/frontend.md` |

## Endpoints viejos → MVP

| Viejo | Nuevo | Fase |
|---|---|---|
| `GET /healthz` | igual | 0 |
| `GET /v1/me` | igual | 1 |
| `GET/POST /v1/projects` | igual | 1 |
| `POST /v1/participants` | igual | 1 |
| `POST /v1/participants/:id/token` | `.../tokens` | 1 |
| `GET /v1/tickets/:id` | igual | 2 |
| `POST /v1/tickets/:id/claims` | igual | 2 |
| `POST /v1/tickets/siguiente` | igual | 2 |
| `POST /v1/tickets/:id/comments` | igual | 2 |
| `GET /v1/tickets/:id/timeline` | igual | 2 |
| `GET /v1/projects/:id/phases` | igual | 3 |
| `GET /v1/projects/:id/epics` | igual | 3 |
| `GET /v1/me/bandeja` | igual | 4 |
| `POST /v1/messages` | igual | 4 |
| `POST /v1/messages/:id/resolver` | igual | 4 |
| `GET /v1/projects/:id/documents` | igual | 5 |
| `GET /v1/documents/:id/versions` | igual | 5 |
| `GET /v1/search` | igual (simple) | 5 |
| `GET /v1/audit/chains` | igual | 1 |
| `GET /v1/audit/verify` | igual | 1 |

Quedan fuera del MVP: UI, Telegram, outbox, contratos, archivado, suscripciones,
long-poll, handoff, defaults automáticos, MCP/CLI completos.

## Modelo de datos

| Viejo | Nuevo | Notas |
|---|---|---|
| `projects` | `projects` | `prefix`, `version` |
| `users` + `agents` | `participants` | `type` humano/agente |
| `agent_grants` | `memberships` | permiso + roles |
| `tokens` | `tokens` | hash, revocación |
| `sessions` | `sessions` | `X-Session-Id` |
| `audit_events` | `audit_events` | hash chain, append-only |
| `tickets` | `tickets` | estados, versión |
| `ticket_edges` | `ticket_edges` | ciclos |
| `claims` | `claims` | lease |
| `comments` | `comments` | timeline |
| `messages` | `messages` | opciones/default/afecta |
| `documents` + `document_versions` | igual | hash/diff |
| `phases`/`epics` | igual | orden |

## Reglas de traducción

- No copiar SQL de Postgres: `jsonb`, `text[]`, `tsvector`, `timestamptz` y
  triggers se reemplazan por `TEXT` JSON, tablas puente y lógica en Go.
- No copiar el frontend ni el CSS.
- No copiar tests 1:1; reescribir casos de negocio.
- Mantener nombres de campos de la API cuando sea posible.
- Documentar cada diferencia en `docs/api.md`.
