package service_test

import (
	"context"
	"testing"

	"github.com/joacomedel/agentbank-go/internal/domain"
	"github.com/joacomedel/agentbank-go/internal/service"
)

func TestDocumentsVersionsDiffSyncAndSearch(t *testing.T) {
	svc, _, _ := newTestService(t)
	human, _ := bootstrap(t, svc)
	project := createProjectForTickets(t, svc, human, "docs")
	doc, err := svc.CreateDocument(context.Background(), human, service.DocumentInput{
		ProjectID: project.Project.ID, Title: "Arquitectura", Route: "docs/arquitectura.md",
		Content: "# Arquitectura\n\nSQLite y Go.\n",
	})
	if err != nil {
		t.Fatalf("CreateDocument: %v", err)
	}
	if doc.CurrentVersion != 1 || doc.Hash == "" {
		t.Fatalf("documento inesperado: %+v", doc)
	}
	updated, err := svc.CreateDocumentVersion(context.Background(), human, doc.ID, "# Arquitectura\n\nSQLite, Go y WAL.\n")
	if err != nil {
		t.Fatalf("CreateDocumentVersion: %v", err)
	}
	if updated.Version != 2 {
		t.Fatalf("version=%d", updated.Version)
	}
	versions, err := svc.ListDocumentVersions(context.Background(), human, doc.ID)
	if err != nil {
		t.Fatalf("ListDocumentVersions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("versiones=%d", len(versions))
	}
	diff, err := svc.DiffDocument(context.Background(), human, doc.ID, 1, 2)
	if err != nil {
		t.Fatalf("DiffDocument: %v", err)
	}
	lines, _ := diff["lines"].([]service.DiffLine)
	if len(lines) == 0 {
		t.Fatalf("diff vacío: %+v", diff)
	}

	same, err := svc.SyncDocuments(context.Background(), human, project.Project.ID, []service.DocumentSyncInput{{
		Route: "docs/arquitectura.md", Content: "# Arquitectura\n\nSQLite, Go y WAL.\n",
	}})
	if err != nil {
		t.Fatalf("SyncDocuments same: %v", err)
	}
	if len(same) != 1 || same[0].Action != "sin_cambios" {
		t.Fatalf("sync same=%+v", same)
	}
	changed, err := svc.SyncDocuments(context.Background(), human, project.Project.ID, []service.DocumentSyncInput{{
		Route: "docs/arquitectura.md", Content: "# Arquitectura\n\nSQLite, Go, WAL y FTS.\n",
	}})
	if err != nil {
		t.Fatalf("SyncDocuments changed: %v", err)
	}
	if len(changed) != 1 || changed[0].Action != "actualizado" || changed[0].Version != 3 {
		t.Fatalf("sync changed=%+v", changed)
	}

	global, err := svc.CreateDocument(context.Background(), human, service.DocumentInput{
		Title: "Glosario global", Type: "glosario", Content: "# Glosario\n\nTérminos.",
	})
	if err != nil {
		t.Fatalf("CreateDocument global: %v", err)
	}
	agent := createAgentForTickets(t, svc, human, "agente-docs", project.Project.ID, domain.PermissionReader, nil)
	if _, err := svc.GetDocument(context.Background(), agent, global.ID, 0); err != nil {
		t.Fatalf("un agente debía leer docs globales: %v", err)
	}
	if _, err := svc.CreateDocument(context.Background(), agent, service.DocumentInput{Title: "Global", Content: "x"}); err == nil {
		t.Fatal("un agente no debía escribir docs globales")
	}

	ticket, err := svc.CreateTicket(context.Background(), human, project.Project.ID, service.TicketInput{
		Title: "Usar arquitectura", Description: "Ver docs de SQLite",
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if err := svc.AttachDocumentToTicket(context.Background(), human, ticket.ID, doc.ID); err != nil {
		t.Fatalf("AttachDocumentToTicket: %v", err)
	}
	documents, err := svc.ListTicketDocuments(context.Background(), human, ticket.ID)
	if err != nil {
		t.Fatalf("ListTicketDocuments: %v", err)
	}
	if len(documents) != 1 || documents[0].ID != doc.ID {
		t.Fatalf("documentos del ticket: %+v", documents)
	}
	message, err := svc.CreateMessage(context.Background(), human, service.MessageInput{
		ProjectID: project.Project.ID, Type: "info", DestinationType: "proyecto",
		Subject: "SQLite es la base", Body: "WAL y Go",
	})
	if err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	_ = message
	items, err := svc.Search(context.Background(), human, "SQLite", "ticket,document,message", project.Project.ID, 50)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("búsqueda sin resultados")
	}
}

func TestBriefingDigestAndTablero(t *testing.T) {
	svc, _, _ := newTestService(t)
	human, _ := bootstrap(t, svc)
	project := createProjectForTickets(t, svc, human, "briefing")
	phase, err := svc.CreatePhase(context.Background(), human, project.Project.ID, service.PhaseInput{Name: "Fase", Key: "f1"})
	if err != nil {
		t.Fatalf("CreatePhase: %v", err)
	}
	epic, err := svc.CreateEpic(context.Background(), human, project.Project.ID, service.EpicInput{Title: "Épica", Key: "e1", PhaseID: phase.ID})
	if err != nil {
		t.Fatalf("CreateEpic: %v", err)
	}
	if _, err := svc.CreateTicket(context.Background(), human, project.Project.ID, service.TicketInput{Title: "Ticket", EpicID: epic.ID}); err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	briefing, err := svc.Briefing(context.Background(), human, project.Project.ID)
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	if briefing.ActivePhaseID != phase.ID || len(briefing.Phases) != 1 {
		t.Fatalf("briefing inesperado: %+v", briefing)
	}
	if briefing.SizeBytes > 4096 {
		t.Fatalf("briefing demasiado grande: %d", briefing.SizeBytes)
	}
	digest, err := svc.Digest(context.Background(), human, project.Project.ID)
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	if digest["project_id"] != project.Project.ID {
		t.Fatalf("digest inesperado: %+v", digest)
	}
	board, err := svc.Tablero(context.Background(), human, project.Project.ID)
	if err != nil {
		t.Fatalf("Tablero: %v", err)
	}
	if board["project_id"] != project.Project.ID {
		t.Fatalf("tablero inesperado: %+v", board)
	}
}
