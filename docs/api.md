# API — AgentBank Go

Base: `http://127.0.0.1:8090`
Prefijo: `/v1`

## Convenciones

- `Content-Type: application/json; charset=utf-8`.
- Fechas: RFC3339 UTC, por ejemplo `2026-11-07T20:00:00.000000000Z`.
- IDs: UUID string.
- Campos JSON: `snake_case`.
- Auth: `Authorization: Bearer <token>`.
- Sesión: `X-Session-Id: <uuid>` (opcional). Si falta, el server crea una
  sesión y la devuelve en el header `X-Session-Id` de la respuesta. El cliente
  debe reutilizarla para no generar una sesión por request.
- Listados: `?limit=50&cursor=...`; `limit` default 50, máximo 200. La respuesta
  es `{"items":[...],"next_cursor":null}`. El `cursor` es opaco para el cliente.
- Validación JSON: campos desconocidos o JSON malformado devuelven `400`.
  Errores semánticos devuelven `422`.
- Escrituras idempotentes: `Idempotency-Key` queda reservado; en Fase 1 las
  escrituras no lo implementan.
- Errores:

```json
{"error":"mensaje legible","code":"codigo_estable"}
```

## Estado

| Endpoint | Estado |
|---|---|
| `GET /healthz` | implementado |
| `GET /v1/meta` | implementado |
| `GET /v1/me` | implementado |
| `GET/POST /v1/projects` | implementado |
| `GET /v1/projects/{id}` | implementado |
| `GET /v1/projects/{id}/members` | implementado |
| `PUT/DELETE /v1/projects/{id}/members/{participant_id}` | implementado |
| `GET/POST /v1/participants` | implementado |
| `POST /v1/participants/{id}/tokens` | implementado |
| `POST /v1/tokens/{id}/revoke` | implementado |
| `GET /v1/audit/chains` | implementado |
| `GET /v1/audit` | implementado |
| `GET /v1/audit/verify` | implementado |
| Tickets, dependencias, claims, fases, épicas, mensajes, documentos | fases 2–5 |

## Salud

