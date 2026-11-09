# ADR 0007 — Revisión en la máquina de estados, evidencia y carriles

- Fecha: 2026-11-09
- Estado: aceptado; Épica 1 implementada, Épicas 2–7 planificadas
- Alcance: `/root/agentbank-go`, rama `feat/subagentes-epica`
- Reemplaza parcialmente: las dudas abiertas de ADR 0003 sobre roles como permisos

## Contexto

El backend está bien asentado: autor y rol salen del token, los cambios de
negocio y la auditoría comparten transacción, la cadena es verificable y
`ticket_next` reclama de forma atómica. El problema está en la **capa de
organización de agentes**: reglas que definen la integridad del flujo viven
solo en prompts de un modelo Flash. Una regla que solo está en el prompt se
cumple “casi siempre”; en un sistema de auditoría, “casi siempre” es “no
garantizado”.

Diagnóstico externo (D1–D12):

- **D1 (crítico):** la auditoría no forma parte del modelo de estados. Se pasa
  a `hecho` y se aprueba con un comentario de texto; los dependientes se
  liberan antes de auditar y una reapertura no los vuelve a bloquear.
- **D2:** no se valida la evidencia de cierre; no hay `commit_sha`.
- **D3:** no hay paralelismo real: el worker comparte checkout, índice de git y
  artefactos; `paths` no se usa en la cola.
- **D4:** la identidad por rol es ficticia en modo C: todos los subagentes del
  proceso escriben como `opencode-main` (orquestador).
- **D5:** claims con lease fijo, sin renovación, barrido manual y sin fencing.
- **D6:** el worker no detecta una salida exitosa sin cierre.
- **D7:** no hay corte del ciclo de rechazos ni escalado al humano.
- **D8:** `divido` se acepta y no se implementa; `avanzar` con `y_siguiente` no
  es idempotente.
- **D9:** `agent_role` mezcla función y especialidad; `role` vs
  `assigned_role` en la documentación.
- **D10:** 75 tools MCP para todos y compactación recién a 400k.
- **D11:** `shell: agentbank *` incluye `agentbank api`; el auditor no puede
  `gofmt -l`.
- **D12:** inconsistencias de documentación (dos “Fase 7”, carriles paralelos
  vendidos, estado de CLI/MCP desactualizado).

Principio rector:

> **Lo que define la integridad del flujo va en la máquina de estados del
> servidor y es independiente del rol. Lo que es preferencia de trabajo queda
> en prompts.**

## Decisiones

### Decisión 1 — La revisión entra en la máquina de estados

Son reglas de transición, no permisos por rol; funcionan igual en modo A y C.

```text
abierto ──claim──▶ en curso ──avanzar──▶ en revisión ──veredicto──▶ hecho
   ▲                  │                      │
   │                  ├──▶ abierto (pregunta/bloqueo, libera claim)
   │                  └──▶ bloqueado
   └──────────── rechazo (reapertura, con motivo) ◀──┘
```

- Desde `en curso` solo se puede ir a `en revisión`, `abierto` o `bloqueado`.
  `en curso → hecho` devuelve `409 transicion_invalida`.
- A `hecho` solo se llega desde `en revisión`.
- `en revisión → abierto` es la reapertura y exige `comment` no vacío.
- Las dependencias se liberan solo en `hecho`; `en revisión` cuenta como
  pendiente para los dependientes.
- Al pasar a `en revisión` se libera el claim de implementación.
- `cancelado` sigue disponible desde cualquier estado no terminal, con motivo.
- Excepción: un ticket sin cambio de código puede crearse con
  `requiere_revision: false`; en ese caso `en curso → hecho` está permitido con
  evidencia completa. El default es `true`.
- Los tickets históricos en `hecho` no se reescriben.

Justificación: el estado “pendiente de auditoría” pasa a ser consultable, el
trabajo sin auditar no desbloquea dependientes y el auditor tiene una cola real
en lugar de una ventana temporal frágil.

### Decisión 2 — Evidencia obligatoria y estructurada

Para entrar en `en revisión`, o en `hecho` cuando `requiere_revision=false`:

