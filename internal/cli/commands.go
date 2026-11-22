package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/frahjokhio/db-agent/internal/audit"
	"github.com/frahjokhio/db-agent/internal/config"
	"github.com/frahjokhio/db-agent/internal/database"
	"github.com/frahjokhio/db-agent/internal/mcpserver"
	"github.com/frahjokhio/db-agent/internal/rules"
	"github.com/frahjokhio/db-agent/internal/service"
	"github.com/frahjokhio/db-agent/internal/sqlsafe"
	"github.com/frahjokhio/db-agent/internal/storage"
)

// ---------- init ----------

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Set up the database connection for this project (.db-agent/config.env)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, _ := filepath.Abs(projectDir)
			in := bufio.NewReader(os.Stdin)
			path := config.ConfigPath(dir)
			if _, err := os.Stat(path); err == nil {
				if !strings.HasPrefix(strings.ToLower(ask(in, path+" exists. Overwrite? (y/N)", "n")), "y") {
					return nil
				}
			}
			// Asked one by one so the order on screen is natural.
			project := ask(in, "Project name", filepath.Base(dir))
			host := ask(in, "MySQL host", "127.0.0.1")
			port := ask(in, "MySQL port", "3306")
			if _, err := strconv.Atoi(port); err != nil {
				return fail("port must be a number")
			}
			name := ask(in, "Database name", "")
			user := ask(in, "Username (read-only recommended)", "")
			password := askSecret(in, "Password")
			values := map[string]string{
				"DB_AGENT_PROJECT_NAME": project,
				"DB_AGENT_DB_HOST":      host,
				"DB_AGENT_DB_PORT":      port,
				"DB_AGENT_DB_NAME":      name,
				"DB_AGENT_DB_USER":      user,
				"DB_AGENT_DB_PASSWORD":  password,
			}
			saved, err := config.Save(dir, values)
			if err != nil {
				return fail("cannot save settings: %v", err)
			}
			fmt.Println("OK: saved", saved)
			fmt.Println("  Tip: add .db-agent/config.env and .db-agent/*.db to .gitignore (password + local data).")
			fmt.Println("       .db-agent/rules.yaml is safe to commit. Create it with: db-agent rules init")
			return runCheck(cmd, true)
		},
	}
}

func ask(in *bufio.Reader, label, fallback string) string {
	if fallback != "" {
		fmt.Printf("%s [%s]: ", label, fallback)
	} else {
		fmt.Printf("%s: ", label)
	}
	line, _ := in.ReadString('\n')
	if line = strings.TrimSpace(line); line == "" {
		return fallback
	}
	return line
}

func askSecret(in *bufio.Reader, label string) string {
	fmt.Printf("%s: ", label)
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		b, _ := term.ReadPassword(fd)
		fmt.Println()
		return string(b)
	}
	line, _ := in.ReadString('\n')
	return strings.TrimRight(line, "\r\n")
}

// ---------- check ----------

func newCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Test the connection to the configured database",
		RunE:  func(cmd *cobra.Command, _ []string) error { return runCheck(cmd, false) },
	}
}

func runCheck(cmd *cobra.Command, sync bool) error {
	a, err := openAgent()
	if err != nil {
		return err
	}
	defer a.Close()
	info, err := database.Check(cmd.Context(), a.DB)
	if err != nil {
		return dbFail(a.Settings, err)
	}
	fmt.Printf("OK: connected to %s\n", a.Settings.ProjectName)
	fmt.Printf("  - Database: %s\n  - MySQL: %s\n  - Tables: %d\n", info.Database, info.ServerVersion, info.TableCount)
	if sync {
		if _, err := syncSchema(cmd.Context(), a); err != nil {
			return dbFail(a.Settings, err)
		}
	}
	return nil
}

// ---------- schema ----------

func newSchemaCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "schema [table]",
		Short: "Show the current database structure",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := openAgent()
			if err != nil {
				return err
			}
			defer a.Close()

			if _, err := syncSchema(cmd.Context(), a); err != nil {
				saved, _ := a.Store.Latest(a.Settings.DBName)
				if saved == nil {
					return fail("database unavailable and no saved schema.\n  %v", err)
				}
				fmt.Printf("WARNING: database unavailable. Showing saved schema from %s UTC (may be outdated).\n",
					saved.CapturedAt.Format("2006-01-02 15:04"))
				a.SetSchema(saved)
			}
			st, err := a.Current(cmd.Context()) // rules applied (confirmed links, descriptions)
			if err != nil {
				return fail("%v", err)
			}

			if len(args) == 0 {
				printSchemaOverview(st)
				return nil
			}
			t := st.Schema.Table(args[0])
			if t == nil {
				return fail("table '%s' not found.", args[0])
			}
			printTableDetail(t, st.Policy)
			return nil
		},
	}
}

func printSchemaOverview(st service.State) {
	s := st.Schema
	rows := [][]string{}
	for _, t := range s.Tables {
		kind := t.Kind
		if st.Policy.IsBlocked(t.Name) {
			kind += " (hidden)"
		}
		var links []string
		for _, fk := range t.ForeignKeys {
			l := fk.RefTable
			if fk.Inferred {
				l += "*"
			}
			links = append(links, l)
		}
		est := "-"
		if t.RowEstimate != nil {
			est = strconv.FormatInt(*t.RowEstimate, 10)
		}
		rows = append(rows, []string{t.Name, kind, strconv.Itoa(len(t.Columns)), est, orDash(strings.Join(links, ", "))})
	}
	printTable(os.Stdout, fmt.Sprintf("%s (%d tables)", s.Database, len(s.Tables)),
		[]string{"Table", "Type", "Columns", "~Rows", "Links to"}, rows)
	fmt.Println("* = guessed from column name (confirm in rules.yaml)   (hidden) = never shown to the AI")
	fmt.Printf("Snapshot: %s UTC\n", s.CapturedAt.Format("2006-01-02 15:04"))
}

func printTableDetail(t *database.Table, p sqlsafe.Policy) {
	pk := map[string]bool{}
	for _, c := range t.PrimaryKey {
		pk[c] = true
	}
	fkOf := map[string]database.ForeignKey{}
	for _, fk := range t.ForeignKeys {
		for _, c := range fk.Columns {
			fkOf[c] = fk
		}
	}
	rows := [][]string{}
	for _, c := range t.Columns {
		var key []string
		if pk[c.Name] {
			key = append(key, "PK")
		}
		if fk, ok := fkOf[c.Name]; ok {
			ref := "-> " + fk.RefTable
			if fk.Inferred {
				ref += "*"
			}
			key = append(key, ref)
		}
		if p.IsSensitive(t.Name, c.Name) {
			key = append(key, "HIDDEN")
		}
		null := "no"
		if c.Nullable {
			null = "yes"
		}
		rows = append(rows, []string{c.Name, c.Type, null, strings.Join(key, " "), c.Comment})
	}
	title := fmt.Sprintf("%s (%s)", t.Name, t.Kind)
	if p.IsBlocked(t.Name) {
		title += " [hidden from the AI]"
	}
	if t.Comment != "" {
		title += " - " + t.Comment
	}
	printTable(os.Stdout, title, []string{"Column", "Type", "Null", "Key", "Comment"}, rows)
	for _, idx := range t.Indexes {
		kind := "index"
		if idx.Unique {
			kind = "unique"
		}
		fmt.Printf("  - %s %s: %s\n", kind, idx.Name, strings.Join(idx.Columns, ", "))
	}
}

