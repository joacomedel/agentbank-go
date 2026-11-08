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
abort térmico de 56 °C (la política anterior era 58; se bajó después de esta
sesión).

## Ajustes de devctl post-sesión (2026-11-08)

Después de la revisión se aplicaron mejoras al flujo:

- Un solo `state_dir` por proyecto: `/var/tmp/devctl-agentbank`.
- `devctl metrics` muestra temperatura.
- `devctl run build` compila stripped, publica `bin/agentbank` en el repo y lo
  registra como artefacto.
- Tareas rápidas `test-core` y `vet-core` para el loop interno.
- Abort térmico bajado de 58 °C a 56 °C; probe local a 54 °C.
- `memory_max` del proyecto bajado de 1G a 512M para que el router no bloquee
  por reserva de memoria.

Validación con el flujo nuevo, sin `--state-dir` manual:

| Tarea | Run ID | Exec | RAM pico | CPU pico | Temp |
|---|---|---:|---:|---:|---:|
| `test-core` | `20261108T034758Z-a8742f` | 17.24 s | 158.3 MB | 203.7% | 48.0 °C |
| `vet-core` | `20261108T034816Z-ca422f` | 10.43 s | 148.0 MB | 197.2% | 47.5 °C |
| `fmt` | `20261108T034833Z-e3f9b1` | 375 ms | 7.7 MB | 157.9% | 42.8 °C |
| `vet` | `20261108T034834Z-5f7bb0` | 15.38 s | 252.1 MB | 204.7% | 47.7 °C |
| `test` | `20261108T034850Z-40d627` | 25.00 s | 171.2 MB | 204.6% | 48.0 °C |
| `build` | `20261108T034916Z-8ab123` | 1.78 s | 62.8 MB | 165.5% | 46.4 °C |

El build publicó `bin/agentbank` (12,583,072 bytes stripped) directamente en el
repo.

## CLI y MCP (2026-11-08)

| Tarea | Run ID | Exec | RAM pico | CPU pico | Temp |
|---|---|---:|---:|---:|---:|
| `fmt` | `20261108T014644Z-7810b3` | 373 ms | 6.7 MB | 133.9% | 43.8 °C |
| `vet` | `20261108T014649Z-12183b` | 15.38 s | 271.8 MB | 203.8% | 49.3 °C |
| `test` | `20261108T014710Z-1ca69e` | 25.04 s | 168.7 MB | 227.5% | 50.0 °C |
| `build` | `20261108T014739Z-592d39` | 4.19 s | 89.4 MB | 194.3% | 46.2 °C |

Binario devctl: 18,586,890 bytes (~17.7 MiB). Stripped: 12,583,072 bytes
(~12.0 MiB). La CLI y el MCP comparten el mismo binario que el server; no
agregan dependencias.

## Feed incremental y `updated_since` (2026-11-08)

| Tarea | Run ID | Exec | RAM pico | CPU pico | Temp |
|---|---|---:|---:|---:|---:|
| `fmt` | `20261108T010251Z-1b9393` | 396 ms | 6.7 MB | 146.2% | 41.0 °C |
| `vet` | `20261108T010257Z-cb05a4` | 14.18 s | 280.3 MB | 204.9% | 49.3 °C |
| `test` | `20261108T010316Z-bcd249` | 22.67 s | 158.2 MB | 216.6% | 50.6 °C |
| `build` | `20261108T010343Z-d5e04e` | 11.55 s | 149.3 MB | 226.5% | 50.0 °C |

Binario devctl: 18,157,955 bytes (~17.3 MiB). Stripped: 12,255,392 bytes
(~11.7 MiB). Se agregaron `/v1/changes` y `updated_since` en tickets/docs para
evitar listados completos.

## OpenAPI / Swagger (2026-11-08)

| Tarea | Run ID | Exec | RAM pico | CPU pico | Temp |
|---|---|---:|---:|---:|---:|
| `fmt` | `20261108T002620Z-624943` | 387 ms | 6.6 MB | 146.1% | 45.1 °C |
| `vet` | `20261108T002626Z-b5f949` | 13.86 s | 267.6 MB | 204.8% | 51.4 °C |
| `test` | `20261108T002645Z-db7ff2` | 21.98 s | 163.7 MB | 251.9% | 51.1 °C |
| `build` | `20261108T002711Z-e688f7` | 7.41 s | 147.9 MB | 196.3% | 49.3 °C |

Binario devctl: 18,150,425 bytes (~17.3 MiB). Stripped: 12,255,392 bytes
(~11.7 MiB). El spec OpenAPI se genera en memoria y se expone en
`/openapi.json`; no agrega dependencias.

