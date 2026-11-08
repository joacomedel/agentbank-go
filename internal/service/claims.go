package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/joacomedel/agentbank-go/internal/audit"
	"github.com/joacomedel/agentbank-go/internal/domain"
	"github.com/joacomedel/agentbank-go/internal/id"
	"github.com/joacomedel/agentbank-go/internal/timefmt"
)

// ClaimTicket toma un claim con lease si el ticket está listo.
func (s *Service) ClaimTicket(ctx context.Context, actor Identity, ref string) (ClaimResult, error) {
	tc, err := resolveTicket(ctx, s.db, ref)
	if err != nil {
		return ClaimResult{}, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return ClaimResult{}, err
	}
	if !rights.CanWrite(tc.Project.ID) {
		return ClaimResult{}, forbidden("requires_project_write", "sin permiso de escritura en el proyecto", tc.Project.ID, "ticket.claim", tc.Ticket.ID)
	}
	var result ClaimResult
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		current, err := ticketByID(ctx, tx, tc.Ticket.ID)
		if err != nil {
			return err
		}
		effective, err := s.effectiveStatus(ctx, tx, current)
		if err != nil {
			return err
		}
		if effective != "listo" {
			return conflict("ticket_no_listo", "el ticket no está listo: "+effective)
		}
		var active int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(1) FROM claims WHERE ticket_id = ? AND released_at IS NULL`,
			current.ID,
		).Scan(&active); err != nil {
			return fmt.Errorf("consultar claims: %w", err)
		}
		if active > 0 {
			return conflict("claim_conflict", "el ticket ya tiene un claim activo")
		}
		now := s.now().UTC()
		claim := domain.Claim{
			ID: id.New(), TicketID: current.ID, ParticipantID: actor.Participant.ID,
			SessionID: actor.SessionID, StatusPrevious: current.Status,
			LeaseUntil: now.Add(s.claimTTL), LastActivityAt: now, CreatedAt: now,
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO claims(id, ticket_id, participant_id, session_id, status_previo,
			                   lease_until, last_activity_at, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			claim.ID, claim.TicketID, claim.ParticipantID, nullable(claim.SessionID),
			claim.StatusPrevious, timefmt.Format(claim.LeaseUntil),
			timefmt.Format(claim.LastActivityAt), timefmt.Format(claim.CreatedAt),
		); err != nil {
			if isUniqueConstraint(err) {
				return conflict("claim_conflict", "el ticket ya tiene un claim activo")
			}
			return fmt.Errorf("crear claim: %w", err)
		}
		current.Status = domain.TicketInProgress
		current.Version++
		current.UpdatedAt = now
		if _, err := tx.ExecContext(ctx,
			`UPDATE tickets SET status = ?, version = ?, updated_at = ? WHERE id = ?`,
			current.Status, current.Version, timefmt.Format(now), current.ID,
		); err != nil {
			return fmt.Errorf("marcar ticket en curso: %w", err)
		}
		if _, err := audit.Append(ctx, tx, audit.Input{
			ChainID: tc.Project.ID, Actor: auditActor(actor),
			Action: "ticket.claim", ObjectType: "ticket", ObjectID: current.ID, ProjectID: tc.Project.ID,
			Payload: map[string]any{
				"ticket_id": current.ID, "claim_id": claim.ID,
				"participant_id": actor.Participant.ID, "lease_until": timefmt.Format(claim.LeaseUntil),
			},
			CreatedAt: now,
		}); err != nil {
			return err
		}
		view, err := s.ticketView(ctx, tx, current, tc.Project, true)
		if err != nil {
			return err
		}
		result = ClaimResult{
			Ticket: view,
			Claim: ClaimView{
				ID: claim.ID, ParticipantID: claim.ParticipantID, Name: actor.Participant.Name,
				LeaseUntil: claim.LeaseUntil, CreatedAt: claim.CreatedAt,
			},
		}
		return nil
	})
	if err != nil {
		return ClaimResult{}, err
	}
	return result, nil
}

// ReleaseClaim libera el claim activo del ticket.
func (s *Service) ReleaseClaim(ctx context.Context, actor Identity, ref string) (TicketView, error) {
	tc, err := resolveTicket(ctx, s.db, ref)
	if err != nil {
		return TicketView{}, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return TicketView{}, err
	}
	var view TicketView
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		claim, err := activeClaimForTicket(ctx, tx, tc.Ticket.ID)
		if errors.Is(err, sql.ErrNoRows) {
			return notFound("claim_not_found", "el ticket no tiene claim activo")
		}
		if err != nil {
			return err
		}
		if claim.ParticipantID != actor.Participant.ID && !rights.Admin && !rights.CanAdmin(tc.Project.ID) {
			return forbidden("forbidden", "solo el dueño del claim o un admin puede liberarlo", tc.Project.ID, "ticket.claim_liberar", tc.Ticket.ID)
		}
		current, err := ticketByID(ctx, tx, tc.Ticket.ID)
		if err != nil {
			return err
		}
		now := s.now().UTC()
		if _, err := tx.ExecContext(ctx,
			`UPDATE claims SET released_at = ?, reason = ? WHERE id = ?`,
			timefmt.Format(now), "liberado manualmente", claim.ID,
		); err != nil {
			return fmt.Errorf("liberar claim: %w", err)
		}
		if current.Status == domain.TicketInProgress {
			current.Status = claim.StatusPrevious
		}
		current.Version++
		current.UpdatedAt = now
		if _, err := tx.ExecContext(ctx,
			`UPDATE tickets SET status = ?, version = ?, updated_at = ? WHERE id = ?`,
			current.Status, current.Version, timefmt.Format(now), current.ID,
		); err != nil {
			return fmt.Errorf("restaurar ticket: %w", err)
		}
		if _, err := audit.Append(ctx, tx, audit.Input{
			ChainID: tc.Project.ID, Actor: auditActor(actor),
			Action: "ticket.claim_liberar", ObjectType: "ticket", ObjectID: current.ID, ProjectID: tc.Project.ID,
			Payload:   map[string]any{"ticket_id": current.ID, "claim_id": claim.ID, "status": current.Status},
			CreatedAt: now,
		}); err != nil {
			return err
		}
		view, err = s.ticketView(ctx, tx, current, tc.Project, true)
		return err
	})
	if err != nil {
		return TicketView{}, err
	}
	return view, nil
}

func activeClaimForTicket(ctx context.Context, q queryer, ticketID string) (domain.Claim, error) {
	var claim domain.Claim
	var sessionID, releasedAt, reason sql.NullString
	var lease, lastActivity, createdAt string
	err := q.QueryRowContext(ctx, `
		SELECT id, ticket_id, participant_id, session_id, status_previo,
		       lease_until, last_activity_at, created_at, released_at, reason
		FROM claims WHERE ticket_id = ? AND released_at IS NULL`, ticketID,
	).Scan(&claim.ID, &claim.TicketID, &claim.ParticipantID, &sessionID, &claim.StatusPrevious,
		&lease, &lastActivity, &createdAt, &releasedAt, &reason)
	if err != nil {
		return domain.Claim{}, err
	}
	claim.SessionID = sessionID.String
	claim.Reason = reason.String
	parsedLease, err := timefmt.Parse(lease)
	if err != nil {
		return domain.Claim{}, fmt.Errorf("lease inválido: %w", err)
	}
	parsedActivity, err := timefmt.Parse(lastActivity)
	if err != nil {
		return domain.Claim{}, fmt.Errorf("last_activity inválido: %w", err)
	}
	parsedCreated, err := timefmt.Parse(createdAt)
	if err != nil {
		return domain.Claim{}, fmt.Errorf("created_at de claim inválido: %w", err)
	}
	claim.LeaseUntil = parsedLease
	claim.LastActivityAt = parsedActivity
	claim.CreatedAt = parsedCreated
	claim.ReleasedAt = parseNullableTime(releasedAt)
	return claim, nil
}

func releaseClaimsForTicket(ctx context.Context, tx *sql.Tx, ticketID string, now time.Time, reason string, actor Identity, chainID string) error {
	rows, err := tx.QueryContext(ctx,
		`SELECT id FROM claims WHERE ticket_id = ? AND released_at IS NULL`, ticketID)
	if err != nil {
		return fmt.Errorf("buscar claims activos: %w", err)
	}
	ids := make([]string, 0, 1)
	for rows.Next() {
		var claimID string
		if err := rows.Scan(&claimID); err != nil {
			rows.Close()
			return fmt.Errorf("escanear claim activo: %w", err)
		}
		ids = append(ids, claimID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("recorrer claims activos: %w", err)
	}
	_ = rows.Close()
	for _, claimID := range ids {
		if _, err := tx.ExecContext(ctx,
			`UPDATE claims SET released_at = ?, reason = ? WHERE id = ?`,
			timefmt.Format(now), reason, claimID,
		); err != nil {
			return fmt.Errorf("liberar claim %s: %w", claimID, err)
		}
		if _, err := audit.Append(ctx, tx, audit.Input{
			ChainID: chainID, Actor: auditActor(actor),
			Action: "ticket.claim_liberar", ObjectType: "ticket", ObjectID: ticketID, ProjectID: chainID,
			Payload:   map[string]any{"ticket_id": ticketID, "claim_id": claimID, "reason": reason},
			CreatedAt: now,
		}); err != nil {
			return err
		}
	}
	return nil
}

// SweepExpiredClaims libera claims vencidos. Devuelve cuántos liberó.
func (s *Service) SweepExpiredClaims(ctx context.Context) (int, error) {
	now := s.now().UTC()
	released := 0
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `
			SELECT c.id, c.ticket_id, c.participant_id, c.status_previo, t.project_id
			FROM claims c
			JOIN tickets t ON t.id = c.ticket_id
			WHERE c.released_at IS NULL AND c.lease_until < ?
			ORDER BY c.lease_until`, timefmt.Format(now))
		if err != nil {
			return fmt.Errorf("buscar claims vencidos: %w", err)
		}
		type expired struct {
			claimID, ticketID, participantID, statusPrevious, projectID string
		}
		items := make([]expired, 0, 8)
		for rows.Next() {
			var item expired
			if err := rows.Scan(&item.claimID, &item.ticketID, &item.participantID,
				&item.statusPrevious, &item.projectID); err != nil {
				rows.Close()
				return fmt.Errorf("escanear claim vencido: %w", err)
			}
			items = append(items, item)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("recorrer claims vencidos: %w", err)
		}
		_ = rows.Close()
		for _, item := range items {
			if _, err := tx.ExecContext(ctx,
				`UPDATE claims SET released_at = ?, reason = 'lease vencido' WHERE id = ?`,
				timefmt.Format(now), item.claimID,
			); err != nil {
				return fmt.Errorf("liberar claim vencido: %w", err)
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE tickets SET status = ?, version = version + 1, updated_at = ?
				WHERE id = ? AND status = 'en curso'`,
				item.statusPrevious, timefmt.Format(now), item.ticketID,
			); err != nil {
				return fmt.Errorf("restaurar ticket vencido: %w", err)
			}
			if _, err := audit.Append(ctx, tx, audit.Input{
				ChainID: item.projectID, Actor: audit.Actor{Type: domain.AuditActorAnonimo},
				Action: "ticket.claim_expirado", ObjectType: "ticket", ObjectID: item.ticketID,
				ProjectID: item.projectID,
				Payload: map[string]any{
					"ticket_id": item.ticketID, "claim_id": item.claimID,
					"participant_id": item.participantID, "reason": "lease vencido",
				},
				CreatedAt: now,
			}); err != nil {
				return err
			}
			released++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return released, nil
}

// Heartbeat actualiza la actividad de la sesión y del participante.
func (s *Service) Heartbeat(ctx context.Context, identity Identity) error {
	now := s.now().UTC()
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`UPDATE participants SET last_heartbeat_at = ? WHERE id = ?`,
			timefmt.Format(now), identity.Participant.ID,
		); err != nil {
			return fmt.Errorf("heartbeat participante: %w", err)
		}
		if identity.SessionID != "" {
			if _, err := tx.ExecContext(ctx,
				`UPDATE sessions SET last_seen_at = ? WHERE id = ?`,
				timefmt.Format(now), identity.SessionID,
			); err != nil {
				return fmt.Errorf("heartbeat sesión: %w", err)
			}
		}
		return nil
	})
}