func printChanges(c database.Changes) {
	fmt.Println("Schema changed since last snapshot:")
	for _, n := range c.AddedTables {
		fmt.Println("  + table", n)
	}
	for _, n := range c.RemovedTables {
		fmt.Println("  - table", n)
	}
	names := make([]string, 0, len(c.ChangedTables))
	for n := range c.ChangedTables {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Printf("  ~ table %s: %s\n", n, strings.Join(c.ChangedTables[n], "; "))
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// ---------- rules ----------

func newRulesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rules",
		Short: "Show business rules (.db-agent/rules.yaml) and check them against the database",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := openAgent()
			if err != nil {
				return err
			}
			defer a.Close()
			path := a.RulesPath()
			if _, err := os.Stat(path); err != nil {
				fmt.Println("No rules file yet:", path)
				fmt.Println("  Create a starter file: db-agent rules init")
				return nil
			}
			r, err := rules.Load(path)
			if err != nil {
				return fail("%v", err)
			}
			schema, err := syncSchema(cmd.Context(), a)
			if err != nil {
				return dbFail(a.Settings, err)
			}

			fmt.Println("Rules:", path)
			if r.IsEmpty() {
				fmt.Println("  (empty: everything is still commented out)")
			}
			if r.Timezone != "" {
				fmt.Println("  - Timezone:", r.Timezone)
			}
			printList("Definitions", sortedKeys(r.Definitions))
			printList("Notes", r.Notes)
			printList("Relationships", r.Relationships)
			printList("Not related", r.NotRelated)
			printList("Hidden tables", r.Hidden.Tables)
			printList("Hidden columns", r.Hidden.Columns)
			printList("Shown columns", r.ShowColumns)
			printList("Table notes", sortedKeys(r.Tables))

			warnings := rules.Validate(schema, r)
			if len(warnings) == 0 {
				fmt.Println("OK: rules match the database.")
				return nil
			}
			fmt.Println("WARNINGS (only these entries are skipped until fixed; the rest apply):")
			for _, w := range warnings {
				fmt.Println("  -", w)
			}
			return exitError{1}
		},
	}

	var force bool
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Create a starter rules.yaml (lists guessed links to confirm)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := openAgent()
			if err != nil {
				return err
			}
			defer a.Close()
			path := a.RulesPath()
			if _, err := os.Stat(path); err == nil && !force {
				return fail("%s already exists (use --force to overwrite).", path)
			}
			schema, err := syncSchema(cmd.Context(), a)
			if err != nil {
				return dbFail(a.Settings, err)
			}
			policy := sqlsafe.DefaultPolicy(a.Settings.MaxRows)
			content := rules.Template(a.Settings.ProjectName, schema, policy.IsSensitive)
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				return fail("cannot write %s: %v", path, err)
			}
			fmt.Println("OK: created", path)
			fmt.Println("  Edit it, then run: db-agent rules")
			return nil
		},
	}
	initCmd.Flags().BoolVar(&force, "force", false, "overwrite an existing rules.yaml")
	cmd.AddCommand(initCmd)
	return cmd
}

func printList(label string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Printf("  - %s (%d):\n", label, len(items))
	for _, it := range items {
		fmt.Println("      ", truncate(it, 100))
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ---------- query / history ----------

func newQueryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   `query "SELECT ..."`,
		Short: "Run a SELECT safely (same checks the AI tools use)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := openAgent()
			if err != nil {
				return err
			}
			defer a.Close()
			if _, err := syncSchema(cmd.Context(), a); err != nil {
				return dbFail(a.Settings, err)
			}
			res, err := a.RunQuery(cmd.Context(), args[0], sqlsafe.Caller{User: currentUser(), Source: "cli"})
			var rejected *sqlsafe.Rejected
			if errors.As(err, &rejected) {
				fmt.Fprintln(os.Stderr, "Blocked:", rejected.Msg)
				return exitError{1}
			}
			if err != nil {
				fmt.Fprintln(os.Stderr, "Query failed:", err)
				return exitError{1}
			}
			rows := make([][]string, len(res.Rows))
			for i, r := range res.Rows {
				rows[i] = make([]string, len(r))
				for j, v := range r {
					if v == nil {
						rows[i][j] = "NULL"
					} else {
						rows[i][j] = fmt.Sprint(v)
					}
				}
			}
			printTable(os.Stdout, "", res.Columns, rows)
			more := ""
			if res.Truncated != "" {
				more = " (more rows exist; limit reached)"
			}
			fmt.Printf("%d rows%s\n", res.RowCount, more)
			return nil
		},
	}
}

