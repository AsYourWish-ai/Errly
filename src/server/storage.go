package main

import (
	"crypto/md5"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Storage persists issues and events in a SQLite database.
// Use path=":memory:" for an in-memory database (testing / ephemeral runs).
type Storage struct {
	db     *sql.DB
	logger *slog.Logger
}

func NewStorage(path string, logger *slog.Logger) (*Storage, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// WAL mode: allows concurrent readers alongside a writer.
	db.Exec("PRAGMA journal_mode=WAL")  //nolint:errcheck
	db.Exec("PRAGMA foreign_keys=ON")  //nolint:errcheck
	db.SetMaxOpenConns(1) // SQLite is not safe with multiple writers

	s := &Storage{db: db, logger: logger}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Storage) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS issues (
			id           TEXT PRIMARY KEY,
			fingerprint  TEXT NOT NULL,
			title        TEXT NOT NULL,
			level        TEXT NOT NULL,
			platform     TEXT NOT NULL DEFAULT '',
			environment  TEXT NOT NULL DEFAULT '',
			project_key  TEXT NOT NULL DEFAULT '',
			count        INTEGER NOT NULL DEFAULT 1,
			status       TEXT NOT NULL DEFAULT 'unresolved',
			first_seen   DATETIME NOT NULL,
			last_seen    DATETIME NOT NULL,
			last_event   TEXT
		);
		CREATE TABLE IF NOT EXISTS events (
			id        TEXT NOT NULL,
			issue_id  TEXT NOT NULL REFERENCES issues(id),
			ts        DATETIME NOT NULL,
			data      TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_issues_last_seen  ON issues(last_seen DESC);
		CREATE INDEX IF NOT EXISTS idx_issues_status     ON issues(status);
		CREATE INDEX IF NOT EXISTS idx_issues_project    ON issues(project_key);
		CREATE INDEX IF NOT EXISTS idx_issues_env        ON issues(environment);
		CREATE INDEX IF NOT EXISTS idx_events_issue      ON events(issue_id, ts DESC);
	`)
	return err
}

func (s *Storage) Close() error { return s.db.Close() }

// SaveEvent persists an event, creating or updating the parent issue.
func (s *Storage) SaveEvent(event *Event) error {
	fp := computeFingerprint(event)
	event.Fingerprint = fp
	issueID := fmt.Sprintf("%x", md5.Sum([]byte(fp)))[:16]

	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}

	eventJSON, err := json.Marshal(event)
	if err != nil {
		return err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	var existing bool
	tx.QueryRow("SELECT 1 FROM issues WHERE id=?", issueID).Scan(&existing) //nolint:errcheck

	if existing {
		_, err = tx.Exec(
			`UPDATE issues SET count=count+1, last_seen=?, last_event=? WHERE id=?`,
			event.Timestamp.UTC().Format(time.RFC3339Nano), string(eventJSON), issueID,
		)
	} else {
		_, err = tx.Exec(
			`INSERT INTO issues(id,fingerprint,title,level,platform,environment,project_key,count,status,first_seen,last_seen,last_event)
			 VALUES(?,?,?,?,?,?,?,1,'unresolved',?,?,?)`,
			issueID, fp, buildTitle(event),
			event.Level, event.Platform, event.Environment, event.ProjectKey,
			event.Timestamp.UTC().Format(time.RFC3339Nano),
			event.Timestamp.UTC().Format(time.RFC3339Nano),
			string(eventJSON),
		)
	}
	if err != nil {
		return err
	}

	// Keep only the 100 most-recent events per issue.
	_, err = tx.Exec(
		`INSERT INTO events(id, issue_id, ts, data) VALUES(?,?,?,?)`,
		event.ID, issueID, event.Timestamp.UTC().Format(time.RFC3339Nano), string(eventJSON),
	)
	if err != nil {
		return err
	}
	tx.Exec( //nolint:errcheck
		`DELETE FROM events WHERE issue_id=? AND id NOT IN (
			SELECT id FROM events WHERE issue_id=? ORDER BY ts DESC LIMIT 100
		)`, issueID, issueID,
	)

	return tx.Commit()
}

// ListIssues returns a filtered, paginated list of issues sorted by last_seen desc.
func (s *Storage) ListIssues(status, project, env string, limit, offset int) ([]*Issue, int, error) {
	where, args := buildWhere(status, project, env)

	var total int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM issues"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, limit, offset)
	rows, err := s.db.Query(
		"SELECT id,fingerprint,title,level,platform,environment,project_key,count,status,first_seen,last_seen,last_event"+
			" FROM issues"+where+
			" ORDER BY last_seen DESC LIMIT ? OFFSET ?", args...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var issues []*Issue
	for rows.Next() {
		iss, err := scanIssue(rows)
		if err != nil {
			return nil, 0, err
		}
		issues = append(issues, iss)
	}
	if issues == nil {
		issues = []*Issue{}
	}
	return issues, total, rows.Err()
}

// GetIssue returns a single issue by ID, or nil if not found.
func (s *Storage) GetIssue(id string) (*Issue, error) {
	row := s.db.QueryRow(
		"SELECT id,fingerprint,title,level,platform,environment,project_key,count,status,first_seen,last_seen,last_event"+
			" FROM issues WHERE id=?", id,
	)
	iss, err := scanIssue(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return iss, err
}

// GetIssueEvents returns up to limit recent events for an issue.
func (s *Storage) GetIssueEvents(issueID string, limit int) ([]*Event, error) {
	rows, err := s.db.Query(
		"SELECT data FROM events WHERE issue_id=? ORDER BY ts DESC LIMIT ?",
		issueID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*Event
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var ev Event
		if err := json.Unmarshal([]byte(raw), &ev); err != nil {
			return nil, err
		}
		events = append(events, &ev)
	}
	return events, rows.Err()
}

// UpdateIssueStatus sets the status of an issue.
func (s *Storage) UpdateIssueStatus(id, status string) error {
	res, err := s.db.Exec("UPDATE issues SET status=? WHERE id=?", status, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("issue not found")
	}
	return nil
}

// DeleteIssue removes an issue and all its events.
func (s *Storage) DeleteIssue(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec("DELETE FROM events WHERE issue_id=?", id); err != nil {
		return err
	}
	res, err := tx.Exec("DELETE FROM issues WHERE id=?", id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("issue not found")
	}
	return tx.Commit()
}

// GetStats returns aggregate counts, optionally filtered by project.
func (s *Storage) GetStats(project string) (*Stats, error) {
	where := ""
	args := []any{}
	if project != "" {
		where = " WHERE project_key=?"
		args = append(args, project)
	}

	since := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339Nano)

	row := s.db.QueryRow(fmt.Sprintf(`
		SELECT
			COUNT(*),
			SUM(CASE WHEN status='unresolved' THEN 1 ELSE 0 END),
			SUM(CASE WHEN status='resolved'   THEN 1 ELSE 0 END),
			SUM(CASE WHEN status='ignored'    THEN 1 ELSE 0 END),
			SUM(count),
			SUM(CASE WHEN last_seen >= ? THEN 1 ELSE 0 END)
		FROM issues%s`, where),
		append([]any{since}, args...)...,
	)

	stats := &Stats{}
	var totalEvents sql.NullInt64
	var unresolved, resolved, ignored sql.NullInt64
	if err := row.Scan(&stats.Total, &unresolved, &resolved, &ignored, &totalEvents, &stats.EventsLast24h); err != nil {
		return nil, err
	}
	stats.Unresolved  = int(unresolved.Int64)
	stats.Resolved    = int(resolved.Int64)
	stats.Ignored     = int(ignored.Int64)
	stats.TotalEvents = int(totalEvents.Int64)
	return stats, nil
}

// ListProjects returns distinct non-empty project keys.
func (s *Storage) ListProjects() ([]string, error) {
	return s.listDistinct("project_key")
}

// ListEnvironments returns distinct non-empty environment values.
func (s *Storage) ListEnvironments() ([]string, error) {
	return s.listDistinct("environment")
}

func (s *Storage) listDistinct(col string) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT DISTINCT `+col+` FROM issues WHERE `+col+` != '' ORDER BY `+col,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var vals []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		vals = append(vals, v)
	}
	return vals, rows.Err()
}

