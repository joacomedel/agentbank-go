-- Fase 0/1: identidad, membresías y base de auditoría.
-- SQLite: UUID como TEXT, timestamps RFC3339, roles como JSON TEXT.

CREATE TABLE IF NOT EXISTS projects (
    id          TEXT PRIMARY KEY,
    slug        TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    prefix      TEXT NOT NULL UNIQUE,
    version     INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS participants (
    id          TEXT PRIMARY KEY,
    type        TEXT NOT NULL CHECK (type IN ('humano', 'agente')),
    name        TEXT NOT NULL UNIQUE,
    admin       INTEGER NOT NULL DEFAULT 0 CHECK (admin IN (0, 1)),
    created_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS memberships (
    participant_id TEXT NOT NULL REFERENCES participants(id) ON DELETE CASCADE,
    project_id     TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    permission     TEXT NOT NULL CHECK (permission IN ('lector', 'colaborador', 'admin')),
    roles          TEXT NOT NULL DEFAULT '[]',
    created_at     TEXT NOT NULL,
    PRIMARY KEY (participant_id, project_id)
);

CREATE TABLE IF NOT EXISTS tokens (
    id             TEXT PRIMARY KEY,
    participant_id TEXT NOT NULL REFERENCES participants(id) ON DELETE CASCADE,
    token_hash     TEXT NOT NULL UNIQUE,
    created_at     TEXT NOT NULL,
    revoked_at     TEXT
);

CREATE INDEX IF NOT EXISTS idx_tokens_participant
    ON tokens(participant_id) WHERE revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS sessions (
    id             TEXT PRIMARY KEY,
    participant_id TEXT NOT NULL REFERENCES participants(id) ON DELETE CASCADE,
    token_id       TEXT REFERENCES tokens(id),
    origin         TEXT NOT NULL DEFAULT '',
    started_at     TEXT NOT NULL,
    last_seen_at   TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sessions_participant ON sessions(participant_id);

CREATE TABLE IF NOT EXISTS audit_chain_heads (
    chain_id  TEXT PRIMARY KEY,
    last_seq  INTEGER NOT NULL DEFAULT 0,
    last_hash TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS audit_events (
    id          TEXT PRIMARY KEY,
    chain_id    TEXT NOT NULL,
    seq         INTEGER NOT NULL,
    actor_id    TEXT,
    actor_type  TEXT,
    action      TEXT NOT NULL,
    object_type TEXT NOT NULL,
    object_id   TEXT,
    project_id  TEXT,
    payload     TEXT NOT NULL DEFAULT '{}',
    prev_hash   TEXT NOT NULL,
    hash        TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    UNIQUE (chain_id, seq)
);

CREATE INDEX IF NOT EXISTS idx_audit_chain ON audit_events(chain_id, seq);
CREATE INDEX IF NOT EXISTS idx_audit_object ON audit_events(object_type, object_id);
CREATE INDEX IF NOT EXISTS idx_audit_project ON audit_events(project_id) WHERE project_id IS NOT NULL;

INSERT OR IGNORE INTO audit_chain_heads(chain_id, last_seq, last_hash)
VALUES ('system', 0, '');