### `GET /healthz`

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
  "features": ["identity", "projects", "participants", "memberships", "tokens", "sessions", "audit"]
}
```

## Autenticación y bootstrap

En el primer arranque, si no existe ningún participante `humano`, el server
exige `AGENTBANK_BOOTSTRAP_TOKEN` (mínimo 32 caracteres). Crea un participante
humano `admin` de instancia y guarda solo `sha256(token)`.

- Si la variable falta en ese primer arranque, el proceso falla con un error
  claro y no arranca.
- Si ya existe un humano, la variable se ignora. El bootstrap es idempotente.
- El token de bootstrap no se devuelve por API ni se escribe en logs.
- Los tokens de agentes tienen formato `ab_<32 bytes base64url>` y también se
  guardan solo hasheados.
- Un participante tiene como máximo un token activo: emitir uno nuevo revoca el
  anterior. El texto plano se devuelve **una sola vez**.
- `401` = token faltante, desconocido o revocado; sesión ajena. `403` = sin
  permiso. Todo rechazo de auth o permisos se audita en la cadena `system`.

## Identidad

### `GET /v1/me`

Devuelve el participante autenticado, sus membresías y la sesión usada.

```json
{
  "participant": {
    "id": "…",
    "type": "humano",
    "name": "humano",
    "admin": true,
    "created_at": "2026-11-07T20:00:00.000000000Z"
  },
  "memberships": [
    {
      "project_id": "…",
      "project_slug": "mi-proyecto",
      "project_name": "Mi proyecto",
      "permission": "admin",
      "roles": ["dueño"],
      "created_at": "2026-11-07T20:05:00.000000000Z"
    }
  ],
  "session_id": "…"
}
```

### `POST /v1/projects`

Requiere admin de instancia. El creador recibe membresía `admin` con rol
`dueño`. `prefix` es opcional; si se omite se deriva del slug y se sufija para
evitar colisiones.

```json
{"slug":"mi-proyecto","name":"Mi proyecto","prefix":"MIP"}
```

Respuesta `201`:

```json
{
  "id": "…",
  "slug": "mi-proyecto",
  "name": "Mi proyecto",
  "prefix": "MIP",
  "version": 1,
  "permission": "admin",
  "roles": ["dueño"],
  "created_at": "2026-11-07T20:00:00.000000000Z",
  "updated_at": "2026-11-07T20:00:00.000000000Z"
}
```

Errores: `409 project_slug_taken`, `409 project_prefix_taken`,
`422 validation_error`, `403 requires_instance_admin`.

### `GET /v1/projects`

Devuelve solo los proyectos legibles por el actor. Un admin de instancia ve
todos; un agente ve únicamente sus membresías. Acepta `limit` y `cursor`.

```json
{"items":[{"id":"…","slug":"…","name":"…","prefix":"…","permission":"lector","roles":["backend"],"version":1,"created_at":"…","updated_at":"…"}],"next_cursor":null}
```

### `GET /v1/projects/{id}`

Acepta UUID o slug. Requiere permiso de lectura en el proyecto. Devuelve el
proyecto con `permission` y `roles` efectivos del actor.
`403 requires_project_read` si no tiene acceso.

### `POST /v1/participants`

Requiere admin de instancia. Crea un humano o agente, emite su primer token y
puede asignarle membresías en la misma transacción. El token en claro se
devuelve una sola vez.

```json
{
  "name": "backend-1",
  "type": "agente",
  "admin": false,
  "projects": [
    {"project_id": "…", "permission": "colaborador", "roles": ["backend"]}
  ]
}
```

`type`: `humano` o `agente`. `permission`: `lector`, `colaborador` o `admin`.
`roles`: hasta 20 roles de 1..32 caracteres (`\p{L}`, `\p{N}`, `_`, `-`).

Respuesta `201`:

```json
{
  "participant": {"id":"…","type":"agente","name":"backend-1","admin":false,"created_at":"…"},
  "token": "ab_…",
  "token_id": "…"
}
```

Errores: `409 participant_name_taken`, `404 project_not_found`,
`422 validation_error`, `403 requires_instance_admin`.

### `GET /v1/participants`

Cualquier participante autenticado puede listar participantes (datos públicos
de identidad; no incluye tokens). Acepta `limit` y `cursor`.

```json
{"items":[{"id":"…","type":"agente","name":"backend-1","admin":false,"created_at":"…"}],"next_cursor":null}
```

### `POST /v1/participants/{id}/tokens`

Rota el token del participante. Revoca el token activo anterior y emite uno
nuevo. Lo puede hacer el propio participante o un admin de instancia.

Respuesta `201`:

```json
{"token":"ab_…","token_id":"…"}
```

El token anterior deja de funcionar inmediatamente (`401`). La rotación deja
eventos `token.revocar` y `token.emitir` en la cadena `system`.

### `POST /v1/tokens/{id}/revoke`

Revoca un token. Lo puede hacer el dueño del token o un admin de instancia.

```json
{"id":"…","revoked":true,"revoked_at":"2026-11-07T20:10:00.000000000Z"}
```

Errores: `404 token_not_found`, `409 token_already_revoked`,
`403 forbidden`.

## Membresías

### `GET /v1/projects/{id}/members`

Requiere lectura del proyecto. Lista membresías con datos del participante.

```json
{
  "items": [
    {
      "participant_id": "…",
      "name": "backend-1",
      "type": "agente",
      "admin": false,
      "permission": "colaborador",
      "roles": ["backend"],
      "created_at": "2026-11-07T20:05:00.000000000Z"
    }
  ],
  "next_cursor": null
}
```

### `PUT /v1/projects/{id}/members/{participant_id}`

Requiere admin del proyecto o admin de instancia. Crea o actualiza la
membresía.

```json
{"permission":"colaborador","roles":["backend"]}
```

Respuesta `200` con la membresía. El cambio se audita en la cadena del proyecto
como `membresia.cambiar`. Errores: `404 participant_not_found`,
`403 requires_project_admin`, `422 validation_error`.

### `DELETE /v1/projects/{id}/members/{participant_id}`

Requiere admin del proyecto o admin de instancia. Respuesta `200`:

```json
{"removed": true}
```

Errores: `404 membership_not_found`, `403 requires_project_admin`. Se audita
como `membresia.quitar`.

## Modelo de permisos

- **Admin de instancia** (`participant.admin=true`): puede leer y escribir todo,
  crear proyectos y participantes, rotar/revocar tokens y ver todas las
  cadenas.
- **Membresía `lector`**: lee el proyecto, sus miembros y su cadena de auditoría.
- **Membresía `colaborador`**: además podrá escribir tickets en fases
  posteriores.
- **Membresía `admin`**: administra membresías del proyecto.
- Los roles son etiquetas libres por proyecto; no cambian permisos en Fase 1.

## Auditoría

Cada cambio de negocio y su evento van en la misma transacción SQLite. Hay una
cadena `system` y una cadena por proyecto (el `chain_id` es el UUID del
proyecto). El hash es SHA-256 de una serialización canónica con longitud por
campo y `prev_hash`; `seq` es correlativo por cadena. Las escrituras usan
`BEGIN IMMEDIATE` para serializar la cadena.

Eventos principales de Fase 1:

- `bootstrap.humano`
- `participante.crear`
- `proyecto.crear`
- `token.emitir`, `token.revocar`
- `membresia.cambiar`, `membresia.quitar`
- `sesion.abrir`
- `rechazo.auth.sin_token`, `rechazo.auth.token_invalido`,
  `rechazo.auth.sesion_invalida`, `rechazo.permiso`

### `GET /v1/audit/chains`

Lista las cadenas visibles para el actor. Un admin de instancia ve todas; un
participante ve solo las cadenas de sus proyectos.

```json
{"items":[{"chain_id":"system","last_seq":12,"last_hash":"…"},{"chain_id":"…","last_seq":3,"last_hash":"…"}],"next_cursor":null}
```

### `GET /v1/audit`

Filtros opcionales: `chain_id`, `actor_id`, `action`, `object_type`,
`object_id`, `project_id`, `from`, `to` (RFC3339), `limit`, `cursor`.

```json
{
  "items": [
    {
      "id": "…",
      "chain_id": "…",
      "seq": 1,
      "actor_type": "humano",
      "actor_id": "…",
      "action": "proyecto.crear",
      "object_type": "proyecto",
      "object_id": "…",
      "project_id": "…",
      "payload": {"slug":"mi-proyecto"},
      "prev_hash": "",
      "hash": "…",
      "created_at": "2026-11-07T20:00:00.000000000Z"
    }
  ],
  "next_cursor": null
}
```

Un no-admin solo puede consultar cadenas de proyectos donde tiene membresía. La
cadena `system` es solo para admin de instancia. `403 requires_audit_access` en
caso contrario.

### `GET /v1/audit/verify?chain_id=…`

Recalcula la cadena completa y devuelve la primera inconsistencia.

Correcta:

```json
{"ok":true,"chain_id":"system","events":12,"first_error":null}
```

Alterada:

```json
{"ok":false,"chain_id":"system","events":5,"first_error":{"seq":3,"reason":"hash no coincide con el contenido"}}
```

Errores: `400 validation_error` si falta `chain_id`, `404 chain_not_found`,
`403 requires_audit_access`.

## Códigos de error

| HTTP | `code` | Cuándo |
|---:|---|---|
| 400 | `invalid_json` | JSON malformado, campo desconocido o body vacío |
| 401 | `unauthorized` | Token faltante, desconocido o revocado |
| 401 | `invalid_session` | `X-Session-Id` no pertenece al participante |
| 403 | `requires_instance_admin` | Operación reservada al admin de instancia |
| 403 | `requires_project_admin` | Membresía insuficiente para administrar |
| 403 | `requires_project_read` | Sin acceso de lectura al proyecto |
| 403 | `requires_audit_access` | Sin acceso a la cadena pedida |
| 403 | `forbidden` | Token de otro participante |
| 404 | `project_not_found` | Proyecto inexistente |
| 404 | `participant_not_found` | Participante inexistente |
| 404 | `token_not_found` | Token inexistente |
| 404 | `membership_not_found` | Membresía inexistente |
| 404 | `chain_not_found` | Cadena inexistente |
| 409 | `project_slug_taken` | Slug duplicado |
| 409 | `project_prefix_taken` | Prefix duplicado |
| 409 | `participant_name_taken` | Nombre duplicado |
| 409 | `membership_exists` | Membresía duplicada al crear participante |
| 409 | `token_already_revoked` | El token ya estaba revocado |
| 422 | `validation_error` | Body válido pero datos inválidos |
| 500 | `internal_error` | Error interno (sin detalles en la respuesta) |

## Diferencias con la API vieja

- Se usan campos JSON en inglés `snake_case` (`name`, `type`, `permission`,
  `project_id`) en lugar de los nombres en español del backend TypeScript.
  Los nombres de acciones de auditoría conservan el espíritu viejo
  (`proyecto.crear`, `token.emitir`, etc.).
- La autenticación es bearer token para humanos y agentes; no hay login web.
- Los listados usan `limit` + `cursor`, no offsets.
- Los errores agregan `code` además de `error`.
- `POST /v1/participants/{id}/tokens` reemplaza a
  `POST /v1/participants/:id/token` del repo viejo.
- Se eliminan endpoints de UI, Telegram, outbox, contratos y archivado del MVP.

## Fuera de Fase 1

Tickets, dependencias, claims, `ticket_siguiente`, fases, épicas, mensajes,
bandeja, documentos, búsqueda, OpenAPI completo y rate limit quedan para fases
posteriores. Ver `docs/scope.md` y `docs/rewrite-plan.md`.
