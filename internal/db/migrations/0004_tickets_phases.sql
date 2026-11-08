-- Fase 2/3: tickets, dependencias, claims, comentarios, fases y épicas.

ALTER TABLE participants ADD COLUMN status TEXT NOT NULL DEFAULT 'activo'
    CHECK (status IN ('activo', 'baja'));
ALTER TABLE participants ADD COLUMN last_heartbeat_at TEXT;

ALTER TABLE projects ADD COLUMN paused INTEGER NOT NULL DEFAULT 0
    CHECK (paused IN (0, 1));
ALTER TABLE projects ADD COLUMN consultar_siempre TEXT NOT NULL DEFAULT '[]';
ALTER TABLE projects ADD COLUMN assume_default_after_minutes INTEGER NOT NULL DEFAULT 360;

CREATE TABLE IF NOT EXISTS phases (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    key         TEXT,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    order_index INTEGER NOT NULL DEFAULT 0,
    status      TEXT NOT NULL DEFAULT 'abierta' CHECK (status IN ('abierta', 'cerrada')),
    created_by  TEXT REFERENCES participants(id),
    created_at  TEXT NOT NULL,
    closed_at   TEXT
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_phases_project_key
    ON phases(project_id, key) WHERE key IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_phases_project_order ON phases(project_id, order_index);

CREATE TABLE IF NOT EXISTS epics (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    phase_id    TEXT REFERENCES phases(id) ON DELETE SET NULL,
    key         TEXT,
    title       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    order_index INTEGER NOT NULL DEFAULT 0,
    status      TEXT NOT NULL DEFAULT 'abierta' CHECK (status IN ('abierta', 'cerrada')),
    created_by  TEXT REFERENCES participants(id),
    created_at  TEXT NOT NULL,
    closed_at   TEXT
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_epics_project_key
    ON epics(project_id, key) WHERE key IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_epics_project_phase_order ON epics(project_id, phase_id, order_index);

CREATE TABLE IF NOT EXISTS tickets (
    id            TEXT PRIMARY KEY,
    project_id    TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    number        INTEGER NOT NULL,
    key           TEXT,
    title         TEXT NOT NULL,
    description   TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'abierto'
        CHECK (status IN ('abierto', 'en curso', 'en revisión', 'hecho', 'bloqueado', 'cancelado')),
    priority      TEXT NOT NULL DEFAULT 'media'
        CHECK (priority IN ('baja', 'media', 'alta', 'urgente')),
    assignee_id   TEXT REFERENCES participants(id),
    assigned_role TEXT,
    phase_id      TEXT REFERENCES phases(id) ON DELETE SET NULL,
    epic_id       TEXT REFERENCES epics(id) ON DELETE SET NULL,
    order_index   INTEGER NOT NULL DEFAULT 0,
    version       INTEGER NOT NULL DEFAULT 1,
    paths         TEXT NOT NULL DEFAULT '[]',
    discovered_in TEXT REFERENCES tickets(id) ON DELETE SET NULL,
    created_by    TEXT REFERENCES participants(id),
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_tickets_project_number ON tickets(project_id, number);
CREATE UNIQUE INDEX IF NOT EXISTS uq_tickets_project_key
    ON tickets(project_id, key) WHERE key IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_tickets_project_status ON tickets(project_id, status);
CREATE INDEX IF NOT EXISTS idx_tickets_queue
    ON tickets(project_id, phase_id, epic_id, order_index, created_at);
CREATE INDEX IF NOT EXISTS idx_tickets_assignee
    ON tickets(assignee_id) WHERE assignee_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS ticket_edges (
    ticket_id      TEXT NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    depends_on_id  TEXT NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    created_by     TEXT REFERENCES participants(id),
    created_at     TEXT NOT NULL,
    PRIMARY KEY (ticket_id, depends_on_id),
    CHECK (ticket_id <> depends_on_id)
);

CREATE INDEX IF NOT EXISTS idx_ticket_edges_depends ON ticket_edges(depends_on_id);

CREATE TABLE IF NOT EXISTS claims (
    id              TEXT PRIMARY KEY,
    ticket_id       TEXT NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    participant_id  TEXT NOT NULL REFERENCES participants(id) ON DELETE CASCADE,
    session_id      TEXT REFERENCES sessions(id) ON DELETE SET NULL,
    status_previo   TEXT NOT NULL,
    lease_until     TEXT NOT NULL,
    last_activity_at TEXT NOT NULL,
    created_at      TEXT NOT NULL,
    released_at     TEXT,
    reason          TEXT
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_claims_ticket_active
    ON claims(ticket_id) WHERE released_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_claims_participant_active
    ON claims(participant_id) WHERE released_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_claims_lease ON claims(lease_until) WHERE released_at IS NULL;

CREATE TABLE IF NOT EXISTS comments (
    id                   TEXT PRIMARY KEY,
    ticket_id            TEXT NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    author_participant_id TEXT NOT NULL REFERENCES participants(id),
    type                 TEXT NOT NULL DEFAULT 'comentario'
        CHECK (type IN ('comentario', 'nota', 'cierre')),
    body                 TEXT NOT NULL,
    created_at           TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_comments_ticket ON comments(ticket_id, created_at);
