package execution

import (
	"context"
	"database/sql"
	_ "embed"
	"time"
)

//go:embed schema-002-rental.sql
var rentalSchema string

// migrateRental runs only during Open, under the enclosing node OS lock and
// before the Store is returned. The pool has one connection. Never alter the
// published original schema.sql or temporarily disable FK checks on a live gate.
func (s *Store) migrateRental(ctx context.Context) (result error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return ErrStorage
	}
	defer conn.Close()
	var version int
	if conn.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version) != nil {
		return ErrStorage
	}
	if version == 2 || version == 3 {
		return nil
	}
	if version != 1 {
		return ErrStorage
	}
	// SQLite's documented rebuild sequence requires this outside a transaction.
	// Restoration also runs after cancellation/rollback, on this same connection.
	defer func() {
		restore, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var enabled int
		if _, e := conn.ExecContext(restore, "PRAGMA foreign_keys=ON"); e != nil {
			result = ErrStorage
			return
		}
		if conn.QueryRowContext(restore, "PRAGMA foreign_keys").Scan(&enabled) != nil || enabled != 1 {
			result = ErrStorage
		}
	}()
	if _, err = conn.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		return ErrStorage
	}
	var enabled int
	if conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&enabled) != nil || enabled != 0 {
		return ErrStorage
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return ErrStorage
	}
	defer tx.Rollback()
	// Repeat original identity/schema checks in the migration write transaction;
	// a missing/inconsistent journal never becomes an empty replacement ledger.
	actual, err := schemaDigest(ctx, tx)
	if err != nil {
		return ErrStorage
	}
	for k, want := range map[string]string{"profile": Profile, "binding_hash": bindingHash(s.binding), "schema_source_hash": digest([]byte(schema)), "schema_live_hash": actual} {
		var got string
		if tx.QueryRowContext(ctx, "SELECT value FROM metadata WHERE name=?", k).Scan(&got) != nil || got != want {
			return ErrStorage
		}
	}
	if _, err = tx.ExecContext(ctx, rentalSchema); err != nil {
		return ErrStorage
	}
	if foreignKeysValid(ctx, tx) != nil {
		return ErrStorage
	}
	live, err := schemaDigest(ctx, tx)
	if err != nil {
		return ErrStorage
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO metadata(name,value) VALUES('rental_schema_source_hash',?)", digest([]byte(rentalSchema))); err != nil {
		return ErrStorage
	}
	if _, err = tx.ExecContext(ctx, "UPDATE metadata SET value=? WHERE name='schema_live_hash'", live); err != nil {
		return ErrStorage
	}
	if _, err = tx.ExecContext(ctx, "PRAGMA user_version=2"); err != nil {
		return ErrStorage
	}
	if tx.Commit() != nil {
		return ErrStorage
	}
	return nil
}

func foreignKeysValid(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return ErrStorage
	}
	defer rows.Close()
	if rows.Next() || rows.Err() != nil {
		return ErrStorage
	}
	return nil
}

func quarantineRentalTx(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `UPDATE rental_owners SET phase='QUARANTINED' WHERE phase<>'CLEANED'; UPDATE rental_grants SET state='STOPPED' WHERE state='CURRENT'; UPDATE rental_invocations SET phase='QUARANTINED' WHERE phase<>'CLEANED'`); err != nil {
		return ErrStorage
	}
	return nil
}
