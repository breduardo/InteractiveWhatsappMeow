package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	AppEnv                string
	Addr                  string
	DatabaseURL           string
	APIKey                string
	WebhookBatchSize      int
	WebhookMaxAttempts    int
	WebhookPollInterval   time.Duration
	WebhookRequestTimeout time.Duration
	PairCodeDisplayName   string
}

func Load() (Config, error) {
	cfg := Config{
		AppEnv:                getEnv("APP_ENV", "development"),
		Addr:                  getEnv("ADDR", ":3000"),
		DatabaseURL:           strings.TrimSpace(os.Getenv("DATABASE_URL")),
		APIKey:                getEnv("API_KEY", "dev-change-me"),
		WebhookBatchSize:      getEnvInt("WEBHOOK_BATCH_SIZE", 25),
		WebhookMaxAttempts:    getEnvInt("WEBHOOK_MAX_ATTEMPTS", 5),
		WebhookPollInterval:   getEnvDuration("WEBHOOK_POLL_INTERVAL", 2*time.Second),
		WebhookRequestTimeout: getEnvDuration("WEBHOOK_REQUEST_TIMEOUT", 10*time.Second),
		PairCodeDisplayName:   getEnv("PAIR_CODE_DISPLAY_NAME", "InteractiveWhatsMeow"),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}

	return parsed
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}

	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}

	return parsed
}
