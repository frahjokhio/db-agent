package sqlsafe

import (
	"context"
	"database/sql"
	"time"

	"github.com/frahjokhio/db-agent/internal/audit"
	"github.com/frahjokhio/db-agent/internal/database"
)

// Runner is the one entry point for running SQL safely: validate -> execute -> audit.
type Runner struct {
	DB      *sql.DB
	Audit   *audit.Log
	Timeout time.Duration
}

// Who is asking (recorded in the audit log).
type Caller struct {
	User   string
	Source string // "cli", "mcp-local", ...
}

// Run validates sql against the schema and policy, executes it, and records the attempt.
func (r *Runner) Run(ctx context.Context, schema *database.Schema, policy Policy, sql string, who Caller) (Result, error) {
	validated, err := Validate(sql, schema, policy)
	if err != nil {
		r.record(who, sql, audit.StatusRejected, err.Error(), nil)
		return Result{}, err
	}

	result, err := Execute(ctx, r.DB, validated, policy.MaxRows, r.Timeout)
	if err != nil {
		r.record(who, validated.SQL, audit.StatusFailed, err.Error(), nil)
		return Result{}, err
	}

	r.record(who, validated.SQL, audit.StatusOK, "", &result)
	return result, nil
}

func (r *Runner) record(who Caller, sql string, status audit.Status, errText string, res *Result) {
	e := audit.Entry{User: who.User, Source: who.Source, SQL: sql, Status: status, Error: errText}
	if res != nil {
		duration, count := res.DurationMs, int64(res.RowCount)
		e.DurationMs, e.RowCount = &duration, &count
	}
	_ = r.Audit.Record(e) // an audit write failure must not hide the query result
}
