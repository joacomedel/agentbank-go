# CLI — AgentBank Go

La CLI es un cliente fino de la API. No tiene lógica de negocio propia.

## Instalación

```bash
cd /root/agentbank-go
go build -trimpath -ldflags="-s -w" -o bin/agentbank ./cmd/agentbank
```

El mismo binario arranca el server, la CLI y el MCP:

```bash
agentbank              # servidor
agentbank serve        # servidor explícito
agentbank help         # ayuda
agentbank mcp          # MCP por stdio
```

## Configuración

Orden de precedencia:

1. `AGENTBANK_URL`, `AGENTBANK_TOKEN`, `AGENTBANK_HUMAN_TOKEN`,
   `AGENTBANK_PROJECT`, `AGENTBANK_AGENT`, `AGENTBANK_AGENT_ROLE`.
2. `.agentbank.json` en el directorio actual (`server`, `project`, `agent`,
   `agent_role`; sin secretos).
3. `~/.config/agentbank/config.json` (`server`, `project`, `agent`,
   `agent_role`).
4. `~/.config/agentbank/human.token` para el humano.
5. `~/.config/agentbank/tokens/<agente>.token` para agentes.

Defaults:

```text
AGENTBANK_URL=http://127.0.0.1:8090
```

### `agentbank login`

```bash
agentbank login --url http://127.0.0.1:8090 --token 'ab_...' --project API
```

Valida el token con `GET /v1/me` y lo guarda en
`~/.config/agentbank/human.token` con `0600`.

### `agentbank init`

```bash
agentbank init \
  --url http://127.0.0.1:8090 \
  --project API \
  --agent opencode-main \
  --role orquestador \
  --token 'ab_...'
```

Escribe:

- `.agentbank.json` con `{server, project, agent, agent_role}` (sin token, apto
  para git).
- `~/.config/agentbank/config.json` con los mismos datos.
- `~/.config/agentbank/human.token` con `0600` si se pasa `--token` o
  `--token-file`.

### `agentbank agent`

```bash
agentbank agent list
agentbank agent list --role auditor
agentbank agent ensure --agent auditor-1 --role auditor --project API
agentbank agent rotate --agent auditor-1
agentbank agent revoke --agent auditor-1
agentbank agent disable --agent auditor-1
```

- `ensure` crea/reutiliza el agente y guarda su token.
- `rotate` rota y guarda el token local.
- `revoke` revoca el token activo.
- `disable` da de baja al participante.

Los agentes se enrolan solos con `human.token`; no hace falta copiar tokens.

## Comandos principales

### Identidad y cambios

```bash
agentbank me
agentbank me --project API
agentbank changes --since '<cursor>' --project API
agentbank novedades --since '<cursor>' --wait 20
```

### Tickets

```bash
agentbank next --project API --role backend
agentbank next --project API --claim=false
agentbank advance --id API-42 --status hecho --nota-cierre "Qué hice..." --y-siguiente
agentbank advance --id API-42 --pregunta '{"asunto":"¿Seguimos?","cuerpo":"...","opciones":["sí","no"],"default_opcion":0,"modo":"sigo"}'
agentbank comment --id API-42 --body "Comentario" --type nota
```

### Proyectos, fases y épicas

```bash
agentbank project list
agentbank project create --slug demo --name "Demo" --prefix DEMO
agentbank project get --id demo
agentbank project pause --id demo
agentbank project resume --id demo
agentbank project members --id demo
agentbank project roles --id demo

agentbank phase list --project demo
agentbank phase create --project demo --key f1 --name "Fase 1"
agentbank phase get --id <fase>
agentbank phase edit --id <fase> --status cerrada

agentbank epic list --project demo
agentbank epic create --project demo --key e1 --title "Auth" --phase <fase>
agentbank epic edit --id <epica> --status cerrada
```

### Mensajes

```bash
agentbank inbox --project API --pending
agentbank resolve --id <mensaje> --option 0
agentbank resolve --id <mensaje> --text "respuesta"
```

Para enviar mensajes con opciones/default se usa `api` o el MCP.

