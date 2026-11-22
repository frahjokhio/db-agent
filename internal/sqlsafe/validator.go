// Package sqlsafe checks and runs AI-written SQL. Never trust the AI.
//
// Rules:
//   - Exactly one read-only query (SELECT / UNION / WITH ... SELECT).
//   - No writes, DDL, SET, CALL, locks (FOR UPDATE), INTO OUTFILE, variables.
//   - Only known tables of the configured database (blocks system schemas).
//   - No sensitive columns (passwords, tokens...), no SELECT * on tables that have them.
//   - No dangerous functions (SLEEP, BENCHMARK, LOAD_FILE, locks...).
//   - Row limit written into the SQL.
//
// The parser reads MySQL's executable comments (/*! ... */) the same way MySQL does,
// so hidden code is checked too. The SQL that runs is rebuilt from the parse tree.
package sqlsafe

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/pingcap/tidb/pkg/parser"
	"github.com/pingcap/tidb/pkg/parser/ast"
	"github.com/pingcap/tidb/pkg/parser/format"
	_ "github.com/pingcap/tidb/pkg/parser/test_driver" // provides literal value nodes

	"github.com/frahjokhio/db-agent/internal/database"
)

var forbiddenFunctions = map[string]bool{
	"sleep": true, "benchmark": true, "load_file": true, "get_lock": true, "release_lock": true,
	"release_all_locks": true, "is_free_lock": true, "is_used_lock": true, "master_pos_wait": true,
	"source_pos_wait": true, "wait_for_executed_gtid_set": true,
}

// DefaultSensitivePattern hides typical secret columns (Laravel's password, remember_token, ...).
const DefaultSensitivePattern = `(?i)password|passwd|secret|token|api_?key|private_?key|two_factor`

// Rejected means the SQL failed a safety check. The message is safe to show.
type Rejected struct{ Msg string }

func (e *Rejected) Error() string { return e.Msg }

func reject(format string, args ...any) error { return &Rejected{fmt.Sprintf(format, args...)} }

// Policy controls what queries may do.
type Policy struct {
	MaxRows        int
	BlockedTables  map[string]bool // lower-case table names
	Sensitive      *regexp.Regexp
	ExtraSensitive map[string]bool // lower-case "table.column": hidden by rules
	AllowColumns   map[string]bool // lower-case "table.column": safe despite the name pattern
}

var defaultSensitive = regexp.MustCompile(DefaultSensitivePattern)

func DefaultPolicy(maxRows int) Policy {
	return Policy{
		MaxRows:        maxRows,
		BlockedTables:  map[string]bool{},
		Sensitive:      defaultSensitive,
		ExtraSensitive: map[string]bool{},
		AllowColumns:   map[string]bool{},
	}
}

func (p Policy) IsSensitive(table, column string) bool {
	key := strings.ToLower(table + "." + column)
	if p.ExtraSensitive[key] {
		return true
	}
	if p.AllowColumns[key] {
		return false
	}
	return p.Sensitive != nil && p.Sensitive.MatchString(column)
}

func (p Policy) IsBlocked(table string) bool { return p.BlockedTables[strings.ToLower(table)] }

// Validated is SQL that passed every check.
type Validated struct {
	SQL      string // the exact SQL that will run
	Tables   []string
	RowLimit int
}

// Validate checks sql against the schema and policy.
func Validate(sql string, schema *database.Schema, p Policy) (Validated, error) {
	if strings.TrimSpace(sql) == "" {
		return Validated{}, reject("Empty query.")
	}
	stmts, _, err := parser.New().Parse(sql, "", "")
	if err != nil {
		return Validated{}, reject("Could not parse SQL: %s", firstLine(err.Error()))
	}
	if len(stmts) != 1 {
		return Validated{}, reject("Only one statement is allowed.")
	}

	root := stmts[0]
	switch root.(type) {
	case *ast.SelectStmt, *ast.SetOprStmt:
	default:
		return Validated{}, reject("Only SELECT queries are allowed.")
	}

	v := &visitor{}
	root.Accept(v)
	if v.err != nil {
		return Validated{}, v.err
	}

	tables, err := checkTables(v, schema, p)
	if err != nil {
		return Validated{}, err
	}
	if err := checkColumns(v, schema, p, tables); err != nil {
		return Validated{}, err
	}

	limit := applyLimit(root, p.MaxRows+1) // +1 lets the executor detect "more rows exist"

	var sb strings.Builder
	if err := root.Restore(format.NewRestoreCtx(format.DefaultRestoreFlags, &sb)); err != nil {
		return Validated{}, reject("Could not rebuild SQL: %v", err)
	}
	return Validated{SQL: sb.String(), Tables: tables, RowLimit: limit}, nil
}

