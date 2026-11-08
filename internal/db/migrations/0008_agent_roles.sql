-- Fase 7/8: rol funcional del agente y snapshot del rol en tickets/auditoría.
-- No rompe filas existentes: los valores viejos quedan vacíos.

ALTER TABLE participants ADD COLUMN agent_role TEXT NOT NULL DEFAULT '';
ALTER TABLE tickets ADD COLUMN created_by_role TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_events ADD COLUMN actor_role TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_participants_agent_role
    ON participants(agent_role) WHERE agent_role <> '';
CREATE INDEX IF NOT EXISTS idx_tickets_created_by_role
    ON tickets(created_by_role) WHERE created_by_role <> '';
