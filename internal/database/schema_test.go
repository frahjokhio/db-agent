package database

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/frahjokhio/db-agent/internal/storage"
)

func cols(names ...string) []Column {
	out := make([]Column, len(names))
	for i, n := range names {
		out[i] = Column{Name: n, Type: "int"}
	}
	return out
}

func sampleSchema() *Schema {
	s := &Schema{Database: "shop", Tables: []*Table{
		{Name: "categories", Columns: cols("id"), PrimaryKey: []string{"id"}},
		{Name: "customers", Columns: cols("id"), PrimaryKey: []string{"id"}},
		{Name: "orders", Columns: cols("id", "customer_id"), PrimaryKey: []string{"id"},
			ForeignKeys: []ForeignKey{{Columns: []string{"customer_id"}, RefTable: "customers", RefColumns: []string{"id"}}}},
		{Name: "order_items", Columns: cols("id", "order_id", "category_id", "vendor_id"), PrimaryKey: []string{"id"}},
	}}
	s.normalize()
	return s
}

func TestInferRelationships(t *testing.T) {
	s := sampleSchema()
	InferRelationships(s.Tables)
	got := map[string]string{}
	for _, fk := range s.Table("order_items").ForeignKeys {
		if !fk.Inferred {
			t.Fatal("expected inferred")
		}
		got[fk.Columns[0]] = fk.RefTable
	}
	want := map[string]string{"order_id": "orders", "category_id": "categories"} // y -> ies; no vendors table
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if n := len(s.Table("orders").ForeignKeys); n != 1 {
		t.Fatalf("real FK duplicated: %d", n)
	}
}

func TestFingerprintIgnoresRowCountsButNotStructure(t *testing.T) {
	a, b := sampleSchema(), sampleSchema()
	n := int64(999)
	b.Tables[0].RowEstimate = &n
	if a.Fingerprint() != b.Fingerprint() {
		t.Fatal("row count changed fingerprint")
	}
	b.Tables[0].Columns = append(b.Tables[0].Columns, Column{Name: "name", Type: "varchar(50)"})
	if a.Fingerprint() == b.Fingerprint() {
		t.Fatal("structure change not detected")
	}
}

func TestDiff(t *testing.T) {
	old, cur := sampleSchema(), sampleSchema()
	cur.Tables = cur.Tables[1:] // remove categories
	cur.Tables = append(cur.Tables, &Table{Name: "refunds", Columns: cols("id")})
	cur.Table("customers").Columns = append(cur.Table("customers").Columns, Column{Name: "email", Type: "varchar(150)"})
	c := Diff(old, cur)
	if !reflect.DeepEqual(c.AddedTables, []string{"refunds"}) ||
		!reflect.DeepEqual(c.RemovedTables, []string{"categories"}) ||
		!reflect.DeepEqual(c.ChangedTables, map[string][]string{"customers": {"+ column email"}}) {
		t.Fatalf("bad diff: %+v", c)
	}
}

func TestStoreRoundTrip(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewSchemaStore(db)
	if s, _ := store.Latest("shop"); s != nil {
		t.Fatal("expected empty store")
	}
	s := sampleSchema()
	if err := store.Save(s); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Latest("shop")
	if err != nil || loaded.Fingerprint() != s.Fingerprint() {
		t.Fatalf("round trip failed: %v", err)
	}
}

func TestSyncDetectsNothingWhenUnchanged(t *testing.T) {
	// Covered against real MySQL in sqlsafe integration tests; here just make sure Diff of equal is empty.
	if !Diff(sampleSchema(), sampleSchema()).IsEmpty() {
		t.Fatal("equal schemas should have no changes")
	}
	_ = context.Background()
}
