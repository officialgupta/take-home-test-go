package app

import (
	"context"
	"net/http"
	"time"
)

func (a *App) handleHealthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if _, err := a.store.Ping(ctx); err != nil {
		a.logger.Error("healthz: database ping failed", "error", err)
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
