# Handoff — estado y playbook para el próximo agente

Este archivo existe para que un agente nuevo **no dependa de que el humano le
explique la sesión anterior**. Si vas a tocar AgentBank Go, leé esto después de
`AGENTS.md` y `TESTING.md`.

## 1. Estado actual

- Backend Go + SQLite, sin UI de producto.
- Etapas **0–7 implementadas**: identidad/auditoría, tickets/cola, fases/épicas,
  mensajes, documentos, operación/hardening, CLI/MCP/worker básico.
- **52 tests** verdes.
- `main` incluye OpenAPI/Swagger, feed incremental `/v1/changes`, CLI y MCP de
  73 tools sin perfiles.
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
8. `docs/decisions/0004-fases-2-6.md` y `0005-devctl-iteraciones.md`

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

## 6. CLI y MCP

- CLI: `agentbank <comando>`; `agentbank help`.
- MCP: `agentbank mcp`, JSON-RPC por stdio, 73 tools sin perfiles.
- Config: `AGENTBANK_URL`, `AGENTBANK_TOKEN`, `AGENTBANK_PROJECT`,
  `.agentbank.json` (sin secretos), token local en
  `~/.config/agentbank/token`.
- `agentbank api --method … --path … --body …` es el escape hatch.

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
