package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"max-proxy-mock/internal/model"

	_ "modernc.org/sqlite"
)

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, q := range []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA foreign_keys=ON`,
		`CREATE TABLE IF NOT EXISTS projects (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, domain TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS endpoints (
			id TEXT PRIMARY KEY, project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			method TEXT NOT NULL, scheme TEXT NOT NULL DEFAULT 'https', host TEXT NOT NULL DEFAULT '', path TEXT NOT NULL,
			status INTEGER NOT NULL DEFAULT 200, request_headers TEXT NOT NULL DEFAULT '{}', request_body TEXT NOT NULL DEFAULT '',
			response_headers TEXT NOT NULL DEFAULT '{}', response_body TEXT NOT NULL DEFAULT '', content_type TEXT NOT NULL DEFAULT '',
			duration_ms INTEGER NOT NULL DEFAULT 0, source TEXT NOT NULL DEFAULT 'recorded', hit_count INTEGER NOT NULL DEFAULT 1,
			last_seen_at TEXT NOT NULL, UNIQUE(project_id, path)
		)`,
		`CREATE TABLE IF NOT EXISTS mock_rules (
			id TEXT PRIMARY KEY, endpoint_id TEXT NOT NULL UNIQUE REFERENCES endpoints(id) ON DELETE CASCADE,
			project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE, method TEXT NOT NULL, host TEXT NOT NULL,
			path TEXT NOT NULL, status INTEGER NOT NULL, headers TEXT NOT NULL DEFAULT '{}', body TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS app_settings (
			key TEXT PRIMARY KEY, value TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_endpoints_project_last_seen ON endpoints(project_id, last_seen_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_mock_rules_match ON mock_rules(host, path, enabled)`,
		`PRAGMA optimize`,
	} {
		if _, err := db.Exec(q); err != nil {
			db.Close()
			return nil, fmt.Errorf("initialize database: %w", err)
		}
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func newID(prefix string) string { return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano()) }
func nowText() string            { return time.Now().UTC().Format(time.RFC3339Nano) }

func (s *Store) Projects(ctx context.Context) ([]model.Project, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,domain,created_at FROM projects ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Project{}
	for rows.Next() {
		var p model.Project
		var t string
		if err := rows.Scan(&p.ID, &p.Name, &p.Domain, &t); err != nil {
			return nil, err
		}
		p.CreatedAt, _ = time.Parse(time.RFC3339Nano, t)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) CreateProject(ctx context.Context, name, domain string) (model.Project, error) {
	p := model.Project{ID: newID("prj"), Name: strings.TrimSpace(name), Domain: normalizeDomain(domain), CreatedAt: time.Now().UTC()}
	if p.Name == "" {
		return p, errors.New("项目名称不能为空")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO projects(id,name,domain,created_at) VALUES(?,?,?,?)`, p.ID, p.Name, p.Domain, p.CreatedAt.Format(time.RFC3339Nano))
	return p, err
}

func (s *Store) UpdateProject(ctx context.Context, id, name, domain string) error {
	domain = normalizeDomain(domain)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, err := tx.ExecContext(ctx, `UPDATE projects SET name=?,domain=? WHERE id=?`, strings.TrimSpace(name), domain, id)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	if _, err = tx.ExecContext(ctx, `UPDATE endpoints SET host=? WHERE project_id=?`, domain, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE mock_rules SET host=? WHERE project_id=?`, domain, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeleteProject(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM projects WHERE id=?`, id)
	return err
}

func normalizeDomain(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	v = strings.TrimPrefix(v, "https://")
	v = strings.TrimPrefix(v, "http://")
	v = strings.Split(v, "/")[0]
	return strings.Split(v, ":")[0]
}

func encode(v any) string { b, _ := json.Marshal(v); return string(b) }
func decodeMap(v string) map[string]string {
	out := map[string]string{}
	_ = json.Unmarshal([]byte(v), &out)
	return out
}

func (s *Store) UpsertEndpoint(ctx context.Context, e model.Endpoint) (model.Endpoint, error) {
	if e.ID == "" {
		e.ID = newID("ep")
	}
	if e.LastSeenAt.IsZero() {
		e.LastSeenAt = time.Now().UTC()
	}
	if e.Source == "" {
		e.Source = "recorded"
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO endpoints(id,project_id,method,scheme,host,path,status,request_headers,request_body,response_headers,response_body,content_type,duration_ms,source,hit_count,last_seen_at)
	VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,1,?)
	ON CONFLICT(project_id,path) DO UPDATE SET method=excluded.method,scheme=excluded.scheme,host=excluded.host,status=excluded.status,
	request_headers=excluded.request_headers,request_body=excluded.request_body,response_headers=excluded.response_headers,response_body=excluded.response_body,
	content_type=excluded.content_type,duration_ms=excluded.duration_ms,hit_count=endpoints.hit_count+1,last_seen_at=excluded.last_seen_at`,
		e.ID, e.ProjectID, e.Method, e.Scheme, e.Host, e.Path, e.Status, encode(e.RequestHeaders), e.RequestBody, encode(e.ResponseHeaders), e.ResponseBody, e.ContentType, e.DurationMs, e.Source, e.LastSeenAt.Format(time.RFC3339Nano))
	if err != nil {
		return e, err
	}
	row := s.db.QueryRowContext(ctx, `SELECT id,hit_count,source FROM endpoints WHERE project_id=? AND path=?`, e.ProjectID, e.Path)
	_ = row.Scan(&e.ID, &e.HitCount, &e.Source)
	return e, nil
}

func (s *Store) Endpoints(ctx context.Context, projectID string) ([]model.Endpoint, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT e.id,e.project_id,e.method,e.scheme,e.host,e.path,e.status,e.request_headers,e.request_body,e.response_headers,e.response_body,e.content_type,e.duration_ms,e.source,e.hit_count,e.last_seen_at,CASE WHEN m.id IS NULL THEN 0 ELSE 1 END
	FROM endpoints e LEFT JOIN mock_rules m ON m.endpoint_id=e.id WHERE e.project_id=? ORDER BY e.last_seen_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Endpoint{}
	for rows.Next() {
		var e model.Endpoint
		var rh, sh, t string
		var mocked int
		if err := rows.Scan(&e.ID, &e.ProjectID, &e.Method, &e.Scheme, &e.Host, &e.Path, &e.Status, &rh, &e.RequestBody, &sh, &e.ResponseBody, &e.ContentType, &e.DurationMs, &e.Source, &e.HitCount, &t, &mocked); err != nil {
			return nil, err
		}
		e.RequestHeaders = decodeMap(rh)
		e.ResponseHeaders = decodeMap(sh)
		e.LastSeenAt, _ = time.Parse(time.RFC3339Nano, t)
		e.Mocked = mocked == 1
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) Endpoint(ctx context.Context, id string) (model.Endpoint, error) {
	var e model.Endpoint
	var rh, sh, t string
	err := s.db.QueryRowContext(ctx, `SELECT id,project_id,method,scheme,host,path,status,request_headers,request_body,response_headers,response_body,content_type,duration_ms,source,hit_count,last_seen_at FROM endpoints WHERE id=?`, id).Scan(&e.ID, &e.ProjectID, &e.Method, &e.Scheme, &e.Host, &e.Path, &e.Status, &rh, &e.RequestBody, &sh, &e.ResponseBody, &e.ContentType, &e.DurationMs, &e.Source, &e.HitCount, &t)
	e.RequestHeaders = decodeMap(rh)
	e.ResponseHeaders = decodeMap(sh)
	e.LastSeenAt, _ = time.Parse(time.RFC3339Nano, t)
	return e, err
}

func (s *Store) DeleteEndpoint(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM endpoints WHERE id=?`, id)
	return err
}

func (s *Store) CreateMock(ctx context.Context, endpointID string) (model.MockRule, error) {
	e, err := s.Endpoint(ctx, endpointID)
	if err != nil {
		return model.MockRule{}, err
	}
	m := model.MockRule{ID: newID("mock"), EndpointID: e.ID, ProjectID: e.ProjectID, Method: e.Method, Host: e.Host, Path: e.Path, Status: e.Status, Headers: e.ResponseHeaders, Body: e.ResponseBody, Enabled: true, CreatedAt: time.Now().UTC()}
	_, err = s.db.ExecContext(ctx, `INSERT INTO mock_rules(id,endpoint_id,project_id,method,host,path,status,headers,body,enabled,created_at) VALUES(?,?,?,?,?,?,?,?,?,1,?) ON CONFLICT(endpoint_id) DO UPDATE SET method=excluded.method,host=excluded.host,path=excluded.path,status=excluded.status,headers=excluded.headers,body=excluded.body,enabled=1`, m.ID, m.EndpointID, m.ProjectID, m.Method, m.Host, m.Path, m.Status, encode(m.Headers), m.Body, m.CreatedAt.Format(time.RFC3339Nano))
	return m, err
}

func (s *Store) Mocks(ctx context.Context) ([]model.MockRule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,endpoint_id,project_id,method,host,path,status,headers,body,enabled,created_at FROM mock_rules`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.MockRule{}
	for rows.Next() {
		var m model.MockRule
		var h, t string
		var enabled int
		if err := rows.Scan(&m.ID, &m.EndpointID, &m.ProjectID, &m.Method, &m.Host, &m.Path, &m.Status, &h, &m.Body, &enabled, &t); err != nil {
			return nil, err
		}
		m.Headers = decodeMap(h)
		m.Enabled = enabled == 1
		m.CreatedAt, _ = time.Parse(time.RFC3339Nano, t)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) SetMockEnabled(ctx context.Context, id string, enabled bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE mock_rules SET enabled=? WHERE id=?`, enabled, id)
	return err
}
func (s *Store) UpdateMock(ctx context.Context, id string, status *int, body *string, headers map[string]string) error {
	if status != nil {
		if _, err := s.db.ExecContext(ctx, `UPDATE mock_rules SET status=? WHERE id=?`, *status, id); err != nil {
			return err
		}
	}
	if body != nil {
		if _, err := s.db.ExecContext(ctx, `UPDATE mock_rules SET body=? WHERE id=?`, *body, id); err != nil {
			return err
		}
	}
	if headers != nil {
		if _, err := s.db.ExecContext(ctx, `UPDATE mock_rules SET headers=? WHERE id=?`, encode(headers), id); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) DeleteMock(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM mock_rules WHERE id=?`, id)
	return err
}

func (s *Store) Setting(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM app_settings WHERE key=?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return value, err == nil, err
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO app_settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

func (s *Store) DeleteSetting(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM app_settings WHERE key=?`, key)
	return err
}
