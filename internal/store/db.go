package store

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

// Store is a thin wrapper around the SQLite connection plus an in-memory
// pubsub for SSE subscribers. Single replica only — SQLite is the constraint.
type Store struct {
	DB *sql.DB

	subMu sync.Mutex
	subs  map[string]map[chan Event]struct{} // jobId → subscribers
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite + writers => serialize

	s := &Store{DB: db, subs: map[string]map[chan Event]struct{}{}}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.DB.Close() }

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS services (
			id                    TEXT PRIMARY KEY,
			spec_json             TEXT NOT NULL,
			status                TEXT NOT NULL,
			last_ready_spec_json  TEXT,
			created_at            INTEGER NOT NULL,
			updated_at            INTEGER NOT NULL
		)`,
		`ALTER TABLE services ADD COLUMN job_id TEXT`,
		`CREATE TABLE IF NOT EXISTS jobs (
			id            TEXT PRIMARY KEY,
			release       TEXT NOT NULL,
			namespace     TEXT NOT NULL,
			operation     TEXT NOT NULL,
			status        TEXT NOT NULL,
			events_jsonl  TEXT NOT NULL DEFAULT '',
			started_at    INTEGER NOT NULL,
			ended_at      INTEGER,
			initiator     TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS jobs_release_idx ON jobs(release, namespace)`,
		`CREATE TABLE IF NOT EXISTS disabled_resources (
			release      TEXT NOT NULL,
			namespace    TEXT NOT NULL,
			kind         TEXT NOT NULL,
			name         TEXT NOT NULL,
			resource_ns  TEXT NOT NULL DEFAULT '',
			disabled_at  INTEGER NOT NULL,
			PRIMARY KEY (release, namespace, kind, name, resource_ns)
		)`,
		`CREATE TABLE IF NOT EXISTS chart_audit (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			ts            INTEGER NOT NULL,
			initiator     TEXT,
			operation     TEXT NOT NULL,
			release       TEXT NOT NULL,
			namespace     TEXT NOT NULL,
			payload_hash  TEXT,
			outcome       TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS rollout_watches (
			id              TEXT PRIMARY KEY,
			namespace       TEXT NOT NULL,
			deployment      TEXT NOT NULL,
			container       TEXT NOT NULL DEFAULT '',
			enabled         INTEGER NOT NULL DEFAULT 1,
			tracked_image   TEXT NOT NULL,
			current_image   TEXT NOT NULL DEFAULT '',
			current_digest  TEXT NOT NULL DEFAULT '',
			latest_image    TEXT NOT NULL DEFAULT '',
			latest_digest   TEXT NOT NULL DEFAULT '',
			last_result     TEXT NOT NULL DEFAULT '',
			last_error      TEXT NOT NULL DEFAULT '',
			check_count     INTEGER NOT NULL DEFAULT 0,
			sync_count      INTEGER NOT NULL DEFAULT 0,
			events_jsonl    TEXT NOT NULL DEFAULT '',
			created_at      INTEGER NOT NULL,
			updated_at      INTEGER NOT NULL,
			last_checked_at INTEGER,
			last_synced_at  INTEGER
		)`,
		`ALTER TABLE rollout_watches ADD COLUMN enabled INTEGER NOT NULL DEFAULT 1`,
		`CREATE UNIQUE INDEX IF NOT EXISTS rollout_watches_target_idx ON rollout_watches(namespace, deployment, container)`,
	}
	for _, q := range stmts {
		if _, err := s.DB.Exec(q); err != nil {
			// Tolerate ALTER TABLE re-runs against an already-migrated schema.
			// modernc/sqlite emits "duplicate column name" for these.
			if strings.Contains(err.Error(), "duplicate column") {
				continue
			}
			return fmt.Errorf("migrate: %w (%s)", err, q[:40])
		}
	}
	return nil
}
