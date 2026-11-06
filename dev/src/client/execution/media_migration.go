package execution

import (
	"context"
	_ "embed"
)

//go:embed schema-003-media.sql
var mediaSchema string

// Called only by Open under the original node OS lock, before exposing Store.
// This additive migration keeps foreign keys enabled throughout.
func (s *Store) migrateMedia(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ErrStorage
	}
	defer tx.Rollback()
	var version int
	if tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version) != nil {
		return ErrStorage
	}
	if version == 3 {
		return nil
	}
	if version != 2 {
		return ErrStorage
	}
	actual, err := schemaDigest(ctx, tx)
	if err != nil {
		return ErrStorage
	}
	for k, want := range map[string]string{"profile": Profile, "binding_hash": bindingHash(s.binding), "schema_source_hash": digest([]byte(schema)), "rental_schema_source_hash": digest([]byte(rentalSchema)), "schema_live_hash": actual} {
		var got string
		if tx.QueryRowContext(ctx, "SELECT value FROM metadata WHERE name=?", k).Scan(&got) != nil || got != want {
			return ErrStorage
		}
	}
	if _, err = tx.ExecContext(ctx, mediaSchema); err != nil {
		return ErrStorage
	}
	if foreignKeysValid(ctx, tx) != nil {
		return ErrStorage
	}
	live, err := schemaDigest(ctx, tx)
	if err != nil {
		return ErrStorage
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO metadata(name,value) VALUES('media_schema_source_hash',?)", digest([]byte(mediaSchema))); err != nil {
		return ErrStorage
	}
	if _, err = tx.ExecContext(ctx, "UPDATE metadata SET value=? WHERE name='schema_live_hash'", live); err != nil {
		return ErrStorage
	}
	if _, err = tx.ExecContext(ctx, "PRAGMA user_version=3"); err != nil {
		return ErrStorage
	}
	if tx.Commit() != nil {
		return ErrStorage
	}
	return nil
}
