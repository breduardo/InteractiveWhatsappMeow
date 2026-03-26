package messages

import (
	"context"
	"fmt"

	"interactivewhatsmeow/internal/store"
)

type Sender interface {
	SendText(ctx context.Context, sessionID, to, text string) (*SendResult, error)
	SendMedia(ctx context.Context, sessionID, to, caption, fileName, mimeType string, contents []byte) (*SendResult, error)
	Reply(ctx context.Context, sessionID, chatJID, messageID, text string) (*SendResult, error)
	Edit(ctx context.Context, sessionID, chatJID, messageID, text string) (*SendResult, error)
}

type Repository interface {
	List(ctx context.Context, params store.ListMessagesParams) ([]store.Message, error)
}

type Service struct {
	sender Sender
	repo   Repository
}

func NewService(sender Sender, repo Repository) *Service {
	return &Service{
		sender: sender,
		repo:   repo,
	}
}

type SendResult struct {
	SessionID   string              `json:"sessionId"`
	MessageID   string              `json:"messageId"`
	ChatJID     string              `json:"chatJid"`
	Recipient   string              `json:"recipient"`
	SenderJID   string              `json:"senderJid"`
	MessageType string              `json:"messageType"`
	Status      store.MessageStatus `json:"status"`
}

type ListInput struct {
	SessionID string
	ChatJID   string
	Limit     int
	Cursor    int64
}

func (s *Service) SendText(ctx context.Context, sessionID, to, text string) (*SendResult, error) {
	result, err := s.sender.SendText(ctx, sessionID, to, text)
	if err != nil {
		return nil, fmt.Errorf("send text: %w", err)
	}
	return result, nil
}

func (s *Service) SendMedia(ctx context.Context, sessionID, to, caption, fileName, mimeType string, contents []byte) (*SendResult, error) {
	result, err := s.sender.SendMedia(ctx, sessionID, to, caption, fileName, mimeType, contents)
	if err != nil {
		return nil, fmt.Errorf("send media: %w", err)
	}
	return result, nil
}

func (s *Service) Reply(ctx context.Context, sessionID, chatJID, messageID, text string) (*SendResult, error) {
	result, err := s.sender.Reply(ctx, sessionID, chatJID, messageID, text)
	if err != nil {
		return nil, fmt.Errorf("reply message: %w", err)
	}
	return result, nil
}

func (s *Service) Edit(ctx context.Context, sessionID, chatJID, messageID, text string) (*SendResult, error) {
	result, err := s.sender.Edit(ctx, sessionID, chatJID, messageID, text)
	if err != nil {
		return nil, fmt.Errorf("edit message: %w", err)
	}
	return result, nil
}

func (s *Service) List(ctx context.Context, input ListInput) ([]store.Message, error) {
	messages, err := s.repo.List(ctx, store.ListMessagesParams{
		SessionID: input.SessionID,
		ChatJID:   input.ChatJID,
		Limit:     input.Limit,
		Cursor:    input.Cursor,
	})
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	return messages, nil
}
