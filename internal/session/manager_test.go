package session

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rs/zerolog"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"interactivewhatsmeow/internal/store"
)

type fakeSessionRepo struct {
	sessions map[string]*store.Session
}

func (r *fakeSessionRepo) Create(context.Context, store.CreateSessionParams) (*store.Session, error) {
	panic("not implemented")
}

func (r *fakeSessionRepo) List(context.Context) ([]store.Session, error) {
	list := make([]store.Session, 0, len(r.sessions))
	for _, session := range r.sessions {
		list = append(list, *session)
	}
	return list, nil
}

func (r *fakeSessionRepo) Get(_ context.Context, sessionID string) (*store.Session, error) {
	sessionRecord, ok := r.sessions[sessionID]
	if !ok {
		return nil, store.ErrNotFound
	}
	return sessionRecord, nil
}

func (r *fakeSessionRepo) UpdateState(_ context.Context, params store.UpdateSessionStateParams) (*store.Session, error) {
	sessionRecord, ok := r.sessions[params.SessionID]
	if !ok {
		return nil, store.ErrNotFound
	}
	sessionRecord.Status = params.Status
	if params.QRCode != nil {
		sessionRecord.QRCode = *params.QRCode
	} else {
		sessionRecord.QRCode = ""
	}
	sessionRecord.QRExpiresAt = params.QRExpiresAt
	sessionRecord.LastError = params.LastError
	if params.Phone != "" {
		sessionRecord.Phone = params.Phone
	}
	if params.DeviceJID != "" {
		sessionRecord.DeviceJID = params.DeviceJID
	}
	return sessionRecord, nil
}

func (r *fakeSessionRepo) MarkDeleted(_ context.Context, sessionID string) error {
	sessionRecord, ok := r.sessions[sessionID]
	if !ok {
		return store.ErrNotFound
	}
	sessionRecord.Status = store.SessionStatusDeleted
	return nil
}

type fakeMessageRepo struct {
	savedMessages []store.SaveMessageParams
	statusCalls   []struct {
		SessionID  string
		MessageIDs []string
		Status     store.MessageStatus
	}
}

func (r *fakeMessageRepo) Save(_ context.Context, params store.SaveMessageParams) (*store.Message, error) {
	r.savedMessages = append(r.savedMessages, params)
	return &store.Message{
		SessionID:         params.SessionID,
		ExternalMessageID: params.ExternalMessageID,
		ChatJID:           params.ChatJID,
		SenderJID:         params.SenderJID,
		Direction:         params.Direction,
		MessageType:       params.MessageType,
		Text:              params.Text,
		Status:            params.Status,
		Payload:           params.Payload,
		MessageTimestamp:  params.MessageTimestamp,
	}, nil
}

func (r *fakeMessageRepo) GetByExternalID(context.Context, string, string) (*store.Message, error) {
	return nil, store.ErrNotFound
}

func (r *fakeMessageRepo) UpdateStatus(_ context.Context, sessionID string, messageIDs []string, status store.MessageStatus) error {
	r.statusCalls = append(r.statusCalls, struct {
		SessionID  string
		MessageIDs []string
		Status     store.MessageStatus
	}{
		SessionID:  sessionID,
		MessageIDs: append([]string(nil), messageIDs...),
		Status:     status,
	})
	return nil
}

type fakeWebhookEmitter struct {
	events []struct {
		SessionID string
		EventType string
		Data      interface{}
	}
}

func (f *fakeWebhookEmitter) Emit(_ context.Context, sessionID, eventType string, data interface{}) error {
	f.events = append(f.events, struct {
		SessionID string
		EventType string
		Data      interface{}
	}{
		SessionID: sessionID,
		EventType: eventType,
		Data:      data,
	})
	return nil
}

func TestManagerGetQRCode(t *testing.T) {
	manager := NewManager(
		"InteractiveWhatsMeow",
		&fakeSessionRepo{
			sessions: map[string]*store.Session{
				"alpha": {
					SessionID: "alpha",
					QRCode:    "qr-code",
				},
			},
		},
		&fakeMessageRepo{},
		&fakeWebhookEmitter{},
		nil,
		zerolog.Nop(),
	)

	result, err := manager.GetQRCode(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("get qr code: %v", err)
	}
	if result.QRCode != "qr-code" {
		t.Fatalf("expected qr-code, got %s", result.QRCode)
	}
}

func TestManagerHandleIncomingMessagePersistsAndEmits(t *testing.T) {
	messageRepo := &fakeMessageRepo{}
	webhookEmitter := &fakeWebhookEmitter{}
	manager := NewManager(
		"InteractiveWhatsMeow",
		&fakeSessionRepo{sessions: map[string]*store.Session{}},
		messageRepo,
		webhookEmitter,
		nil,
		zerolog.Nop(),
	)

	manager.handleEvent("alpha", nil, &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     types.NewJID("5511999999999", types.DefaultUserServer),
				Sender:   types.NewJID("5511999999999", types.DefaultUserServer),
				IsFromMe: false,
			},
			ID:        types.MessageID("wamid-1"),
			Timestamp: time.Unix(1700000000, 0).UTC(),
		},
		RawMessage: &waE2E.Message{
			Conversation: proto.String("hello"),
		},
	})

	if len(messageRepo.savedMessages) != 1 {
		t.Fatalf("expected 1 saved message, got %d", len(messageRepo.savedMessages))
	}
	if messageRepo.savedMessages[0].Text != "hello" {
		t.Fatalf("expected text hello, got %s", messageRepo.savedMessages[0].Text)
	}
	if len(webhookEmitter.events) != 1 || webhookEmitter.events[0].EventType != "message.received" {
		t.Fatalf("unexpected emitted events: %+v", webhookEmitter.events)
	}

	payload, err := json.Marshal(webhookEmitter.events[0].Data)
	if err != nil {
		t.Fatalf("marshal emitted payload: %v", err)
	}
	if string(payload) == "" {
		t.Fatal("expected non-empty emitted payload")
	}
}

func TestManagerHandleReceiptUpdatesStatus(t *testing.T) {
	messageRepo := &fakeMessageRepo{}
	webhookEmitter := &fakeWebhookEmitter{}
	manager := NewManager(
		"InteractiveWhatsMeow",
		&fakeSessionRepo{sessions: map[string]*store.Session{}},
		messageRepo,
		webhookEmitter,
		nil,
		zerolog.Nop(),
	)

	manager.handleEvent("alpha", nil, &events.Receipt{
		MessageIDs: []types.MessageID{"wamid-1", "wamid-2"},
		Timestamp:  time.Unix(1700000001, 0).UTC(),
		Type:       types.ReceiptTypeDelivered,
	})

	if len(messageRepo.statusCalls) != 1 {
		t.Fatalf("expected 1 status update, got %d", len(messageRepo.statusCalls))
	}
	if messageRepo.statusCalls[0].Status != store.MessageStatusDelivered {
		t.Fatalf("expected delivered status, got %s", messageRepo.statusCalls[0].Status)
	}
	if len(webhookEmitter.events) != 1 || webhookEmitter.events[0].EventType != "message.status_updated" {
		t.Fatalf("unexpected emitted events: %+v", webhookEmitter.events)
	}
}
