package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"schedulebot/internal/model"
)

var ErrNotFound = errors.New("event not found")

type JSON struct {
	mu     sync.Mutex
	path   string
	events []model.Event
}

func OpenJSON(path string) (*JSON, error) {
	s := &JSON{path: path, events: make([]model.Event, 0)}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) != 0 {
		if err := json.Unmarshal(data, &s.events); err != nil {
			return nil, fmt.Errorf("decode %s: %w", path, err)
		}
	}
	return s, nil
}

func (s *JSON) Add(event model.Event) (model.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if event.ID == "" {
		id, err := newID()
		if err != nil {
			return model.Event{}, err
		}
		event.ID = id
	}
	s.events = append(s.events, event)
	if err := s.saveLocked(); err != nil {
		s.events = s.events[:len(s.events)-1]
		return model.Event{}, err
	}
	return event, nil
}

func (s *JSON) ListByChat(chatID int64) []model.Event {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make([]model.Event, 0)
	for _, event := range s.events {
		if event.ChatID == chatID {
			result = append(result, event)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].StartsAt.Before(result[j].StartsAt) })
	return result
}

func (s *JSON) ListAll() []model.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := append([]model.Event(nil), s.events...)
	sort.Slice(result, func(i, j int) bool { return result[i].StartsAt.Before(result[j].StartsAt) })
	return result
}

func (s *JSON) Delete(chatID int64, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, event := range s.events {
		if event.ChatID != chatID || event.ID != id {
			continue
		}
		old := s.events
		s.events = append(append([]model.Event(nil), old[:i]...), old[i+1:]...)
		if err := s.saveLocked(); err != nil {
			s.events = old
			return err
		}
		return nil
	}
	return ErrNotFound
}

func (s *JSON) DeleteByID(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, event := range s.events {
		if event.ID != id {
			continue
		}
		old := s.events
		s.events = append(append([]model.Event(nil), old[:i]...), old[i+1:]...)
		if err := s.saveLocked(); err != nil {
			s.events = old
			return err
		}
		return nil
	}
	return ErrNotFound
}

// Due returns events whose notification time has passed but whose start time has not.
// This makes delivery resilient to restarts: a late notification is still sent.
func (s *JSON) Due(now time.Time) []model.Event {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make([]model.Event, 0)
	for _, event := range s.events {
		if event.NotifiedAt != nil || !event.StartsAt.After(now) || !event.CanNotifyAt(now) {
			continue
		}
		if !now.Before(event.NotificationAt()) {
			result = append(result, event)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].StartsAt.Before(result[j].StartsAt) })
	return result
}

func (s *JSON) MarkNotified(id string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.events {
		if s.events[i].ID == id {
			old := s.events[i].NotifiedAt
			s.events[i].NotifiedAt = &at
			if err := s.saveLocked(); err != nil {
				s.events[i].NotifiedAt = old
				return err
			}
			return nil
		}
	}
	return ErrNotFound
}

func (s *JSON) MarkDelivered(id string, chatID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.events {
		if s.events[i].ID != id {
			continue
		}
		if s.events[i].DeliveredTo(chatID) {
			return nil
		}
		old := append([]int64(nil), s.events[i].DeliveredChatIDs...)
		s.events[i].DeliveredChatIDs = append(s.events[i].DeliveredChatIDs, chatID)
		if err := s.saveLocked(); err != nil {
			s.events[i].DeliveredChatIDs = old
			return err
		}
		return nil
	}
	return ErrNotFound
}

func (s *JSON) saveLocked() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.events, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".events-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, s.path)
}

func newID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
