package model

import "time"

type EventKind string

const (
	Checkpoint EventKind = "checkpoint"
	PreDefense EventKind = "predefense"
	Defense    EventKind = "defense"
)

func (k EventKind) Valid() bool {
	switch k {
	case Checkpoint, PreDefense, Defense:
		return true
	default:
		return false
	}
}

type Event struct {
	ID               string     `json:"id"`
	ChatID           int64      `json:"chat_id"`
	Kind             EventKind  `json:"kind"`
	Title            string     `json:"title"`
	RKNumber         string     `json:"rk_number,omitempty"`
	SheetURL         string     `json:"sheet_url,omitempty"`
	StartsAt         time.Time  `json:"starts_at"`
	CreatedAt        time.Time  `json:"created_at"`
	NotifiedAt       *time.Time `json:"notified_at,omitempty"`
	DeliveredChatIDs []int64    `json:"delivered_chat_ids,omitempty"`
}

func (e Event) DeliveredTo(chatID int64) bool {
	for _, delivered := range e.DeliveredChatIDs {
		if delivered == chatID {
			return true
		}
	}
	return false
}

func (e Event) NotificationLeadTime() time.Duration {
	if e.Kind == Checkpoint {
		return 24 * time.Hour
	}
	return 48 * time.Hour
}

// NotificationAt returns the desired reminder time restricted to the
// 10:00–22:00 delivery window in the event's timezone.
func (e Event) NotificationAt() time.Time {
	desired := e.StartsAt.Add(-e.NotificationLeadTime())
	year, month, day := desired.Date()
	if desired.Hour() < 10 {
		return time.Date(year, month, day, 10, 0, 0, 0, desired.Location())
	}
	if desired.Hour() >= 22 {
		return time.Date(year, month, day, 10, 0, 0, 0, desired.Location()).AddDate(0, 0, 1)
	}
	return desired
}

func (e Event) CanNotifyAt(now time.Time) bool {
	local := now.In(e.StartsAt.Location())
	return local.Hour() >= 10 && local.Hour() < 22
}
