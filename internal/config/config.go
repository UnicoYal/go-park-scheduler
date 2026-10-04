package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	Token       string
	Password    string
	DataFile    string
	AuthFile    string
	TemplateDir string
	Location    *time.Location
}

func FromEnv() (Config, error) {
	token := strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))
	if token == "" {
		return Config{}, fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}
	password := strings.TrimSpace(os.Getenv("BOT_PASSWORD"))
	if password == "" || password == "change_me" {
		return Config{}, fmt.Errorf("BOT_PASSWORD is required and must not be the example value")
	}

	dataFile := strings.TrimSpace(os.Getenv("DATA_FILE"))
	if dataFile == "" {
		dataFile = "./data/events.json"
	}
	authFile := strings.TrimSpace(os.Getenv("AUTH_FILE"))
	if authFile == "" {
		authFile = "./data/auth.json"
	}
	templateDir := strings.TrimSpace(os.Getenv("TEMPLATE_DIR"))
	if templateDir == "" {
		templateDir = "./templates"
	}

	tz := strings.TrimSpace(os.Getenv("TIMEZONE"))
	if tz == "" {
		tz = "Europe/Moscow"
	}
	location, err := time.LoadLocation(tz)
	if err != nil {
		return Config{}, fmt.Errorf("invalid TIMEZONE %q: %w", tz, err)
	}

	return Config{Token: token, Password: password, DataFile: dataFile, AuthFile: authFile, TemplateDir: templateDir, Location: location}, nil
}
