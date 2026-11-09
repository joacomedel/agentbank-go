package service_test

import (
	"context"
	"testing"

	"github.com/joacomedel/agentbank-go/internal/domain"
	"github.com/joacomedel/agentbank-go/internal/service"
)

func TestAgentRoleSnapshotAndParticipantFilters(t *testing.T) {
	svc, _, database := newTestService(t)
	human, _ := bootstrap(t, svc)
	project := createProjectForTickets(t, svc, human, "roles-agente")
	created, err := svc.CreateParticipant(context.Background(), human, service.ParticipantInput{
		Name: "auditor-1", Type: domain.ParticipantAgent, AgentRole: "auditor",
		Projects: []service.ParticipantProjectInput{{
			ProjectID: project.Project.ID, Permission: domain.PermissionWriter, Roles: []string{"auditor"},
		}},
	})
	if err != nil {
		t.Fatalf("CreateParticipant: %v", err)
	}
	agent, err := svc.Authenticate(context.Background(), created.Token)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if agent.Participant.AgentRole != "auditor" {
		t.Fatalf("agent_role=%q", agent.Participant.AgentRole)
	}
	agent.SessionID = "ses_agent_test"
	ticket, err := svc.CreateTicket(context.Background(), agent, project.Project.ID, service.TicketInput{
		Title: "Ticket del auditor",
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if ticket.CreatedByRole != "auditor" || ticket.CreatedBy != agent.Participant.ID {
		t.Fatalf("autoría inesperada: created_by=%q role=%q", ticket.CreatedBy, ticket.CreatedByRole)
	}
	var actorRole string
	if err := database.QueryRow(`
		SELECT actor_role FROM audit_events
		WHERE action = 'ticket.crear' AND object_id = ?`, ticket.ID,
	).Scan(&actorRole); err != nil {
		t.Fatalf("leer actor_role: %v", err)
	}
	if actorRole != "auditor" {
		t.Fatalf("actor_role=%q", actorRole)
	}
	timeline, err := svc.Timeline(context.Background(), human, ticket.ID, 50)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	foundRole := false
	for _, item := range timeline {
		if item.Action == "ticket.crear" && item.ActorRole == "auditor" && item.SessionID != "" {
			foundRole = true
			break
		}
	}
	if !foundRole {
		t.Fatalf("timeline sin actor_role/session_id del auditor: %+v", timeline)
	}
	list, _, err := svc.ListParticipants(context.Background(), 50, "", false, "agente", "auditor", "auditor-1")
	if err != nil {
		t.Fatalf("ListParticipants: %v", err)
	}
	if len(list) != 1 || list[0].AgentRole != "auditor" {
		t.Fatalf("filtro de participantes inesperado: %+v", list)
	}

	if _, err := svc.UpdateParticipant(context.Background(), human, created.Participant.ID, "orquestador"); err != nil {
		t.Fatalf("UpdateParticipant: %v", err)
	}
	updated, err := svc.Authenticate(context.Background(), created.Token)
	if err != nil {
		t.Fatalf("Authenticate post-update: %v", err)
	}
	if updated.Participant.AgentRole != "orquestador" {
		t.Fatalf("agent_role post-update=%q", updated.Participant.AgentRole)
	}
	oldTicket, err := svc.GetTicket(context.Background(), human, ticket.ID)
	if err != nil {
		t.Fatalf("GetTicket viejo: %v", err)
	}
	if oldTicket.CreatedByRole != "auditor" {
		t.Fatalf("el ticket viejo cambió de rol: %q", oldTicket.CreatedByRole)
	}
	newTicket, err := svc.CreateTicket(context.Background(), updated, project.Project.ID, service.TicketInput{
		Title: "Ticket del orquestador",
	})
	if err != nil {
		t.Fatalf("CreateTicket nuevo: %v", err)
	}
	if newTicket.CreatedByRole != "orquestador" {
		t.Fatalf("created_by_role nuevo=%q", newTicket.CreatedByRole)
	}

	if err := svc.RevokeParticipantToken(context.Background(), human, created.Participant.ID); err != nil {
		t.Fatalf("RevokeParticipantToken: %v", err)
	}
	if _, err := svc.Authenticate(context.Background(), created.Token); err == nil {
		t.Fatal("el token revocado seguía válido")
	}
}
