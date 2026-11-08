-- Fase 5: documentos versionados, globales, decisiones/ADRs y adjuntos.

CREATE TABLE IF NOT EXISTS documents (
    id              TEXT PRIMARY KEY,
    project_id      TEXT REFERENCES projects(id) ON DELETE CASCADE,
    title           TEXT NOT NULL,
    route           TEXT,
    type            TEXT NOT NULL DEFAULT 'doc'
        CHECK (type IN ('doc', 'glosario')),
    pinned          INTEGER NOT NULL DEFAULT 0 CHECK (pinned IN (0, 1)),
    current_version INTEGER NOT NULL DEFAULT 0,
    created_by      TEXT REFERENCES participants(id),
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_documents_project_route
    ON documents(project_id, route)
    WHERE project_id IS NOT NULL AND route IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_documents_global_route
    ON documents(route)
    WHERE project_id IS NULL AND route IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_documents_project ON documents(project_id, updated_at);

CREATE TABLE IF NOT EXISTS document_versions (
    id                    TEXT PRIMARY KEY,
    document_id           TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    version               INTEGER NOT NULL,
    content               TEXT NOT NULL,
    hash                  TEXT NOT NULL,
    author_participant_id TEXT NOT NULL REFERENCES participants(id),
    created_at            TEXT NOT NULL,
    UNIQUE (document_id, version)
);

CREATE INDEX IF NOT EXISTS idx_document_versions_doc ON document_versions(document_id, version);

CREATE TABLE IF NOT EXISTS decisions (
    id                TEXT PRIMARY KEY,
    project_id        TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    title             TEXT NOT NULL,
    status            TEXT NOT NULL DEFAULT 'vigente'
        CHECK (status IN ('vigente', 'reemplazada')),
    body              TEXT NOT NULL,
    source_message_id TEXT REFERENCES messages(id) ON DELETE SET NULL,
    created_by        TEXT REFERENCES participants(id),
    created_at        TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_decisions_project ON decisions(project_id, created_at);

CREATE TABLE IF NOT EXISTS ticket_documents (
    ticket_id   TEXT NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    created_at  TEXT NOT NULL,
    PRIMARY KEY (ticket_id, document_id)
);
