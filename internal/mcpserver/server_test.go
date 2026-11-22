package mcpserver_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/frahjokhio/db-agent/internal/mcpserver"
	"github.com/frahjokhio/db-agent/internal/testutil"
)

// End-to-end over MCP (in-memory transport) against the dev MySQL.
func TestMCPEndToEnd(t *testing.T) {
	agent := testutil.DevAgent(t)
	ctx := context.Background()

	serverT, clientT := mcp.NewInMemoryTransports()
	server := mcpserver.New(agent, "test", "tester", "mcp-test")
	if _, err := server.Connect(ctx, serverT, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	if !strings.Contains(session.InitializeResult().Instructions, `"shop"`) {
		t.Fatal("instructions missing database name")
	}

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
		if !tool.Annotations.ReadOnlyHint {
			t.Fatalf("%s should be read-only", tool.Name)
		}
	}
	want := []string{"describe_table", "find_relevant_tables", "get_business_rules", "get_query_history", "list_tables", "run_query"}
	if !reflect.DeepEqual(sorted(names), want) {
		t.Fatalf("tools: %v", names)
	}

	call := func(name string, args map[string]any) (*mcp.CallToolResult, map[string]any) {
		t.Helper()
		res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if !res.IsError {
			raw, _ := json.Marshal(res.StructuredContent)
			_ = json.Unmarshal(raw, &payload)
		}
		return res, payload
	}

	_, list := call("list_tables", nil)
	if list["table_count"].(float64) != 4 {
		t.Fatalf("list: %v", list)
	}

	_, found := call("find_relevant_tables", map[string]any{"question": "top customers by order total"})
	matches := found["matches"].([]any)
	first := matches[0].(map[string]any)["table"]
	second := matches[1].(map[string]any)["table"]
	if first != "orders" || second != "customers" {
		t.Fatalf("ranking: %v, %v", first, second)
	}

	_, q := call("run_query", map[string]any{"sql": "SELECT c.name, SUM(o.total) AS spent FROM customers c " +
		"JOIN orders o ON o.customer_id = c.id WHERE o.status = 'paid' GROUP BY c.name ORDER BY spent DESC LIMIT 2"})
	wantRows := []any{[]any{"Ali Khan", "203000.00"}, []any{"Bilal Shah", "150000.00"}}
	if !reflect.DeepEqual(q["rows"], wantRows) {
		t.Fatalf("rows: %v", q["rows"])
	}

	blockedRes, _ := call("run_query", map[string]any{"sql": "DELETE FROM orders"})
	if !blockedRes.IsError || !strings.Contains(text(blockedRes), "Only SELECT") {
		t.Fatalf("expected blocked: %v", text(blockedRes))
	}

	missing, _ := call("describe_table", map[string]any{"table": "nope"})
	if !missing.IsError || !strings.Contains(text(missing), "not found") {
		t.Fatalf("expected not found: %v", text(missing))
	}

	_, hist := call("get_query_history", map[string]any{"limit": 5})
	qs := hist["queries"].([]any)
	if qs[0].(map[string]any)["status"] != "rejected" || qs[1].(map[string]any)["status"] != "ok" {
		t.Fatalf("history: %v", qs)
	}
}

// Business rules over MCP: definitions returned, hidden column blocked, edits picked up live.
func TestMCPBusinessRules(t *testing.T) {
	agent := testutil.DevAgent(t)
	ctx := context.Background()
	if err := os.MkdirAll(agent.Settings.StorageDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	writeRules := func(content string) {
		if err := os.WriteFile(agent.RulesPath(), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeRules(`
timezone: Asia/Karachi
definitions:
  revenue: Sum of orders.total where orders.status = 'paid'.
relationships:
  - order_items.order_id -> orders.id
hidden:
  columns: [customers.email]
`)

	serverT, clientT := mcp.NewInMemoryTransports()
	if _, err := mcpserver.New(agent, "test", "tester", "mcp-test").Connect(ctx, serverT, nil); err != nil {
		t.Fatal(err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "1"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	call := func(name string, args map[string]any) (*mcp.CallToolResult, map[string]any) {
		t.Helper()
		res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		raw, _ := json.Marshal(res.StructuredContent)
		_ = json.Unmarshal(raw, &payload)
		return res, payload
	}

	_, br := call("get_business_rules", nil)
	defs, _ := br["definitions"].(map[string]any)
	if br["business_timezone"] != "Asia/Karachi" || defs["revenue"] == nil || br["database_now"] == "" {
		t.Fatalf("business rules: %v", br)
	}

	_, items := call("describe_table", map[string]any{"table": "order_items"})
	links := fmt.Sprint(items["links"])
	if !strings.Contains(links, "order_id -> orders.id") || strings.Contains(links, "order_id -> orders.id (guessed)") {
		t.Fatalf("link not confirmed: %v", links)
	}

	res, _ := call("run_query", map[string]any{"sql": "SELECT email FROM customers"})
	if !res.IsError || !strings.Contains(text(res), "sensitive") {
		t.Fatalf("hidden column should be blocked: %v", text(res))
	}

	// Edit the file: rule removed -> allowed again (mtime must change).
	time.Sleep(20 * time.Millisecond)
	writeRules("definitions:\n  revenue: paid orders\n")
	future := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(agent.RulesPath(), future, future)
	res, _ = call("run_query", map[string]any{"sql": "SELECT email FROM customers LIMIT 1"})
	if res.IsError {
		t.Fatalf("after edit, email should be allowed: %v", text(res))
	}
}

func text(r *mcp.CallToolResult) string {
	var parts []string
	for _, c := range r.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, " ")
}

func sorted(in []string) []string {
	out := append([]string(nil), in...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}
