package session

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rs/zerolog"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	waStore "go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"interactivewhatsmeow/internal/store"
)

type fakeSessionRepo struct {
	sessions map[string]*store.Session
	resetIDs []string
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

func (r *fakeSessionRepo) ResetForQRCode(_ context.Context, sessionID string) (*store.Session, error) {
	sessionRecord, ok := r.sessions[sessionID]
	if !ok {
		return nil, store.ErrNotFound
	}

	r.resetIDs = append(r.resetIDs, sessionID)
	sessionRecord.LoginMethod = store.LoginMethodQR
	sessionRecord.Status = store.SessionStatusInitializing
	sessionRecord.DeviceJID = ""
	sessionRecord.QRCode = ""
	sessionRecord.QRExpiresAt = nil
	sessionRecord.LastError = ""

	return sessionRecord, nil
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

type fakeWAContainer struct {
	devices        map[string]*waStore.Device
	deletedDevices []string
	newDeviceCount int
}

func (c *fakeWAContainer) NewDevice() *waStore.Device {
	c.newDeviceCount++
	device := &waStore.Device{}
	device.Container = c
	return device
}

func (c *fakeWAContainer) GetDevice(_ context.Context, jid types.JID) (*waStore.Device, error) {
	if c.devices == nil {
		return nil, nil
	}
	return c.devices[jid.String()], nil
}

func (c *fakeWAContainer) PutDevice(context.Context, *waStore.Device) error {
	return nil
}

func (c *fakeWAContainer) DeleteDevice(_ context.Context, device *waStore.Device) error {
	if device != nil && device.ID != nil {
		c.deletedDevices = append(c.deletedDevices, device.ID.String())
		delete(c.devices, device.ID.String())
	}
	return nil
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

func TestManagerOpenQRCodeReturnsExistingActiveQR(t *testing.T) {
	expiresAt := time.Now().UTC().Add(30 * time.Second)
	repo := &fakeSessionRepo{
		sessions: map[string]*store.Session{
			"alpha": {
				SessionID:   "alpha",
				LoginMethod: store.LoginMethodQR,
				Status:      store.SessionStatusQRReady,
				QRCode:      "qr-code",
				QRExpiresAt: &expiresAt,
			},
		},
	}

	manager := NewManager(
		"InteractiveWhatsMeow",
		repo,
		&fakeMessageRepo{},
		&fakeWebhookEmitter{},
		&fakeWAContainer{},
		zerolog.Nop(),
	)

	result, err := manager.OpenQRCode(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("open qr code: %v", err)
	}
	if len(repo.resetIDs) != 0 {
		t.Fatalf("expected no reset call, got %+v", repo.resetIDs)
	}
	if result.QRCode == nil || result.QRCode.QRCode != "qr-code" {
		t.Fatalf("unexpected qr result: %+v", result.QRCode)
	}
}

func TestManagerOpenQRCodeRejectsConnectedSession(t *testing.T) {
	repo := &fakeSessionRepo{
		sessions: map[string]*store.Session{
			"alpha": {
				SessionID: "alpha",
				Status:    store.SessionStatusConnected,
			},
		},
	}
	container := &fakeWAContainer{}
	manager := NewManager(
		"InteractiveWhatsMeow",
		repo,
		&fakeMessageRepo{},
		&fakeWebhookEmitter{},
		container,
		zerolog.Nop(),
	)

	runtimeSession := manager.newRuntime(
		store.Session{SessionID: "alpha", LoginMethod: store.LoginMethodQR},
		container.NewDevice(),
	)
	manager.runtimes["alpha"] = runtimeSession

	if _, err := manager.OpenQRCode(context.Background(), "alpha"); err != ErrSessionConnected {
		t.Fatalf("expected ErrSessionConnected, got %v", err)
	}
}

func TestManagerOpenQRCodeResetsDisconnectedSession(t *testing.T) {
	deviceJID := types.JID{User: "5511999999999", Device: 1, Server: types.DefaultUserServer}
	container := &fakeWAContainer{
		devices: map[string]*waStore.Device{},
	}
	storedDevice := &waStore.Device{ID: &deviceJID}
	storedDevice.Container = container
	container.devices[deviceJID.String()] = storedDevice

	repo := &fakeSessionRepo{
		sessions: map[string]*store.Session{
			"alpha": {
				SessionID:   "alpha",
				LoginMethod: store.LoginMethodPairCode,
				Status:      store.SessionStatusDisconnected,
				DeviceJID:   deviceJID.String(),
				QRCode:      "old-qr",
				LastError:   "socket closed",
			},
		},
	}

	manager := NewManager(
		"InteractiveWhatsMeow",
		repo,
		&fakeMessageRepo{},
		&fakeWebhookEmitter{},
		container,
		zerolog.Nop(),
	)

	launched := false
	manager.launchPendingLogin = func(context.Context, *runtimeSession, string) {
		launched = true
	}

	result, err := manager.OpenQRCode(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("open qr code: %v", err)
	}

	if !launched {
		t.Fatal("expected pending login launch")
	}
	if len(repo.resetIDs) != 1 || repo.resetIDs[0] != "alpha" {
		t.Fatalf("expected reset for alpha, got %+v", repo.resetIDs)
	}
	if len(container.deletedDevices) != 1 || container.deletedDevices[0] != deviceJID.String() {
		t.Fatalf("expected deleted stored device %s, got %+v", deviceJID.String(), container.deletedDevices)
	}

	sessionRecord := repo.sessions["alpha"]
	if sessionRecord.LoginMethod != store.LoginMethodQR {
		t.Fatalf("expected login method qr, got %s", sessionRecord.LoginMethod)
	}
	if sessionRecord.Status != store.SessionStatusInitializing {
		t.Fatalf("expected initializing status, got %s", sessionRecord.Status)
	}
	if sessionRecord.DeviceJID != "" || sessionRecord.QRCode != "" || sessionRecord.LastError != "" {
		t.Fatalf("expected cleared session state, got %+v", sessionRecord)
	}
	if result.Session == nil || result.Session.SessionID != "alpha" {
		t.Fatalf("unexpected result session: %+v", result.Session)
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
