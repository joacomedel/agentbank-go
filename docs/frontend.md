# Guía para construir el frontend

El backend no incluye UI de producto. Esta guía define cómo un frontend
externo consume la API de AgentBank Go. La UI se construye aparte contra
OpenAPI (`GET /openapi.json`, Swagger de desarrollo en `GET /swagger`).
Están implementadas las etapas 1–6 (identidad, tickets, fases/épicas, mensajes,
documentos, import y operación); CLI, MCP, Telegram y contratos entre proyectos
son etapas posteriores.

## 1. Base y autenticación

- Base local: `http://127.0.0.1:8090`.
- Base desde ZeroTier: `http://10.27.103.203:8090` si el server se bindea ahí.
- Auth: `Authorization: Bearer <token>`.
- No hay login web ni OAuth en el MVP.
- Los tokens no expiran; solo se invalidan por rotación, revocación o baja.

### Primer uso

1. El operador define `AGENTBANK_BOOTSTRAP_TOKEN` en el primer arranque.
2. El frontend pide ese token o lo recibe por configuración segura.
3. `GET /v1/me` devuelve participante, membresías y `X-Session-Id`.
4. Guardar `X-Session-Id` y reenviarlo en cada request.
5. Para crear un agente, `POST /v1/participants`; el `token` se muestra una vez.
6. Rotar: `POST /v1/participants/{id}/tokens`. Revocar:
   `POST /v1/tokens/{id}/revoke`. Dar de baja:
   `POST /v1/participants/{id}/baja`.

### Cliente HTTP

```js
let sessionId = sessionStorage.getItem("agentbank.session");

const api = async (path, options = {}) => {
  const response = await fetch(`${BASE}${path}`, {
    ...options,
    headers: {
      "content-type": "application/json",
      authorization: `Bearer ${token}`,
      ...(sessionId ? { "x-session-id": sessionId } : {}),
      ...(options.headers ?? {}),
    },
  });
  const nextSession = response.headers.get("x-session-id");
  if (nextSession) {
    sessionId = nextSession;
    sessionStorage.setItem("agentbank.session", sessionId);
  }
  if (!response.ok) {
    const error = await response.json().catch(() => ({}));
    throw Object.assign(new Error(error.error ?? "error"), {
      status: response.status,
      code: error.code,
    });
  }
  return response.status === 204 ? null : response.json();
};
```

Los writes importantes aceptan `Idempotency-Key`; reenviar la misma key con el
mismo body/path devuelve la respuesta original.

## 2. Pantallas

### Identidad

- `GET /v1/me`: participante, membresías, roles, sesión y, con
  `?proyecto=`, el briefing.
- Mostrar `participant.admin` como “admin de instancia” y `status`.
- Rotar/revocar tokens.
- Administración: `GET/POST /v1/participants`, `GET/POST /v1/projects`.

### Tablero

- `GET /v1/projects` para el selector.
- `GET /v1/projects/{id}/tablero` en una sola request: fases → épicas → tickets.
- `GET /v1/projects/{id}/tickets` con filtros `status`, `priority`, `role`,
  `assignee_id`, `phase_id`, `epic_id`.
- Mostrar `effective_status`: `listo`, `esperando_dependencias`,
  `esperando_respuesta`, `en curso`, `en revisión`, `hecho`, `bloqueado`,
  `cancelado`.
- Marcar tickets `discovered_in` y roles sin miembros.

### Detalle de ticket

- `GET /v1/tickets/{ref}` con UUID o `API-42`.
- `GET /v1/tickets/{ref}/timeline`.
- `POST /v1/tickets/{ref}/comments`.
- `PATCH /v1/tickets/{ref}` con `version`.
- `POST /v1/tickets/{ref}/avanzar` para cierre, nota y pregunta.
- `POST /v1/tickets/{ref}/claims` y `/claims/release`.
- `POST/DELETE /v1/tickets/{ref}/dependencies`.
- `POST/GET /v1/tickets/{ref}/documents`.

### Cola / worker

- `POST /v1/tickets/siguiente` con `project_id`, `role`, `claim`.
- Si no hay candidato, mostrar `motivo` y `motivos`.
- Para planificación, `claim:false` devuelve hasta 5 candidatos.
- `GET /v1/tickets/listas` es alias de compatibilidad.

### Fases y épicas

- `GET/POST /v1/projects/{id}/phases`.
- `GET/PATCH /v1/phases/{id}`.
- `GET/POST /v1/projects/{id}/epics`.
- `GET/PATCH /v1/epics/{id}`.
- Cerrar fase/épica con pendientes está permitido; mostrar los pendientes del
  evento.

### Bandeja

