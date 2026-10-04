package bot

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"schedulebot/internal/model"
)

const dateLayout = "2006-01-02 15:04"

func splitCommand(text string) (string, string) {
	parts := strings.SplitN(strings.TrimSpace(text), " ", 2)
	command := strings.ToLower(strings.SplitN(parts[0], "@", 2)[0])
	if len(parts) == 1 {
		return command, ""
	}
	return command, strings.TrimSpace(parts[1])
}

func parseAdd(args string, chatID int64, now time.Time, loc *time.Location) (model.Event, error) {
	parts := strings.Fields(args)
	if len(parts) < 3 {
		return model.Event{}, fmt.Errorf("недостаточно параметров")
	}
	kind := model.EventKind(strings.ToLower(parts[0]))
	if !kind.Valid() {
		return model.Event{}, fmt.Errorf("неизвестный шаблон %q", parts[0])
	}
	startsAt, err := time.ParseInLocation(dateLayout, parts[1]+" "+parts[2], loc)
	if err != nil {
		return model.Event{}, fmt.Errorf("дата должна быть в формате ГГГГ-ММ-ДД ЧЧ:ММ")
	}
	if !startsAt.After(now.In(loc)) {
		return model.Event{}, fmt.Errorf("дата должна быть в будущем")
	}
	event := model.Event{ChatID: chatID, Kind: kind, StartsAt: startsAt, CreatedAt: now}
	if kind == model.Checkpoint {
		if len(parts) != 5 {
			return model.Event{}, fmt.Errorf("для РК укажите номер и ссылку на ведомость")
		}
		event.RKNumber = parts[3]
		event.SheetURL = parts[4]
		if !strings.HasPrefix(event.SheetURL, "https://") && !strings.HasPrefix(event.SheetURL, "http://") {
			return model.Event{}, fmt.Errorf("ссылка на ведомость должна начинаться с http:// или https://")
		}
	} else if len(parts) > 3 {
		return model.Event{}, fmt.Errorf("для этого шаблона после времени параметры не нужны")
	}
	return event, nil
}

func helpText(loc *time.Location) string {
	return "Управление работает только в личном чате, а уведомления отправляются во все подключённые группы и каналы. Бот напомнит о РК за 1 день, а о предзащите и защите — за 2 дня. Отправка выполняется только с 10:00 до 22:00 по Москве.\n\n" +
		addUsage() + "\n\n" +
		"Шаблоны:\n" +
		"checkpoint — рубежный контроль\n" +
		"predefense — предзащита проекта\n" +
		"defense — защита проекта\n\n" +
		"Другие команды:\n/test ... — сразу отправить тестовый шаблон без сохранения\n/clear — удалить тестовые сообщения из группы\n/leave — покинуть подключённую группу или канал\n/leaveall — покинуть все группы и каналы\n/list — показать расписание\n/delete ID — удалить событие\n/chatid — показать ID чата\n/resetowner ПАРОЛЬ — сменить владельца\n/help — эта справка\n\n" +
		"Часовой пояс: " + loc.String()
}

func testUsage() string {
	return "Команда /test использует те же параметры, что и /add.\nПример:\n/test checkpoint 2027-04-20 18:00 РК1 https://example.com/sheet"
}

func addUsage() string {
	return "Форматы:\n" +
		"/add checkpoint ГГГГ-ММ-ДД ЧЧ:ММ НОМЕР_РК ССЫЛКА_НА_ВЕДОМОСТЬ\n" +
		"/add predefense ГГГГ-ММ-ДД ЧЧ:ММ\n" +
		"/add defense ГГГГ-ММ-ДД ЧЧ:ММ\n\n" +
		"Пример:\n/add checkpoint 2027-04-20 18:00 РК1 https://example.com/sheet"
}

func listText(events []model.Event, now time.Time, loc *time.Location) string {
	upcoming := make([]model.Event, 0, len(events))
	for _, event := range events {
		if event.StartsAt.After(now) {
			upcoming = append(upcoming, event)
		}
	}
	if len(upcoming) == 0 {
		return "Предстоящих событий нет."
	}
	sort.Slice(upcoming, func(i, j int) bool { return upcoming[i].StartsAt.Before(upcoming[j].StartsAt) })
	var result strings.Builder
	result.WriteString("Предстоящие события:\n")
	for _, event := range upcoming {
		fmt.Fprintf(&result, "\n%s · %s\n%s", event.ID, formatDate(event.StartsAt, loc), kindLabel(event.Kind))
		if event.Kind == model.Checkpoint && event.RKNumber != "" {
			result.WriteString(": " + event.RKNumber)
		}
		if event.NotifiedAt != nil {
			result.WriteString("\nНапоминание отправлено")
		}
	}
	return result.String()
}

func kindLabel(kind model.EventKind) string {
	switch kind {
	case model.Checkpoint:
		return "Рубежный контроль"
	case model.PreDefense:
		return "Предзащита проекта"
	case model.Defense:
		return "Защита проекта"
	default:
		return string(kind)
	}
}

func formatDate(value time.Time, loc *time.Location) string {
	return value.In(loc).Format("02.01.2006 15:04")
}
