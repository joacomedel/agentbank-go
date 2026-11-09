package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/joacomedel/agentbank-go/internal/domain"
	"github.com/joacomedel/agentbank-go/internal/service"
)

const validNote = "Solución: implementado\nCambios: internal/service\nVerificación: go test ./...\nArtefactos: abc1234\nRiesgos: ninguno"

func assertConflictCode(t *testing.T, err error, code string) {
	t.Helper()
	var conflict *service.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("err=%v, want ConflictError %s", err, code)
	}
	if conflict.Code != code {
		t.Fatalf("code=%q, want %q (%v)", conflict.Code, code, err)
	}
}

func assertValidationCode(t *testing.T, err error, code string) {
	t.Helper()
	var invalid *service.ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("err=%v, want ValidationError %s", err, code)
	}
	if invalid.Code != code {
		t.Fatalf("code=%q, want %q (%v)", invalid.Code, code, err)
	}
}

func TestTicketStateMachineTransitions(t *testing.T) {
	svc, _, _ := newTestService(t)
	human, _ := bootstrap(t, svc)
	project := createProjectForTickets(t, svc, human, "fsm")
	agent := createAgentForTickets(t, svc, human, "worker-fsm", project.Project.ID, domain.PermissionWriter, []string{"backend"})
	ctx := context.Background()

	mk := func(requires bool) service.TicketView {
		t.Helper()
		value := requires
		ticket, err := svc.CreateTicket(ctx, human, project.Project.ID, service.TicketInput{
			Title: "FSM", AssignedRole: "backend", RequiereRevision: &value,
		})
		if err != nil {
			t.Fatalf("CreateTicket: %v", err)
		}
		return ticket
	}
	claim := func(ticket service.TicketView) int64 {
		t.Helper()
		result, err := svc.ClaimTicket(ctx, agent, ticket.ID)
		if err != nil {
			t.Fatalf("ClaimTicket: %v", err)
		}
		return result.Ticket.Version
	}
	toReview := func(ticket service.TicketView) {
		t.Helper()
		version := claim(ticket)
		if _, err := svc.AdvanceTicket(ctx, agent, ticket.ID, service.AdvanceInput{
			Version: &version, Status: strPtr(domain.TicketInReview),
			ClosingNote: validNote, CommitSHA: "abc1234",
		}); err != nil {
			t.Fatalf("avanzar a revisión: %v", err)
		}
	}

	t.Run("abierto a hecho", func(t *testing.T) {
		ticket := mk(true)
		_, err := svc.AdvanceTicket(ctx, agent, ticket.ID, service.AdvanceInput{
			Status: strPtr(domain.TicketDone), ClosingNote: validNote, CommitSHA: "abc1234",
		})
		assertConflictCode(t, err, "transicion_invalida")
	})

	t.Run("abierto a en curso", func(t *testing.T) {
		ticket := mk(true)
		_, err := svc.AdvanceTicket(ctx, agent, ticket.ID, service.AdvanceInput{
			Status: strPtr(domain.TicketInProgress),
		})
		assertConflictCode(t, err, "transicion_invalida")
	})

	t.Run("en curso a hecho con revisión requerida", func(t *testing.T) {
		ticket := mk(true)
		version := claim(ticket)
		_, err := svc.AdvanceTicket(ctx, agent, ticket.ID, service.AdvanceInput{
			Version: &version, Status: strPtr(domain.TicketDone),
			ClosingNote: validNote, CommitSHA: "abc1234",
		})
		assertConflictCode(t, err, "transicion_invalida")
	})

	t.Run("en curso a en revisión", func(t *testing.T) {
		ticket := mk(true)
		version := claim(ticket)
		result, err := svc.AdvanceTicket(ctx, agent, ticket.ID, service.AdvanceInput{
			Version: &version, Status: strPtr(domain.TicketInReview),
			ClosingNote: validNote, CommitSHA: "abc1234",
		})
		if err != nil {
			t.Fatalf("AdvanceTicket: %v", err)
		}
		if result.Ticket.Status != domain.TicketInReview {
			t.Fatalf("status=%q", result.Ticket.Status)
		}
		if len(result.Ticket.Claims) != 0 {
			t.Fatalf("el claim debería liberarse al entrar en revisión: %+v", result.Ticket.Claims)
		}
		if result.Ticket.CommitSHA != "abc1234" {
			t.Fatalf("commit_sha=%q", result.Ticket.CommitSHA)
		}
	})

	t.Run("en revisión a hecho por avanzar", func(t *testing.T) {
		ticket := mk(true)
		toReview(ticket)
		_, err := svc.AdvanceTicket(ctx, agent, ticket.ID, service.AdvanceInput{
			Status: strPtr(domain.TicketDone),
		})
		assertConflictCode(t, err, "transicion_invalida")
	})

	t.Run("en revisión a abierto sin comentario", func(t *testing.T) {
		ticket := mk(true)
		toReview(ticket)
		_, err := svc.AdvanceTicket(ctx, agent, ticket.ID, service.AdvanceInput{
			Status: strPtr(domain.TicketOpen),
		})
		assertValidationCode(t, err, "")
	})

	t.Run("en revisión a abierto con comentario", func(t *testing.T) {
		ticket := mk(true)
		toReview(ticket)
		result, err := svc.AdvanceTicket(ctx, agent, ticket.ID, service.AdvanceInput{
			Status: strPtr(domain.TicketOpen), Comment: "falta un test",
		})
		if err != nil {
			t.Fatalf("AdvanceTicket: %v", err)
		}
		if result.Ticket.Status != domain.TicketOpen {
			t.Fatalf("status=%q", result.Ticket.Status)
		}
	})

	t.Run("cancelado sin motivo", func(t *testing.T) {
		ticket := mk(true)
		version := claim(ticket)
		_, err := svc.AdvanceTicket(ctx, agent, ticket.ID, service.AdvanceInput{
			Version: &version, Status: strPtr(domain.TicketCancelled),
		})
		assertValidationCode(t, err, "")
	})

	t.Run("cancelado con motivo", func(t *testing.T) {
		ticket := mk(true)
		version := claim(ticket)
		result, err := svc.AdvanceTicket(ctx, agent, ticket.ID, service.AdvanceInput{
			Version: &version, Status: strPtr(domain.TicketCancelled), Comment: "ya no aplica",
		})
		if err != nil {
			t.Fatalf("AdvanceTicket: %v", err)
		}
		if result.Ticket.Status != domain.TicketCancelled {
			t.Fatalf("status=%q", result.Ticket.Status)
		}
	})

	t.Run("hecho es terminal", func(t *testing.T) {
		ticket := mk(false)
		version := claim(ticket)
		if _, err := svc.AdvanceTicket(ctx, agent, ticket.ID, service.AdvanceInput{
			Version: &version, Status: strPtr(domain.TicketDone), ClosingNote: validNote,
		}); err != nil {
			t.Fatalf("cerrar sin revisión: %v", err)
		}
		_, err := svc.AdvanceTicket(ctx, agent, ticket.ID, service.AdvanceInput{
			Status: strPtr(domain.TicketOpen), Comment: "reabrir",
		})
		assertConflictCode(t, err, "transicion_invalida")
	})

	t.Run("sin revisión permite en curso a hecho", func(t *testing.T) {
		ticket := mk(false)
		version := claim(ticket)
		result, err := svc.AdvanceTicket(ctx, agent, ticket.ID, service.AdvanceInput{
			Version: &version, Status: strPtr(domain.TicketDone), ClosingNote: validNote,
		})
		if err != nil {
			t.Fatalf("AdvanceTicket: %v", err)
		}
		if result.Ticket.Status != domain.TicketDone {
			t.Fatalf("status=%q", result.Ticket.Status)
		}
	})
}

