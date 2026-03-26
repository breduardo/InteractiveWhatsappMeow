package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/lib/pq"
)

type CreateWebhookParams struct {
	SessionID *string
	URL       string
	Events    []string
}

type WebhookRepository struct {
	db *sql.DB
}

func NewWebhookRepository(db *sql.DB) *WebhookRepository {
	return &WebhookRepository{db: db}
}

func (r *WebhookRepository) Create(ctx context.Context, params CreateWebhookParams) (*Webhook, error) {
	row := r.db.QueryRowContext(
		ctx,
		`INSERT INTO webhooks (session_id, url, events)
		 VALUES ($1, $2, $3)
		 RETURNING id, session_id, url, events, is_active, created_at, updated_at`,
		params.SessionID,
		params.URL,
		pq.Array(params.Events),
	)

	webhook, err := scanWebhook(row)
	if err != nil {
		return nil, fmt.Errorf("create webhook: %w", err)
	}

	return webhook, nil
}

func (r *WebhookRepository) List(ctx context.Context, sessionID *string) ([]Webhook, error) {
	var (
		rows *sql.Rows
		err  error
	)

	if sessionID == nil {
		rows, err = r.db.QueryContext(
			ctx,
			`SELECT id, session_id, url, events, is_active, created_at, updated_at
			 FROM webhooks
			 ORDER BY created_at DESC`,
		)
	} else {
		rows, err = r.db.QueryContext(
			ctx,
			`SELECT id, session_id, url, events, is_active, created_at, updated_at
			 FROM webhooks
			 WHERE session_id = $1 OR session_id IS NULL
			 ORDER BY created_at DESC`,
			*sessionID,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("list webhooks: %w", err)
	}
	defer rows.Close()

	webhooks := make([]Webhook, 0)
	for rows.Next() {
		webhook, err := scanWebhook(rows)
		if err != nil {
			return nil, fmt.Errorf("scan webhook: %w", err)
		}
		webhooks = append(webhooks, *webhook)
	}

	return webhooks, rows.Err()
}

func (r *WebhookRepository) Delete(ctx context.Context, id int64) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM webhooks WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete webhook: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete webhook rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

func (r *WebhookRepository) ListMatching(ctx context.Context, sessionID, eventType string) ([]Webhook, error) {
	rows, err := r.db.QueryContext(
		ctx,
		`SELECT id, session_id, url, events, is_active, created_at, updated_at
		 FROM webhooks
		 WHERE is_active = TRUE
		   AND $2 = ANY(events)
		   AND (session_id IS NULL OR session_id = $1)
		 ORDER BY created_at ASC`,
		sessionID,
		eventType,
	)
	if err != nil {
		return nil, fmt.Errorf("list matching webhooks: %w", err)
	}
	defer rows.Close()

	webhooks := make([]Webhook, 0)
	for rows.Next() {
		webhook, err := scanWebhook(rows)
		if err != nil {
			return nil, fmt.Errorf("scan webhook: %w", err)
		}
		webhooks = append(webhooks, *webhook)
	}

	return webhooks, rows.Err()
}

func (r *WebhookRepository) EnqueueDelivery(ctx context.Context, webhookID int64, sessionID, eventType string, payload json.RawMessage) error {
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO webhook_deliveries (
			webhook_id, session_id, event_type, payload, status, next_attempt_at
		)
		VALUES ($1, $2, $3, $4, 'pending', NOW())`,
		webhookID,
		sessionID,
		eventType,
		payload,
	)
	if err != nil {
		return fmt.Errorf("enqueue webhook delivery: %w", err)
	}

	return nil
}

func (r *WebhookRepository) ListDueDeliveries(ctx context.Context, now time.Time, limit int) ([]WebhookDelivery, error) {
	rows, err := r.db.QueryContext(
		ctx,
		`SELECT d.id, d.webhook_id, d.session_id, d.event_type, d.payload, d.status,
		        d.attempt_count, d.next_attempt_at, d.last_error, d.last_http_status,
		        d.last_response_body, d.delivered_at, d.created_at, d.updated_at, w.url
		 FROM webhook_deliveries d
		 JOIN webhooks w ON w.id = d.webhook_id
		 WHERE d.status IN ('pending', 'retry')
		   AND d.next_attempt_at <= $1
		   AND w.is_active = TRUE
		 ORDER BY d.next_attempt_at ASC, d.id ASC
		 LIMIT $2`,
		now,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list due deliveries: %w", err)
	}
	defer rows.Close()

	deliveries := make([]WebhookDelivery, 0)
	for rows.Next() {
		delivery, err := scanWebhookDelivery(rows)
		if err != nil {
			return nil, fmt.Errorf("scan webhook delivery: %w", err)
		}
		deliveries = append(deliveries, *delivery)
	}

	return deliveries, rows.Err()
}

func (r *WebhookRepository) StartAttempt(ctx context.Context, deliveryID int64, attemptCount int) error {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE webhook_deliveries
		 SET status = 'processing',
		     attempt_count = $2,
		     updated_at = NOW()
		 WHERE id = $1`,
		deliveryID,
		attemptCount,
	)
	if err != nil {
		return fmt.Errorf("start webhook attempt: %w", err)
	}

	return nil
}

