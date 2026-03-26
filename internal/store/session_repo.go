package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type CreateSessionParams struct {
	SessionID   string
	Name        string
	LoginMethod LoginMethod
	Phone       string
	Status      SessionStatus
}

type UpdateSessionStateParams struct {
	SessionID   string
	Status      SessionStatus
	Phone       string
	DeviceJID   string
	QRCode      *string
	QRExpiresAt *time.Time
	LastError   string
}

type SessionRepository struct {
	db *sql.DB
}

func NewSessionRepository(db *sql.DB) *SessionRepository {
	return &SessionRepository{db: db}
}

func (r *SessionRepository) Create(ctx context.Context, params CreateSessionParams) (*Session, error) {
	row := r.db.QueryRowContext(
		ctx,
		`INSERT INTO sessions (
			session_id, name, login_method, phone, status
		)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, session_id, name, login_method, phone, COALESCE(device_jid, ''), status,
		          COALESCE(qr_code, ''), qr_expires_at, COALESCE(last_error, ''),
		          created_at, updated_at, deleted_at`,
		params.SessionID,
		params.Name,
		params.LoginMethod,
		params.Phone,
		params.Status,
	)

	session, err := scanSession(row)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	return session, nil
}

func (r *SessionRepository) List(ctx context.Context) ([]Session, error) {
	rows, err := r.db.QueryContext(
		ctx,
		`SELECT id, session_id, name, login_method, phone, COALESCE(device_jid, ''), status,
		        COALESCE(qr_code, ''), qr_expires_at, COALESCE(last_error, ''),
		        created_at, updated_at, deleted_at
		 FROM sessions
		 WHERE deleted_at IS NULL
		 ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	sessions := make([]Session, 0)
	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		sessions = append(sessions, *session)
	}

	return sessions, rows.Err()
}

func (r *SessionRepository) Get(ctx context.Context, sessionID string) (*Session, error) {
	row := r.db.QueryRowContext(
		ctx,
		`SELECT id, session_id, name, login_method, phone, COALESCE(device_jid, ''), status,
		        COALESCE(qr_code, ''), qr_expires_at, COALESCE(last_error, ''),
		        created_at, updated_at, deleted_at
		 FROM sessions
		 WHERE session_id = $1`,
		sessionID,
	)

	session, err := scanSession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}

	return session, nil
}

func (r *SessionRepository) UpdateState(ctx context.Context, params UpdateSessionStateParams) (*Session, error) {
	row := r.db.QueryRowContext(
		ctx,
		`UPDATE sessions
		 SET status = $2,
		     phone = CASE WHEN $3 = '' THEN phone ELSE $3 END,
		     device_jid = CASE WHEN $4 = '' THEN device_jid ELSE $4 END,
		     qr_code = $5,
		     qr_expires_at = $6,
		     last_error = $7,
		     updated_at = NOW()
		 WHERE session_id = $1
		 RETURNING id, session_id, name, login_method, phone, COALESCE(device_jid, ''), status,
		           COALESCE(qr_code, ''), qr_expires_at, COALESCE(last_error, ''),
		           created_at, updated_at, deleted_at`,
		params.SessionID,
		params.Status,
		params.Phone,
		params.DeviceJID,
		params.QRCode,
		params.QRExpiresAt,
		params.LastError,
	)

	session, err := scanSession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("update session state: %w", err)
	}

	return session, nil
}

func (r *SessionRepository) MarkDeleted(ctx context.Context, sessionID string) error {
	result, err := r.db.ExecContext(
		ctx,
		`UPDATE sessions
		 SET status = $2,
		     qr_code = NULL,
		     qr_expires_at = NULL,
		     updated_at = NOW(),
		     deleted_at = NOW()
		 WHERE session_id = $1`,
		sessionID,
		SessionStatusDeleted,
	)
	if err != nil {
		return fmt.Errorf("mark session deleted: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("mark session deleted rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

func scanSession(scanner interface {
	Scan(dest ...interface{}) error
}) (*Session, error) {
	var session Session
	var deviceJID string
	var qrCode string
	var qrExpiresAt sql.NullTime
	var deletedAt sql.NullTime

	err := scanner.Scan(
		&session.ID,
		&session.SessionID,
		&session.Name,
		&session.LoginMethod,
		&session.Phone,
		&deviceJID,
		&session.Status,
		&qrCode,
		&qrExpiresAt,
		&session.LastError,
		&session.CreatedAt,
		&session.UpdatedAt,
		&deletedAt,
	)
	if err != nil {
		return nil, err
	}

	session.DeviceJID = deviceJID
	session.QRCode = qrCode
	if qrExpiresAt.Valid {
		session.QRExpiresAt = &qrExpiresAt.Time
	}
	if deletedAt.Valid {
		session.DeletedAt = &deletedAt.Time
	}

	return &session, nil
}
