# Subagentes sobre AgentBank

> Estado: propuesta implementada como configuración de OpenCode. Fecha:
> 2026-11-09. Aplica a OpenCode V2 y a AgentBank Go.

## 1. Recomendación corta

**No armes un subagente por ticket ni por fase.** Los subagentes de OpenCode
son *roles* con prompt, modelo y permisos; no son instancias por unidad de
trabajo. La división que mejor funciona es:

```text
Rol (subagente)        → fijo: orquestador, implementador, auditor
Carril de ejecución    → por épica
Unidad de trabajo      → por ticket
Punto de control       → por fase
Auditoría              → por ticket, con cierre de épica
```

En una frase: **roles fijos, carriles por épica, tickets como unidad atómica y
auditoría por cierre**.

### Por qué

| Opción | Ventaja | Problema |
|---|---|---|
| Subagente por ticket | Contexto limpio por ticket | Pierde decisiones de la épica, duplica setup, multiplica coordinación y costo |
| Subagente por fase | Menos agentes | Sesiones larguísimas, compactación frecuente, un ticket bloqueado frena toda la fase |
| Subagente por épica | Contexto compartido y acotado; cierre claro | Hay que controlar que no se mezclen épicas |
| **Roles fijos + carril por épica** | **Equilibrio: contexto, aislamiento y auditoría** | Requiere disciplina de claims y paths |

La épica es la unidad natural de contexto: comparte arquitectura, decisiones y
tests. El ticket es la unidad natural de *claim*, cierre y evidencia.

## 2. Qué encontré en OpenCode V2

Fuente: <https://opencode.ai/v2/docs/> (agents, skills, config, compaction,
mcp-servers).

- Un agente es un Markdown con frontmatter en
  `~/.config/opencode/agents/<nombre>.md` o `.opencode/agents/<nombre>.md`.
- `mode: subagent` solo corre en sesión hija lanzada por el agente principal;
  `mode: primary` corre como agente principal; `mode: all` permite ambas. Los
  roles de carril usan `all` para poder correr también como worker con
  `opencode run --agent ...`.
- Los subagentes corren con **contexto fresco** en sesiones hijas. No comparten
  el historial del padre.
- `permissions` es una lista ordenada de reglas `action`/`resource`/`effect`.
  Gana la última regla que matchea. Sirve para hacer un auditor de solo
  lectura o un implementador sin `git push`.
- `steps` limita los pasos del agente; al llegar al final lo obliga a resumir.
- Modelo por agente: `provider/model#variant`, por ejemplo
  `opencode-go/deepseek-v4.1-flash#high`.
- Las **skills** son `SKILL.md` en `.opencode/skills/<id>/` o
  `~/.config/opencode/skills/<id>/`. Se anuncian por `description` y se cargan
  on demand con la tool `skill`. No conviene meter todo en el prompt.
- Los MCP se configuran en `mcp.servers.<nombre>` (V2; la forma V1
  `mcp.<nombre>` ya no se usa).
- OpenCode manda `_meta["ai.opencode/sessionID"]` en las llamadas MCP, pero
  AgentBank no lo usa como identidad: la identidad sale del token.
- La compactación automática es un resumen; `keep.tokens` conserva el tramo
  reciente y `buffer` es el espacio libre antes de compactar.

## 3. Qué hay en “awesome opencode” y qué no copiar

El ecosistema tiene catálogos enormes: 108–175 agentes, 136+ skills, plugins de
memoria, reporte de subagentes, orquestadores y frameworks multiagente
(`weisser-dev/awesome-opencode`, `Diaszano/awesome-opencode-agents`,
`VoltAgent/awesome-codex-subagents`, `awesome-opencode/awesome-opencode`).

Patrones útiles:

- **Un rol, una responsabilidad.** Reviewer de solo lectura, implementador con
  escritura, investigador sin edición.
- **Permisos mínimos.** El auditor no edita; el implementador no hace push.
- **Contrato de salida.** El subagente devuelve un formato fijo (hallazgos,
  archivos, veredicto), no un chat libre.
