package service

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/joacomedel/agentbank-go/internal/audit"
	"github.com/joacomedel/agentbank-go/internal/domain"
)

// Metrics devuelve contadores operativos.
func (s *Service) Metrics(ctx context.Context, actor Identity) (map[string]any, error) {
	if !actor.Participant.Admin {
		return nil, forbidden("requires_instance_admin", "solo el admin de instancia ve métricas", "", "admin.metrics", "")
	}
	tables := []string{"projects", "participants", "memberships", "tokens", "sessions", "tickets", "claims", "comments", "messages", "documents", "decisions", "audit_events", "outbox"}
	counts := make(map[string]int, len(tables))
	for _, table := range tables {
		var count int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM `+table).Scan(&count); err != nil {
			return nil, fmt.Errorf("contar %s: %w", table, err)
		}
		counts[table] = count
	}
	var activeClaims, pendingOutbox int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM claims WHERE released_at IS NULL`).Scan(&activeClaims); err != nil {
		return nil, fmt.Errorf("contar claims activos: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM outbox WHERE status = 'pendiente'`).Scan(&pendingOutbox); err != nil {
		return nil, fmt.Errorf("contar outbox: %w", err)
	}
	return map[string]any{
		"counts": counts, "active_claims": activeClaims, "pending_outbox": pendingOutbox,
	}, nil
}

// VerifyAll verifica todas las cadenas de auditoría.
func (s *Service) VerifyAll(ctx context.Context, actor Identity) (map[string]any, error) {
	if !actor.Participant.Admin {
		return nil, forbidden("requires_instance_admin", "solo el admin de instancia verifica todas las cadenas", "", "admin.verify_all", "")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT chain_id FROM audit_chain_heads ORDER BY chain_id`)
	if err != nil {
		return nil, fmt.Errorf("listar cadenas: %w", err)
	}
	chains := make([]string, 0, 8)
	for rows.Next() {
		var chainID string
		if err := rows.Scan(&chainID); err != nil {
			rows.Close()
			return nil, fmt.Errorf("escanear cadena: %w", err)
		}
		chains = append(chains, chainID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("recorrer cadenas: %w", err)
	}
	_ = rows.Close()
	results := make([]audit.VerifyResult, 0, len(chains))
	allOK := true
	for _, chainID := range chains {
		result, err := audit.Verify(ctx, s.db, chainID)
		if err != nil {
			return nil, err
		}
		if !result.OK {
			allOK = false
		}
		results = append(results, result)
	}
	return map[string]any{"ok": allOK, "chains": results}, nil
}

// SweepAll ejecuta los barridos de claims y defaults.
func (s *Service) SweepAll(ctx context.Context, actor Identity) (map[string]int, error) {
	if !actor.Participant.Admin {
		return nil, forbidden("requires_instance_admin", "solo el admin de instancia ejecuta barridos", "", "admin.barridos", "")
	}
	claims, err := s.SweepExpiredClaims(ctx)
	if err != nil {
		return nil, err
	}
	defaults, err := s.SweepMessageDefaults(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]int{"claims_liberados": claims, "defaults_asumidos": defaults}, nil
}

// RecordAdminSweep deja constancia del barrido.
func (s *Service) RecordAdminSweep(ctx context.Context, actor Identity, counts map[string]int) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		_, err := audit.Append(ctx, tx, audit.Input{
			ChainID: domain.SystemChain,
			Actor:   auditActor(actor),
			Action:  "admin.barridos", ObjectType: "sistema", ObjectID: "barridos",
			Payload: counts, CreatedAt: s.now().UTC(),
		})
		return err
	})
}
