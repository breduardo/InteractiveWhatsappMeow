package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"interactivewhatsmeow/internal/auth"
	"interactivewhatsmeow/internal/messages"
	"interactivewhatsmeow/internal/session"
	"interactivewhatsmeow/internal/store"
	"interactivewhatsmeow/internal/webhooks"
)

type RouterDependencies struct {
	Logger         zerolog.Logger
	AuthValidator  auth.APIKeyValidator
	SessionService SessionService
	MessageService MessageService
	WebhookService WebhookService
	ReadService    ReadService
	StaticFS       fs.FS
}

type SessionService interface {
	CreateSession(ctx context.Context, input session.CreateSessionInput) (*session.CreateSessionResult, error)
	ListSessions(ctx context.Context) ([]store.Session, error)
	GetSession(ctx context.Context, sessionID string) (*store.Session, error)
	GetQRCode(ctx context.Context, sessionID string) (*session.QRCodeResult, error)
	GeneratePairCode(ctx context.Context, sessionID, phone string) (*session.PairCodeResult, error)
	ReconnectSession(ctx context.Context, sessionID string) (*store.Session, error)
	DeleteSession(ctx context.Context, sessionID string) error
	VerifyNumber(ctx context.Context, sessionID, phone string) (*session.VerifyNumberResult, error)
}

type MessageService interface {
	SendText(ctx context.Context, sessionID, to, text string) (*messages.SendResult, error)
	SendMedia(ctx context.Context, sessionID, to, caption, fileName, mimeType string, contents []byte) (*messages.SendResult, error)
	Reply(ctx context.Context, sessionID, chatJID, messageID, text string) (*messages.SendResult, error)
	Edit(ctx context.Context, sessionID, chatJID, messageID, text string) (*messages.SendResult, error)
	List(ctx context.Context, input messages.ListInput) ([]store.Message, error)
}

type WebhookService interface {
	CreateWebhook(ctx context.Context, input webhooks.CreateWebhookInput) (*store.Webhook, error)
	ListWebhooks(ctx context.Context, sessionID *string) ([]store.Webhook, error)
	DeleteWebhook(ctx context.Context, id int64) error
}

type ReadService interface {
	GetDashboardSummary(ctx context.Context) (*store.DashboardSummary, error)
	ListChats(ctx context.Context, sessionID string) ([]store.ChatSummary, error)
	GetAnalyticsSummary(ctx context.Context, input store.AnalyticsSummaryInput) (*store.AnalyticsSummary, error)
}

func NewRouter(deps RouterDependencies) http.Handler {
	router := chi.NewRouter()
	registerStaticRoutes(router, deps.StaticFS)

	router.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	router.Group(func(r chi.Router) {
		r.Use(auth.Middleware(deps.AuthValidator))

		r.Route("/v1", func(r chi.Router) {
			r.Post("/sessions", createSessionHandler(deps.SessionService))
			r.Get("/sessions", listSessionsHandler(deps.SessionService))
			r.Get("/sessions/{sessionId}", getSessionHandler(deps.SessionService))
			r.Get("/sessions/{sessionId}/qr", getQRCodeHandler(deps.SessionService))
			r.Post("/sessions/{sessionId}/pair-code", generatePairCodeHandler(deps.SessionService))
			r.Post("/sessions/{sessionId}/reconnect", reconnectSessionHandler(deps.SessionService))
			r.Delete("/sessions/{sessionId}", deleteSessionHandler(deps.SessionService))

			r.Post("/messages/text", sendTextHandler(deps.MessageService))
			r.Post("/messages/media", sendMediaHandler(deps.MessageService))
			r.Post("/messages/reply", replyMessageHandler(deps.MessageService))
			r.Put("/messages/{messageId}", editMessageHandler(deps.MessageService))
			r.Get("/messages", listMessagesHandler(deps.MessageService))

			r.Get("/dashboard/summary", dashboardSummaryHandler(deps.ReadService))
			r.Get("/chats", listChatsHandler(deps.ReadService))
			r.Get("/analytics/summary", analyticsSummaryHandler(deps.ReadService))

			r.Post("/numbers/verify", verifyNumberHandler(deps.SessionService))

			r.Post("/webhooks", createWebhookHandler(deps.WebhookService))
			r.Get("/webhooks", listWebhooksHandler(deps.WebhookService))
			r.Delete("/webhooks/{id}", deleteWebhookHandler(deps.WebhookService))
		})
	})

	return router
}

