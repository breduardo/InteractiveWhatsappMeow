package httpapi

import (
	"net/http"
	"strings"

	"interactivewhatsmeow/internal/store"
)

func dashboardSummaryHandler(service ReadService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if service == nil {
			writeError(w, http.StatusNotImplemented, store.ErrNotFound)
			return
		}

		result, err := service.GetDashboardSummary(r.Context())
		if err != nil {
			writeDomainError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, result)
	}
}

func listChatsHandler(service ReadService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if service == nil {
			writeError(w, http.StatusNotImplemented, store.ErrNotFound)
			return
		}

		result, err := service.ListChats(r.Context(), strings.TrimSpace(r.URL.Query().Get("sessionId")))
		if err != nil {
			writeDomainError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{"chats": result})
	}
}

func analyticsSummaryHandler(service ReadService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if service == nil {
			writeError(w, http.StatusNotImplemented, store.ErrNotFound)
			return
		}

		var sessionID *string
		if value := strings.TrimSpace(r.URL.Query().Get("sessionId")); value != "" {
			sessionID = &value
		}

		result, err := service.GetAnalyticsSummary(r.Context(), store.AnalyticsSummaryInput{
			SessionID: sessionID,
			Range:     strings.TrimSpace(r.URL.Query().Get("range")),
		})
		if err != nil {
			writeDomainError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, result)
	}
}
