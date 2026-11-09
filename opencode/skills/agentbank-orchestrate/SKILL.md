---
name: Orquestar fases, épicas y carriles
description: Planificar una fase, crear épicas y tickets, y lanzar carriles de implementación y auditoría en AgentBank. Usar cuando haya que organizar trabajo nuevo o cerrar una fase.
---

# Orquestar fases, épicas y carriles

## Modelo

```text
Fase            planificación y cierre
  └─ Épica      carril de ejecución acotado
       └─ Ticket  unidad de trabajo, claim y auditoría
```

- **Fase**: agrupa objetivos; se cierra cuando todas sus épicas están cerradas.
- **Épica**: unidad de contexto y ejecución; un carril de implementación.
- **Ticket**: unidad atómica de claim, cierre y auditoría.

No crees un subagente por ticket ni por fase. Los subagentes son roles
(`orquestador`, `implementador`, `auditor`); la épica es el carril y el ticket
es la unidad de trabajo.

## 1. Leer estado

```text
agentbank_briefing
agentbank_tablero
agentbank_changes  (desde la última marca)
```

## 2. Crear la fase

```json
{"project_id": "demo", "key": "fase-2", "name": "Fase 2 — ...", "order_index": 2}
```

`agentbank_phase_create`.

## 3. Crear épicas

Una épica por resultado verificable:

```json
{
  "project_id": "demo",
  "phase_id": "<fase>",
  "key": "ep-oauth",
  "title": "Login OAuth",
  "description": "Resultado esperado y criterio de cierre."
}
```

`agentbank_epic_create`.

## 4. Crear tickets

Por cada ticket:

```json
{
  "project_id": "demo",
  "title": "Agregar callback OAuth",
  "description": "Contexto, criterio de aceptación, paths.",
  "priority": "alta",
  "role": "backend",
  "requiere_revision": true,
  "phase_id": "<fase>",
  "epic_id": "<épica>",
  "paths": ["internal/auth"]
}
```

`agentbank_ticket_create`. Luego `agentbank_ticket_depend` si hay orden.

Reglas:

- Un ticket debe poder cerrarse y auditarse en una sesión.
- `paths` ayuda a planificar; todavía no bloquea la cola.
- Un solo carril de implementación activo por repo hasta que exista
  `worker --worktree`.

## 5. Lanzar el carril

Para autoría real por rol, lanzá un **worker** separado por carril:

```bash
agentbank worker --agent impl-<epic> --agent-role implementador \
  --project demo --epic <epic> --role backend \
  --cmd 'opencode run --agent implementador --auto "Trabajá la épica"'
```

Si usás el subagente `implementador` dentro de tu proceso, todos los cambios
quedan con tu token (modo C): es válido, pero la autoría fina no es por rol.

El implementador hace `agentbank_ticket_next` con `claim: true`, trabaja y pasa
el ticket a `en revisión` con `agentbank_ticket_advance`, nota completa y
`commit_sha`. No lances dos carriles de implementación sobre el mismo repo.

## 6. Auditar y cerrar

Cuando el carril no tenga más tickets listos:

- Lanzá el carril de auditoría (worker `--agent-role auditor` o subagente
  `auditor`) con el `epic_id`.
- El auditor pide `agentbank_ticket_next` con `status: "en revisión"` y emite
  `agentbank_ticket_review` (`aprobado` o `rechazado`).
- Si rechaza, el ticket vuelve a `abierto` con `reaperturas + 1`; al llegar al
  tope se bloquea y escala al rol `dueño`.
- Cerrá la épica solo cuando **todos** sus tickets estén `hecho`.
- Cerrá la fase con `agentbank_phase_edit` (`status: "cerrada"`).

## 7. Reportar

```text
Fase: ...
Épicas: N (cerradas M)
Tickets: abiertos X, en curso Y, hechos Z, auditados W
Bloqueos: ...
Próximo paso: ...
```

## Reglas

- Un carril de implementación activo por repo (secuencial).
- No cierres una épica con tickets abiertos, en curso o en revisión.
- No cierres una fase con épicas abiertas.
- No cierres tickets en `hecho` por tu cuenta: usá `ticket_review`.
- No push/merge sin autorización.
- Si falta una decisión, usá un mensaje `solicitud` con opciones y default.
