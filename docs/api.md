# API — AgentBank Go

Base: `http://127.0.0.1:8090`
Prefijo: `/v1`

## Convenciones

- `Content-Type: application/json; charset=utf-8`.
- Fechas: RFC3339 UTC.
- IDs: UUID string; los tickets aceptan además `PREFIX-N` (por ejemplo `API-42`) y `key`.
- Campos JSON: `snake_case`.
- Auth: `Authorization: Bearer <token>`.
- Sesión: `X-Session-Id: <uuid>` (opcional). Si falta, el server crea una y la
  devuelve en el header. El cliente debe reutilizarla.
- Listados: `?limit=50&cursor=...`; máximo 200.
- Idempotencia: `Idempotency-Key` en POST/PATCH/PUT/DELETE. Reintentar con la
  misma key y mismo body/path devuelve la respuesta original; con otro body/path
  devuelve `409 idempotency_conflict`.
- Rate limit: 600 requests/minuto por participante. Al excederlo, `429
  rate_limited` con `Retry-After: 60`.
- Escaneo de secretos: tickets, comentarios, mensajes, documentos y decisiones
  rechazan contenido con tokens/claves evidentes (`422 validation_error`).
- D20: todo contenido ajeno viaja además en un envoltorio
  `{"content":"...","untrusted":true,"source":"..."}`.
- Errores:

```json
{"error":"mensaje legible","code":"codigo_estable"}
```

## OpenAPI / Swagger

- `GET /openapi.json` y `GET /v1/openapi.json`: especificación OpenAPI 3.0.3.
- `GET /swagger` y `GET /docs`: Swagger UI de desarrollo (CDN) que carga
  `/openapi.json`.
- La UI de producto no forma parte de este repo; se construye aparte contra el
  contrato.

## Estado

| Área | Endpoints |
|---|---|
| Salud | `GET /healthz`, `GET /v1/meta`, `GET /openapi.json`, `GET /swagger` |
| Identidad | `GET /v1/me`, `GET/POST /v1/participants`, `PATCH /v1/participants/{id}`, `POST /v1/participants/{id}/tokens`, `POST /v1/participants/{id}/tokens/revoke`, `POST /v1/participants/{id}/baja`, `POST /v1/tokens/{id}/revoke`, `POST /v1/me/heartbeat` |
| Proyectos | `GET/POST /v1/projects`, `GET/PATCH /v1/projects/{id}`, `POST /v1/projects/{id}/pausa`, `POST /v1/projects/{id}/seguir` |
| Membresías | `GET /v1/projects/{id}/members`, `PUT/DELETE /v1/projects/{id}/members/{participant_id}`, `GET /v1/projects/{id}/roles` |
| Tickets | `GET/POST /v1/projects/{id}/tickets`, `GET/PATCH /v1/tickets/{id}`, `POST /v1/tickets/{id}/avanzar`, `POST /v1/tickets/siguiente`, `GET /v1/tickets/listas`, `POST /v1/tickets/{id}/claims`, `POST /v1/tickets/{id}/claims/release`, `POST/DELETE /v1/tickets/{id}/dependencies`, `POST/GET /v1/tickets/{id}/comments`, `GET /v1/tickets/{id}/timeline`, `POST/GET /v1/tickets/{id}/documents` |
| Fases/épicas | `GET/POST /v1/projects/{id}/phases`, `GET/PATCH /v1/phases/{id}`, `GET/POST /v1/projects/{id}/epics`, `GET/PATCH /v1/epics/{id}` |
| Mensajes | `POST/GET /v1/messages`, `GET /v1/messages/{id}`, `GET /v1/me/bandeja`, `POST /v1/messages/{id}/estado`, `POST /v1/messages/{id}/visto`, `POST /v1/messages/{id}/acusar`, `POST /v1/messages/{id}/tomar`, `POST /v1/messages/{id}/resolver`, `POST /v1/messages/{id}/responder`, `POST /v1/messages/{id}/descartar`, `POST /v1/messages/{id}/convertir`, `POST /v1/messages/{id}/handoff/aceptar`, `POST /v1/messages/{id}/handoff/rechazar` |
| Docs | `GET/POST /v1/projects/{id}/documents`, `POST /v1/projects/{id}/documents/sync`, `GET/POST /v1/documents`, `POST /v1/documents/sync`, `GET /v1/documents/{id}`, `POST/GET /v1/documents/{id}/versions`, `GET /v1/documents/{id}/diff` |
| Decisiones | `GET/POST /v1/projects/{id}/decisions` |
| Búsqueda/contexto | `GET /v1/search`, `GET /v1/projects/{id}/briefing`, `GET /v1/projects/{id}/digest`, `GET /v1/projects/{id}/tablero`, `GET /v1/novedades`, `GET /v1/changes` |
| Import | `POST /v1/projects/{id}/import` |
| Auditoría | `GET /v1/audit/chains`, `GET /v1/audit`, `GET /v1/audit/verify`, `POST /v1/admin/verify-all` |
| Operación | `POST /v1/admin/barridos`, `POST /v1/admin/claims/sin-actividad`, `GET /v1/admin/metrics`, `GET /v1/admin/outbox`, `POST /v1/admin/outbox/despachar`, `GET/POST /v1/projects/{id}/webhooks`, `DELETE /v1/webhooks/{id}` |

