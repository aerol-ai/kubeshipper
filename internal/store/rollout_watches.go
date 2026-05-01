package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type RolloutWatch struct {
	ID            string              `json:"id"`
	Namespace     string              `json:"namespace"`
	Deployment    string              `json:"deployment"`
	Container     string              `json:"container,omitempty"`
	Enabled       bool                `json:"enabled"`
	TrackedImage  string              `json:"tracked_image"`
	CurrentImage  string              `json:"current_image,omitempty"`
	CurrentDigest string              `json:"current_digest,omitempty"`
	LatestImage   string              `json:"latest_image,omitempty"`
	LatestDigest  string              `json:"latest_digest,omitempty"`
	LastResult    string              `json:"last_result,omitempty"`
	LastError     string              `json:"last_error,omitempty"`
	CheckCount    int64               `json:"check_count"`
	SyncCount     int64               `json:"sync_count"`
	CreatedAt     time.Time           `json:"created_at"`
	UpdatedAt     time.Time           `json:"updated_at"`
	LastCheckedAt *time.Time          `json:"last_checked_at,omitempty"`
	LastSyncedAt  *time.Time          `json:"last_synced_at,omitempty"`
	Timeline      []RolloutWatchEvent `json:"timeline,omitempty"`
}

type RolloutWatchEvent struct {
	Type          string `json:"type"`
	Result        string `json:"result,omitempty"`
	Message       string `json:"message,omitempty"`
	CurrentImage  string `json:"current_image,omitempty"`
	CurrentDigest string `json:"current_digest,omitempty"`
	LatestImage   string `json:"latest_image,omitempty"`
	LatestDigest  string `json:"latest_digest,omitempty"`
	Error         string `json:"error,omitempty"`
	TS            int64  `json:"ts"`
}

type RolloutWatchCheck struct {
	TrackedImage  string
	Result        string
	Message       string
	CurrentImage  string
	CurrentDigest string
	LatestImage   string
	LatestDigest  string
	Error         string
	Applied       bool
	RecordEvent   bool
	EventType     string
}

func (s *Store) UpsertRolloutWatch(namespace, deployment, container, trackedImage, currentImage, currentDigest string) (*RolloutWatch, bool, error) {
	now := time.Now().UnixMilli()
	existing, err := s.getRolloutWatchByTarget(namespace, deployment, container)
	if err != nil {
		return nil, false, err
	}
	if existing != nil {
		_, err = s.DB.Exec(
			`UPDATE rollout_watches
			    SET tracked_image = ?,
			        current_image = COALESCE(NULLIF(?, ''), current_image),
			        current_digest = COALESCE(NULLIF(?, ''), current_digest),
			        last_result = 'registered',
			        last_error = '',
			        updated_at = ?
			  WHERE id = ?`,
			trackedImage, currentImage, currentDigest, now, existing.ID,
		)
		if err != nil {
			return nil, false, err
		}
		watch, err := s.GetRolloutWatch(existing.ID)
		return watch, false, err
	}

	id, err := newID()
	if err != nil {
		return nil, false, err
	}
	_, err = s.DB.Exec(
		`INSERT INTO rollout_watches (
			id, namespace, deployment, container, enabled, tracked_image,
			current_image, current_digest, last_result,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, 1, ?, ?, ?, 'registered', ?, ?)`,
		id, namespace, deployment, container, trackedImage, currentImage, currentDigest, now, now,
	)
	if err != nil {
		return nil, false, err
	}
	watch, err := s.GetRolloutWatch(id)
	return watch, true, err
}

func (s *Store) GetRolloutWatch(id string) (*RolloutWatch, error) {
	row := s.DB.QueryRow(
		`SELECT id, namespace, deployment, container, enabled, tracked_image, current_image, current_digest,
		        latest_image, latest_digest, last_result, last_error, check_count, sync_count,
		        events_jsonl, created_at, updated_at, last_checked_at, last_synced_at
		   FROM rollout_watches
		  WHERE id = ?`,
		id,
	)
	return scanRolloutWatch(row)
}