func createSessionHandler(service SessionService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			SessionID   string `json:"sessionId"`
			Name        string `json:"name"`
			LoginMethod string `json:"loginMethod"`
			Phone       string `json:"phone"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}

		result, err := service.CreateSession(r.Context(), session.CreateSessionInput{
			SessionID:   req.SessionID,
			Name:        req.Name,
			LoginMethod: store.LoginMethod(req.LoginMethod),
			Phone:       req.Phone,
		})
		if err != nil {
			writeDomainError(w, err)
			return
		}

		writeJSON(w, http.StatusCreated, result)
	}
}

func listSessionsHandler(service SessionService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessions, err := service.ListSessions(r.Context())
		if err != nil {
			writeDomainError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"sessions": sessions})
	}
}

func getSessionHandler(service SessionService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionRecord, err := service.GetSession(r.Context(), chi.URLParam(r, "sessionId"))
		if err != nil {
			writeDomainError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, sessionRecord)
	}
}

func getQRCodeHandler(service SessionService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		qr, err := service.GetQRCode(r.Context(), chi.URLParam(r, "sessionId"))
		if err != nil {
			writeDomainError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, qr)
	}
}

func generatePairCodeHandler(service SessionService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Phone string `json:"phone"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}

		result, err := service.GeneratePairCode(r.Context(), chi.URLParam(r, "sessionId"), req.Phone)
		if err != nil {
			writeDomainError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func reconnectSessionHandler(service SessionService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionRecord, err := service.ReconnectSession(r.Context(), chi.URLParam(r, "sessionId"))
		if err != nil {
			writeDomainError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, sessionRecord)
	}
}

func deleteSessionHandler(service SessionService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := service.DeleteSession(r.Context(), chi.URLParam(r, "sessionId")); err != nil {
			writeDomainError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func sendTextHandler(service MessageService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			SessionID string `json:"sessionId"`
			To        string `json:"to"`
			Text      string `json:"text"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}

		result, err := service.SendText(r.Context(), req.SessionID, req.To, req.Text)
		if err != nil {
			writeDomainError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func sendMediaHandler(service MessageService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(25 << 20); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		defer file.Close()

		contents, err := io.ReadAll(file)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}

		result, err := service.SendMedia(
			r.Context(),
			r.FormValue("sessionId"),
			r.FormValue("to"),
			r.FormValue("caption"),
			header.Filename,
			detectMultipartContentType(header),
			contents,
		)
		if err != nil {
			writeDomainError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, result)
	}
}

func replyMessageHandler(service MessageService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			SessionID string `json:"sessionId"`
			ChatJID   string `json:"chatJid"`
			MessageID string `json:"messageId"`
			Text      string `json:"text"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}

		result, err := service.Reply(r.Context(), req.SessionID, req.ChatJID, req.MessageID, req.Text)
		if err != nil {
			writeDomainError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func editMessageHandler(service MessageService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			SessionID string `json:"sessionId"`
			ChatJID   string `json:"chatJid"`
			Text      string `json:"text"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}

		result, err := service.Edit(r.Context(), req.SessionID, req.ChatJID, chi.URLParam(r, "messageId"), req.Text)
		if err != nil {
			writeDomainError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func listMessagesHandler(service MessageService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit")))
		cursor, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("cursor")), 10, 64)

		messages, err := service.List(r.Context(), messages.ListInput{
			SessionID: strings.TrimSpace(r.URL.Query().Get("sessionId")),
			ChatJID:   strings.TrimSpace(r.URL.Query().Get("chatJid")),
			Limit:     limit,
			Cursor:    cursor,
		})
		if err != nil {
			writeDomainError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{"messages": messages})
	}
}

func verifyNumberHandler(service SessionService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			SessionID string `json:"sessionId"`
			Phone     string `json:"phone"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}

		result, err := service.VerifyNumber(r.Context(), req.SessionID, req.Phone)
		if err != nil {
			writeDomainError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func createWebhookHandler(service WebhookService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			SessionID *string  `json:"sessionId"`
			URL       string   `json:"url"`
			Events    []string `json:"events"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}

		result, err := service.CreateWebhook(r.Context(), webhooks.CreateWebhookInput{
			SessionID: req.SessionID,
			URL:       req.URL,
			Events:    req.Events,
		})
		if err != nil {
			writeDomainError(w, err)
			return
		}

		writeJSON(w, http.StatusCreated, result)
	}
}

func listWebhooksHandler(service WebhookService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var sessionID *string
		if value := strings.TrimSpace(r.URL.Query().Get("sessionId")); value != "" {
			sessionID = &value
		}

		result, err := service.ListWebhooks(r.Context(), sessionID)
		if err != nil {
			writeDomainError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{"webhooks": result})
	}
}

func deleteWebhookHandler(service WebhookService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}

		if err := service.DeleteWebhook(r.Context(), id); err != nil {
			writeDomainError(w, err)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

func decodeJSON(r *http.Request, target interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(target)
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func writeDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, session.ErrSessionExists):
		writeError(w, http.StatusConflict, err)
	case errors.Is(err, session.ErrSessionNotConnected):
		writeError(w, http.StatusConflict, err)
	case errors.Is(err, session.ErrQRCodeUnavailable):
		writeError(w, http.StatusNotFound, err)
	default:
		writeError(w, http.StatusBadRequest, err)
	}
}

func detectMultipartContentType(header *multipart.FileHeader) string {
	if header == nil {
		return "application/octet-stream"
	}
	if header.Header != nil && header.Header.Get("Content-Type") != "" {
		return header.Header.Get("Content-Type")
	}
	return "application/octet-stream"
}
