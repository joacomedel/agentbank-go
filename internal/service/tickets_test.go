package service_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/joacomedel/agentbank-go/internal/audit"
	"github.com/joacomedel/agentbank-go/internal/domain"
	"github.com/joacomedel/agentbank-go/internal/service"
)

func createProjectForTickets(t *testing.T, svc *service.Service, human service.Identity, slug string) service.ProjectView {
	t.Helper()
	project, err := svc.CreateProject(context.Background(), human, service.ProjectInput{Slug: slug, Name: slug})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	return project
}

func createAgentForTickets(t *testing.T, svc *service.Service, human service.Identity, name, projectID string, permission string, roles []string) service.Identity {
	t.Helper()
	created, err := svc.CreateParticipant(context.Background(), human, service.ParticipantInput{
		Name: name, Type: domain.ParticipantAgent,
		Projects: []service.ParticipantProjectInput{{
			ProjectID: projectID, Permission: permission, Roles: roles,
		}},
	})
	if err != nil {
		t.Fatalf("CreateParticipant: %v", err)
	}
	identity, err := svc.Authenticate(context.Background(), created.Token)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	return identity
}

func TestTicketCRUDDependenciesAndTimeline(t *testing.T) {
	svc, _, database := newTestService(t)
	human, _ := bootstrap(t, svc)
	project := createProjectForTickets(t, svc, human, "tickets-crud")

	first, err := svc.CreateTicket(context.Background(), human, project.Project.ID, service.TicketInput{
		Title: "Primer ticket", Description: "Detalle", Priority: domain.PriorityHigh,
		Paths: []string{"internal/service"}, Key: "t-1",
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if first.Ref == "" || first.Number != 1 {
		t.Fatalf("ticket sin número/ref: %+v", first)
	}
	if first.EffectiveStatus != "listo" {
		t.Fatalf("effective_status=%q", first.EffectiveStatus)
	}
	second, err := svc.CreateTicket(context.Background(), human, project.Project.ID, service.TicketInput{
		Title: "Segundo ticket", DependsOn: []string{first.Ref},
	})
	if err != nil {
		t.Fatalf("CreateTicket segundo: %v", err)
	}
	if second.EffectiveStatus != "esperando_dependencias" {
		t.Fatalf("effective_status=%q, want esperando_dependencias", second.EffectiveStatus)
	}
	if _, err := svc.AddDependency(context.Background(), human, first.ID, second.Ref); err == nil {
		t.Fatal("se aceptó un ciclo")
	}
	var forbidden *service.ForbiddenError
	_ = forbidden

	version := first.Version
	if _, err := svc.UpdateTicket(context.Background(), human, first.Ref, service.TicketUpdate{
		Status: strPtr(domain.TicketDone), Version: &version,
	}); err != nil {
		t.Fatalf("UpdateTicket: %v", err)
	}
	got, err := svc.GetTicket(context.Background(), human, second.Ref)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if got.EffectiveStatus != "listo" {
		t.Fatalf("effective_status después de cerrar dep=%q", got.EffectiveStatus)
	}

	comment, err := svc.AddComment(context.Background(), human, first.Ref, "Listo, revisado", domain.CommentClose)
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if !comment.BodyContent.Untrusted || comment.BodyContent.Content != "Listo, revisado" {
		t.Fatalf("comentario sin D20: %+v", comment)
	}
	timeline, err := svc.Timeline(context.Background(), human, first.ID, 50)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	if len(timeline) < 3 {
		t.Fatalf("timeline demasiado corta: %d", len(timeline))
	}
	verify, err := audit.Verify(context.Background(), database, project.Project.ID)
	if err != nil || !verify.OK {
		t.Fatalf("cadena inválida: %+v err=%v", verify, err)
	}
}

func TestClaimsQueueAndSweep(t *testing.T) {
	svc, clock, database := newTestService(t)
	human, _ := bootstrap(t, svc)
	project := createProjectForTickets(t, svc, human, "cola")
	agent := createAgentForTickets(t, svc, human, "worker-1", project.Project.ID, domain.PermissionWriter, []string{"backend"})
	other := createAgentForTickets(t, svc, human, "worker-2", project.Project.ID, domain.PermissionWriter, []string{"backend"})

	ticketOne, err := svc.CreateTicket(context.Background(), human, project.Project.ID, service.TicketInput{
		Title: "Backend uno", AssignedRole: "backend", OrderIndex: 1,
	})
	if err != nil {
		t.Fatalf("CreateTicket uno: %v", err)
	}
	ticketTwo, err := svc.CreateTicket(context.Background(), human, project.Project.ID, service.TicketInput{
		Title: "Backend dos", AssignedRole: "backend", OrderIndex: 2,
	})
	if err != nil {
		t.Fatalf("CreateTicket dos: %v", err)
	}

	next, err := svc.TicketNext(context.Background(), agent, service.TicketNextInput{
		ProjectID: project.Project.ID, Role: "backend", Claim: true,
	})
	if err != nil {
		t.Fatalf("TicketNext: %v", err)
	}
	if next.Ticket == nil || next.Ticket.ID != ticketOne.ID {
		t.Fatalf("ticket siguiente inesperado: %+v", next)
	}
	if next.Ticket.Status != domain.TicketInProgress {
		t.Fatalf("status=%q", next.Ticket.Status)
	}
	if next.ClaimsActive != 1 {
		t.Fatalf("claims_activos=%d", next.ClaimsActive)
	}

	if _, err := svc.ClaimTicket(context.Background(), other, ticketOne.ID); err == nil {
		t.Fatal("se reclamó un ticket ya reclamado")
	}
	nextOther, err := svc.TicketNext(context.Background(), other, service.TicketNextInput{
		ProjectID: project.Project.ID, Role: "backend", Claim: true,
	})
	if err != nil {
		t.Fatalf("TicketNext other: %v", err)
	}
	if nextOther.Ticket == nil || nextOther.Ticket.ID != ticketTwo.ID {
		t.Fatalf("el segundo agente debía recibir el otro ticket: %+v", nextOther)
	}

	if _, err := svc.ReleaseClaim(context.Background(), other, ticketTwo.ID); err != nil {
		t.Fatalf("ReleaseClaim: %v", err)
	}
	released, err := svc.GetTicket(context.Background(), other, ticketTwo.ID)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if released.Status != domain.TicketOpen {
		t.Fatalf("status después de liberar=%q", released.Status)
	}

	clock.Advance(31 * time.Minute)
	if _, err := svc.ClaimTicket(context.Background(), other, ticketOne.ID); err != nil {
		// El claim de agent sigue activo; liberamos el de agent por vencimiento.
		_ = err
	}
	releasedCount, err := svc.SweepExpiredClaims(context.Background())
	if err != nil {
		t.Fatalf("SweepExpiredClaims: %v", err)
	}
	if releasedCount == 0 {
		t.Fatal("no se liberó ningún claim vencido")
	}
	expired, err := svc.GetTicket(context.Background(), human, ticketOne.ID)
	if err != nil {
		t.Fatalf("GetTicket expirado: %v", err)
	}
	if expired.Status != domain.TicketOpen {
		t.Fatalf("status expirado=%q, want abierto", expired.Status)
	}
	var expiredEvents int
	if err := database.QueryRow(`
		SELECT COUNT(1) FROM audit_events
		WHERE action = 'ticket.claim_expirado' AND object_id = ?`, ticketOne.ID,
	).Scan(&expiredEvents); err != nil {
		t.Fatalf("contar expirados: %v", err)
	}
	if expiredEvents == 0 {
		t.Fatal("no se auditó el claim expirado")
	}
}

func TestPhasesEpicsAndCloseWithPending(t *testing.T) {
	svc, _, database := newTestService(t)
	human, _ := bootstrap(t, svc)
	project := createProjectForTickets(t, svc, human, "estructura")

	phase, err := svc.CreatePhase(context.Background(), human, project.Project.ID, service.PhaseInput{
		Name: "Fase uno", Key: "f1", OrderIndex: int64Ptr(1),
	})
	if err != nil {
		t.Fatalf("CreatePhase: %v", err)
	}
	epic, err := svc.CreateEpic(context.Background(), human, project.Project.ID, service.EpicInput{
		Title: "Épica uno", Key: "e1", PhaseID: phase.ID,
	})
	if err != nil {
		t.Fatalf("CreateEpic: %v", err)
	}
	ticket, err := svc.CreateTicket(context.Background(), human, project.Project.ID, service.TicketInput{
		Title: "Ticket de épica", EpicID: epic.ID,
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if _, err := svc.CreateTicket(context.Background(), human, project.Project.ID, service.TicketInput{
		Title: "Ticket con épica y fase", EpicID: epic.ID, PhaseID: phase.ID,
	}); err == nil {
		t.Fatal("se aceptó ticket con épica y phase_id")
	}

	gotPhase, err := svc.GetPhase(context.Background(), human, phase.ID)
	if err != nil {
		t.Fatalf("GetPhase: %v", err)
	}
	if gotPhase.Progress.Total != 1 || len(gotPhase.Epics) != 1 {
		t.Fatalf("fase inesperada: %+v", gotPhase)
	}
	gotEpic, err := svc.GetEpic(context.Background(), human, epic.ID)
	if err != nil {
		t.Fatalf("GetEpic: %v", err)
	}
	if len(gotEpic.Tickets) != 1 || gotEpic.Tickets[0].ID != ticket.ID {
		t.Fatalf("épica sin tickets: %+v", gotEpic)
	}

	closed, err := svc.UpdatePhase(context.Background(), human, phase.ID, service.PhaseUpdate{
		Status: strPtr(domain.PhaseClosed),
	})
	if err != nil {
		t.Fatalf("cerrar fase: %v", err)
	}
	if closed.Status != domain.PhaseClosed {
		t.Fatalf("status fase=%q", closed.Status)
	}
	var closeEvents int
	if err := database.QueryRow(`
		SELECT COUNT(1) FROM audit_events
		WHERE action = 'fase.cerrar' AND object_id = ?`, phase.ID,
	).Scan(&closeEvents); err != nil {
		t.Fatalf("contar cierres: %v", err)
	}
	if closeEvents != 1 {
		t.Fatalf("eventos de cierre=%d", closeEvents)
	}
}

func strPtr(value string) *string { return &value }
func int64Ptr(value int64) *int64 { return &value }

func TestTicketNextConcurrentClaimsAreDistinct(t *testing.T) {
	svc, _, _ := newTestService(t)
	human, _ := bootstrap(t, svc)
	project := createProjectForTickets(t, svc, human, "concurrente")
	agentOne := createAgentForTickets(t, svc, human, "worker-a", project.Project.ID, domain.PermissionWriter, []string{"backend"})
	agentTwo := createAgentForTickets(t, svc, human, "worker-b", project.Project.ID, domain.PermissionWriter, []string{"backend"})
	for i := 0; i < 5; i++ {
		if _, err := svc.CreateTicket(context.Background(), human, project.Project.ID, service.TicketInput{
			Title: "Ticket concurrente", AssignedRole: "backend", OrderIndex: int64(i),
		}); err != nil {
			t.Fatalf("CreateTicket: %v", err)
		}
	}
	type result struct {
		id  string
		err error
	}
	results := make(chan result, 20)
	var wg sync.WaitGroup
	for _, agent := range []service.Identity{agentOne, agentTwo} {
		agent := agent
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				next, err := svc.TicketNext(context.Background(), agent, service.TicketNextInput{
					ProjectID: project.Project.ID, Role: "backend", Claim: true,
				})
				if err != nil {
					results <- result{err: err}
					return
				}
				if next.Ticket != nil {
					results <- result{id: next.Ticket.ID}
				}
			}
		}()
	}
	wg.Wait()
	close(results)
	seen := map[string]bool{}
	for item := range results {
		if item.err != nil {
			t.Fatalf("TicketNext concurrente: %v", item.err)
		}
		if item.id != "" {
			if seen[item.id] {
				t.Fatalf("ticket reclamado dos veces: %s", item.id)
			}
			seen[item.id] = true
		}
	}
	if len(seen) != 5 {
		t.Fatalf("claims distintos=%d, want 5", len(seen))
	}
}

func TestAdvanceTicketWithClosingNoteAndQuestion(t *testing.T) {
	svc, _, _ := newTestService(t)
	human, _ := bootstrap(t, svc)
	project := createProjectForTickets(t, svc, human, "avanzar")
	agent := createAgentForTickets(t, svc, human, "worker-avanzar", project.Project.ID, domain.PermissionWriter, []string{"backend"})
	ticket, err := svc.CreateTicket(context.Background(), human, project.Project.ID, service.TicketInput{
		Title: "Avanzar", AssignedRole: "backend",
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	claimed, err := svc.ClaimTicket(context.Background(), agent, ticket.ID)
	if err != nil {
		t.Fatalf("ClaimTicket: %v", err)
	}
	version := claimed.Ticket.Version
	result, err := svc.AdvanceTicket(context.Background(), agent, ticket.ID, service.AdvanceInput{
		Version: &version, Status: strPtr(domain.TicketDone), ClosingNote: "Terminado",
		Question: &service.AdvanceQuestion{
			Subject: "¿Revisar?", Body: "Quedó listo", Options: []string{"sí", "no"},
			DefaultOption: intPtr(0), Mode: "espero", Motivo: "consultar_siempre",
		},
	})
	if err != nil {
		t.Fatalf("AdvanceTicket: %v", err)
	}
	if result.Ticket.Status != domain.TicketDone {
		t.Fatalf("status=%q", result.Ticket.Status)
	}
	if result.Message == nil {
		t.Fatal("no se creó la pregunta")
	}
	comments, _, err := svc.ListComments(context.Background(), human, ticket.ID, 50, "")
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	foundClose := false
	for _, comment := range comments {
		if comment.Type == domain.CommentClose {
			foundClose = true
		}
	}
	if !foundClose {
		t.Fatal("no se guardó la nota de cierre")
	}
}

func TestTicketNextFiltersByEpicLane(t *testing.T) {
	svc, _, _ := newTestService(t)
	human, _ := bootstrap(t, svc)
	project := createProjectForTickets(t, svc, human, "carril-epica")
	agent := createAgentForTickets(t, svc, human, "worker-epic", project.Project.ID, domain.PermissionWriter, []string{"backend"})

	phase, err := svc.CreatePhase(context.Background(), human, project.Project.ID, service.PhaseInput{
		Name: "Fase carril", Key: "fc", OrderIndex: int64Ptr(1),
	})
	if err != nil {
		t.Fatalf("CreatePhase: %v", err)
	}
	epicA, err := svc.CreateEpic(context.Background(), human, project.Project.ID, service.EpicInput{
		Title: "Épica A", Key: "ea", PhaseID: phase.ID,
	})
	if err != nil {
		t.Fatalf("CreateEpic A: %v", err)
	}
	epicB, err := svc.CreateEpic(context.Background(), human, project.Project.ID, service.EpicInput{
		Title: "Épica B", Key: "eb", PhaseID: phase.ID,
	})
	if err != nil {
		t.Fatalf("CreateEpic B: %v", err)
	}
	ticketA, err := svc.CreateTicket(context.Background(), human, project.Project.ID, service.TicketInput{
		Title: "Ticket A", EpicID: epicA.ID, AssignedRole: "backend",
	})
	if err != nil {
		t.Fatalf("CreateTicket A: %v", err)
	}
	ticketB, err := svc.CreateTicket(context.Background(), human, project.Project.ID, service.TicketInput{
		Title: "Ticket B", EpicID: epicB.ID, AssignedRole: "backend",
	})
	if err != nil {
		t.Fatalf("CreateTicket B: %v", err)
	}

	nextA, err := svc.TicketNext(context.Background(), agent, service.TicketNextInput{
		ProjectID: project.Project.ID, EpicID: epicA.ID, Role: "backend", Claim: true,
	})
	if err != nil {
		t.Fatalf("TicketNext A: %v", err)
	}
	if nextA.Ticket == nil || nextA.Ticket.ID != ticketA.ID {
		t.Fatalf("carril A reclamó %+v", nextA.Ticket)
	}
	nextB, err := svc.TicketNext(context.Background(), agent, service.TicketNextInput{
		ProjectID: project.Project.ID, EpicID: epicB.ID, Role: "backend", Claim: true,
	})
	if err != nil {
		t.Fatalf("TicketNext B: %v", err)
	}
	if nextB.Ticket == nil || nextB.Ticket.ID != ticketB.ID {
		t.Fatalf("carril B reclamó %+v", nextB.Ticket)
	}
	empty, err := svc.TicketNext(context.Background(), agent, service.TicketNextInput{
		ProjectID: project.Project.ID, EpicID: epicA.ID, Role: "backend", Claim: true,
	})
	if err != nil {
		t.Fatalf("TicketNext A vacío: %v", err)
	}
	if empty.Ticket != nil {
		t.Fatalf("el carril A devolvió un ticket de otra épica: %+v", empty.Ticket)
	}
	if empty.Motive != "sin_candidatos" && empty.Motive != "reclamados" {
		t.Fatalf("motivo inesperado: %q", empty.Motive)
	}
}
