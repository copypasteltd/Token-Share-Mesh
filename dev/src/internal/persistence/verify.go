package persistence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
)

// VerifyMigrations is read-only startup validation against every embedded SQL
// byte, not a stale hard-coded maximum version. It never repairs schema drift.
func (d *DB) VerifyMigrations(ctx context.Context) error {
	paths, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	var count int
	if err = d.Pool.QueryRow(ctx, "SELECT count(*) FROM p11_meta.schema_migrations").Scan(&count); err != nil {
		return err
	}
	if count != len(paths) {
		return fmt.Errorf("migration set does not match this binary")
	}
	for _, path := range paths {
		name := strings.TrimPrefix(path, "migrations/")
		version, e := strconv.Atoi(strings.SplitN(name, "_", 2)[0])
		if e != nil {
			return e
		}
		body, e := migrations.ReadFile(path)
		if e != nil {
			return e
		}
		hash := sha256.Sum256(body)
		var actualName, actualHash string
		if e = d.Pool.QueryRow(ctx, `SELECT name,sha256 FROM p11_meta.schema_migrations WHERE version=$1`, version).Scan(&actualName, &actualHash); e != nil {
			return e
		}
		if actualName != name || actualHash != hex.EncodeToString(hash[:]) {
			return fmt.Errorf("migration name or checksum mismatch")
		}
	}
	return nil
}
