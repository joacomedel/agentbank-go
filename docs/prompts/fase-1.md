# Prompt — Fase 1: identidad y auditoría

Copiá el bloque de abajo en el agente. El **working directory** debe ser
`/root/agentbank-go`.

---

Sos el agente de implementación de **AgentBank Go**. Trabajás en
`/root/agentbank-go`.

## Misión de esta sesión

Implementar **solo la Fase 1: identidad, proyectos, participantes, tokens,
sesiones, membresías y auditoría**. No empezar tickets, claims, mensajes,
documentos, fases, épicas ni frontend. Esas son fases posteriores.

## Leer antes de tocar código

En este orden:

1. `/root/agentbank-go/AGENTS.md`
2. `/root/agentbank-go/TESTING.md`
3. `/root/pipelineCompilacion/docs/testing-policy.md`
4. `/root/agentbank-go/docs/rewrite-plan.md`
5. `/root/agentbank-go/docs/scope.md`
6. `/root/agentbank-go/docs/api.md`
7. `/root/agentbank-go/docs/frontend.md`
8. `/root/agentbank-go/REFERENCE.md`

Referencia de solo lectura: `/root/reference/BancoDeDatosAgente` (commit
`95f9a00`). **No lo modifiques.** Si necesitás semántica, leé
`packages/server/src/auth/*`, `audit/*`, `services/permisos.ts`,
`services/identity.ts`, `routes/identity.ts`, `routes/audit.ts` y las
migraciones `0001`, `0007`, `0008`, `0013`.

## Entregables de Fase 1

### Base de datos

- Migración nueva solo si hace falta; la `0001` ya tiene `projects`,
  `participants`, `memberships`, `tokens`, `sessions`, `audit_events` y
  `audit_chain_heads`.
- Agregar índices/constraints que falten.
- No romper migraciones existentes; son forward-only.
- La app debe poder arrancar desde una base vacía y desde una base ya migrada.

### Bootstrap y auth

- Si no existe un participante humano, crear uno con el token de bootstrap
  `AGENTBANK_BOOTSTRAP_TOKEN` (variable de entorno) y guardar solo su hash.
- Si la variable no está definida en el primer arranque, fallar con un mensaje
  claro; no crear un token predecible.
- Token de agente: `ab_<32 bytes base64url>`, guardar `sha256(token)`, mostrar
  el texto plano **una sola vez**.
- Un solo token activo por participante; emitir uno nuevo revoca el anterior.
- `Authorization: Bearer <token>`; `X-Session-Id` opcional; si falta, crear
  sesión.
- Comparación de hashes en tiempo constante.
- 401 token inválido/revocado; 403 sin permiso; auditar el rechazo.

### Endpoints

Implementar y documentar:

- `GET /v1/me` — participante, membresías y roles.
- `POST /v1/projects`, `GET /v1/projects`, `GET /v1/projects/{id}`.
- `POST /v1/participants`, `GET /v1/participants`.
- `POST /v1/participants/{id}/tokens` — rotación; devuelve token una vez.
- `POST /v1/tokens/{id}/revoke`.
- `GET /v1/projects/{id}/members`, `PUT/DELETE /v1/projects/{id}/members/{participant_id}`.
- `GET /v1/audit/chains`, `GET /v1/audit`, `GET /v1/audit/verify`.

Mantener `snake_case`, UUIDs, RFC3339 UTC y el formato de error
`{"error":"...","code":"..."}`.

### Auditoría

- Cada escritura de negocio genera un evento en la **misma transacción**.
- Cadena `system` y una por proyecto.
- Hash = SHA-256 de una representación canónica estable del evento +
  `prev_hash`; `seq` por cadena.
- Usar `BEGIN IMMEDIATE` para serializar la escritura de la cadena.
- `audit_events` append-only: sin `UPDATE`/`DELETE` desde la app.
- `GET /v1/audit/verify` debe detectar un evento alterado y devolver el primer
  `seq` inconsistente.
- Auditar al menos: login/bootstrap, creación de proyecto/participante, emisión
  y revocación de token, cambio de membresía y rechazo de permisos.

### Tests

- SQLite temporal por test (`t.TempDir()`), migraciones desde cero.
- Tests de:
  - bootstrap y token hasheado;
  - token inválido/revocado;
  - rotación de token;
  - permisos por membresía y admin;
  - autor tomado del token (no del body);
  - auditoría en la misma transacción;
  - verificación de cadena correcta;
  - alteración de un evento detectada;
  - migraciones idempotentes;
  - endpoints con `httptest`.
- Nada de Postgres, Docker, red externa ni `-race` local.
- `-race` se cubre en CI.

### Documentación

- Actualizar `docs/api.md` con cada endpoint implementado.
- Actualizar `docs/frontend.md` si cambia el contrato de auth/identidad.
- Actualizar el estado en `README.md` (Fase 1, no más).
- Si tomás una decisión no trivial, crear `docs/decisions/NNNN-*.md`.

## Restricciones

- No implementar tickets, claims, mensajes, docs, fases, épicas ni frontend.
- No tocar `/root/reference/BancoDeDatosAgente`.
- No agregar dependencias fuera de `modernc.org/sqlite` y stdlib.
- No agregar Postgres, Docker, Redis, Node, pnpm ni Telegram.
- No cambiar los límites de `dev-environment.toml` sin aprobación.
- No usar `0.0.0.0`; el server sigue en `127.0.0.1:8090`.
- No loguear tokens, hashes ni cuerpos sensibles.
- No hacer `git push`, `gh repo create`, merge ni deploy sin autorización.
- No desactivar ni skipear tests para “terminar”.

## Flujo de trabajo

1. Crear rama `feat/fase-1-identidad-auditoria`.
2. Implementar en capas: `internal/domain`, `internal/auth`, `internal/audit`,
   `internal/service`, `internal/httpapi`, `internal/db`.
3. Ejecutar desde `/root/pipelineCompilacion`:

```bash
./bin/devctl run fmt   --project /root/agentbank-go
./bin/devctl run vet   --project /root/agentbank-go
./bin/devctl run test  --project /root/agentbank-go
./bin/devctl run build --project /root/agentbank-go
```

4. Reportar `run_id`, duración, RAM pico, CPU y temperatura de cada corrida.
5. `test-race` no corre local (sin gcc); dejar constancia de que lo cubre CI.
6. Commit(s) en la rama local. No push ni merge sin autorización.
7. Al terminar, mostrar:
   - resumen de cambios;
   - endpoints implementados;
   - tests agregados y resultados;
   - métricas de `devctl`;
   - decisiones/ADRs;
   - dudas abiertas;
   - `git diff --stat` y `git log --oneline`.

## Definition of Done de Fase 1

- Endpoints de identidad/auditoría implementados.
- Bootstrap y rotación/revocación de tokens funcionando.
- Auditoría transaccional y verificable.
- Tests verdes y `gofmt`/`go vet` limpios.
- `docs/api.md` y `docs/frontend.md` actualizados.
- Sin secretos ni dependencias nuevas.
- Sin tocar fases posteriores ni el repo viejo.
- Reporte con evidencia y métricas.
