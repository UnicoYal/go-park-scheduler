package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"schedulebot/internal/auth"
	"schedulebot/internal/bot"
	"schedulebot/internal/config"
	"schedulebot/internal/store"
	"schedulebot/internal/telegram"
)

func main() {
	cfg, err := config.FromEnv()
	if err != nil {
		log.Fatal(err)
	}

	eventStore, err := store.OpenJSON(cfg.DataFile)
	if err != nil {
		log.Fatalf("open event store: %v", err)
	}
	authStore, err := auth.Open(cfg.AuthFile)
	if err != nil {
		log.Fatalf("open auth store: %v", err)
	}
	templates, err := bot.LoadTemplates(cfg.TemplateDir)
	if err != nil {
		log.Fatalf("load message templates: %v", err)
	}

	client := telegram.NewClient(cfg.Token, &http.Client{Timeout: 70 * time.Second})
	service := bot.New(client, eventStore, authStore, templates, cfg.Password, cfg.Location, log.Default())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Printf("schedule bot started; timezone=%s data=%s", cfg.Location, cfg.DataFile)
	if err := service.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}
