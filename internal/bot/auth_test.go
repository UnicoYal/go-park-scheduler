package bot

import (
	"context"
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"schedulebot/internal/model"
	"schedulebot/internal/telegram"
)

type authTestClient struct {
	messages []string
	chatIDs  []int64
	deleted  []int64
	left     []int64
}

func (c *authTestClient) GetUpdates(context.Context, int64) ([]telegram.Update, error) {
	return nil, nil
}
func (c *authTestClient) SendMessage(_ context.Context, chatID int64, text string) error {
	c.messages = append(c.messages, text)
	c.chatIDs = append(c.chatIDs, chatID)
	return nil
}
func (c *authTestClient) SendMessageTracked(ctx context.Context, chatID int64, text string) (int64, error) {
	if err := c.SendMessage(ctx, chatID, text); err != nil {
		return 0, err
	}
	return int64(1000 + len(c.messages)), nil
}
func (c *authTestClient) DeleteMessage(_ context.Context, _ int64, messageID int64) error {
	c.deleted = append(c.deleted, messageID)
	return nil
}
func (c *authTestClient) LeaveChat(_ context.Context, chatID int64) error {
	c.left = append(c.left, chatID)
	return nil
}

type authTestStore struct {
	owner    int64
	target   int64
	known    []int64
	delivery []int64
}

func (s *authTestStore) Owner() int64  { return s.owner }
func (s *authTestStore) Target() int64 { return s.target }
func (s *authTestStore) KnownChats() []int64 {
	return append([]int64(nil), s.known...)
}
func (s *authTestStore) DeliveryChats() []int64 {
	return append([]int64(nil), s.delivery...)
}
func (s *authTestStore) Claim(userID int64) (bool, error) {
	if s.owner != 0 {
		return s.owner == userID, nil
	}
	s.owner = userID
	return true, nil
}
func (s *authTestStore) ResetOwner() error {
	s.owner = 0
	return nil
}
func (s *authTestStore) SetTarget(ownerUserID, chatID int64) error {
	if ownerUserID != s.owner {
		return context.Canceled
	}
	s.target = chatID
	if !containsID(s.known, chatID) {
		s.known = append(s.known, chatID)
	}
	if !containsID(s.delivery, chatID) {
		s.delivery = append(s.delivery, chatID)
	}
	return nil
}
func (s *authTestStore) RegisterChat(chatID int64) error {
	if !containsID(s.known, chatID) {
		s.known = append(s.known, chatID)
	}
	return nil
}

func containsID(values []int64, target int64) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
func (s *authTestStore) UnregisterChat(chatID int64) error {
	filtered := s.known[:0]
	for _, known := range s.known {
		if known != chatID {
			filtered = append(filtered, known)
		}
	}
	s.known = filtered
	deliveryFiltered := s.delivery[:0]
	for _, delivery := range s.delivery {
		if delivery != chatID {
			deliveryFiltered = append(deliveryFiltered, delivery)
		}
	}
	s.delivery = deliveryFiltered
	if s.target == chatID {
		s.target = 0
	}
	return nil
}
func (s *authTestStore) ClearTarget(chatID int64) error {
	if s.target == chatID {
		s.target = 0
	}
	return nil
}

type unusedEventStore struct{}

func (unusedEventStore) Add(event model.Event) (model.Event, error) { return event, nil }
func (unusedEventStore) ListByChat(int64) []model.Event             { return nil }
func (unusedEventStore) Delete(int64, string) error                 { return nil }
func (unusedEventStore) ListAll() []model.Event                     { return nil }
func (unusedEventStore) DeleteByID(string) error                    { return nil }
func (unusedEventStore) Due(time.Time) []model.Event                { return nil }
func (unusedEventStore) MarkDelivered(string, int64) error          { return nil }
func (unusedEventStore) MarkNotified(string, time.Time) error       { return nil }

