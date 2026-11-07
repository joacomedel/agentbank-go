# ADR 0003 — Roles auto-registrados y edición abierta de proyectos

- Fecha: 2026-11-07
- Estado: aceptado para el MVP
- Alcance: `/root/agentbank-go`, Fase 1

## Contexto

Después de la primera entrega de Fase 1 quedaron cuatro dudas abiertas sobre
tokens, sesiones, catálogo de roles y permisos de edición de proyectos. La
decisión del operador fue:

- tokens lo más simples posible y sin expiración;
- catálogo de roles por proyecto, sin fricción para crear roles;
- más libertad para editar proyectos: alcanza con estar registrado/autenticado.

## Decisiones

### Tokens y sesiones

- Los tokens no expiran. Solo se invalidan por rotación, revocación o baja del
  participante.
- No hay refresh tokens, scopes ni expiración de sesión en el MVP.
- El token de bootstrap sigue aceptando cualquier valor de al menos 32
  caracteres; no se exige el prefijo `ab_` para credenciales externas.
- Los tokens generados por la app conservan `ab_<32 bytes base64url>` para
  mantener la regla del proyecto y facilitar el reconocimiento en logs/cliente.

### Catálogo de roles por proyecto

- Cada proyecto tiene una tabla `project_roles(project_id, name, created_at)`.
- Los roles se **auto-registran** al asignarse en una membresía. No hace falta
  crearlos antes ni hay un endpoint de alta separado.
- `GET /v1/projects/{id}/roles` lista el catálogo con la cantidad de membresías
  que usan cada rol.
- Al crear un proyecto se registra el rol `dueño` del creador.
- La migración `0003` rellena el catálogo desde las membresías existentes usando
  `json_each` sobre el JSON de roles.
- Los roles siguen siendo etiquetas: no cambian permisos en Fase 1.

### Edición de proyectos

- `PATCH /v1/projects/{id}` puede ser usado por **cualquier participante
  autenticado**, aunque no tenga membresía en el proyecto.
- Se pueden editar `name`, `slug` y `prefix`.
- Cada edición se audita en la cadena del proyecto como `proyecto.editar`, con
  `previous` y `new`.
- La versión se incrementa en cada edición. `version` es opcional en el body;
  si se envía y no coincide, devuelve `409 version_conflict`.
- Esta decisión prioriza operación simple en el Moto y trazabilidad por
  auditoría sobre un modelo de permisos más fino.

## Consecuencias

- Un participante autenticado puede corregir metadatos de cualquier proyecto;
  el costo es que la autorización de edición es más laxa que en fases
  posteriores.
- El catálogo de roles no bloquea la asignación: un typo crea un rol nuevo. Se
  puede limpiar en una fase futura si hace falta.
- La auditoría conserva quién editó y los valores anteriores/nuevos.
- El frontend puede mostrar el catálogo y usar `version` para evitar pisadas.

## Dudas abiertas

- ¿En Fase 3 conviene agregar borrado/renombrado de roles del catálogo?
- ¿La edición de proyectos debería restringirse a miembros del proyecto cuando
  exista UI multiusuario?
- ¿Los roles deberían pasar a afectar permisos además de etiquetar?
