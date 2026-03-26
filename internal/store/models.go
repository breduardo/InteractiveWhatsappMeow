package store

import (
	"encoding/json"
	"errors"
	"time"
)

var ErrNotFound = errors.New("record not found")

type LoginMethod string

const (
	LoginMethodQR       LoginMethod = "qr"
	LoginMethodPairCode LoginMethod = "pair_code"
)

type SessionStatus string

const (
	SessionStatusInitializing SessionStatus = "initializing"
	SessionStatusQRReady      SessionStatus = "qr_ready"
	SessionStatusPairing      SessionStatus = "pairing"
	SessionStatusConnected    SessionStatus = "connected"
	SessionStatusDisconnected SessionStatus = "disconnected"
	SessionStatusLoggedOut    SessionStatus = "logged_out"
	SessionStatusDeleted      SessionStatus = "deleted"
	SessionStatusError        SessionStatus = "error"
)

type MessageDirection string

const (
	MessageDirectionInbound  MessageDirection = "inbound"
	MessageDirectionOutbound MessageDirection = "outbound"
)

type MessageStatus string

const (
	MessageStatusReceived  MessageStatus = "received"
	MessageStatusSent      MessageStatus = "sent"
	MessageStatusDelivered MessageStatus = "delivered"
	MessageStatusRead      MessageStatus = "read"
	MessageStatusEdited    MessageStatus = "edited"
	MessageStatusFailed    MessageStatus = "failed"
)

type Session struct {
	ID          int64         `json:"-"`
	SessionID   string        `json:"sessionId"`
	Name        string        `json:"name"`
	LoginMethod LoginMethod   `json:"loginMethod"`
	Phone       string        `json:"phone"`
	DeviceJID   string        `json:"deviceJid,omitempty"`
	Status      SessionStatus `json:"status"`
	QRCode      string        `json:"qrCode,omitempty"`
	QRExpiresAt *time.Time    `json:"qrExpiresAt,omitempty"`
	LastError   string        `json:"lastError,omitempty"`
	CreatedAt   time.Time     `json:"createdAt"`
	UpdatedAt   time.Time     `json:"updatedAt"`
	DeletedAt   *time.Time    `json:"deletedAt,omitempty"`
}

type Message struct {
	ID                int64            `json:"id"`
	SessionID         string           `json:"sessionId"`
	ExternalMessageID string           `json:"messageId"`
	ChatJID           string           `json:"chatJid"`
	SenderJID         string           `json:"senderJid"`
	Direction         MessageDirection `json:"direction"`
	MessageType       string           `json:"messageType"`
	Text              string           `json:"text"`
	MediaMimeType     string           `json:"mediaMimeType,omitempty"`
	MediaFileName     string           `json:"mediaFileName,omitempty"`
	Status            MessageStatus    `json:"status"`
	Payload           json.RawMessage  `json:"payload,omitempty"`
	MessageTimestamp  time.Time        `json:"messageTimestamp"`
	CreatedAt         time.Time        `json:"createdAt"`
	UpdatedAt         time.Time        `json:"updatedAt"`
}

type Webhook struct {
	ID        int64     `json:"id"`
	SessionID *string   `json:"sessionId,omitempty"`
	URL       string    `json:"url"`
	Events    []string  `json:"events"`
	IsActive  bool      `json:"isActive"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type WebhookDelivery struct {
	ID               int64           `json:"id"`
	WebhookID        int64           `json:"webhookId"`
	SessionID        string          `json:"sessionId"`
	EventType        string          `json:"eventType"`
	Payload          json.RawMessage `json:"payload"`
	Status           string          `json:"status"`
	AttemptCount     int             `json:"attemptCount"`
	NextAttemptAt    time.Time       `json:"nextAttemptAt"`
	LastError        string          `json:"lastError,omitempty"`
	LastHTTPStatus   *int            `json:"lastHttpStatus,omitempty"`
	LastResponseBody string          `json:"lastResponseBody,omitempty"`
	DeliveredAt      *time.Time      `json:"deliveredAt,omitempty"`
	CreatedAt        time.Time       `json:"createdAt"`
	UpdatedAt        time.Time       `json:"updatedAt"`
	WebhookURL       string          `json:"-"`
}

type WebhookEnvelope struct {
	Event     string      `json:"event"`
	SessionID string      `json:"sessionId"`
	Timestamp time.Time   `json:"timestamp"`
	Data      interface{} `json:"data"`
}
