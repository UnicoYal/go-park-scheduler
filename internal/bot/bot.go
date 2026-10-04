package bot

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"schedulebot/internal/model"
	"schedulebot/internal/store"
	"schedulebot/internal/telegram"
)

type telegramClient interface {
	GetUpdates(ctx context.Context, offset int64) ([]telegram.Update, error)
	SendMessage(ctx context.Context, chatID int64, text string) error
	SendMessageTracked(ctx context.Context, chatID int64, text string) (int64, error)
	DeleteMessage(ctx context.Context, chatID, messageID int64) error
	LeaveChat(ctx context.Context, chatID int64) error
}

type authStore interface {
	Owner() int64
	Target() int64
	Claim(userID int64) (bool, error)
	ResetOwner() error
	SetTarget(ownerUserID, chatID int64) error
	ClearTarget(chatID int64) error
	KnownChats() []int64
	DeliveryChats() []int64
	RegisterChat(chatID int64) error
	UnregisterChat(chatID int64) error
}

type eventStore interface {
	Add(event model.Event) (model.Event, error)
	ListAll() []model.Event
	DeleteByID(id string) error
	Due(now time.Time) []model.Event
	MarkDelivered(id string, chatID int64) error
	MarkNotified(id string, at time.Time) error
}

type Bot struct {
	client       telegramClient
	store        eventStore
	auth         authStore
	tmpl         *Templates
	password     [32]byte
	loc          *time.Location
	log          *log.Logger
	now          func() time.Time
	attempts     map[int64]loginAttempt
	testMessages []sentMessage
}

type loginAttempt struct {
	failures     int
	blockedUntil time.Time
}

type sentMessage struct {
	chatID    int64
	messageID int64
}

func New(client telegramClient, store eventStore, auth authStore, templates *Templates, password string, loc *time.Location, logger *log.Logger) *Bot {
	return &Bot{
		client: client, store: store, auth: auth, tmpl: templates,
		password: sha256.Sum256([]byte(password)), loc: loc,
		log: logger, now: time.Now, attempts: make(map[int64]loginAttempt),
	}
}

func (b *Bot) Run(ctx context.Context) error {
	go b.runScheduler(ctx)

	var offset int64
	for ctx.Err() == nil {
		updates, err := b.client.GetUpdates(ctx, offset)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			b.log.Printf("get updates: %v", err)
			if !sleep(ctx, 3*time.Second) {
				return ctx.Err()
			}
			continue
		}
		for _, update := range updates {
			if update.UpdateID >= offset {
				offset = update.UpdateID + 1
			}
			if update.MyChatMember != nil {
				if err := b.handleMembership(ctx, update.MyChatMember); err != nil {
					b.log.Printf("handle membership in chat %d: %v", update.MyChatMember.Chat.ID, err)
				}
			}
			if update.Message == nil || strings.TrimSpace(update.Message.Text) == "" {
				continue
			}
			if err := b.handleMessage(ctx, update.Message); err != nil {
				b.log.Printf("handle message in chat %d: %v", update.Message.Chat.ID, err)
			}
		}
	}
	return ctx.Err()
}

func (b *Bot) runScheduler(ctx context.Context) {
	b.deliverDue(ctx)
	firstCheck := time.NewTimer(untilNextHour(b.now()))
	defer firstCheck.Stop()
	select {
	case <-ctx.Done():
		return
	case <-firstCheck.C:
		b.deliverDue(ctx)
	}

	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.deliverDue(ctx)
		}
	}
}

func untilNextHour(now time.Time) time.Duration {
	return now.Truncate(time.Hour).Add(time.Hour).Sub(now)
}

func (b *Bot) deliverDue(ctx context.Context) {
	now := b.now()
	deliveryChats := b.auth.DeliveryChats()
	if len(deliveryChats) == 0 {
		return
	}
	for _, event := range b.store.Due(now) {
		text, err := b.tmpl.Render(event, b.loc)
		if err != nil {
			b.log.Printf("render notification for %s: %v", event.ID, err)
			continue
		}
		allDelivered := true
		for _, chatID := range deliveryChats {
			if event.DeliveredTo(chatID) {
				continue
			}
			if err := b.client.SendMessage(ctx, chatID, text); err != nil {
				b.log.Printf("send notification %s to chat %d: %v", event.ID, chatID, err)
				allDelivered = false
				continue
			}
			if err := b.store.MarkDelivered(event.ID, chatID); err != nil {
				b.log.Printf("IMPORTANT: notification %s sent to chat %d but delivery not saved: %v", event.ID, chatID, err)
				allDelivered = false
			}
		}
		if allDelivered {
			if err := b.store.MarkNotified(event.ID, now); err != nil {
				b.log.Printf("IMPORTANT: notification %s delivered but not completed: %v", event.ID, err)
			}
		}
	}
}

