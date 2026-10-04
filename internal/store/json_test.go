package store

import (
	"path/filepath"
	"testing"
	"time"

	"schedulebot/internal/model"
)

func TestJSONStorePersistsAndReturnsDueEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "events.json")
	s, err := OpenJSON(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	due, err := s.Add(model.Event{ChatID: 1, Kind: model.Defense, StartsAt: now.Add(23 * time.Hour), CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(model.Event{ChatID: 1, Kind: model.Checkpoint, StartsAt: now.Add(25 * time.Hour), CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if got := s.Due(now); len(got) != 1 || got[0].ID != due.ID {
		t.Fatalf("unexpected due events: %+v", got)
	}
	if err := s.MarkNotified(due.ID, now); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenJSON(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.ListByChat(1); len(got) != 2 || got[0].NotifiedAt == nil {
		t.Fatalf("events were not persisted: %+v", got)
	}
	if got := reopened.Due(now); len(got) != 0 {
		t.Fatalf("marked event is still due: %+v", got)
	}
}

func TestDeleteIsScopedToChat(t *testing.T) {
	s, err := OpenJSON(filepath.Join(t.TempDir(), "events.json"))
	if err != nil {
		t.Fatal(err)
	}
	event, err := s.Add(model.Event{ChatID: 10, StartsAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(20, event.ID); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := s.Delete(10, event.ID); err != nil {
		t.Fatal(err)
	}
}

func TestDueUsesLeadTimeForEachEventKind(t *testing.T) {
	s, err := OpenJSON(filepath.Join(t.TempDir(), "events.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		kind  model.EventKind
		after time.Duration
		due   bool
	}{
		{model.Checkpoint, 23 * time.Hour, true},
		{model.Checkpoint, 25 * time.Hour, false},
		{model.PreDefense, 47 * time.Hour, true},
		{model.PreDefense, 49 * time.Hour, false},
		{model.Defense, 48 * time.Hour, true},
		{model.Defense, 49 * time.Hour, false},
	}
	for _, test := range tests {
		if _, err := s.Add(model.Event{ChatID: 1, Kind: test.kind, StartsAt: now.Add(test.after), CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	due := s.Due(now)
	if len(due) != 3 {
		t.Fatalf("got %d due events, want 3: %+v", len(due), due)
	}
	if due[0].Kind != model.Checkpoint || due[1].Kind != model.PreDefense || due[2].Kind != model.Defense {
		t.Fatalf("unexpected due events: %+v", due)
	}
}

func TestDeliveryProgressIsPersistedPerChat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.json")
	s, err := OpenJSON(path)
	if err != nil {
		t.Fatal(err)
	}
	event, err := s.Add(model.Event{Kind: model.Checkpoint, StartsAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkDelivered(event.ID, -100); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkDelivered(event.ID, -100); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenJSON(path)
	if err != nil {
		t.Fatal(err)
	}
	events := reopened.ListAll()
	if len(events) != 1 || len(events[0].DeliveredChatIDs) != 1 || !events[0].DeliveredTo(-100) {
		t.Fatalf("delivery progress was not persisted: %+v", events)
	}
	if err := reopened.DeleteByID(event.ID); err != nil {
		t.Fatal(err)
	}
	if len(reopened.ListAll()) != 0 {
		t.Fatal("event was not deleted globally")
	}
}

func TestDueWaitsForDeliveryWindow(t *testing.T) {
	s, err := OpenJSON(filepath.Join(t.TempDir(), "events.json"))
	if err != nil {
		t.Fatal(err)
	}
	loc := time.FixedZone("Moscow", 3*60*60)
	event := model.Event{
		Kind: model.Checkpoint,
		// The regular reminder was due at 12:00, but the bot first checks at 23:00.
		StartsAt: time.Date(2026, 10, 5, 12, 0, 0, 0, loc),
	}
	if _, err := s.Add(event); err != nil {
		t.Fatal(err)
	}
	lateNight := time.Date(2026, 10, 4, 23, 0, 0, 0, loc)
	if due := s.Due(lateNight); len(due) != 0 {
		t.Fatalf("notification became due outside window: %+v", due)
	}
	nextMorning := time.Date(2026, 10, 5, 10, 0, 0, 0, loc)
	if due := s.Due(nextMorning); len(due) != 1 {
		t.Fatalf("notification did not become due at 10:00: %+v", due)
	}
}