func TestLoginClaimsBotAndRestrictsOtherUsers(t *testing.T) {
	client := &authTestClient{}
	auth := &authTestStore{}
	b := New(client, unusedEventStore{}, auth, nil, "correct-password", time.UTC, log.New(io.Discard, "", 0))
	ctx := context.Background()

	login := &telegram.Message{
		MessageID: 7,
		Text:      "/login correct-password",
		Chat:      telegram.Chat{ID: 100, Type: "private"},
		From:      &telegram.User{ID: 100},
	}
	if err := b.handleMessage(ctx, login); err != nil {
		t.Fatal(err)
	}
	if auth.owner != 100 || len(client.deleted) != 1 || client.deleted[0] != 7 {
		t.Fatalf("login did not claim and delete password: owner=%d deleted=%v", auth.owner, client.deleted)
	}

	other := &telegram.Message{Text: "/help", Chat: telegram.Chat{ID: 200, Type: "private"}, From: &telegram.User{ID: 200}}
	if err := b.handleMessage(ctx, other); err != nil {
		t.Fatal(err)
	}
	if got := client.messages[len(client.messages)-1]; got != "Доступ закрыт." {
		t.Fatalf("unexpected response to another user: %q", got)
	}

	owner := &telegram.Message{Text: "/help", Chat: telegram.Chat{ID: 100, Type: "private"}, From: &telegram.User{ID: 100}}
	if err := b.handleMessage(ctx, owner); err != nil {
		t.Fatal(err)
	}
	if got := client.messages[len(client.messages)-1]; !strings.Contains(got, "Бот напомнит") {
		t.Fatalf("owner did not receive help: %q", got)
	}
}

