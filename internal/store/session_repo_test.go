package store

import (
	"context"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func TestSessionRepositoryResetForQRCode(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock: %v", err)
	}
	defer db.Close()

	repo := NewSessionRepository(db)
	createdAt := time.Date(2026, 3, 26, 10, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(2 * time.Minute)

	mock.ExpectQuery("UPDATE sessions").
		WithArgs("alpha", LoginMethodQR, SessionStatusInitializing).
		WillReturnRows(
			sqlmock.NewRows([]string{
				"id",
				"session_id",
				"name",
				"login_method",
				"phone",
				"device_jid",
				"status",
				"qr_code",
				"qr_expires_at",
				"last_error",
				"created_at",
				"updated_at",
				"deleted_at",
			}).AddRow(
				1,
				"alpha",
				"Alpha",
				string(LoginMethodQR),
				"+5511999999999",
				"",
				string(SessionStatusInitializing),
				"",
				nil,
				"",
				createdAt,
				updatedAt,
				nil,
			),
		)

	sessionRecord, err := repo.ResetForQRCode(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("reset for qr code: %v", err)
	}

	if sessionRecord.LoginMethod != LoginMethodQR {
		t.Fatalf("expected login method qr, got %s", sessionRecord.LoginMethod)
	}
	if sessionRecord.Status != SessionStatusInitializing {
		t.Fatalf("expected initializing status, got %s", sessionRecord.Status)
	}
	if sessionRecord.DeviceJID != "" || sessionRecord.QRCode != "" || sessionRecord.QRExpiresAt != nil || sessionRecord.LastError != "" {
		t.Fatalf("expected cleared session qr state, got %+v", sessionRecord)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
