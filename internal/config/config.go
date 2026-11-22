// Package config loads the project's settings from .db-agent/config.env and
// DB_AGENT_* environment variables (environment wins).
//
// The app's own .env (e.g. Laravel) is never read or written.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

const (
	DirName    = ".db-agent"
	ConfigFile = "config.env"
	EnvPrefix  = "DB_AGENT_"
)

// Secret hides its value when printed or logged.
type Secret string

func (Secret) String() string   { return "******" }
func (Secret) GoString() string { return "******" }

// Value returns the real secret. Only use it where it is actually needed.
func (s Secret) Value() string { return string(s) }

// Settings for the one database this installation belongs to.
type Settings struct {
	ProjectDir   string
	ProjectName  string
	DBHost       string
	DBPort       int
	DBName       string
	DBUser       string
	DBPassword   Secret
	QueryTimeout time.Duration
	MaxRows      int
}

func (s Settings) StorageDir() string  { return filepath.Join(s.ProjectDir, DirName) }
func (s Settings) AgentDBPath() string { return filepath.Join(s.StorageDir(), "agent.db") }

// ConfigPath is where `db-agent init` saves settings for a project.
func ConfigPath(projectDir string) string {
	return filepath.Join(projectDir, DirName, ConfigFile)
}

// Error is a configuration problem with a message safe to show the user.
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

// Load reads settings for the project in projectDir.
func Load(projectDir string) (Settings, error) {
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		return Settings{}, err
	}
	path := ConfigPath(abs)

	values := map[string]string{}
	if _, statErr := os.Stat(path); statErr == nil {
		fileValues, readErr := godotenv.Read(path)
		if readErr != nil {
			return Settings{}, &Error{fmt.Sprintf("Cannot read %s: %v", path, readErr)}
		}
		values = fileValues
	}
	for _, kv := range os.Environ() {
		key, val, ok := strings.Cut(kv, "=")
		// Empty env vars are ignored so they never wipe a value from the file.
		if ok && val != "" && strings.HasPrefix(strings.ToUpper(key), EnvPrefix) {
			values[strings.ToUpper(key)] = val
		}
	}

	get := func(key, fallback string) string {
		if v := strings.TrimSpace(values[EnvPrefix+key]); v != "" {
			return v
		}
		return fallback
	}

	s := Settings{
		ProjectDir:  abs,
		ProjectName: get("PROJECT_NAME", "My Project"),
		DBHost:      get("DB_HOST", "127.0.0.1"),
		DBName:      get("DB_NAME", ""),
		DBUser:      get("DB_USER", ""),
		DBPassword:  Secret(values[EnvPrefix+"DB_PASSWORD"]),
	}

	var missing []string
	if s.DBName == "" {
		missing = append(missing, EnvPrefix+"DB_NAME")
	}
	if s.DBUser == "" {
		missing = append(missing, EnvPrefix+"DB_USER")
	}
	if len(missing) > 0 {
		return s, &Error{fmt.Sprintf(
			"Missing settings: %s. Run 'db-agent init' or edit %s.", strings.Join(missing, ", "), path)}
	}

	if s.DBPort, err = intSetting(get("DB_PORT", "3306"), "DB_PORT", 1, 65535); err != nil {
		return s, err
	}
	timeout, err := intSetting(get("QUERY_TIMEOUT_SECONDS", "30"), "QUERY_TIMEOUT_SECONDS", 1, 3600)
	if err != nil {
		return s, err
	}
	s.QueryTimeout = time.Duration(timeout) * time.Second
	if s.MaxRows, err = intSetting(get("MAX_ROWS", "100"), "MAX_ROWS", 1, 10_000); err != nil {
		return s, err
	}
	return s, nil
}

func intSetting(raw, name string, min, max int) (int, error) {
	n, err := strconv.Atoi(raw)
	if err != nil || n < min || n > max {
		return 0, &Error{fmt.Sprintf("Invalid %s%s: must be a number from %d to %d.", EnvPrefix, name, min, max)}
	}
	return n, nil
}

// Save writes settings to .db-agent/config.env (readable by the owner only).
func Save(projectDir string, values map[string]string) (string, error) {
	path := ConfigPath(projectDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	content, err := godotenv.Marshal(values)
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, []byte(content+"\n"), 0o600)
}
