# MCP — AgentBank Go

Servidor MCP por stdio, sin perfiles. Expone todas las tools disponibles y un
escape hatch `api_request`. Cada agente se auto-enrola con su propio token.

## Arranque

```bash
agentbank mcp
```

Configuración por entorno:

```bash
export AGENTBANK_URL=http://127.0.0.1:8090
export AGENTBANK_PROJECT=API
export AGENTBANK_AGENT=opencode-main
export AGENTBANK_AGENT_ROLE=orquestador
agentbank mcp
```

También lee `~/.config/agentbank/config.json`, `.agentbank.json` y los tokens
locales. Si el token del agente no existe, lo crea con
`~/.config/agentbank/human.token`. No hay secretos en `.agentbank.json`.

## Configuración en OpenCode

```json
{
  "mcp": {
    "agentbank": {
      "type": "local",
      "command": ["/usr/local/bin/agentbank", "mcp"],
      "environment": {
        "AGENTBANK_URL": "http://127.0.0.1:8090",
        "AGENTBANK_AGENT": "opencode-main",
        "AGENTBANK_AGENT_ROLE": "orquestador",
        "AGENTBANK_PROJECT": "API"
      }
    }
  }
}
```

El token humano se guarda una vez con `agentbank login`; el MCP no lo recibe.
Cada agente usa `tokens/<agente>.token`.

Para configurarlo automáticamente:

```bash
AGENTBANK_TOKEN='ab_...' scripts/setup-opencode-mcp.py \
  --project API --agent opencode-main --role orquestador
```

El script hace login, enrola el agente y escribe la entrada `mcp.agentbank` en
`~/.config/opencode/opencode.json` sin tokens.

## Protocolo

- JSON-RPC 2.0 por stdio, un mensaje por línea.
- Métodos: `initialize`, `notifications/initialized`, `ping`, `tools/list`,
  `tools/call`.
- Protocol version: `2025-06-18`.
- No hay `resources` ni `prompts` en esta etapa.

## Tools

Hay **75 tools**, sin filtro por perfil. Categorías:

- Identidad: `me`, `participants_list`, `participant_create`,
  `participant_update`, `participant_token`, `participant_revoke_token`,
  `participant_disable`, `token_revoke`.
- Proyectos y membresías: `projects_list`, `project_create`, `project_get`,
  `project_edit`, `project_pause`, `project_resume`, `members_list`,
  `member_set`, `member_remove`, `roles_list`.
- Fases y épicas: `phases_list`, `phase_create`, `phase_get`, `phase_edit`,
  `epics_list`, `epic_create`, `epic_get`, `epic_edit`.
- Tickets: `tickets_list`, `ticket_create`, `ticket_get`, `ticket_edit`,
  `ticket_advance`, `ticket_next`, `ticket_claim`, `ticket_release`,
  `ticket_comment`, `ticket_comments`, `ticket_timeline`, `ticket_depend`,
  `ticket_undepend`, `ticket_documents`, `ticket_attach_document`.
- Mensajes: `messages_send`, `messages_list`, `message_get`, `inbox`,
  `message_state`, `message_take`, `message_resolve`, `message_discard`,
  `message_convert`.
- Documentos: `documents_list`, `document_create`, `document_get`,
  `document_version`, `document_versions`, `document_diff`, `documents_sync`.
- Decisiones: `decisions_list`, `decision_create`.
- Contexto y cambios: `changes`, `novedades`, `search`, `briefing`, `digest`,
  `tablero`.
- Import: `import_plan`.
- Auditoría: `audit_chains`, `audit_list`, `audit_verify`.
- Administración: `admin_sweeps`, `admin_metrics`, `admin_verify_all`,
  `admin_outbox`, `admin_dispatch`.
- Escape hatch: `api_request` (`method`, `path`, `body`, `query`,
  `idempotency_key`).

`tools/list` devuelve `inputSchema` por tool. Los argumentos de path se toman
del nombre (`id`, `participant_id`, `depends_on`). Si falta `id` o `project_id`
y hay `AGENTBANK_PROJECT`, se usa como fallback. `participant_create` acepta
`agent_role` (auditor, orquestador, backend, etc.).

## Flujos con pocas llamadas

| Flujo | Tools | Llamadas |
|---|---|---:|
| Arranque | `me` + `ticket_next` | 2 |
| Loop por ticket | `ticket_next` + `ticket_advance` | 2 |
| Ver novedades | `changes` o `tickets_list(updated_since)` | 1 |
| Esperar trabajo | `novedades` con `wait` | 1 |
| Bandeja | `inbox` + `message_resolve` por N | 1 + N |
| Planificar | `import_plan` (dry-run + real) | 1–2 |
| Tablero | `tablero` | 1 |
| Docs | `documents_list(updated_since)` + `document_get` de M | 1 + M |
| Auditar | `audit_verify` | 1 |

Detalle en `docs/mcp-flows.md`.

## Ejemplos de `tools/call`

```json
{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"me","arguments":{"proyecto":"API"}}}
```

```json
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"ticket_next","arguments":{"project_id":"API","role":"backend","claim":true}}}
```

```json
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"ticket_advance","arguments":{"id":"API-42","version":3,"status":"hecho","nota_cierre":"Listo","y_siguiente":true}}}
```

```json
{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"changes","arguments":{"project_id":"API","since":"<cursor>"}}}
```

## Idempotencia

Las tools de escritura aceptan `idempotency_key`. Se envía como
`Idempotency-Key` a la API.

## Límites

- No hay perfiles: todas las tools están visibles. Si el costo de contexto se
  vuelve un problema, se puede agregar un filtro opcional más adelante.
- No incluye Telegram.
- La UI de producto se construye aparte.
- El MCP no decide identidad: el autor sale del token en la API.
