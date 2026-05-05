package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/slack-go/slack"

	"sergeant/internal/i18n"
	"sergeant/internal/ledger"
	"sergeant/internal/parser"
	"sergeant/internal/slackapi"
	"sergeant/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	botToken := os.Getenv("SLACK_BOT_TOKEN")
	signingSecret := os.Getenv("SLACK_SIGNING_SECRET")
	if botToken == "" || signingSecret == "" {
		return errors.New("SLACK_BOT_TOKEN and SLACK_SIGNING_SECRET are required")
	}
	dbPath := getEnv("SERGEANT_DB_PATH", "./sergeant.db")
	addr := getEnv("SERGEANT_HTTP_ADDR", ":8080")
	defaultCcy, err := parser.NormalizeCurrency(getEnv("SERGEANT_DEFAULT_CURRENCY", "PLN"))
	if err != nil {
		return fmt.Errorf("SERGEANT_DEFAULT_CURRENCY: %w", err)
	}
	translator := i18n.New(getEnv("SERGEANT_LOCALE", i18n.DefaultLocale))
	logger.Info("locale", "active", translator.Locale(), "available", i18n.Available())

	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	client := slack.New(botToken)
	authCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	auth, err := client.AuthTestContext(authCtx)
	cancel()
	if err != nil {
		return err
	}
	logger.Info("connected", "team", auth.Team, "bot_user_id", auth.UserID)

	mux := http.NewServeMux()
	mux.Handle("/slack/events", &slackapi.Handler{
		SigningSecret: signingSecret,
		BotUserID:     auth.UserID,
		Ledger:        ledger.New(st, defaultCcy, translator),
		Responder:     slackapi.SlackResponder{Client: client},
		Translator:    translator,
		Logger:        logger,
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {})

	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}
	return nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
