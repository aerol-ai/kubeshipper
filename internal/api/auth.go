package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	dashboardCookieName = "kubeshipper_session"
	dashboardSessionTTL = 7 * 24 * time.Hour
)

type sessionClaims struct {
	Sub  string `json:"sub"`
	Kind string `json:"kind"`
	Iat  int64  `json:"iat"`
	Exp  int64  `json:"exp"`
}

type authSessionResponse struct {
	Authenticated bool   `json:"authenticated"`
	Mode          string `json:"mode"`
	ExpiresAt     string `json:"expires_at,omitempty"`
	Version       string `json:"version,omitempty"`
}

// authMiddleware enforces a dashboard session cookie or the legacy static
// bearer token when AUTH_TOKEN is set in env. When unset, all requests pass.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.deps.AuthToken == "" {
			next.ServeHTTP(w, r)
			return
		}
		if _, ok := s.authenticateCookie(r); ok {
			next.ServeHTTP(w, r)
			return
		}
		h := r.Header.Get("Authorization")
		if h != "" {
			if !strings.HasPrefix(h, "Bearer ") {
				writeJSON(w, http.StatusUnauthorized, map[string]string{
					"error": "Unauthorized: missing or invalid session",
				})
				return
			}
			token := strings.TrimPrefix(h, "Bearer ")
			if subtle.ConstantTimeCompare([]byte(token), []byte(s.deps.AuthToken)) != 1 {
				writeJSON(w, http.StatusForbidden, map[string]string{
					"error": "Forbidden: invalid token",
				})
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "Unauthorized: missing or invalid session",
		})
	})
}

func (s *Server) authenticateRequest(r *http.Request) (*sessionClaims, bool) {
	if claims, ok := s.authenticateCookie(r); ok {
		return claims, true
	}
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return nil, false
	}
	token := strings.TrimPrefix(h, "Bearer ")
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.deps.AuthToken)) != 1 {
		return nil, false
	}
	return &sessionClaims{Sub: "token", Kind: "api-token"}, true
}

func (s *Server) authenticateCookie(r *http.Request) (*sessionClaims, bool) {
	if s.deps.AuthToken == "" {
		return nil, false
	}
	cookie, err := r.Cookie(dashboardCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return nil, false
	}
	claims, err := parseSessionToken(cookie.Value, s.sessionSigningKey())
	if err != nil {
		return nil, false
	}
	return claims, true
}

func (s *Server) sessionSigningKey() []byte {
	sum := sha256.Sum256([]byte("kubeshipper-dashboard-session:" + s.deps.AuthToken))
	return sum[:]
}

func (s *Server) setDashboardCookie(w http.ResponseWriter, r *http.Request, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     dashboardCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   requestIsSecure(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  expiresAt,
		MaxAge:   int(time.Until(expiresAt).Seconds()),
	})
}

func (s *Server) clearDashboardCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     dashboardCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   requestIsSecure(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

func requestIsSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
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
		if _, err := r.Cookie(dashboardCookieName); err == nil {
			return "session:ui"
		}
		return ""
	}
	t := strings.TrimPrefix(h, "Bearer ")
	if len(t) < 8 {
		return "token:short"
	}
	return "token:" + t[:8]
}

func signSessionToken(secret []byte, now time.Time) (string, time.Time, error) {
	expiresAt := now.Add(dashboardSessionTTL)
	claims := sessionClaims{
		Sub:  "dashboard",
		Kind: "dashboard-session",
		Iat:  now.Unix(),
		Exp:  expiresAt.Unix(),
	}
	headerJSON, err := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	if err != nil {
		return "", time.Time{}, err
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", time.Time{}, err
	}
	header := base64.RawURLEncoding.EncodeToString(headerJSON)
	payload := base64.RawURLEncoding.EncodeToString(claimsJSON)
	signingInput := header + "." + payload
	h := hmac.New(sha256.New, secret)
	_, _ = h.Write([]byte(signingInput))
	signature := base64.RawURLEncoding.EncodeToString(h.Sum(nil))
	return signingInput + "." + signature, expiresAt, nil
}

func parseSessionToken(token string, secret []byte) (*sessionClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid token format")
	}
	signingInput := parts[0] + "." + parts[1]
	h := hmac.New(sha256.New, secret)
	_, _ = h.Write([]byte(signingInput))
	expected := h.Sum(nil)
	provided, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, err
	}
	if !hmac.Equal(provided, expected) {
		return nil, fmt.Errorf("invalid token signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	var claims sessionClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, err
	}
	if claims.Kind != "dashboard-session" || claims.Exp <= time.Now().Unix() {
		return nil, fmt.Errorf("session expired")
	}
	return &claims, nil
}
