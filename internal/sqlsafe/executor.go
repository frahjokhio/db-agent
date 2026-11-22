package sqlsafe

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/frahjokhio/db-agent/internal/database"
)

const mysqlTimeoutError = 3024 // "maximum statement execution time exceeded"

// Failed means the database returned an error. The message is safe to show.
type Failed struct{ Msg string }

func (e *Failed) Error() string { return e.Msg }

// Result of a query, with JSON-safe values.
type Result struct {
	Columns    []string
	Rows       [][]any
	RowCount   int
	Truncated  bool // more rows existed than MaxRows
	DurationMs int64
}

// Execute runs a validated query in a read-only session with a time limit.
func Execute(ctx context.Context, db *sql.DB, q Validated, maxRows int, timeout time.Duration) (Result, error) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(ctx, timeout+5*time.Second)
	defer cancel()

	conn, err := db.Conn(ctx)
	if err != nil {
		return Result{}, failed(err, timeout)
	}
	defer conn.Close()

	// Second safety layer after the read-only DB user.
	setup := []string{
		"SET SESSION TRANSACTION READ ONLY",
		fmt.Sprintf("SET SESSION max_execution_time = %d", timeout.Milliseconds()),
	}
	for _, stmt := range setup {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			return Result{}, failed(err, timeout)
		}
	}

	// No arguments: the SQL is sent as-is, so '%' and ':' in literals are safe.
	rows, err := conn.QueryContext(ctx, q.SQL)
	if err != nil {
		return Result{}, failed(err, timeout)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return Result{}, failed(err, timeout)
	}
	types, err := rows.ColumnTypes()
	if err != nil {
		return Result{}, failed(err, timeout)
	}

	result := Result{Columns: columns, Rows: [][]any{}}
	for rows.Next() {
		if len(result.Rows) == maxRows {
			result.Truncated = true
			break
		}
		values := make([]any, len(columns))
		ptrs := make([]any, len(columns))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return Result{}, failed(err, timeout)
		}
		for i, v := range values {
			values[i] = jsonSafe(v, types[i].DatabaseTypeName())
		}
		result.Rows = append(result.Rows, values)
	}
	if err := rows.Err(); err != nil {
		return Result{}, failed(err, timeout)
	}

	result.RowCount = len(result.Rows)
	result.DurationMs = time.Since(started).Milliseconds()
	return result, nil
}

func jsonSafe(v any, dbType string) any {
	switch val := v.(type) {
	case nil, int64, float64, bool, string:
		return val
	case []byte:
		if strings.Contains(dbType, "BLOB") || strings.Contains(dbType, "BINARY") {
			return fmt.Sprintf("<binary %d bytes>", len(val))
		}
		return string(val) // DECIMAL stays an exact string, e.g. "203000.00"
	case time.Time:
		if dbType == "DATE" {
			return val.Format("2006-01-02")
		}
		return val.Format("2006-01-02T15:04:05")
	default:
		return fmt.Sprint(val)
	}
}

func failed(err error, timeout time.Duration) error {
	var my *mysql.MySQLError
	if errors.As(err, &my) && my.Number == mysqlTimeoutError || errors.Is(err, context.DeadlineExceeded) {
		return &Failed{fmt.Sprintf("Query took longer than %ds and was stopped.", int(timeout.Seconds()))}
	}
	return &Failed{database.SafeError(err).Error()}
}
