# Testing — agentbank-go

La regla general es
[`/root/pipelineCompilacion/docs/testing-policy.md`](/root/pipelineCompilacion/docs/testing-policy.md).
Este archivo solo agrega lo específico del proyecto.

## Matriz

| Nivel | Comando | Dónde |
|---|---|---|
| L0 | `devctl run fmt`, `devctl run vet` | Moto |
| L1 | `devctl run test` (`go test -count=1 ./...`) | Moto |
| L2 | tests de integración con SQLite temporal y `httptest` | Moto/PC |
| L3 | `go test -race ./...` + matriz x86/ARM64 | CI/PC |
| L4 | benchmarks de cola/búsqueda | PC/CI |

## Reglas del proyecto

- Cada test abre SQLite en `t.TempDir()` y corre todas las migraciones.
- Nunca usar `data/agentbank.db` ni una base compartida en tests.
- Cada endpoint nuevo necesita:
  - handler + servicio + SQL;
  - test de éxito, validación, error y permisos;
  - evento de auditoría verificado si escribe;
  - `docs/api.md` y `docs/frontend.md` actualizados.
- Concurrencia: claims y `ticket_siguiente` se prueban con goroutines y
  `-race` en CI.
- Auditoría: un test debe alterar un evento y verificar que la cadena falla.
- No Postgres, no Docker, no Telegram en tests del MVP.
- `test-race` no corre en el Moto (sin gcc); es L3 y va a CI/PC.
- El build debe seguir siendo ≤ 20 MB y el RSS local ≤ 64 MB bajo carga.

## Evidencia mínima para cerrar una tarea

```bash
cd /root/pipelineCompilacion
./bin/devctl run fmt   --project /root/agentbank-go
./bin/devctl run vet   --project /root/agentbank-go
./bin/devctl run test  --project /root/agentbank-go
./bin/devctl run build --project /root/agentbank-go
./bin/devctl metrics --last 5 --state-dir /tmp/opencode/devctl-agentbank
```

Reportar `run_id`, duración, RAM pico, CPU y temperatura. Si `test-race` no
corre local, indicar el run de CI que lo cubre.
