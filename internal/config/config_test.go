package config

import (
	"fmt"
	"strings"
	"testing"
)

func clearEnv(t *testing.T) {
	for _, k := range []string{"PROJECT_NAME", "DB_HOST", "DB_PORT", "DB_NAME", "DB_USER", "DB_PASSWORD", "MAX_ROWS", "QUERY_TIMEOUT_SECONDS"} {
		t.Setenv(EnvPrefix+k, "") // empty = not set for our loader
	}
}

func TestMissingSettingsGiveClearError(t *testing.T) {
	clearEnv(t)
	_, err := Load(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "DB_AGENT_DB_NAME") || !strings.Contains(err.Error(), "db-agent init") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	_, err := Save(dir, map[string]string{
		"DB_AGENT_DB_NAME": "shop", "DB_AGENT_DB_USER": "u", "DB_AGENT_DB_PORT": "3307",
		"DB_AGENT_DB_PASSWORD": `p@ss "quoted" \ word`,
	})
	if err != nil {
		t.Fatal(err)
	}
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.DBName != "shop" || s.DBUser != "u" || s.DBPort != 3307 || s.MaxRows != 100 {
		t.Fatalf("bad settings: %+v", s)
	}
	if s.DBPassword.Value() != `p@ss "quoted" \ word` {
		t.Fatalf("password not preserved: %q", s.DBPassword.Value())
	}
}

func TestEnvOverridesFile(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	if _, err := Save(dir, map[string]string{"DB_AGENT_DB_NAME": "shop", "DB_AGENT_DB_USER": "u"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DB_AGENT_DB_NAME", "other")
	s, err := Load(dir)
	if err != nil || s.DBName != "other" {
		t.Fatalf("env should win: %v %+v", err, s)
	}
}

func TestInvalidPort(t *testing.T) {
	clearEnv(t)
	t.Setenv("DB_AGENT_DB_NAME", "shop")
	t.Setenv("DB_AGENT_DB_USER", "u")
	t.Setenv("DB_AGENT_DB_PORT", "99999")
	if _, err := Load(t.TempDir()); err == nil || !strings.Contains(err.Error(), "DB_PORT") {
		t.Fatalf("expected port error, got %v", err)
	}
}

func TestPasswordNeverPrinted(t *testing.T) {
	s := Settings{DBPassword: "topsecret"}
	for _, out := range []string{fmt.Sprint(s), fmt.Sprintf("%+v", s), fmt.Sprintf("%#v", s)} {
		if strings.Contains(out, "topsecret") {
			t.Fatalf("password leaked: %s", out)
		}
	}
}
