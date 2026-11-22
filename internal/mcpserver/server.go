// Package mcpserver exposes safe database tools to the user's own AI over MCP.
//
// The AI (Claude, ChatGPT, Cursor, Codex...) does the thinking and writes SQL.
// We provide schema knowledge and run only validated read-only queries.
package mcpserver

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/frahjokhio/db-agent/internal/database"
	"github.com/frahjokhio/db-agent/internal/service"
	"github.com/frahjokhio/db-agent/internal/sqlsafe"
)

const instructions = `You answer questions about the project's MySQL database "%s" (%s).

How to work:
1. Call get_business_rules once at the start. Company definitions (e.g. what "revenue"
   or "active customer" means) override your own assumptions. It also gives the database clock.
2. Find the right tables: call find_relevant_tables with the user's question, or list_tables.
3. Call describe_table for every table you will use. Never guess column names.
   Table/column descriptions there come from the company: follow them.
4. Write ONE MySQL SELECT and call run_query. Read-only: writes are always blocked.
5. Answer in short, plain bullet points for non-technical users. No SQL unless asked.
   If you used a business definition, name it briefly (e.g. "revenue = paid orders").

Rules:
- Do calculations (SUM, COUNT, AVG, percentages, comparisons) in SQL, not in your head.
- Only state facts that come from returned rows. If no rows, say so plainly.
- Max %d rows per query. Prefer aggregates over raw rows.
- Dates: use the database clock (CURDATE(), NOW()), state the exact date range you used,
  and say when a period is incomplete (e.g. "this month so far").
- Links marked "(guessed)" come from column names, not real foreign keys. Use them,
  but mention the assumption if the answer depends on it.
- If business meaning is unclear and not in get_business_rules (e.g. "active user",
  "best customer"), ask the user ONE short question before querying.
- Sensitive columns (passwords, tokens) are hidden and cannot be queried.
- If a query is blocked or fails, read the message, fix the SQL, and try again.
`

type tableArgs struct {
	Table string `json:"table" jsonschema:"Table name"`
}

type questionArgs struct {
	Question string `json:"question" jsonschema:"The user's question in plain words"`
}

type queryArgs struct {
	SQL string `json:"sql" jsonschema:"One MySQL SELECT statement"`
}

type historyArgs struct {
	Limit int `json:"limit,omitempty" jsonschema:"How many recent queries (1-50, default 10)"`
}

type historyResult struct {
	Queries []service.HistoryItem `json:"queries"`
}

// friendly turns our errors into clear messages the AI can act on.
func friendly(err error) error {
	var rejected *sqlsafe.Rejected
	var failed *sqlsafe.Failed
	var dbErr *database.Error
	switch {
	case errors.As(err, &rejected):
		return fmt.Errorf("Query blocked: %s", rejected.Msg)
	case errors.As(err, &failed):
		return fmt.Errorf("Query failed: %s", failed.Msg)
	case errors.As(err, &dbErr):
		return fmt.Errorf("Database unavailable: %s", dbErr.Msg)
	}
	return err
}

// New builds the MCP server. user/source are recorded in the audit log.
func New(agent *service.Agent, version, user, source string) *mcp.Server {
	s := agent.Settings
	server := mcp.NewServer(
		&mcp.Implementation{Name: "db-agent", Title: "Database Agent (" + s.ProjectName + ")", Version: version},
		&mcp.ServerOptions{Instructions: fmt.Sprintf(instructions, s.DBName, s.ProjectName, s.MaxRows)},
	)
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: new(bool)}
	who := sqlsafe.Caller{User: user, Source: source}

	mcp.AddTool(server, &mcp.Tool{
		Name: "get_business_rules", Annotations: readOnly,
		Description: "Company definitions (revenue, customer...), notes, business timezone and the database clock. Call once at the start.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, service.BusinessRules, error) {
		out, err := agent.GetBusinessRules(ctx)
		return nil, out, friendly(err)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "list_tables", Annotations: readOnly,
		Description: "List all tables with descriptions and how they link to each other.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, service.TableList, error) {
		out, err := agent.ListTables(ctx)
		return nil, out, friendly(err)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "describe_table", Annotations: readOnly,
		Description: "Columns, types, keys, indexes and links for one table. Call before writing SQL.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in tableArgs) (*mcp.CallToolResult, *service.TableDetail, error) {
		out, err := agent.DescribeTable(ctx, in.Table)
		if err != nil {
			return nil, nil, friendly(err)
		}
		if out == nil {
			return nil, nil, fmt.Errorf("Table '%s' not found. Call list_tables to see available tables.", in.Table)
		}
		return nil, out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "find_relevant_tables", Annotations: readOnly,
		Description: "Suggest the tables (with columns) most related to a question, plus linked tables.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in questionArgs) (*mcp.CallToolResult, service.RelevantTables, error) {
		out, err := agent.FindRelevantTables(ctx, in.Question)
		return nil, out, friendly(err)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "run_query", Annotations: readOnly,
		Description: "Run ONE read-only MySQL SELECT. Returns columns and rows (limited). " +
			"Blocked: writes, DDL, multiple statements, system tables, sensitive columns.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in queryArgs) (*mcp.CallToolResult, service.QueryResponse, error) {
		out, err := agent.RunQuery(ctx, in.SQL, who)
		return nil, out, friendly(err)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "get_query_history", Annotations: readOnly,
		Description: "Your recent queries (ok, blocked, failed), newest first.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in historyArgs) (*mcp.CallToolResult, historyResult, error) {
		limit := in.Limit
		if limit <= 0 {
			limit = 10
		}
		limit = min(limit, 50)
		items, err := agent.QueryHistory(who.User, limit)
		return nil, historyResult{Queries: items}, err
	})

	return server
}
