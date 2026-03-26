package httpapi

import (
	"net/http"
	"strings"
)

func legacyTestHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func legacySendTextHandler(service MessageService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			SessionID string `json:"sessionId"`
			To        string `json:"to"`
			Phone     string `json:"phone"`
			Message   string `json:"message"`
			Text      string `json:"text"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}

		sessionID := strings.TrimSpace(req.SessionID)
		to := firstNonEmpty(req.To, req.Phone)
		text := firstNonEmpty(req.Message, req.Text)

		result, err := service.SendText(r.Context(), sessionID, to, text)
		if err != nil {
			writeDomainError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, result)
	}
}

func legacyQRCodeHandler(service SessionService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := strings.TrimSpace(r.URL.Query().Get("id"))
		if sessionID == "" {
			writeError(w, http.StatusBadRequest, errRequired("id"))
			return
		}

		qr, err := service.GetQRCode(r.Context(), sessionID)
		if err != nil {
			writeDomainError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"sessionId": qr.SessionID,
			"qrCode":    qr.QRCode,
			"qr":        qr.QRCode,
			"expiresAt": qr.ExpiresAt,
		})
	}
}

func legacyReconnectHandler(service SessionService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := strings.TrimSpace(r.URL.Query().Get("id"))
		if sessionID == "" {
			writeError(w, http.StatusBadRequest, errRequired("id"))
			return
		}

		sessionRecord, err := service.ReconnectSession(r.Context(), sessionID)
		if err != nil {
			writeDomainError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status":  "ok",
			"session": sessionRecord,
		})
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func errRequired(field string) error {
	return &fieldRequiredError{field: field}
}

type fieldRequiredError struct {
	field string
}

func (e *fieldRequiredError) Error() string {
	return e.field + " is required"
}
