-- Fase 1: un solo token activo por participante e índices de búsqueda.
-- forward-only; no modifica datos existentes.

DROP INDEX IF EXISTS idx_tokens_participant;
CREATE UNIQUE INDEX IF NOT EXISTS uq_tokens_participant_active
    ON tokens(participant_id) WHERE revoked_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_memberships_project
    ON memberships(project_id);
CREATE INDEX IF NOT EXISTS idx_sessions_token
    ON sessions(token_id);
CREATE INDEX IF NOT EXISTS idx_audit_created
    ON audit_events(created_at);
CREATE INDEX IF NOT EXISTS idx_audit_actor
    ON audit_events(actor_id) WHERE actor_id IS NOT NULL;
