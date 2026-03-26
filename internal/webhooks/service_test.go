package webhooks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"interactivewhatsmeow/internal/store"
)

type memoryRepo struct {
	mu         sync.Mutex
	webhooks   []store.Webhook
	deliveries map[int64]*store.WebhookDelivery
	nextID     int64
}

func newMemoryRepo(webhooks []store.Webhook) *memoryRepo {
	return &memoryRepo{
		webhooks:   webhooks,
		deliveries: make(map[int64]*store.WebhookDelivery),
		nextID:     1,
	}
}

func (r *memoryRepo) Create(context.Context, store.CreateWebhookParams) (*store.Webhook, error) {
	panic("not implemented")
}

func (r *memoryRepo) List(context.Context, *string) ([]store.Webhook, error) {
	panic("not implemented")
}

func (r *memoryRepo) Delete(context.Context, int64) error {
	panic("not implemented")
}

func (r *memoryRepo) ListMatching(_ context.Context, sessionID, eventType string) ([]store.Webhook, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	matches := make([]store.Webhook, 0)
	for _, webhook := range r.webhooks {
		if !webhook.IsActive {
			continue
		}

		if webhook.SessionID != nil && *webhook.SessionID != sessionID {
			continue
		}

		for _, event := range webhook.Events {
			if event == eventType {
				matches = append(matches, webhook)
				break
			}
		}
	}

	return matches, nil
}

func (r *memoryRepo) EnqueueDelivery(_ context.Context, webhookID int64, sessionID, eventType string, payload json.RawMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.deliveries[r.nextID] = &store.WebhookDelivery{
		ID:            r.nextID,
		WebhookID:     webhookID,
		SessionID:     sessionID,
		EventType:     eventType,
		Payload:       payload,
		Status:        "pending",
		NextAttemptAt: time.Now().UTC(),
	}
	r.nextID++

	return nil
}

func (r *memoryRepo) ListDueDeliveries(_ context.Context, now time.Time, limit int) ([]store.WebhookDelivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	deliveries := make([]store.WebhookDelivery, 0, limit)
	for _, delivery := range r.deliveries {
		if len(deliveries) >= limit {
			break
		}
		if delivery.NextAttemptAt.After(now) {
			continue
		}
		if delivery.Status != "pending" && delivery.Status != "retry" {
			continue
		}
		copied := *delivery
		for _, webhook := range r.webhooks {
			if webhook.ID == delivery.WebhookID {
				copied.WebhookURL = webhook.URL
				break
			}
		}
		deliveries = append(deliveries, copied)
	}

	return deliveries, nil
}

func (r *memoryRepo) StartAttempt(_ context.Context, deliveryID int64, attemptCount int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.deliveries[deliveryID].AttemptCount = attemptCount
	r.deliveries[deliveryID].Status = "processing"
	return nil
}

func (r *memoryRepo) MarkDelivered(_ context.Context, deliveryID int64, statusCode int, responseBody string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.deliveries[deliveryID].Status = "delivered"
	r.deliveries[deliveryID].DeliveredAt = timePointer(time.Now().UTC())
	r.deliveries[deliveryID].LastHTTPStatus = &statusCode
	r.deliveries[deliveryID].LastResponseBody = responseBody
	return nil
}

func (r *memoryRepo) MarkRetry(_ context.Context, deliveryID int64, statusCode *int, responseBody, lastError string, nextAttemptAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.deliveries[deliveryID].Status = "retry"
	r.deliveries[deliveryID].NextAttemptAt = nextAttemptAt
	r.deliveries[deliveryID].LastHTTPStatus = statusCode
	r.deliveries[deliveryID].LastResponseBody = responseBody
	r.deliveries[deliveryID].LastError = lastError
	return nil
}

func (r *memoryRepo) MarkFailed(_ context.Context, deliveryID int64, statusCode *int, responseBody, lastError string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.deliveries[deliveryID].Status = "failed"
	r.deliveries[deliveryID].LastHTTPStatus = statusCode
	r.deliveries[deliveryID].LastResponseBody = responseBody
	r.deliveries[deliveryID].LastError = lastError
	return nil
}

func TestServiceEmitEnqueuesMatchingWebhooks(t *testing.T) {
	sessionID := "alpha"
	repo := newMemoryRepo([]store.Webhook{
		{ID: 1, SessionID: &sessionID, URL: "https://example.com/one", Events: []string{"message.received"}, IsActive: true},
		{ID: 2, URL: "https://example.com/two", Events: []string{"session.connected"}, IsActive: true},
	})
	service := NewService(repo, time.Second, 10, 3, time.Second, zerolog.Nop())

	if err := service.Emit(context.Background(), "alpha", "message.received", map[string]string{"hello": "world"}); err != nil {
		t.Fatalf("emit webhook: %v", err)
	}

	if len(repo.deliveries) != 1 {
		t.Fatalf("expected 1 queued delivery, got %d", len(repo.deliveries))
	}
}

func TestServiceProcessDueMarksDelivered(t *testing.T) {
	var received store.WebhookEnvelope
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode webhook request: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	repo := newMemoryRepo([]store.Webhook{
		{ID: 1, URL: server.URL, Events: []string{"message.received"}, IsActive: true},
	})
	service := NewService(repo, 2*time.Second, 10, 3, time.Second, zerolog.Nop())

	if err := service.Emit(context.Background(), "alpha", "message.received", map[string]string{"hello": "world"}); err != nil {
		t.Fatalf("emit webhook: %v", err)
	}

	if err := service.processDue(context.Background()); err != nil {
		t.Fatalf("process due deliveries: %v", err)
	}

	delivery := repo.deliveries[1]
	if delivery.Status != "delivered" {
		t.Fatalf("expected delivered status, got %s", delivery.Status)
	}
	if received.Event != "message.received" || received.SessionID != "alpha" {
		t.Fatalf("unexpected webhook payload: %+v", received)
	}
}

func TestServiceProcessDueRetriesOnFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer server.Close()

	repo := newMemoryRepo([]store.Webhook{
		{ID: 1, URL: server.URL, Events: []string{"message.received"}, IsActive: true},
	})
	service := NewService(repo, 2*time.Second, 10, 3, time.Second, zerolog.Nop())

	if err := service.Emit(context.Background(), "alpha", "message.received", map[string]string{"hello": "world"}); err != nil {
		t.Fatalf("emit webhook: %v", err)
	}

	if err := service.processDue(context.Background()); err != nil {
		t.Fatalf("process due deliveries: %v", err)
	}

	delivery := repo.deliveries[1]
	if delivery.Status != "retry" {
		t.Fatalf("expected retry status, got %s", delivery.Status)
	}
	if delivery.LastHTTPStatus == nil || *delivery.LastHTTPStatus != http.StatusInternalServerError {
		t.Fatalf("expected last status 500, got %+v", delivery.LastHTTPStatus)
	}
	if !delivery.NextAttemptAt.After(time.Now().UTC()) {
		t.Fatalf("expected next attempt in the future, got %s", delivery.NextAttemptAt)
	}
}

func timePointer(value time.Time) *time.Time {
	return &value
}
