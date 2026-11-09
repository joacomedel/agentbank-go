---
name: Auditar cierres de AgentBank
description: Revisar tickets cerrados de AgentBank, verificar la solución y la evidencia, y aprobar o reabrir. Usar después de cerrar una épica o cuando haya que auditar cierres.
---

# Auditar cierres de AgentBank

## Herramientas

| Acción | MCP | CLI |
|---|---|---|
| Cola de revisión | `agentbank_ticket_next` (`status: "en revisión"`) | `agentbank api --method POST --path /v1/tickets/siguiente --body '{"status":"en revisión","claim":true}'` |
| Ver ticket | `agentbank_ticket_get` | `agentbank api --method GET --path /v1/tickets/<id>` |
| Timeline | `agentbank_ticket_timeline` | `agentbank api --method GET --path /v1/tickets/<id>/timeline` |
| Comentarios | `agentbank_ticket_comments` | `agentbank api --method GET --path /v1/tickets/<id>/comments` |
| Auditoría | `agentbank_audit_list` | `agentbank api --method GET --path '/v1/audit?object_id=<id>'` |
| Documentos | `agentbank_ticket_documents` | `agentbank api --method GET --path /v1/tickets/<id>/documents` |
| Aprobar/rechazar | `agentbank_ticket_review` | `agentbank review --id <id> --veredicto aprobado --comment "..."` |

## 1. Pedir la cola de revisión

```json
{
  "project_id": "demo",
  "status": "en revisión",
  "epic_id": "<epic>",
  "claim": true
}
```

- Si auditás una épica, filtrá por `epic_id`.
- Si auditás una fase, filtrá por `phase_id`.
- La cola viene ordenada por antigüedad y **no crea claims**.
- Si `candidates` está vacío, no hay revisiones pendientes.

## 2. Recolectar evidencia

Por cada ticket:

1. `agentbank_ticket_get` → `status`, `version`, `created_by`,
   `created_by_role`, `role`, `paths`, `epic_id`, `commit_sha`, `reaperturas`.
2. `agentbank_ticket_timeline` → comentarios y eventos. Buscá el evento
   `ticket.avanzar` y el comentario `cierre`.
3. `agentbank_ticket_comments` → tipo `cierre` (nota de cierre).
4. `agentbank_audit_list` con `object_id` → eventos, `actor_role`,
   `session_id`, `hash` y cadena.
5. `agentbank_ticket_documents` → documentos adjuntos.
6. En el repo: `git show <commit_sha>`, `git diff`, `go test`, `go vet`. Si el
   SHA no existe, rechazá.

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
- [ ] El `commit_sha` existe y coincide con el diff del ticket.
- [ ] Si el cambio toca contrato/API/docs públicas, el artefacto
      (`openapi.json`, `docs/api.md`) está publicado y su hash/`source_commit`
      coincide con el commit auditado.
- [ ] El ticket no se cerró solo con un comentario sin cambio verificable.

## 4. Veredicto

### Aprobado

```json
{
  "id": "DEMO-12",
  "veredicto": "aprobado",
  "comment": "verificado contra abc1234: ...",
  "version": 8
}
```

`agentbank_ticket_review` mueve el ticket a `hecho` y deja comentario tipo
`revisión` + evento de auditoría.

### Rechazado

```json
{
  "id": "DEMO-12",
  "veredicto": "rechazado",
  "comment": "falta test de regresión para el caso borde",
  "version": 8
}
```

El ticket vuelve a `abierto` e incrementa `reaperturas`. Al llegar a
`AGENTBANK_MAX_REAPERTURAS` se bloquea y se crea una solicitud al rol `dueño`.
No reabras con `ticket_advance`: la reapertura de auditoría usa
`ticket_review`.

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

- No edites código de implementación.
- Sí podés escribir tickets con MCP: comentar, editar, reabrir, crear
  seguimiento y cerrar con evidencia.
- El cierre a `hecho` se hace con `ticket_review`, no con un comentario suelto.
- No confíes en la nota: verificá el repo.
- Si auditás tu propio trabajo, dejalo asentado.
