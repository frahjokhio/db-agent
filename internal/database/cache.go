package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"time"
)

// SchemaStore saves schema snapshots in the agent's SQLite file.
type SchemaStore struct{ db *sql.DB }

func NewSchemaStore(agentDB *sql.DB) *SchemaStore { return &SchemaStore{db: agentDB} }

// Latest returns the newest snapshot, or nil if there is none.
func (s *SchemaStore) Latest(database string) (*Schema, error) {
	var raw string
	err := s.db.QueryRow(
		"SELECT schema_json FROM schema_snapshots WHERE database = ? ORDER BY id DESC LIMIT 1", database,
	).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var schema Schema
	if err := json.Unmarshal([]byte(raw), &schema); err != nil {
		return nil, fmt.Errorf("saved schema is unreadable: %w", err)
	}
	schema.normalize()
	return &schema, nil
}

func (s *SchemaStore) Save(schema *Schema) error {
	payload, err := json.Marshal(schema)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		"INSERT INTO schema_snapshots (database, fingerprint, captured_at, schema_json) VALUES (?, ?, ?, ?)",
		schema.Database, schema.Fingerprint(), schema.CapturedAt.Format(time.RFC3339Nano), string(payload),
	)
	return err
}

// Changes between two snapshots.
type Changes struct {
	AddedTables   []string
	RemovedTables []string
	ChangedTables map[string][]string // table -> notes like "+ column phone"
}

func (c Changes) IsEmpty() bool {
	return len(c.AddedTables) == 0 && len(c.RemovedTables) == 0 && len(c.ChangedTables) == 0
}

// Diff compares an old and a new snapshot.
func Diff(old, new *Schema) Changes {
	oldT, newT := tableMap(old), tableMap(new)
	changes := Changes{ChangedTables: map[string][]string{}}
	for name := range newT {
		if oldT[name] == nil {
			changes.AddedTables = append(changes.AddedTables, name)
		}
	}
	for name, o := range oldT {
		n := newT[name]
		if n == nil {
			changes.RemovedTables = append(changes.RemovedTables, name)
			continue
		}
		if notes := tableChanges(o, n); len(notes) > 0 {
			changes.ChangedTables[name] = notes
		}
	}
	sort.Strings(changes.AddedTables)
	sort.Strings(changes.RemovedTables)
	return changes
}

func tableMap(s *Schema) map[string]*Table {
	m := map[string]*Table{}
	for _, t := range s.Tables {
		m[t.Name] = t
	}
	return m
}

func tableChanges(old, new *Table) []string {
	var notes []string
	oldCols := map[string]Column{}
	for _, c := range old.Columns {
		oldCols[c.Name] = c
	}
	newCols := map[string]bool{}
	for _, c := range new.Columns {
		newCols[c.Name] = true
		if prev, ok := oldCols[c.Name]; !ok {
			notes = append(notes, "+ column "+c.Name)
		} else if !reflect.DeepEqual(prev, c) {
			notes = append(notes, "~ column "+c.Name)
		}
	}
	for _, c := range old.Columns {
		if !newCols[c.Name] {
			notes = append(notes, "- column "+c.Name)
		}
	}
	if !reflect.DeepEqual(old.PrimaryKey, new.PrimaryKey) {
		notes = append(notes, "~ primary key")
	}
	if !reflect.DeepEqual(old.Indexes, new.Indexes) {
		notes = append(notes, "~ indexes")
	}
	if !reflect.DeepEqual(old.ForeignKeys, new.ForeignKeys) {
		notes = append(notes, "~ relationships")
	}
	if old.Comment != new.Comment {
		notes = append(notes, "~ comment")
	}
	return notes
}

// SyncResult tells what happened during a sync.
type SyncResult struct {
	Schema        *Schema
	FirstSnapshot bool
	Changes       Changes
}

// Sync reads the live schema, compares it with the saved one, and saves if it changed.
func Sync(ctx context.Context, db *sql.DB, store *SchemaStore) (SyncResult, error) {
	current, err := Introspect(ctx, db)
	if err != nil {
		return SyncResult{}, err
	}
	previous, err := store.Latest(current.Database)
	if err != nil {
		return SyncResult{}, err
	}
	if previous == nil {
		return SyncResult{Schema: current, FirstSnapshot: true}, store.Save(current)
	}
	if previous.Fingerprint() == current.Fingerprint() {
		return SyncResult{Schema: current}, nil
	}
	return SyncResult{Schema: current, Changes: Diff(previous, current)}, store.Save(current)
}