func TestClosingEvidenceValidation(t *testing.T) {
	svc, _, _ := newTestService(t)
	human, _ := bootstrap(t, svc)
	project := createProjectForTickets(t, svc, human, "evidencia")
	agent := createAgentForTickets(t, svc, human, "worker-evidencia", project.Project.ID, domain.PermissionWriter, []string{"backend"})
	ctx := context.Background()

	newInProgress := func(t *testing.T) (service.TicketView, int64) {
		t.Helper()
		ticket, err := svc.CreateTicket(ctx, human, project.Project.ID, service.TicketInput{Title: "Evidencia"})
		if err != nil {
			t.Fatalf("CreateTicket: %v", err)
		}
		result, err := svc.ClaimTicket(ctx, agent, ticket.ID)
		if err != nil {
			t.Fatalf("ClaimTicket: %v", err)
		}
		return ticket, result.Ticket.Version
	}

	t.Run("nota vacía", func(t *testing.T) {
		ticket, version := newInProgress(t)
		_, err := svc.AdvanceTicket(ctx, agent, ticket.ID, service.AdvanceInput{
			Version: &version, Status: strPtr(domain.TicketInReview), CommitSHA: "abc1234",
		})
		assertValidationCode(t, err, "evidencia_incompleta")
	})

	t.Run("falta una sección", func(t *testing.T) {
		ticket, version := newInProgress(t)
		note := "Solución: x\nCambios: y\nVerificación: z\nArtefactos: abc1234"
		_, err := svc.AdvanceTicket(ctx, agent, ticket.ID, service.AdvanceInput{
			Version: &version, Status: strPtr(domain.TicketInReview), ClosingNote: note, CommitSHA: "abc1234",
		})
		assertValidationCode(t, err, "evidencia_incompleta")
		if !strings.Contains(err.Error(), "Riesgos") {
			t.Fatalf("el error debería nombrar la sección faltante: %v", err)
		}
	})

	t.Run("falta commit_sha", func(t *testing.T) {
		ticket, version := newInProgress(t)
		_, err := svc.AdvanceTicket(ctx, agent, ticket.ID, service.AdvanceInput{
			Version: &version, Status: strPtr(domain.TicketInReview), ClosingNote: validNote,
		})
		assertValidationCode(t, err, "evidencia_incompleta")
		if !strings.Contains(err.Error(), "commit_sha") {
			t.Fatalf("el error debería nombrar commit_sha: %v", err)
		}
	})

	t.Run("sha inválido", func(t *testing.T) {
		ticket, version := newInProgress(t)
		_, err := svc.AdvanceTicket(ctx, agent, ticket.ID, service.AdvanceInput{
			Version: &version, Status: strPtr(domain.TicketInReview), ClosingNote: validNote, CommitSHA: "no-es-sha",
		})
		assertValidationCode(t, err, "evidencia_incompleta")
	})

	t.Run("evidencia válida", func(t *testing.T) {
		ticket, version := newInProgress(t)
		result, err := svc.AdvanceTicket(ctx, agent, ticket.ID, service.AdvanceInput{
			Version: &version, Status: strPtr(domain.TicketInReview), ClosingNote: validNote, CommitSHA: "abc1234",
		})
		if err != nil {
			t.Fatalf("AdvanceTicket: %v", err)
		}
		if result.Ticket.CommitSHA != "abc1234" {
			t.Fatalf("commit_sha=%q", result.Ticket.CommitSHA)
		}
	})
}