- `nota_cierre` debe contener las cinco secciones `Solución:`, `Cambios:`,
  `Verificación:`, `Artefactos:` y `Riesgos:`, cada una con contenido no vacío.
  Validación por prefijo de línea, sin parser complejo.
- `commit_sha` de 7–40 hex es obligatorio si `requiere_revision=true`. Se
  guarda en el ticket y en el payload de auditoría.
- Error `422 evidencia_incompleta` con la lista de secciones o campos faltantes.

Justificación: una nota que solo exige “no vacía” pasa con “ok”. El SHA
convierte la auditoría en “verificar este commit” en lugar de “verificar el
árbol ahora”.

### Decisión 3 — Veredicto de auditoría estructurado

- `POST /v1/tickets/{id}/revision` con
  `{"veredicto":"aprobado|rechazado","comment":"…","version":N}`.
  - `aprobado` → `hecho`.
  - `rechazado` → `abierto` e incrementa `reaperturas`.
- Comentario de tipo `revision` con el veredicto; eventos de auditoría
  `ticket.revision.aprobar` y `ticket.revision.rechazar`.
- Si `reaperturas` llega a `AGENTBANK_MAX_REAPERTURAS` (default 2):
  - el ticket pasa a `bloqueado`;
  - se crea automáticamente una `solicitud` al rol `dueño` con el historial.
- El auditor usa `ticket_next` con `status=en revisión` como cola, sin depender
  de `updated_since`.
- MCP: tool `ticket_review`.
- Chequeo de rol solo cuando el modo A sea real (`AGENTBANK_ENFORCE_REVIEWER=1`,
  apagado por defecto): quien pasó el ticket a `en revisión` no puede emitir su
  veredicto.

### Decisión 4 — Carriles secuenciales hasta que existan worktrees

- Se declara oficialmente: **un carril de implementación activo por repo**
  hasta implementar `worker --worktree`. Se corrige la documentación y
  `orquestador.md`.
- Diseño futuro (tickets, no se implementa en esta sesión):
  - `agentbank worker --worktree <dir_base>` crea
    `git worktree add <dir>/<epic-key> -b feat/<epic-key>` y corre el comando
    con `cmd.Dir` en ese worktree.
  - El merge a la rama base es serial al cerrar el carril y lo hace el humano o
    el orquestador con autorización.
  - El auditor verifica en un worktree propio con `git worktree add --detach <sha>`.
  - `ticket_next` agrega
    `advertencias: [{"tipo":"paths_solapados","con":"ABG-7"}]` si los `paths`
    del candidato se solapan por prefijo con un claim activo de otra épica. Es
    un aviso, no un bloqueo.

Justificación: no se documenta una capacidad que el ejecutor no tiene. El aviso
de paths es barato y no frena la cola.

### Decisión 5 — Identidad híbrida explícita

- Orquestador interactivo: **modo C** (un proceso, `opencode-main`).
- Implementador y auditor: **modo A** como procesos `agentbank worker`
  separados, con su propio token y una config de OpenCode propia
  (`OPENCODE_CONFIG`) cuyo MCP no fija `AGENTBANK_AGENT`.
- El orquestador lanza carriles como workers, no como subagentes de su proceso.
  Mientras se usen subagentes, la autoría real es modo C y este ADR lo dice.
- Los roles **no** son permisos. Los permisos siguen siendo de membresía
  (`lector`, `colaborador`, `admin`). La única excepción futura es el flag de la
  Decisión 3.

### Decisión 6 — Separar función y especialidad

- `participants.agent_role` = **función**:
  `orquestador | implementador | auditor | humano | otro`.
- El ticket mantiene `role` (especialidad: `backend`, `docs`, …) como único
  nombre; los prompts dejan de decir `assigned_role`.
- El worker usa `--agent-role` para la función y `--role` para la especialidad
  de la cola.
- Los snapshots históricos no se migran.

### Decisión 7 — Claims robustos

- Renovación implícita: cualquier escritura del dueño del claim sobre el ticket
  extiende `lease_until` a ahora + lease. También
  `POST /v1/tickets/{id}/claims/renovar`.
- Fencing: `avanzar` sobre un ticket `en curso` exige que el actor sea el dueño
  del claim vigente o admin; si no, `409 claim_ajeno`.
