package service

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/frahjokhio/db-agent/internal/database"
	"github.com/frahjokhio/db-agent/internal/sqlsafe"
)

// ---------- shapes returned to the AI ----------

type TableSummary struct {
	Table       string   `json:"table"`
	Type        string   `json:"type,omitempty"`
	Description string   `json:"description,omitempty"`
	Links       []string `json:"links,omitempty"`
}

type ColumnInfo struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
	Comment  string `json:"comment,omitempty"`
}

type TableDetail struct {
	TableSummary
	Columns       []ColumnInfo `json:"columns"`
	PrimaryKey    []string     `json:"primary_key,omitempty"`
	Indexes       []string     `json:"indexes,omitempty"`
	ApproxRows    *int64       `json:"approx_rows,omitempty"`
	HiddenColumns string       `json:"hidden_columns,omitempty"`
}

type TableList struct {
	Database   string         `json:"database"`
	TableCount int            `json:"table_count"`
	Tables     []TableSummary `json:"tables"`
}

type RelevantTables struct {
	Matches      []TableDetail `json:"matches"`
	LinkedTables []string      `json:"linked_tables"`
	Note         string        `json:"note"`
}

type QueryResponse struct {
	Columns   []string `json:"columns"`
	Rows      [][]any  `json:"rows"`
	RowCount  int      `json:"row_count"`
	Truncated string   `json:"truncated,omitempty"`
}

type HistoryItem struct {
	WhenUTC string `json:"when_utc"`
	Status  string `json:"status"`
	SQL     string `json:"sql"`
	Rows    *int64 `json:"rows,omitempty"`
	Error   string `json:"error,omitempty"`
}

type BusinessRules struct {
	Timezone         string            `json:"business_timezone,omitempty"`
	DatabaseNow      string            `json:"database_now"`
	DatabaseTimeZone string            `json:"database_time_zone"`
	Definitions      map[string]string `json:"definitions,omitempty"`
	Notes            []string          `json:"notes,omitempty"`
	Message          string            `json:"message,omitempty"`
}

// ---------- operations ----------

func visibleTables(st State) []*database.Table {
	var out []*database.Table
	for _, t := range st.Schema.Tables {
		if !st.Policy.IsBlocked(t.Name) {
			out = append(out, t)
		}
	}
	return out
}

func summary(t *database.Table, p sqlsafe.Policy) TableSummary {
	s := TableSummary{Table: t.Name, Description: t.Comment}
	if t.Kind == "view" {
		s.Type = "view"
	}
	for _, fk := range t.ForeignKeys {
		if p.IsBlocked(fk.RefTable) {
			continue // don't reveal hidden tables through links
		}
		link := fmt.Sprintf("%s -> %s.%s", strings.Join(fk.Columns, ","), fk.RefTable, strings.Join(fk.RefColumns, ","))
		if fk.Inferred {
			link += " (guessed)"
		}
		s.Links = append(s.Links, link)
	}
	return s
}

func detail(t *database.Table, p sqlsafe.Policy) *TableDetail {
	d := &TableDetail{TableSummary: summary(t, p), Columns: []ColumnInfo{}, PrimaryKey: t.PrimaryKey, ApproxRows: t.RowEstimate}
	hidden := 0
	for _, c := range t.Columns {
		if p.IsSensitive(t.Name, c.Name) {
			hidden++
			continue
		}
		d.Columns = append(d.Columns, ColumnInfo{Name: c.Name, Type: c.Type, Nullable: c.Nullable, Comment: c.Comment})
	}
	for _, idx := range t.Indexes {
		prefix := ""
		if idx.Unique {
			prefix = "unique: "
		}
		d.Indexes = append(d.Indexes, prefix+strings.Join(idx.Columns, ", "))
	}
	if hidden > 0 {
		d.HiddenColumns = fmt.Sprintf("%d sensitive column(s) hidden", hidden)
	}
	return d
}

func (a *Agent) ListTables(ctx context.Context) (TableList, error) {
	st, err := a.Current(ctx)
	if err != nil {
		return TableList{}, err
	}
	tables := visibleTables(st)
	out := TableList{Database: st.Schema.Database, TableCount: len(tables), Tables: []TableSummary{}}
	for _, t := range tables {
		out.Tables = append(out.Tables, summary(t, st.Policy))
	}
	return out, nil
}

// DescribeTable returns nil if the table does not exist or is hidden.
func (a *Agent) DescribeTable(ctx context.Context, name string) (*TableDetail, error) {
	st, err := a.Current(ctx)
	if err != nil {
		return nil, err
	}
	t := st.Schema.Table(name)
	if t == nil || st.Policy.IsBlocked(t.Name) {
		return nil, nil
	}
	return detail(t, st.Policy), nil
}

var stopWords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`the a an of in on for to by with and or me show find list get all who what
		which how many much is are was were have has did do does this that last top most per each from
		than more less never ever number`) {
		stopWords[w] = true
	}
}

var wordRe = regexp.MustCompile(`[a-z0-9]+`)

// stem: tiny plural -> singular, enough for table/column matching.
func stem(w string) string {
	switch {
	case strings.HasSuffix(w, "ies") && len(w) > 4:
		return w[:len(w)-3] + "y"
	case strings.HasSuffix(w, "ses") && len(w) > 4:
		return w[:len(w)-2]
	case strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") && len(w) > 3:
		return w[:len(w)-1]
	}
	return w
}