func (b *Bot) handleMessage(ctx context.Context, message *telegram.Message) error {
	chatID := message.Chat.ID
	if message.Chat.Type != "private" {
		// Group chats are delivery-only: never add management chatter there.
		return nil
	}
	cmd, args := splitCommand(message.Text)
	if cmd == "/login" {
		return b.handleLogin(ctx, message, args)
	}
	if message.From == nil || message.From.ID != b.auth.Owner() {
		if b.auth.Owner() == 0 {
			return b.client.SendMessage(ctx, chatID, "Бот ещё не привязан. Для доступа отправьте в личном чате: /login ПАРОЛЬ")
		}
		return b.client.SendMessage(ctx, chatID, "Доступ закрыт.")
	}
	var reply string
	switch cmd {
	case "/start", "/help":
		reply = helpText(b.loc)
	case "/chatid":
		reply = fmt.Sprintf("ID этого чата: %d", chatID)
	case "/resetowner":
		if err := b.client.DeleteMessage(ctx, chatID, message.MessageID); err != nil {
			b.log.Printf("delete resetowner message in chat %d: %v", chatID, err)
		}
		provided := sha256.Sum256([]byte(strings.TrimSpace(args)))
		if subtle.ConstantTimeCompare(provided[:], b.password[:]) != 1 {
			reply = "Неверный пароль. Владелец не изменён."
			break
		}
		if err := b.auth.ResetOwner(); err != nil {
			return fmt.Errorf("reset bot owner: %w", err)
		}
		reply = "Владелец сброшен. Группы и расписание сохранены. Теперь новый аккаунт может выполнить /login ПАРОЛЬ."
	case "/add":
		if len(b.auth.DeliveryChats()) == 0 {
			reply = noTargetText()
			break
		}
		event, err := parseAdd(args, 0, b.now(), b.loc)
		if err != nil {
			reply = "Не удалось добавить событие: " + err.Error() + "\n\n" + addUsage()
			break
		}
		event, err = b.store.Add(event)
		if err != nil {
			return fmt.Errorf("save event: %w", err)
		}
		reply = fmt.Sprintf("Событие добавлено.\nID: %s\n%s\nДата: %s\nНапоминание: %s", event.ID, kindLabel(event.Kind), formatDate(event.StartsAt, b.loc), formatDate(event.NotificationAt(), b.loc))
	case "/test":
		deliveryChats := b.auth.DeliveryChats()
		if len(deliveryChats) == 0 {
			reply = noTargetText()
			break
		}
		event, err := parseAdd(args, 0, b.now(), b.loc)
		if err != nil {
			reply = "Не удалось сформировать тестовое сообщение: " + err.Error() + "\n\n" + testUsage()
			break
		}
		testMessage, err := b.tmpl.Render(event, b.loc)
		if err != nil {
			return fmt.Errorf("render test notification: %w", err)
		}
		sentCount := 0
		failedCount := 0
		for _, deliveryChatID := range deliveryChats {
			messageID, err := b.client.SendMessageTracked(ctx, deliveryChatID, testMessage)
			if err != nil {
				b.log.Printf("send test notification to chat %d: %v", deliveryChatID, err)
				failedCount++
				continue
			}
			b.testMessages = append(b.testMessages, sentMessage{chatID: deliveryChatID, messageID: messageID})
			sentCount++
		}
		reply = fmt.Sprintf("Тестовое уведомление отправлено в %d чатов", sentCount)
		if failedCount > 0 {
			reply += fmt.Sprintf(", ошибок: %d", failedCount)
		}
		reply += "."
	case "/clear":
		if len(b.testMessages) == 0 {
			reply = "Тестовых сообщений для удаления нет."
			break
		}
		deleted := 0
		failed := 0
		for _, sent := range b.testMessages {
			if err := b.client.DeleteMessage(ctx, sent.chatID, sent.messageID); err != nil {
				b.log.Printf("delete test message %d in chat %d: %v", sent.messageID, sent.chatID, err)
				failed++
				continue
			}
			deleted++
		}
		b.testMessages = nil
		reply = fmt.Sprintf("Очистка завершена: удалено %d тестовых сообщений", deleted)
		if failed > 0 {
			reply += fmt.Sprintf(", не удалось удалить %d", failed)
		}
		reply += "."
	case "/leaveall":
		chats := b.auth.KnownChats()
		if len(chats) == 0 {
			reply = "Бот не состоит ни в одной известной группе или канале."
			break
		}
		left := 0
		failed := 0
		for _, groupChatID := range chats {
			if err := b.client.LeaveChat(ctx, groupChatID); err != nil {
				b.log.Printf("leave chat %d: %v", groupChatID, err)
				failed++
				continue
			}
			if err := b.auth.UnregisterChat(groupChatID); err != nil {
				return fmt.Errorf("forget chat %d after leaving: %w", groupChatID, err)
			}
			left++
		}
		reply = fmt.Sprintf("Готово: бот покинул %d групп и каналов", left)
		if failed > 0 {
			reply += fmt.Sprintf(", не удалось покинуть %d", failed)
		}
		reply += "."
	case "/leave":
		targetChatID := b.auth.Target()
		if targetChatID == 0 {
			reply = "Подключённой группы или канала нет."
			break
		}
		if err := b.client.LeaveChat(ctx, targetChatID); err != nil {
			return fmt.Errorf("leave target chat %d: %w", targetChatID, err)
		}
		if err := b.auth.UnregisterChat(targetChatID); err != nil {
			return fmt.Errorf("forget target chat %d after leaving: %w", targetChatID, err)
		}
		reply = "Готово. Бот покинул подключённую группу или канал."
	case "/list":
		if len(b.auth.DeliveryChats()) == 0 {
			reply = noTargetText()
			break
		}
		reply = listText(b.store.ListAll(), b.now(), b.loc)
	case "/delete":
		if len(b.auth.DeliveryChats()) == 0 {
			reply = noTargetText()
			break
		}
		id := strings.TrimSpace(args)
		if id == "" {
			reply = "Укажите ID: /delete a1b2c3d4"
			break
		}
		if err := b.store.DeleteByID(id); errors.Is(err, store.ErrNotFound) {
			reply = "Событие с таким ID не найдено."
		} else if err != nil {
			return fmt.Errorf("delete event: %w", err)
		} else {
			reply = "Событие удалено."
		}
	default:
		reply = "Неизвестная команда. Используйте /help."
	}
	return b.client.SendMessage(ctx, chatID, reply)
}

