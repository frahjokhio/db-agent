package rules

import (
	"fmt"
	"strings"

	"github.com/frahjokhio/db-agent/internal/database"
)

// Template builds a starter rules.yaml for this schema: guessed links and
// default-hidden columns are listed as comments so people can confirm them.
func Template(project string, s *database.Schema, isSensitive func(table, column string) bool) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }

	w("# Business rules for %s.", project)
	w("# Safe to commit (no passwords). The AI reads this before answering.")
	w("# Edit freely: changes apply within a minute. Check with: db-agent rules")
	w("")
	w("timezone: \"\"            # e.g. Asia/Karachi")
	w("")
	w("definitions:             # what business words mean in THIS app (only the ones you need)")
	w("  # <term>: <what it means, using real table/column names>")
	w("  # Example: active member: A row in members with status = 'active' and expires_at in the future.")
	w("")
	w("notes:                   # anything else the AI should know")
	w("  # - <e.g. currency, units, which records are test data>")
	w("")

	w("relationships:           # confirm guessed links or add missing ones")
	guessed := 0
	for _, t := range s.Tables {
		for _, fk := range t.ForeignKeys {
			if fk.Inferred {
				w("  # - %s.%s -> %s.%s", t.Name, fk.Columns[0], fk.RefTable, fk.RefColumns[0])
				guessed++
			}
		}
	}
	if guessed == 0 {
		w("  # - <table>.<column> -> <other_table>.<column>")
	} else {
		w("  # (above: guessed from column names; uncomment the correct ones)")
	}
	w("")
	w("not_related: []          # guessed links that are wrong, same format")
	w("")

	w("hidden:")
	w("  tables: []             # never shown to the AI or queried")
	w("  columns: []            # format: table.column")
	var hiddenByDefault []string
	for _, t := range s.Tables {
		for _, c := range t.Columns {
			if isSensitive(t.Name, c.Name) {
				hiddenByDefault = append(hiddenByDefault, t.Name+"."+c.Name)
			}
		}
	}
	if len(hiddenByDefault) > 0 {
		w("  # Already hidden by name (password/token/secret...): %s", strings.Join(hiddenByDefault, ", "))
	}
	w("")
	w("show_columns: []         # columns hidden by name rules that are actually safe")
	w("")

	w("tables:                  # descriptions for tables/columns (override DB comments)")
	for _, t := range s.Tables {
		if t.Kind == "view" {
			continue
		}
		w("  # %s:", t.Name)
		w("  #   description: \"\"")
		for _, c := range t.Columns {
			if strings.HasPrefix(c.Type, "enum(") || strings.Contains(strings.ToLower(c.Name), "status") {
				w("  #   columns:")
				w("  #     %s: \"\"   # %s", c.Name, c.Type)
				break
			}
		}
	}
	return b.String()
}
