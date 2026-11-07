-- Fase 1: catálogo de roles por proyecto.
-- Los roles se auto-registran al asignar una membresía. Esta migración
-- también rellena el catálogo con los roles ya existentes.

CREATE TABLE IF NOT EXISTS project_roles (
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (project_id, name)
);

INSERT OR IGNORE INTO project_roles(project_id, name, created_at)
SELECT m.project_id, j.value, m.created_at
FROM memberships m, json_each(m.roles) AS j
WHERE json_valid(m.roles);