- **Orquestador que no implementa.** El padre reparte, integra y controla.
- **Trazabilidad.** Plugins como “Subagent Reporter” muestran qué hizo cada
  subagente; en AgentBank eso lo resuelve el timeline + auditoría.

Qué **no** copiar:

- Instalar 100+ agentes: el modelo elige mal y el contexto se llena de
  descripciones.
- Agentes que editan y auditan a la vez.
- Depender de memoria vectorial para el estado del proyecto: AgentBank ya tiene
  tickets, documentos, timeline y auditoría.
- Frameworks que inventan su propio estado paralelo al banco.

## 4. Modelo recomendado

### Roles

| Agente | Modo | Modelo | Permisos | Para qué |
|---|---|---|---|---|
| `orquestador` | `primary` | flash `#high` | todo menos push/merge | Planifica fase/épica, crea tickets, lanza carriles |
| `implementador` | `all` | flash `#high` | edita, shell, sin push/merge, sin subagentes | Trabaja una épica ticket por ticket |
| `auditor` | `all` | flash `#max` | código de solo lectura; MCP de AgentBank con escritura | Verifica cierres, comenta, reabre y crea seguimiento |

Todos usan **DeepSeek V4.1 Flash**. El auditor usa el variant `max` del mismo
modelo; el resumen de compactación también usa Flash.

Los archivos están en `opencode/agents/`. El instalador los copia a
`~/.config/opencode/agents/`.

**Todos los roles pueden escribir tickets.** El MCP de AgentBank expone
`ticket_create`, `ticket_edit`, `ticket_advance`, `ticket_comment`,
`ticket_claim`, `ticket_release`, `ticket_depend`, `ticket_attach_document`,
`messages_send` y `document_create`. Los permisos de archivo del auditor no
bloquean esas tools: solo le impiden editar código.

### Carril por épica

```text
orquestador
  ├─ implementador(epic=A)  → ticket_next(epic=A) → ... → cierre
  ├─ implementador(epic=B)  → solo si no comparte paths con A
  └─ auditor(epic=A)        → tickets hechos → aprobar/reabrir
```

- Un carril por épica.
- No paralelices tickets que comparten `paths`.
- El carril termina cuando `ticket_next` devuelve `ticket: null` con
  `motivo: sin_candidatos`.
- La épica se cierra cuando no quedan tickets abiertos/en curso y el auditor
  aprobó.

### Cómo se pasan los tickets

Hay tres formas, de la más simple a la más automatizada:

1. **El orquestador le da el `ticket_id`** al implementador en el prompt. Sirve
   para un ticket puntual.
2. **El implementador pide el siguiente** con `ticket_next`:

   ```json
   {"project_id":"demo","epic_id":"<epic>","role":"backend","claim":true}
   ```

   Es atómico: crea el claim, pone el ticket `en curso` y devuelve el contexto.
3. **Un worker por épica**:

   ```bash
   agentbank worker \
     --agent impl-epic-a --agent-role backend \
     --project demo --epic <epic> --role backend \
     --cmd 'opencode run --agent implementador --auto "Trabajá los tickets de esta épica"'
   ```

   El worker reclama, inyecta `AGENTBANK_TICKET`, `AGENTBANK_TICKET_ID` y
   `AGENTBANK_TOKEN`, y libera el claim si el comando falla.

Para cerrar y pedir el siguiente en una sola llamada:

```json
{
  "version": 7,
  "status": "hecho",
  "nota_cierre": "Solución: ...\nCambios: ...\nVerificación: ...\nArtefactos: ...\nRiesgos: ...",
  "y_siguiente": true
}
```

## 5. Identidad: la letra chica

OpenCode **no tiene MCP por subagente**. Todos los subagentes de un mismo
proceso comparten el servidor MCP y, por lo tanto, el mismo token de AgentBank.
Si el MCP está configurado con `AGENTBANK_AGENT=opencode-main`, todo lo que
hagan los subagentes queda auditado como `opencode-main`.

