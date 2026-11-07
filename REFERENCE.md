# Referencia — repo viejo

- Ruta: `/root/reference/BancoDeDatosAgente`
- Commit: `95f9a00`
- Rama por defecto: `feat/v3.1-requisitos`
- Visibilidad: privada
- Uso: **solo lectura**

No modificar, no commitear, no pushear desde este proyecto.

## Qué leer

| Documento | Para qué |
|---|---|
| `spec-banco-datos-agentes.md` | visión V1, modelo, auditoría, seguridad |
| `spec-banco-datos-agentes-v2.md` | cola de trabajo, épicas, MCP, UI |
| `requisitos-v3.1.md` | fases, participantes, mensajes, runner |
| `IMPLEMENTACION-V31.md` | estado y decisiones de implementación |
| `TAREAS-V31.md` | pendientes y deuda |
| `relevamiento-v3.md` | evidencia de problemas reales |
| `packages/server/migrations/*.sql` | modelo de datos |
| `packages/server/src/routes/*` | contrato HTTP |
| `packages/server/src/services/*` | reglas de negocio |
| `packages/shared/src/*` | dominio y audit |

## Qué NO copiar

- `packages/server/src/ui/**` y cualquier HTML/CSS/JS de UI.
- `DESIGN-UI.md`, `REFERENCIAS-UI.md`, scripts de smoke de UI.
- Código TypeScript línea por línea.
- SQL de Postgres con `jsonb`, `text[]`, `tsvector` o triggers.
- El adaptador de OpenCode 2 sin validarlo contra la versión real.
