# Paridad con el original — estado

Fuente: `requisitos.md`, `spec-banco-datos-agentes.md`,
`spec-banco-datos-agentes-v2.md` y `requisitos-v3.1.md` del repo viejo
(`/root/reference/BancoDeDatosAgente`, commit `95f9a00`).

Estado al 2026-11-08. El backend Go implementa las etapas 1–6 del plan de
reescritura. CLI, MCP, UI, Telegram, contratos y choque de áreas siguen
pendientes (etapas 7–9). SQLite reemplaza a Postgres por decisión de ADR 0001.

## Matriz de requerimientos

| REQ | Tema | Estado |
|---|---|---|
| 01 | Contratos versionados | ⬜ Etapa 9 |
| 02 | Detección de breaking changes | ⬜ Etapa 9 |
| 03 | Consumidores declarados | ⬜ Etapa 9 |
| 04 | Aviso automático al consumidor | ⬜ Etapa 9 |
| 05 | Heartbeat implícito | ✅ |
| 06 | Liberación automática de claims | ✅ barrido por lease |
| 07 | Bloqueado por / escalamiento | ⬜ Etapa 9 |
| 08 | Áreas tocadas / choque | ⬜ Etapa 9 (`tickets.paths` ya existe) |
| 09 | Webhooks + outbox + dead-letter | ✅ |
| 10 | ADRs inmutables | ✅ decisiones |
| 11 | Glosario | ✅ docs `type=glosario` |
| 12 | Retención/export | ⛔ Fuera de V3.1 |
| 13 | Digest | ✅ |
| 14 | Aprobaciones | ✅ vía mensajes con opciones |
| 15 | Plantillas | ⛔ Eliminado por V3.1 |
| 16 | Un token por agente | ✅ REQ-29 |
| 17 | Tests de schema | ✅ JSON estricto + tests |
| 18 | Limpieza administrativa | ➖ Operativo |
| 19 | Fases y orden | ✅ |
| 20 | Mensajes unificados | ✅ con límites (`divido` simplificado) |
| 21 | Listo y `ticket_siguiente` | ✅ |
| 22 | Carga masiva | ✅ JSON, dry-run, ciclos |
| 23 | Docs globales/sync/D20 | ✅ |
| 24 | Roles | ✅ catálogo y roles en tickets |
| 25 | Briefing | ✅ |
| 26 | Convertir/corregir respuestas | ✅ |
| 27 | Trazas | ✅ cierre, descubierto, timeline; menciones `@rol` pendientes |
| 28 | UI tablero/bandeja | ➖ Fuera de este repo; se construye contra OpenAPI |
| 29 | Participantes/membresías | ✅ |
| 30 | Red de seguridad | ✅ en lo implementado: strict JSON, 409, D20 |
| 31 | Telegram | ⬜ Etapa 9 |
| 32 | Runner de workers | ⬜ Etapa 7 |
| 33 | Init / `.agentbank.json` | ⬜ Etapa 7 |
| 34 | IDs cortos | ✅ `PREFIX-N` |

## Endpoints

Implementado: salud, identidad, proyectos, membresías, roles, tickets,
dependencias, claims, cola, comentarios, timeline, fases, épicas, mensajes,
bandeja, documentos, versiones, diff, sync, decisiones, búsqueda, briefing,
digest, tablero, import, auditoría, novedades, métricas, outbox, webhooks y
barridos. Ver `docs/api.md`.

Pendiente: rutas de contratos, suscripciones, bloqueos/escalado, Telegram y
archivado.

## Diferencias conocidas

- SQLite en lugar de Postgres.
- Búsqueda `LIKE`, no FTS5.
- `ticket_avanzar` modo `divido` no crea ticket hijo todavía.
- Handoff se resuelve como mensaje con texto, sin estructura adicional.
- Menciones `@rol`/`@participante` en comentarios todavía no generan mensajes.
- Sin UI de producto (se construye aparte contra OpenAPI), sin CLI/MCP,
  Telegram ni contratos.

## Próximas etapas

7. CLI Go (`init`, `participante`, `import`, `docs`, `worker`, `telegram`) y MCP
   con perfiles.
8. UI de tablero y bandeja.
9. Telegram, contratos, bloqueos/escalado y choque de áreas.