## Salud y meta

### `GET /healthz`

```json
{"ok": true, "db": "ok"}
```

### `GET /v1/meta`

```json
{"name":"agentbank-go","version":"0.1.0","api_version":"v1","database":"sqlite","features":["identity","projects","participants","memberships","tokens","sessions","audit"]}
```

## Autenticación y participantes

En el primer arranque, si no existe un humano, el server exige
`AGENTBANK_BOOTSTRAP_TOKEN` (mínimo 32 caracteres), crea el humano `admin` de
instancia y guarda solo `sha256(token)`. Si ya existe un humano, la variable se
ignora. Los tokens no expiran; solo se invalidan por rotación, revocación o baja.
Un participante tiene como máximo un token activo.

### `GET /v1/me`

```json
{
  "participant": {"id":"…","type":"humano","name":"humano","status":"activo","agent_role":"","admin":true,"created_at":"…"},
  "memberships": [{"project_id":"…","project_slug":"…","project_name":"…","permission":"admin","roles":["dueño"],"created_at":"…"}],
  "session_id": "…"
}
```

Con `?proyecto=<id|slug>` agrega `briefing`.

### `GET /v1/participants?activos=1&type=agente&role=auditor&name=auditor-1`

Lista participantes. Filtros: `activos=1`, `type`, `role` (agent_role),
`name`, `limit`, `cursor`. No expone tokens. Cada participante incluye
`agent_role`.

### `POST /v1/participants`

Requiere admin de instancia. Crea humano/agente, emite token una sola vez y
asigna membresías en la misma transacción.

```json
{"name":"backend-1","type":"agente","agent_role":"backend","admin":false,"projects":[{"project_id":"…","permission":"colaborador","roles":["backend"]}]}
```

```json
{"participant":{"id":"…","type":"agente","name":"backend-1","status":"activo","agent_role":"backend","admin":false,"created_at":"…"},"token":"ab_…","token_id":"…"}
```

### `PATCH /v1/participants/{id}`

Requiere admin de instancia. Actualiza el rol funcional del agente.

```json
{"agent_role":"auditor"}
```

### `POST /v1/participants/{id}/tokens`

Rota el token: revoca el activo y devuelve el nuevo una vez.

### `POST /v1/participants/{id}/tokens/revoke`

Revoca el token activo del participante sin emitir uno nuevo.

### `POST /v1/tokens/{id}/revoke`

Revoca un token. `409 token_already_revoked` si ya estaba revocado.

