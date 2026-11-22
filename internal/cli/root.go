// Package cli defines the db-agent terminal commands.
package cli

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/frahjokhio/db-agent/internal/config"
	"github.com/frahjokhio/db-agent/internal/database"
	"github.com/frahjokhio/db-agent/internal/service"
)

// Version is set at build time: go build -ldflags "-X github.com/frahjokhio/db-agent/internal/cli.Version=1.2.3"
var Version = "0.1.0-dev"

var projectDir string

// exitError carries a message already printed for the user plus an exit code.
type exitError struct{ code int }

func (e exitError) Error() string { return fmt.Sprintf("exit %d", e.code) }

func fail(format string, args ...any) error {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", args...)
	return exitError{1}
}

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	root := newRoot()
	if err := root.Execute(); err != nil {
		var ee exitError
		if errors.As(err, &ee) {
			return ee.code
		}
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1
	}
	return 0
}

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "db-agent",
		Short:         "Database AI Agent: safe database tools for your AI (Claude, ChatGPT, Cursor, Codex).",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runStatus,
	}
	root.PersistentFlags().StringVar(&projectDir, "dir", ".", "project folder (where .db-agent/ lives)")
	root.AddCommand(
		newInitCmd(), newCheckCmd(), newSchemaCmd(), newRulesCmd(), newQueryCmd(), newHistoryCmd(),
		newMCPCmd(), newMCPConfigCmd(), newVersionCmd(),
	)
	return root
}

func loadSettings() (config.Settings, error) {
	s, err := config.Load(projectDir)
	if err != nil {
		return s, fail("%v", err)
	}
	return s, nil
}

// openAgent loads settings and opens the core engine.
func openAgent() (*service.Agent, error) {
	s, err := loadSettings()
	if err != nil {
		return nil, err
	}
	agent, err := service.New(s)
	if err != nil {
		return nil, fail("%v", err)
	}
	return agent, nil
}

func dbFail(s config.Settings, err error) error {
	return fail("database problem at %s:%d/%s\n  %v", s.DBHost, s.DBPort, s.DBName, err)
}

// syncSchema reads the live schema and prints what changed.
func syncSchema(ctx context.Context, a *service.Agent) (*database.Schema, error) {
	res, err := database.Sync(ctx, a.DB, a.Store)
	if err != nil {
		return nil, err
	}
	if res.FirstSnapshot {
		fmt.Printf("OK: schema saved (%d tables)\n", len(res.Schema.Tables))
	} else if !res.Changes.IsEmpty() {
		printChanges(res.Changes)
	}
	a.SetSchema(res.Schema)
	return res.Schema, nil
}

func runStatus(cmd *cobra.Command, _ []string) error {
	a, err := openAgent()
	if err != nil {
		return err
	}
	defer a.Close()
	schema, err := syncSchema(cmd.Context(), a)
	if err != nil {
		return dbFail(a.Settings, err)
	}
	fmt.Printf("Database AI Agent v%s connected to %s (%d tables).\n", Version, a.Settings.ProjectName, len(schema.Tables))
	fmt.Println("  - Ask questions from your AI tool: run 'db-agent mcp-config' to connect it.")
	fmt.Println(`  - Run SQL safely: db-agent query "SELECT ..."`)
	fmt.Println("  - All commands: db-agent --help")
	return nil
}