func (r *WebhookRepository) MarkDelivered(ctx context.Context, deliveryID int64, statusCode int, responseBody string) error {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE webhook_deliveries
		 SET status = 'delivered',
		     last_http_status = $2,
		     last_response_body = $3,
		     delivered_at = NOW(),
		     updated_at = NOW()
		 WHERE id = $1`,
		deliveryID,
		statusCode,
		responseBody,
	)
	if err != nil {
		return fmt.Errorf("mark webhook delivered: %w", err)
	}

	return nil
}

func (r *WebhookRepository) MarkRetry(ctx context.Context, deliveryID int64, statusCode *int, responseBody, lastError string, nextAttemptAt time.Time) error {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE webhook_deliveries
		 SET status = 'retry',
		     last_http_status = $2,
		     last_response_body = $3,
		     last_error = $4,
		     next_attempt_at = $5,
		     updated_at = NOW()
		 WHERE id = $1`,
		deliveryID,
		statusCode,
		responseBody,
		lastError,
		nextAttemptAt,
	)
	if err != nil {
		return fmt.Errorf("mark webhook retry: %w", err)
	}

	return nil
}

func (r *WebhookRepository) MarkFailed(ctx context.Context, deliveryID int64, statusCode *int, responseBody, lastError string) error {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE webhook_deliveries
		 SET status = 'failed',
		     last_http_status = $2,
		     last_response_body = $3,
		     last_error = $4,
		     updated_at = NOW()
		 WHERE id = $1`,
		deliveryID,
		statusCode,
		responseBody,
		lastError,
	)
	if err != nil {
		return fmt.Errorf("mark webhook failed: %w", err)
	}

	return nil
}

func scanWebhook(scanner interface {
	Scan(dest ...interface{}) error
}) (*Webhook, error) {
	var (
		webhook   Webhook
		sessionID sql.NullString
		events    []string
	)

	err := scanner.Scan(
		&webhook.ID,
		&sessionID,
		&webhook.URL,
		pq.Array(&events),
		&webhook.IsActive,
		&webhook.CreatedAt,
		&webhook.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if sessionID.Valid {
		webhook.SessionID = &sessionID.String
	}
	webhook.Events = events

	return &webhook, nil
}

func scanWebhookDelivery(scanner interface {
	Scan(dest ...interface{}) error
}) (*WebhookDelivery, error) {
	var (
		delivery         WebhookDelivery
		payload          []byte
		deliveredAt      sql.NullTime
		lastHTTPStatus   sql.NullInt64
		lastResponseBody sql.NullString
		lastError        sql.NullString
	)

	err := scanner.Scan(
		&delivery.ID,
		&delivery.WebhookID,
		&delivery.SessionID,
		&delivery.EventType,
		&payload,
		&delivery.Status,
		&delivery.AttemptCount,
		&delivery.NextAttemptAt,
		&lastError,
		&lastHTTPStatus,
		&lastResponseBody,
		&deliveredAt,
		&delivery.CreatedAt,
		&delivery.UpdatedAt,
		&delivery.WebhookURL,
	)
	if err != nil {
		return nil, err
	}

	delivery.Payload = payload
	if lastError.Valid {
		delivery.LastError = lastError.String
	}
	if lastHTTPStatus.Valid {
		code := int(lastHTTPStatus.Int64)
		delivery.LastHTTPStatus = &code
	}
	if lastResponseBody.Valid {
		delivery.LastResponseBody = lastResponseBody.String
	}
	if deliveredAt.Valid {
		delivery.DeliveredAt = &deliveredAt.Time
	}

	return &delivery, nil
}