### `POST /v1/participants/{id}/baja`

Requiere admin de instancia. Pone `status='baja'` y revoca todos sus tokens.

### `POST /v1/me/heartbeat`

Actualiza actividad. El heartbeat también es implícito en cada request
autenticado, con un máximo de una escritura por minuto.

## Proyectos y membresías

### `POST /v1/projects`

Requiere admin de instancia. El creador recibe membresía `admin` con rol `dueño`.

```json
{"slug":"mi-proyecto","name":"Mi proyecto","prefix":"MIP"}
```

### `GET /v1/projects`

Solo proyectos legibles. Admin de instancia ve todos.

### `GET /v1/projects/{id}`

Acepta UUID o slug. Devuelve `permission` y `roles` efectivos.

### `PATCH /v1/projects/{id}`

Cualquier participante autenticado puede editar metadatos. `version` opcional.

```json
{"version":1,"name":"Nuevo nombre","slug":"nuevo-slug","prefix":"NUEVO"}
```

`409 version_conflict` si la versión no coincide. Se audita `proyecto.editar`.

### `POST /v1/projects/{id}/pausa` / `POST /v1/projects/{id}/seguir`

Requiere admin del proyecto o de instancia. `ticket_siguiente` respeta la pausa.

### Membresías

- `GET /v1/projects/{id}/members`: requiere lectura.
- `PUT /v1/projects/{id}/members/{participant_id}`: requiere admin del proyecto.
  Body `{"permission":"colaborador","roles":["backend"]}`.
- `DELETE /v1/projects/{id}/members/{participant_id}`: requiere admin.
- `GET /v1/projects/{id}/roles`: catálogo con cantidad de miembros. Los roles se
  auto-registran al asignarse.

## Tickets y cola

### `POST /v1/projects/{id}/tickets`

Requiere escritura en el proyecto.

```json
{
  "title":"Implementar login",
  "description":"Detalle",
  "priority":"alta",
  "role":"backend",
  "key":"auth-1",
  "order_index":1,
  "paths":["src/auth"],
  "epic_id":"…",
  "phase_id":"…",
  "discovered_in":"API-3",
  "depends_on":["API-1","API-2"]
}
```

Reglas:
- Un ticket con `epic_id` no acepta `phase_id`; la fase efectiva sale de la épica.
- Las dependencias detectan ciclos y se rechazan.
- `priority`: `baja|media|alta|urgente`.
- `status` inicial: `abierto` por defecto.
- El ticket recibe `number` y `ref = PREFIX-N`.

Respuesta `201`:

```json
{
  "id":"…","project_id":"…","ref":"API-1","number":1,"key":"auth-1",
  "title":"…","description":"…","description_content":{"content":"…","untrusted":true,"source":"ticket_description"},
  "status":"abierto","effective_status":"listo","priority":"alta",
  "role":"backend","phase_id":"","epic_id":"…","order_index":1,"version":1,
  "created_by":"…","created_by_role":"auditor",
  "paths":["src/auth"],"dependencies":[],"dependents":[],"claims":[],"comments_count":0,
  "created_at":"…","updated_at":"…"
}
```

`effective_status` puede ser `listo`, `esperando_dependencias`,
`esperando_respuesta`, `en curso`, `en revisión`, `hecho`, `bloqueado` o
`cancelado`.

### `GET /v1/projects/{id}/tickets`

Filtros: `status`, `priority`, `role`, `assignee_id`, `phase_id`, `epic_id`,
`key`, `limit`, `cursor`. Orden: más nuevos primero.
Con `updated_since=<RFC3339>` devuelve solo tickets cambiados desde esa fecha,
ordenados por `updated_at` ascendente; `next_cursor` sirve para la página
siguiente sin volver a listar todo.

### `GET /v1/tickets/{id}`

Acepta UUID, `PREFIX-N` o `key`. Devuelve dependencias, dependientes, claims
activos y cantidad de comentarios.