func words(text string) map[string]bool {
	out := map[string]bool{}
	for _, w := range wordRe.FindAllString(strings.ToLower(text), -1) {
		if !stopWords[w] {
			out[stem(w)] = true
		}
	}
	return out
}

func overlap(a, b map[string]bool) int {
	n := 0
	for w := range a {
		if b[w] {
			n++
		}
	}
	return n
}

// FindRelevantTables: keyword match on names/descriptions, plus directly linked tables.
func (a *Agent) FindRelevantTables(ctx context.Context, question string) (RelevantTables, error) {
	st, err := a.Current(ctx)
	if err != nil {
		return RelevantTables{}, err
	}
	q := words(question)
	tables := visibleTables(st)
	scores := map[string]int{}
	byName := map[string]*database.Table{}
	for _, t := range tables {
		byName[t.Name] = t
		score := 0
		if q[stem(strings.ToLower(t.Name))] { // "orders" table <- "order(s)"
			score += 8
		} else { // "order_items" <- "order" (partial)
			score += 3 * overlap(q, words(strings.ReplaceAll(t.Name, "_", " ")))
		}
		score += 2 * overlap(q, words(t.Comment))
		for _, c := range t.Columns {
			if !st.Policy.IsSensitive(t.Name, c.Name) {
				score += overlap(q, words(strings.ReplaceAll(c.Name, "_", " ")+" "+c.Comment))
			}
		}
		if score > 0 {
			scores[t.Name] = score
		}
	}

	top := make([]string, 0, len(scores))
	for name := range scores {
		top = append(top, name)
	}
	sort.Slice(top, func(i, j int) bool {
		if scores[top[i]] != scores[top[j]] {
			return scores[top[i]] > scores[top[j]]
		}
		return top[i] < top[j]
	})
	if len(top) > 5 {
		top = top[:5]
	}

	inTop := map[string]bool{}
	for _, n := range top {
		inTop[n] = true
	}
	linked := map[string]bool{}
	for _, name := range top {
		for _, fk := range byName[name].ForeignKeys {
			if byName[fk.RefTable] != nil && !inTop[fk.RefTable] {
				linked[fk.RefTable] = true
			}
		}
		for _, other := range tables { // tables pointing at this one (order_items -> orders)
			if inTop[other.Name] {
				continue
			}
			for _, fk := range other.ForeignKeys {
				if fk.RefTable == name {
					linked[other.Name] = true
				}
			}
		}
	}

	out := RelevantTables{Matches: []TableDetail{}, LinkedTables: []string{},
		Note: "Keyword match only. Use list_tables if something is missing."}
	for _, n := range top {
		out.Matches = append(out.Matches, *detail(byName[n], st.Policy))
	}
	for n := range linked {
		out.LinkedTables = append(out.LinkedTables, n)
	}
	sort.Strings(out.LinkedTables)
	return out, nil
}

func (a *Agent) RunQuery(ctx context.Context, sql string, who sqlsafe.Caller) (QueryResponse, error) {
	st, err := a.Current(ctx)
	if err != nil {
		return QueryResponse{}, err
	}
	res, err := a.Runner.Run(ctx, st.Schema, st.Policy, sql, who)
	if err != nil {
		return QueryResponse{}, err
	}
	out := QueryResponse{Columns: res.Columns, Rows: res.Rows, RowCount: res.RowCount}
	if res.Truncated {
		out.Truncated = fmt.Sprintf("More than %d rows exist; only the first %d are shown. "+
			"Use aggregates (COUNT, SUM, GROUP BY) or tighter filters.", st.Policy.MaxRows, st.Policy.MaxRows)
	}
	return out, nil
}

// GetBusinessRules returns company definitions plus the database clock (for date questions).
func (a *Agent) GetBusinessRules(ctx context.Context) (BusinessRules, error) {
	st, err := a.Current(ctx)
	if err != nil {
		return BusinessRules{}, err
	}
	out := BusinessRules{
		Timezone:    st.Rules.Timezone,
		Definitions: st.Rules.Definitions,
		Notes:       st.Rules.Notes,
	}
	var sessionTZ, systemTZ string
	err = a.DB.QueryRowContext(ctx,
		"SELECT DATE_FORMAT(NOW(), '%Y-%m-%d %H:%i:%s'), @@session.time_zone, @@system_time_zone",
	).Scan(&out.DatabaseNow, &sessionTZ, &systemTZ)
	if err != nil {
		return BusinessRules{}, database.SafeError(err)
	}
	out.DatabaseTimeZone = sessionTZ
	if sessionTZ == "SYSTEM" {
		out.DatabaseTimeZone = "SYSTEM (" + systemTZ + ")"
	}

	switch {
	case st.RulesErr != nil:
		out.Message = "rules.yaml currently has an error; using the last valid rules."
	case len(out.Definitions) == 0 && len(out.Notes) == 0:
		out.Message = "No business definitions yet. When a business term is unclear, ask the user, " +
			"and suggest adding the answer to .db-agent/rules.yaml."
	}
	return out, nil
}

func (a *Agent) QueryHistory(user string, limit int) ([]HistoryItem, error) {
	entries, err := a.Audit.Recent(limit, user)
	if err != nil {
		return nil, err
	}
	out := []HistoryItem{}
	for _, e := range entries {
		out = append(out, HistoryItem{
			WhenUTC: e.CreatedAt.Format("2006-01-02 15:04:05"),
			Status:  string(e.Status), SQL: e.SQL, Rows: e.RowCount, Error: e.Error,
		})
	}
	return out, nil
}
