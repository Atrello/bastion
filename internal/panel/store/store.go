// Package store is the panel's database: one SQLite file, opened with the settings that keep it fast and
// safe for one process, and brought up to date with the migrations in migrations/ when the panel starts.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct {
	DB *sql.DB
}

// Open opens (or creates) the database in dir and runs any migrations it hasn't had yet.
func Open(ctx context.Context, dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return open(ctx, "file:"+filepath.Join(dir, "bastion.db"))
}

// OpenMemory is an empty database that lives only as long as the process, for tests.
func OpenMemory(ctx context.Context) (*Store, error) {
	return open(ctx, "file::memory:")
}

func open(ctx context.Context, dsn string) (*Store, error) {
	// WAL lets reads carry on during a write; foreign keys are off in SQLite unless asked for; the busy
	// timeout waits for a lock instead of failing at once.
	db, err := sql.Open("sqlite", dsn+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	// One connection: SQLite allows one writer at a time anyway, and an in-memory database is per
	// connection.
	db.SetMaxOpenConns(1)
	s := &Store{DB: db}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.DB.Close() }

// migrate runs the numbered .sql files in migrations/ that haven't run yet, each in its own transaction.
func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.DB.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		return err
	}
	var done int
	if err := s.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&done); err != nil {
		return err
	}
	files, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(files)
	for _, file := range files {
		version, err := strconv.Atoi(strings.SplitN(filepath.Base(file), "_", 2)[0])
		if err != nil {
			return fmt.Errorf("migration %s isn't numbered", file)
		}
		if version <= done {
			continue
		}
		body, _ := migrations.ReadFile(file)
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", file, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES (?)`, version); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
