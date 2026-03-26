package webhooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/rs/zerolog"

	"interactivewhatsmeow/internal/store"
)

type Repository interface {
	Create(ctx context.Context, params store.CreateWebhookParams) (*store.Webhook, error)
	List(ctx context.Context, sessionID *string) ([]store.Webhook, error)
	Delete(ctx context.Context, id int64) error
	ListMatching(ctx context.Context, sessionID, eventType string) ([]store.Webhook, error)
	EnqueueDelivery(ctx context.Context, webhookID int64, sessionID, eventType string, payload json.RawMessage) error
	ListDueDeliveries(ctx context.Context, now time.Time, limit int) ([]store.WebhookDelivery, error)
	StartAttempt(ctx context.Context, deliveryID int64, attemptCount int) error
	MarkDelivered(ctx context.Context, deliveryID int64, statusCode int, responseBody string) error
	MarkRetry(ctx context.Context, deliveryID int64, statusCode *int, responseBody, lastError string, nextAttemptAt time.Time) error
	MarkFailed(ctx context.Context, deliveryID int64, statusCode *int, responseBody, lastError string) error
}

type Service struct {
	repo          Repository
	client        *http.Client
	logger        zerolog.Logger
	batchSize     int
	maxAttempts   int
	pollInterval  time.Duration
	workerStarted chan struct{}
}

func NewService(repo Repository, requestTimeout time.Duration, batchSize, maxAttempts int, pollInterval time.Duration, logger zerolog.Logger) *Service {
	return &Service{
		repo: repo,
		client: &http.Client{
			Timeout: requestTimeout,
		},
		logger:        logger,
		batchSize:     batchSize,
		maxAttempts:   maxAttempts,
		pollInterval:  pollInterval,
		workerStarted: make(chan struct{}),
	}
}

type CreateWebhookInput struct {
	SessionID *string
	URL       string
	Events    []string
}

func (s *Service) CreateWebhook(ctx context.Context, input CreateWebhookInput) (*store.Webhook, error) {
	return s.repo.Create(ctx, store.CreateWebhookParams{
		SessionID: input.SessionID,
		URL:       input.URL,
		Events:    input.Events,
	})
}

func (s *Service) ListWebhooks(ctx context.Context, sessionID *string) ([]store.Webhook, error) {
	return s.repo.List(ctx, sessionID)
}

func (s *Service) DeleteWebhook(ctx context.Context, id int64) error {
	return s.repo.Delete(ctx, id)
}

func (s *Service) Emit(ctx context.Context, sessionID, eventType string, data interface{}) error {
	webhooks, err := s.repo.ListMatching(ctx, sessionID, eventType)
	if err != nil {
		return fmt.Errorf("list matching webhooks: %w", err)
	}
	if len(webhooks) == 0 {
		return nil
	}

	envelope := store.WebhookEnvelope{
		Event:     eventType,
		SessionID: sessionID,
		Timestamp: time.Now().UTC(),
		Data:      data,
	}

	payload, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("marshal webhook payload: %w", err)
	}

	for _, webhook := range webhooks {
		if err := s.repo.EnqueueDelivery(ctx, webhook.ID, sessionID, eventType, payload); err != nil {
			return fmt.Errorf("enqueue webhook delivery: %w", err)
		}
	}

	return nil
}

func (s *Service) Start(ctx context.Context) {
	go s.run(ctx)
}

func (s *Service) run(ctx context.Context) {
	close(s.workerStarted)

	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	for {
		if err := s.processDue(ctx); err != nil {
			s.logger.Error().Err(err).Msg("webhook worker iteration failed")
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) processDue(ctx context.Context) error {
	deliveries, err := s.repo.ListDueDeliveries(ctx, time.Now().UTC(), s.batchSize)
	if err != nil {
		return err
	}

	for _, delivery := range deliveries {
		attemptCount := delivery.AttemptCount + 1
		if err := s.repo.StartAttempt(ctx, delivery.ID, attemptCount); err != nil {
			return err
		}

		statusCode, responseBody, deliverErr := s.deliver(ctx, delivery)
		if deliverErr == nil {
			if err := s.repo.MarkDelivered(ctx, delivery.ID, statusCode, responseBody); err != nil {
				return err
			}
			continue
		}

		if attemptCount >= s.maxAttempts {
			if err := s.repo.MarkFailed(ctx, delivery.ID, intPointer(statusCode), responseBody, deliverErr.Error()); err != nil {
				return err
			}
			continue
		}

		nextAttempt := time.Now().UTC().Add(backoffForAttempt(attemptCount))
		if err := s.repo.MarkRetry(ctx, delivery.ID, intPointer(statusCode), responseBody, deliverErr.Error(), nextAttempt); err != nil {
			return err
		}
	}

	return nil
}

func (s *Service) deliver(ctx context.Context, delivery store.WebhookDelivery) (int, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, delivery.WebhookURL, bytes.NewReader(delivery.Payload))
	if err != nil {
		return 0, "", fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 32*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, string(body), fmt.Errorf("unexpected response status %d", resp.StatusCode)
	}

	return resp.StatusCode, string(body), nil
}

func backoffForAttempt(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 5 {
		attempt = 5
	}

	return time.Duration(1<<uint(attempt-1)) * 10 * time.Second
}

func intPointer(value int) *int {
	if value == 0 {
		return nil
	}
	v := value
	return &v
}
