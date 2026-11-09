-- Fase 9: snapshot del rol del actor en cada creación y en los cambios.
-- `audit_events.actor_role` ya guarda el rol de cada evento; estas columnas
-- hacen visible el rol en las entidades sin tener que cruzar auditoría.
-- Forward-only: las filas viejas quedan con '' si no se pueden reconstruir.

ALTER TABLE projects ADD COLUMN created_by TEXT;
ALTER TABLE projects ADD COLUMN created_by_role TEXT NOT NULL DEFAULT '';

ALTER TABLE participants ADD COLUMN created_by TEXT;
ALTER TABLE participants ADD COLUMN created_by_role TEXT NOT NULL DEFAULT '';

ALTER TABLE memberships ADD COLUMN granted_by TEXT;
ALTER TABLE memberships ADD COLUMN granted_by_role TEXT NOT NULL DEFAULT '';

ALTER TABLE tokens ADD COLUMN created_by_role TEXT NOT NULL DEFAULT '';

ALTER TABLE sessions ADD COLUMN agent_role TEXT NOT NULL DEFAULT '';

ALTER TABLE phases ADD COLUMN created_by_role TEXT NOT NULL DEFAULT '';
ALTER TABLE epics ADD COLUMN created_by_role TEXT NOT NULL DEFAULT '';

ALTER TABLE tickets ADD COLUMN updated_by_role TEXT NOT NULL DEFAULT '';

ALTER TABLE ticket_edges ADD COLUMN created_by_role TEXT NOT NULL DEFAULT '';

ALTER TABLE claims ADD COLUMN created_by_role TEXT NOT NULL DEFAULT '';

ALTER TABLE comments ADD COLUMN author_role TEXT NOT NULL DEFAULT '';

ALTER TABLE messages ADD COLUMN from_role TEXT NOT NULL DEFAULT '';
ALTER TABLE messages ADD COLUMN resolved_by_role TEXT NOT NULL DEFAULT '';
ALTER TABLE messages ADD COLUMN taken_by_role TEXT NOT NULL DEFAULT '';

ALTER TABLE documents ADD COLUMN created_by_role TEXT NOT NULL DEFAULT '';
ALTER TABLE document_versions ADD COLUMN author_role TEXT NOT NULL DEFAULT '';
ALTER TABLE decisions ADD COLUMN created_by_role TEXT NOT NULL DEFAULT '';

ALTER TABLE webhooks ADD COLUMN created_by_role TEXT NOT NULL DEFAULT '';

ALTER TABLE ticket_documents ADD COLUMN created_by TEXT REFERENCES participants(id);
ALTER TABLE ticket_documents ADD COLUMN created_by_role TEXT NOT NULL DEFAULT '';

-- Backfill best-effort con el rol actual del participante. No es perfecto
-- para roles que cambiaron, pero mejora los datos históricos.
UPDATE phases SET created_by_role = COALESCE(
    (SELECT p.agent_role FROM participants p WHERE p.id = phases.created_by), ''
) WHERE created_by_role = '';

UPDATE epics SET created_by_role = COALESCE(
    (SELECT p.agent_role FROM participants p WHERE p.id = epics.created_by), ''
) WHERE created_by_role = '';

UPDATE tickets SET updated_by_role = created_by_role WHERE updated_by_role = '';

UPDATE ticket_edges SET created_by_role = COALESCE(
    (SELECT p.agent_role FROM participants p WHERE p.id = ticket_edges.created_by), ''
) WHERE created_by_role = '';

UPDATE claims SET created_by_role = COALESCE(
    (SELECT p.agent_role FROM participants p WHERE p.id = claims.participant_id), ''
) WHERE created_by_role = '';

UPDATE comments SET author_role = COALESCE(
    (SELECT p.agent_role FROM participants p WHERE p.id = comments.author_participant_id), ''
) WHERE author_role = '';

UPDATE messages SET from_role = COALESCE(
    (SELECT p.agent_role FROM participants p WHERE p.id = messages.from_participant_id), ''
) WHERE from_role = '';

UPDATE messages SET resolved_by_role = COALESCE(
    (SELECT p.agent_role FROM participants p WHERE p.id = messages.resolved_by), ''
) WHERE resolved_by_role = '';

UPDATE messages SET taken_by_role = COALESCE(
    (SELECT p.agent_role FROM participants p WHERE p.id = messages.taken_by), ''
) WHERE taken_by_role = '';

UPDATE documents SET created_by_role = COALESCE(
    (SELECT p.agent_role FROM participants p WHERE p.id = documents.created_by), ''
) WHERE created_by_role = '';

UPDATE document_versions SET author_role = COALESCE(
    (SELECT p.agent_role FROM participants p WHERE p.id = document_versions.author_participant_id), ''
) WHERE author_role = '';

UPDATE decisions SET created_by_role = COALESCE(
    (SELECT p.agent_role FROM participants p WHERE p.id = decisions.created_by), ''
) WHERE created_by_role = '';

UPDATE webhooks SET created_by_role = COALESCE(
    (SELECT p.agent_role FROM participants p WHERE p.id = webhooks.created_by), ''
) WHERE created_by_role = '';

UPDATE sessions SET agent_role = COALESCE(
    (SELECT p.agent_role FROM participants p WHERE p.id = sessions.participant_id), ''
) WHERE agent_role = '';

CREATE INDEX IF NOT EXISTS idx_audit_actor_role
    ON audit_events(actor_role) WHERE actor_role <> '';
CREATE INDEX IF NOT EXISTS idx_tickets_updated_by_role
    ON tickets(updated_by_role) WHERE updated_by_role <> '';
