package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"interactivewhatsmeow/internal/messages"
	"interactivewhatsmeow/internal/session"
	"interactivewhatsmeow/internal/store"
	"interactivewhatsmeow/internal/webhooks"
)

type fakeValidator struct{}

func (fakeValidator) Validate(context.Context, string) (bool, error) {
	return true, nil
}

type fakeSessionService struct {
	createResult *session.CreateSessionResult
	listResult   []store.Session
	getResult    *store.Session
	qrResult     *session.QRCodeResult
	pairResult   *session.PairCodeResult
	reconnect    *store.Session
	verify       *session.VerifyNumberResult
}

func (f fakeSessionService) CreateSession(context.Context, session.CreateSessionInput) (*session.CreateSessionResult, error) {
	return f.createResult, nil
}

func (f fakeSessionService) ListSessions(context.Context) ([]store.Session, error) {
	return f.listResult, nil
}

func (f fakeSessionService) GetSession(context.Context, string) (*store.Session, error) {
	return f.getResult, nil
}

func (f fakeSessionService) GetQRCode(context.Context, string) (*session.QRCodeResult, error) {
	return f.qrResult, nil
}

func (f fakeSessionService) GeneratePairCode(context.Context, string, string) (*session.PairCodeResult, error) {
	return f.pairResult, nil
}

func (f fakeSessionService) ReconnectSession(context.Context, string) (*store.Session, error) {
	return f.reconnect, nil
}

func (f fakeSessionService) DeleteSession(context.Context, string) error {
	return nil
}

func (f fakeSessionService) VerifyNumber(context.Context, string, string) (*session.VerifyNumberResult, error) {
	return f.verify, nil
}

type fakeMessageService struct {
	textResult  *messages.SendResult
	mediaResult *messages.SendResult
	replyResult *messages.SendResult
	editResult  *messages.SendResult
	listResult  []store.Message
}

func (f fakeMessageService) SendText(context.Context, string, string, string) (*messages.SendResult, error) {
	return f.textResult, nil
}

func (f fakeMessageService) SendMedia(context.Context, string, string, string, string, string, []byte) (*messages.SendResult, error) {
	return f.mediaResult, nil
}

func (f fakeMessageService) Reply(context.Context, string, string, string, string) (*messages.SendResult, error) {
	return f.replyResult, nil
}

func (f fakeMessageService) Edit(context.Context, string, string, string, string) (*messages.SendResult, error) {
	return f.editResult, nil
}

func (f fakeMessageService) List(context.Context, messages.ListInput) ([]store.Message, error) {
	return f.listResult, nil
}

type fakeWebhookService struct {
	createResult *store.Webhook
	listResult   []store.Webhook
}

func (f fakeWebhookService) CreateWebhook(context.Context, webhooks.CreateWebhookInput) (*store.Webhook, error) {
	return f.createResult, nil
}

func (f fakeWebhookService) ListWebhooks(context.Context, *string) ([]store.Webhook, error) {
	return f.listResult, nil
}

func (f fakeWebhookService) DeleteWebhook(context.Context, int64) error {
	return nil
}

type fakeReadService struct {
	dashboard *store.DashboardSummary
	chats     []store.ChatSummary
	analytics *store.AnalyticsSummary
}

func (f fakeReadService) GetDashboardSummary(context.Context) (*store.DashboardSummary, error) {
	return f.dashboard, nil
}

func (f fakeReadService) ListChats(context.Context, string) ([]store.ChatSummary, error) {
	return f.chats, nil
}

func (f fakeReadService) GetAnalyticsSummary(context.Context, store.AnalyticsSummaryInput) (*store.AnalyticsSummary, error) {
	return f.analytics, nil
}

