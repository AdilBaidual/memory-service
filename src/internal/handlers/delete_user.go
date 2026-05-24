package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

func (h *Handler) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	reqID := chimiddleware.GetReqID(r.Context())

	req, err := parseDeleteUserRequest(r)
	if err != nil {
		writeError(w, err)
		return
	}

	if err := h.user.Delete(ctx, req.UserID); err != nil {
		h.log.Error("delete user", "error", err, "request_id", reqID, "user_id", req.UserID)
		writeError(w, err)
		return
	}

	h.log.Info("user deleted", "user_id", req.UserID)
	w.WriteHeader(http.StatusNoContent)
}

type DeleteUserRequest struct {
	UserID string
}

func parseDeleteUserRequest(r *http.Request) (*DeleteUserRequest, error) {
	userID := chi.URLParam(r, "user_id")
	if userID == "" {
		return nil, &apiError{http.StatusBadRequest, "user_id is required"}
	}
	return &DeleteUserRequest{UserID: userID}, nil
}