### Documentos

```bash
agentbank doc list --project API
agentbank doc list --project API --updated-since 2026-11-08T00:00:00Z
agentbank doc create --project API --title "Arquitectura" --file docs/arquitectura.md --route docs/arquitectura.md
agentbank doc get --id <doc>
agentbank doc version --id <doc> --file docs/arquitectura.md
agentbank doc versions --id <doc>
agentbank doc diff --id <doc> --from 1 --to 2
agentbank doc sync --project API --dir docs
```

### Artefactos compartidos

```bash
agentbank artifact put --project API --path openapi.json --file dist/openapi.json --commit "$(git rev-parse HEAD)"
agentbank artifact put --project API --path openapi.json --stdin < dist/openapi.json
agentbank artifact list --project API --updated-since 2026-11-09T00:00:00Z
agentbank artifact list --all --path openapi.json
agentbank artifact get --project API --path openapi.json --out /tmp/openapi.json
agentbank artifact get --project API --path openapi.json --version 2
agentbank artifact delete --project API --id <uuid>
```

Los artefactos son legibles por toda la instancia; publicar requiere permiso de
escritura en el proyecto. Para CI y verificación de frescura ver
`docs/artifacts.md` y `scripts/publish-artifacts.sh`.

### Import

```bash
agentbank import --project API plan.json --dry-run
agentbank import --project API plan.json --update
```

El archivo es JSON con `fases`, `epicas` y `tickets` (mismo body que
`POST /v1/projects/{id}/import`).

### Auditoría y administración

```bash
agentbank audit chains
agentbank audit list --chain <chain_id> --limit 50
agentbank audit verify --chain system

agentbank admin sweeps
agentbank admin metrics
agentbank admin verify-all
agentbank admin outbox
agentbank admin dispatch
```

### Worker

```bash
agentbank worker --project API --role backend --cmd 'tu-comando' --max 10
agentbank worker --project API --agent worker-backend --agent-role backend --role backend --cmd 'tu-comando'
agentbank worker --project API --agent worker-epic --role backend --epic <epic> --cmd 'tu-comando'
agentbank worker --project API --agent worker-fase --role backend --phase <fase> --cmd 'tu-comando'
agentbank worker --project API --simulado --max 1
```

- Reclama con `ticket_siguiente`.
- Manda heartbeat mientras el comando corre.
- Si el comando falla, comenta y libera el claim.
- Si termina y el ticket sigue en curso, comenta "el agente terminó sin cerrar"
  y libera el claim.
- Expone `AGENTBANK_TICKET`, `AGENTBANK_TICKET_ID`, `AGENTBANK_TICKET_REF`,
  `AGENTBANK_TOKEN`, `AGENTBANK_URL` y `AGENTBANK_PROJECT` al comando hijo.
- `--agent`/`--agent-role` definen la identidad y el rol funcional; si no hay
  token local, se enrola con `human.token`.
- `--epic`/`--phase` acotan el carril: el worker solo reclama tickets de esa
  épica o fase. Es la forma recomendada de correr un carril por épica.
- No implementa `opencode-server` ni `--worktree` todavía.

### Escape hatch

Cualquier endpoint se puede llamar con `api`:

```bash
agentbank api --method GET --path /v1/projects
agentbank api --method POST --path /v1/messages \
  --body '{"project_id":"API","type":"info","destination_type":"proyecto","subject":"Hola","body":"..."}'
agentbank api --method POST --path /v1/tickets/siguiente \
  --idempotency-key "next-1" --body '{"project_id":"API","claim":true}'
```

Flags:

- `--method`
- `--path`
- `--body` (JSON inline o `@archivo`)
- `--query k=v` (repetible)
- `--header k=v` (repetible)
- `--idempotency-key`

## Salida

Todos los comandos imprimen JSON indentado. Los errores de API salen por stderr
con `error` y `code`.

## Límites

- El `worker` es básico (`cmd`/`simulado`); no incluye el ejecutor de OpenCode 2
  ni worktrees.
- Telegram queda para la etapa 9.
- La UI de producto no está en este repo.
