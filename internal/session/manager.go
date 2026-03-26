package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	waStore "go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"interactivewhatsmeow/internal/messages"
	"interactivewhatsmeow/internal/store"
	"interactivewhatsmeow/internal/whatsapp"
)

var (
	ErrSessionExists        = errors.New("session already exists")
	ErrSessionNotConnected  = errors.New("session is not connected")
	ErrQRCodeUnavailable    = errors.New("qr code not available")
	ErrPhoneRequired        = errors.New("phone is required")
	ErrNumberNotOnWhatsApp  = errors.New("number is not on WhatsApp")
	ErrLoginMethodInvalid   = errors.New("invalid login method")
	ErrPairCodeNotAvailable = errors.New("pairing flow is not ready yet")
	ErrSessionConnected     = errors.New("session already connected")
)

type SessionRepository interface {
	Create(ctx context.Context, params store.CreateSessionParams) (*store.Session, error)
	List(ctx context.Context) ([]store.Session, error)
	Get(ctx context.Context, sessionID string) (*store.Session, error)
	UpdateState(ctx context.Context, params store.UpdateSessionStateParams) (*store.Session, error)
	ResetForQRCode(ctx context.Context, sessionID string) (*store.Session, error)
	MarkDeleted(ctx context.Context, sessionID string) error
}

type MessageRepository interface {
	Save(ctx context.Context, params store.SaveMessageParams) (*store.Message, error)
	GetByExternalID(ctx context.Context, sessionID, externalMessageID string) (*store.Message, error)
	UpdateStatus(ctx context.Context, sessionID string, messageIDs []string, status store.MessageStatus) error
}

type WebhookEmitter interface {
	Emit(ctx context.Context, sessionID, eventType string, data interface{}) error
}

type DeviceContainer interface {
	NewDevice() *waStore.Device
	GetDevice(ctx context.Context, jid types.JID) (*waStore.Device, error)
}

type Manager struct {
	displayName string
	sessions    SessionRepository
	messages    MessageRepository
	webhooks    WebhookEmitter
	container   DeviceContainer
	logger      zerolog.Logger

	mu                 sync.RWMutex
	runtimes           map[string]*runtimeSession
	launchPendingLogin func(context.Context, *runtimeSession, string)
}

type runtimeSession struct {
	sessionID   string
	loginMethod store.LoginMethod
	client      *whatsmeow.Client
	device      *waStore.Device

	readyOnce sync.Once
	readyCh   chan struct{}
}

type CreateSessionInput struct {
	SessionID   string
	Name        string
	LoginMethod store.LoginMethod
	Phone       string
}

type CreateSessionResult struct {
	Session  *store.Session  `json:"session"`
	PairCode *PairCodeResult `json:"pairCode,omitempty"`
	QRCode   *QRCodeResult   `json:"qr,omitempty"`
}

