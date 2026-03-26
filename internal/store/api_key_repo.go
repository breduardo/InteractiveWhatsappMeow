package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
)

type APIKeyRepository struct {
	db *sql.DB
}

func NewAPIKeyRepository(db *sql.DB) *APIKeyRepository {
	return &APIKeyRepository{db: db}
}

func (r *APIKeyRepository) Bootstrap(ctx context.Context, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}

	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO api_keys (name, key_hash)
		 VALUES ($1, $2)
		 ON CONFLICT (key_hash) DO NOTHING`,
		"bootstrap",
		hashAPIKey(key),
	)
	if err != nil {
		return fmt.Errorf("bootstrap api key: %w", err)
	}

	return nil
}

func (r *APIKeyRepository) Validate(ctx context.Context, key string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(
		ctx,
		`SELECT EXISTS(
			SELECT 1
			FROM api_keys
			WHERE key_hash = $1
		)`,
		hashAPIKey(key),
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("validate api key: %w", err)
	}

	return exists, nil
}

func hashAPIKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}
