package bot

import (
	"context"
	"io"
	"log"
	"testing"
	"time"

	"schedulebot/internal/model"
)

type schedulerEventStore struct {
	event model.Event
}

func (s *schedulerEventStore) Add(event model.Event) (model.Event, error) { return event, nil }
func (s *schedulerEventStore) ListAll() []model.Event                     { return []model.Event{s.event} }
func (s *schedulerEventStore) DeleteByID(string) error                    { return nil }
func (s *schedulerEventStore) Due(time.Time) []model.Event {
	if s.event.NotifiedAt != nil {
		return nil
	}
	return []model.Event{s.event}
}
func (s *schedulerEventStore) MarkDelivered(_ string, chatID int64) error {
	if !s.event.DeliveredTo(chatID) {
		s.event.DeliveredChatIDs = append(s.event.DeliveredChatIDs, chatID)
	}
	return nil
}
func (s *schedulerEventStore) MarkNotified(_ string, at time.Time) error {
	s.event.NotifiedAt = &at
	return nil
}

func TestSchedulerDeliversOnceToEveryConnectedChat(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	events := &schedulerEventStore{event: model.Event{
		ID: "event-1", Kind: model.Checkpoint, RKNumber: "РК1",
		SheetURL: "https://example.com/sheet", StartsAt: now.Add(time.Hour),
	}}
	auth := &authTestStore{
		owner: 100, target: -200, known: []int64{-100, -200}, delivery: []int64{-100, -200},
	}
	client := &authTestClient{}
	templates, err := LoadTemplates("../../templates")
	if err != nil {
		t.Fatal(err)
	}
	b := New(client, events, auth, templates, "password", time.UTC, log.New(io.Discard, "", 0))
	b.now = func() time.Time { return now }

	b.deliverDue(context.Background())
	if len(client.chatIDs) != 2 || client.chatIDs[0] != -100 || client.chatIDs[1] != -200 {
		t.Fatalf("notification recipients=%v, want [-100 -200]", client.chatIDs)
	}
	if events.event.NotifiedAt == nil || len(events.event.DeliveredChatIDs) != 2 {
		t.Fatalf("delivery was not completed: %+v", events.event)
	}
	b.deliverDue(context.Background())
	if len(client.chatIDs) != 2 {
		t.Fatalf("notification was sent twice: chats=%v", client.chatIDs)
	}
}

func TestUntilNextHour(t *testing.T) {
	now := time.Date(2026, 10, 4, 14, 24, 5, 0, time.FixedZone("Moscow", 3*60*60))
	want := 35*time.Minute + 55*time.Second
	if got := untilNextHour(now); got != want {
		t.Fatalf("until next hour=%s, want %s", got, want)
	}
}