func TestDependenciesWaitForReviewApproval(t *testing.T) {
	svc, _, _ := newTestService(t)
	human, _ := bootstrap(t, svc)
	project := createProjectForTickets(t, svc, human, "deps-revision")
	agent := createAgentForTickets(t, svc, human, "worker-deps", project.Project.ID, domain.PermissionWriter, []string{"backend"})
	ctx := context.Background()

	parent, err := svc.CreateTicket(ctx, human, project.Project.ID, service.TicketInput{Title: "Padre"})
	if err != nil {
		t.Fatalf("CreateTicket padre: %v", err)
	}
	child, err := svc.CreateTicket(ctx, human, project.Project.ID, service.TicketInput{
		Title: "Hijo", DependsOn: []string{parent.Ref},
	})
	if err != nil {
		t.Fatalf("CreateTicket hijo: %v", err)
	}
	claimed, err := svc.ClaimTicket(ctx, agent, parent.ID)
	if err != nil {
		t.Fatalf("ClaimTicket: %v", err)
	}
	version := claimed.Ticket.Version
	if _, err := svc.AdvanceTicket(ctx, agent, parent.ID, service.AdvanceInput{
		Version: &version, Status: strPtr(domain.TicketInReview), ClosingNote: validNote, CommitSHA: "abc1234",
	}); err != nil {
		t.Fatalf("AdvanceTicket: %v", err)
	}

	next, err := svc.TicketNext(ctx, agent, service.TicketNextInput{ProjectID: project.Project.ID})
	if err != nil {
		t.Fatalf("TicketNext: %v", err)
	}
	if len(next.Candidates) != 0 {
		t.Fatalf("el hijo no debe estar listo con el padre en revisión: %+v", next.Candidates)
	}
	if next.Motive != "esperando_dependencias" {
		t.Fatalf("motivo=%q", next.Motive)
	}

	if _, err := svc.ReviewTicket(ctx, human, parent.ID, service.ReviewInput{
		Veredicto: "aprobado", Comment: "ok",
	}); err != nil {
		t.Fatalf("ReviewTicket: %v", err)
	}
	next, err = svc.TicketNext(ctx, agent, service.TicketNextInput{ProjectID: project.Project.ID})
	if err != nil {
		t.Fatalf("TicketNext tras aprobar: %v", err)
	}
	if len(next.Candidates) != 1 || next.Candidates[0].ID != child.ID {
		t.Fatalf("el hijo debería estar listo: %+v", next.Candidates)
	}
}

