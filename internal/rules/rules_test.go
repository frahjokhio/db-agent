package rules

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/frahjokhio/db-agent/internal/database"
)

func TestMissingFileMeansNoRules(t *testing.T) {
	r, err := Load(filepath.Join(t.TempDir(), "rules.yaml"))
	if err != nil || !r.IsEmpty() {
		t.Fatalf("got %+v %v", r, err)
	}
}

func TestParseFullFile(t *testing.T) {
	r, err := Parse([]byte(`
timezone: Asia/Karachi
definitions:
  revenue: Sum of orders.total where status = 'paid'.
notes:
  - Amounts are in PKR.
relationships:
  - order_items.order_id -> orders.id
not_related: []
hidden:
  tables: [audit_logs]
  columns: [customers.email]
show_columns: [users.token_count]
tables:
  orders:
    description: One row per checkout.
    columns:
      status: "'paid' = money received"
`))
	if err != nil {
		t.Fatal(err)
	}
	if r.Timezone != "Asia/Karachi" || r.Definitions["revenue"] == "" || len(r.Notes) != 1 ||
		r.Hidden.Tables[0] != "audit_logs" || r.Tables["orders"].Columns["status"] == "" {
		t.Fatalf("bad parse: %+v", r)
	}
}

func TestCommentedTemplateSectionsAreEmpty(t *testing.T) {
	r, err := Parse([]byte("definitions:\n  # revenue: x\nrelationships:\n  # - a.b -> c.d\nhidden:\n  tables: []\n"))
	if err != nil || !r.IsEmpty() {
		t.Fatalf("got %+v %v", r, err)
	}
}

func TestTyposAndBadSyntaxRejected(t *testing.T) {
	for _, bad := range []string{
		"hidden:\n  tabels: [x]\n",                 // unknown key
		"relationships:\n  - orders.customer_id\n", // not a link
		"hidden:\n  columns: [email]\n",            // missing table
	} {
		if _, err := Parse([]byte(bad)); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
}

func shop() *database.Schema {
	col := func(n string) database.Column { return database.Column{Name: n, Type: "int"} }
	return &database.Schema{Database: "shop", Tables: []*database.Table{
		{Name: "orders", Kind: "table", Comment: "db comment", Columns: []database.Column{col("id"), col("customer_id"), col("coupon_code")}, PrimaryKey: []string{"id"},
			ForeignKeys: []database.ForeignKey{{Columns: []string{"customer_id"}, RefTable: "customers", RefColumns: []string{"id"}}}},
		{Name: "customers", Kind: "table", Columns: []database.Column{col("id")}, PrimaryKey: []string{"id"}},
		{Name: "coupons", Kind: "table", Columns: []database.Column{col("code")}},
		{Name: "order_items", Kind: "table", Columns: []database.Column{col("id"), col("order_id")}, PrimaryKey: []string{"id"},
			ForeignKeys: []database.ForeignKey{{Columns: []string{"order_id"}, RefTable: "orders", RefColumns: []string{"id"}, Inferred: true}}},
	}}
}

func TestApplyDoesNotChangeOriginal(t *testing.T) {
	s := shop()
	r := &Rules{Relationships: []string{"order_items.order_id -> orders.id"},
		Tables: map[string]TableNotes{"orders": {Description: "rules comment"}}}
	view := Apply(s, r)
	if view.Table("orders").Comment != "rules comment" || view.Table("order_items").ForeignKeys[0].Inferred {
		t.Fatal("rules not applied")
	}
	if s.Table("orders").Comment != "db comment" || !s.Table("order_items").ForeignKeys[0].Inferred {
		t.Fatal("original schema was modified")
	}
}

func TestApplyAddsNewLinkAndKeepsRealFK(t *testing.T) {
	r := &Rules{
		Relationships: []string{"orders.coupon_code -> coupons.code"},
		NotRelated:    []string{"orders.customer_id -> customers.id"}, // real FK: must stay
	}
	view := Apply(shop(), r)
	fks := view.Table("orders").ForeignKeys
	if len(fks) != 2 || fks[1].RefTable != "coupons" || fks[1].Inferred {
		t.Fatalf("fks: %+v", fks)
	}
	w := Validate(shop(), r)
	if len(w) != 1 || !strings.Contains(w[0], "real foreign key") {
		t.Fatalf("warnings: %v", w)
	}
}

func TestValidateFindsTypos(t *testing.T) {
	r := &Rules{
		Relationships: []string{"order_items.ordr_id -> orders.id"},
		Hidden:        Hidden{Tables: []string{"payments"}, Columns: []string{"customers.emial"}},
		Tables:        map[string]TableNotes{"orders": {Columns: map[string]string{"stat": "x"}}},
	}
	w := Validate(shop(), r)
	if len(w) != 4 {
		t.Fatalf("expected 4 warnings, got %v", w)
	}
}

func TestTemplateListsGuessedLinksAndParses(t *testing.T) {
	tpl := Template("Shop", shop(), func(_, c string) bool { return c == "coupon_code" })
	if !strings.Contains(tpl, "# - order_items.order_id -> orders.id") || !strings.Contains(tpl, "orders.coupon_code") {
		t.Fatal(tpl)
	}
	if r, err := Parse([]byte(tpl)); err != nil || !r.IsEmpty() {
		t.Fatalf("template should parse to empty rules: %v", err)
	}
}
