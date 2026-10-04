package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type Store struct {
	mu              sync.Mutex
	path            string
	ownerUserID     int64
	targetChatID    int64
	knownChatIDs    []int64
	deliveryChatIDs []int64
}

type fileData struct {
	Version         int     `json:"version,omitempty"`
	OwnerUserID     int64   `json:"owner_user_id"`
	TargetChatID    int64   `json:"target_chat_id,omitempty"`
	KnownChatIDs    []int64 `json:"known_chat_ids,omitempty"`
	DeliveryChatIDs []int64 `json:"delivery_chat_ids,omitempty"`
}

func Open(path string) (*Store, error) {
	s := &Store{path: path}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var saved fileData
	if err := json.Unmarshal(data, &saved); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	if saved.OwnerUserID < 0 {
		return nil, fmt.Errorf("invalid owner user id in %s", path)
	}
	s.ownerUserID = saved.OwnerUserID
	s.targetChatID = saved.TargetChatID
	for _, chatID := range saved.KnownChatIDs {
		if chatID != 0 && !contains(s.knownChatIDs, chatID) {
			s.knownChatIDs = append(s.knownChatIDs, chatID)
		}
	}
	deliveryChatIDs := saved.DeliveryChatIDs
	if saved.Version < 2 {
		// Version 1 tracked every owner-connected chat as known, but only kept the
		// latest one as the delivery target. Upgrade all of them to recipients.
		deliveryChatIDs = saved.KnownChatIDs
	}
	for _, chatID := range deliveryChatIDs {
		if chatID != 0 && !contains(s.deliveryChatIDs, chatID) {
			s.deliveryChatIDs = append(s.deliveryChatIDs, chatID)
		}
	}
	// Backward compatibility for a target saved before group tracking existed.
	if s.targetChatID != 0 && !contains(s.knownChatIDs, s.targetChatID) {
		s.knownChatIDs = append(s.knownChatIDs, s.targetChatID)
	}
	if s.targetChatID != 0 && !contains(s.deliveryChatIDs, s.targetChatID) {
		s.deliveryChatIDs = append(s.deliveryChatIDs, s.targetChatID)
	}
	if saved.Version < 2 {
		if err := s.saveLocked(); err != nil {
			return nil, fmt.Errorf("migrate %s: %w", path, err)
		}
	}
	return s, nil
}

func (s *Store) Target() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.targetChatID
}

func (s *Store) KnownChats() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int64(nil), s.knownChatIDs...)
}

func (s *Store) DeliveryChats() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int64(nil), s.deliveryChatIDs...)
}

func (s *Store) Owner() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ownerUserID
}

func (s *Store) ResetOwner() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.ownerUserID
	s.ownerUserID = 0
	if err := s.saveLocked(); err != nil {
		s.ownerUserID = old
		return err
	}
	return nil
}

func (s *Store) SetTarget(ownerUserID, chatID int64) error {
	if chatID == 0 {
		return fmt.Errorf("invalid target chat id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ownerUserID == 0 || s.ownerUserID != ownerUserID {
		return fmt.Errorf("only the owner can set the target chat")
	}
	old := s.targetChatID
	oldKnown := append([]int64(nil), s.knownChatIDs...)
	oldDelivery := append([]int64(nil), s.deliveryChatIDs...)
	s.targetChatID = chatID
	if !contains(s.knownChatIDs, chatID) {
		s.knownChatIDs = append(s.knownChatIDs, chatID)
	}
	if !contains(s.deliveryChatIDs, chatID) {
		s.deliveryChatIDs = append(s.deliveryChatIDs, chatID)
	}
	if err := s.saveLocked(); err != nil {
		s.targetChatID = old
		s.knownChatIDs = oldKnown
		s.deliveryChatIDs = oldDelivery
		return err
	}
	return nil
}

func (s *Store) RegisterChat(chatID int64) error {
	if chatID == 0 {
		return fmt.Errorf("invalid chat id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if contains(s.knownChatIDs, chatID) {
		return nil
	}
	s.knownChatIDs = append(s.knownChatIDs, chatID)
	if err := s.saveLocked(); err != nil {
		s.knownChatIDs = s.knownChatIDs[:len(s.knownChatIDs)-1]
		return err
	}
	return nil
}

func (s *Store) UnregisterChat(chatID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	oldKnown := append([]int64(nil), s.knownChatIDs...)
	oldDelivery := append([]int64(nil), s.deliveryChatIDs...)
	oldTarget := s.targetChatID
	filtered := s.knownChatIDs[:0]
	for _, known := range s.knownChatIDs {
		if known != chatID {
			filtered = append(filtered, known)
		}
	}
	s.knownChatIDs = filtered
	deliveryFiltered := s.deliveryChatIDs[:0]
	for _, delivery := range s.deliveryChatIDs {
		if delivery != chatID {
			deliveryFiltered = append(deliveryFiltered, delivery)
		}
	}
	s.deliveryChatIDs = deliveryFiltered
	if s.targetChatID == chatID {
		s.targetChatID = 0
	}
	if err := s.saveLocked(); err != nil {
		s.knownChatIDs = oldKnown
		s.deliveryChatIDs = oldDelivery
		s.targetChatID = oldTarget
		return err
	}
	return nil
}

func (s *Store) ClearTarget(chatID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.targetChatID != chatID {
		return nil
	}
	old := s.targetChatID
	s.targetChatID = 0
	if err := s.saveLocked(); err != nil {
		s.targetChatID = old
		return err
	}
	return nil
}

// Claim binds an unclaimed bot to userID. It never replaces an existing owner.
func (s *Store) Claim(userID int64) (bool, error) {
	if userID <= 0 {
		return false, fmt.Errorf("invalid user id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ownerUserID != 0 {
		return s.ownerUserID == userID, nil
	}
	s.ownerUserID = userID
	if err := s.saveLocked(); err != nil {
		s.ownerUserID = 0
		return false, err
	}
	return true, nil
}

func (s *Store) saveLocked() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	data, err := json.MarshalIndent(fileData{
		Version:     2,
		OwnerUserID: s.ownerUserID, TargetChatID: s.targetChatID,
		KnownChatIDs: s.knownChatIDs, DeliveryChatIDs: s.deliveryChatIDs,
	}, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".auth-*.json")
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

func contains(values []int64, target int64) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
