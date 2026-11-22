package database

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
)

// Column of a table. JSON names match the earlier Python version, so old snapshots still load.
type Column struct {
	Name     string  `json:"name"`
	Type     string  `json:"type"` // full MySQL type, e.g. "decimal(10,2)"
	Nullable bool    `json:"nullable"`
	Default  *string `json:"default"`
	Comment  string  `json:"comment"`
}

type Index struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Unique  bool     `json:"unique"`
}

type ForeignKey struct {
	Columns    []string `json:"columns"`
	RefTable   string   `json:"ref_table"`
	RefColumns []string `json:"ref_columns"`
	Name       string   `json:"name"`
	Inferred   bool     `json:"inferred"` // guessed from naming, not a real DB constraint
}

type Table struct {
	Name        string       `json:"name"`
	Kind        string       `json:"kind"` // "table" or "view"
	Comment     string       `json:"comment"`
	RowEstimate *int64       `json:"row_estimate"` // MySQL's approximate count; not exact
	Columns     []Column     `json:"columns"`
	PrimaryKey  []string     `json:"primary_key"`
	Indexes     []Index      `json:"indexes"`
	ForeignKeys []ForeignKey `json:"foreign_keys"`
}

// Column finds a column by name (MySQL column names are case-insensitive).
func (t *Table) Column(name string) *Column {
	for i := range t.Columns {
		if strings.EqualFold(t.Columns[i].Name, name) {
			return &t.Columns[i]
		}
	}
	return nil
}

type Schema struct {
	Database   string    `json:"database"`
	Tables     []*Table  `json:"tables"`
	CapturedAt time.Time `json:"captured_at"`
}

// Table finds a table by name, case-insensitively.
func (s *Schema) Table(name string) *Table {
	for _, t := range s.Tables {
		if strings.EqualFold(t.Name, name) {
			return t
		}
	}
	return nil
}

// Fingerprint is a hash of the structure only (ignores timestamps and row counts).
func (s *Schema) Fingerprint() string {
	tables := make([]Table, len(s.Tables))
	for i, t := range s.Tables {
		c := *t
		c.RowEstimate = nil
		tables[i] = c
	}
	payload, _ := json.Marshal(struct {
		Database string
		Tables   []Table
	}{s.Database, tables})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// normalize turns nil slices into empty ones so comparisons and hashes are stable.
func (s *Schema) normalize() {
	for _, t := range s.Tables {
		if t.Columns == nil {
			t.Columns = []Column{}
		}
		if t.PrimaryKey == nil {
			t.PrimaryKey = []string{}
		}
		if t.Indexes == nil {
			t.Indexes = []Index{}
		}
		if t.ForeignKeys == nil {
			t.ForeignKeys = []ForeignKey{}
		}
		if t.Kind == "" {
			t.Kind = "table"
		}
	}
}
