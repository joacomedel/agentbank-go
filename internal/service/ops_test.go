package service_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/joacomedel/agentbank-go/internal/service"
	"github.com/joacomedel/agentbank-go/internal/timefmt"
)

func TestImportDryRunAndCycles(t *testing.T) {
	svc, _, database := newTestService(t)
	human, _ := bootstrap(t, svc)
	project := createProjectForTickets(t, svc, human, "import")

	input := service.ImportInput{
		DryRun: true,
		Phases: []service.ImportPhase{{Key: "f1", Name: "Fase 1"}},
		Epics:  []service.ImportEpic{{Key: "e1", Title: "Épica 1", Phase: "f1"}},
		Tickets: []service.ImportTicket{
			{Key: "t1", Title: "Ticket 1", Epic: "e1", OrderIndex: int64Ptr(1)},
			{Key: "t2", Title: "Ticket 2", Priority: "alta", DependsOn: []string{"t1"}},
		},
	}
	summary, err := svc.ImportPlan(context.Background(), human, project.Project.ID, input)
	if err != nil {
		t.Fatalf("ImportPlan dry-run: %v", err)
	}
	if !summary.DryRun || summary.TicketsCreated != 2 {
		t.Fatalf("summary dry-run: %+v", summary)
	}
	var tickets int
	if err := database.QueryRow(`SELECT COUNT(1) FROM tickets`).Scan(&tickets); err != nil {
		t.Fatalf("contar tickets: %v", err)
	}
	if tickets != 0 {
		t.Fatalf("dry-run creó tickets: %d", tickets)
	}

	input.DryRun = false
	summary, err = svc.ImportPlan(context.Background(), human, project.Project.ID, input)
	if err != nil {
		t.Fatalf("ImportPlan: %v", err)
	}
	if summary.TicketsCreated != 2 || summary.PhasesCreated != 1 || summary.EpicsCreated != 1 {
		t.Fatalf("summary: %+v", summary)
	}
	summary, err = svc.ImportPlan(context.Background(), human, project.Project.ID, input)
	if err != nil {
		t.Fatalf("ImportPlan repetido: %v", err)
	}
	if summary.TicketsSkipped != 2 {
		t.Fatalf("skipped=%d", summary.TicketsSkipped)
	}
	if err := database.QueryRow(`SELECT COUNT(1) FROM tickets`).Scan(&tickets); err != nil {
		t.Fatalf("contar tickets: %v", err)
	}
	if tickets != 2 {
		t.Fatalf("el import duplicó tickets: %d", tickets)
	}
	cycle := service.ImportInput{Tickets: []service.ImportTicket{
		{Key: "a", Title: "A", DependsOn: []string{"b"}},
		{Key: "b", Title: "B", DependsOn: []string{"a"}},
	}}
	if _, err := svc.ImportPlan(context.Background(), human, project.Project.ID, cycle); err == nil {
		t.Fatal("el import aceptó un ciclo")
	}
}

func TestWebhookOutboxDelivery(t *testing.T) {
	svc, _, _ := newTestService(t)
	human, _ := bootstrap(t, svc)
	project := createProjectForTickets(t, svc, human, "webhooks")
	received := make(chan map[string]any, 1)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(raw, &payload)
		received <- payload
		w.WriteHeader(http.StatusOK)
	}))
	defer receiver.Close()
	if _, err := svc.CreateWebhook(context.Background(), human, project.Project.ID, service.WebhookInput{
		URL: receiver.URL, Secret: "s3creto", Events: []string{"ticket.crear"},
	}); err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}
	if _, err := svc.CreateTicket(context.Background(), human, project.Project.ID, service.TicketInput{
		Title: "Webhook ticket",
	}); err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	delivered, err := svc.DispatchOutbox(context.Background())
	if err != nil {
		t.Fatalf("DispatchOutbox: %v", err)
	}
	if delivered != 1 {
		t.Fatalf("delivered=%d", delivered)
	}
	select {
	case payload := <-received:
		if payload["event"] != "ticket.crear" {
			t.Fatalf("payload inesperado: %+v", payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("el webhook no llegó")
	}
	outbox, err := svc.ListOutbox(context.Background(), human, "entregado", 10)
	if err != nil {
		t.Fatalf("ListOutbox: %v", err)
	}
	if len(outbox) != 1 {
		t.Fatalf("outbox=%+v", outbox)
	}
}

func TestSecretScannerAndNovedades(t *testing.T) {
	svc, clock, _ := newTestService(t)
	human, _ := bootstrap(t, svc)
	project := createProjectForTickets(t, svc, human, "secretos")
	if _, err := svc.CreateMessage(context.Background(), human, service.MessageInput{
		ProjectID: project.Project.ID, Type: "info", DestinationType: "proyecto",
		Subject: "Token", Body: "ghp_abcdefghijklmnopqrstuvwxyz123456",
	}); err == nil {
		t.Fatal("se guardó un mensaje con un secreto")
	}
	if _, err := svc.CreateDocument(context.Background(), human, service.DocumentInput{
		ProjectID: project.Project.ID, Title: "Doc", Content: "sk-abcdefghijklmnopqrstuvwxyz",
	}); err == nil {
		t.Fatal("se guardó un documento con un secreto")
	}
	before := timefmt.Format(clock.Now().Add(-time.Minute))
	clock.Advance(time.Minute)
	if _, err := svc.CreateTicket(context.Background(), human, project.Project.ID, service.TicketInput{
		Title: "Novedad",
	}); err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	result, err := svc.Novedades(context.Background(), human, before, 0)
	if err != nil {
		t.Fatalf("Novedades: %v", err)
	}
	items, _ := result["items"].([]service.AuditEventView)
	if len(items) == 0 {
		t.Fatalf("sin novedades: %+v", result)
	}
	metrics, err := svc.Metrics(context.Background(), human)
	if err != nil {
		t.Fatalf("Metrics: %v", err)
	}
	if metrics["counts"] == nil {
		t.Fatalf("métricas inesperadas: %+v", metrics)
	}
	verify, err := svc.VerifyAll(context.Background(), human)
	if err != nil {
		t.Fatalf("VerifyAll: %v", err)
	}
	if verify["ok"] != true {
		t.Fatalf("verify-all: %+v", verify)
	}
}
