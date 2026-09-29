// Package migrate applies catalog's schema, embedded in the binary.
//
// The schema ships with the code that reads it, so a schema change promotes
// atomically with the image digest — in the same bo-deploy PR as any
// storefront change that depends on it. That is what Phase 6 scenario 5
// (expand-contract) relies on; the Postgres Cluster itself is infrastructure
// and lives in bo-platform.
//
// Migrations run at startup under a session-level advisory lock, so two
// replicas — or v1 and v2 of a canary — never apply the same file twice. Each
// file runs in its own transaction together with its bookkeeping row.
package migrate

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed sql/*.sql
var files embed.FS

// lockKey is the advisory lock every catalog instance takes before migrating:
// "catalog" in ASCII.
const lockKey int64 = 0x636174616c6f67

// Versions lists the embedded migrations in the order they apply.
func Versions() ([]string, error) {
	names, err := fs.Glob(files, "sql/*.sql")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = strings.TrimSuffix(path.Base(n), ".sql")
	}
	return out, nil
}

// Apply runs every migration not yet recorded in schema_migrations and returns
// the versions it applied.
func Apply(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", lockKey); err != nil {
		return nil, fmt.Errorf("advisory lock: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", lockKey)
	}()

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    text        PRIMARY KEY,
		applied_at timestamptz NOT NULL DEFAULT now()
	)`); err != nil {
		return nil, fmt.Errorf("schema_migrations: %w", err)
	}

	versions, err := Versions()
	if err != nil {
		return nil, err
	}
	var applied []string
	for _, v := range versions {
		var done bool
		if err := conn.QueryRow(ctx,
			"SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)", v).Scan(&done); err != nil {
			return applied, fmt.Errorf("%s: %w", v, err)
		}
		if done {
			continue
		}
		body, err := files.ReadFile("sql/" + v + ".sql")
		if err != nil {
			return applied, err
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return applied, fmt.Errorf("%s: begin: %w", v, err)
		}
		// No arguments, so pgx uses the simple protocol and a file may hold
		// several statements.
		if _, err := tx.Exec(ctx, string(body)); err != nil {
			_ = tx.Rollback(ctx)
			return applied, fmt.Errorf("%s: %w", v, err)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", v); err != nil {
			_ = tx.Rollback(ctx)
			return applied, fmt.Errorf("%s: record: %w", v, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return applied, fmt.Errorf("%s: commit: %w", v, err)
		}
		applied = append(applied, v)
	}
	return applied, nil
}