func TestReviewApproveRejectAndEscalation(t *testing.T) {
	svc, _, database := newTestService(t)
	human, _ := bootstrap(t, svc)
	project := createProjectForTickets(t, svc, human, "revision")
	agent := createAgentForTickets(t, svc, human, "worker-revision", project.Project.ID, domain.PermissionWriter, []string{"backend"})
	ctx := context.Background()

	enterReview := func(t *testing.T, title string) service.TicketView {
		t.Helper()
		ticket, err := svc.CreateTicket(ctx, human, project.Project.ID, service.TicketInput{Title: title})
		if err != nil {
			t.Fatalf("CreateTicket: %v", err)
		}
		claimed, err := svc.ClaimTicket(ctx, agent, ticket.ID)
		if err != nil {
			t.Fatalf("ClaimTicket: %v", err)
		}
		version := claimed.Ticket.Version
		if _, err := svc.AdvanceTicket(ctx, agent, ticket.ID, service.AdvanceInput{
			Version: &version, Status: strPtr(domain.TicketInReview),
			ClosingNote: validNote, CommitSHA: "abc1234",
		}); err != nil {
			t.Fatalf("AdvanceTicket: %v", err)
		}
		return ticket
	}

	t.Run("aprobar", func(t *testing.T) {
		ticket := enterReview(t, "Aprobar")
		result, err := svc.ReviewTicket(ctx, human, ticket.ID, service.ReviewInput{
			Veredicto: "aprobado", Comment: "revisado",
		})
		if err != nil {
			t.Fatalf("ReviewTicket: %v", err)
		}
		if result.Ticket.Status != domain.TicketDone {
			t.Fatalf("status=%q", result.Ticket.Status)
		}
		comments, _, err := svc.ListComments(ctx, human, ticket.ID, 50, "")
		if err != nil {
			t.Fatalf("ListComments: %v", err)
		}
		found := false
		for _, comment := range comments {
			if comment.Type == domain.CommentReview {
				found = true
			}
		}
		if !found {
			t.Fatalf("falta comentario de revisión: %+v", comments)
		}
		var actions int
		if err := database.QueryRow(
			`SELECT COUNT(1) FROM audit_events WHERE object_id = ? AND action = 'ticket.revision.aprobar'`,
			ticket.ID,
		).Scan(&actions); err != nil {
			t.Fatalf("auditoría: %v", err)
		}
		if actions != 1 {
			t.Fatalf("eventos de aprobación=%d", actions)
		}
	})

	t.Run("rechazar hasta bloquear", func(t *testing.T) {
		ticket := enterReview(t, "Rechazar")
		result, err := svc.ReviewTicket(ctx, human, ticket.ID, service.ReviewInput{
			Veredicto: "rechazado", Comment: "falta test",
		})
		if err != nil {
			t.Fatalf("ReviewTicket rechazo 1: %v", err)
		}
		if result.Ticket.Status != domain.TicketOpen || result.Ticket.Reaperturas != 1 {
			t.Fatalf("tras rechazo 1: status=%q reaperturas=%d", result.Ticket.Status, result.Ticket.Reaperturas)
		}
		claimed, err := svc.ClaimTicket(ctx, agent, ticket.ID)
		if err != nil {
			t.Fatalf("ClaimTicket 2: %v", err)
		}
		version := claimed.Ticket.Version
		if _, err := svc.AdvanceTicket(ctx, agent, ticket.ID, service.AdvanceInput{
			Version: &version, Status: strPtr(domain.TicketInReview),
			ClosingNote: validNote, CommitSHA: "def5678",
		}); err != nil {
			t.Fatalf("AdvanceTicket 2: %v", err)
		}
		result, err = svc.ReviewTicket(ctx, human, ticket.ID, service.ReviewInput{
			Veredicto: "rechazado", Comment: "sigue faltando",
		})
		if err != nil {
			t.Fatalf("ReviewTicket rechazo 2: %v", err)
		}
		if result.Ticket.Status != domain.TicketBlocked || result.Ticket.Reaperturas != 2 {
			t.Fatalf("tras rechazo 2: status=%q reaperturas=%d", result.Ticket.Status, result.Ticket.Reaperturas)
		}
		var messages int
		if err := database.QueryRow(
			`SELECT COUNT(1) FROM messages WHERE project_id = ? AND type = 'solicitud' AND subject LIKE '%bloqueado%'`,
			project.Project.ID,
		).Scan(&messages); err != nil {
			t.Fatalf("consultar mensajes: %v", err)
		}
		if messages != 1 {
			t.Fatalf("solicitudes de escalado=%d", messages)
		}
	})
}

