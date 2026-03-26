package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
)

type SaveMessageParams struct {
	SessionID         string
	ExternalMessageID string
	ChatJID           string
	SenderJID         string
	Direction         MessageDirection
	MessageType       string
	Text              string
	MediaMimeType     string
	MediaFileName     string
	Status            MessageStatus
	Payload           json.RawMessage
	MessageTimestamp  time.Time
}

type ListMessagesParams struct {
	SessionID string
	ChatJID   string
	Limit     int
	Cursor    int64
}

type MessageRepository struct {
	db *sql.DB
}

func NewMessageRepository(db *sql.DB) *MessageRepository {
	return &MessageRepository{db: db}
}

func (r *MessageRepository) Save(ctx context.Context, params SaveMessageParams) (*Message, error) {
	row := r.db.QueryRowContext(
		ctx,
		`INSERT INTO messages (
			session_id, external_message_id, chat_jid, sender_jid, direction,
			message_type, text, media_mime_type, media_file_name, status, payload, message_timestamp
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (session_id, external_message_id)
		DO UPDATE SET
			chat_jid = EXCLUDED.chat_jid,
			sender_jid = EXCLUDED.sender_jid,
			direction = EXCLUDED.direction,
			message_type = EXCLUDED.message_type,
			text = EXCLUDED.text,
			media_mime_type = EXCLUDED.media_mime_type,
			media_file_name = EXCLUDED.media_file_name,
			status = EXCLUDED.status,
			payload = EXCLUDED.payload,
			message_timestamp = EXCLUDED.message_timestamp,
			updated_at = NOW()
		RETURNING id, session_id, external_message_id, chat_jid, sender_jid, direction,
		          message_type, text, media_mime_type, media_file_name, status,
		          payload, message_timestamp, created_at, updated_at`,
		params.SessionID,
		params.ExternalMessageID,
		params.ChatJID,
		params.SenderJID,
		params.Direction,
		params.MessageType,
		params.Text,
		params.MediaMimeType,
		params.MediaFileName,
		params.Status,
		params.Payload,
		params.MessageTimestamp,
	)

	message, err := scanMessage(row)
	if err != nil {
		return nil, fmt.Errorf("save message: %w", err)
	}

	return message, nil
}

func (r *MessageRepository) GetByExternalID(ctx context.Context, sessionID, externalMessageID string) (*Message, error) {
	row := r.db.QueryRowContext(
		ctx,
		`SELECT id, session_id, external_message_id, chat_jid, sender_jid, direction,
		        message_type, text, media_mime_type, media_file_name, status,
		        payload, message_timestamp, created_at, updated_at
		 FROM messages
		 WHERE session_id = $1 AND external_message_id = $2`,
		sessionID,
		externalMessageID,
	)

	message, err := scanMessage(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get message by external id: %w", err)
	}

	return message, nil
}

func (r *MessageRepository) UpdateStatus(ctx context.Context, sessionID string, messageIDs []string, status MessageStatus) error {
	if len(messageIDs) == 0 {
		return nil
	}

	query := `UPDATE messages
	          SET status = $3, updated_at = NOW()
	          WHERE session_id = $1 AND external_message_id = ANY($2)`

	_, err := r.db.ExecContext(ctx, query, sessionID, pqStringArray(messageIDs), status)
	if err != nil {
		return fmt.Errorf("update message status: %w", err)
	}

	return nil
}

func (r *MessageRepository) List(ctx context.Context, params ListMessagesParams) ([]Message, error) {
	limit := params.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	var (
		rows *sql.Rows
		err  error
	)

	if strings.TrimSpace(params.ChatJID) == "" {
		if params.Cursor > 0 {
			rows, err = r.db.QueryContext(
				ctx,
				`SELECT id, session_id, external_message_id, chat_jid, sender_jid, direction,
				        message_type, text, media_mime_type, media_file_name, status,
				        payload, message_timestamp, created_at, updated_at
				 FROM messages
				 WHERE session_id = $1 AND id < $2
				 ORDER BY id DESC
				 LIMIT $3`,
				params.SessionID,
				params.Cursor,
				limit,
			)
		} else {
			rows, err = r.db.QueryContext(
				ctx,
				`SELECT id, session_id, external_message_id, chat_jid, sender_jid, direction,
				        message_type, text, media_mime_type, media_file_name, status,
				        payload, message_timestamp, created_at, updated_at
				 FROM messages
				 WHERE session_id = $1
				 ORDER BY id DESC
				 LIMIT $2`,
				params.SessionID,
				limit,
			)
		}
	} else if params.Cursor > 0 {
		canonicalChatExpr := canonicalChatJIDExpr("chat_jid")
		canonicalInputExpr := canonicalChatJIDExpr("$2")
		rows, err = r.db.QueryContext(
			ctx,
			`SELECT id, session_id, external_message_id, chat_jid, sender_jid, direction,
			        message_type, text, media_mime_type, media_file_name, status,
			        payload, message_timestamp, created_at, updated_at
			 FROM messages
			 WHERE session_id = $1 AND (`+canonicalChatExpr+` = `+canonicalInputExpr+` OR chat_jid = $2) AND id < $3
			 ORDER BY id DESC
			 LIMIT $4`,
			params.SessionID,
			params.ChatJID,
			params.Cursor,
			limit,
		)
	} else {
		canonicalChatExpr := canonicalChatJIDExpr("chat_jid")
		canonicalInputExpr := canonicalChatJIDExpr("$2")
		rows, err = r.db.QueryContext(
			ctx,
			`SELECT id, session_id, external_message_id, chat_jid, sender_jid, direction,
			        message_type, text, media_mime_type, media_file_name, status,
			        payload, message_timestamp, created_at, updated_at
			 FROM messages
			 WHERE session_id = $1 AND (`+canonicalChatExpr+` = `+canonicalInputExpr+` OR chat_jid = $2)
			 ORDER BY id DESC
			 LIMIT $3`,
			params.SessionID,
			params.ChatJID,
			limit,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()

	messages := make([]Message, 0)
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		messages = append(messages, *message)
	}

	return messages, rows.Err()
}

func scanMessage(scanner interface {
	Scan(dest ...interface{}) error
}) (*Message, error) {
	var message Message
	var payload []byte
	err := scanner.Scan(
		&message.ID,
		&message.SessionID,
		&message.ExternalMessageID,
		&message.ChatJID,
		&message.SenderJID,
		&message.Direction,
		&message.MessageType,
		&message.Text,
		&message.MediaMimeType,
		&message.MediaFileName,
		&message.Status,
		&payload,
		&message.MessageTimestamp,
		&message.CreatedAt,
		&message.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	message.Payload = payload
	return &message, nil
}

func pqStringArray(values []string) interface{} {
	return pq.Array(values)
}
