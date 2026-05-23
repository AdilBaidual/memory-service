// Package api contains the HTTP router, handlers, and middleware for the memory service.
package api

import (
	"strings"
	"unicode/utf8"
)

// sanitizeText prepares user-provided text for storage:
// strips NUL bytes (Postgres TEXT cannot store them) and
// replaces invalid UTF-8 sequences with "?".
func sanitizeText(s string) string {
	s = strings.ReplaceAll(s, "\x00", "")
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "?")
	}
	return s
}
