package service

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/frahjokhio/db-agent/internal/config"
	"github.com/frahjokhio/db-agent/internal/database"
	"github.com/frahjokhio/db-agent/internal/rules"
)

func cols(names ...string) []database.Column {
	out := make([]database.Column, len(names))
	for i, n := range names {
		out[i] = database.Column{Name: n, Type: "varchar(50)", Nullable: true}
	}
	return out
}

// newTestAgent uses an injected schema, so no MySQL is needed.
func newTestAgent(t *testing.T) *Agent {
	a, err := New(config.Settings{ProjectDir: t.TempDir(), DBName: "shop", DBUser: "u", DBPort: 3306, MaxRows: 100})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	a.SetSchema(&database.Schema{Database: "shop", Tables: []*database.Table{
		{Name: "users", Kind: "table", Columns: cols("id", "email", "password", "remember_token"), PrimaryKey: []string{"id"}},
		{Name: "subscriptions", Kind: "table", Columns: cols("id", "user_id", "status"), PrimaryKey: []string{"id"},
			ForeignKeys: []database.ForeignKey{{Columns: []string{"user_id"}, RefTable: "users", RefColumns: []string{"id"}, Inferred: true}}},
	}})
	return a
}

func TestSensitiveColumnsHiddenFromAI(t *testing.T) {
	a := newTestAgent(t)
	d, err := a.DescribeTable(context.Background(), "users")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range d.Columns {
		names = append(names, c.Name)
	}
	if !reflect.DeepEqual(names, []string{"id", "email"}) || d.HiddenColumns != "2 sensitive column(s) hidden" {
		t.Fatalf("got %v / %q", names, d.HiddenColumns)
	}
}

func TestGuessedLinksLabelled(t *testing.T) {
	a := newTestAgent(t)
	list, _ := a.ListTables(context.Background())
	if !reflect.DeepEqual(list.Tables[1].Links, []string{"user_id -> users.id (guessed)"}) {
		t.Fatalf("got %v", list.Tables[1].Links)
	}
}

func TestFindRelevantTablesIncludesLinked(t *testing.T) {
	a := newTestAgent(t)
	found, _ := a.FindRelevantTables(context.Background(), "active subscriptions")
	if found.Matches[0].Table != "subscriptions" || !reflect.DeepEqual(found.LinkedTables, []string{"users"}) {
		t.Fatalf("got %+v", found)
	}
}

func TestHiddenTablesFromRules(t *testing.T) {
	a := newTestAgent(t)
	a.SetRules(&rules.Rules{Hidden: rules.Hidden{Tables: []string{"users"}}})
	if d, _ := a.DescribeTable(context.Background(), "users"); d != nil {
		t.Fatal("hidden table should not be described")
	}
	list, _ := a.ListTables(context.Background())
	if list.TableCount != 1 {
		t.Fatalf("got %d tables", list.TableCount)
	}
	if len(list.Tables[0].Links) != 0 {
		t.Fatalf("link to hidden table leaked: %v", list.Tables[0].Links)
	}
}

func TestRulesHideAndShowColumnsAndConfirmLinks(t *testing.T) {
	a := newTestAgent(t)
	a.SetRules(&rules.Rules{
		Hidden:        rules.Hidden{Columns: []string{"users.email"}},
		ShowColumns:   []string{"users.remember_token"},
		Relationships: []string{"subscriptions.user_id -> users.id"},
		Tables: map[string]rules.TableNotes{
			"subscriptions": {Description: "One row per plan purchase.", Columns: map[string]string{"status": "'active' = paying now"}},
		},
	})
	ctx := context.Background()
	users, _ := a.DescribeTable(ctx, "users")
	var names []string
	for _, c := range users.Columns {
		names = append(names, c.Name)
	}
	if !reflect.DeepEqual(names, []string{"id", "remember_token"}) {
		t.Fatalf("columns: %v", names)
	}

	subs, _ := a.DescribeTable(ctx, "subscriptions")
	if subs.Description != "One row per plan purchase." || subs.Columns[2].Comment != "'active' = paying now" {
		t.Fatalf("notes not applied: %+v", subs)
	}
	if !reflect.DeepEqual(subs.Links, []string{"user_id -> users.id"}) { // no "(guessed)" anymore
		t.Fatalf("links: %v", subs.Links)
	}
}

func TestNotRelatedRemovesGuessedLink(t *testing.T) {
	a := newTestAgent(t)
	a.SetRules(&rules.Rules{NotRelated: []string{"subscriptions.user_id -> users.id"}})
	subs, _ := a.DescribeTable(context.Background(), "subscriptions")
	if len(subs.Links) != 0 {
		t.Fatalf("links: %v", subs.Links)
	}
}

func TestBrokenRulesFileFailsClosed(t *testing.T) {
	a := newTestAgent(t)
	if err := os.MkdirAll(a.Settings.StorageDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.RulesPath(), []byte("hidden:\n  tabels: [users]\n"), 0o644); err != nil { // typo
		t.Fatal(err)
	}
	if _, err := a.ListTables(context.Background()); err == nil || !strings.Contains(err.Error(), "rules.yaml") {
		t.Fatalf("expected rules error, got %v", err)
	}
}

func TestUnknownTable(t *testing.T) {
	a := newTestAgent(t)
	if d, _ := a.DescribeTable(context.Background(), "nope"); d != nil {
		t.Fatal("expected nil")
	}
}
