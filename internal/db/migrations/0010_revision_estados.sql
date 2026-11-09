-- Fase 10: revisión en la máquina de estados y evidencia estructurada.
-- Forward-only: los tickets históricos conservan su estado y sus datos.
-- `requiere_revision = 1` es el default para todo ticket nuevo; un ticket sin
-- cambio de código puede crearse con 0 y cerrarse sin pasar por revisión.

ALTER TABLE tickets ADD COLUMN requiere_revision INTEGER NOT NULL DEFAULT 1;
ALTER TABLE tickets ADD COLUMN commit_sha TEXT NOT NULL DEFAULT '';
ALTER TABLE tickets ADD COLUMN reaperturas INTEGER NOT NULL DEFAULT 0;

-- Cola de auditoría: tickets esperando veredicto.
CREATE INDEX IF NOT EXISTS idx_tickets_status_updated
    ON tickets(status, updated_at) WHERE status IN ('en revisión', 'hecho');

-- El veredicto de auditoría usa un tipo de comentario propio. SQLite no
-- permite alterar un CHECK, así que se reconstruye la tabla conservando datos.
CREATE TABLE comments_new (
    id                   TEXT PRIMARY KEY,
    ticket_id            TEXT NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    author_participant_id TEXT NOT NULL REFERENCES participants(id),
    type                 TEXT NOT NULL DEFAULT 'comentario'
        CHECK (type IN ('comentario', 'nota', 'cierre', 'revisión')),
    body                 TEXT NOT NULL,
    author_role          TEXT NOT NULL DEFAULT '',
    created_at           TEXT NOT NULL
);

INSERT INTO comments_new(id, ticket_id, author_participant_id, type, body, author_role, created_at)
SELECT id, ticket_id, author_participant_id, type, body, author_role, created_at
FROM comments;

DROP TABLE comments;
ALTER TABLE comments_new RENAME TO comments;

CREATE INDEX IF NOT EXISTS idx_comments_ticket ON comments(ticket_id, created_at);
