# API — AgentBank Go

Base: `http://127.0.0.1:8090`
Prefijo: `/v1`

## Convenciones

- `Content-Type: application/json; charset=utf-8`.
- Fechas: RFC3339 en UTC, por ejemplo `2026-11-07T20:00:00Z`.
- IDs: UUID string.
- Campos JSON: `snake_case` para acercarse a la API vieja.
- Auth: `Authorization: Bearer <token>`.
- Sesión: `X-Session-Id: <uuid>` (opcional; el server la crea si falta).
- Listados: `?limit=50&cursor=...`; `limit` máximo 200.
- Idempotencia: `Idempotency-Key: <clave>` en escrituras que la soporten.
- Errores:

```json
{
  "error": "mensaje legible",
  "code": "ticket_no_encontrado"
}
```

## Estado

| Endpoint | Estado |
|---|---|
| `GET /healthz` | implementado |
| `GET /v1/meta` | implementado |
| Resto del MVP | planificado (ver `docs/rewrite-plan.md`) |

## Salud

### `GET /healthz`

Verifica el proceso y la base.

```json
{"ok": true, "db": "ok"}
```

Si SQLite no responde: `503 {"error":"base no disponible","code":"db_unavailable"}`.

### `GET /v1/meta`

```json
{
  "name": "agentbank-go",
  "version": "0.1.0",
  "api_version": "v1",
  "database": "sqlite",
  "features": ["projects", "participants", "tickets", "messages", "documents", "audit"]
}
```

## Identidad (planificado)

### `POST /v1/projects`

```json
{"slug":"mi-proyecto","name":"Mi proyecto","prefix":"MIP"}
```

Respuesta `201`:

```json
{"id":"...","slug":"mi-proyecto","name":"Mi proyecto","prefix":"MIP","version":1}
```

### `GET /v1/projects`

```json
{"items":[{"id":"...","slug":"...","name":"...","prefix":"..."}],"next_cursor":null}
```

### `GET /v1/me`

Devuelve el participante autenticado, sus membresías y roles.

### `POST /v1/participants`

Alta de agente o humano (según permisos). El token en claro se devuelve **una
sola vez**:

```json
{"participante":{"id":"...","name":"backend-1","type":"agente"},"token":"ab_..."}
```

### `POST /v1/participants/{id}/tokens`

Rota el token: revoca el anterior y devuelve el nuevo en claro una vez.

## Tickets (planificado)

### `POST /v1/projects/{project_id}/tickets`

```json
{
  "title": "Implementar login",
  "description": "Detalle...",
  "priority": "alta",
  "role": "backend",
  "depends_on": []
}
```

### `GET /v1/tickets/{id}`

Devuelve ticket, estado efectivo, dependencias, claims activos y `version`.

### `PATCH /v1/tickets/{id}`

Requiere `version` para evitar pisadas:

```json
{"version":3,"title":"Nuevo título","status":"en curso"}
```

Conflicto: `409 {"error":"versión desactualizada","code":"version_conflict"}`.

### `POST /v1/tickets/siguiente`

```json
{"project_id":"...","role":"backend","claim":true}
```

Respuesta:

```json
{
  "ticket": {
    "id":"...",
    "ref":"MIP-42",
    "title":"...",
    "description":"...",
    "contexto": "..."
  },
  "motivo": null,
  "claims_activos": 1
}
```

Sin candidatos: `200` con `ticket: null` y un `motivo` accionable
(`esperando_dependencias`, `esperando_respuesta`, `reclamados`, `pausado`).

### `POST /v1/tickets/{id}/claims`

Reclama el ticket. Si otro lo tiene: `409 claim_conflict`.

### `POST /v1/tickets/{id}/claims/release`

Libera el claim propio.

### `POST /v1/tickets/{id}/comments`

```json
{"body":"Dejé el diseño en docs/login.md"}
```

### `GET /v1/tickets/{id}/timeline`

Lista eventos y comentarios en orden, con autor y fecha.

## Mensajes (planificado)

### `POST /v1/messages`

```json
{
  "type":"solicitud",
  "destination":{"type":"role","role":"dueño","project_id":"..."},
  "subject":"¿Qué base usamos?",
  "body":"...",
  "options":["postgres","sqlite"],
  "default_option":1,
  "affects":["ticket-uuid"]
}
```

### `GET /v1/me/bandeja`

```json
{"items":[...],"next_cursor":null}
```

### `POST /v1/messages/{id}/resolver`

```json
{"option":1}
```

o `{"text":"..."}`.

## Documentos (planificado)

### `POST /v1/projects/{project_id}/documents`

```json
{"title":"Arquitectura","format":"markdown","content":"# ..."}
```

### `POST /v1/documents/{id}/versions`

Crea una versión nueva inmutable; devuelve `version` y `hash`.

### `GET /v1/documents/{id}/versions`

Lista versiones con hash y autor.

### `GET /v1/documents/{id}/diff?from=1&to=2`

Diff por líneas.

### `GET /v1/search?q=login&type=ticket,document`

Búsqueda simple con permisos.

## Auditoría (planificado)

### `GET /v1/audit/chains`

Lista cadenas y su último `seq`.

### `GET /v1/audit?chain_id=...&limit=100`

Eventos en orden.

### `GET /v1/audit/verify?chain_id=...`

```json
{"ok":true,"chain_id":"...","eventos":123}
```

Si hay alteración: `{"ok":false,"chain_id":"...","primer_error":{"seq":42}}`.

## Diferencias con la API vieja

- Se eliminan endpoints de UI, Telegram, outbox, contratos y archivado del MVP.
- Se mantienen los nombres `snake_case` y los conceptos (`ref`, `version`,
  `claims`, `bandeja`).
- Los errores agregan `code` además de `error`.
- La paginación usa `cursor`; la vieja usaba offsets en algunos listados.
- La autenticación es bearer token; no se implementa login web.
