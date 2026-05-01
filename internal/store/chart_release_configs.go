package store

import (
	"database/sql"
	"errors"
	"time"
)

type ChartReleaseConfig struct {
	Release              string     `json:"release"`
	Namespace            string     `json:"namespace"`
	SourceJSON           string     `json:"-"`
	SourceType           string     `json:"source_type,omitempty"`
	SourceAuthConfigured bool       `json:"source_auth_configured,omitempty"`
	MonitorEnabled       bool       `json:"monitor_enabled"`
	CurrentVersion       string     `json:"current_version,omitempty"`
	LatestVersion        string     `json:"latest_version,omitempty"`
	LastResult           string     `json:"last_result,omitempty"`
	LastError            string     `json:"last_error,omitempty"`
	CheckCount           int64      `json:"check_count"`
	SyncCount            int64      `json:"sync_count"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
	LastCheckedAt        *time.Time `json:"last_checked_at,omitempty"`
	LastSyncedAt         *time.Time `json:"last_synced_at,omitempty"`
}

type ChartReleaseCheck struct {
	CurrentVersion string
	LatestVersion  string
	Result         string
	Error          string
	Applied        bool
}

func (s *Store) UpsertChartReleaseConfig(release, namespace, sourceJSON, sourceType string, sourceAuthConfigured bool, currentVersion string) (*ChartReleaseConfig, error) {
	now := time.Now().UnixMilli()
	sourceAuthInt := 0
	if sourceAuthConfigured {
		sourceAuthInt = 1
	}
	_, err := s.DB.Exec(
		`INSERT INTO chart_release_configs (
			release, namespace, source_json, source_type, source_auth_configured,
			current_version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(release, namespace) DO UPDATE SET
			source_json = excluded.source_json,
			source_type = excluded.source_type,
			source_auth_configured = excluded.source_auth_configured,
			current_version = CASE
				WHEN excluded.current_version <> '' THEN excluded.current_version
				ELSE chart_release_configs.current_version
			END,
			updated_at = excluded.updated_at`,
		release, namespace, sourceJSON, sourceType, sourceAuthInt, currentVersion, now, now,
	)
	if err != nil {
		return nil, err
	}
	return s.GetChartReleaseConfig(release, namespace)
}

func (s *Store) GetChartReleaseConfig(release, namespace string) (*ChartReleaseConfig, error) {
	row := s.DB.QueryRow(
		`SELECT release, namespace, source_json, source_type, source_auth_configured,
		        monitor_enabled, current_version, latest_version, last_result, last_error,
		        check_count, sync_count, created_at, updated_at, last_checked_at, last_synced_at
		   FROM chart_release_configs
		  WHERE release = ? AND namespace = ?`,
		release, namespace,
	)
	return scanChartReleaseConfig(row)
}

func (s *Store) ListChartReleaseConfigs() ([]*ChartReleaseConfig, error) {
	rows, err := s.DB.Query(
		`SELECT release, namespace, source_json, source_type, source_auth_configured,
		        monitor_enabled, current_version, latest_version, last_result, last_error,
		        check_count, sync_count, created_at, updated_at, last_checked_at, last_synced_at
		   FROM chart_release_configs
		  ORDER BY updated_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*ChartReleaseConfig
	for rows.Next() {
		record, err := scanChartReleaseConfig(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func (s *Store) SetChartReleaseMonitorEnabled(release, namespace string, enabled bool) error {
	enabledInt := 0
	if enabled {
		enabledInt = 1
	}
	_, err := s.DB.Exec(
		`UPDATE chart_release_configs
		    SET monitor_enabled = ?, updated_at = ?
		  WHERE release = ? AND namespace = ?`,
		enabledInt, time.Now().UnixMilli(), release, namespace,
	)
	return err
}

func (s *Store) RecordChartReleaseCheck(release, namespace string, check ChartReleaseCheck) error {
	now := time.Now().UnixMilli()
	var lastSynced any
	syncDelta := 0
	if check.Applied {
		syncDelta = 1
		lastSynced = now
	}
	_, err := s.DB.Exec(
		`UPDATE chart_release_configs
		    SET current_version = COALESCE(NULLIF(?, ''), current_version),
		        latest_version = COALESCE(NULLIF(?, ''), latest_version),
		        last_result = ?,
		        last_error = ?,
		        check_count = check_count + 1,
		        sync_count = sync_count + ?,
		        updated_at = ?,
		        last_checked_at = ?,
		        last_synced_at = COALESCE(?, last_synced_at)
		  WHERE release = ? AND namespace = ?`,
		check.CurrentVersion,
		check.LatestVersion,
		check.Result,
		check.Error,
		syncDelta,
		now,
		now,
		lastSynced,
		release,
		namespace,
	)
	return err
}

func (s *Store) DeleteChartReleaseConfig(release, namespace string) error {
	_, err := s.DB.Exec(`DELETE FROM chart_release_configs WHERE release = ? AND namespace = ?`, release, namespace)
	return err
}

func scanChartReleaseConfig(r rowScanner) (*ChartReleaseConfig, error) {
	var (
		record               ChartReleaseConfig
		sourceAuthConfigured int
		monitorEnabled       int
		created              int64
		updated              int64
		lastChecked          sql.NullInt64
		lastSynced           sql.NullInt64
	)
	if err := r.Scan(
		&record.Release,
		&record.Namespace,
		&record.SourceJSON,
		&record.SourceType,
		&sourceAuthConfigured,
		&monitorEnabled,
		&record.CurrentVersion,
		&record.LatestVersion,
		&record.LastResult,
		&record.LastError,
		&record.CheckCount,
		&record.SyncCount,
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
	record.SourceAuthConfigured = sourceAuthConfigured == 1
	record.MonitorEnabled = monitorEnabled == 1
	record.CreatedAt = time.UnixMilli(created)
	record.UpdatedAt = time.UnixMilli(updated)
	if lastChecked.Valid {
		value := time.UnixMilli(lastChecked.Int64)
		record.LastCheckedAt = &value
	}
	if lastSynced.Valid {
		value := time.UnixMilli(lastSynced.Int64)
		record.LastSyncedAt = &value
	}
	return &record, nil
}