### `PATCH /v1/tickets/{id}`

Requiere `version`. Permite editar `title`, `description`, `status`, `priority`,
`assignee_id`, `role`, `phase_id`, `epic_id`, `order_index`, `paths`,
`discovered_in`. `409 version_conflict` si la versión no coincide.

### `POST /v1/tickets/{id}/avanzar`

Cierra o avanza un ticket y puede dejar nota de cierre y pregunta.

```json
{
  "version":3,
  "status":"hecho",
  "nota_cierre":"Qué hice, archivos tocados, qué quedó pendiente",
  "comment":"Comentario adicional",
  "pregunta":{
    "asunto":"¿Cómo seguimos?",
    "cuerpo":"…",
    "opciones":["A","B"],
    "default_opcion":0,
    "modo":"espero",
    "motivo":"consultar_siempre",
    "destino_tipo":"rol",
    "destino_rol":"dueño"
  },
  "y_siguiente":true
}
```

`modo`: `sigo`, `divido` o `espero`. `espero` requiere `motivo` y deja el ticket
afectado fuera de la cola hasta que se resuelva. `divido` se comporta como
`sigo` en esta versión (no crea ticket hijo todavía). Respuesta incluye el
ticket, el mensaje creado y `next` si `y_siguiente=true`.

### `POST /v1/tickets/siguiente`

```json
{"project_id":"…","role":"backend","phase_id":"…","epic_id":"…","claim":true}
```

- Sin `project_id`, busca en todos los proyectos donde el participante es
  colaborador/admin.
- Solo tickets `abierto`, sin claim activo, sin dependencias pendientes y sin
  mensajes pendientes que los afecten.
- Orden: fase → épica → ticket → prioridad → antigüedad.
- Con `claim=false` devuelve hasta 5 candidatos.
- Con `claim=true` toma el claim y marca el ticket `en curso`.
- Respuesta incluye `motivo`, `motivos`, `claims_activos` y `contexto`
  (notas de cierre de dependencias, resoluciones de mensajes y docs).

### Claims

- `POST /v1/tickets/{id}/claims`: requiere ticket listo. `409 ticket_no_listo` o
  `409 claim_conflict`. Lease de 30 minutos.
- `POST /v1/tickets/{id}/claims/release`: libera el claim propio o de un admin.
- Los claims vencidos se liberan con `POST /v1/admin/barridos`.

### Dependencias

- `POST /v1/tickets/{id}/dependencies` con `{"depends_on":"API-1"}`.
- `DELETE /v1/tickets/{id}/dependencies/{depends_on}`.
- Detección de ciclos; se audita `ticket.dependencia.crear/quitar`.

### Comentarios y timeline

- `POST /v1/tickets/{id}/comments` con `{"body":"…","type":"comentario|nota|cierre"}`.
- `GET /v1/tickets/{id}/comments`.
- `GET /v1/tickets/{id}/timeline`: comentarios + eventos de auditoría del ticket.

### `GET /v1/tickets/listas`

Compatibilidad: devuelve candidatos sin reclamar (`claim=false`).

## Fases y épicas

### Fases

- `POST /v1/projects/{id}/phases` con `{"key":"f1","name":"Fundaciones","order_index":1}`.
- `GET /v1/projects/{id}/phases`.
- `GET /v1/phases/{id}`: fase, épicas y tickets sueltos, con `progress`.
- `PATCH /v1/phases/{id}`: `name`, `description`, `order_index`, `status`.
  Cerrar una fase con pendientes está permitido y el evento registra
  `pendientes`.

### Épicas

- `POST /v1/projects/{id}/epics` con `{"key":"e1","title":"Auth","phase_id":"…"}`.
- `GET /v1/projects/{id}/epics?phase_id=…`.
- `GET /v1/epics/{id}`: épica con tickets.
- `PATCH /v1/epics/{id}`: título, descripción, fase, orden, estado. Mover de
  fase se audita como `epic.mover_fase`; cerrar con pendientes como
  `epica.cerrar`.

