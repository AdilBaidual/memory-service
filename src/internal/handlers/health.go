package handlers

import (
	"context"
	"net/http"
	"time"
)

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	pingCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	if err := h.pool.Ping(pingCtx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "unhealthy",
			"db":     "down",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
