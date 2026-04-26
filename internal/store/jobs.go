package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

type JobStatus string

const (
	JobPending   JobStatus = "pending"
	JobRunning   JobStatus = "running"
	JobSucceeded JobStatus = "succeeded"
	JobFailed    JobStatus = "failed"
)

type Job struct {
	ID        string    `json:"id"`
	Release   string    `json:"release"`
	Namespace string    `json:"namespace"`
	Operation string    `json:"operation"`
	Status    JobStatus `json:"status"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitempty"`
	Initiator string    `json:"initiator,omitempty"`
	Events    []Event   `json:"events,omitempty"`
}

// Event is one streamed step of a long-running operation. The same struct is
// pushed to SSE subscribers and persisted as a JSONL line in the job row.
type Event struct {
	Phase     string `json:"phase"`             // validation | prereqs | apply | wait | done | error | complete
	Message   string `json:"message,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Error     string `json:"error,omitempty"`
	TS        int64  `json:"ts"`
}

func (s *Store) CreateJob(release, namespace, operation, initiator string) (string, error) {
	id, err := newID()
	if err != nil {
		return "", err
	}
	_, err = s.DB.Exec(
		`INSERT INTO jobs (id, release, namespace, operation, status, started_at, initiator) VALUES (?, ?, ?, ?, 'pending', ?, NULLIF(?, ''))`,
		id, release, namespace, operation, time.Now().UnixMilli(), initiator,
	)
	if err != nil {
		return "", err
	}
	return id, nil
}

func (s *Store) SetJobStatus(id string, status JobStatus) error {
	var ended any
	if status == JobSucceeded || status == JobFailed {
		ended = time.Now().UnixMilli()
	}
	_, err := s.DB.Exec(
		`UPDATE jobs SET status = ?, ended_at = COALESCE(?, ended_at) WHERE id = ?`,
		string(status), ended, id,
	)
	if err != nil {
		return err
	}
	if status == JobSucceeded || status == JobFailed {
		s.publish(id, Event{Phase: "complete", Message: string(status), TS: time.Now().UnixMilli()})
	}
	return nil
}

func (s *Store) AppendEvent(jobID string, ev Event) error {
	if ev.TS == 0 {
		ev.TS = time.Now().UnixMilli()
	}
	line, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(
		`UPDATE jobs SET events_jsonl = events_jsonl || ? || char(10) WHERE id = ?`,
		string(line), jobID,
	)
	if err != nil {
		return err
	}
	s.publish(jobID, ev)
	return nil
}

func (s *Store) GetJob(id string) (*Job, error) {
	row := s.DB.QueryRow(
		`SELECT id, release, namespace, operation, status, events_jsonl, started_at, ended_at, initiator FROM jobs WHERE id = ?`,
		id,
	)
	var (
		j         Job
		events    string
		started   int64
		ended     sql.NullInt64
		initiator sql.NullString
	)
	if err := row.Scan(&j.ID, &j.Release, &j.Namespace, &j.Operation, &j.Status, &events, &started, &ended, &initiator); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	j.StartedAt = time.UnixMilli(started)
	if ended.Valid {
		j.EndedAt = time.UnixMilli(ended.Int64)
	}
	if initiator.Valid {
		j.Initiator = initiator.String
	}
	j.Events = parseEvents(events)
	return &j, nil
}

func parseEvents(jsonl string) []Event {
	out := []Event{}
	if jsonl == "" {
		return out
	}
	start := 0
	for i := 0; i <= len(jsonl); i++ {
		if i == len(jsonl) || jsonl[i] == '\n' {
			if i > start {
				var ev Event
				if err := json.Unmarshal([]byte(jsonl[start:i]), &ev); err == nil {
					out = append(out, ev)
				}
			}
			start = i + 1
		}
	}
	return out
}

// --- pubsub for SSE ---

// Subscribe registers a buffered channel that receives every Event appended to
// the given jobID, plus a final {phase: "complete"} when the job terminates.
// The returned cancel fn must be called to unsubscribe.
func (s *Store) Subscribe(jobID string) (<-chan Event, func()) {
	ch := make(chan Event, 64)
	s.subMu.Lock()
	if s.subs[jobID] == nil {
		s.subs[jobID] = map[chan Event]struct{}{}
	}
	s.subs[jobID][ch] = struct{}{}
	s.subMu.Unlock()
	return ch, func() {
		s.subMu.Lock()
		defer s.subMu.Unlock()
		if set, ok := s.subs[jobID]; ok {
			delete(set, ch)
			if len(set) == 0 {
				delete(s.subs, jobID)
			}
		}
		close(ch)
	}
}

func (s *Store) publish(jobID string, ev Event) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	for ch := range s.subs[jobID] {
		select {
		case ch <- ev:
		default: // drop on slow subscriber rather than block the producer
		}
	}
}

func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