## Mensajes y bandeja

### `POST /v1/messages`

```json
{
  "project_id":"…",
  "type":"solicitud",
  "destination_type":"rol",
  "destination_role":"dueño",
  "subject":"¿Qué base usamos?",
  "body":"…",
  "options":["postgres","sqlite"],
  "default_option":1,
  "assume_default_at":"2026-11-08T02:00:00Z",
  "affects":["API-42"],
  "refs":[{"object_type":"ticket","object_id":"…"}],
  "reply_to":"…"
}
```

- `type`: `info` o `solicitud`.
- `destination_type`: `participante`, `rol` o `proyecto`.
- `info` no acepta opciones ni `affects`.
- `default_option` requiere `options`; `assume_default_at` requiere default.
- `options`: hasta 10, sin duplicados.
- `affects` solo aplica a solicitudes y saca el ticket de `listo`.
- Respuesta incluye `body_content` D20.

### `GET /v1/me/bandeja?pendientes=1&project_id=`

Bandeja del participante. Incluye mensajes directos, a sus roles y al proyecto.
Excluye los enviados por el propio participante.

### `GET /v1/messages`

Lista mensajes enviados por el participante. Filtro `project_id`, `limit`,
`cursor`.

### `GET /v1/messages/{id}`

Requiere poder ver el mensaje (emisor, destinatario, rol o miembro del proyecto).

### Estados

- `POST /v1/messages/{id}/estado` con `{"state":"visto|atendido"}`.
- Alias: `POST /v1/messages/{id}/visto` y `POST /v1/messages/{id}/acusar`.

### `POST /v1/messages/{id}/tomar`

Solo mensajes a rol. El primero que lo toma queda en `tomado_por`;
`409 message_taken` si ya lo tomó otro.

### `POST /v1/messages/{id}/resolver`

```json
{"option":1}
```

o `{"text":"…"}`. Alias: `POST /v1/messages/{id}/responder`.
Resolver una solicitud que afectaba un ticket lo devuelve a `listo`.
Si la solicitud había asumido un default y la respuesta difiere, se crea un
ticket `Corregir: <asunto>` asignado al emisor, prioridad alta.

### `POST /v1/messages/{id}/descartar`

Solo el emisor, mientras no esté resuelto.

### `POST /v1/messages/{id}/convertir`

```json
{"to":"decision|ticket","title":"…","body":"…"}
```

Crea un ADR o un ticket enlazado al mensaje.

### Handoff

`POST /v1/messages/{id}/handoff/aceptar` y `/rechazar` son alias que resuelven
con texto `aceptar`/`rechazar`.

### Barrido de defaults

`POST /v1/admin/barridos` asume defaults vencidos (`default_asumido`) y libera
claims vencidos. Es idempotente.

## Documentos, decisiones y búsqueda

### Documentos

- `POST /v1/projects/{id}/documents`:
  `{"title":"Arquitectura","route":"docs/arquitectura.md","type":"doc","pinned":true,"content":"# …"}`
- `GET /v1/projects/{id}/documents`: docs del proyecto + globales. Acepta
  `updated_since=<RFC3339>` para traer solo metadatos de docs cambiados.
- `POST /v1/documents` y `GET /v1/documents`: docs globales (escritura solo
  admin de instancia; lectura para todos).
- `GET /v1/documents/{id}?version=N`.
- `POST /v1/documents/{id}/versions` con `{"content":"…"}`. Si el hash no
  cambió, no crea versión.
- `GET /v1/documents/{id}/versions`.
- `GET /v1/documents/{id}/diff?from=1&to=2`: diff por líneas.
- `POST /v1/projects/{id}/documents/sync` y `POST /v1/documents/sync`:
  `{"documents":[{"route":"docs/x.md","title":"X","content":"…"}]}`. Crea o
  versiona solo si cambió el hash.
