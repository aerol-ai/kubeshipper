package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type ServiceStatus string

const (
	StatusPending   ServiceStatus = "PENDING"
	StatusDeploying ServiceStatus = "DEPLOYING"
	StatusReady     ServiceStatus = "READY"
	StatusFailed    ServiceStatus = "FAILED"
)

type Service struct {
	ID                string          `json:"id"`
	Spec              json.RawMessage `json:"spec"`
	Status            ServiceStatus   `json:"status"`
	LastReadySpec     json.RawMessage `json:"-"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
	// JobID — the streaming job currently driving this row. Cleared on terminal.
	JobID             string          `json:"job_id,omitempty"`
}

func (s *Store) GetService(id string) (*Service, error) {
	row := s.DB.QueryRow(
		`SELECT id, spec_json, status, last_ready_spec_json, created_at, updated_at, job_id FROM services WHERE id = ?`,
		id,
	)
	return scanService(row)
}

func (s *Store) ListServices() ([]*Service, error) {
	rows, err := s.DB.Query(
		`SELECT id, spec_json, status, last_ready_spec_json, created_at, updated_at, job_id FROM services ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Service
	for rows.Next() {
		svc, err := scanService(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, svc)
	}
	return out, rows.Err()
}

func (s *Store) UpsertService(id string, spec json.RawMessage, status ServiceStatus) error {
	now := time.Now().UnixMilli()
	_, err := s.DB.Exec(`
		INSERT INTO services (id, spec_json, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
		  spec_json = excluded.spec_json,
		  status = excluded.status,
		  updated_at = excluded.updated_at
	`, id, string(spec), string(status), now, now)
	return err
}

func (s *Store) UpdateStatus(id string, status ServiceStatus) error {
	_, err := s.DB.Exec(
		`UPDATE services SET status = ?, updated_at = ? WHERE id = ?`,
		string(status), time.Now().UnixMilli(), id,
	)
	return err
}

func (s *Store) MarkReady(id string, spec json.RawMessage) error {
	_, err := s.DB.Exec(
		`UPDATE services SET status = 'READY', last_ready_spec_json = ?, updated_at = ? WHERE id = ?`,
		string(spec), time.Now().UnixMilli(), id,
	)
	return err
}

func (s *Store) ResetStuckDeployments() error {
	_, err := s.DB.Exec(`UPDATE services SET status = 'PENDING' WHERE status = 'DEPLOYING'`)
	return err
}

// AttachJob links a job to a service row so the worker knows where to publish events.
// Pass empty string to clear.
func (s *Store) AttachJob(serviceID, jobID string) error {
	if jobID == "" {
		_, err := s.DB.Exec(`UPDATE services SET job_id = NULL, updated_at = ? WHERE id = ?`,
			time.Now().UnixMilli(), serviceID)
		return err
	}
	_, err := s.DB.Exec(`UPDATE services SET job_id = ?, updated_at = ? WHERE id = ?`,
		jobID, time.Now().UnixMilli(), serviceID)
	return err
}

func (s *Store) ServicesByStatus(status ServiceStatus) ([]*Service, error) {
	rows, err := s.DB.Query(
		`SELECT id, spec_json, status, last_ready_spec_json, created_at, updated_at, job_id FROM services WHERE status = ?`,
		string(status),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Service
	for rows.Next() {
		svc, err := scanService(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, svc)
	}
	return out, rows.Err()
}

func (s *Store) DeleteService(id string) error {
	_, err := s.DB.Exec(`DELETE FROM services WHERE id = ?`, id)
	return err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanService(r rowScanner) (*Service, error) {
	var (
		svc       Service
		spec      string
		lastReady sql.NullString
		created   int64
		updated   int64
		jobID     sql.NullString
	)
	if err := r.Scan(&svc.ID, &spec, &svc.Status, &lastReady, &created, &updated, &jobID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	svc.Spec = json.RawMessage(spec)
	if jobID.Valid {
		svc.JobID = jobID.String
	}
	if lastReady.Valid {
		svc.LastReadySpec = json.RawMessage(lastReady.String)
	}
	svc.CreatedAt = time.UnixMilli(created)
	svc.UpdatedAt = time.UnixMilli(updated)
	return &svc, nil
}
