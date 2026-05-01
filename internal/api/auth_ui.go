package api

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"time"
)

type loginRequest struct {
	Token string `json:"token"`
	Key   string `json:"key"`
}

type authRouteBinder interface {
	Post(string, http.HandlerFunc)
	Get(string, http.HandlerFunc)
}

func (s *Server) mountAuthEndpoints(r authRouteBinder) {
	r.Post("/auth/login", s.login)
	r.Post("/auth/logout", s.logout)
	r.Get("/auth/session", s.session)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if s.deps.AuthToken == "" {
		writeJSON(w, http.StatusOK, authSessionResponse{
			Authenticated: true,
			Mode:          "open",
			Version:       s.deps.Version,
		})
		return
	}
	var req loginRequest
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON", "details": err.Error()})
		return
	}
	token := strings.TrimSpace(req.Token)
	if token == "" {
		token = strings.TrimSpace(req.Key)
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.deps.AuthToken)) != 1 {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden: invalid token"})
		return
	}
	signed, expiresAt, err := signSessionToken(s.sessionSigningKey(), time.Now().UTC())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.setDashboardCookie(w, r, signed, expiresAt)
	writeJSON(w, http.StatusOK, authSessionResponse{
		Authenticated: true,
		Mode:          "jwt",
		ExpiresAt:     expiresAt.Format(time.RFC3339),
		Version:       s.deps.Version,
	})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	s.clearDashboardCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	if s.deps.AuthToken == "" {
		writeJSON(w, http.StatusOK, authSessionResponse{
			Authenticated: true,
			Mode:          "open",
			Version:       s.deps.Version,
		})
		return
	}
	if claims, ok := s.authenticateCookie(r); ok {
		writeJSON(w, http.StatusOK, authSessionResponse{
			Authenticated: true,
			Mode:          "jwt",
			ExpiresAt:     time.Unix(claims.Exp, 0).UTC().Format(time.RFC3339),
			Version:       s.deps.Version,
		})
		return
	}
	if _, ok := s.authenticateRequest(r); ok {
		writeJSON(w, http.StatusOK, authSessionResponse{
			Authenticated: true,
			Mode:          "token",
			Version:       s.deps.Version,
		})
		return
	}
	writeJSON(w, http.StatusUnauthorized, authSessionResponse{
		Authenticated: false,
		Mode:          "jwt",
		Version:       s.deps.Version,
	})
}
