# agentbank-go

[![CI](https://github.com/joacomedel/agentbank-go/actions/workflows/go.yml/badge.svg)](https://github.com/joacomedel/agentbank-go/actions)

Reescritura del backend de AgentBank en **Go + SQLite**, sin frontend, pensada
para correr en el Moto G20 con bajo consumo.

> Este repo es el **piloto del ecosistema de testing** (`devctl`). La política
> general está en `/root/pipelineCompilacion/docs/testing-policy.md` y la matriz
> del proyecto en `TESTING.md`.

- API HTTP `/v1` con OpenAPI/Swagger para construir la UI aparte.
- Un solo binario, SQLite embebido, sin Docker/Postgres/Node.
- Auditoría append-only con hash chain.
- Objetivo: RSS en reposo ≤ 30 MB, binario ≤ 20 MB, p95 local ≤ 50 ms.

## Estado

Etapas 1–6 implementadas (backend HTTP):

- **Identidad**: bootstrap del humano, proyectos, participantes humano/agente,
  membresías, roles por proyecto, tokens hasheados sin expiración, rotación,
  revocación y baja, sesiones, tokens por agente con auto-enrolamiento y
  `agent_role` funcional.
- **Tickets**: CRUD, estados, prioridad, versión, dependencias con detección de
  ciclos, claims con lease, `ticket_siguiente`, comentarios, timeline, IDs
  `PREFIX-N`, `ticket_avanzar` con nota y pregunta.
- **Organización**: fases, épicas, orden, cierre con pendientes, briefing,
  digest y tablero.
- **Mensajes**: `info`/`solicitud`, bandeja unificada, destinos, opciones con
  default, `afecta`, resolución, defaults asumidos, conversión a decisión o
  ticket, corrección y handoff.
- **Conocimiento**: documentos versionados con hash y diff, docs globales, sync
  por ruta, decisiones/ADRs, adjuntos a tickets y búsqueda `LIKE`.
- **Operación**: import JSON con dry-run y ciclos, barridos, heartbeat, pausa,
  outbox/webhooks, long-poll de novedades, feed incremental `/v1/changes`,
  `updated_since` en tickets/docs, idempotencia, rate limit, escaneo de secretos,
  métricas y verificación global de auditoría.
- **Auditoría**: append-only con hash chain por proyecto y `system`, sesión,
  origen, correlación y verificación de integridad.

Etapa 7 implementada: CLI Go, MCP por stdio sin perfiles, `init`, config
`.agentbank.json` y worker básico. Faltan la UI de producto (fuera de este repo),
Telegram, contratos y choque de áreas. Ver `docs/rewrite-plan.md`,
`docs/scope.md`, `docs/cli.md`, `docs/mcp.md` y `docs/api.md`.

## Requisitos

- Go 1.27.3 en `/opt/go` (ya instalado en el Moto).
- Sin CGO: SQLite puro Go (`modernc.org/sqlite`).

## Correr

```bash
export PATH=/opt/go/bin:$PATH
export AGENTBANK_BOOTSTRAP_TOKEN="$(openssl rand -base64 32 | tr '+/' '-_' | tr -d '=')"
go run ./cmd/agentbank
```

`AGENTBANK_BOOTSTRAP_TOKEN` solo es obligatorio en el primer arranque, cuando
todavía no existe un participante humano. Después se ignora; guardá el token
porque es la credencial inicial del humano.

En otra terminal:

```bash
curl -s localhost:8090/healthz
curl -s localhost:8090/v1/meta
```

Configuración por entorno:

| Variable | Default | Uso |
|---|---|---|
| `AGENTBANK_ADDR` | `127.0.0.1:8090` | dirección HTTP |
| `AGENTBANK_DB` | `./data/agentbank.db` | archivo SQLite |
| `AGENTBANK_LOG_LEVEL` | `info` | `debug|info|warn|error` |
| `AGENTBANK_MAX_BODY_BYTES` | `1048576` | límite de body |
| `AGENTBANK_CORS_ORIGINS` | vacío | orígenes permitidos, separados por coma |
| `AGENTBANK_BOOTSTRAP_TOKEN` | vacío | token inicial del humano; requerido solo en el primer arranque |

## Build

```bash
go build -trimpath -ldflags="-s -w" -o bin/agentbank ./cmd/agentbank
```

## CLI y MCP

```bash
agentbank init --url http://127.0.0.1:8090 --project API --token 'ab_...'
agentbank me --project API
agentbank next --project API --role backend
agentbank advance --id API-42 --status hecho --nota-cierre "Listo" --y-siguiente
agentbank mcp
```

- CLI completa en `docs/cli.md`.
- MCP por stdio, 75 tools, sin perfiles, en `docs/mcp.md`.
- Ambos consumen la misma API/OpenAPI.

## OpenAPI / Swagger

- Spec: `GET /openapi.json` o `GET /v1/openapi.json`.
- Swagger UI de desarrollo: `GET /swagger` (carga el spec; requiere internet
  para el CDN, o usar cualquier Swagger UI local apuntando a `/openapi.json`).
- La UI de producto no se implementa en este repo.

## Documentación

- `AGENTS.md`: reglas del agente de reescritura.
- `TESTING.md`: matriz de testing del proyecto.
- `docs/rewrite-plan.md`: fases, alcance y criterios.
- `docs/scope.md`: MVP vs deferred.
- `docs/api.md`: contrato HTTP.
- `docs/frontend.md`: guía para el frontend futuro.
- `docs/mapping.md`: mapa desde el repo TypeScript viejo.
- `docs/mcp-flows.md`: flujos MCP/CLI y conteo de llamadas.
- `docs/handoff.md`: estado y playbook para una sesión nueva.
- `docs/cli.md`: comandos de la CLI.
- `docs/mcp.md`: servidor MCP, tools y configuración.
- `docs/tokens.md`: gestión de token humano y tokens por agente.
- `docs/subagentes.md`: orquestador/implementador/auditor, carriles por épica
  y auditoría de cierres.
- `scripts/setup-opencode-mcp.py`: login + enrolamiento + config MCP de OpenCode.
- `scripts/smoke_agent_tokens.py`: smoke end-to-end de tokens y `agent_role`.
- `scripts/install-opencode-assets.py`: instala agentes y skills en OpenCode.
- `docs/benchmarks.md`: mediciones en el Moto.
- `docs/deploy.md` y `deploy/`: systemd, límites y backup.
- `docs/decisions/`: ADRs.

## Referencia

Repo viejo de solo lectura: `/root/reference/BancoDeDatosAgente` en `95f9a00`.
No se copia su frontend ni se modifica ese repositorio.