func TestWrongPasswordDoesNotClaimBot(t *testing.T) {
	client := &authTestClient{}
	auth := &authTestStore{}
	b := New(client, unusedEventStore{}, auth, nil, "correct-password", time.UTC, log.New(io.Discard, "", 0))
	message := &telegram.Message{
		MessageID: 8,
		Text:      "/login wrong-password",
		Chat:      telegram.Chat{ID: 100, Type: "private"},
		From:      &telegram.User{ID: 100},
	}
	if err := b.handleMessage(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if auth.owner != 0 {
		t.Fatalf("wrong password claimed bot for %d", auth.owner)
	}
	if len(client.deleted) != 1 || client.messages[len(client.messages)-1] != "Неверный пароль." {
		t.Fatalf("unexpected client state: deleted=%v messages=%v", client.deleted, client.messages)
	}
}

func TestResetOwnerPreservesGroupsAndAllowsNewLogin(t *testing.T) {
	client := &authTestClient{}
	auth := &authTestStore{owner: 100, target: -500, known: []int64{-500}, delivery: []int64{-500}}
	b := New(client, unusedEventStore{}, auth, nil, "correct-password", time.UTC, log.New(io.Discard, "", 0))
	reset := &telegram.Message{
		MessageID: 20,
		Text:      "/resetowner correct-password",
		Chat:      telegram.Chat{ID: 100, Type: "private"},
		From:      &telegram.User{ID: 100},
	}
	if err := b.handleMessage(context.Background(), reset); err != nil {
		t.Fatal(err)
	}
	if auth.owner != 0 || auth.target != -500 || len(auth.delivery) != 1 {
		t.Fatalf("reset lost state: owner=%d target=%d delivery=%v", auth.owner, auth.target, auth.delivery)
	}
	if len(client.deleted) != 1 || client.deleted[0] != 20 {
		t.Fatalf("reset command with password was not deleted: %v", client.deleted)
	}

	login := &telegram.Message{
		MessageID: 21,
		Text:      "/login correct-password",
		Chat:      telegram.Chat{ID: 200, Type: "private"},
		From:      &telegram.User{ID: 200},
	}
	if err := b.handleMessage(context.Background(), login); err != nil {
		t.Fatal(err)
	}
	if auth.owner != 200 || auth.target != -500 || len(auth.delivery) != 1 {
		t.Fatalf("new login lost state: owner=%d target=%d delivery=%v", auth.owner, auth.target, auth.delivery)
	}
}

func TestOwnerAddingBotBindsDeliveryGroup(t *testing.T) {
	client := &authTestClient{}
	auth := &authTestStore{owner: 100}
	b := New(client, unusedEventStore{}, auth, nil, "password", time.UTC, log.New(io.Discard, "", 0))
	change := &telegram.ChatMemberUpdated{
		Chat:          telegram.Chat{ID: -500, Type: "supergroup", Title: "Учебная группа"},
		From:          telegram.User{ID: 100},
		OldChatMember: telegram.ChatMember{Status: "left"},
		NewChatMember: telegram.ChatMember{Status: "member"},
	}
	if err := b.handleMembership(context.Background(), change); err != nil {
		t.Fatal(err)
	}
	if auth.target != -500 {
		t.Fatalf("target=%d, want -500", auth.target)
	}
	if len(client.chatIDs) != 1 || client.chatIDs[0] != 100 {
		t.Fatalf("binding confirmation should be private: chats=%v", client.chatIDs)
	}

	before := len(client.messages)
	groupCommand := &telegram.Message{
		Text: "/help", Chat: telegram.Chat{ID: -500, Type: "supergroup"}, From: &telegram.User{ID: 100},
	}
	if err := b.handleMessage(context.Background(), groupCommand); err != nil {
		t.Fatal(err)
	}
	if len(client.messages) != before {
		t.Fatal("bot responded to a management command in the delivery group")
	}
	if err := auth.RegisterChat(-600); err != nil {
		t.Fatal(err)
	}
	leave := &telegram.Message{
		Text: "/leave", Chat: telegram.Chat{ID: 100, Type: "private"}, From: &telegram.User{ID: 100},
	}
	if err := b.handleMessage(context.Background(), leave); err != nil {
		t.Fatal(err)
	}
	if len(client.left) != 1 || client.left[0] != -500 || auth.target != 0 || len(auth.known) != 1 || auth.known[0] != -600 {
		t.Fatalf("leave failed: left=%v target=%d known=%v", client.left, auth.target, auth.known)
	}

	leaveAll := &telegram.Message{
		Text: "/leaveall", Chat: telegram.Chat{ID: 100, Type: "private"}, From: &telegram.User{ID: 100},
	}
	if err := b.handleMessage(context.Background(), leaveAll); err != nil {
		t.Fatal(err)
	}
	if len(client.left) != 2 || client.left[1] != -600 || auth.target != 0 || len(auth.known) != 0 {
		t.Fatalf("leaveall failed: left=%v target=%d known=%v", client.left, auth.target, auth.known)
	}
}

func TestTestCommandIsManagedPrivatelyAndDeliveredToGroup(t *testing.T) {
	client := &authTestClient{}
	auth := &authTestStore{owner: 100, target: -500, known: []int64{-500}, delivery: []int64{-500}}
	templates, err := LoadTemplates("../../templates")
	if err != nil {
		t.Fatal(err)
	}
	b := New(client, unusedEventStore{}, auth, templates, "password", time.UTC, log.New(io.Discard, "", 0))
	b.now = func() time.Time { return time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC) }
	message := &telegram.Message{
		Text: "/test defense 2026-12-19 11:00",
		Chat: telegram.Chat{ID: 100, Type: "private"}, From: &telegram.User{ID: 100},
	}
	if err := b.handleMessage(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if len(client.chatIDs) != 2 || client.chatIDs[0] != -500 || client.chatIDs[1] != 100 {
		t.Fatalf("messages went to wrong chats: %v", client.chatIDs)
	}
	if !strings.Contains(client.messages[0], "защита проектов") {
		t.Fatalf("group did not receive rendered template: %q", client.messages[0])
	}
	if !strings.Contains(client.messages[1], "отправлено") {
		t.Fatalf("owner did not receive confirmation: %q", client.messages[1])
	}

	clear := &telegram.Message{
		Text: "/clear", Chat: telegram.Chat{ID: 100, Type: "private"}, From: &telegram.User{ID: 100},
	}
	if err := b.handleMessage(context.Background(), clear); err != nil {
		t.Fatal(err)
	}
	if len(client.deleted) != 1 || client.deleted[0] != 1001 {
		t.Fatalf("test message was not deleted: %v", client.deleted)
	}
}