Tenés dos modos:

### Modo A — una identidad por proceso (recomendado)

- Cada rol corre en su propio proceso/worker con su propio token.
- El MCP no fija `AGENTBANK_AGENT`; usa el `AGENTBANK_TOKEN` heredado.
- El worker ya inyecta el token del agente.
- Así el auditor tiene su propio `agent_role` y su propio token.

```bash
# el worker ya hace el ensure y pasa AGENTBANK_TOKEN
agentbank worker --agent auditor-1 --agent-role auditor \
  --project demo --role auditor --max 1 \
  --cmd 'opencode run --agent auditor --auto "Auditá los cierres de la épica A"'
```

Para que el MCP herede ese token, el servidor `agentbank` **no** debe fijar
`AGENTBANK_AGENT` en su `environment`. Si lo fija, `agentbank mcp` intenta
auto-enrolar ese nombre y pisa la identidad del worker.

Si ya corriste el setup normal, regenerá la config sin agente:

```bash
AGENTBANK_TOKEN='ab_...' scripts/setup-opencode-mcp.py --no-agent --project demo
```

O quitá `AGENTBANK_AGENT` y `AGENTBANK_AGENT_ROLE` del `environment` del server
`agentbank` en `~/.config/opencode/opencode.json`.

### Modo B — escrituras por CLI con rol explícito

Si querés que varios subagentes del mismo proceso tengan autoría distinta, usá
el CLI para las escrituras:

```bash
AGENTBANK_AGENT=auditor agentbank comment --id <id> --body "..." --type nota
AGENTBANK_AGENT=implementador agentbank advance --id <id> --status hecho --nota-cierre "..."
```

Cada llamada resuelve/crea el token de ese agente con `human.token`. El MCP
queda para lecturas. Es más verboso, pero da autoría real por rol.

### Modo C — una identidad compartida

Es el modo más simple: todo el proceso es `opencode-main`. Sirve si el
orquestador es el único que escribe y los subagentes son “manos” efímeras.
En ese caso, la trazabilidad fina queda en el timeline y en el comentario de
cierre, no en `created_by_role`.

En cualquiera de los tres modos, **todos los roles pueden crear/editar/cerrar
tickets**: lo que cambia es a nombre de qué participante queda el snapshot del
rol.

## 6. Auditar cómo se cerró un ticket

AgentBank guarda el rol del actor en **cada creación y cambio**:

- Proyecto/participante/membresía: `created_by_role` / `granted_by_role`.
- Fase/épica/ticket: `created_by_role`; el ticket además `updated_by_role`.
- Comentario: `author_role`.
- Mensaje: `from_role`, `resolved_by_role`, `taken_by_role`.
- Documento/versión: `created_by_role`, `author_role`.
- Decisión/webhook/claim/dependencia: `created_by_role`.
- Evento de auditoría: `actor_role` y `session_id` en cada cambio.
- Timeline del ticket: comentarios + eventos con rol y sesión.
- Comentario de tipo `cierre` con la nota de cierre.
- Cadena de auditoría verificable (`audit_verify`).

El histórico completo de cualquier objeto se consulta con
`GET /v1/audit?object_id=<id>`. Cambiar el `agent_role` de un agente no
reescribe los snapshots viejos.

### Flujo del auditor

1. Buscar cierres:

   ```json
   {"project_id":"demo","status":"hecho","epic_id":"<epic>","updated_since":"2026-11-09T00:00:00Z","limit":50}
   ```

2. Por cada ticket: `ticket_get`, `ticket_timeline`, `ticket_comments`,
   `audit_list` (`object_id`), `ticket_documents`.
3. Verificar el repo: `git show`, `git diff`, `go test`, `go vet`.
4. Aprobar con `ticket_comment` tipo `nota` o reabrir con
   `ticket_advance` (`status: "abierto"`) y el motivo.
5. Reportar tabla de aprobados/rechazados y próximo paso.

