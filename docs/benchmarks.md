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
