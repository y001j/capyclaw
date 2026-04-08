package pond

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"CapyClaw/internal/shared/config"
)

// DB wraps a PostgreSQL connection pool.
type DB struct {
	Pool *pgxpool.Pool
}

// New creates a new database connection pool.
func New(ctx context.Context, cfg config.PostgresConfig) (*DB, error) {
	connStr := fmt.Sprintf(
		"host=%s port=%d dbname=%s user=%s password=%s sslmode=%s",
		cfg.Host, cfg.Port, cfg.Database, cfg.Username, cfg.Password, cfg.SSLMode,
	)

	poolCfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("parsing pool config: %w", err)
	}

	poolCfg.MaxConns = int32(cfg.Pool.MaxConns)
	poolCfg.MinConns = int32(cfg.Pool.MinConns)

	if cfg.Pool.MaxConnLifetime != "" {
		d, err := time.ParseDuration(cfg.Pool.MaxConnLifetime)
		if err == nil {
			poolCfg.MaxConnLifetime = d
		}
	}

	if cfg.Pool.MaxConnIdleTime != "" {
		d, err := time.ParseDuration(cfg.Pool.MaxConnIdleTime)
		if err == nil {
			poolCfg.MaxConnIdleTime = d
		}
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("creating connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	slog.Info("database connection pool established",
		"host", cfg.Host,
		"database", cfg.Database,
		"max_conns", cfg.Pool.MaxConns,
	)

	return &DB{Pool: pool}, nil
}

// Close closes the database connection pool.
func (db *DB) Close() {
	db.Pool.Close()
}

// SetTenantContext sets the tenant_id for row-level security on the connection.
func (db *DB) SetTenantContext(ctx context.Context, tenantID string) error {
	_, err := db.Pool.Exec(ctx, "SET app.tenant_id = $1", tenantID)
	return err
}

// AcquireWithTenant acquires a connection with tenant context set for RLS.
func (db *DB) AcquireWithTenant(ctx context.Context, tenantID string) (*pgxpool.Conn, error) {
	conn, err := db.Pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquiring connection: %w", err)
	}
	_, err = conn.Exec(ctx, "SET app.tenant_id = $1", tenantID)
	if err != nil {
		conn.Release()
		return nil, fmt.Errorf("setting tenant context: %w", err)
	}
	return conn, nil
}