func newHistoryCmd() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "history",
		Short: "Show recent queries (ok, blocked, failed)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := loadSettings()
			if err != nil {
				return err
			}
			db, err := storage.Open(s.AgentDBPath())
			if err != nil {
				return fail("%v", err)
			}
			defer db.Close()
			entries, err := audit.New(db).Recent(limit, "")
			if err != nil {
				return fail("%v", err)
			}
			if len(entries) == 0 {
				fmt.Println("No queries yet.")
				return nil
			}
			rows := [][]string{}
			for _, e := range entries {
				count, ms := "", ""
				if e.RowCount != nil {
					count = strconv.FormatInt(*e.RowCount, 10)
				}
				if e.DurationMs != nil {
					ms = strconv.FormatInt(*e.DurationMs, 10)
				}
				detail := e.SQL
				if e.Error != "" {
					detail += "  >> " + e.Error
				}
				rows = append(rows, []string{
					strconv.FormatInt(e.ID, 10), e.CreatedAt.Format("01-02 15:04:05"), e.User, e.Source,
					string(e.Status), count, ms, truncate(detail, 90),
				})
			}
			printTable(os.Stdout, "", []string{"#", "When (UTC)", "User", "Source", "Status", "Rows", "ms", "SQL / Error"}, rows)
			return nil
		},
	}
	cmd.Flags().IntVarP(&limit, "limit", "n", 20, "how many entries")
	return cmd
}

// ---------- mcp ----------

func newMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Run the local MCP server (stdio) for Claude Code, Cursor, Codex, Claude Desktop",
		Long: "Run the local MCP server over stdio. Your AI tool starts this automatically;\n" +
			"running it by hand just waits silently (Ctrl+C to stop).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// stdout is the MCP channel: never print to it here. Errors go to stderr.
			a, err := openAgent()
			if err != nil {
				return err
			}
			defer a.Close()
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()
			server := mcpserver.New(a, Version, currentUser(), "mcp-local")
			if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && ctx.Err() == nil {
				return fail("MCP server stopped: %v", err)
			}
			return nil
		},
	}
}

func newMCPConfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp-config",
		Short: "Print the config to connect this project to your AI tool",
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, _ := filepath.Abs(projectDir)
			exe, args := launchCommand(dir)

			fmt.Println("Claude Code (run in your terminal):")
			fmt.Printf("  claude mcp add db-agent -- %s\n\n", shellJoin(append([]string{exe}, args...)))

			fmt.Println("Cursor (.cursor/mcp.json) / Claude Desktop (claude_desktop_config.json):")
			cfg := map[string]any{"mcpServers": map[string]any{"db-agent": map[string]any{"command": exe, "args": args}}}
			out, _ := json.MarshalIndent(cfg, "", "  ")
			fmt.Println(string(out))

			fmt.Println("\nCodex (~/.codex/config.toml):")
			argsJSON, _ := json.Marshal(args)
			exeJSON, _ := json.Marshal(exe)
			fmt.Printf("  [mcp_servers.db-agent]\n  command = %s\n  args = %s\n", exeJSON, argsJSON)
			return nil
		},
	}
}

// NPMPackage is the npm package that ships this binary.
const NPMPackage = "@db-agent/cli"

// launchCommand returns how an AI tool should start the MCP server.
// Installed via npm: use npx (stable, survives npm cache cleanup).
// Otherwise: the absolute path of this binary.
func launchCommand(dir string) (string, []string) {
	mcpArgs := []string{"mcp", "--dir", dir}
	if os.Getenv("DB_AGENT_LAUNCHER") == "npm" {
		npx := append([]string{"-y", NPMPackage}, mcpArgs...)
		if runtime.GOOS == "windows" {
			// AI tools on Windows can't start npx.cmd directly.
			return "cmd", append([]string{"/c", "npx"}, npx...)
		}
		return "npx", npx
	}
	exe, err := os.Executable()
	if err != nil {
		exe = "db-agent"
	}
	return exe, mcpArgs
}

// shellJoin quotes arguments that contain spaces (for copy-paste into a terminal).
func shellJoin(parts []string) string {
	out := make([]string, len(parts))
	for i, p := range parts {
		if strings.ContainsAny(p, " \t") {
			out[i] = `"` + p + `"` // plain quotes: Windows paths keep single backslashes
		} else {
			out[i] = p
		}
	}
	return strings.Join(out, " ")
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show the version",
		Run:   func(*cobra.Command, []string) { fmt.Println(Version) },
	}
}

// currentUser returns the OS user name without a Windows domain prefix.
func currentUser() string {
	u, err := user.Current()
	if err != nil {
		return "unknown"
	}
	name := u.Username
	if i := strings.LastIndex(name, `\`); i >= 0 {
		name = name[i+1:]
	}
	return name
}
