package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/joacomedel/agentbank-go/internal/domain"
	"github.com/joacomedel/agentbank-go/internal/service"
	"github.com/joacomedel/agentbank-go/internal/timefmt"
)

func TestMessagesInboxAffectsDefaultsAndCorrection(t *testing.T) {
	svc, clock, database := newTestService(t)
	human, _ := bootstrap(t, svc)
	project := createProjectForTickets(t, svc, human, "mensajes")
	agent := createAgentForTickets(t, svc, human, "agente-mensajes", project.Project.ID, domain.PermissionWriter, []string{"backend"})
	other := createAgentForTickets(t, svc, human, "agente-otro", project.Project.ID, domain.PermissionWriter, []string{"backend"})

	ticket, err := svc.CreateTicket(context.Background(), human, project.Project.ID, service.TicketInput{
		Title: "Ticket afectado",
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	message, err := svc.CreateMessage(context.Background(), human, service.MessageInput{
		ProjectID: project.Project.ID, Type: "solicitud", DestinationType: "rol",
		DestinationRole: "backend", Subject: "¿Qué opción?", Body: "Elegí una",
		Options: []string{"uno", "dos"}, DefaultOption: intPtr(0),
		Affects: []string{ticket.Ref},
	})
	if err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	if !message.BodyContent.Untrusted || message.Pending != true {
		t.Fatalf("mensaje inesperado: %+v", message)
	}
	gotTicket, err := svc.GetTicket(context.Background(), human, ticket.ID)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if gotTicket.EffectiveStatus != "esperando_respuesta" {
		t.Fatalf("effective_status=%q", gotTicket.EffectiveStatus)
	}

	inbox, _, err := svc.ListInbox(context.Background(), agent, project.Project.ID, true, 50, "")
	if err != nil {
		t.Fatalf("ListInbox: %v", err)
	}
	if len(inbox) != 1 || inbox[0].ID != message.ID {
		t.Fatalf("bandeja inesperada: %+v", inbox)
	}
	if _, err := svc.TakeMessage(context.Background(), agent, message.ID); err != nil {
		t.Fatalf("TakeMessage: %v", err)
	}
	if _, err := svc.TakeMessage(context.Background(), other, message.ID); err == nil {
		t.Fatal("otro participante tomó el mensaje")
	}
	resolved, err := svc.ResolveMessage(context.Background(), agent, message.ID, intPtr(1), "")
	if err != nil {
		t.Fatalf("ResolveMessage: %v", err)
	}
	if resolved.Resolution != "respondida" || resolved.ResponseOption == nil || *resolved.ResponseOption != 1 {
		t.Fatalf("resolución inesperada: %+v", resolved)
	}
	gotTicket, err = svc.GetTicket(context.Background(), human, ticket.ID)
	if err != nil {
		t.Fatalf("GetTicket post-resolución: %v", err)
	}
	if gotTicket.EffectiveStatus != "listo" {
		t.Fatalf("effective_status=%q, want listo", gotTicket.EffectiveStatus)
	}

	clock.Advance(time.Hour)
	defaultMessage, err := svc.CreateMessage(context.Background(), human, service.MessageInput{
		ProjectID: project.Project.ID, Type: "solicitud", DestinationType: "proyecto",
		Subject: "Default vencido", Body: "Elegí", Options: []string{"a", "b"},
		DefaultOption: intPtr(0), AssumeDefaultAt: timefmt.Format(clock.Now().Add(-time.Minute)),
		Affects: []string{ticket.ID},
	})
	if err != nil {
		t.Fatalf("CreateMessage default: %v", err)
	}
	assumed, err := svc.SweepMessageDefaults(context.Background())
	if err != nil {
		t.Fatalf("SweepMessageDefaults: %v", err)
	}
	if assumed != 1 {
		t.Fatalf("assumed=%d", assumed)
	}
	assumedMessage, err := svc.GetMessage(context.Background(), human, defaultMessage.ID)
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if assumedMessage.Resolution != "default_asumido" {
		t.Fatalf("resolution=%q", assumedMessage.Resolution)
	}
	if _, err := svc.ResolveMessage(context.Background(), agent, defaultMessage.ID, intPtr(1), ""); err != nil {
		t.Fatalf("resolver corrección: %v", err)
	}
	var corrections int
	if err := database.QueryRow(`SELECT COUNT(1) FROM tickets WHERE project_id = ? AND title LIKE 'Corregir:%'`, project.Project.ID).Scan(&corrections); err != nil {
		t.Fatalf("contar correcciones: %v", err)
	}
	if corrections != 1 {
		t.Fatalf("correcciones=%d, want 1", corrections)
	}
}

func TestMessageThreadConvertAndDiscard(t *testing.T) {
	svc, _, _ := newTestService(t)
	human, _ := bootstrap(t, svc)
	project := createProjectForTickets(t, svc, human, "hilos")

	root, err := svc.CreateMessage(context.Background(), human, service.MessageInput{
		ProjectID: project.Project.ID, Type: "info", DestinationType: "proyecto",
		Subject: "Contexto", Body: "Detalle",
	})
	if err != nil {
		t.Fatalf("CreateMessage raíz: %v", err)
	}
	reply, err := svc.CreateMessage(context.Background(), human, service.MessageInput{
		ProjectID: project.Project.ID, Type: "info", DestinationType: "proyecto",
		Subject: "Respuesta", Body: "Más detalle", ReplyTo: root.ID,
	})
	if err != nil {
		t.Fatalf("CreateMessage reply: %v", err)
	}
	if reply.CorrelationID != root.ID {
		t.Fatalf("correlation_id=%q, want root", reply.CorrelationID)
	}
	decision, err := svc.ConvertMessage(context.Background(), human, root.ID, "decision", "Decisión", "Cuerpo")
	if err != nil {
		t.Fatalf("ConvertMessage decision: %v", err)
	}
	if decision["decision_id"] == "" {
		t.Fatalf("decisión inesperada: %+v", decision)
	}
	ticket, err := svc.ConvertMessage(context.Background(), human, root.ID, "ticket", "Ticket desde mensaje", "")
	if err != nil {
		t.Fatalf("ConvertMessage ticket: %v", err)
	}
	if ticket["ticket_id"] == "" {
		t.Fatalf("ticket inesperado: %+v", ticket)
	}
	if _, err := svc.DiscardMessage(context.Background(), human, reply.ID); err != nil {
		t.Fatalf("DiscardMessage: %v", err)
	}
	if _, err := svc.ResolveMessage(context.Background(), human, reply.ID, nil, "texto"); err == nil {
		t.Fatal("se resolvió un mensaje descartado")
	}
}

func intPtr(value int) *int { return &value }