// SearchIssues returns issues whose title contains query (case-insensitive).
func (s *Storage) SearchIssues(query string, limit int) ([]*Issue, error) {
	rows, err := s.db.Query(
		"SELECT id,fingerprint,title,level,platform,environment,project_key,count,status,first_seen,last_seen,last_event"+
			" FROM issues WHERE LOWER(title) LIKE ? ORDER BY last_seen DESC LIMIT ?",
		"%"+strings.ToLower(query)+"%", limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var issues []*Issue
	for rows.Next() {
		iss, err := scanIssue(rows)
		if err != nil {
			return nil, err
		}
		issues = append(issues, iss)
	}
	return issues, rows.Err()
}

// ── helpers ───────────────────────────────────────────────────────────────────

type scanner interface {
	Scan(dest ...any) error
}

func scanIssue(s scanner) (*Issue, error) {
	var iss Issue
	var firstSeen, lastSeen string
	var lastEventJSON sql.NullString

	err := s.Scan(
		&iss.ID, &iss.Fingerprint, &iss.Title,
		&iss.Level, &iss.Platform, &iss.Environment, &iss.ProjectKey,
		&iss.Count, &iss.Status,
		&firstSeen, &lastSeen, &lastEventJSON,
	)
	if err != nil {
		return nil, err
	}
	iss.FirstSeen, _ = time.Parse(time.RFC3339Nano, firstSeen)
	iss.LastSeen, _ = time.Parse(time.RFC3339Nano, lastSeen)
	if lastEventJSON.Valid && lastEventJSON.String != "" {
		var ev Event
		if json.Unmarshal([]byte(lastEventJSON.String), &ev) == nil {
			iss.LastEvent = &ev
		}
	}
	return &iss, nil
}

func buildWhere(status, project, env string) (string, []any) {
	var conditions []string
	var args []any
	if status != "" {
		conditions = append(conditions, "status=?")
		args = append(args, status)
	}
	if project != "" {
		conditions = append(conditions, "project_key=?")
		args = append(args, project)
	}
	if env != "" {
		conditions = append(conditions, "environment=?")
		args = append(args, env)
	}
	if len(conditions) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}

func computeFingerprint(e *Event) string {
	if e.Fingerprint != "" {
		return e.Fingerprint
	}
	key := e.Message
	if e.Exception != nil {
		key = e.Exception.Type + ":" + e.Exception.Value
		if len(e.Stacktrace) > 0 {
			top := e.Stacktrace[0]
			key += fmt.Sprintf("@%s:%d", top.Filename, top.Lineno)
		}
	}
	return fmt.Sprintf("%x", md5.Sum([]byte(key)))
}

func buildTitle(e *Event) string {
	if e.Exception != nil {
		v := e.Exception.Value
		if len(v) > 100 {
			v = v[:100] + "..."
		}
		return e.Exception.Type + ": " + v
	}
	if len(e.Message) > 120 {
		return e.Message[:120] + "..."
	}
	return e.Message
}
