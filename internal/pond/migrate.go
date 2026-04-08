package pond

import (
	"context"
	"embed"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Migrator handles database schema migrations.
type Migrator struct {
	pool *pgxpool.Pool
}

// NewMigrator creates a new migration runner.
func NewMigrator(pool *pgxpool.Pool) *Migrator {
	return &Migrator{pool: pool}
}

// Up runs all pending migrations.
func (m *Migrator) Up(ctx context.Context) error {
	if err := m.ensureMigrationsTable(ctx); err != nil {
		return err
	}

	applied, err := m.getAppliedMigrations(ctx)
	if err != nil {
		return err
	}

	files, err := m.getMigrationFiles("up")
	if err != nil {
		return err
	}

	for _, f := range files {
		if applied[f.version] {
			continue
		}

		slog.Info("applying migration", "version", f.version, "file", f.name)

		sql, err := migrationFS.ReadFile("migrations/" + f.name)
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", f.name, err)
		}

		tx, err := m.pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("beginning transaction: %w", err)
		}

		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("executing migration %s: %w", f.name, err)
		}

		if _, err := tx.Exec(ctx,
			"INSERT INTO schema_migrations (version) VALUES ($1)", f.version); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("recording migration %s: %w", f.name, err)
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("committing migration %s: %w", f.name, err)
		}

		slog.Info("migration applied", "version", f.version)
	}

	return nil
}

// Down rolls back the last migration.
func (m *Migrator) Down(ctx context.Context) error {
	// TODO: implement rollback
	return nil
}

type migrationFile struct {
	version string
	name    string
}

func (m *Migrator) ensureMigrationsTable(ctx context.Context) error {
	_, err := m.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version VARCHAR(255) PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	return err
}

func (m *Migrator) getAppliedMigrations(ctx context.Context) (map[string]bool, error) {
	rows, err := m.pool.Query(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		applied[v] = true
	}
	return applied, nil
}

func (m *Migrator) getMigrationFiles(direction string) ([]migrationFile, error) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, err
	}

	var files []migrationFile
	suffix := "." + direction + ".sql"
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), suffix) {
			version := strings.Split(e.Name(), "_")[0]
			files = append(files, migrationFile{
				version: version,
				name:    e.Name(),
			})
		}
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].version < files[j].version
	})

	return files, nil
}
