// Package database connects to the configured MySQL database and reads its structure.
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/frahjokhio/db-agent/internal/config"
)

// Error is a database problem with a message safe to show (never contains the password).
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

// SafeError converts driver errors into a safe *Error.
func SafeError(err error) error {
	if err == nil {
		return nil
	}
	var safe *Error
	if errors.As(err, &safe) {
		return safe
	}
	var my *mysql.MySQLError
	if errors.As(err, &my) {
		return &Error{fmt.Sprintf("(%d) %s", my.Number, my.Message)}
	}
	return &Error{err.Error()}
}

// Open prepares a connection pool. It does not connect until first use.
func Open(s config.Settings) (*sql.DB, error) {
	cfg := mysql.NewConfig()
	cfg.User = s.DBUser
	cfg.Passwd = s.DBPassword.Value()
	cfg.Net = "tcp"
	cfg.Addr = net.JoinHostPort(s.DBHost, strconv.Itoa(s.DBPort))
	cfg.DBName = s.DBName
	cfg.Timeout = 10 * time.Second
	cfg.ReadTimeout = s.QueryTimeout + 10*time.Second
	cfg.ParseTime = true

	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		return nil, SafeError(err)
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(time.Hour)
	return db, nil
}

// Info describes a successful connection.
type Info struct {
	ServerVersion string
	Database      string
	TableCount    int
}

// Check connects and reads basic info.
func Check(ctx context.Context, db *sql.DB) (Info, error) {
	var info Info
	err := db.QueryRowContext(ctx, `
		SELECT VERSION(), DATABASE(),
		       (SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE())`,
	).Scan(&info.ServerVersion, &info.Database, &info.TableCount)
	if err != nil {
		return Info{}, SafeError(err)
	}
	return info, nil
}
