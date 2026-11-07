# Benchmarks — Moto G20

Fecha: 2026-11-07. Mediciones del scaffold y de la matriz local de `devctl`.
No es el MVP completo.

## Servidor

| Métrica | Valor |
|---|---:|
| Binario `agentbank` (stripped) | 11 MB |
| RSS en reposo (server + SQLite) | 5.6 MB |
| SQLite inicial | 4 KB + WAL 121 KB |
| `/healthz` + `/v1/meta` | 200 OK |

## Matriz local (`devctl`)

Proyecto `/root/agentbank-go`, `default_policy=auto`, una corrida a la vez,
`taskset 0-1`, SQLite temporal.

| Tarea | Nivel | Exec | RAM pico | CPU pico | Temp | Estado |
|---|---|---:|---:|---:|---:|---|
| `fmt` | L0 | 177 ms | 0.3 MB | — | 41 °C | ✅ |
| `vet` | L0 | 1.68 s | 45.1 MB | 189% | 48 °C | ✅ |
| `test` | L1 | 3.80 s | 89.8 MB | 190% | 49 °C | ✅ |
| `build` | L2 | 3.56 s | 78.8 MB | 197% | 47.5 °C | ✅ |
| `test-race` | L3 | — | — | — | — | ⛔ bloqueado: no hay gcc local; va a CI/PC |

La temperatura de partida fue 40.2 °C y terminó en 48.8 °C. No se acercó al
abort térmico de 58 °C.

## Fase 1 — identidad y auditoría (2026-11-07)

Corrida local con `devctl`, `default_policy=auto`, `taskset 0-1`, SQLite
temporal. State dir: `/tmp/opencode/devctl-agentbank-fase1`.

| Tarea | Run ID | Exec | RAM pico | CPU pico | Temp | Estado |
|---|---|---:|---:|---:|---:|---|
| `fmt` | `20261107T211506Z-f9f4aa` | 149 ms | 1.5 MB | — | 48.2 °C | ✅ |
| `vet` | `20261107T211508Z-e98b4e` | 3.34 s | 63.6 MB | 188.7% | 53.4 °C | ✅ |
| `test` | `20261107T211516Z-cddaca` | 7.55 s | 100.0 MB | 196.2% | 54.7 °C | ✅ |
| `build` | `20261107T211526Z-7ad79c` | 4.42 s | 84.2 MB | 194.2% | 54.0 °C | ✅ |

Mediciones del binario con Fase 1:

| Métrica | Valor |
|---|---:|
| Binario de `devctl build` (sin `-ldflags`) | 16,175,368 bytes (~15.4 MiB) |
| Binario stripped (`-s -w`) | 10,748,064 bytes (~10.25 MiB) |
| RSS en reposo tras bootstrap + `/v1/me` | 2.0 MB |
| SQLite inicial | 4 KB + WAL 272 KB |
| `test-race` local | no corre (sin gcc); lo cubre CI x86/ARM64 |

Los tests de Fase 1 incluyen SQLite temporal, `httptest`, bootstrap, rotación y
revocación de tokens, permisos, auditoría transaccional y detección de un evento
alterado. No se superó el abort térmico de 58 °C.

## CI L3 (GitHub Actions)

Run: https://github.com/joacomedel/agentbank-go/actions/runs/42413062755

| Job | Resultado | Tiempo |
|---|---|---:|
| test (ubuntu-24.04) | ✅ race | 1m30s |
| test (ubuntu-24.04-arm) | ✅ race | 1m02s |
| build (linux/amd64) | ✅ | 46s |
| build (linux/arm64) | ✅ | 32s |

Artefactos: `agentbank-linux-amd64` 11.1 MB y `agentbank-linux-arm64`
10.5 MB.

## Frío

| Medición | Valor |
|---|---:|
| Primer `go test ./...` (compilando `modernc.org/sqlite`) | 82 s |
| Temperatura durante el build frío | 46 °C |
| Compilación posterior (cacheada) | segundos |

## Cómo se midió

```bash
cd /root/pipelineCompilacion
./bin/devctl run fmt   --project /root/agentbank-go
./bin/devctl run vet   --project /root/agentbank-go
./bin/devctl run test  --project /root/agentbank-go
./bin/devctl run build --project /root/agentbank-go
./bin/devctl metrics --last 10 --state-dir /tmp/opencode/devctl-agentbank-policy
```

## Presupuesto

| Recurso | Límite | Estado |
|---|---|---|
| Binario | ≤ 20 MB | 11 MB ✅ |
| RSS reposo | ≤ 30 MB | 5.6 MB ✅ |
| RSS test | ≤ 128 MB | 89.8 MB ✅ |
| CPU local | ≤ 2 núcleos | 2 ✅ |
| Temp abort | 58 °C | máx. 49 °C ✅ |
| p95 endpoints | ≤ 50 ms | pendiente de endpoints reales |
