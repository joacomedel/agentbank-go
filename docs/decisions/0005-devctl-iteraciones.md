# ADR 0005 — Flujo de devctl para iteraciones largas

- Fecha: 2026-11-08
- Estado: aceptado
- Alcance: `/root/agentbank-go` y configuración local de `devctl`

## Contexto

La sesión de implementación de las etapas 1–7 agregó ~18.700 líneas y 52 tests.
`devctl` funcionó para la evidencia final, pero aparecieron fricciones:

- Se usaron varios `state-dir` distintos, así que las métricas no se podían
  comparar entre etapas.
- `devctl run build` compilaba en una copia y no actualizaba `bin/agentbank`;
  después hacíamos builds stripped manuales para medir tamaño y hacer smoke.
- El loop interno corría `go test ./...` y `go vet ./...` completos demasiadas
  veces.
- Una corrida llegó a 57.3 °C, cerca del abort anterior de 58 °C.
- El router bloqueó corridas locales por reserva de memoria cuando
  `MemAvailable` bajó de ~2.2 GB, aunque los tests usaban <200 MB.
- Quedaron servidores de smoke corriendo y un binario en uso impidió publicar
  el artefacto.

## Decisiones

### Estado único por proyecto

- `dev-environment.toml` define `[project].state_dir`.
- Para AgentBank Go: `/var/tmp/devctl-agentbank`.
- `devctl` usa ese directorio si no se pasa `--state-dir`.
- `devctl metrics` permite comparar todas las etapas en una sola tabla.

### Artefactos y publicación

- `[tasks.<nombre>].artifacts` copia archivos al directorio del run.
- `publish = true` copia el artefacto de vuelta al proyecto.
- `[tasks.build]` compila stripped (`-ldflags='-s -w'`) y publica
  `bin/agentbank`, así no se repite un build manual.
- `devctl metrics` muestra `TEMP`.

### Loop interno y cierre

- Tareas nuevas:
  - `test-core`: `go test` sobre service, httpapi, audit y db.
  - `vet-core`: `go vet` sobre service y httpapi.
- El flujo recomendado es `test-core`/`vet-core` durante la edición y
  `fmt/vet/test/build` completos al cerrar la etapa.

### Temperatura y memoria

- Probe local: `max_temp_c = 54`.
- Abort térmico: `abort_temp_c = 56` (antes 58).
- `memory_max` del proyecto: `512M` (antes `1G`). Los picos medidos fueron
  ~280 MB en `vet` y ~171 MB en `test`; 512 MB deja margen y evita que el
  router bloquee por la reserva de 1200 MB.

### Higiene de procesos

- Antes de un build que publica binario, no debe haber un `agentbank` corriendo
  desde `bin/agentbank`.
- Los smoke tests deben usar binarios en `/tmp/opencode` y cerrarse al terminar.

## Consecuencias

- Menos builds duplicados y métricas comparables.
- El loop interno es más barato; el cierre de etapa sigue siendo completo.
- La política térmica es más conservadora.
- El router ya no bloquea AgentBank Go con la memoria disponible típica del
  Moto.
- Si un build falla al publicar por “Text file busy”, hay que detener el server
  local antes de reintentar.
