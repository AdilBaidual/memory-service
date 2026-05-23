// Package api contains the HTTP router, handlers, and middleware for the memory service.
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// apiError carries an HTTP status code and message.
// Implements the error interface so it can be returned from parseRequest helpers.
type apiError struct {
	status int
	msg    string
}

func (e *apiError) Error() string { return e.msg }

// writeError writes an error response, extracting the status code from apiError
// when available and falling back to 500 for unexpected errors.
func writeError(w http.ResponseWriter, err error) {
	var ae *apiError
	if errors.As(err, &ae) {
		writeJSON(w, ae.status, ErrorResponse{Error: ae.msg})
		return
	}
	writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal error"})
}

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	status int
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w, status: http.StatusOK}
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// RecoverMiddleware recovers from panics, logs the stack trace, and returns 500.
func RecoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic recovered",
					"error", rec,
					"stack", string(debug.Stack()),
					"request_id", middleware.GetReqID(r.Context()),
				)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				//nolint:errcheck
				json.NewEncoder(w).Encode(map[string]string{"error": "internal error"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// RequestLogger logs each request after the handler returns.
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := newResponseWriter(w)
		next.ServeHTTP(rw, r)
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", middleware.GetReqID(r.Context()),
			"remote_addr", r.RemoteAddr,
		)
	})
}

// BodySizeLimiter limits the request body to n bytes. No-op when n <= 0.
func BodySizeLimiter(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if n > 0 {
				r.Body = http.MaxBytesReader(w, r.Body, n) // TODO: подумать насколько по тз и как отвечать на большие запросы
			}
			next.ServeHTTP(w, r)
		})
	}
}

// writeJSON writes a JSON-encoded body with the given status code.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	//nolint:errcheck
	json.NewEncoder(w).Encode(body)
}
