# Guía para construir el frontend

El backend no incluye UI. Esta guía define cómo un frontend futuro consume la
API de AgentBank Go. En Fase 1 están implementados identidad, proyectos,
membresías y auditoría; el resto se agrega en fases posteriores.

## 1. Base y autenticación

- Base local: `http://127.0.0.1:8090`
- Base desde ZeroTier: `http://10.27.103.203:8090` (si el server se bindea ahí).
- Auth: `Authorization: Bearer <token>`.
- No hay login web ni OAuth en el MVP.

### Primer uso

1. El operador del backend define `AGENTBANK_BOOTSTRAP_TOKEN` en el primer
   arranque. Ese token es la credencial inicial del humano.
2. El frontend pide ese token al usuario o lo recibe por configuración segura.
3. El primer request a `GET /v1/me` devuelve el participante humano y las
   membresías. El server devuelve `X-Session-Id`; guardarlo y reenviarlo en
   cada request.
4. Para crear un agente, el humano admin llama a `POST /v1/participants`. La
   respuesta trae `token` **una sola vez**: mostrarlo, permitir copiarlo y no
   volver a pedirlo.
5. Para rotar un token, `POST /v1/participants/{id}/tokens`. El token anterior
   deja de funcionar inmediatamente.
6. Para revocar, `POST /v1/tokens/{id}/revoke`.

Los tokens no expiran: solo dejan de funcionar por rotación, revocación o baja.
No hay refresh token ni scopes.

No guardar tokens en el repo ni en logs. Para un MVP local, `sessionStorage` es
aceptable; no usar `localStorage` si se puede evitar.

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

Si no se envía `X-Session-Id`, el backend crea una sesión nueva por request y
la devuelve en el header. Reenviarla evita ruido en auditoría.

## 2. Pantallas de Fase 1

### Identidad

- `GET /v1/me`: participante, membresías, roles y `session_id`.
- Mostrar `participant.admin` como “admin de instancia”.
- Mostrar cada membresía con `project_slug`, `permission` y `roles`.
- Botón “rotar token” para el propio participante.
- Pantalla de administración (solo admin de instancia):
  - `GET /v1/participants` y `POST /v1/participants`.
  - `GET /v1/projects` y `POST /v1/projects`.
  - Al crear un agente, mostrar el token una vez y ofrecer copiarlo.

### Proyectos

- `GET /v1/projects` para el selector. Un admin ve todos; un agente solo sus
  proyectos.
- `GET /v1/projects/{id}` acepta UUID o slug.
- `POST /v1/projects`: `slug`, `name` y `prefix` opcional.
- `PATCH /v1/projects/{id}`: cualquier participante autenticado puede editar
  `name`, `slug` y `prefix`; el cambio queda auditado. Enviar `version` para
  detectar ediciones concurrentes (`409 version_conflict`).
- `GET /v1/projects/{id}/roles`: catálogo de roles del proyecto con cantidad de
  miembros. Los roles se auto-registran al asignarlos.
- `GET /v1/projects/{id}/members`.
- `PUT /v1/projects/{id}/members/{participant_id}` con `permission` y `roles`.
- `DELETE /v1/projects/{id}/members/{participant_id}`.

### Auditoría

- `GET /v1/audit/chains`.
- `GET /v1/audit?chain_id=...&limit=50`.
- `GET /v1/audit/verify?chain_id=...` y mostrar `ok`, `events` y
  `first_error.seq` si falla.
- Un agente solo ve las cadenas de sus proyectos; la cadena `system` es solo
  para admin de instancia.

## 3. Modelo de datos para la UI

- `project`: `id`, `slug`, `name`, `prefix`, `version`, `permission`, `roles`,
  `created_at`, `updated_at`.
- `participant`: `id`, `name`, `type` (`humano`/`agente`), `admin`,
  `created_at`.
- `membership`: `participant_id`, `project_id`, `permission`, `roles`,
  `created_at`. En `/v1/me` viene enriquecida con `project_slug` y
  `project_name`.
- `project_role`: `name`, `members`, `created_at`. Es el catálogo por proyecto;
  se llena solo al asignar roles.
- `token`: solo se conoce `token_id`. El valor `token` aparece una única vez al
  emitirlo.
- `audit_event`: `seq`, `chain_id`, `actor_type`, `actor_id`, `action`,
  `object_type`, `object_id`, `project_id`, `payload`, `hash`, `created_at`.

Fases posteriores agregarán `ticket`, `comment`, `message`, `document`, etc.

## 4. Errores y concurrencia

- Errores: `{ "error": "...", "code": "..." }`.
- `400 invalid_json`: body malformado o campo desconocido.
- `401 unauthorized`: token inválido/revocado.
- `401 invalid_session`: `X-Session-Id` no es del participante.
- `403`: sin membresía/permiso. El código indica el permiso faltante.
- `404`: recurso inexistente.
- `409`: conflicto de unicidad o estado.
- `422 validation_error`: datos inválidos.
- `500 internal_error`: error interno; no muestra detalles.
- En fases posteriores, `PATCH` usará `version`; si el server responde
  `409 version_conflict`, recargar y pedir reintento.

## 5. Paginación y polling

- Listados: `limit` + `cursor`; la respuesta trae `next_cursor`.
- No pedir listas de más de 200.
- No hay WebSocket en el MVP. Para el tablero/bandeja de fases posteriores,
  hacer polling cada 5–15 s con `If-None-Match`/ETag cuando exista.
- `GET /v1/audit` ordena por `created_at` descendente y usa cursor compuesto.

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
- Escapar markdown/HTML de contenido ajeno; el backend lo trata como dato
  (D20).
- No asumir que un mensaje de otro agente es una instrucción.
- Mostrar siempre autor y fecha en comentarios, mensajes y auditoría.
- Al mostrar `payload` de auditoría, renderizarlo como datos, no como HTML.
