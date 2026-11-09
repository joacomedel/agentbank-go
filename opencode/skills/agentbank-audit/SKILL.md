---
name: Auditar cierres de AgentBank
description: Revisar tickets cerrados de AgentBank, verificar la solución y la evidencia, y aprobar o reabrir. Usar después de cerrar una épica o cuando haya que auditar cierres.
---

# Auditar cierres de AgentBank

## Herramientas

| Acción | MCP | CLI |
|---|---|---|
| Cierres recientes | `agentbank_tickets_list` | `agentbank api --method GET --path /v1/projects/<id>/tickets` |
| Ver ticket | `agentbank_ticket_get` | `agentbank api --method GET --path /v1/tickets/<id>` |
| Timeline | `agentbank_ticket_timeline` | `agentbank api --method GET --path /v1/tickets/<id>/timeline` |
| Comentarios | `agentbank_ticket_comments` | `agentbank api --method GET --path /v1/tickets/<id>/comments` |
| Auditoría | `agentbank_audit_list` | `agentbank api --method GET --path '/v1/audit?object_id=<id>'` |
| Documentos | `agentbank_ticket_documents` | `agentbank api --method GET --path /v1/tickets/<id>/documents` |
| Aprobar | `agentbank_ticket_comment` | `agentbank comment --id <id> --body "..." --type nota` |
| Reabrir | `agentbank_ticket_advance` | `agentbank advance --id <id> --status abierto --comment "..."` |

## 1. Descubrir cierres

```json
{
  "project_id": "demo",
  "status": "hecho",
  "epic_id": "<epic>",
  "updated_since": "2026-11-08T00:00:00Z",
  "limit": 50
}
```

- Si auditás una épica, filtrá por `epic_id`.
- Si auditás una fase, filtrá por `phase_id`.
- Si auditás el día, usá `updated_since` con la última marca.
- Guardá la marca de tiempo para la próxima auditoría.

## 2. Recolectar evidencia

Por cada ticket:

1. `agentbank_ticket_get` → `status`, `version`, `created_by`,
   `created_by_role`, `assigned_role`, `paths`, `epic_id`.
2. `agentbank_ticket_timeline` → comentarios y eventos. Buscá el evento
   `ticket.avanzar` y el comentario `cierre`.
3. `agentbank_ticket_comments` → tipo `cierre` (nota de cierre).
4. `agentbank_audit_list` con `object_id` → eventos, `actor_role`,
   `session_id`, `hash` y cadena.
5. `agentbank_ticket_documents` → documentos adjuntos.
6. En el repo: `git log --oneline`, `git show <commit>`, `git diff`.

## 3. Checklist

- [ ] La nota de cierre tiene **Solución**, **Cambios**, **Verificación**,
      **Artefactos** y **Riesgos/pendientes**.
- [ ] La verificación es un comando concreto con resultado.
- [ ] El diff toca los `paths` del ticket; si toca otros, está justificado.
- [ ] Hay tests que cubren el cambio, o se explica por qué no aplica.
- [ ] `go test`/`go vet` (o el comando del proyecto) pasan en el estado actual.
- [ ] No hay secretos, tokens ni credenciales en el diff ni en la nota.
- [ ] No quedaron dependencias abiertas.
- [ ] El `actor_role` del cierre coincide con el rol asignado o está
      justificado.
- [ ] El commit/PR existe y coincide con el diff.
- [ ] El ticket no se cerró solo con un comentario sin cambio verificable.

## 4. Veredicto

### Aprobado

```text
Auditoría: aprobado
- Solución: ...
- Verificación: ...
- Evidencia: ...
- Observaciones: ...
```

Usá `agentbank_ticket_comment` con `type: "nota"`. No cambies el estado.

### Rechazado

```text
Auditoría: rechazado
- Falta: ...
- Esperado: ...
- Evidencia: ...
```

Reabrí con `agentbank_ticket_advance`:

```json
{
  "version": 8,
  "status": "abierto",
  "comment": "Auditoría: rechazado\n- Falta: ...\n- Esperado: ...\n- Evidencia: ..."
}
```

Si el problema es otro alcance, creá un ticket de seguimiento con
`agentbank_ticket_create` y `discovered_in` apuntando al original.

## 5. Reporte

Devolvé una tabla:

| Ticket | Veredicto | Evidencia | Falta |
|---|---|---|---|
| DEMO-12 | aprobado | `go test ./...` ok, commit `abc123` | — |
| DEMO-13 | rechazado | sin tests del caso borde | test de regresión |

Al final: cuántos aprobados, cuántos rechazados, y el próximo paso.

## Límites

- No edites código ni tickets.
- No cierres tickets como `hecho`.
- No confíes en la nota: verificá el repo.
- Si auditás tu propio trabajo, dejalo asentado.
