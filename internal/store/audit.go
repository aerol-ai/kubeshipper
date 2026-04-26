package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// AuditLog appends an audit row, hashing the (redacted) payload.
func (s *Store) AuditLog(initiator, operation, release, namespace, outcome string, payload any) error {
	redacted := redact(payload)
	body, _ := json.Marshal(redacted)
	sum := sha256.Sum256(body)

	_, err := s.DB.Exec(
		`INSERT INTO chart_audit (ts, initiator, operation, release, namespace, payload_hash, outcome) VALUES (?, NULLIF(?, ''), ?, ?, ?, ?, ?)`,
		time.Now().UnixMilli(),
		initiator,
		operation, release, namespace,
		hex.EncodeToString(sum[:]),
		outcome,
	)
	return err
}

var sensitiveKeys = map[string]bool{
	"password": true, "token": true, "sshKeyPem": true,
	"tgzBase64": true, "stringData": true, "auth": true,
}

func redact(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, val := range t {
			if sensitiveKeys[k] {
				out[k] = "[redacted]"
			} else {
				out[k] = redact(val)
			}
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = redact(x)
		}
		return out
	default:
		return v
	}
}
