package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/joacomedel/agentbank-go/internal/service"
	"github.com/joacomedel/agentbank-go/internal/timefmt"
)

func TestChangesFeedAndUpdatedSince(t *testing.T) {
	svc, clock, _ := newTestService(t)
	human, _ := bootstrap(t, svc)
	project := createProjectForTickets(t, svc, human, "changes")

	first, err := svc.CreateTicket(context.Background(), human, project.Project.ID, service.TicketInput{
		Title: "Primero",
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	feed, err := svc.Changes(context.Background(), human, "", project.Project.ID, 100)
	if err != nil {
		t.Fatalf("Changes inicial: %v", err)
	}
	items, _ := feed["items"].([]service.ChangeItem)
	if len(items) == 0 {
		t.Fatalf("feed vacío: %+v", feed)
	}
	found := false
	for _, item := range items {
		if item.ObjectType == "ticket" && item.ObjectID == first.ID {
			found = true
			if item.Summary["ref"] == nil {
				t.Fatalf("cambio sin resumen: %+v", item)
			}
		}
	}
	if !found {
		t.Fatalf("no apareció el ticket en changes: %+v", items)
	}
	cursor, _ := feed["next_cursor"].(string)
	if cursor == "" {
		t.Fatal("changes sin next_cursor")
	}

	clock.Advance(time.Minute)
	second, err := svc.CreateTicket(context.Background(), human, project.Project.ID, service.TicketInput{
		Title: "Segundo",
	})
	if err != nil {
		t.Fatalf("CreateTicket segundo: %v", err)
	}
	feed, err = svc.Changes(context.Background(), human, cursor, project.Project.ID, 100)
	if err != nil {
		t.Fatalf("Changes incremental: %v", err)
	}
	items, _ = feed["items"].([]service.ChangeItem)
	if len(items) != 1 || items[0].ObjectID != second.ID {
		t.Fatalf("changes incremental inesperado: %+v", items)
	}

	since := clock.Now().Add(-time.Second)
	tickets, _, err := svc.ListTickets(context.Background(), human, project.Project.ID, service.TicketFilter{
		UpdatedSince: timefmt.Format(since),
	}, 100, "")
	if err != nil {
		t.Fatalf("ListTickets updated_since: %v", err)
	}
	if len(tickets) != 1 || tickets[0].ID != second.ID {
		t.Fatalf("updated_since tickets=%+v", tickets)
	}

	doc, err := svc.CreateDocument(context.Background(), human, service.DocumentInput{
		ProjectID: project.Project.ID, Title: "Doc cambios", Content: "uno",
	})
	if err != nil {
		t.Fatalf("CreateDocument: %v", err)
	}
	clock.Advance(time.Minute)
	if _, err := svc.CreateDocumentVersion(context.Background(), human, doc.ID, "dos"); err != nil {
		t.Fatalf("CreateDocumentVersion: %v", err)
	}
	docs, _, err := svc.ListDocuments(context.Background(), human, project.Project.ID, 100, "", timefmt.Format(clock.Now().Add(-time.Second)))
	if err != nil {
		t.Fatalf("ListDocuments updated_since: %v", err)
	}
	if len(docs) != 1 || docs[0].ID != doc.ID || docs[0].CurrentVersion != 2 {
		t.Fatalf("updated_since docs=%+v", docs)
	}
}
