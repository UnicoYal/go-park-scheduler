package bot

import (
	"bytes"
	"fmt"
	"path/filepath"
	"text/template"
	"time"

	"schedulebot/internal/model"
)

type Templates struct {
	items map[model.EventKind]*template.Template
}

type templateData struct {
	DayMonth string
	Time     string
	Weekday  string
	RKNumber string
	SheetURL string
}

func LoadTemplates(dir string) (*Templates, error) {
	files := map[model.EventKind]string{
		model.Checkpoint: "checkpoint.txt",
		model.PreDefense: "predefense.txt",
		model.Defense:    "defense.txt",
	}
	result := &Templates{items: make(map[model.EventKind]*template.Template, len(files))}
	for kind, name := range files {
		path := filepath.Join(dir, name)
		tmpl, err := template.New(name).Option("missingkey=error").ParseFiles(path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		result.items[kind] = tmpl
	}
	return result, nil
}

func (t *Templates) Render(event model.Event, loc *time.Location) (string, error) {
	tmpl, ok := t.items[event.Kind]
	if !ok {
		return "", fmt.Errorf("template for event kind %q is not loaded", event.Kind)
	}
	startsAt := event.StartsAt.In(loc)
	data := templateData{
		DayMonth: russianDayMonth(startsAt),
		Time:     startsAt.Format("15:04"),
		Weekday:  russianWeekday(startsAt.Weekday()),
		RKNumber: event.RKNumber,
		SheetURL: event.SheetURL,
	}
	var rendered bytes.Buffer
	if err := tmpl.ExecuteTemplate(&rendered, filepath.Base(tmpl.Name()), data); err != nil {
		return "", err
	}
	return rendered.String(), nil
}

func russianDayMonth(value time.Time) string {
	months := [...]string{"января", "февраля", "марта", "апреля", "мая", "июня", "июля", "августа", "сентября", "октября", "ноября", "декабря"}
	return fmt.Sprintf("%d %s", value.Day(), months[value.Month()-1])
}

func russianWeekday(day time.Weekday) string {
	weekdays := [...]string{"в воскресенье", "в понедельник", "во вторник", "в среду", "в четверг", "в пятницу", "в субботу"}
	return weekdays[day]
}