- `POST /v1/tickets/{id}/documents` con `{"document_id":"…"}`.
- `GET /v1/tickets/{id}/documents`.

### Decisiones/ADRs

- `POST /v1/projects/{id}/decisions` con `{"title":"…","body":"…","source_message_id":"…"}`.
- `GET /v1/projects/{id}/decisions`.

### `GET /v1/search?q=…&type=ticket,document,message&project_id=…`

Búsqueda simple con `LIKE`, permisos por proyecto y snippets D20.

## Briefing, digest y tablero

- `GET /v1/projects/{id}/briefing`: fase activa, progreso, decisiones, docs
  fijados, claims propios, próximos 5 tickets, bandeja pendiente y roles sin
  miembros. Tope 4 KB; no audita.
- `GET /v1/projects/{id}/digest`: contadores y 10 eventos recientes.
- `GET /v1/projects/{id}/tablero`: fases → épicas → tickets y tickets sueltos.

## Import

### `POST /v1/projects/{id}/import?dry_run=1&modo=omitir|actualizar`

```json
{
  "fases":[{"key":"f1","nombre":"Fundaciones","orden":1}],
  "epicas":[{"key":"auth","fase":"f1","titulo":"Auth","orden":1}],
  "tickets":[{"key":"auth-1","epica":"auth","titulo":"…","prioridad":"alta","orden":1,"rol":"backend","depende_de":["auth-0"]}]
}
```

- Todo o nada, máximo 500 tickets.
- `dry_run` no persiste nada.
- `omitir` salta claves existentes; `actualizar` actualiza.
- Detecta ciclos antes de confirmar.
- Audita `proyecto.importar` y eventos por objeto.

## Auditoría

Cada cambio de negocio y su evento van en la misma transacción. Hay cadena
`system` y una por proyecto. Hash SHA-256 con `prev_hash` y `seq` por cadena.
Las escrituras usan `BEGIN IMMEDIATE`.

### `GET /v1/audit/chains`

Lista cadenas visibles.

### `GET /v1/audit`

Filtros: `chain_id`, `actor_id`, `action`, `object_type`, `object_id`,
`project_id`, `from`, `to`, `limit`, `cursor`. Devuelve también `session_id`,
`origin`, `correlation_id` y `parent_event_id`.

### `GET /v1/audit/verify?chain_id=…`

```json
{"ok":true,"chain_id":"system","events":12,"first_error":null}
```

```json
{"ok":false,"chain_id":"…","events":5,"first_error":{"seq":3,"reason":"hash no coincide con el contenido"}}
```

### `POST /v1/admin/verify-all`

Verifica todas las cadenas. Solo admin de instancia.

### `GET /v1/novedades?since=<cursor>&wait=20`

Long-poll de eventos posteriores al cursor. `wait` máximo 30 s. No muta nada.

### `GET /v1/changes?since=<cursor>&project_id=…&limit=100`

Feed incremental y deduplicado de objetos cambiados: tickets, mensajes, docs,
fases, épicas, decisiones y participantes. Cada item trae `object_type`,
`object_id`, `action`, `project_id`, `changed_at` y un `summary` con datos
básicos (por ejemplo `ref`, `status`, `version` del ticket). Una sola llamada
alcanza para saber qué cambió; el contenido completo se pide solo para los
objetos que interesan.

```json
{
  "items": [
    {
      "object_type": "ticket",
      "object_id": "…",
      "action": "ticket.editar",
      "project_id": "…",
      "changed_at": "2026-11-08T00:00:00Z",
      "summary": {"ref":"API-42","title":"…","status":"en curso","version":3,"updated_at":"…"}
    }
  ],
  "next_cursor": "…"
}
```

Sin `since`, devuelve los últimos cambios y un cursor para continuar. Ver
`docs/mcp-flows.md` para los flujos y conteos de llamadas.

