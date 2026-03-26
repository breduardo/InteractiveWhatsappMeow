package auth

import (
	"context"
	"net/http"
	"strings"
)

type APIKeyValidator interface {
	Validate(ctx context.Context, key string) (bool, error)
}

func Middleware(validator APIKeyValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := strings.TrimSpace(r.Header.Get("X-API-Key"))
			if key == "" {
				http.Error(w, "missing X-API-Key header", http.StatusUnauthorized)
				return
			}

			valid, err := validator.Validate(r.Context(), key)
			if err != nil {
				http.Error(w, "failed to validate api key", http.StatusInternalServerError)
				return
			}
			if !valid {
				http.Error(w, "invalid api key", http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
