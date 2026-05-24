// Package identity resolves an operation's identity scope from HTTP request inputs.
package identity

import "fmt"

// Scope represents the identity scope for memory operations.
// Type is "user" when user_id is known, "session" for anonymous.
type Scope struct {
	Type  string // "user" | "session"
	Value string // user_id or session_id
}

// Resolve returns a Scope from optional user_id and required session_id.
// Prefers user_id when present.
func Resolve(userID *string, sessionID string) Scope {
	if userID != nil && *userID != "" {
		return Scope{Type: "user", Value: *userID}
	}
	return Scope{Type: "session", Value: sessionID}
}

// UserID returns *string for storage — nil when session-scoped.
func (s Scope) UserID() *string {
	if s.Type == "user" {
		return &s.Value
	}
	return nil
}

// IsUser returns true when scope is user-based.
func (s Scope) IsUser() bool {
	return s.Type == "user"
}

// String implements Stringer for logging.
func (s Scope) String() string {
	return fmt.Sprintf("%s:%s", s.Type, s.Value)
}
