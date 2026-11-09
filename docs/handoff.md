# Handoff — estado y playbook para el próximo agente

Este archivo existe para que un agente nuevo **no dependa de que el humano le
explique la sesión anterior**. Si vas a tocar AgentBank Go, leé esto después de
`AGENTS.md` y `TESTING.md`.

## 1. Estado actual

- Backend Go + SQLite, sin UI de producto.
- Etapas **0–7 implementadas**: identidad/auditoría, tickets/cola, fases/épicas,
  mensajes, documentos, operación/hardening, CLI/MCP/worker básico.
- **57 tests** verdes.
- `main` incluye OpenAPI/Swagger, feed incremental `/v1/changes`, CLI y MCP de
  75 tools sin perfiles, tokens por agente, `agent_role`, snapshots de rol en
  cada creación y subagentes de OpenCode (orquestador/implementador/auditor).
- Migración `0009_actor_roles_everywhere.sql` verificada sobre una base creada
  con el binario anterior (0008): agrega columnas y rellena `created_by_role`
  de tickets, comentarios, fases y documentos desde `participants.agent_role`.
  Las filas sin rol reconstruible quedan con `''`.
- Pendiente: ejecutor `opencode-server`/worktrees del worker, Telegram,
  contratos entre proyectos, bloqueos/escalado y choque de áreas.
- La UI se construye **fuera de este repo** contra OpenAPI.

## 2. Orden de lectura para un agente nuevo

1. `AGENTS.md`
2. `TESTING.md`
3. `docs/handoff.md` (este archivo)
4. `docs/rewrite-plan.md`
5. `docs/paridad-original.md`
6. `docs/api.md`
7. `docs/mcp-flows.md`
8. `docs/tokens.md`
9. `docs/subagentes.md`
10. `docs/decisions/0004-fases-2-6.md`, `0005-devctl-iteraciones.md` y
    `0006-tokens-por-agente.md`

## 3. Flujo de trabajo

### Loop interno

```bash
cd /root/pipelineCompilacion
./bin/devctl run test-core --project /root/agentbank-go
./bin/devctl run vet-core  --project /root/agentbank-go
```

### Cierre de etapa

```bash
./bin/devctl run fmt   --project /root/agentbank-go
./bin/devctl run vet   --project /root/agentbank-go
./bin/devctl run test  --project /root/agentbank-go
./bin/devctl run build --project /root/agentbank-go
./bin/devctl metrics --last 10 --project /root/agentbank-go
```

- `state_dir` del proyecto: `/var/tmp/devctl-agentbank` (definido en
  `dev-environment.toml`).
- `devctl run build` compila stripped, publica `bin/agentbank` en el repo y
  registra el artefacto.
- No hace falta repetir `go build -ldflags='-s -w'` a mano.

### Reglas de recursos

- Probe local a 54 °C; abort térmico a 56 °C.
- `memory_max` del proyecto: 512M.
- `taskset 0-1`, una corrida local por vez.
- `test-race` no corre en el Moto; va a CI.

## 4. Pitfalls que ya nos costaron tiempo

### SQLite con una sola conexión

`db.Open` usa `SetMaxOpenConns(1)`. **Nunca consultes `s.db` mientras un
`*sql.Rows` esté abierto**: recolectá las filas, cerrá el `Rows` y después
consultá. Si no, se produce un deadlock. Los casos que ya se corrigieron
estaban en `ListTickets`, `ListPhases` y `listTicketsByQuery`.

### Auditoría

- Toda acción nueva debe agregarse a `internal/audit/actions.go`; si no,
  `audit.Append` falla con “acción desconocida”.
- El evento de negocio y su auditoría van en la misma transacción.
- No cambies el algoritmo de hash sin una migración/versión; las cadenas
  existentes deben seguir verificando.

### Migraciones

- Son forward-only y embebidas.
- No edites una migración ya aplicada; agregá una nueva.
- Los tests abren SQLite temporal y corren todas las migraciones.

### OpenAPI

- Hay un test de drift (`internal/httpapi/openapi_test.go`) que falla si una
  ruta registrada no está en OpenAPI.
- Si agregás una ruta, actualizá `openAPIOperations()`.

### Procesos y artefactos

- Si `devctl run build` falla con “Text file busy”, hay un `agentbank` corriendo
  desde `bin/agentbank`. Paralo antes de publicar.
- Los smoke tests deben usar binarios en `/tmp/opencode` y cerrarse al terminar.
- No dejes servidores de prueba escuchando en `127.0.0.1:1809x`.

### Mensajes y D20

- El contenido ajeno se devuelve también en `*_content` con
  `untrusted: true`.
- No renderices HTML ajeno en un cliente.

## 5. Optimización de llamadas

- `GET /v1/changes` trae cambios de tickets, mensajes, docs, fases, épicas,
  decisiones y participantes desde un cursor, con resumen.
- `updated_since` en tickets/docs trae solo cambios.
- `ticket_siguiente` trae ticket + contexto + claim.
- `ticket_avanzar` combina estado + nota + pregunta + siguiente.
- `me?proyecto=` trae briefing.
- `novedades?wait=` es long-poll.
- Flujos y conteos: `docs/mcp-flows.md`.

## 6. Tokens, CLI y MCP

- Token humano: `~/.config/agentbank/human.token` (`0600`), guardado con
  `agentbank login`.
- Tokens de agentes: `~/.config/agentbank/tokens/<agente>.token` (`0600`).
- `agentbank mcp` y `agentbank worker` se auto-enrolan con el token humano si
  falta el token del agente.
- `agent_role` es la etiqueta funcional (`orquestador`, `auditor`, `backend`):
  se guarda en `participants.agent_role`, se snapshotea en
  `created_by_role`/`author_role` de cada entidad y se registra en
  `audit_events.actor_role`. El ticket además guarda `updated_by_role`.
  El historial de un objeto se consulta con `GET /v1/audit?object_id=<id>`.
- CLI: `agentbank <comando>`; `agentbank login`, `agentbank init`,
  `agentbank agent list|ensure|rotate|revoke|disable`.
- MCP: `agentbank mcp`, JSON-RPC por stdio, 75 tools sin perfiles.
- Config: `AGENTBANK_URL`, `AGENTBANK_HUMAN_TOKEN`, `AGENTBANK_AGENT`,
  `AGENTBANK_AGENT_ROLE`, `AGENTBANK_PROJECT`, `.agentbank.json` (sin secretos).
- `agentbank api --method … --path … --body …` es el escape hatch.
- Detalle: `docs/tokens.md`.
- Setup OpenCode: `scripts/setup-opencode-mcp.py` (login + ensure + MCP).
- Agentes/skills: `scripts/install-opencode-assets.py`; ver
  `docs/subagentes.md`.
- Compactación de DeepSeek V4.1 Flash a 400k: `compaction.buffer = 600000`
  en `~/.config/opencode/opencode.json`.

## 7. Próximos pasos sugeridos

1. Worker con ejecutor `opencode-server` y `--worktree`.
2. Telegram (etapa 9).
3. Contratos entre proyectos y comparador de breaking changes.
4. Bloqueos/escalado y choque de áreas.
5. Despliegue en el Moto (systemd, backup, benchmark).

## 8. Cómo cerrar una tarea

- Tests del paquete afectado.
- `test-core`/`vet-core`.
- `fmt/vet/test/build` completos.
- Actualizar `docs/api.md`, `docs/frontend.md`, OpenAPI y ADR si cambió el
  contrato o una decisión.
- Commit en rama; PR; CI x86/ARM64; merge solo con autorización.