La skill `agentbank-audit` tiene el checklist completo.

### Plantilla de cierre

```text
Solución: qué se hizo y por qué alcanza.
Cambios: archivos/módulos tocados.
Verificación: comando exacto + resultado.
Artefactos: commit/PR/documento.
Riesgos: pendientes, deuda o “ninguno”.
```

Sin esa nota, el auditor rechaza. La nota es la memoria durable del ticket:
la compactación puede perder contexto, pero el cierre queda en AgentBank.

## 7. Compactación para DeepSeek V4.1 Flash

DeepSeek V4.1 Flash en OpenCode Go tiene **1.000.000 de tokens** de contexto.
Para compactar a los **400.000**:

```json
{
  "model": "opencode-go/deepseek-v4.1-flash",
  "small_model": "opencode-go/deepseek-v4.1-flash",
  "agents": {
    "compaction": { "model": "opencode-go/deepseek-v4.1-flash" }
  },
  "compaction": {
    "auto": true,
    "keep": { "tokens": 24000 },
    "buffer": 600000
  }
}
```

El umbral real de V2 es `contexto - max(output, buffer)`. DeepSeek V4.1 Flash
tiene `context=1.000.000` y `output=384.000`; con `buffer=600.000` queda
`1.000.000 - 600.000 = 400.000`. Está aplicado en
`~/.config/opencode/opencode.json`.

Notas:

- La compactación es **lossy**: resume lo viejo y conserva ~24k recientes.
- Si el trabajo depende de detalles exactos, subí `keep.tokens` o compactá a
  mano antes:

  ```bash
  opencode api post /api/session/<sesión>/compact --data '{}'
  ```

- `buffer` como número se ignora si es mayor o igual al contexto del modelo;
  para modelos chicos vuelve al default.
- Opcional: `experimental.background_compaction: true` escribe el resumen en
  segundo plano, pero puede gastar tokens si la sesión termina antes.

## 8. Instalación

```bash
cd /root/agentbank-go

# agentes + skills globales
scripts/install-opencode-assets.py

# MCP modo simple: una identidad por proceso interactivo
AGENTBANK_TOKEN='ab_...' scripts/setup-opencode-mcp.py \
  --project demo --agent opencode-main --role orquestador

# MCP modo multi-rol: el token lo pone cada worker/proceso
AGENTBANK_TOKEN='ab_...' scripts/setup-opencode-mcp.py \
  --no-agent --project demo
```

El instalador copia:

```text
~/.config/opencode/
  agents/orquestador.md
  agents/implementador.md
  agents/auditor.md
  skills/agentbank-ticket/SKILL.md
  skills/agentbank-audit/SKILL.md
  skills/agentbank-orchestrate/SKILL.md
```

No copia tokens. El token humano queda en
`~/.config/agentbank/human.token` y cada agente en
`~/.config/agentbank/tokens/<agente>.token`.

## 9. Ejemplo de operación

```text
Humano: creá la fase 2 y la épica “login OAuth”; después corré el carril.
Orquestador:
  phase_create, epic_create, ticket_create x4, ticket_depend
  subagent implementador(epic=<id>)
Implementador:
  ticket_next(epic=<id>, claim=true) → implementa → ticket_advance(y_siguiente=true)
  ... hasta ticket:null
Orquestador:
  subagent auditor(epic=<id>)
Auditor:
  tickets_list(status=hecho, epic=<id>) → verifica → aprueba/reabre
Orquestador:
  epic_edit(status=cerrada) → phase_edit(status=cerrada)
```

## 10. Límites conocidos

- El MCP de AgentBank no distingue subagentes por token; la identidad es del
  proceso (ver §5).
- No hay `worktree` automático por ticket en el worker todavía.
- El auditor no tiene acceso a un “diff” guardado en AgentBank: lo lee del
  repo. Por eso el cierre debe incluir commit/PR.
- La compactación no reemplaza la memoria del proyecto: para eso están los
  tickets, documentos y notas de cierre.
