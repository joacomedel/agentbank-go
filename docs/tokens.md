# Tokens por agente y roles funcionales

Objetivo: que vos manejes **un solo token humano** y que cada agente se enrole
solo con su propia identidad.

## Modelo

```text
~/.config/agentbank/
  human.token                0600  ← solo administración/enrolamiento
  config.json                      ← {server, project, agent, agent_role}
  tokens/
    opencode-main.token      0600
    worker-backend.token     0600
```

- `.agentbank.json` en el repo solo tiene `{server, project, agent, agent_role}`.
  Nunca secretos.
- El token humano se guarda una vez con `agentbank login`.
- El token de cada agente se guarda en `tokens/<agente>.token` con `0600`.
- El humano no copia tokens de agentes.

## Alta inicial

```bash
# una sola vez
agentbank login --url http://127.0.0.1:8090 --token 'ab_...'

# en el repo
agentbank init --project API --agent opencode-main --role orquestador
```

O todo junto para OpenCode:

```bash
AGENTBANK_TOKEN='ab_...' scripts/setup-opencode-mcp.py \
  --project API --agent opencode-main --role orquestador
```

El script hace `login`, `agent ensure` y escribe la entrada MCP en
`~/.config/opencode/opencode.json` sin tokens.

## Auto-enrolamiento

`agentbank mcp` y `agentbank worker` resuelven el agente así:

1. `--agent` o `AGENTBANK_AGENT`.
2. `agent` de `.agentbank.json` / `config.json`.
3. Si no hay token local válido, usan `human.token` para:
   - buscar el participante por nombre;
   - crearlo si no existe, con `agent_role` y membresía;
   - actualizar `agent_role` si cambió;
   - emitir/rotar el token;
   - guardarlo en `tokens/<agente>.token`.
4. Si el token local sigue válido, no tocan el token humano.

## Rol funcional del agente

`agent_role` es una etiqueta global del agente: `orquestador`, `auditor`,
`backend`, `reviewer`, etc. No reemplaza los roles de membresía por proyecto.

Se guarda en:

- `participants.agent_role`;
- el snapshot `created_by_role` (o `author_role`, `from_role`,
  `granted_by_role`) de cada creación;
- `tickets.updated_by_role` en cada edición/claim/avance;
- `audit_events.actor_role` y `session_id` en cada evento.

Así podés saber si un ticket lo creó el auditor, el orquestador o un worker,
incluso si después el agente cambia de rol. El mismo snapshot se guarda en
proyectos, fases, épicas, comentarios, mensajes, documentos, versiones,
decisiones, webhooks y claims. Cada cambio individual, además, queda en
`audit_events.actor_role`; el historial de un objeto se consulta con
`GET /v1/audit?object_id=<id>`.

## Comandos

```bash
agentbank agent list
agentbank agent list --role auditor
agentbank agent ensure --agent auditor-1 --role auditor --project API
agentbank agent rotate --agent auditor-1
agentbank agent revoke --agent auditor-1
agentbank agent disable --agent auditor-1
```

- `ensure`: crea/reutiliza el agente y guarda su token.
- `rotate`: revoca el token anterior y guarda el nuevo localmente.
- `revoke`: revoca el token activo; el agente puede re-enrolarse con el humano.
- `disable`: da de baja al participante y revoca todos sus tokens.

## Worker

```bash
agentbank worker \
  --agent worker-backend \
  --agent-role backend \
  --role backend \
  --project API \
  --cmd 'tu-comando'
```

- `--agent`/`--agent-role` definen identidad y etiqueta.
- `--role` es el rol de trabajo para la cola.
- Si no hay token de agente, se enrola con `human.token`.

## MCP

Config de OpenCode:

```json
{
  "mcp": {
    "servers": {
      "agentbank": {
        "type": "local",
        "command": ["/usr/local/bin/agentbank", "mcp"],
        "environment": {
          "AGENTBANK_AGENT": "opencode-main",
          "AGENTBANK_AGENT_ROLE": "orquestador",
          "AGENTBANK_PROJECT": "API"
        }
      }
    }
  }
}
```

- El MCP no recibe el token humano.
- El MCP puede leer y escribir: crear/editar/cerrar tickets, comentar, mensajes
  y documentos. El rol no limita las tools MCP.
- Si `tokens/opencode-main.token` no existe, el primer arranque lo crea con
  `human.token`.
- Para otro agente, usá otro `AGENTBANK_AGENT` en su configuración.
- Si querés que el MCP use el token heredado de un worker (carriles por rol),
  generá la config con `--no-agent`: no fija `AGENTBANK_AGENT` y `agentbank mcp`
  usa `AGENTBANK_TOKEN`.

## Por qué no un token compartido

- La auditoría dejaría de distinguir agentes.
- No podrías revocar a un agente sin afectar a todos.
- Los permisos, roles y rate limits serían globales.
- No podrías investigar un incidente por agente.

## Probarlo

```bash
cd /root/agentbank-go
go build -trimpath -ldflags='-s -w' -o bin/agentbank ./cmd/agentbank
./scripts/smoke_agent_tokens.py
```

El smoke arranca un server temporal, usa una config temporal y verifica:
login, init, proyecto, `agent ensure`, `me` con `agent_role`, ticket con
`created_by_role`, auditoría con `actor_role`, rotación, revocación,
re-enrolamiento, MCP y baja. No toca tu `~/.config/agentbank` real.

## Seguridad

- `human.token` y `tokens/*.token` van con `0600`; el directorio con `0700`.
- No se imprimen tokens en logs ni en respuestas de `agent ensure`.
- Si `devctl run build` falla con “Text file busy”, hay un server corriendo
  desde `bin/agentbank`; detenelo antes de publicar.
