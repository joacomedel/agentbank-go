---
name: Trabajar un ticket de AgentBank
description: Reclamar, implementar, verificar y cerrar un ticket de AgentBank con nota de cierre estructurada y evidencia. Usar cuando haya que ejecutar trabajo de una épica o un ticket.
---

# Trabajar un ticket de AgentBank

## Herramientas

Las tools MCP se llaman `agentbank_<tool>` (servidor `agentbank`). **El MCP
puede leer y escribir**: crear/editar/cerrar tickets, comentar, mensajes y
documentos. El rol no limita las tools MCP; los permisos de archivo son aparte.
Si el MCP no está disponible, usá la CLI `agentbank`.

| Acción | MCP | CLI |
|---|---|---|
| Identidad | `agentbank_me` | `agentbank me` |
| Próximo ticket | `agentbank_ticket_next` | `agentbank next --epic <id>` |
| Ver ticket | `agentbank_ticket_get` | `agentbank api --method GET --path /v1/tickets/<id>` |
| Comentar | `agentbank_ticket_comment` | `agentbank comment --id <id> --body "..." --type nota` |
| Pasar a revisión | `agentbank_ticket_advance` | `agentbank advance --id <id> --status "en revisión" --nota-cierre "..." --commit-sha <sha>` |
| Veredicto | `agentbank_ticket_review` | `agentbank review --id <id> --veredicto aprobado --comment "..."` |
| Timeline | `agentbank_ticket_timeline` | `agentbank api --method GET --path /v1/tickets/<id>/timeline` |

## 1. Reclamar

Si el orquestador te dio un `ticket_id`, usalo. Si no:

```json
{
  "project_id": "demo",
  "epic_id": "<epic>",
  "role": "backend",
  "claim": true
}
```

`agentbank_ticket_next` es atómico: crea el claim, pone el ticket `en curso` y
devuelve el contexto. Si devuelve `claim_conflict`, no insistas: otro agente lo
tiene.

## 2. Entender antes de tocar

- `agentbank_ticket_get` trae estado, versión, paths, dependencias y claims.
- `agentbank_ticket_timeline` trae comentarios y eventos.
- Si hay un mensaje `solicitud` pendiente que afecta el ticket, no lo cierres.
- Si el ticket no tiene criterio de aceptación, pedí uno con `pregunta` antes de
  inventar alcance.

## 3. Implementar

- Trabajá dentro de los `paths` declarados. Si necesitás salir, justificalo.
- Cambios chicos y revisables. Un ticket = un commit local como mínimo.
- Rama local: `feat/<epic>-<ticket>`. No push, no PR, no merge sin autorización.
- Corré el comando de verificación del proyecto:
  - Go: `gofmt -l .`, `go vet ./...`, `go test ./...`.
  - Otros: el comando que declare el repo.
- No commitees tokens, `.env`, bases ni binarios generados. Los artefactos
  compartidos (openapi, docs) se publican en AgentBank, no se commitean
  duplicados.
- Si el cambio toca contrato/API/docs públicas, republicá `openapi.json` y
  `docs/api.md` con `agentbank artifact put … --commit <sha>` y verificá el
  hash. Ver `docs/artifacts.md`.

## 4. Pasar a revisión con evidencia

No cierres en `hecho`: el veredicto lo emite el auditor.

```json
{
  "version": 7,
  "status": "en revisión",
  "nota_cierre": "Solución: ...\nCambios: ...\nVerificación: ...\nArtefactos: ...\nRiesgos: ...",
  "commit_sha": "abc1234",
  "y_siguiente": true
}
```

### Plantilla de `nota_cierre`

```text
Solución: qué se hizo y por qué alcanza.
Cambios: archivos/módulos tocados.
Verificación: comando exacto + resultado (ej. go test ./... → ok).
Artefactos: commit/PR + artefacto publicado (ruta, versión, hash/source_commit);
           si no toca contrato/docs públicos, “no aplica”.
Riesgos: pendientes, deuda o “ninguno”.
```

Reglas:

- `version` tiene que ser la última. Si hay `version_conflict`, releé.
- Las cinco secciones son obligatorias y no vacías; si falta una, el servidor
  devuelve `422 evidencia_incompleta`.
- `commit_sha` de 7–40 hex es obligatorio si `requiere_revision=true`.
- `y_siguiente: true` devuelve el próximo ticket del mismo carril.
- Si no pudiste terminar, no cierres: `status: "abierto"` + `comment` con el
  bloqueo, o una `pregunta` con opciones/default.
- Si el auditor rechaza, el ticket vuelve a `abierto` con `reaperturas + 1`:
  leé el comentario y corregí.
- Un ticket marcado `requiere_revision: false` puede cerrarse en `hecho` con
  evidencia completa.

## 5. Terminar el carril

Cuando `agentbank_ticket_next` devuelva `ticket: null`:

- `motivo: sin_candidatos` → no hay más trabajo en esa épica.
- `esperando_dependencias` → otra épica/ticket debe cerrarse primero.
- `esperando_respuesta` → hay una pregunta pendiente.
- `reclamados` → otro agente tiene los tickets.
- `pausado` → el proyecto está pausado.

Reportá el motivo y no inventes tickets.