type QRCodeResult struct {
	SessionID string     `json:"sessionId"`
	QRCode    string     `json:"qrCode"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

type OpenQRSessionResult struct {
	Session *store.Session `json:"session"`
	QRCode  *QRCodeResult  `json:"qr,omitempty"`
}

type PairCodeResult struct {
	SessionID   string `json:"sessionId"`
	Phone       string `json:"phone"`
	PairingCode string `json:"pairingCode"`
}

type VerifyNumberResult struct {
	SessionID    string `json:"sessionId"`
	Phone        string `json:"phone"`
	Exists       bool   `json:"exists"`
	JID          string `json:"jid,omitempty"`
	VerifiedName string `json:"verifiedName,omitempty"`
}

func NewManager(
	displayName string,
	sessions SessionRepository,
	messagesRepo MessageRepository,
	webhooks WebhookEmitter,
	container DeviceContainer,
	logger zerolog.Logger,
) *Manager {
	manager := &Manager{
		displayName: displayName,
		sessions:    sessions,
		messages:    messagesRepo,
		webhooks:    webhooks,
		container:   container,
		logger:      logger,
		runtimes:    make(map[string]*runtimeSession),
	}

	manager.launchPendingLogin = func(ctx context.Context, runtimeSession *runtimeSession, sessionID string) {
		go manager.startPendingLogin(ctx, runtimeSession, sessionID)
	}

	return manager
}

func (m *Manager) Rehydrate(ctx context.Context) error {
	records, err := m.sessions.List(ctx)
	if err != nil {
		return fmt.Errorf("list sessions for rehydrate: %w", err)
	}

	for _, record := range records {
		if record.DeviceJID == "" || record.Status == store.SessionStatusDeleted || record.DeletedAt != nil {
			continue
		}

		runtimeSession, err := m.attachExistingRuntime(record)
		if err != nil {
			m.logger.Error().Err(err).Str("sessionId", record.SessionID).Msg("failed to attach runtime during rehydrate")
			continue
		}

		go m.connectExisting(runtimeSession, record.SessionID)
	}

	return nil
}

func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for sessionID, runtime := range m.runtimes {
		runtime.client.Disconnect()
		delete(m.runtimes, sessionID)
	}

	return ctx.Err()
}

func (m *Manager) CreateSession(ctx context.Context, input CreateSessionInput) (*CreateSessionResult, error) {
	if strings.TrimSpace(input.SessionID) == "" {
		return nil, fmt.Errorf("sessionId is required")
	}

	if input.LoginMethod != store.LoginMethodQR && input.LoginMethod != store.LoginMethodPairCode {
		return nil, ErrLoginMethodInvalid
	}

	if _, err := m.sessions.Get(ctx, input.SessionID); err == nil {
		return nil, ErrSessionExists
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("get existing session: %w", err)
	}

	record, err := m.sessions.Create(ctx, store.CreateSessionParams{
		SessionID:   input.SessionID,
		Name:        strings.TrimSpace(input.Name),
		LoginMethod: input.LoginMethod,
		Phone:       strings.TrimSpace(input.Phone),
		Status:      store.SessionStatusInitializing,
	})
	if err != nil {
		return nil, fmt.Errorf("create session record: %w", err)
	}

	runtimeSession, err := m.attachNewRuntime(*record)
	if err != nil {
		return nil, err
	}

	m.launchPendingLogin(context.Background(), runtimeSession, record.SessionID)

	result := &CreateSessionResult{Session: record}

	if input.LoginMethod == store.LoginMethodPairCode {
		if strings.TrimSpace(input.Phone) == "" {
			return result, nil
		}

		pairCode, err := m.GeneratePairCode(ctx, input.SessionID, input.Phone)
		if err != nil {
			return nil, err
		}
		result.PairCode = pairCode
		return result, nil
	}

	qr, err := m.GetQRCode(ctx, input.SessionID)
	if err == nil {
		result.QRCode = qr
	}

	return result, nil
}

func (m *Manager) ListSessions(ctx context.Context) ([]store.Session, error) {
	return m.sessions.List(ctx)
}

func (m *Manager) GetSession(ctx context.Context, sessionID string) (*store.Session, error) {
	return m.sessions.Get(ctx, sessionID)
}

func (m *Manager) GetQRCode(ctx context.Context, sessionID string) (*QRCodeResult, error) {
	record, err := m.sessions.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(record.QRCode) == "" {
		return nil, ErrQRCodeUnavailable
	}

	return &QRCodeResult{
		SessionID: sessionID,
		QRCode:    record.QRCode,
		ExpiresAt: record.QRExpiresAt,
	}, nil
}

func (m *Manager) GeneratePairCode(ctx context.Context, sessionID, phone string) (*PairCodeResult, error) {
	phone = whatsapp.NormalizePhone(phone)
	if phone == "" {
		return nil, ErrPhoneRequired
	}

	record, err := m.sessions.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	runtimeSession, err := m.ensurePendingRuntime(record)
	if err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(20 * time.Second):
		return nil, ErrPairCodeNotAvailable
	case <-runtimeSession.readyCh:
	}

	code, err := runtimeSession.client.PairPhone(
		ctx,
		phone,
		false,
		whatsmeow.PairClientChrome,
		m.displayName,
	)
	if err != nil {
		return nil, fmt.Errorf("pair phone: %w", err)
	}

	updated, err := m.sessions.UpdateState(ctx, store.UpdateSessionStateParams{
		SessionID: record.SessionID,
		Status:    store.SessionStatusPairing,
		Phone:     phone,
		QRCode:    nil,
		LastError: "",
	})
	if err == nil {
		record = updated
	}

	return &PairCodeResult{
		SessionID:   record.SessionID,
		Phone:       record.Phone,
		PairingCode: code,
	}, nil
}

func (m *Manager) OpenQRCode(ctx context.Context, sessionID string) (*OpenQRSessionResult, error) {
	record, err := m.sessions.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if record.DeletedAt != nil || record.Status == store.SessionStatusDeleted {
		return nil, store.ErrNotFound
	}

	if m.isSessionConnected(record) {
		return nil, ErrSessionConnected
	}

	if record.Status == store.SessionStatusQRReady && strings.TrimSpace(record.QRCode) != "" {
		if record.QRExpiresAt == nil || record.QRExpiresAt.After(time.Now().UTC()) {
			return &OpenQRSessionResult{
				Session: record,
				QRCode: &QRCodeResult{
					SessionID: record.SessionID,
					QRCode:    record.QRCode,
					ExpiresAt: record.QRExpiresAt,
				},
			}, nil
		}
	}

	if err := m.disconnectRuntime(sessionID); err != nil {
		return nil, err
	}
	if err := m.deleteStoredDevice(ctx, record.DeviceJID); err != nil {
		return nil, err
	}

	record, err = m.sessions.ResetForQRCode(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("reset session for qr: %w", err)
	}

	runtimeSession, err := m.attachNewRuntime(*record)
	if err != nil {
		return nil, err
	}

	m.launchPendingLogin(context.Background(), runtimeSession, record.SessionID)

	result := &OpenQRSessionResult{
		Session: record,
	}
	if qr, err := m.GetQRCode(ctx, sessionID); err == nil {
		result.QRCode = qr
	}

	return result, nil
}

func (m *Manager) ReconnectSession(ctx context.Context, sessionID string) (*store.Session, error) {
	record, err := m.sessions.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	if err := m.disconnectRuntime(sessionID); err != nil {
		return nil, err
	}

	var runtimeSession *runtimeSession
	if record.DeviceJID != "" {
		runtimeSession, err = m.attachExistingRuntime(*record)
		if err != nil {
			return nil, err
		}
		go m.connectExisting(runtimeSession, record.SessionID)
	} else {
		runtimeSession, err = m.attachNewRuntime(*record)
		if err != nil {
			return nil, err
		}
		m.launchPendingLogin(context.Background(), runtimeSession, record.SessionID)
	}

	updated, updateErr := m.sessions.UpdateState(ctx, store.UpdateSessionStateParams{
		SessionID: record.SessionID,
		Status:    store.SessionStatusInitializing,
		Phone:     record.Phone,
		LastError: "",
	})
	if updateErr == nil {
		record = updated
	}

	_ = runtimeSession
	return record, nil
}

func (m *Manager) DeleteSession(ctx context.Context, sessionID string) error {
	if err := m.disconnectRuntime(sessionID); err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	return m.sessions.MarkDeleted(ctx, sessionID)
}

func (m *Manager) VerifyNumber(ctx context.Context, sessionID, phone string) (*VerifyNumberResult, error) {
	runtimeSession, record, err := m.requireConnected(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	normalized := whatsapp.NormalizePhone(phone)
	if normalized == "" {
		return nil, ErrPhoneRequired
	}

	results, err := runtimeSession.client.IsOnWhatsApp(ctx, []string{normalized})
	if err != nil {
		return nil, fmt.Errorf("verify phone on whatsapp: %w", err)
	}
	if len(results) == 0 {
		return &VerifyNumberResult{
			SessionID: record.SessionID,
			Phone:     normalized,
			Exists:    false,
		}, nil
	}

	response := results[0]
	result := &VerifyNumberResult{
		SessionID: record.SessionID,
		Phone:     normalized,
		Exists:    response.IsIn,
	}
	if response.IsIn {
		result.JID = response.JID.String()
	}
	if response.VerifiedName != nil {
		result.VerifiedName = response.VerifiedName.Details.GetVerifiedName()
	}

	return result, nil
}

func (m *Manager) SendText(ctx context.Context, sessionID, to, text string) (*messages.SendResult, error) {
	runtimeSession, record, err := m.requireConnected(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("text is required")
	}

	target, err := whatsapp.ParseTargetJID(to)
	if err != nil {
		return nil, err
	}
	target, err = m.resolveDirectTarget(ctx, runtimeSession, target)
	if err != nil {
		return nil, err
	}

	message := whatsapp.NewTextMessage(text)
	response, err := runtimeSession.client.SendMessage(ctx, target, message)
	if err != nil {
		return nil, fmt.Errorf("send text message: %w", err)
	}

	return m.persistOutboundMessage(record.SessionID, runtimeSession, target, response.ID, response.Timestamp, message)
}

func (m *Manager) SendMedia(ctx context.Context, sessionID, to, caption, fileName, mimeType string, contents []byte) (*messages.SendResult, error) {
	runtimeSession, record, err := m.requireConnected(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	target, err := whatsapp.ParseTargetJID(to)
	if err != nil {
		return nil, err
	}
	target, err = m.resolveDirectTarget(ctx, runtimeSession, target)
	if err != nil {
		return nil, err
	}

	upload, err := runtimeSession.client.Upload(ctx, contents, whatsmeow.MediaType(whatsapp.DetectMediaTypeFromMime(mimeType)))
	if err != nil {
		return nil, fmt.Errorf("upload media: %w", err)
	}

	mediaMessage, err := whatsapp.BuildMediaMessage(upload, mimeType, fileName, caption)
	if err != nil {
		return nil, err
	}

	response, err := runtimeSession.client.SendMessage(ctx, target, mediaMessage.Message)
	if err != nil {
		return nil, fmt.Errorf("send media message: %w", err)
	}

	return m.persistOutboundMessage(record.SessionID, runtimeSession, target, response.ID, response.Timestamp, mediaMessage.Message)
}

func (m *Manager) Reply(ctx context.Context, sessionID, chatJID, messageID, text string) (*messages.SendResult, error) {
	runtimeSession, record, err := m.requireConnected(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("text is required")
	}

	targetMessage, err := m.messages.GetByExternalID(ctx, sessionID, messageID)
	if err != nil {
		return nil, err
	}

	targetChat, err := whatsapp.MustParseJID(chatJID)
	if err != nil {
		return nil, err
	}
	targetChat, err = m.resolveDirectTarget(ctx, runtimeSession, targetChat)
	if err != nil {
		return nil, err
	}

	message := whatsapp.NewReplyMessage(text, targetMessage.ChatJID, targetMessage.SenderJID, targetMessage.ExternalMessageID)
	response, err := runtimeSession.client.SendMessage(ctx, targetChat, message)
	if err != nil {
		return nil, fmt.Errorf("reply to message: %w", err)
	}

	return m.persistOutboundMessage(record.SessionID, runtimeSession, targetChat, response.ID, response.Timestamp, message)
}

func (m *Manager) Edit(ctx context.Context, sessionID, chatJID, messageID, text string) (*messages.SendResult, error) {
	runtimeSession, record, err := m.requireConnected(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("text is required")
	}

	targetChat, err := whatsapp.MustParseJID(chatJID)
	if err != nil {
		return nil, err
	}
	targetChat, err = m.resolveDirectTarget(ctx, runtimeSession, targetChat)
	if err != nil {
		return nil, err
	}

	editMessage := runtimeSession.client.BuildEdit(targetChat, types.MessageID(messageID), whatsapp.NewTextMessage(text))
	response, err := runtimeSession.client.SendMessage(ctx, targetChat, editMessage)
	if err != nil {
		return nil, fmt.Errorf("edit message: %w", err)
	}

	if err := m.messages.UpdateStatus(ctx, sessionID, []string{messageID}, store.MessageStatusEdited); err != nil {
		m.logger.Warn().Err(err).Str("sessionId", sessionID).Str("messageId", messageID).Msg("failed to mark original message as edited")
	}

	return m.persistOutboundMessage(record.SessionID, runtimeSession, targetChat, response.ID, response.Timestamp, editMessage)
}

func (m *Manager) persistOutboundMessage(sessionID string, runtimeSession *runtimeSession, target types.JID, messageID types.MessageID, timestamp time.Time, message *waE2E.Message) (*messages.SendResult, error) {
	if timestamp.IsZero() {
		timestamp = time.Now().UTC()
	}

	senderJID := ""
	if runtimeSession.client.Store != nil && runtimeSession.client.Store.ID != nil {
		senderJID = runtimeSession.client.Store.ID.ToNonAD().String()
	}

	mediaMimeType, mediaFileName := whatsapp.MediaMetadata(message)
	savedMessage, err := m.messages.Save(context.Background(), store.SaveMessageParams{
		SessionID:         sessionID,
		ExternalMessageID: string(messageID),
		ChatJID:           target.String(),
		SenderJID:         senderJID,
		Direction:         store.MessageDirectionOutbound,
		MessageType:       whatsapp.DetectMessageType(message),
		Text:              whatsapp.ExtractText(message),
		MediaMimeType:     mediaMimeType,
		MediaFileName:     mediaFileName,
		Status:            store.MessageStatusSent,
		Payload:           whatsapp.MarshalMessage(message),
		MessageTimestamp:  timestamp,
	})
	if err != nil {
		return nil, fmt.Errorf("save outbound message: %w", err)
	}

	_ = m.webhooks.Emit(context.Background(), sessionID, "message.sent", savedMessage)

	return &messages.SendResult{
		SessionID:   sessionID,
		MessageID:   savedMessage.ExternalMessageID,
		ChatJID:     savedMessage.ChatJID,
		Recipient:   target.String(),
		SenderJID:   savedMessage.SenderJID,
		MessageType: savedMessage.MessageType,
		Status:      savedMessage.Status,
	}, nil
}

func (m *Manager) preferredDirectTarget(ctx context.Context, runtimeSession *runtimeSession, target types.JID) types.JID {
	target = target.ToNonAD()
	if runtimeSession == nil || runtimeSession.client == nil || runtimeSession.client.Store == nil {
		return target
	}
	if target.Server != types.HiddenUserServer {
		return target
	}

	alt, err := runtimeSession.client.Store.GetAltJID(ctx, target)
	if err != nil {
		m.logger.Warn().Err(err).Str("sessionId", runtimeSession.sessionID).Str("target", target.String()).Msg("failed to resolve alternate jid")
		return target
	}
	if alt.IsEmpty() {
		return target
	}

	alt = alt.ToNonAD()
	if alt.Server == types.DefaultUserServer {
		return alt
	}
	return target
}

func (m *Manager) resolveDirectTarget(ctx context.Context, runtimeSession *runtimeSession, target types.JID) (types.JID, error) {
	target = m.preferredDirectTarget(ctx, runtimeSession, target)
	if runtimeSession == nil || runtimeSession.client == nil {
		return target, nil
	}
	if target.Server != types.DefaultUserServer || target.Device > 0 {
		return target, nil
	}

	candidates := whatsapp.CandidatePhones(target.User)
	if len(candidates) == 0 {
		return target, nil
	}

	results, err := runtimeSession.client.IsOnWhatsApp(ctx, candidates)
	if err != nil {
		return target, fmt.Errorf("verify target on whatsapp: %w", err)
	}

	var fallback types.JID
	for _, result := range results {
		if !result.IsIn || result.JID.IsEmpty() {
			continue
		}
		candidate := result.JID.ToNonAD()
		if candidate.User == target.User {
			return candidate, nil
		}
		if fallback.IsEmpty() {
			fallback = candidate
		}
	}

	if !fallback.IsEmpty() {
		m.logger.Info().
			Str("sessionId", runtimeSession.sessionID).
			Str("requestedTarget", target.String()).
			Str("resolvedTarget", fallback.String()).
			Msg("resolved direct target using WhatsApp number variant lookup")
		return fallback, nil
	}

	return types.EmptyJID, fmt.Errorf("%w: %s", ErrNumberNotOnWhatsApp, target.String())
}

func (m *Manager) attachNewRuntime(record store.Session) (*runtimeSession, error) {
	if m.container == nil {
		return nil, fmt.Errorf("whatsmeow container is not configured")
	}
	device := m.container.NewDevice()
	runtimeSession := m.newRuntime(record, device)

	m.mu.Lock()
	m.runtimes[record.SessionID] = runtimeSession
	m.mu.Unlock()

	return runtimeSession, nil
}

func (m *Manager) attachExistingRuntime(record store.Session) (*runtimeSession, error) {
	if m.container == nil {
		return nil, fmt.Errorf("whatsmeow container is not configured")
	}
	deviceJID, err := types.ParseJID(record.DeviceJID)
	if err != nil {
		return nil, fmt.Errorf("parse stored device jid: %w", err)
	}

	device, err := m.container.GetDevice(context.Background(), deviceJID)
	if err != nil {
		return nil, fmt.Errorf("get existing device: %w", err)
	}
	if device == nil {
		return nil, fmt.Errorf("device not found for session %s", record.SessionID)
	}

	runtimeSession := m.newRuntime(record, device)
	runtimeSession.readyOnce.Do(func() {
		close(runtimeSession.readyCh)
	})

	m.mu.Lock()
	m.runtimes[record.SessionID] = runtimeSession
	m.mu.Unlock()

	return runtimeSession, nil
}

func (m *Manager) newRuntime(record store.Session, device *waStore.Device) *runtimeSession {
	client := whatsmeow.NewClient(device, whatsapp.NewLogger(m.logger.With().Str("sessionId", record.SessionID).Logger()))
	runtimeSession := &runtimeSession{
		sessionID:   record.SessionID,
		loginMethod: record.LoginMethod,
		client:      client,
		device:      device,
		readyCh:     make(chan struct{}),
	}

	client.AddEventHandler(func(evt interface{}) {
		m.handleEvent(record.SessionID, runtimeSession, evt)
	})

	return runtimeSession
}

func (m *Manager) startPendingLogin(ctx context.Context, runtimeSession *runtimeSession, sessionID string) {
	qrChan, err := runtimeSession.client.GetQRChannel(ctx)
	if err != nil {
		m.setSessionError(sessionID, fmt.Errorf("get qr channel: %w", err))
		return
	}

	go m.consumeQRChannel(sessionID, runtimeSession, qrChan)

	if err := runtimeSession.client.Connect(); err != nil {
		m.setSessionError(sessionID, fmt.Errorf("connect new session: %w", err))
		return
	}
}

func (m *Manager) connectExisting(runtimeSession *runtimeSession, sessionID string) {
	if err := runtimeSession.client.Connect(); err != nil {
		m.setSessionError(sessionID, fmt.Errorf("connect existing session: %w", err))
	}
}

func (m *Manager) consumeQRChannel(sessionID string, runtimeSession *runtimeSession, qrChan <-chan whatsmeow.QRChannelItem) {
	for item := range qrChan {
		switch item.Event {
		case "code":
			runtimeSession.readyOnce.Do(func() {
				close(runtimeSession.readyCh)
			})

			var qrCode *string
			if runtimeSession.loginMethod == store.LoginMethodQR {
				qrCode = &item.Code
			}

			expiresAt := time.Now().UTC().Add(20 * time.Second)
			status := store.SessionStatusQRReady
			if runtimeSession.loginMethod == store.LoginMethodPairCode {
				status = store.SessionStatusPairing
			}

			record, err := m.sessions.UpdateState(context.Background(), store.UpdateSessionStateParams{
				SessionID:   sessionID,
				Status:      status,
				QRCode:      qrCode,
				QRExpiresAt: &expiresAt,
				LastError:   "",
			})
			if err != nil {
				m.logger.Error().Err(err).Str("sessionId", sessionID).Msg("failed to update qr state")
				continue
			}

			if runtimeSession.loginMethod == store.LoginMethodQR {
				_ = m.webhooks.Emit(context.Background(), sessionID, "session.qr_updated", QRCodeResult{
					SessionID: sessionID,
					QRCode:    record.QRCode,
					ExpiresAt: record.QRExpiresAt,
				})
			}
		case "timeout":
			m.setSessionError(sessionID, fmt.Errorf("pairing QR timed out"))
		case "error":
			m.setSessionError(sessionID, item.Error)
		}
	}
}

func (m *Manager) handleEvent(sessionID string, runtimeSession *runtimeSession, evt interface{}) {
	if runtimeSession != nil && !m.isCurrentRuntime(sessionID, runtimeSession) {
		return
	}

	switch event := evt.(type) {
	case *events.Connected:
		_ = runtimeSession.client.SendPresence(context.Background(), types.PresenceAvailable)

		deviceJID := ""
		phone := ""
		if runtimeSession.client.Store != nil && runtimeSession.client.Store.ID != nil {
			deviceJID = runtimeSession.client.Store.ID.String()
			phone = "+" + runtimeSession.client.Store.ID.User
		}

		record, err := m.sessions.UpdateState(context.Background(), store.UpdateSessionStateParams{
			SessionID: sessionID,
			Status:    store.SessionStatusConnected,
			Phone:     phone,
			DeviceJID: deviceJID,
			QRCode:    nil,
			LastError: "",
		})
		if err != nil {
			m.logger.Error().Err(err).Str("sessionId", sessionID).Msg("failed to mark session connected")
			return
		}

		_ = m.webhooks.Emit(context.Background(), sessionID, "session.connected", record)
	case *events.Disconnected:
		record, err := m.sessions.UpdateState(context.Background(), store.UpdateSessionStateParams{
			SessionID: sessionID,
			Status:    store.SessionStatusDisconnected,
			QRCode:    nil,
			LastError: "",
		})
		if err == nil {
			_ = m.webhooks.Emit(context.Background(), sessionID, "session.disconnected", record)
		}
	case *events.LoggedOut:
		_, err := m.sessions.UpdateState(context.Background(), store.UpdateSessionStateParams{
			SessionID: sessionID,
			Status:    store.SessionStatusLoggedOut,
			QRCode:    nil,
			LastError: event.Reason.String(),
		})
		if err != nil {
			m.logger.Error().Err(err).Str("sessionId", sessionID).Msg("failed to mark session logged out")
		}
		m.mu.Lock()
		delete(m.runtimes, sessionID)
		m.mu.Unlock()
	case *events.Message:
		m.persistIncomingEvent(sessionID, event)
	case *events.Receipt:
		status := store.MessageStatus(whatsapp.ResolveReceiptStatus(event.Type))
		messageIDs := make([]string, 0, len(event.MessageIDs))
		for _, messageID := range event.MessageIDs {
			messageIDs = append(messageIDs, string(messageID))
		}
		if err := m.messages.UpdateStatus(context.Background(), sessionID, messageIDs, status); err != nil {
			m.logger.Warn().Err(err).Str("sessionId", sessionID).Msg("failed to update receipt status")
			return
		}
		_ = m.webhooks.Emit(context.Background(), sessionID, "message.status_updated", map[string]interface{}{
			"sessionId":  sessionID,
			"messageIds": messageIDs,
			"status":     status,
			"timestamp":  event.Timestamp,
		})
	}
}

func (m *Manager) persistIncomingEvent(sessionID string, event *events.Message) {
	message := event.UnwrapRaw()
	payload := whatsapp.MarshalMessage(message.Message)
	text := whatsapp.ExtractText(message.Message)
	messageType := whatsapp.DetectMessageType(message.Message)
	mediaMimeType, mediaFileName := whatsapp.MediaMetadata(message.Message)
	direction := store.MessageDirectionInbound
	status := store.MessageStatusReceived
	if message.Info.IsFromMe {
		direction = store.MessageDirectionOutbound
		status = store.MessageStatusSent
	}

	record, err := m.messages.Save(context.Background(), store.SaveMessageParams{
		SessionID:         sessionID,
		ExternalMessageID: string(message.Info.ID),
		ChatJID:           message.Info.Chat.String(),
		SenderJID:         message.Info.Sender.String(),
		Direction:         direction,
		MessageType:       messageType,
		Text:              text,
		MediaMimeType:     mediaMimeType,
		MediaFileName:     mediaFileName,
		Status:            status,
		Payload:           payload,
		MessageTimestamp:  message.Info.Timestamp,
	})
	if err != nil {
		m.logger.Warn().Err(err).Str("sessionId", sessionID).Msg("failed to persist incoming message")
		return
	}

	eventName := "message.received"
	if record.Direction == store.MessageDirectionOutbound {
		eventName = "message.sent"
	}
	_ = m.webhooks.Emit(context.Background(), sessionID, eventName, record)
}

func (m *Manager) setSessionError(sessionID string, err error) {
	if err == nil {
		return
	}

	_, updateErr := m.sessions.UpdateState(context.Background(), store.UpdateSessionStateParams{
		SessionID: sessionID,
		Status:    store.SessionStatusError,
		QRCode:    nil,
		LastError: err.Error(),
	})
	if updateErr != nil {
		m.logger.Error().Err(updateErr).Str("sessionId", sessionID).Msg("failed to persist session error")
	}
}

func (m *Manager) ensurePendingRuntime(record *store.Session) (*runtimeSession, error) {
	m.mu.RLock()
	runtimeSession := m.runtimes[record.SessionID]
	m.mu.RUnlock()
	if runtimeSession != nil {
		return runtimeSession, nil
	}

	var err error
	if record.DeviceJID != "" {
		runtimeSession, err = m.attachExistingRuntime(*record)
		if err != nil {
			return nil, err
		}
		go m.connectExisting(runtimeSession, record.SessionID)
		return runtimeSession, nil
	}

	runtimeSession, err = m.attachNewRuntime(*record)
	if err != nil {
		return nil, err
	}
	m.launchPendingLogin(context.Background(), runtimeSession, record.SessionID)

	return runtimeSession, nil
}

func (m *Manager) isCurrentRuntime(sessionID string, runtimeSession *runtimeSession) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	current := m.runtimes[sessionID]
	return current == runtimeSession
}

func (m *Manager) isSessionConnected(record *store.Session) bool {
	if record == nil {
		return false
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	runtimeSession := m.runtimes[record.SessionID]
	if runtimeSession == nil {
		return false
	}

	return record.Status == store.SessionStatusConnected || runtimeSession.client.IsConnected()
}

func (m *Manager) deleteStoredDevice(ctx context.Context, deviceJID string) error {
	deviceJID = strings.TrimSpace(deviceJID)
	if deviceJID == "" || m.container == nil {
		return nil
	}

	jid, err := types.ParseJID(deviceJID)
	if err != nil {
		m.logger.Warn().Err(err).Str("deviceJid", deviceJID).Msg("failed to parse stored device jid while reopening qr")
		return nil
	}

	device, err := m.container.GetDevice(ctx, jid)
	if err != nil {
		return fmt.Errorf("get stored device: %w", err)
	}
	if device == nil {
		return nil
	}

	if err := device.Delete(ctx); err != nil {
		return fmt.Errorf("delete stored device: %w", err)
	}

	return nil
}

func (m *Manager) requireConnected(ctx context.Context, sessionID string) (*runtimeSession, *store.Session, error) {
	record, err := m.sessions.Get(ctx, sessionID)
	if err != nil {
		return nil, nil, err
	}

	m.mu.RLock()
	runtimeSession := m.runtimes[sessionID]
	m.mu.RUnlock()
	if runtimeSession == nil || !runtimeSession.client.IsConnected() {
		return nil, nil, ErrSessionNotConnected
	}

	return runtimeSession, record, nil
}

func (m *Manager) disconnectRuntime(sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	runtimeSession, ok := m.runtimes[sessionID]
	if !ok {
		return nil
	}

	runtimeSession.client.Disconnect()
	delete(m.runtimes, sessionID)
	return nil
}
