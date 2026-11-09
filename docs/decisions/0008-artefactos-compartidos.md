# ADR 0008 — Artefactos compartidos entre proyectos

- Estado: aceptado
- Fecha: 2026-11-09

## Contexto

Cada proyecto expone archivos que otro proyecto necesita consumir: el contrato
OpenAPI, la documentación de API, esquemas de base y manifiestos de versión.
Copiarlos por chat, filesystem o webhooks acopla a los proyectos y no deja
trazabilidad de qué versión se consumió. Los documentos (`documents`) ya
existen, pero están pensados para markdown de conocimiento, tienen `type`
restringido a `doc|glosario` y visibilidad por membresía; forzarlos como
artefactos requería reconstruir la tabla por el `CHECK`.

## Decisión

1. Se agrega una entidad **artefacto** separada (`artifacts` +
   `artifact_versions`) con path, content-type, hash, tamaño y
   `source_commit`.
2. Los artefactos son de **lectura para toda la instancia**: cualquier
   participante autenticado puede listarlos y descargarlos, aunque no sea
   miembro del proyecto. Publicar, versionar y borrar requiere ser
   `colaborador` o `admin` del proyecto.
3. Solo se acepta **texto UTF-8** de hasta 1 MiB (JSON, YAML, Markdown, texto,
   XML, TOML, JS, SVG). Sin binarios ni adjuntos.
4. El contenido se deduplica por hash SHA-256: republicar lo mismo devuelve
   `sin_cambios` y no crea versión.
5. `GET /v1/projects/{id}/artifacts/raw/{path...}` devuelve el contenido crudo
   con `ETag` igual al hash y responde `304` ante `If-None-Match`.
6. Toda publicación/borrado se audita dentro de la misma transacción
   (`artefacto.publicar`, `artefacto.eliminar`) en la cadena del proyecto.
7. La frescura se garantiza en CI: `scripts/publish-artifacts.sh --check`
   compara byte a byte el artefacto publicado con el archivo del checkout y
   falla si falta o difiere.

## Consecuencias

- Un proyecto puede descubrir y consumir el contrato de otro con una API
  estable, sin compartir filesystem.
- El escaneo de secretos es obligatorio porque la lectura es de instancia:
  publicar es una decisión de exposición.
- No hay búsqueda FTS dentro de artefactos ni soporte binario; si se necesitan,
  se evalúan en otra ADR.
- La UI puede listar/descargar artefactos, pero no es parte de este repo.
