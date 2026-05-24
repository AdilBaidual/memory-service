package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"memory-service/internal/adapters/store"
	"memory-service/internal/usecase"
)

func (h *Handler) handleListMemories(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	req, err := parseMemoriesRequest(r)
	if err != nil {
		writeError(w, err)
		return
	}

	out, err := h.memories.List(ctx, usecase.MemoriesInput{
		UserID:  req.UserID,
		Filters: req.Filters,
	})
	if err != nil {
		h.log.Error("list memories", "error", err, "user_id", req.UserID)
		writeError(w, fmt.Errorf("list memories: %w", err))
		return
	}

	writeJSON(w, http.StatusOK, buildMemoriesResponse(out))
}

type MemoriesRequest struct {
	UserID  string
	Filters store.ListMemoriesFilters
}

func parseMemoriesRequest(r *http.Request) (*MemoriesRequest, error) {
	userID := chi.URLParam(r, "user_id")
	if userID == "" {
		return nil, &apiError{http.StatusBadRequest, "user_id is required"}
	}

	q := r.URL.Query()
	filters := store.ListMemoriesFilters{}

	if v := q.Get("type"); v != "" {
		filters.Type = &v
	}
	if v := q.Get("active"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, &apiError{http.StatusBadRequest, "invalid active param: must be true or false"}
		}
		filters.Active = &b
	}
	if v := q.Get("key"); v != "" {
		filters.Key = &v
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, &apiError{http.StatusBadRequest, "invalid limit param"}
		}
		filters.Limit = n
	}
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, &apiError{http.StatusBadRequest, "invalid offset param"}
		}
		filters.Offset = n
	}

	return &MemoriesRequest{UserID: userID, Filters: filters}, nil
}

func buildMemoriesResponse(out usecase.MemoriesOutput) MemoriesListResponse {
	views := make([]MemoryView, len(out.Memories))
	for i, m := range out.Memories {
		views[i] = memoryToView(m)
	}
	return MemoriesListResponse{
		Memories: views,
		Total:    out.Total,
		Limit:    out.Limit,
		Offset:   out.Offset,
	}
}

func memoryToView(m store.Memory) MemoryView {
	entities := m.Entities
	if entities == nil {
		entities = json.RawMessage("[]")
	}
	metadata := m.Metadata
	if metadata == nil {
		metadata = json.RawMessage("{}")
	}
	return MemoryView{
		ID:            m.ID.String(),
		Type:          m.Type,
		Key:           m.Key,
		Value:         m.Value,
		Confidence:    m.Confidence,
		Evidence:      m.Evidence,
		Entities:      entities,
		SourceSession: m.SourceSession,
		SourceTurn:    uuidPtrToStr(m.SourceTurn),
		ValidFrom:     m.ValidFrom,
		ValidTo:       m.ValidTo,
		CreatedAt:     m.CreatedAt,
		UpdatedAt:     m.UpdatedAt,
		Supersedes:    uuidPtrToStr(m.Supersedes),
		Active:        m.Active,
		Metadata:      metadata,
	}
}

func uuidPtrToStr(u *uuid.UUID) *string {
	if u == nil {
		return nil
	}
	s := u.String()
	return &s
}
