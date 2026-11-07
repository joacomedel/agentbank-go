# agentbank-go

[![CI](https://github.com/joacomedel/agentbank-go/actions/workflows/go.yml/badge.svg)](https://github.com/joacomedel/agentbank-go/actions)

Reescritura del backend de AgentBank en **Go + SQLite**, sin frontend, pensada
para correr en el Moto G20 con bajo consumo.

> Este repo es el **piloto del ecosistema de testing** (`devctl`). La política
> general está en `/root/pipelineCompilacion/docs/testing-policy.md` y la matriz
> del proyecto en `TESTING.md`.

- API HTTP `/v1` con documentación para construir el frontend después.
- Un solo binario, SQLite embebido, sin Docker/Postgres/Node.
- Auditoría append-only con hash chain.
- Objetivo: RSS en reposo ≤ 30 MB, binario ≤ 20 MB, p95 local ≤ 50 ms.

## Estado

Fase 1 (identidad y auditoría) implementada:

- `GET /healthz` (ping a SQLite) y `GET /v1/meta`.
- Bootstrap del humano con `AGENTBANK_BOOTSTRAP_TOKEN`.
- Proyectos, participantes humano/agente, membresías y catálogo de roles por
  proyecto (auto-registrado al asignar).
- Edición de metadatos de proyecto por cualquier participante autenticado, con
  auditoría y control opcional de versión.
- Tokens hasheados sin expiración, con rotación y revocación; un token activo
  por participante.
- Sesiones auditadas vía `X-Session-Id`.
- Auditoría append-only con hash chain por proyecto y `system`, y verificación
  de integridad.
- Migraciones embebidas e idempotentes.

Tickets, claims, fases, épicas, mensajes y documentos se implementan en fases
posteriores; ver `docs/rewrite-plan.md` y `AGENTS.md`.

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

## Documentación

- `AGENTS.md`: reglas del agente de reescritura.
- `TESTING.md`: matriz de testing del proyecto.
- `docs/rewrite-plan.md`: fases, alcance y criterios.
- `docs/scope.md`: MVP vs deferred.
- `docs/api.md`: contrato HTTP.
- `docs/frontend.md`: guía para el frontend futuro.
- `docs/mapping.md`: mapa desde el repo TypeScript viejo.
- `docs/benchmarks.md`: mediciones en el Moto.
- `docs/deploy.md` y `deploy/`: systemd, límites y backup.
- `docs/decisions/`: ADRs.

## Referencia

Repo viejo de solo lectura: `/root/reference/BancoDeDatosAgente` en `95f9a00`.
No se copia su frontend ni se modifica ese repositorio.
