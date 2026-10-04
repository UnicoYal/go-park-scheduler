package bot

import (
	"strings"
	"testing"
	"time"

	"schedulebot/internal/model"
)

func TestParseAddSupportsLongTermSchedule(t *testing.T) {
	loc := time.FixedZone("test", 3*60*60)
	now := time.Date(2026, 1, 1, 9, 0, 0, 0, loc)
	event, err := parseAdd("checkpoint 2027-04-20 10:30 РК1 https://example.com/sheet", -100, now, loc)
	if err != nil {
		t.Fatal(err)
	}
	if event.Kind != model.Checkpoint || event.RKNumber != "РК1" || event.SheetURL != "https://example.com/sheet" || event.ChatID != -100 {
		t.Fatalf("unexpected event: %+v", event)
	}
	if got := event.StartsAt.Format(dateLayout); got != "2027-04-20 10:30" {
		t.Fatalf("unexpected date: %s", got)
	}
}

func TestParseAddRejectsPastAndUnknownTemplate(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 1, 2, 0, 0, 0, 0, loc)
	for _, args := range []string{
		"lesson 2027-01-01 10:00",
		"defense 2026-01-01 10:00",
		"checkpoint bad-date 10:00",
		"checkpoint 2027-01-01 10:00 РК1",
		"checkpoint 2027-01-01 10:00 РК1 not-a-link",
		"defense 2027-01-01 10:00 лишний-параметр",
	} {
		if _, err := parseAdd(args, 1, now, loc); err == nil {
			t.Fatalf("expected error for %q", args)
		}
	}
}

func TestNotificationTemplates(t *testing.T) {
	templates, err := LoadTemplates("../../templates")
	if err != nil {
		t.Fatal(err)
	}
	starts := time.Date(2027, 3, 4, 12, 30, 0, 0, time.UTC)
	for _, tc := range []struct {
		kind model.EventKind
		want string
	}{
		{model.Checkpoint, "РК2"},
		{model.PreDefense, "предзащита проектов"},
		{model.Defense, "защита проектов"},
	} {
		event := model.Event{Kind: tc.kind, StartsAt: starts, RKNumber: "РК2", SheetURL: "https://example.com/sheet"}
		got, err := templates.Render(event, time.UTC)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, tc.want) || !strings.Contains(got, "4 марта") || !strings.Contains(got, "12:30") {
			t.Errorf("template %s: %q", tc.kind, got)
		}
		if tc.kind == model.Checkpoint && (!strings.Contains(got, "РК2") || !strings.Contains(got, event.SheetURL)) {
			t.Errorf("checkpoint substitutions missing: %q", got)
		}
	}
}

func TestNotificationLeadTimes(t *testing.T) {
	for _, tc := range []struct {
		kind model.EventKind
		want time.Duration
	}{
		{model.Checkpoint, 24 * time.Hour},
		{model.PreDefense, 48 * time.Hour},
		{model.Defense, 48 * time.Hour},
	} {
		event := model.Event{Kind: tc.kind}
		if got := event.NotificationLeadTime(); got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.kind, got, tc.want)
		}
	}
}

func TestNotificationTimeIsClampedToDeliveryWindow(t *testing.T) {
	loc := time.FixedZone("Moscow", 3*60*60)
	tests := []struct {
		name   string
		kind   model.EventKind
		starts string
		want   string
	}{
		{"before opening", model.Checkpoint, "2026-10-07 08:00", "2026-10-06 10:00"},
		{"inside window", model.Checkpoint, "2026-10-07 18:00", "2026-10-06 18:00"},
		{"at closing", model.Checkpoint, "2026-10-07 22:00", "2026-10-07 10:00"},
		{"after closing", model.Checkpoint, "2026-10-07 23:30", "2026-10-07 10:00"},
		{"two day reminder", model.PreDefense, "2026-11-14 11:00", "2026-11-12 11:00"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			starts, err := time.ParseInLocation(dateLayout, test.starts, loc)
			if err != nil {
				t.Fatal(err)
			}
			event := model.Event{Kind: test.kind, StartsAt: starts}
			if got := event.NotificationAt().Format(dateLayout); got != test.want {
				t.Fatalf("notification=%s, want %s", got, test.want)
			}
		})
	}
}

func TestSplitCommandRemovesBotName(t *testing.T) {
	cmd, args := splitCommand(" /ADD@my_bot   defense 2027-01-01 10:00 ")
	if cmd != "/add" || args != "defense 2027-01-01 10:00" {
		t.Fatalf("got %q %q", cmd, args)
	}
}