func TestReviewQueueDoesNotClaim(t *testing.T) {
	svc, _, database := newTestService(t)
	human, _ := bootstrap(t, svc)
	project := createProjectForTickets(t, svc, human, "cola-revision")
	agent := createAgentForTickets(t, svc, human, "worker-cola", project.Project.ID, domain.PermissionWriter, []string{"backend"})
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		ticket, err := svc.CreateTicket(ctx, human, project.Project.ID, service.TicketInput{Title: "Revisar"})
		if err != nil {
			t.Fatalf("CreateTicket: %v", err)
		}
		claimed, err := svc.ClaimTicket(ctx, agent, ticket.ID)
		if err != nil {
			t.Fatalf("ClaimTicket: %v", err)
		}
		version := claimed.Ticket.Version
		if _, err := svc.AdvanceTicket(ctx, agent, ticket.ID, service.AdvanceInput{
			Version: &version, Status: strPtr(domain.TicketInReview),
			ClosingNote: validNote, CommitSHA: "abc1234",
		}); err != nil {
			t.Fatalf("AdvanceTicket: %v", err)
		}
	}

	result, err := svc.TicketNext(ctx, agent, service.TicketNextInput{
		ProjectID: project.Project.ID, Status: domain.TicketInReview, Claim: true,
	})
	if err != nil {
		t.Fatalf("TicketNext revisión: %v", err)
	}
	if result.Ticket == nil || len(result.Candidates) != 2 {
		t.Fatalf("cola de revisión inesperada: %+v", result)
	}
	var claims int
	if err := database.QueryRow(
		`SELECT COUNT(1) FROM claims WHERE released_at IS NULL`,
	).Scan(&claims); err != nil {
		t.Fatalf("contar claims: %v", err)
	}
	if claims != 0 {
		t.Fatalf("la cola de revisión no debe reclamar: %d", claims)
	}
	if _, err := svc.TicketNext(ctx, agent, service.TicketNextInput{
		ProjectID: project.Project.ID, Status: "hecho",
	}); err == nil {
		t.Fatal("status hecho debería rechazarse en ticket_next")
	}
}
