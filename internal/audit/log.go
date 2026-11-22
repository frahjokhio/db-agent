// Package audit records every query attempt (ok, rejected, failed). No row data is stored.
package audit

import (
	"database/sql"
	"time"
)

type Status string

const (
	StatusOK       Status = "ok"
	StatusRejected Status = "rejected"
	StatusFailed   Status = "failed"
)

type Entry struct {
	ID         int64
	CreatedAt  time.Time
	User       string
	Source     string
	SQL        string
	Status     Status
	DurationMs *int64
	RowCount   *int64
	Error      string
}

type Log struct{ db *sql.DB }

func New(agentDB *sql.DB) *Log { return &Log{db: agentDB} }

func (l *Log) Record(e Entry) error {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	var errText *string
	if e.Error != "" {
		errText = &e.Error
	}
	_, err := l.db.Exec(
		`INSERT INTO query_log (created_at, user, source, sql, status, duration_ms, row_count, error)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		e.CreatedAt.Format(time.RFC3339Nano), e.User, e.Source, e.SQL, string(e.Status),
		e.DurationMs, e.RowCount, errText,
	)
	return err
}

// Recent returns newest entries first. user "" means all users.
func (l *Log) Recent(limit int, user string) ([]Entry, error) {
	query := "SELECT id, created_at, user, source, sql, status, duration_ms, row_count, error FROM query_log"
	args := []any{}
	if user != "" {
		query += " WHERE user = ?"
		args = append(args, user)
	}
	query += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit)

	rows, err := l.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		var created, status string
		var duration, count sql.NullInt64
		var errText sql.NullString
		if err := rows.Scan(&e.ID, &created, &e.User, &e.Source, &e.SQL, &status, &duration, &count, &errText); err != nil {
			return nil, err
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		e.Status = Status(status)
		if duration.Valid {
			e.DurationMs = &duration.Int64
		}
		if count.Valid {
			e.RowCount = &count.Int64
		}
		e.Error = errText.String
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
