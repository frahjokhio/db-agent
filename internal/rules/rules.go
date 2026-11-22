// Package rules loads the project's business context from .db-agent/rules.yaml.
//
// Schema metadata can't explain business meaning ("revenue = paid orders only").
// This file lets people say it once; the AI reads it before answering, and the
// safety layer enforces the hidden tables/columns. It holds no secrets, so it can
// be committed to git (reviewable + versioned).
package rules

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

const FileName = "rules.yaml"

type Rules struct {
	Timezone      string                `yaml:"timezone"`
	Definitions   map[string]string     `yaml:"definitions"`
	Notes         []string              `yaml:"notes"`
	Relationships []string              `yaml:"relationships"` // "a.col -> b.col": confirmed or added links
	NotRelated    []string              `yaml:"not_related"`   // guessed links that are wrong
	Hidden        Hidden                `yaml:"hidden"`
	ShowColumns   []string              `yaml:"show_columns"` // "table.column" safe despite default name rules
	Tables        map[string]TableNotes `yaml:"tables"`
}

type Hidden struct {
	Tables  []string `yaml:"tables"`
	Columns []string `yaml:"columns"` // "table.column"
}

type TableNotes struct {
	Description string            `yaml:"description"`
	Columns     map[string]string `yaml:"columns"`
}

// Link is a parsed "table.column -> table.column".
type Link struct {
	Table, Column, RefTable, RefColumn string
}

func (l Link) String() string {
	return fmt.Sprintf("%s.%s -> %s.%s", l.Table, l.Column, l.RefTable, l.RefColumn)
}

var linkRe = regexp.MustCompile(`^\s*([\w$]+)\.([\w$]+)\s*->\s*([\w$]+)\.([\w$]+)\s*$`)

func ParseLink(s string) (Link, error) {
	m := linkRe.FindStringSubmatch(s)
	if m == nil {
		return Link{}, fmt.Errorf("bad link %q: use \"table.column -> table.column\"", s)
	}
	return Link{m[1], m[2], m[3], m[4]}, nil
}

// SplitColumn parses "table.column".
func SplitColumn(s string) (table, column string, err error) {
	t, c, ok := strings.Cut(strings.TrimSpace(s), ".")
	if !ok || t == "" || c == "" || strings.Contains(c, ".") {
		return "", "", fmt.Errorf("bad column %q: use \"table.column\"", s)
	}
	return t, c, nil
}

// IsEmpty reports whether no rules are defined.
func (r *Rules) IsEmpty() bool {
	return r == nil || (r.Timezone == "" && len(r.Definitions) == 0 && len(r.Notes) == 0 &&
		len(r.Relationships) == 0 && len(r.NotRelated) == 0 && len(r.Hidden.Tables) == 0 &&
		len(r.Hidden.Columns) == 0 && len(r.ShowColumns) == 0 && len(r.Tables) == 0)
}

// Load reads the rules file. A missing file means "no rules" (not an error).
func Load(path string) (*Rules, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Rules{}, nil
	}
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Parse decodes YAML strictly (unknown keys are errors, to catch typos) and checks syntax.
func Parse(data []byte) (*Rules, error) {
	r := &Rules{}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(r); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("rules.yaml: %w", err)
	}
	for _, s := range append(append([]string{}, r.Relationships...), r.NotRelated...) {
		if _, err := ParseLink(s); err != nil {
			return nil, fmt.Errorf("rules.yaml: %w", err)
		}
	}
	for _, s := range append(append([]string{}, r.Hidden.Columns...), r.ShowColumns...) {
		if _, _, err := SplitColumn(s); err != nil {
			return nil, fmt.Errorf("rules.yaml: %w", err)
		}
	}
	return r, nil
}
