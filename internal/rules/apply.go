package rules

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/frahjokhio/db-agent/internal/database"
)

// Apply returns a copy of the schema with the rules applied:
//   - confirmed links lose their "guessed" mark; new links are added
//   - not_related links are removed (only guessed ones; real foreign keys stay)
//   - table/column descriptions from the rules replace empty or DB comments
//
// The saved snapshot is never changed; this is a view for the AI.
func Apply(s *database.Schema, r *Rules) *database.Schema {
	view := clone(s)
	if r.IsEmpty() {
		return view
	}

	for _, raw := range r.Relationships {
		l, _ := ParseLink(raw) // syntax already checked in Parse
		t := view.Table(l.Table)
		ref := view.Table(l.RefTable)
		if t == nil || ref == nil || t.Column(l.Column) == nil || ref.Column(l.RefColumn) == nil {
			continue // reported by Validate
		}
		if fk := findLink(t, l); fk != nil {
			fk.Inferred = false
			fk.RefTable, fk.RefColumns = ref.Name, []string{ref.Column(l.RefColumn).Name}
			continue
		}
		t.ForeignKeys = append(t.ForeignKeys, database.ForeignKey{
			Name: "rules", Columns: []string{t.Column(l.Column).Name},
			RefTable: ref.Name, RefColumns: []string{ref.Column(l.RefColumn).Name},
		})
	}

	for _, raw := range r.NotRelated {
		l, _ := ParseLink(raw)
		t := view.Table(l.Table)
		if t == nil {
			continue
		}
		kept := t.ForeignKeys[:0]
		for _, fk := range t.ForeignKeys {
			if !(fk.Inferred && matches(fk, l)) {
				kept = append(kept, fk)
			}
		}
		t.ForeignKeys = kept
	}

	for name, notes := range r.Tables {
		t := view.Table(name)
		if t == nil {
			continue
		}
		if notes.Description != "" {
			t.Comment = notes.Description
		}
		for col, note := range notes.Columns {
			if c := t.Column(col); c != nil && note != "" {
				c.Comment = note
			}
		}
	}
	return view
}

// findLink finds an existing single-column link from the same column (any target).
func findLink(t *database.Table, l Link) *database.ForeignKey {
	for i := range t.ForeignKeys {
		fk := &t.ForeignKeys[i]
		if len(fk.Columns) == 1 && strings.EqualFold(fk.Columns[0], l.Column) {
			return fk
		}
	}
	return nil
}

func matches(fk database.ForeignKey, l Link) bool {
	return len(fk.Columns) == 1 && strings.EqualFold(fk.Columns[0], l.Column) &&
		strings.EqualFold(fk.RefTable, l.RefTable) &&
		len(fk.RefColumns) == 1 && strings.EqualFold(fk.RefColumns[0], l.RefColumn)
}

func clone(s *database.Schema) *database.Schema {
	data, _ := json.Marshal(s)
	var c database.Schema
	_ = json.Unmarshal(data, &c)
	return &c
}

// Validate lists rules that point at tables/columns that don't exist (typos, renamed columns).
func Validate(s *database.Schema, r *Rules) []string {
	var warnings []string
	warn := func(format string, args ...any) { warnings = append(warnings, fmt.Sprintf(format, args...)) }

	checkColumn := func(where, table, column string) {
		t := s.Table(table)
		if t == nil {
			warn("%s: unknown table %q", where, table)
		} else if t.Column(column) == nil {
			warn("%s: unknown column %q in table %q", where, column, table)
		}
	}

	for _, raw := range r.Relationships {
		l, _ := ParseLink(raw)
		checkColumn("relationships", l.Table, l.Column)
		checkColumn("relationships", l.RefTable, l.RefColumn)
	}
	for _, raw := range r.NotRelated {
		l, _ := ParseLink(raw)
		checkColumn("not_related", l.Table, l.Column)
		if t := s.Table(l.Table); t != nil {
			for _, fk := range t.ForeignKeys {
				if !fk.Inferred && matches(fk, l) {
					warn("not_related: %s is a real foreign key in the database and stays", l)
				}
			}
		}
	}
	for _, name := range r.Hidden.Tables {
		if s.Table(name) == nil {
			warn("hidden.tables: unknown table %q", name)
		}
	}
	for _, raw := range r.Hidden.Columns {
		t, c, _ := SplitColumn(raw)
		checkColumn("hidden.columns", t, c)
	}
	for _, raw := range r.ShowColumns {
		t, c, _ := SplitColumn(raw)
		checkColumn("show_columns", t, c)
	}
	for name, notes := range r.Tables {
		t := s.Table(name)
		if t == nil {
			warn("tables: unknown table %q", name)
			continue
		}
		for col := range notes.Columns {
			if t.Column(col) == nil {
				warn("tables.%s.columns: unknown column %q", name, col)
			}
		}
	}
	return warnings
}
