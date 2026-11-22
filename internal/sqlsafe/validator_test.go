package sqlsafe

import (
	"errors"
	"strings"
	"testing"

	"github.com/frahjokhio/db-agent/internal/database"
)

func cols(names ...string) []database.Column {
	out := make([]database.Column, len(names))
	for i, n := range names {
		out[i] = database.Column{Name: n, Type: "varchar(50)", Nullable: true}
	}
	return out
}

var testSchema = &database.Schema{Database: "shop", Tables: []*database.Table{
	{Name: "customers", Columns: cols("id", "name", "city"), PrimaryKey: []string{"id"}},
	{Name: "orders", Columns: cols("id", "customer_id", "total", "status"), PrimaryKey: []string{"id"}},
	{Name: "users", Columns: cols("id", "name", "password", "remember_token"), PrimaryKey: []string{"id"}},
	{Name: "audit_secrets", Columns: cols("id", "note"), PrimaryKey: []string{"id"}},
}}

func testPolicy() Policy {
	p := DefaultPolicy(100)
	p.BlockedTables["audit_secrets"] = true
	return p
}

func ok(t *testing.T, sql string) string {
	t.Helper()
	v, err := Validate(sql, testSchema, testPolicy())
	if err != nil {
		t.Fatalf("expected OK for %q, got %v", sql, err)
	}
	return v.SQL
}

func blocked(t *testing.T, sql, contains string) {
	t.Helper()
	_, err := Validate(sql, testSchema, testPolicy())
	var r *Rejected
	if !errors.As(err, &r) {
		t.Fatalf("expected rejection for %q, got %v", sql, err)
	}
	if contains != "" && !strings.Contains(r.Msg, contains) {
		t.Fatalf("for %q: message %q does not contain %q", sql, r.Msg, contains)
	}
}

// ---------- allowed ----------

func TestLimitAddedKeptOrCapped(t *testing.T) {
	if s := ok(t, "SELECT name FROM customers"); !strings.HasSuffix(s, "LIMIT 101") {
		t.Fatal(s)
	}
	if s := ok(t, "SELECT name FROM customers LIMIT 5"); !strings.HasSuffix(s, "LIMIT 5") {
		t.Fatal(s)
	}
	if s := ok(t, "SELECT name FROM customers LIMIT 5000"); !strings.HasSuffix(s, "LIMIT 101") {
		t.Fatal(s)
	}
	if s := ok(t, "SELECT name FROM customers LIMIT 20, 5000"); !strings.HasSuffix(s, "LIMIT 20,101") {
		t.Fatal(s)
	}
	if s := ok(t, "SELECT name FROM customers UNION SELECT status FROM orders"); !strings.HasSuffix(s, "LIMIT 101") {
		t.Fatal(s)
	}
}

func TestJoinsAggregatesCTEsAllowed(t *testing.T) {
	ok(t, "SELECT c.name, SUM(o.total) FROM customers c JOIN orders o ON o.customer_id = c.id GROUP BY c.name")
	ok(t, "WITH big AS (SELECT customer_id FROM orders WHERE total > 100) SELECT COUNT(*) FROM big")
	ok(t, "SELECT name FROM shop.customers")
	ok(t, "SELECT name FROM customers WHERE id IN (SELECT customer_id FROM orders)")
}

func TestSafeUseOfSensitiveTable(t *testing.T) {
	ok(t, "SELECT COUNT(*) FROM users")
	ok(t, "SELECT id, name FROM users")
}

func TestLiteralsPreserved(t *testing.T) {
	s := ok(t, "SELECT name FROM customers WHERE name LIKE '%ali%' AND city <> '10:30'")
	if !strings.Contains(s, "'%ali%'") || !strings.Contains(s, "'10:30'") {
		t.Fatal(s)
	}
}

func TestPlainCommentsDropped(t *testing.T) {
	if s := ok(t, "SELECT name /* hi */ FROM customers -- bye"); strings.Contains(s, "hi") || strings.Contains(s, "bye") {
		t.Fatal(s)
	}
}

// ---------- blocked ----------

func TestNonSelectBlocked(t *testing.T) {
	for _, sql := range []string{
		"INSERT INTO customers (name) VALUES ('x')",
		"UPDATE customers SET name = 'x'",
		"DELETE FROM customers",
		"DROP TABLE customers",
		"CREATE TABLE x (id int)",
		"ALTER TABLE customers ADD COLUMN x int",
		"TRUNCATE TABLE customers",
		"SET @a = 1",
		"CALL some_proc()",
		"GRANT SELECT ON shop.* TO 'x'",
		"SHOW TABLES",
	} {
		blocked(t, sql, "")
	}
}

func TestMultipleStatementsBlocked(t *testing.T) {
	blocked(t, "SELECT name FROM customers; DELETE FROM customers", "one statement")
	blocked(t, "SELECT name FROM customers; SELECT 1", "one statement")
}

func TestLocksAndIntoBlocked(t *testing.T) {
	blocked(t, "SELECT name FROM customers FOR UPDATE", "Locking")
	blocked(t, "SELECT name FROM customers LOCK IN SHARE MODE", "Locking")
	blocked(t, "SELECT name FROM customers INTO OUTFILE '/tmp/x'", "")
}

func TestDangerousFunctionsBlocked(t *testing.T) {
	for _, fn := range []string{"SLEEP(5)", "BENCHMARK(1000000, MD5('a'))", "LOAD_FILE('/etc/passwd')", "GET_LOCK('a', 10)"} {
		blocked(t, "SELECT "+fn+" FROM customers", "Function not allowed")
	}
}

func TestSystemAndOtherDatabasesBlocked(t *testing.T) {
	blocked(t, "SELECT table_name FROM information_schema.tables", "Only the 'shop' database")
	blocked(t, "SELECT user FROM mysql.user", "Only the 'shop' database")
	blocked(t, "SELECT name FROM other_db.customers", "Only the 'shop' database")
}

func TestHiddenExecutableCommentIsChecked(t *testing.T) {
	// MySQL runs /*! ... */ as code; the parser does too, so the hidden subquery is caught.
	blocked(t, "SELECT name FROM customers /*!50000 , (SELECT password FROM users) x */", "sensitive")
}

func TestUnknownAndBlockedTables(t *testing.T) {
	blocked(t, "SELECT * FROM invoices", "Unknown table")
	blocked(t, "SELECT note FROM audit_secrets", "Table not allowed")
}

func TestSensitiveColumnsBlocked(t *testing.T) {
	blocked(t, "SELECT password FROM users", "sensitive")
	blocked(t, "SELECT name FROM users WHERE remember_token IS NOT NULL", "sensitive")
	blocked(t, "SELECT * FROM users", "SELECT *")
	blocked(t, "SELECT u.* FROM users u", "SELECT *")
	blocked(t, "SELECT c.name FROM customers c JOIN users u ON u.id = c.id WHERE u.password = 'x'", "sensitive")
}

func TestVariablesBlocked(t *testing.T) {
	blocked(t, "SELECT @@version FROM customers", "Variables")
	blocked(t, "SELECT @a := 1 FROM customers", "Variables")
}

func TestEmptyAndGarbage(t *testing.T) {
	blocked(t, "", "Empty")
	blocked(t, "SELEC name FROM", "Could not parse")
}
