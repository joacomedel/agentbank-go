package audit

import (
	"testing"
	"time"
)

func sampleEvent() Event {
	return Event{
		ID:         "11111111-1111-4111-8111-111111111111",
		ChainID:    "system",
		Seq:        1,
		ActorType:  "humano",
		ActorID:    "22222222-2222-4222-8222-222222222222",
		Action:     "proyecto.crear",
		ObjectType: "proyecto",
		ObjectID:   "33333333-3333-4333-8333-333333333333",
		ProjectID:  "33333333-3333-4333-8333-333333333333",
		Payload:    `{"slug":"demo"}`,
		PrevHash:   "",
		CreatedAt:  time.Date(2026, 11, 7, 20, 0, 0, 123, time.UTC),
	}
}

func TestHashEventIsStable(t *testing.T) {
	event := sampleEvent()
	first := HashEvent(event)
	second := HashEvent(event)
	if first == "" || first != second {
		t.Fatalf("hash inestable: %q vs %q", first, second)
	}
	if len(first) != 64 {
		t.Fatalf("hash debe ser sha256 hex: %q", first)
	}
}

func TestHashEventCoversFields(t *testing.T) {
	base := sampleEvent()
	baseHash := HashEvent(base)

	mutations := []func(*Event){
		func(e *Event) { e.Seq++ },
		func(e *Event) { e.ChainID = "otra" },
		func(e *Event) { e.ActorID = "otro" },
		func(e *Event) { e.Action = "otra.accion" },
		func(e *Event) { e.ObjectType = "otro" },
		func(e *Event) { e.ObjectID = "otro" },
		func(e *Event) { e.ProjectID = "otro" },
		func(e *Event) { e.Payload = `{"slug":"otro"}` },
		func(e *Event) { e.PrevHash = "previo" },
		func(e *Event) { e.CreatedAt = e.CreatedAt.Add(time.Nanosecond) },
	}
	for i, mutate := range mutations {
		changed := base
		mutate(&changed)
		if HashEvent(changed) == baseHash {
			t.Fatalf("mutación %d no cambió el hash", i)
		}
	}
}

func TestFieldEncodingAvoidsAmbiguity(t *testing.T) {
	if field("ab")+"|"+field("c") == field("a")+"|"+field("bc") {
		t.Fatal("la codificación con longitud es ambigua")
	}
}
