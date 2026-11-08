-- Fase 4: mensajes unificados, bandeja, opciones/default y afectación de tickets.

CREATE TABLE IF NOT EXISTS messages (
    id                       TEXT PRIMARY KEY,
    project_id               TEXT REFERENCES projects(id) ON DELETE CASCADE,
    from_participant_id      TEXT NOT NULL REFERENCES participants(id),
    type                     TEXT NOT NULL DEFAULT 'info'
        CHECK (type IN ('info', 'solicitud')),
    destination_type         TEXT NOT NULL DEFAULT 'proyecto'
        CHECK (destination_type IN ('participante', 'rol', 'proyecto')),
    destination_participant_id TEXT REFERENCES participants(id),
    destination_role         TEXT,
    subject                  TEXT NOT NULL,
    body                     TEXT NOT NULL,
    options                  TEXT NOT NULL DEFAULT '[]',
    default_option           INTEGER,
    assume_default_at        TEXT,
    resolution               TEXT
        CHECK (resolution IS NULL OR resolution IN ('respondida', 'default_asumido', 'descartada')),
    resolved_by              TEXT REFERENCES participants(id),
    resolved_at              TEXT,
    response_option          INTEGER,
    response_text            TEXT,
    taken_by                 TEXT REFERENCES participants(id),
    taken_at                 TEXT,
    reply_to                 TEXT REFERENCES messages(id) ON DELETE SET NULL,
    correlation_id           TEXT,
    created_at               TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_messages_project ON messages(project_id, created_at);
CREATE INDEX IF NOT EXISTS idx_messages_destination
    ON messages(destination_participant_id, destination_role);
CREATE INDEX IF NOT EXISTS idx_messages_correlation ON messages(correlation_id);
CREATE INDEX IF NOT EXISTS idx_messages_reply ON messages(reply_to);

CREATE TABLE IF NOT EXISTS message_affects (
    message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    ticket_id  TEXT NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    PRIMARY KEY (message_id, ticket_id)
);

CREATE INDEX IF NOT EXISTS idx_message_affects_ticket ON message_affects(ticket_id);

CREATE TABLE IF NOT EXISTS message_refs (
    message_id  TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    object_type TEXT NOT NULL,
    object_id   TEXT NOT NULL,
    PRIMARY KEY (message_id, object_type, object_id)
);

CREATE TABLE IF NOT EXISTS message_state (
    message_id     TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    participant_id TEXT NOT NULL REFERENCES participants(id) ON DELETE CASCADE,
    state          TEXT NOT NULL DEFAULT 'nuevo'
        CHECK (state IN ('nuevo', 'visto', 'atendido')),
    updated_at     TEXT NOT NULL,
    PRIMARY KEY (message_id, participant_id)
);

CREATE INDEX IF NOT EXISTS idx_message_state_participant
    ON message_state(participant_id, state);