- Barrido automático: goroutine cada `AGENTBANK_SWEEP_INTERVAL` (default 60 s)
  con la misma lógica idempotente de `/admin/barridos`.
- Chequeo del worker al terminar: con exit 0 o no, lee el ticket; si sigue
  `en curso` con su claim, lo libera y comenta
  `worker: el comando terminó sin cerrar el ticket (exit=N)`.

### Decisión 8 — Semántica honesta e idempotencia

- `modo: divido` → `501 no_implementado` hasta que cree un ticket hijo real.
- `avanzar` acepta `Idempotency-Key`: repetir la clave devuelve la misma
  respuesta (incluido `next`) sin volver a aplicar.

### Decisión 9 — Contexto más chico para modelos Flash

- Variable opcional `AGENTBANK_MCP_TOOLS` (lista de prefijos) para filtrar tools
  por proceso. Sin la variable se exponen todas (compatible).
- Sugerencia: compactar alrededor de 150k tokens, porque la memoria durable está
  en AgentBank. Medir y documentar; no es parte del binario.

### Decisión 10 — Ajustes de permisos del auditor

- Reemplazar `agentbank *` por subcomandos de lectura y revisión explícitos (sin
  `agentbank api`).
- Permitir `gofmt -l*` y `git worktree*`.

## Lo que NO se hace

- **Matriz de permisos por rol:** duplica la autorización por membresía y en
  modo C no aporta.
- **Bloqueo duro por `paths`:** son declarativos e imprecisos; el aviso alcanza.
- **Reescribir snapshots o tickets históricos:** viola append-only.
- **Merge automático de worktrees:** sigue prohibido sin pedido explícito.
- **FTS5, ticket hijo real para `divido`, firmas de webhook con timestamp:**
  siguen deferred.

## Consecuencias

- El cierre de una épica pasa a ser consultable: todos sus tickets en `hecho`.
- Un ticket en `en revisión` sigue bloqueando a sus dependientes; el trabajo
  dependiente no se construye sobre código sin auditar.
- Los clientes que cerraban directo desde `en curso` reciben
  `409 transicion_invalida` y deben pasar por `en revisión` o usar
  `requiere_revision=false`.
- La evidencia deja de ser prosa libre: cinco secciones y un SHA.
- La reapertura tiene costo: incrementa `reaperturas` y escala al humano al
  llegar al tope.
- La autoría real por rol requiere workers en modo A; en modo C el rol del
  actor sigue siendo el del proceso.

## Dudas abiertas

- ¿El tope de reaperturas debe ser por ticket, por épica o por ventana de tiempo?
- ¿`commit_sha` debe referenciar un commit ya existente en el repo o alcanza con
  el formato? En esta etapa se valida formato; la existencia la verifica el
  auditor.
- ¿Conviene que `en revisión` también libere el claim del auditor cuando el
  veredicto se emite, o el claim de revisión es opcional?
- ¿La cola de revisión debe ordenar por antigüedad de `en revisión` o por
  prioridad?
- ¿El flag `AGENTBANK_ENFORCE_REVIEWER` debe mirar `agent_role` o una membresía
  de proyecto (`auditor`)?

## Plan de implementación

| Épica | Alcance | Estado |
|---|---|---|
| E1 | Revisión en la máquina de estados, evidencia, endpoint `revision`, cola y MCP | Implementada en esta sesión |
| E2 | Claims robustos: renovación, fencing, barrido automático, chequeo del worker | Planificada |
| E3 | Semántica e idempotencia: `divido` 501, `Idempotency-Key` en `avanzar` | Planificada |
| E4 | Identidad y roles: función/especialidad, `OPENCODE_CONFIG`, workers | Planificada |
| E5 | Worktrees: `worker --worktree`, verificación detached, aviso de paths | Planificada |
| E6 | Contexto y permisos: `AGENTBANK_MCP_TOOLS`, permisos del auditor, compactación | Planificada |
| E7 | Documentación y cierre de inconsistencias D12 | Planificada |

Dependencias: E2–E7 dependen de este ADR. E5 depende de E4. E7 se cierra al
final.
