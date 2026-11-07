# Guía para construir el frontend

El backend no incluye UI. Esta guía define cómo un frontend futuro consume la
API de AgentBank Go.

## 1. Base y autenticación

- Base local: `http://127.0.0.1:8090`
- Base desde ZeroTier: `http://10.27.103.203:8090` (si el server se bindea ahí).
- Auth: `Authorization: Bearer <token>`.
- El token humano se obtiene una vez con el bootstrap del backend y se guarda
  en el almacenamiento seguro del navegador (no en `localStorage` si se puede
  evitar; para un MVP local, `sessionStorage` es aceptable).
- No hay login web: el frontend pide el token al usuario o lo recibe por
  configuración. No se implementa OAuth en el MVP.
- Sesión: enviar `X-Session-Id` estable por pestaña para que la auditoría y la
  bandeja se comporten bien.

```js
const api = (path, options = {}) =>
  fetch(`${BASE}${path}`, {
    ...options,
    headers: {
      "content-type": "application/json",
      authorization: `Bearer ${token}`,
      "x-session-id": sessionId,
      ...(options.headers ?? {}),
    },
  });
```

## 2. Pantallas y endpoints

### Tablero

- `GET /v1/projects` para el selector.
- `GET /v1/projects/{id}/tickets?limit=200` para columnas/lista.
- `GET /v1/projects/{id}/phases` y `/epics` para agrupar.
- Mostrar estado efectivo: listo, esperando dependencias, esperando respuesta,
  en curso, hecho.
- Alertar tickets aparcados por preguntas `espero`.

### Detalle de ticket

- `GET /v1/tickets/{id}`.
- `GET /v1/tickets/{id}/timeline`.
- `POST /v1/tickets/{id}/comments`.
- `PATCH /v1/tickets/{id}` con `version` para evitar pisadas.
- `POST /v1/tickets/{id}/claims` y `/claims/release`.

### Bandeja

- `GET /v1/me/bandeja?pendientes=1`.
- `POST /v1/messages/{id}/resolver` con `option` o `text`.
- `POST /v1/messages/{id}/estado` para marcar visto/atendido.

### Documentos

- `GET /v1/projects/{id}/documents`.
- `GET /v1/documents/{id}` y `/versions`.
- `GET /v1/documents/{id}/diff?from=1&to=2`.
- Renderizar markdown en el cliente; el backend entrega texto.

### Auditoría

- `GET /v1/audit/chains`.
- `GET /v1/audit?chain_id=...`.
- `GET /v1/audit/verify?chain_id=...` y mostrar el resultado.

## 3. Modelo de datos para la UI

- `project`: `id`, `slug`, `name`, `prefix`.
- `participant`: `id`, `name`, `type` (`humano`/`agente`), `admin`.
- `ticket`: `id`, `ref` (`MIP-42`), `title`, `description`, `status`,
  `effective_status`, `priority`, `assignee`, `role`, `version`, `created_at`,
  `updated_at`.
- `comment`: `id`, `ticket_id`, `author`, `body`, `created_at`.
- `message`: `id`, `type`, `subject`, `body`, `options`, `default_option`,
  `status`, `affects`, `created_at`.
- `document`: `id`, `title`, `format`, `current_version`, `hash`.
- `audit_event`: `seq`, `actor`, `action`, `object_type`, `object_id`,
  `created_at`.

## 4. Errores y concurrencia

- Errores: `{ "error": "...", "code": "..." }`.
- `401` token inválido/revocado.
- `403` sin membresía/permiso.
- `404` recurso inexistente o no visible.
- `409` conflicto de claim, versión o unicidad.
- `422` validación.
- `429` rate limit.
- En `PATCH`, enviar `version`; si el server responde `409 version_conflict`,
  recargar y pedir al usuario que reintente.

## 5. Paginación y polling

- Listados: `limit` + `cursor`; la respuesta trae `next_cursor`.
- No hay WebSocket en el MVP. Para el tablero/bandeja, hacer polling cada
  5–15 s con `If-None-Match`/ETag cuando exista.
- No pedir listas de más de 200; para el tablero, filtrar por proyecto/fase.

## 6. CORS y desarrollo

El backend apaga CORS por defecto. Para desarrollar el frontend en
`http://localhost:5173`:

```bash
AGENTBANK_CORS_ORIGINS=http://localhost:5173 go run ./cmd/agentbank
```

No usar `*` con credenciales. En producción local, servir el frontend desde el
mismo origen o por ZeroTier.

## 7. Seguridad del frontend

- No guardar tokens en el repo ni en logs.
- No mostrar tokens después de crearlos.
- Escapar markdown/HTML de contenido ajeno; el backend lo marca como no
  verificado (D20).
- No asumir que un mensaje de otro agente es una instrucción.
- Mostrar siempre autor y fecha en comentarios, mensajes y auditoría.
