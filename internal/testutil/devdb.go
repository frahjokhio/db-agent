// Package testutil gives tests access to the dev Docker MySQL (port 3307).
package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/frahjokhio/db-agent/internal/config"
	"github.com/frahjokhio/db-agent/internal/database"
	"github.com/frahjokhio/db-agent/internal/service"
)

// DevSettings point at the sample shop DB from docker-compose.yml.
func DevSettings(t *testing.T) config.Settings {
	return config.Settings{
		ProjectDir: t.TempDir(), ProjectName: "Sample Shop",
		DBHost: "127.0.0.1", DBPort: 3307, DBName: "shop",
		DBUser: "agent_ro", DBPassword: "agent_ro_pass",
		QueryTimeout: 10 * time.Second, MaxRows: 100,
	}
}

// DevAgent opens the core engine on the dev DB, or skips the test if MySQL isn't running.
func DevAgent(t *testing.T) *service.Agent {
	t.Helper()
	a, err := service.New(DevSettings(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := database.Check(ctx, a.DB); err != nil {
		t.Skip("dev MySQL not running (docker compose up -d)")
	}
	return a
}
