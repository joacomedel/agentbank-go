# ADR 0002 — Identidad, tokens y auditoría de Fase 1

- Fecha: 2026-11-07
- Estado: aceptado para el MVP
- Alcance: `/root/agentbank-go`, Fase 1

## Contexto

La Fase 1 necesita bootstrap del humano, tokens hasheados con rotación y
revocación, membresías por proyecto y una auditoría append-only verificable que
corra en SQLite sobre el Moto G20 sin servicios externos.

El repo viejo usa un token de humano en variable de entorno y un modelo de
agentes separado. La reescritura unifica humanos y agentes en `participants` y
mantiene una sola capa de permisos (`participants.admin` o `memberships`).

## Decisiones

### Bootstrap

- Si no existe ningún participante `humano`, el server exige
  `AGENTBANK_BOOTSTRAP_TOKEN` y crea el humano `admin` de instancia.
- La variable debe tener al menos 32 caracteres. Si falta o es corta, el
  proceso falla con un mensaje claro; no se genera un token predecible.
- Si ya existe un humano, la variable se ignora. El bootstrap es idempotente.
- El token de bootstrap se guarda solo como `sha256(token)` y no se devuelve
  por API ni se loguea.
- Los tokens generados por la app usan `ab_<32 bytes base64url>`. Los tokens
  externos (bootstrap) se aceptan por hash aunque no tengan ese formato.

### Tokens

- Un participante puede tener como máximo un token activo. La base lo impone
  con un índice único parcial (`WHERE revoked_at IS NULL`).
- Emitir un token nuevo dentro de una rotación revoca el anterior en la misma
  transacción.
- La comparación de hashes usa `crypto/subtle.ConstantTimeCompare`.
- El texto plano solo se devuelve en la respuesta de creación o rotación.
- Un token revocado o desconocido devuelve `401` y se audita el rechazo sin
  guardar el token.

### Sesiones

- `X-Session-Id` es opcional. Si falta, el server crea una sesión y la devuelve
  en el header `X-Session-Id`, auditada como `sesion.abrir`.
- Si el header no corresponde al participante, se responde `401` y se audita
  `rechazo.auth.sesion_invalida`.
- No hay expiración de sesión en Fase 1. El frontend debe reutilizar el header
  para no crear una sesión por request.

### Autorización

- `participants.admin=true` es admin de instancia: puede todo.
- `memberships.permission` es `lector`, `colaborador` o `admin`.
- `lector` lee proyecto, miembros y cadena; `colaborador` sumará escritura de
  trabajo en fases posteriores; `admin` administra membresías.
- Los roles son etiquetas por proyecto y no cambian permisos en Fase 1.
- Crear proyectos y participantes queda reservado al admin de instancia.
- Los rechazos de permisos se auditan en el handler HTTP, que es el único
  punto con IP/origen y respuesta HTTP. El servicio devuelve un error tipado
  `ForbiddenError` para que el handler lo audite antes de responder.

### Auditoría

- Hay una cadena `system` y una cadena por proyecto (`chain_id` = UUID).
- `seq` es correlativo por cadena y el hash es
  `SHA-256(prev_hash ‖ campos_canónicos)`.
- La serialización canónica usa longitud en base 36 por campo y un separador
  fijo, para evitar ambigüedades de concatenación.
- Las fechas se guardan con 9 dígitos de fracción en UTC, de ancho fijo, para
  que el orden lexicográfico coincida con el cronológico.
- El `payload` se serializa con `encoding/json` (claves de mapas ordenadas) y
  se hashea como string canónico.
- Cada cambio de negocio y sus eventos van en la misma transacción. Las
  escrituras usan `_txlock=immediate` y una sola conexión.
- La verificación recorre la cadena, recalcula hashes, valida `prev_hash`,
  `seq` y que la cabeza (`audit_chain_heads`) apunte al último evento.
- La app solo hace `INSERT` sobre `audit_events`; no hay `UPDATE` ni `DELETE`.
  No se agregaron triggers de SQLite en Fase 1: el test de alteración necesita
  simular una modificación directa del archivo y la verificación debe detectarla.
  Un atacante con acceso al archivo puede alterar filas, pero no puede
  recalcular la cadena sin acceso a la lógica y a todas las cabezas.

### Contrato HTTP

- El contrato nuevo usa campos JSON en inglés `snake_case` (`name`, `type`,
  `permission`, `project_id`) y conserva nombres de acciones de auditoría con
  el espíritu del repo viejo.
- Listados con `limit` (default 50, máximo 200) y `cursor`.
- Errores con `error` y `code` estable.
- Validación estricta de JSON con `DisallowUnknownFields`.
- El autor sale del token; ningún campo del body puede declararlo.

## Consecuencias

- El humano puede trabajar desde el primer arranque sin un endpoint público de
  registro.
- Rotar un token es atómico y no deja ventanas con dos tokens activos.
- La cadena es barata de verificar y no depende de Postgres ni de triggers.
- Se pierde paridad literal con la API vieja en nombres de campos; se documenta
  la diferencia en `docs/api.md`.
- Si en el futuro se agrega un trigger append-only, el test de alteración deberá
  desactivarlo explícitamente o usar una copia del archivo.

## Dudas abiertas

- ¿El operador prefiere que `AGENTBANK_BOOTSTRAP_TOKEN` exija el prefijo `ab_`
  además del mínimo de 32 caracteres?
- ¿La sesión debe expirar por inactividad o alcanza con rotación/revocación de
  token en el MVP?
- ¿Los roles deben tener un catálogo por proyecto en Fase 3 o seguir siendo
  etiquetas libres?
- ¿Un admin de proyecto debería poder crear participantes en su proyecto sin
  ser admin de instancia? En Fase 1 se reserva al admin de instancia.