## Operación

### `POST /v1/admin/barridos` / `POST /v1/admin/claims/sin-actividad`

Libera claims vencidos y asume defaults vencidos. Solo admin de instancia.
Respuesta: `{"claims_liberados":N,"defaults_asumidos":M}`.

### `GET /v1/admin/metrics`

Contadores de tablas, claims activos y outbox pendiente.

### `GET /v1/admin/outbox?status=pendiente`

Lista entregas. Solo admin de instancia.

### `POST /v1/admin/outbox/despachar`

Intenta entregar pendientes con backoff y dead-letter a los 10 intentos.

### Webhooks

- `POST /v1/projects/{id}/webhooks` con
  `{"url":"https://…","secret":"…","events":["ticket.crear","ticket.*"]}`.
  Requiere admin del proyecto.
- `GET /v1/projects/{id}/webhooks`.
- `DELETE /v1/webhooks/{id}`.
- Los webhooks se encolan en outbox para `ticket.crear`, `ticket.editar`,
  `ticket.comentar` y `mensaje.enviar`.
- Si hay `secret`, se envía `X-AgentBank-Signature: sha256=<hmac>`.

## Códigos de error

| HTTP | `code` | Cuándo |
|---:|---|---|
| 400 | `invalid_json` | JSON malformado, campo desconocido o body vacío |
| 400 | `invalid_idempotency_key` | Key demasiado larga |
| 401 | `unauthorized` | Token faltante, desconocido, revocado o participante de baja |
| 401 | `invalid_session` | `X-Session-Id` ajeno |
| 403 | `requires_instance_admin` | Operación reservada al admin de instancia |
| 403 | `requires_project_admin` | Membresía insuficiente para administrar |
| 403 | `requires_project_read` | Sin lectura del proyecto |
| 403 | `requires_project_write` | Sin escritura del proyecto |
| 403 | `requires_audit_access` | Sin acceso a la cadena |
| 403 | `forbidden` | Operación sobre recurso ajeno |
| 404 | `project_not_found`, `participant_not_found`, `ticket_not_found`, `phase_not_found`, `epic_not_found`, `message_not_found`, `document_not_found`, `document_version_not_found`, `claim_not_found`, `membership_not_found`, `token_not_found`, `chain_not_found`, `webhook_not_found` | Recurso inexistente |
| 409 | `version_conflict` | Versión desactualizada |
| 409 | `claim_conflict` | Ticket ya reclamado |
| 409 | `ticket_no_listo` | Claim sobre ticket no listo |
| 409 | `dependency_exists` | Dependencia duplicada |
| 409 | `message_taken` | Mensaje a rol ya tomado |
| 409 | `message_already_resolved` | Mensaje ya resuelto/descartado |
| 409 | `idempotency_conflict` | Misma key, distinto request |
| 409 | `project_slug_taken`, `project_prefix_taken`, `participant_name_taken`, `token_already_revoked`, `phase_key_taken`, `epic_key_taken`, `ticket_key_taken`, `document_route_taken` | Unicidad/estado |
| 422 | `validation_error` | Datos inválidos |
| 429 | `rate_limited` | Rate limit |
| 500 | `internal_error` | Error interno |

## Diferencias y límites de esta etapa

- Contrato nuevo en inglés `snake_case`; el repo viejo usaba español.
- SQLite en lugar de Postgres; sin `jsonb`, `text[]`, `tsvector` ni triggers.
- Búsqueda con `LIKE`, no FTS5.
- `divido` de `ticket_avanzar` no crea ticket hijo todavía.
- Handoff se resuelve como mensaje con texto, sin estado estructurado.
- Telegram, CLI, MCP, UI, contratos entre proyectos, bloqueos/escalado y choque
  de áreas quedan para las etapas 7–9.
- Retención/export queda fuera por decisión de V3.1.
