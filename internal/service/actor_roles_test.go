package service_test

import (
	"context"
	"testing"

	"github.com/joacomedel/agentbank-go/internal/domain"
	"github.com/joacomedel/agentbank-go/internal/service"
)

func TestActorRoleSnapshotsAcrossEntities(t *testing.T) {
	svc, _, _ := newTestService(t)
	human, _ := bootstrap(t, svc)

	created, err := svc.CreateParticipant(context.Background(), human, service.ParticipantInput{
		Name: "auditor-todo", Type: domain.ParticipantAgent, AgentRole: "auditor", Admin: true,
	})
	if err != nil {
		t.Fatalf("CreateParticipant: %v", err)
	}
	agent, err := svc.Authenticate(context.Background(), created.Token)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	sessionID, err := svc.OpenSession(context.Background(), agent, "", "test")
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	agent.SessionID = sessionID

	project, err := svc.CreateProject(context.Background(), agent, service.ProjectInput{
		Slug: "roles-todo", Name: "Roles todo",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if project.Project.CreatedByRole != "auditor" || project.Project.CreatedBy != agent.Participant.ID {
		t.Fatalf("proyecto sin rol de creador: %+v", project.Project)
	}

	phase, err := svc.CreatePhase(context.Background(), agent, project.Project.ID, service.PhaseInput{
		Name: "Fase roles", Key: "fr", OrderIndex: int64Ptr(1),
	})
	if err != nil {
		t.Fatalf("CreatePhase: %v", err)
	}
	if phase.CreatedByRole != "auditor" {
		t.Fatalf("fase sin created_by_role: %+v", phase)
	}

	epic, err := svc.CreateEpic(context.Background(), agent, project.Project.ID, service.EpicInput{
		Title: "Épica roles", Key: "er", PhaseID: phase.ID,
	})
	if err != nil {
		t.Fatalf("CreateEpic: %v", err)
	}
	if epic.CreatedByRole != "auditor" {
		t.Fatalf("épica sin created_by_role: %+v", epic)
	}

	ticket, err := svc.CreateTicket(context.Background(), agent, project.Project.ID, service.TicketInput{
		Title: "Ticket con roles", EpicID: epic.ID,
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if ticket.CreatedByRole != "auditor" || ticket.UpdatedByRole != "auditor" {
		t.Fatalf("ticket sin roles: %+v", ticket)
	}

	comment, err := svc.AddComment(context.Background(), agent, ticket.ID, "Comentario del auditor", domain.CommentNota)
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if comment.AuthorRole != "auditor" {
		t.Fatalf("comentario sin author_role: %+v", comment)
	}

	if _, err := svc.ClaimTicket(context.Background(), agent, ticket.ID); err != nil {
		t.Fatalf("ClaimTicket: %v", err)
	}
	advanced, err := svc.AdvanceTicket(context.Background(), agent, ticket.ID, service.AdvanceInput{
		Status:      strPtr(domain.TicketInReview),
		ClosingNote: "Solución: probada\nCambios: ninguno\nVerificación: go test\nArtefactos: abc1234\nRiesgos: ninguno",
		CommitSHA:   "abc1234",
	})
	if err != nil {
		t.Fatalf("AdvanceTicket: %v", err)
	}
	if advanced.Ticket.UpdatedByRole != "auditor" {
		t.Fatalf("ticket en revisión sin updated_by_role: %+v", advanced.Ticket)
	}
	reviewed, err := svc.ReviewTicket(context.Background(), agent, ticket.ID, service.ReviewInput{
		Veredicto: "aprobado", Comment: "ok",
	})
	if err != nil {
		t.Fatalf("ReviewTicket: %v", err)
	}
	if reviewed.Ticket.UpdatedByRole != "auditor" {
		t.Fatalf("ticket revisado sin updated_by_role: %+v", reviewed.Ticket)
	}
	timeline, err := svc.Timeline(context.Background(), agent, ticket.ID, 50)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	closingRole := false
	for _, item := range timeline {
		if item.Type == "comentario" && item.ActorRole == "auditor" {
			closingRole = true
		}
	}
	if !closingRole {
		t.Fatalf("timeline sin rol en comentarios: %+v", timeline)
	}

	message, err := svc.CreateMessage(context.Background(), agent, service.MessageInput{
		ProjectID: project.Project.ID, Type: "solicitud", DestinationType: "proyecto",
		Subject: "Pregunta del auditor", Body: "¿Seguimos?", Options: []string{"sí", "no"},
	})
	if err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	if message.FromRole != "auditor" {
		t.Fatalf("mensaje sin from_role: %+v", message)
	}
	option := 0
	resolved, err := svc.ResolveMessage(context.Background(), agent, message.ID, &option, "")
	if err != nil {
		t.Fatalf("ResolveMessage: %v", err)
	}
	if resolved.ResolvedByRole != "auditor" {
		t.Fatalf("mensaje sin resolved_by_role: %+v", resolved)
	}

	doc, err := svc.CreateDocument(context.Background(), agent, service.DocumentInput{
		ProjectID: project.Project.ID, Title: "Doc roles", Route: "doc-roles", Content: "v1",
	})
	if err != nil {
		t.Fatalf("CreateDocument: %v", err)
	}
	if doc.CreatedByRole != "auditor" || doc.AuthorRole != "auditor" {
		t.Fatalf("documento sin roles: %+v", doc)
	}
	docV2, err := svc.CreateDocumentVersion(context.Background(), agent, doc.ID, "v2")
	if err != nil {
		t.Fatalf("CreateDocumentVersion: %v", err)
	}
	if docV2.AuthorRole != "auditor" {
		t.Fatalf("versión sin author_role: %+v", docV2)
	}

	decision, err := svc.CreateDecision(context.Background(), agent, project.Project.ID, "ADR roles", "cuerpo", "")
	if err != nil {
		t.Fatalf("CreateDecision: %v", err)
	}
	if decision["created_by_role"] != "auditor" {
		t.Fatalf("decisión sin created_by_role: %+v", decision)
	}

	webhook, err := svc.CreateWebhook(context.Background(), agent, project.Project.ID, service.WebhookInput{
		URL: "http://127.0.0.1:9/hook",
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}
	if webhook.CreatedByRole != "auditor" {
		t.Fatalf("webhook sin created_by_role: %+v", webhook)
	}

	claimTicket, err := svc.CreateTicket(context.Background(), agent, project.Project.ID, service.TicketInput{
		Title: "Ticket para claim",
	})
	if err != nil {
		t.Fatalf("CreateTicket claim: %v", err)
	}
	next, err := svc.TicketNext(context.Background(), agent, service.TicketNextInput{
		ProjectID: project.Project.ID, Claim: true,
	})
	if err != nil {
		t.Fatalf("TicketNext: %v", err)
	}
	if next.Ticket == nil || next.Ticket.ID != claimTicket.ID {
		t.Fatalf("claim inesperado: %+v", next)
	}
	if len(next.Ticket.Claims) != 1 || next.Ticket.Claims[0].CreatedByRole != "auditor" {
		t.Fatalf("claim sin created_by_role: %+v", next.Ticket.Claims)
	}
}
