# Benchmarks — Moto G20

Fecha: 2026-11-07. Mediciones del scaffold, no del MVP completo.

| Métrica | Valor |
|---|---|
| Binario `agentbank` (stripped) | 11 MB |
| RSS en reposo (server + SQLite) | 5.6 MB |
| SQLite inicial | 4 KB + WAL 121 KB |
| `go test ./...` en frío (incluye compilar modernc) | 82 s |
| `go test ./...` en caliente, vía `devctl` | 3.94 s |
| RAM pico del test vía `devctl` | 78.5 MB |
| CPU del test | 2 núcleos A55 |
| Temperatura durante el test | 52.1 °C |
| Health/meta | 200 OK |

## Cómo se midió

```bash
cd /root/agentbank-go
go build -trimpath -ldflags="-s -w" -o bin/agentbank ./cmd/agentbank
ls -lh bin/agentbank

AGENTBANK_DB=/tmp/agentbank.db AGENTBANK_ADDR=127.0.0.1:8090 ./bin/agentbank &
ps -o pid,rss,vsz,etime,cmd -p $!
curl -s localhost:8090/healthz
curl -s localhost:8090/v1/meta
```

Vía `devctl`:

```bash
cd /root/pipelineCompilacion
./bin/devctl run test --project /root/agentbank-go
```

## Presupuesto

| Recurso | Límite | Estado |
|---|---|---|
| Binario | ≤ 20 MB | 11 MB ✅ |
| RSS reposo | ≤ 30 MB | 5.6 MB ✅ |
| RSS test | ≤ 128 MB | 78.5 MB ✅ |
| CPU local | ≤ 2 núcleos | 2 ✅ |
| p95 endpoints | ≤ 50 ms | pendiente de endpoints reales |

El primer build en frío (compilar `modernc.org/sqlite`) tardó 82 s con 2
núcleos y llegó a 46 °C. Después queda cacheado.