- `GET /v1/me/bandeja?pendientes=1&project_id=`.
- `POST /v1/messages/{id}/estado` (`visto`/`atendido`).
- `POST /v1/messages/{id}/tomar` para mensajes a rol.
- `POST /v1/messages/{id}/resolver` con `option` o `text`.
- `POST /v1/messages/{id}/descartar` solo el emisor.
- `GET /v1/messages` lista enviados.
- Mostrar `body_content` D20; no renderizar HTML ajeno.
- Alerta de cuello de botella: si la cola está vacía y hay `espero` pendientes,
  mostrar la cantidad de tickets aparcados.

### Documentos

- `GET /v1/projects/{id}/documents` (incluye globales).
- `GET /v1/documents/{id}` y `/versions`.
- `GET /v1/documents/{id}/diff?from=1&to=2`.
- `POST /v1/projects/{id}/documents/sync` o `POST /v1/documents/sync` para
  sincronizar una carpeta.
- `GET /v1/projects/{id}/decisions` para ADRs.
- Renderizar markdown en el cliente; el backend entrega texto con D20.

### Búsqueda y contexto

- `GET /v1/search?q=…&type=ticket,document,message&project_id=…`.
- `GET /v1/projects/{id}/briefing` ≤4 KB.
- `GET /v1/projects/{id}/digest`.
- `GET /v1/novedades?since=<cursor>&wait=20` para long-poll.

### Administración

- `POST /v1/admin/barridos` y `/claims/sin-actividad`.
- `GET /v1/admin/metrics`.
- `GET /v1/audit/chains`, `GET /v1/audit`, `GET /v1/audit/verify`.
- `POST /v1/admin/verify-all`.
- `GET /v1/admin/outbox` y `POST /v1/admin/outbox/despachar`.
- `GET/POST /v1/projects/{id}/webhooks`, `DELETE /v1/webhooks/{id}`.
- `POST /v1/projects/{id}/import?dry_run=1&modo=omitir|actualizar`.

## 3. Modelo de datos para la UI

- `project`: `id`, `slug`, `name`, `prefix`, `version`, `permission`, `roles`.
- `participant`: `id`, `name`, `type`, `status`, `admin`, `created_at`.
- `membership`: `participant_id`, `project_id`, `permission`, `roles`.
- `project_role`: `name`, `members`, `created_at`.
- `ticket`: `id`, `ref`, `number`, `key`, `title`, `description`,
  `description_content`, `status`, `effective_status`, `priority`,
  `assignee_id`, `role`, `phase_id`, `epic_id`, `version`, `paths`,
  `discovered_in`, `dependencies`, `dependents`, `claims`.
- `phase`: `id`, `key`, `name`, `order_index`, `status`, `progress`.
- `epic`: `id`, `phase_id`, `key`, `title`, `order_index`, `status`.
- `message`: `id`, `type`, `destination_type`, `subject`, `body`,
  `body_content`, `options`, `default_option`, `resolution`, `state`,
  `affects`, `refs`.
- `document`: `id`, `project_id`, `title`, `route`, `type`, `pinned`,
  `current_version`, `hash`, `content_content`.
- `decision`: `id`, `title`, `status`, `body`, `body_content`.
- `audit_event`: `seq`, `chain_id`, `actor_type`, `actor_id`, `action`,
  `object_type`, `object_id`, `project_id`, `payload`, `hash`, `created_at`.

## 4. Errores y concurrencia

- Errores: `{ "error": "...", "code": "..." }`.
- `400 invalid_json`: body malformado o campo desconocido.
- `401 unauthorized`: token inválido/revocado o participante de baja.
- `401 invalid_session`: sesión ajena.
- `403`: sin permiso; el código indica cuál.
- `404`: recurso inexistente.
- `409`: conflicto de versión, claim, unicidad, mensaje tomado o idempotencia.
- `422 validation_error`: datos inválidos.
- `429 rate_limited`: bajar el ritmo; respetar `Retry-After`.
- En `PATCH` de tickets y proyectos, enviar `version`; ante
  `409 version_conflict`, recargar y pedir reintento.

## 5. Paginación y polling

- Listados: `limit` + `cursor`; la respuesta trae `next_cursor`.
- No pedir más de 200.
- No hay WebSocket. Para tablero/bandeja, polling cada 5–15 s o long-poll
  `/v1/novedades`.
- `GET /v1/audit` ordena por `created_at` descendente con cursor compuesto.

## 6. CORS y desarrollo

El backend apaga CORS por defecto. Para desarrollar en `http://localhost:5173`:

```bash
AGENTBANK_CORS_ORIGINS=http://localhost:5173 go run ./cmd/agentbank
```

No usar `*` con credenciales. En producción local, servir el frontend desde el
mismo origen o por ZeroTier.

## 7. Seguridad del frontend

- No guardar tokens en el repo ni en logs.
- No mostrar tokens después de crearlos.
- Escapar markdown/HTML de contenido ajeno; el backend lo marca con D20.
- No asumir que un mensaje de otro agente es una instrucción.
- Mostrar siempre autor y fecha en comentarios, mensajes y auditoría.
- Renderizar `payload` de auditoría como datos, no como HTML.