func (b *Bot) handleMembership(ctx context.Context, change *telegram.ChatMemberUpdated) error {
	owner := b.auth.Owner()
	wasOutside := isOutsideChat(change.OldChatMember.Status)
	isOutside := isOutsideChat(change.NewChatMember.Status)
	if wasOutside && !isOutside {
		if err := b.auth.RegisterChat(change.Chat.ID); err != nil {
			return fmt.Errorf("remember chat: %w", err)
		}
		if owner == 0 || change.From.ID != owner {
			return nil
		}
		if err := b.auth.SetTarget(owner, change.Chat.ID); err != nil {
			return fmt.Errorf("save target chat: %w", err)
		}
		name := change.Chat.Title
		if name == "" {
			name = fmt.Sprint(change.Chat.ID)
		}
		return b.client.SendMessage(ctx, owner, fmt.Sprintf("Группа «%s» подключена. Уведомления будут отправляться во все подключённые группы, а управление остаётся в этом личном чате.", name))
	}
	if !wasOutside && isOutside {
		wasTarget := b.auth.Target() == change.Chat.ID
		if err := b.auth.UnregisterChat(change.Chat.ID); err != nil {
			return fmt.Errorf("forget chat: %w", err)
		}
		if wasTarget && owner != 0 {
			return b.client.SendMessage(ctx, owner, "Бот покинул подключённую группу. Добавьте его в нужную группу снова.")
		}
	}
	return nil
}

func isOutsideChat(status string) bool {
	return status == "left" || status == "kicked"
}

func noTargetText() string {
	return "Группы для уведомлений ещё не подключены. Добавьте этого бота в нужные группы со своего Telegram-аккаунта — он подключит их автоматически."
}

func (b *Bot) handleLogin(ctx context.Context, message *telegram.Message, password string) error {
	chatID := message.Chat.ID
	// Passwords should not remain in chat history, whether they are correct or not.
	if err := b.client.DeleteMessage(ctx, chatID, message.MessageID); err != nil {
		b.log.Printf("delete login message in chat %d: %v", chatID, err)
	}
	if message.From == nil || message.Chat.Type != "private" {
		return b.client.SendMessage(ctx, chatID, "Авторизация доступна только в личном чате с ботом.")
	}
	userID := message.From.ID
	owner := b.auth.Owner()
	if owner != 0 {
		if owner == userID {
			return b.client.SendMessage(ctx, chatID, "Вы уже авторизованы как владелец.")
		}
		return b.client.SendMessage(ctx, chatID, "Доступ закрыт.")
	}

	now := b.now()
	attempt := b.attempts[userID]
	if now.Before(attempt.blockedUntil) {
		return b.client.SendMessage(ctx, chatID, "Слишком много попыток. Повторите через 15 минут.")
	}
	provided := sha256.Sum256([]byte(strings.TrimSpace(password)))
	if subtle.ConstantTimeCompare(provided[:], b.password[:]) != 1 {
		attempt.failures++
		if attempt.failures >= 5 {
			attempt.failures = 0
			attempt.blockedUntil = now.Add(15 * time.Minute)
		}
		b.attempts[userID] = attempt
		return b.client.SendMessage(ctx, chatID, "Неверный пароль.")
	}

	claimed, err := b.auth.Claim(userID)
	if err != nil {
		return fmt.Errorf("save bot owner: %w", err)
	}
	if !claimed {
		return b.client.SendMessage(ctx, chatID, "Доступ закрыт.")
	}
	delete(b.attempts, userID)
	return b.client.SendMessage(ctx, chatID, "Готово. Этот Telegram-аккаунт назначен единственным владельцем бота.")
}

func sleep(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
