package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	waLog "go.mau.fi/whatsmeow/util/log"

	"interactivewhatsmeow/internal/config"
	"interactivewhatsmeow/internal/httpapi"
	"interactivewhatsmeow/internal/messages"
	"interactivewhatsmeow/internal/session"
	"interactivewhatsmeow/internal/store"
	"interactivewhatsmeow/internal/webhooks"
)

func main() {
	zerolog.TimeFieldFormat = time.RFC3339

	cfg, err := config.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load config")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to open database")
	}
	defer db.Close()

	if err := store.Migrate(db); err != nil {
		log.Fatal().Err(err).Msg("failed to run migrations")
	}

	apiKeys := store.NewAPIKeyRepository(db)
	if err := apiKeys.Bootstrap(ctx, cfg.APIKey); err != nil {
		log.Fatal().Err(err).Msg("failed to bootstrap api key")
	}

	sessionRepo := store.NewSessionRepository(db)
	messageRepo := store.NewMessageRepository(db)
	webhookRepo := store.NewWebhookRepository(db)

	webhookSvc := webhooks.NewService(
		webhookRepo,
		cfg.WebhookRequestTimeout,
		cfg.WebhookBatchSize,
		cfg.WebhookMaxAttempts,
		cfg.WebhookPollInterval,
		log.With().Str("component", "webhooks").Logger(),
	)
	webhookSvc.Start(ctx)

	waContainer, err := session.NewWAContainer(ctx, cfg.DatabaseURL, waLog.Noop)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create whatsmeow store")
	}
	defer waContainer.Close()

	sessionManager := session.NewManager(
		cfg.PairCodeDisplayName,
		sessionRepo,
		messageRepo,
		webhookSvc,
		waContainer,
		log.With().Str("component", "session-manager").Logger(),
	)

	if err := sessionManager.Rehydrate(ctx); err != nil {
		log.Fatal().Err(err).Msg("failed to rehydrate sessions")
	}

	messageSvc := messages.NewService(sessionManager, messageRepo)
	readSvc := store.NewReadService(db, sessionRepo)

	router := httpapi.NewRouter(httpapi.RouterDependencies{
		Logger:         log.Logger,
		AuthValidator:  apiKeys,
		SessionService: sessionManager,
		MessageService: messageSvc,
		WebhookService: webhookSvc,
		ReadService:    readSvc,
		StaticFS:       os.DirFS("public"),
	})

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		if err := sessionManager.Close(shutdownCtx); err != nil {
			log.Error().Err(err).Msg("failed to close session manager")
		}

		if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error().Err(err).Msg("failed to shutdown http server")
		}
	}()

	log.Info().Str("addr", cfg.Addr).Msg("http server listening")
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal().Err(err).Msg("http server stopped unexpectedly")
	}
}
