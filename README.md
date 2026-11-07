# agentbank-go

Reescritura del backend de AgentBank en **Go + SQLite**, sin frontend, pensada
para correr en el Moto G20 con bajo consumo.

- API HTTP `/v1` con documentación para construir el frontend después.
- Un solo binario, SQLite embebido, sin Docker/Postgres/Node.
- Auditoría append-only con hash chain.
- Objetivo: RSS en reposo ≤ 30 MB, binario ≤ 20 MB, p95 local ≤ 50 ms.

## Estado

Scaffold inicial. Ya funciona:

- `GET /healthz` (ping a SQLite).
- `GET /v1/meta`.
- Migraciones embebidas y esquema base de identidad/auditoría.

El resto del backend se implementa por fases; ver `docs/rewrite-plan.md` y
`AGENTS.md`.

## Requisitos

- Go 1.27.3 en `/opt/go` (ya instalado en el Moto).
- Sin CGO: SQLite puro Go (`modernc.org/sqlite`).

## Correr

```bash
export PATH=/opt/go/bin:$PATH
go run ./cmd/agentbank
```

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

## Build

```bash
go build -trimpath -ldflags="-s -w" -o bin/agentbank ./cmd/agentbank
```

## Documentación

- `AGENTS.md`: reglas del agente de reescritura.
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
