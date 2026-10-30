package persistence

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"github.com/jackc/pgx/v5"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Migrate applies immutable, forward-only migrations under a transaction-level
// advisory lock. Metadata is separate from the sixty domain tables.
func (d *DB) Migrate(ctx context.Context) error {
	return d.MigrateFS(ctx, migrations)
}
func (d *DB) MigrateFS(ctx context.Context, source fs.FS) error {
	return d.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		// Native first-run DDL can outlive the ordinary 3s business lock budget.
		// Only migration admission gets a bounded 30s wait, still limited by ctx.
		if _, err := tx.Exec(ctx, `SELECT set_config('lock_timeout','30000',true),set_config('statement_timeout','45000',true)`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7410112026)`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT set_config('lock_timeout','3000',true),set_config('statement_timeout','15000',true)`); err != nil {
			return err
		}
		var server int
		if err := tx.QueryRow(ctx, `SELECT current_setting('server_version_num')::integer`).Scan(&server); err != nil {
			return err
		}
		if server < 170000 || server >= 180000 {
			return fmt.Errorf("PostgreSQL 17 required, received server major %d", server/10000)
		}
		if _, err := tx.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS p11_meta;
CREATE TABLE IF NOT EXISTS p11_meta.schema_migrations(version integer PRIMARY KEY,name text NOT NULL,sha256 text NOT NULL,applied_at timestamptz NOT NULL DEFAULT clock_timestamp())`); err != nil {
			return err
		}
		entries, err := fs.Glob(source, "migrations/*.sql")
		if err != nil {
			return err
		}
		sort.Strings(entries)
		known := map[int]bool{}
		for _, name := range entries {
			base := strings.TrimPrefix(name, "migrations/")
			parts := strings.SplitN(base, "_", 2)
			version, err := strconv.Atoi(parts[0])
			if err != nil || version < 1 || len(parts) != 2 || known[version] {
				return fmt.Errorf("invalid migration name: %s", base)
			}
			known[version] = true
			body, err := fs.ReadFile(source, name)
			if err != nil {
				return err
			}
			hash := sha256.Sum256(body)
			digest := hex.EncodeToString(hash[:])
			var existing string
			err = tx.QueryRow(ctx, `SELECT sha256 FROM p11_meta.schema_migrations WHERE version=$1`, version).Scan(&existing)
			if err == nil {
				if existing != digest {
					return fmt.Errorf("migration checksum drift: %s", base)
				}
				continue
			}
			if err != pgx.ErrNoRows {
				return err
			}
			if _, err = tx.Exec(ctx, string(body)); err != nil {
				return fmt.Errorf("migration %s: %w", base, err)
			}
			if _, err = tx.Exec(ctx, `INSERT INTO p11_meta.schema_migrations(version,name,sha256) VALUES($1,$2,$3)`, version, base, digest); err != nil {
				return err
			}
		}
		rows, err := tx.Query(ctx, `SELECT version FROM p11_meta.schema_migrations`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v int
			if err = rows.Scan(&v); err != nil {
				return err
			}
			if !known[v] {
				return fmt.Errorf("database contains unknown migration version %d", v)
			}
		}
		return rows.Err()
	})
}