type tableRef struct{ schema, name string }

// visitor walks every node, collecting tables/columns and stopping on forbidden things.
type visitor struct {
	err      error
	tables   []tableRef
	columns  []string // lower-case column names
	cteNames map[string]bool
	hasStar  bool
}

func (v *visitor) Enter(n ast.Node) (ast.Node, bool) {
	if v.err != nil {
		return n, true
	}
	switch node := n.(type) {
	case *ast.SelectStmt:
		if node.LockInfo != nil && node.LockInfo.LockType != ast.SelectLockNone {
			v.err = reject("Locking reads (FOR UPDATE / LOCK IN SHARE MODE) are not allowed.")
		}
		if node.SelectIntoOpt != nil {
			v.err = reject("SELECT ... INTO is not allowed.")
		}
	case *ast.VariableExpr:
		v.err = reject("Variables (@var, @@setting) are not allowed.")
	case *ast.FuncCallExpr:
		if forbiddenFunctions[node.FnName.L] {
			v.err = reject("Function not allowed: %s.", strings.ToUpper(node.FnName.O))
		}
	case *ast.WithClause:
		if v.cteNames == nil {
			v.cteNames = map[string]bool{}
		}
		for _, cte := range node.CTEs {
			v.cteNames[cte.Name.L] = true
		}
	case *ast.TableName:
		v.tables = append(v.tables, tableRef{schema: node.Schema.O, name: node.Name.O})
	case *ast.ColumnName:
		v.columns = append(v.columns, node.Name.L)
	case *ast.SelectField:
		if node.WildCard != nil {
			v.hasStar = true
		}
	}
	return n, v.err != nil
}

func (v *visitor) Leave(n ast.Node) (ast.Node, bool) { return n, v.err == nil }

func checkTables(v *visitor, schema *database.Schema, p Policy) ([]string, error) {
	used := map[string]bool{}
	for _, ref := range v.tables {
		if ref.schema != "" && !strings.EqualFold(ref.schema, schema.Database) {
			return nil, reject("Only the '%s' database can be queried.", schema.Database)
		}
		if ref.schema == "" && v.cteNames[strings.ToLower(ref.name)] {
			continue
		}
		known := schema.Table(ref.name)
		if known == nil {
			return nil, reject("Unknown table: %s.", ref.name)
		}
		if p.IsBlocked(known.Name) {
			return nil, reject("Table not allowed: %s.", known.Name)
		}
		used[known.Name] = true
	}
	names := make([]string, 0, len(used))
	for name := range used {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func checkColumns(v *visitor, schema *database.Schema, p Policy, tables []string) error {
	// Conservative and name-based: a column name that is sensitive in any used table is blocked.
	sensitive := map[string]bool{}
	var hidden []string
	for _, name := range tables {
		for _, c := range schema.Table(name).Columns {
			if p.IsSensitive(name, c.Name) {
				sensitive[strings.ToLower(c.Name)] = true
				hidden = append(hidden, name+"."+c.Name)
			}
		}
	}
	for _, col := range v.columns {
		if sensitive[col] {
			return reject("Column not allowed (sensitive): %s.", col)
		}
	}
	if v.hasStar && len(hidden) > 0 {
		sort.Strings(hidden)
		return reject("SELECT * is not allowed here because of sensitive columns (%s). List the columns you need.",
			strings.Join(hidden, ", "))
	}
	return nil
}

type valueGetter interface{ GetValue() any }

// applyLimit keeps a smaller LIMIT, otherwise caps it. Any OFFSET is kept.
func applyLimit(root ast.StmtNode, max int) int {
	var limit **ast.Limit
	switch s := root.(type) {
	case *ast.SelectStmt:
		limit = &s.Limit
	case *ast.SetOprStmt:
		limit = &s.Limit
	}
	if *limit == nil {
		*limit = &ast.Limit{}
	}
	if count, ok := (*limit).Count.(valueGetter); ok {
		switch n := count.GetValue().(type) {
		case int64:
			if n >= 0 && n <= int64(max) {
				return int(n)
			}
		case uint64:
			if n <= uint64(max) {
				return int(n)
			}
		}
	}
	(*limit).Count = ast.NewValueExpr(int64(max), "", "")
	return max
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return strings.TrimSpace(s)
}
