# Alcance — MVP vs deferred

## MVP (debe funcionar en el Moto)

### Identidad
- Proyecto con `slug`, `nombre`, `prefijo` único.
- Participante humano y agentes; un token activo por participante.
- Membresías por proyecto (`lector`, `colaborador`, `admin`) y roles.
- Sesiones (`X-Session-Id`) para auditoría.
- Bootstrap del humano con token inicial.

### Trabajo
- Tickets con estados, prioridad, descripción, asignación y versión.
- Dependencias entre tickets, con detección de ciclos.
- `listo` derivado: abierto + dependencias hechas/canceladas + sin mensajes
  pendientes que lo afecten.
- Claims con lease y heartbeat; liberación manual y por vencimiento.
- `POST /v1/tickets/siguiente` atómico y ordenado.
- Comentarios y timeline.

### Organización
- Fases por proyecto y épicas.
- Orden fase → épica → ticket → prioridad → antigüedad.
- Cierre de fase/épica con pendientes registrados.

### Comunicación
- Mensajes `info` y `solicitud`.
- Destino participante, rol o proyecto.
- Opciones + default; resolución y descarte.
- Bandeja unificada para humanos y agentes.
- `afecta` tickets.

### Conocimiento
- Documentos markdown con versiones inmutables y hash.
- Diff entre versiones.
- Búsqueda simple de tickets/documentos/mensajes.

### Auditoría
- Evento append-only por cambio, con hash chain.
- Cadena `system` y por proyecto.
- Verificación de integridad.

### Operación
- `/healthz`, `/v1/meta`.
- Logs estructurados con `request_id`.
- Límites de body, timeouts y rate limit básico.
- Backup SQLite y systemd.

## Deferred (documentado, no se implementa ahora)

- Frontend/UI.
- Telegram, webhooks, outbox, notificaciones push.
- Handoff estructurado y defaults automáticos por reloj.
- Contratos entre proyectos y breaking changes.
- Archivado, retención, export.
- Suscripciones, long-poll, novedades por secciones.
- MCP y CLI completos.
- Multi-tenant, OAuth, múltiples humanos.
- Búsqueda FTS5 si `LIKE` alcanza para el MVP.
- Paridad 1:1 con las 53 tools del MCP viejo.

## Criterio para decir “listo”

El MVP está listo cuando un humano y dos agentes pueden:

1. Crear un proyecto y sus participantes.
2. Cargar tickets con dependencias.
3. Reclamar y ejecutar tickets sin pisarse.
4. Comentar, cerrar y ver el timeline.
5. Hacer una pregunta con opciones/default y resolverla desde la bandeja.
6. Crear un documento versionado y buscarlo.
7. Verificar la cadena de auditoría.
8. Correr todo en el Moto con el presupuesto de recursos.