func (s *Store) ListRolloutWatches() ([]*RolloutWatch, error) {
	rows, err := s.DB.Query(
		`SELECT id, namespace, deployment, container, enabled, tracked_image, current_image, current_digest,
		        latest_image, latest_digest, last_result, last_error, check_count, sync_count,
		        events_jsonl, created_at, updated_at, last_checked_at, last_synced_at
		   FROM rollout_watches
		  ORDER BY updated_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*RolloutWatch
	for rows.Next() {
		watch, err := scanRolloutWatch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, watch)
	}
	return out, rows.Err()
}

func (s *Store) DeleteRolloutWatch(id string) error {
	_, err := s.DB.Exec(`DELETE FROM rollout_watches WHERE id = ?`, id)
	return err
}

func (s *Store) SetRolloutWatchEnabled(id string, enabled bool) error {
	enabledInt := 0
	if enabled {
		enabledInt = 1
	}
	_, err := s.DB.Exec(
		`UPDATE rollout_watches SET enabled = ?, updated_at = ? WHERE id = ?`,
		enabledInt, time.Now().UnixMilli(), id,
	)
	return err
}

func (s *Store) UpdateRolloutWatchResult(id, result, errText string) error {
	_, err := s.DB.Exec(
		`UPDATE rollout_watches SET last_result = ?, last_error = ?, updated_at = ? WHERE id = ?`,
		result, errText, time.Now().UnixMilli(), id,
	)
	return err
}

func (s *Store) RecordRolloutWatchCheck(id string, check RolloutWatchCheck) error {
	now := time.Now().UnixMilli()
	var lastSynced any
	syncDelta := 0
	if check.Applied {
		syncDelta = 1
		lastSynced = now
	}
	_, err := s.DB.Exec(
		`UPDATE rollout_watches
		    SET tracked_image = COALESCE(NULLIF(?, ''), tracked_image),
		        current_image = COALESCE(NULLIF(?, ''), current_image),
		        current_digest = COALESCE(NULLIF(?, ''), current_digest),
		        latest_image = COALESCE(NULLIF(?, ''), latest_image),
		        latest_digest = COALESCE(NULLIF(?, ''), latest_digest),
		        last_result = ?,
		        last_error = ?,
		        check_count = check_count + 1,
		        sync_count = sync_count + ?,
		        updated_at = ?,
		        last_checked_at = ?,
		        last_synced_at = COALESCE(?, last_synced_at)
		  WHERE id = ?`,
		check.TrackedImage,
		check.CurrentImage,
		check.CurrentDigest,
		check.LatestImage,
		check.LatestDigest,
		check.Result,
		check.Error,
		syncDelta,
		now,
		now,
		lastSynced,
		id,
	)
	if err != nil {
		return err
	}

	if check.RecordEvent {
		eventType := check.EventType
		if eventType == "" {
			eventType = "checked"
		}
		return s.AppendRolloutWatchEvent(id, RolloutWatchEvent{
			Type:          eventType,
			Result:        check.Result,
			Message:       check.Message,
			CurrentImage:  check.CurrentImage,
			CurrentDigest: check.CurrentDigest,
			LatestImage:   check.LatestImage,
			LatestDigest:  check.LatestDigest,
			Error:         check.Error,
			TS:            now,
		})
	}
	return nil
}

func (s *Store) AppendRolloutWatchEvent(id string, ev RolloutWatchEvent) error {
	if ev.TS == 0 {
		ev.TS = time.Now().UnixMilli()
	}
	line, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(
		`UPDATE rollout_watches SET events_jsonl = events_jsonl || ? || char(10), updated_at = ? WHERE id = ?`,
		string(line), time.Now().UnixMilli(), id,
	)
	return err
}

func (s *Store) getRolloutWatchByTarget(namespace, deployment, container string) (*RolloutWatch, error) {
	row := s.DB.QueryRow(
		`SELECT id, namespace, deployment, container, enabled, tracked_image, current_image, current_digest,
		        latest_image, latest_digest, last_result, last_error, check_count, sync_count,
		        events_jsonl, created_at, updated_at, last_checked_at, last_synced_at
		   FROM rollout_watches
		  WHERE namespace = ? AND deployment = ? AND container = ?`,
		namespace, deployment, container,
	)
	return scanRolloutWatch(row)
}

func scanRolloutWatch(r rowScanner) (*RolloutWatch, error) {
	var (
		watch       RolloutWatch
		events      string
		created     int64
		updated     int64
		lastChecked sql.NullInt64
		lastSynced  sql.NullInt64
	)
	if err := r.Scan(
		&watch.ID,
		&watch.Namespace,
		&watch.Deployment,
		&watch.Container,
		&watch.Enabled,
		&watch.TrackedImage,
		&watch.CurrentImage,
		&watch.CurrentDigest,
		&watch.LatestImage,
		&watch.LatestDigest,
		&watch.LastResult,
		&watch.LastError,
		&watch.CheckCount,
		&watch.SyncCount,
		&events,
		&created,
		&updated,
		&lastChecked,
		&lastSynced,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	watch.CreatedAt = time.UnixMilli(created)
	watch.UpdatedAt = time.UnixMilli(updated)
	if lastChecked.Valid {
		t := time.UnixMilli(lastChecked.Int64)
		watch.LastCheckedAt = &t
	}
	if lastSynced.Valid {
		t := time.UnixMilli(lastSynced.Int64)
		watch.LastSyncedAt = &t
	}
	watch.Timeline = parseRolloutWatchEvents(events)
	return &watch, nil
}

func parseRolloutWatchEvents(jsonl string) []RolloutWatchEvent {
	out := []RolloutWatchEvent{}
	if jsonl == "" {
		return out
	}
	start := 0
	for i := 0; i <= len(jsonl); i++ {
		if i == len(jsonl) || jsonl[i] == '\n' {
			if i > start {
				var ev RolloutWatchEvent
				if err := json.Unmarshal([]byte(jsonl[start:i]), &ev); err == nil {
					out = append(out, ev)
				}
			}
			start = i + 1
		}
	}
	return out
}
