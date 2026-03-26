package httpapi

import (
	"bytes"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

func registerStaticRoutes(router chi.Router, assets fs.FS) {
	if assets == nil {
		return
	}

	router.Get("/", serveStaticPage(assets, "dashboard.html"))
	router.Get("/dashboard", serveStaticPage(assets, "dashboard.html"))
	router.Get("/chat", serveStaticPage(assets, "chat.html"))
	router.Get("/analytics", serveStaticPage(assets, "analytics.html"))
	router.Get("/settings", serveStaticPage(assets, "settings.html"))

	fileServer := http.StripPrefix("/public/", http.FileServer(http.FS(assets)))
	router.Get("/public/*", func(w http.ResponseWriter, r *http.Request) {
		assetPath := strings.TrimSpace(chi.URLParam(r, "*"))
		if assetPath == "" || strings.Contains(assetPath, "..") {
			http.NotFound(w, r)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

func serveStaticPage(assets fs.FS, fileName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := fs.ReadFile(assets, fileName)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		contentType := mime.TypeByExtension(path.Ext(fileName))
		if contentType == "" {
			contentType = "text/html; charset=utf-8"
		}
		w.Header().Set("Content-Type", contentType)
		http.ServeContent(w, r, fileName, time.Time{}, bytes.NewReader(data))
	}
}
