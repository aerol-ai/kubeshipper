package store

import "time"

type DisabledResource struct {
	Release      string `json:"release"`
	Namespace    string `json:"namespace"`
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	ResourceNs   string `json:"resource_ns"`
	DisabledAt   int64  `json:"disabled_at"`
}

func (s *Store) RecordDisabled(release, namespace, kind, name, resourceNs string) error {
	_, err := s.DB.Exec(
		`INSERT OR IGNORE INTO disabled_resources (release, namespace, kind, name, resource_ns, disabled_at) VALUES (?, ?, ?, ?, ?, ?)`,
		release, namespace, kind, name, resourceNs, time.Now().UnixMilli(),
	)
	return err
}

func (s *Store) ClearDisabled(release, namespace, kind, name, resourceNs string) error {
	_, err := s.DB.Exec(
		`DELETE FROM disabled_resources WHERE release = ? AND namespace = ? AND kind = ? AND name = ? AND resource_ns = ?`,
		release, namespace, kind, name, resourceNs,
	)
	return err
}

func (s *Store) ListDisabled(release, namespace string) ([]DisabledResource, error) {
	rows, err := s.DB.Query(
		`SELECT release, namespace, kind, name, resource_ns, disabled_at FROM disabled_resources WHERE release = ? AND namespace = ? ORDER BY disabled_at`,
		release, namespace,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DisabledResource{}
	for rows.Next() {
		var d DisabledResource
		if err := rows.Scan(&d.Release, &d.Namespace, &d.Kind, &d.Name, &d.ResourceNs, &d.DisabledAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