## Etapas 2–6 — tickets, mensajes, docs y operación (2026-11-08)

Corrida local con `devctl`, state dir
`/tmp/opencode/devctl-agentbank-fases26`.

| Tarea | Run ID | Exec | RAM pico | CPU pico | Temp | Estado |
|---|---|---:|---:|---:|---:|---|
| `fmt` | `20261108T001420Z-f1fe2c` | 399 ms | 8.2 MB | 146.7% | 44.6 °C | ✅ |
| `vet` | `20261108T001425Z-290655` | 8.65 s | 127.1 MB | 231.8% | 52.1 °C | ✅ |
| `test` | `20261108T001438Z-5ee61a` | 15.80 s | 159.5 MB | 204.7% | 51.9 °C | ✅ |
| `build` | `20261108T001500Z-372f15` | 10.05 s | 124.2 MB | 204.0% | 50.8 °C | ✅ |

Mediciones del binario:

| Métrica | Valor |
|---|---:|
| Binario de `devctl build` (sin `-ldflags`) | 17,880,750 bytes (~17.1 MiB) |
| Binario stripped (`-s -w`) | 11,993,248 bytes (~11.4 MiB) |
| RSS en reposo tras bootstrap + `/v1/me` | 2.0 MB |
| SQLite inicial | 4 KB + WAL 696 KB |
| Tests | 44 |

El binario sigue por debajo de 20 MB. El test subió a 15.8 s y 159.5 MB por la
cantidad de integración nueva; sigue dentro del presupuesto local de 1 GB y de
2 núcleos. `test-race` se cubre en CI.

## Fase 1 — identidad y auditoría (2026-11-07)

Corrida local con `devctl`, `default_policy=auto`, `taskset 0-1`, SQLite
temporal. State dir: `/tmp/opencode/devctl-agentbank-fase1`.

| Tarea | Run ID | Exec | RAM pico | CPU pico | Temp | Estado |
|---|---|---:|---:|---:|---:|---|
| `fmt` | `20261107T214022Z-91faf6` | 232 ms | 0.2 MB | — | 49.8 °C | ✅ |
| `vet` | `20261107T214028Z-15a295` | 3.45 s | 68.0 MB | 197.0% | 54.0 °C | ✅ |
| `test` | `20261107T214037Z-4a8431` | 7.81 s | 97.6 MB | 228.5% | 57.3 °C | ✅ |
| `build` | `20261107T214053Z-9152c5` | 4.46 s | 89.3 MB | 197.3% | 53.7 °C | ✅ |

Mediciones del binario con Fase 1:

| Métrica | Valor |
|---|---:|
| Binario de `devctl build` (sin `-ldflags`) | 16,248,270 bytes (~15.5 MiB) |
| Binario stripped (`-s -w`) | 10,813,600 bytes (~10.31 MiB) |
| RSS en reposo tras bootstrap + `/v1/me` | 2.0 MB |
| SQLite inicial | 4 KB + WAL 296 KB |
| `test-race` local | no corre (sin gcc); lo cubre CI x86/ARM64 |

El pico de temperatura de `test` fue 57.3 °C. Con la política nueva (abort a
56 °C) esa corrida se habría cortado; conviene enfriar antes de `test`/`vet`.

Los tests de Fase 1 incluyen SQLite temporal, `httptest`, bootstrap, rotación y
revocación de tokens, permisos, auditoría transaccional y detección de un evento
alterado. No se superó el abort térmico de 56 °C (la política anterior era 58; se bajó después de esta
sesión).

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
# loop interno
./bin/devctl run test-core --project /root/agentbank-go
./bin/devctl run vet-core  --project /root/agentbank-go
# cierre
./bin/devctl run fmt   --project /root/agentbank-go
./bin/devctl run vet   --project /root/agentbank-go
./bin/devctl run test  --project /root/agentbank-go
./bin/devctl run build --project /root/agentbank-go
./bin/devctl metrics --last 10 --project /root/agentbank-go
```

## Presupuesto

| Recurso | Límite | Estado |
|---|---|---|
| Binario | ≤ 20 MB | 17.7 MB devctl / 12.0 MB stripped ✅ |
| RSS reposo | ≤ 30 MB | ~2.0 MB ✅ |
| RSS test | ≤ 256 MB | 171 MB ✅ |
| CPU local | ≤ 2 núcleos | 2 ✅ |
| Temp abort | 56 °C | máx. 48 °C en la última corrida ✅ |
| p95 endpoints | ≤ 50 ms | pendiente de medición formal |
