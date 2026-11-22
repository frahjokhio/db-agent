package sqlsafe_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/frahjokhio/db-agent/internal/database"
	"github.com/frahjokhio/db-agent/internal/sqlsafe"
	"github.com/frahjokhio/db-agent/internal/testutil"
)

var tester = sqlsafe.Caller{User: "tester", Source: "test"}

func TestIntrospectSampleShop(t *testing.T) {
	a := testutil.DevAgent(t)
	s, err := database.Introspect(context.Background(), a.DB)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Tables) != 4 {
		t.Fatalf("want 4 tables, got %d", len(s.Tables))
	}
	orders := s.Table("orders")
	if !reflect.DeepEqual(orders.PrimaryKey, []string{"id"}) || orders.Column("total").Type != "decimal(12,2)" {
		t.Fatalf("bad orders: %+v", orders)
	}
	if fk := orders.ForeignKeys[0]; fk.Inferred || fk.RefTable != "customers" {
		t.Fatalf("bad FK: %+v", fk)
	}
	if s.Table("customers").Comment != "People who registered in the shop" {
		t.Fatal("table comment missing")
	}
	inferred := map[string]bool{}
	for _, fk := range s.Table("order_items").ForeignKeys {
		if fk.Inferred {
			inferred[fk.RefTable] = true
		}
	}
	if !inferred["orders"] || !inferred["products"] {
		t.Fatalf("inferred links missing: %v", inferred)
	}
}

func TestRunJoinWithJSONSafeValues(t *testing.T) {
	a := testutil.DevAgent(t)
	res, err := a.RunQuery(context.Background(), `
		SELECT c.name, SUM(o.total) AS spent, MAX(o.ordered_at) AS last_order
		FROM customers c JOIN orders o ON o.customer_id = c.id
		WHERE o.status = 'paid' GROUP BY c.name ORDER BY spent DESC`, tester)
	if err != nil {
		t.Fatal(err)
	}
	want := []any{"Ali Khan", "203000.00", "2026-10-02T13:00:00"}
	if !reflect.DeepEqual(res.Rows[0], want) {
		t.Fatalf("got %v", res.Rows[0])
	}
	entries, _ := a.Audit.Recent(1, "")
	if entries[0].Status != "ok" || *entries[0].RowCount != int64(res.RowCount) || entries[0].User != "tester" {
		t.Fatalf("bad audit: %+v", entries[0])
	}
}

func TestLikeWithPercent(t *testing.T) {
	a := testutil.DevAgent(t)
	res, err := a.RunQuery(context.Background(), "SELECT name FROM customers WHERE email LIKE '%sara%'", tester)
	if err != nil || len(res.Rows) != 1 || res.Rows[0][0] != "Sara Ahmed" {
		t.Fatalf("got %v %v", res.Rows, err)
	}
}

func TestTruncation(t *testing.T) {
	a := testutil.DevAgent(t)
	a.Settings.MaxRows = 3 // read when the policy is first built
	res, err := a.RunQuery(context.Background(), "SELECT id FROM customers", tester)
	if err != nil || res.RowCount != 3 || res.Truncated == "" {
		t.Fatalf("got %+v %v", res, err)
	}
}

func TestRejectedQueryIsAudited(t *testing.T) {
	a := testutil.DevAgent(t)
	_, err := a.RunQuery(context.Background(), "DELETE FROM orders", tester)
	var r *sqlsafe.Rejected
	if !errors.As(err, &r) {
		t.Fatalf("expected rejection, got %v", err)
	}
	entries, _ := a.Audit.Recent(1, "")
	if entries[0].Status != "rejected" || entries[0].SQL != "DELETE FROM orders" {
		t.Fatalf("bad audit: %+v", entries[0])
	}
}

func TestWriteBlockedByDatabaseEvenIfValidatorBypassed(t *testing.T) {
	a := testutil.DevAgent(t)
	_, err := sqlsafe.Execute(context.Background(), a.DB, sqlsafe.Validated{SQL: "DELETE FROM orders"}, 10, 5*time.Second)
	var f *sqlsafe.Failed
	if !errors.As(err, &f) {
		t.Fatalf("expected DB failure, got %v", err)
	}
}

func TestTimeoutStopsSlowQuery(t *testing.T) {
	a := testutil.DevAgent(t)
	slow := sqlsafe.Validated{SQL: "SELECT COUNT(*) FROM information_schema.columns a, " +
		"information_schema.columns b, information_schema.columns c"}
	_, err := sqlsafe.Execute(context.Background(), a.DB, slow, 10, time.Second)
	var f *sqlsafe.Failed
	if !errors.As(err, &f) || f.Msg != "Query took longer than 1s and was stopped." {
		t.Fatalf("expected timeout, got %v", err)
	}
}
