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
  "assigned_role": "backend",
  "phase_id": "<fase>",
  "epic_id": "<épica>",
  "paths": ["internal/auth"]
}
```

`agentbank_ticket_create`. Luego `agentbank_ticket_depend` si hay orden.

Reglas:

- Un ticket debe poder cerrarse y auditarse en una sesión.
- `paths` ayuda a evitar choques entre carriles.
- No pongas dos tickets con el mismo `paths` en carriles paralelos.

## 5. Lanzar el carril

Usá el subagente `implementador` con:

- `project_id`
- `epic_id`
- `role` (`backend`, `frontend`, `docs`, `test`, `infra`)

El implementador hace `agentbank_ticket_next` con `claim: true` y cierra con
`agentbank_ticket_advance`. No lances dos carriles sobre la misma épica.

## 6. Auditar y cerrar

Cuando el carril no tenga más tickets:

- Lanzá el subagente `auditor` con el `epic_id`.
- El auditor aprueba con comentario o reabre con `status: "abierto"`.
- Si reabre, el implementador retoma.
- Cerrá la épica con `agentbank_epic_edit` (`status: "cerrada"`).
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

- Un carril por épica; no paralelices tickets que comparten `paths`.
- No cierres una épica con tickets abiertos o en curso.
- No cierres una fase con épicas abiertas.
- No push/merge sin autorización.
- Si falta una decisión, usá un mensaje `solicitud` con opciones y default.
