# Flujos MCP/CLI y optimización de llamadas

Objetivo: que un agente o humano resuelva el ciclo completo con la menor
cantidad de llamadas posible. El MCP y la CLI son clientes finos de la API; no
tienen lógica de negocio propia.

## Principios

1. **Incremental antes que listado completo.** Usar `updated_since`, cursores y
   `/v1/changes`; nunca listar todos los tickets para detectar uno nuevo.
2. **Endpoints compuestos.** `ticket_siguiente` trae ticket + contexto + claim;
   `ticket_avanzar` combina estado + nota + pregunta + siguiente.
3. **Long-poll antes que polling.** `/v1/novedades?wait=20` espera cambios en
   vez de repetir requests.
4. **Metadatos antes que contenido.** Los listados de docs devuelven
   `current_version` y hash; el contenido se pide solo si cambió.
5. **Idempotencia.** Reintentar un write con `Idempotency-Key` no duplica
   efectos.
6. **Sin perfiles MCP.** El MCP expone todas las tools; si el costo de contexto
   molesta, se agrega un filtro opcional más adelante.

## Endpoints clave para pocas llamadas

| Necesidad | Endpoint | Llamadas |
|---|---|---:|
| Arranque de agente | `GET /v1/me?proyecto=…` → briefing | 1 |
| Próximo ticket + contexto + claim | `POST /v1/tickets/siguiente` | 1 |
| Cerrar/avanzar/preguntar/siguiente | `POST /v1/tickets/{id}/avanzar` | 1 |
| Ver un ticket completo | `GET /v1/tickets/{ref}` | 1 |
| Tickets nuevos/cambiados | `GET /v1/projects/{id}/tickets?updated_since=…` | 1 |
| Cambios de cualquier objeto | `GET /v1/changes?since=…` | 1 |
| Bandeja pendiente | `GET /v1/me/bandeja?pendientes=1` | 1 |
| Resolver una solicitud | `POST /v1/messages/{id}/resolver` | 1 |
| Cargar plan completo | `POST /v1/projects/{id}/import` | 1 |
| Tablero | `GET /v1/projects/{id}/tablero` | 1 |
| Docs cambiados | `GET /v1/projects/{id}/documents?updated_since=…` | 1 |
| Sync de carpeta | `POST /v1/projects/{id}/documents/sync` | 1 |
| Buscar | `GET /v1/search?q=…` | 1 |
| Verificar auditoría | `GET /v1/audit/verify?chain_id=…` | 1 |

## Flujos concretos

### 1. Worker: arranque

1. `GET /v1/me?proyecto=API` → participante, membresías, briefing, bandeja
   pendiente, próximos tickets.
2. `POST /v1/tickets/siguiente` con `claim=true` → ticket + contexto + claim.

**2 llamadas** para empezar a trabajar.

### 2. Worker: loop por ticket

1. `POST /v1/tickets/siguiente` → ticket + contexto.
2. Trabajar.
3. `POST /v1/tickets/{id}/avanzar` → estado, nota, pregunta y siguiente ticket.

**2 llamadas por ticket.** Si necesita comentar adjuntos o docs, suma 1–2.
Antes del rediseño eran 5–7 llamadas (listar, obtener, reclamar, comentar,
cerrar, buscar siguiente).

### 2b. Worker: carril por épica

1. `POST /v1/tickets/siguiente` con `claim=true`, `epic_id=<épica>` y
   `role=<rol>` → solo tickets de esa épica.
2. Cerrar con `POST /v1/tickets/{id}/avanzar` y `y_siguiente=true` → próximo
   ticket del mismo carril.
3. Cuando devuelve `ticket: null`, el carril terminó (`motivo` explica por qué).

El worker de la CLI ya soporta `--epic` y `--phase`; el `--cmd` puede lanzar
`opencode run --agent implementador`.

### 2c. Auditor: cierres de una épica

1. `GET /v1/projects/{id}/tickets?status=hecho&epic_id=<épica>&updated_since=…`
   → cierres nuevos.
2. `GET /v1/tickets/{id}/timeline` → nota de cierre, eventos, `actor_role` y
   `session_id`.
3. `GET /v1/audit?object_id=<id>` → cadena y payload del cierre.
4. `POST /v1/tickets/{id}/comments` (`type=nota`) para aprobar o
   `POST /v1/tickets/{id}/avanzar` (`status=abierto`) para reabrir.

**3–4 llamadas por ticket auditado**, más las de verificación en el repo.

### 3. Worker: detectar trabajo nuevo sin listar todo

- Opción A: `GET /v1/projects/API/tickets?updated_since=<cursor>` → solo tickets
  nuevos/cambiados, con detalle y `next_cursor`.
- Opción B: `GET /v1/changes?since=<cursor>&project_id=API` → tickets, mensajes,
  docs, fases y épicas cambiados, con resumen y `next_cursor`.
- Si no hay nada, `GET /v1/novedades?since=<cursor>&wait=20` espera.

