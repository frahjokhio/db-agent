// Package service is the core engine used by every interface (MCP now, dashboard later).
//
// It holds the DB pool, an in-memory schema that refreshes itself, and the business
// rules (reloaded when rules.yaml changes). The same visibility rules apply everywhere:
// hidden tables and sensitive columns are never shown, and every query goes through
// the safe runner with the current policy.
package service

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/frahjokhio/db-agent/internal/audit"
	"github.com/frahjokhio/db-agent/internal/config"
	"github.com/frahjokhio/db-agent/internal/database"
	"github.com/frahjokhio/db-agent/internal/rules"
	"github.com/frahjokhio/db-agent/internal/sqlsafe"
	"github.com/frahjokhio/db-agent/internal/storage"
)

type Agent struct {
	Settings config.Settings
	DB       *sql.DB
	AgentDB  *sql.DB
	Store    *database.SchemaStore
	Audit    *audit.Log
	Runner   *sqlsafe.Runner

	mu         sync.Mutex
	raw        *database.Schema // live schema as read from MySQL
	loadedAt   time.Time
	maxAge     time.Duration
	rules      *rules.Rules // last valid rules
	rulesErr   error        // problem with the current rules.yaml, if any
	rulesMod   time.Time
	rulesFixed bool // set by tests: don't read the file
	view       *database.Schema
	policy     sqlsafe.Policy
	dirty      bool
}

// New opens the app DB pool (lazily) and the agent's SQLite file.
func New(s config.Settings) (*Agent, error) {
	db, err := database.Open(s)
	if err != nil {
		return nil, err
	}
	agentDB, err := storage.Open(s.AgentDBPath())
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("cannot open agent storage: %w", err)
	}
	log := audit.New(agentDB)
	return &Agent{
		Settings: s,
		DB:       db,
		AgentDB:  agentDB,
		Store:    database.NewSchemaStore(agentDB),
		Audit:    log,
		Runner:   &sqlsafe.Runner{DB: db, Audit: log, Timeout: s.QueryTimeout},
		maxAge:   time.Minute,
	}, nil
}

func (a *Agent) Close() {
	a.DB.Close()
	a.AgentDB.Close()
}

// RulesPath is the project's business rules file.
func (a *Agent) RulesPath() string { return filepath.Join(a.Settings.StorageDir(), rules.FileName) }

// SetSchema injects a freshly read schema (CLI after sync, tests without a DB).
func (a *Agent) SetSchema(s *database.Schema) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.raw, a.loadedAt, a.dirty = s, time.Now(), true
}

// SetRules injects rules and stops reading rules.yaml (tests).
func (a *Agent) SetRules(r *rules.Rules) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rules, a.rulesErr, a.rulesFixed, a.dirty = r, nil, true, true
}

// State is a consistent snapshot: the AI's view of the schema, the policy, and the rules.
type State struct {
	Schema   *database.Schema
	Policy   sqlsafe.Policy
	Rules    *rules.Rules
	RulesErr error
}

// Current returns the snapshot, re-reading the schema at most once a minute and
// rules.yaml whenever it changes.
func (a *Agent) Current(ctx context.Context) (State, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.raw == nil || time.Since(a.loadedAt) >= a.maxAge {
		res, err := database.Sync(ctx, a.DB, a.Store)
		if err != nil {
			return State{}, err
		}
		a.raw, a.loadedAt, a.dirty = res.Schema, time.Now(), true
	}
	a.reloadRulesLocked()
	if a.rules == nil {
		// Broken rules.yaml and no earlier valid version: fail closed, because hidden
		// tables/columns from the file can't be enforced.
		return State{}, fmt.Errorf("rules.yaml has an error, fix it first (run 'db-agent rules'): %v", a.rulesErr)
	}
	if a.dirty {
		a.view = rules.Apply(a.raw, a.rules)
		a.policy = buildPolicy(a.Settings.MaxRows, a.rules)
		a.dirty = false
	}
	return State{Schema: a.view, Policy: a.policy, Rules: a.rules, RulesErr: a.rulesErr}, nil
}

func (a *Agent) reloadRulesLocked() {
	if a.rulesFixed {
		return
	}
	var mod time.Time // zero = file missing
	if info, err := os.Stat(a.RulesPath()); err == nil {
		mod = info.ModTime()
	}
	if (a.rules != nil || a.rulesErr != nil) && mod.Equal(a.rulesMod) {
		return
	}
	a.rulesMod = mod
	r, err := rules.Load(a.RulesPath())
	if err != nil {
		a.rulesErr = err // keep the last valid rules, if any
		fmt.Fprintln(os.Stderr, "db-agent:", err)
		return
	}
	a.rules, a.rulesErr, a.dirty = r, nil, true
}

func buildPolicy(maxRows int, r *rules.Rules) sqlsafe.Policy {
	p := sqlsafe.DefaultPolicy(maxRows)
	for _, t := range r.Hidden.Tables {
		p.BlockedTables[strings.ToLower(t)] = true
	}
	for _, c := range r.Hidden.Columns {
		p.ExtraSensitive[strings.ToLower(strings.TrimSpace(c))] = true
	}
	for _, c := range r.ShowColumns {
		p.AllowColumns[strings.ToLower(strings.TrimSpace(c))] = true
	}
	return p
}
