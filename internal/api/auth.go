package api

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
)

// authMiddleware enforces a static bearer token if AUTH_TOKEN is set in env.
// When unset, all requests pass — useful for in-cluster open mode + dev.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.deps.AuthToken == "" {
			next.ServeHTTP(w, r)
			return
		}
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			writeJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "Unauthorized: missing or malformed Authorization header",
			})
			return
		}
		token := strings.TrimPrefix(h, "Bearer ")
		if subtle.ConstantTimeCompare([]byte(token), []byte(s.deps.AuthToken)) != 1 {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden: invalid token"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// writeJSON is the canonical response helper used by all handlers.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// initiator fingerprints the bearer token for audit logs without persisting the secret.
func initiator(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	t := strings.TrimPrefix(h, "Bearer ")
	if len(t) < 8 {
		return "token:short"
	}
	return "token:" + t[:8]
}
