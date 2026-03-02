package handler

import (
	"net/http"

	"github.com/JeffResc/DisplayLoop/internal/db"
)

// AuditData is passed to the global audit log template.
type AuditData struct {
	Entries []db.AuditEntry
}

func (a *App) HandleAuditLog(w http.ResponseWriter, r *http.Request) {
	entries, err := db.ListAuditLog(r.Context(), a.DB, 0, 200)
	if err != nil {
		a.respondError(w, http.StatusInternalServerError, "failed to load audit log")
		return
	}

	a.render(w, "audit.html", AuditData{Entries: entries})
}
