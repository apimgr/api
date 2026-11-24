package server

import (
	"net/http"

	"github.com/apimgr/api/src/database"
)

type securityThanksAPIEntry struct {
	Credit   string `json:"credit"`
	Date     string `json:"date"`
	Severity string `json:"severity"`
}

func securityThanksAPIHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := database.ListSecurityThanks(r.Context())
	if err != nil {
		writeLocalizedAPIError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR")
		return
	}
	entries := make([]securityThanksAPIEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, securityThanksAPIEntry{Credit: row.Credit, Date: row.DisclosedOn, Severity: row.Severity})
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]any{"entries": entries})
}
