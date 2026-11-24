package server

import (
	"net/http"

	"github.com/apimgr/api/src/common/i18n"
	"github.com/go-chi/chi/v5"
)

func localeJSONHandler(w http.ResponseWriter, r *http.Request) {
	lang := chi.URLParam(r, "lang")
	data, err := i18n.LocaleJSON(lang)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
