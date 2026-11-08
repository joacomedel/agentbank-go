-- Fase 6: operación, outbox, idempotencia, suscripciones y columnas de auditoría.

ALTER TABLE audit_events ADD COLUMN session_id TEXT;
ALTER TABLE audit_events ADD COLUMN origin TEXT;
ALTER TABLE audit_events ADD COLUMN correlation_id TEXT;
ALTER TABLE audit_events ADD COLUMN parent_event_id TEXT;
ALTER TABLE audit_events ADD COLUMN previous_value TEXT NOT NULL DEFAULT 'null';
ALTER TABLE audit_events ADD COLUMN content_hash TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_audit_correlation ON audit_events(correlation_id)
    WHERE correlation_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_audit_session ON audit_events(session_id)
    WHERE session_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS idempotency_keys (
    participant_id TEXT NOT NULL REFERENCES participants(id) ON DELETE CASCADE,
    key            TEXT NOT NULL,
    method         TEXT NOT NULL,
    path           TEXT NOT NULL,
    request_hash   TEXT NOT NULL,
    status_code    INTEGER NOT NULL,
    response_body  TEXT NOT NULL,
    created_at     TEXT NOT NULL,
    PRIMARY KEY (participant_id, key)
);

CREATE TABLE IF NOT EXISTS outbox (
    id              TEXT PRIMARY KEY,
    kind            TEXT NOT NULL,
    destination     TEXT NOT NULL,
    payload         TEXT NOT NULL DEFAULT '{}',
    attempts        INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'pendiente'
        CHECK (status IN ('pendiente', 'entregado', 'dead')),
    last_error      TEXT,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_outbox_pending ON outbox(status, next_attempt_at);

CREATE TABLE IF NOT EXISTS webhooks (
    id         TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    url        TEXT NOT NULL,
    secret     TEXT,
    events     TEXT NOT NULL DEFAULT '["*"]',
    active     INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0, 1)),
    created_by TEXT REFERENCES participants(id),
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS subscriptions (
    id             TEXT PRIMARY KEY,
    participant_id TEXT NOT NULL REFERENCES participants(id) ON DELETE CASCADE,
    object_type    TEXT NOT NULL,
    object_id      TEXT NOT NULL,
    created_at     TEXT NOT NULL,
    UNIQUE (participant_id, object_type, object_id)
);

CREATE TABLE IF NOT EXISTS participant_channels (
    participant_id TEXT NOT NULL REFERENCES participants(id) ON DELETE CASCADE,
    channel        TEXT NOT NULL,
    destination    TEXT NOT NULL,
    preferences    TEXT NOT NULL DEFAULT '{}',
    linked_at      TEXT NOT NULL,
    PRIMARY KEY (participant_id, channel)
);

CREATE TABLE IF NOT EXISTS telegram_link_codes (
    code           TEXT PRIMARY KEY,
    participant_id TEXT NOT NULL REFERENCES participants(id) ON DELETE CASCADE,
    expires_at     TEXT NOT NULL,
    used_at        TEXT
);
