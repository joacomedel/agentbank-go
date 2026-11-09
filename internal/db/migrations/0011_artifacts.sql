-- Fase 6+: artefactos compartidos (archivos expuestos por proyecto).

CREATE TABLE IF NOT EXISTS artifacts (
    id              TEXT PRIMARY KEY,
    project_id      TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    path            TEXT NOT NULL,
    content_type    TEXT NOT NULL DEFAULT 'text/plain',
    current_version INTEGER NOT NULL DEFAULT 1,
    source_commit   TEXT NOT NULL DEFAULT '',
    created_by      TEXT REFERENCES participants(id),
    created_by_role TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_artifacts_project_path
    ON artifacts(project_id, path);
CREATE INDEX IF NOT EXISTS idx_artifacts_project_updated
    ON artifacts(project_id, updated_at);
CREATE INDEX IF NOT EXISTS idx_artifacts_updated
    ON artifacts(updated_at);

CREATE TABLE IF NOT EXISTS artifact_versions (
    id                    TEXT PRIMARY KEY,
    artifact_id           TEXT NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE,
    version               INTEGER NOT NULL,
    content               TEXT NOT NULL,
    hash                  TEXT NOT NULL,
    size_bytes            INTEGER NOT NULL,
    content_type          TEXT NOT NULL,
    source_commit         TEXT NOT NULL DEFAULT '',
    author_participant_id TEXT REFERENCES participants(id),
    author_role           TEXT NOT NULL DEFAULT '',
    created_at            TEXT NOT NULL,
    UNIQUE (artifact_id, version)
);

CREATE INDEX IF NOT EXISTS idx_artifact_versions_artifact
    ON artifact_versions(artifact_id, version);