**1 llamada** para saber si hay novedades; no hace falta revisar la lista
completa.

### 4. Humano: bandeja

1. `GET /v1/me/bandeja?pendientes=1` → solicitudes pendientes.
2. `POST /v1/messages/{id}/resolver` por cada una.

**1 + N llamadas**, donde N es la cantidad de respuestas. Si el cliente usa
`/v1/changes`, puede detectar resoluciones y nuevas solicitudes en una sola
llamada.

### 5. Planificador: cargar fases, épicas y tickets

1. `POST /v1/projects/{id}/import?dry_run=1` → validar.
2. `POST /v1/projects/{id}/import` → crear todo.

**2 llamadas** (1 si no usa dry-run). Antes eran decenas de POST.

### 6. Monitor / tablero

- `GET /v1/projects/{id}/tablero` → fases, épicas, tickets y estados.
- `GET /v1/projects/{id}/digest` → contadores y eventos recientes.
- `GET /v1/changes?since=…` → actualizaciones incrementales.

**1 llamada** por refresco.

### 7. Docs

- `GET /v1/projects/{id}/documents?updated_since=…` → solo docs cambiados con
  `current_version` y hash (sin contenido).
- `GET /v1/documents/{id}` → contenido solo de los que cambiaron.
- `POST /v1/projects/{id}/documents/sync` → subir una carpeta completa.

**1 + M llamadas**, donde M son los docs que realmente cambiaron.

### 8. Auditoría

- `GET /v1/audit/verify?chain_id=…` → verificar una cadena.
- `POST /v1/admin/verify-all` → verificar todas.
- `GET /v1/novedades?since=…` → eventos nuevos.

**1 llamada** por verificación o por bloque de novedades.

## Mapa de tools MCP (sin perfiles)

| Tool | Endpoint | Uso |
|---|---|---|
| `me` | `GET /v1/me` | identidad + briefing |
| `changes` | `GET /v1/changes` | cambios incrementales |
| `novedades` | `GET /v1/novedades` | long-poll |
| `tickets_listar` | `GET /v1/projects/{id}/tickets` | listado con `updated_since` |
| `ticket_obtener` | `GET /v1/tickets/{id}` | detalle |
| `ticket_siguiente` | `POST /v1/tickets/siguiente` | cola + claim + contexto |
| `ticket_avanzar` | `POST /v1/tickets/{id}/avanzar` | cierre + nota + pregunta + siguiente |
| `ticket_editar` | `PATCH /v1/tickets/{id}` | edición |
| `ticket_comentar` | `POST /v1/tickets/{id}/comments` | comentario |
| `ticket_dependencia_crear` | `POST /v1/tickets/{id}/dependencies` | dependencia |
| `ticket_dependencia_quitar` | `DELETE /v1/tickets/{id}/dependencies/{dep}` | dependencia |
| `ticket_claim` | `POST /v1/tickets/{id}/claims` | claim |
| `ticket_claim_liberar` | `POST /v1/tickets/{id}/claims/release` | liberar |
| `bandeja` | `GET /v1/me/bandeja` | solicitudes |
| `mensaje_enviar` | `POST /v1/messages` | pregunta/info |
| `mensaje_resolver` | `POST /v1/messages/{id}/resolver` | respuesta |
| `mensaje_tomar` | `POST /v1/messages/{id}/tomar` | tomar de rol |
| `fase_crear` / `fases_listar` | `/v1/projects/{id}/phases` | fases |
| `epica_crear` / `epicas_listar` | `/v1/projects/{id}/epics` | épicas |
| `importar` | `POST /v1/projects/{id}/import` | carga masiva |
| `documentos_listar` | `GET /v1/projects/{id}/documents` | docs con `updated_since` |
| `documento_obtener` | `GET /v1/documents/{id}` | contenido |
| `documento_version_crear` | `POST /v1/documents/{id}/versions` | versión |
| `docs_sync` | `POST /v1/projects/{id}/documents/sync` | sync |
| `buscar` | `GET /v1/search` | búsqueda |
| `audit_verificar` | `GET /v1/audit/verify` | integridad |
| `admin_barridos` | `POST /v1/admin/barridos` | claims/defaults |

## Optimizaciones futuras

- **ETag/If-None-Match** en listados y snapshots para respuestas `304`.
- **Batch resolve** de mensajes (`POST /v1/messages/resolver-batch`) si una
  bandeja grande se responde en bloque.
- **SSE** o long-poll específico por proyecto para tablero.
- **FTS5** si `LIKE` deja de alcanzar.
- **Snapshots con `since`** para exportar un tablero completo en una llamada.

## Regla para el MCP

Cada tool debe:
- aceptar cursores/`updated_since` cuando devuelve listas;
- devolver resúmenes y un `next_cursor`;
- evitar N+1;
- reutilizar `Idempotency-Key` en writes;
- no ocultar funcionalidad por perfiles.
