# ADR 0006 — Tokens por agente, auto-enrolamiento y `agent_role`

- Fecha: 2026-11-08
- Estado: aceptado
- Alcance: `/root/agentbank-go`

## Contexto

El MCP y la CLI ya existían, pero todos los clientes usaban el mismo token
(humano o `AGENTBANK_TOKEN`). Eso hace que la auditoría no distinga agentes y
que revocar uno afecte a todos. Además, el operador no quiere copiar tokens de
agente a mano.

También faltaba saber si un ticket lo creó el auditor, el orquestador o un
worker, incluso si el agente cambia de rol después.

## Decisiones

### Token humano único para administración

- `agentbank login` valida el token con `GET /v1/me` y lo guarda en
  `~/.config/agentbank/human.token` con `0600`.
- El humano no usa ese token para trabajar: solo para enrolar/rotar/revocar
  agentes y administrar.
- `.agentbank.json` y `config.json` no contienen secretos.

### Token por agente y auto-enrolamiento

- Cada agente tiene `~/.config/agentbank/tokens/<agente>.token` (`0600`).
- `agentbank mcp` y `agentbank worker` resuelven el agente por `--agent`,
  `AGENTBANK_AGENT` o `.agentbank.json`.
- Si el token local es válido, se usa sin tocar el token humano.
- Si falta o fue revocado, el CLI usa `human.token` para:
  - buscar el participante por nombre;
  - crearlo si no existe;
  - actualizar `agent_role`;
  - asegurar membresía en el proyecto;
  - emitir/rotar token;
  - guardarlo localmente.
- Un token compartido queda prohibido para agentes.

### `agent_role`

- `participants.agent_role` es una etiqueta funcional global:
  `orquestador`, `auditor`, `backend`, `reviewer`, etc.
- No reemplaza los roles de membresía por proyecto.
- Al crear un ticket se snapshotea en `tickets.created_by_role`.
- Cada evento de auditoría guarda `audit_events.actor_role`.
- Cambiar el rol de un agente no reescribe tickets históricos.

### Administración

- `agentbank agent list [--role]`
- `agentbank agent ensure --agent A --role R --project P`
- `agentbank agent rotate --agent A`
- `agentbank agent revoke --agent A`
- `agentbank agent disable --agent A`
- API: `PATCH /v1/participants/{id}` y
  `POST /v1/participants/{id}/tokens/revoke`.

### OpenCode

- Script `scripts/setup-opencode-mcp.py` hace login, enrola el agente y agrega
  la entrada `mcp.agentbank` a `~/.config/opencode/opencode.json`.
- La config global solo lleva `AGENTBANK_AGENT`, `AGENTBANK_AGENT_ROLE`,
  `AGENTBANK_PROJECT` y `AGENTBANK_URL`; nunca el token.
- Para agentes distintos, usar otro `AGENTBANK_AGENT` en su configuración.

## Consecuencias

- La auditoría identifica al agente real, no al humano.
- Revocar/rotar un agente no afecta a los demás.
- Los permisos, roles y rate limits son por agente.
- El humano solo administra un token; los agentes se auto-enrolan.
- Si se pierde `human.token`, hay que recuperar/rotar la credencial humana.
- Si un agente se enrola con el nombre equivocado, se crea otro participante;
  conviene fijar `agent`/`agent_role` en `.agentbank.json`.