func TestRouterCreateSession(t *testing.T) {
	router := NewRouter(RouterDependencies{
		Logger:        zerolog.Nop(),
		AuthValidator: fakeValidator{},
		SessionService: fakeSessionService{
			createResult: &session.CreateSessionResult{
				Session: &store.Session{
					SessionID:   "alpha",
					LoginMethod: store.LoginMethodQR,
					Status:      store.SessionStatusInitializing,
				},
			},
		},
		MessageService: fakeMessageService{},
		WebhookService: fakeWebhookService{},
		ReadService:    fakeReadService{},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/sessions", bytes.NewBufferString(`{"sessionId":"alpha","loginMethod":"qr"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "test")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", rec.Code)
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	var created store.Session
	if err := json.Unmarshal(body["session"], &created); err != nil {
		t.Fatalf("decode created session: %v", err)
	}

	if created.SessionID != "alpha" {
		t.Fatalf("expected sessionId alpha, got %s", created.SessionID)
	}
}

func TestRouterSendMedia(t *testing.T) {
	router := NewRouter(RouterDependencies{
		Logger:         zerolog.Nop(),
		AuthValidator:  fakeValidator{},
		SessionService: fakeSessionService{},
		MessageService: fakeMessageService{
			mediaResult: &messages.SendResult{
				SessionID:   "alpha",
				MessageID:   "wamid-1",
				ChatJID:     "5511999999999@s.whatsapp.net",
				Recipient:   "5511999999999@s.whatsapp.net",
				SenderJID:   "5511888888888@s.whatsapp.net",
				MessageType: "image",
				Status:      store.MessageStatusSent,
			},
		},
		WebhookService: fakeWebhookService{},
		ReadService:    fakeReadService{},
	})

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("sessionId", "alpha"); err != nil {
		t.Fatalf("write sessionId: %v", err)
	}
	if err := writer.WriteField("to", "5511999999999"); err != nil {
		t.Fatalf("write to: %v", err)
	}
	if err := writer.WriteField("caption", "hello"); err != nil {
		t.Fatalf("write caption: %v", err)
	}
	fileWriter, err := writer.CreateFormFile("file", "image.png")
	if err != nil {
		t.Fatalf("create file part: %v", err)
	}
	if _, err := fileWriter.Write([]byte("png")); err != nil {
		t.Fatalf("write file part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/messages/media", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-API-Key", "test")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var result messages.SendResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if result.MessageType != "image" {
		t.Fatalf("expected message type image, got %s", result.MessageType)
	}
}

func TestRouterListMessages(t *testing.T) {
	router := NewRouter(RouterDependencies{
		Logger:         zerolog.Nop(),
		AuthValidator:  fakeValidator{},
		SessionService: fakeSessionService{},
		MessageService: fakeMessageService{
			listResult: []store.Message{
				{
					ID:                12,
					SessionID:         "alpha",
					ExternalMessageID: "wamid-1",
					ChatJID:           "5511999999999@s.whatsapp.net",
					SenderJID:         "5511999999999@s.whatsapp.net",
					Direction:         store.MessageDirectionInbound,
					MessageType:       "text",
					Text:              "hello",
					Status:            store.MessageStatusReceived,
					MessageTimestamp:  time.Unix(1700000000, 0).UTC(),
				},
			},
		},
		WebhookService: fakeWebhookService{},
		ReadService:    fakeReadService{},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/messages?sessionId=alpha&limit=10&cursor="+strconv.FormatInt(99, 10), nil)
	req.Header.Set("X-API-Key", "test")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var body struct {
		Messages []store.Message `json:"messages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(body.Messages) != 1 || body.Messages[0].ExternalMessageID != "wamid-1" {
		t.Fatalf("unexpected messages payload: %+v", body.Messages)
	}
}

func TestRouterDashboardSummary(t *testing.T) {
	router := NewRouter(RouterDependencies{
		Logger:         zerolog.Nop(),
		AuthValidator:  fakeValidator{},
		SessionService: fakeSessionService{},
		MessageService: fakeMessageService{},
		WebhookService: fakeWebhookService{},
		ReadService: fakeReadService{
			dashboard: &store.DashboardSummary{
				Totals: store.DashboardTotals{
					TotalSessions:     2,
					ConnectedSessions: 1,
					Messages24h:       12,
				},
				Sessions: []store.Session{{SessionID: "alpha"}},
				RecentActivity: []store.DashboardActivity{{
					SessionID: "alpha",
					MessageID: "wamid-1",
				}},
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/dashboard/summary", nil)
	req.Header.Set("X-API-Key", "test")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var body store.DashboardSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if body.Totals.TotalSessions != 2 || len(body.Sessions) != 1 {
		t.Fatalf("unexpected dashboard payload: %+v", body)
	}
}

func TestRouterListChats(t *testing.T) {
	router := NewRouter(RouterDependencies{
		Logger:         zerolog.Nop(),
		AuthValidator:  fakeValidator{},
		SessionService: fakeSessionService{},
		MessageService: fakeMessageService{},
		WebhookService: fakeWebhookService{},
		ReadService: fakeReadService{
			chats: []store.ChatSummary{{
				SessionID:       "alpha",
				ChatJID:         "5511999999999@s.whatsapp.net",
				LastMessageText: "hello",
				MessageCount:    4,
			}},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/chats?sessionId=alpha", nil)
	req.Header.Set("X-API-Key", "test")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var body struct {
		Chats []store.ChatSummary `json:"chats"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(body.Chats) != 1 || body.Chats[0].MessageCount != 4 {
		t.Fatalf("unexpected chats payload: %+v", body.Chats)
	}
}

func TestRouterAnalyticsSummary(t *testing.T) {
	router := NewRouter(RouterDependencies{
		Logger:         zerolog.Nop(),
		AuthValidator:  fakeValidator{},
		SessionService: fakeSessionService{},
		MessageService: fakeMessageService{},
		WebhookService: fakeWebhookService{},
		ReadService: fakeReadService{
			analytics: &store.AnalyticsSummary{
				Range: "7d",
				Totals: store.AnalyticsTotals{
					TotalMessages:    20,
					InboundMessages:  11,
					OutboundMessages: 9,
				},
				DailySeries: []store.AnalyticsDaily{{Date: "2026-03-25", TotalMessages: 3}},
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/analytics/summary?range=7d", nil)
	req.Header.Set("X-API-Key", "test")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var body store.AnalyticsSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if body.Range != "7d" || body.Totals.TotalMessages != 20 {
		t.Fatalf("unexpected analytics payload: %+v", body)
	}
}

func TestRouterStaticRoutes(t *testing.T) {
	router := NewRouter(RouterDependencies{
		Logger:         zerolog.Nop(),
		AuthValidator:  fakeValidator{},
		SessionService: fakeSessionService{},
		MessageService: fakeMessageService{},
		WebhookService: fakeWebhookService{},
		ReadService:    fakeReadService{},
		StaticFS:       os.DirFS("../../public"),
	})

	for _, route := range []string{"/", "/dashboard", "/chat", "/analytics", "/settings", "/public/styles.css", "/public/config.js"} {
		req := httptest.NewRequest(http.MethodGet, route, nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200 for %s, got %d", route, rec.Code)
		}
	}
}
