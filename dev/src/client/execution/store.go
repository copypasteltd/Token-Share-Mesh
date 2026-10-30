// Package execution implements a bounded native metadata inbox and resource
// gate. The enclosing node must hold its OS process lock and private directory.
// No engine, hardware qualification, funding, rental grant or content execution
// is enabled merely by opening this store.
package execution

import (
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/shared/ids"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

const Profile = "P11_NATIVE_EXECUTION_STORE_1"
const applicationID = 0x50313145
const MaximumResources = 128
const MaximumReceipts = 4096

var (
	ErrStorage  = errors.New("execution metadata unavailable; retain state and stop owned work")
	ErrBusy     = errors.New("physical resource is occupied or quarantined")
	ErrFence    = errors.New("execution owner, generation or command does not match")
	ErrQuery    = errors.New("submission outcome requires an original-request backend query")
	ErrRecovery = errors.New("execution authority is closed; query, stop and clean the original owner")
	ErrEvidence = errors.New("stopped-work and complete cleanup evidence required")
	ErrCapacity = errors.New("native metadata capacity reached; admission closed")
)

type Binding struct {
	Installation, LedgerID, Device, Seller string
}
type Stamp struct {
	Wall    time.Time
	Elapsed time.Duration
}
type Clock interface{ Sample() Stamp }
type nativeClock struct{ origin time.Time }

func (c nativeClock) Sample() Stamp { return Stamp{time.Now().UTC(), time.Since(c.origin)} }

// Hold is an opaque compare-and-set owner. A platform resource generation and
// a local owner generation are separate; local use never resets remote fences.
type Hold struct{ resource, owner, local, mode string }

func (h Hold) Resource() string        { return h.resource }
func (h Hold) Owner() string           { return h.owner }
func (h Hold) LocalGeneration() string { return h.local }

type guard struct {
	expires  time.Time
	until    time.Duration
	deadline time.Time
}
type Store struct {
	gate        chan struct{}
	db          *sql.DB
	binding     Binding
	clock       Clock
	guards      map[string]guard
	lastElapsed time.Duration
	poisoned    bool
	clockUnsafe bool
	sampledAt   time.Time
}
type ResourceStatus struct {
	Resource   string `json:"resource_id"`
	Phase      string `json:"phase"`
	Mode       string `json:"owner_kind,omitempty"`
	RemoteHigh string `json:"resource_generation_high_water"`
	LocalHigh  string `json:"local_owner_generation"`
	Recovery   bool   `json:"recovery_required"`
}
type Status struct {
	Profile          string           `json:"profile"`
	Resources        []ResourceStatus `json:"resources"`
	Receipts         int              `json:"receipt_count"`
	ExecutionEnabled bool             `json:"execution_enabled"`
}

func bindingHash(b Binding) string {
	h := sha256.Sum256([]byte(Profile + "\n" + b.Installation + "\n" + b.LedgerID + "\n" + b.Device + "\n" + b.Seller + "\n"))
	return hex.EncodeToString(h[:])
}
func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// Open requires a pre-created regular file in the node's private directory.
// mode=rw never recreates a lost database. create is only for the explicit
// initialization transaction anchored first in the encrypted node journal.
func Open(ctx context.Context, path string, b Binding, create bool, clock Clock) (*Store, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || !executionlease.Hash(b.Installation) || !executionlease.Hash(b.LedgerID) || !ids.Valid(b.Device) || !ids.Valid(b.Seller) {
		return nil, ErrStorage
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 64*1024*1024 || create && info.Size() != 0 {
		return nil, ErrStorage
	}
	uriPath := filepath.ToSlash(path)
	if volume := filepath.VolumeName(path); volume != "" {
		// file:///E:/... is a local Windows file. file://E:/... instead
		// interprets E: as a URI authority; UNC/network databases are refused.
		if len(volume) != 2 || volume[1] != ':' {
			return nil, ErrStorage
		}
		uriPath = "/" + uriPath
	}
	u := url.URL{Scheme: "file", Path: uriPath}
	q := u.Query()
	q.Set("mode", "rw")
	q.Set("_txlock", "immediate")
	for _, p := range []string{"busy_timeout(1500)", "foreign_keys(ON)", "trusted_schema(OFF)", "journal_mode(WAL)", "synchronous(FULL)", "temp_store(MEMORY)", "cache_size(-2048)", "mmap_size(0)", "wal_autocheckpoint(64)", "secure_delete(ON)", "max_page_count(16384)"} {
		q.Add("_pragma", p)
	}
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, ErrStorage
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if clock == nil {
		clock = nativeClock{time.Now()}
	}
	s := &Store{gate: make(chan struct{}, 1), db: db, binding: b, clock: clock, guards: map[string]guard{}}
	if err = s.initialize(ctx, create); err != nil {
		db.Close()
		return nil, ErrStorage
	}
	return s, nil
}

func schemaDigest(ctx context.Context, tx *sql.Tx) (string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT type,name,tbl_name,coalesce(sql,'') FROM sqlite_master WHERE name NOT LIKE 'sqlite_%' ORDER BY type,name`)
	if err != nil {
		return "", ErrStorage
	}
	defer rows.Close()
	var fields []string
	for rows.Next() {
		var kind, name, table, ddl string
		if rows.Scan(&kind, &name, &table, &ddl) != nil {
			return "", ErrStorage
		}
		fields = append(fields, kind, name, table, ddl)
	}
	if rows.Err() != nil {
		return "", ErrStorage
	}
	return digest([]byte(strings.Join(fields, "\x00"))), nil
}
func (s *Store) initialize(ctx context.Context, create bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ErrStorage
	}
	defer tx.Rollback()
	for pragma, want := range map[string]int{"page_size": 4096, "foreign_keys": 1, "trusted_schema": 0, "synchronous": 2, "cache_size": -2048, "mmap_size": 0, "max_page_count": 16384} {
		var got int
		if tx.QueryRowContext(ctx, "PRAGMA "+pragma).Scan(&got) != nil || got != want {
			return ErrStorage
		}
	}
	var journal string
	if tx.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journal) != nil || journal != "wal" {
		return ErrStorage
	}
	if create {
		if _, err = tx.ExecContext(ctx, schema); err != nil {
			return ErrStorage
		}
		if _, err = tx.ExecContext(ctx, `PRAGMA application_id=1345401157; PRAGMA user_version=1`); err != nil {
			return ErrStorage
		}
		actual, e := schemaDigest(ctx, tx)
		if e != nil {
			return ErrStorage
		}
		for k, v := range map[string]string{"profile": Profile, "binding_hash": bindingHash(s.binding), "schema_source_hash": digest([]byte(schema)), "schema_live_hash": actual, "last_wall_ns": "0"} {
			if _, err = tx.ExecContext(ctx, `INSERT INTO metadata(name,value) VALUES(?,?)`, k, v); err != nil {
				return ErrStorage
			}
		}
	}
	var app, version int
	if tx.QueryRowContext(ctx, `PRAGMA application_id`).Scan(&app) != nil || app != applicationID || tx.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version) != nil || version != 1 {
		return ErrStorage
	}
	actual, err := schemaDigest(ctx, tx)
	if err != nil {
		return ErrStorage
	}
	for k, want := range map[string]string{"profile": Profile, "binding_hash": bindingHash(s.binding), "schema_source_hash": digest([]byte(schema)), "schema_live_hash": actual} {
		var got string
		if tx.QueryRowContext(ctx, `SELECT value FROM metadata WHERE name=?`, k).Scan(&got) != nil || got != want {
			return ErrStorage
		}
	}
	var integrity string
	if tx.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&integrity) != nil || integrity != "ok" {
		return ErrStorage
	}
	fk, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return ErrStorage
	}
	invalid := fk.Next() || fk.Err() != nil
	fk.Close()
	if invalid {
		return ErrStorage
	}
	// Every reopen loses in-memory execution authority, even after clean exit.
	// A previous disk ACTIVE is never enough to resume or release the resource.
	if _, err = tx.ExecContext(ctx, `UPDATE resources SET phase='QUARANTINED',stopped=0 WHERE owner_id<>''; UPDATE inbox SET phase='QUARANTINED' WHERE phase<>'CLEANED'`); err != nil {
		return ErrStorage
	}
	return tx.Commit()
}

// transaction commits safe stop/quarantine transitions even when a later
// admission check returns a domain refusal. Any SQL/commit failure poisons the
// whole gate. Callers never receive raw SQL, filesystem or input diagnostics.
func (s *Store) transaction(ctx context.Context, action func(*sql.Tx, Stamp) error) error {
	if s.gate == nil {
		return ErrStorage
	}
	select {
	case s.gate <- struct{}{}:
	case <-ctx.Done():
		return ErrStorage
	}
	defer func() { <-s.gate }()
	// A cancelled waiter has not touched SQL and cannot make its outcome
	// ambiguous. Preserve the ledger for original-owner recovery. SQL/commit
	// errors after BeginTx still poison the gate conservatively.
	if ctx.Err() != nil {
		return ErrStorage
	}
	if s.db == nil || s.poisoned {
		return ErrStorage
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		s.poisoned = true
		return ErrStorage
	}
	defer tx.Rollback()
	s.sampledAt = time.Now()
	stamp := s.clock.Sample()
	err := s.advance(ctx, tx, stamp)
	if err == nil {
		err = action(tx, stamp)
	}
	if errors.Is(err, ErrStorage) {
		s.poisoned = true
		return ErrStorage
	}
	if tx.Commit() != nil {
		s.poisoned = true
		return ErrStorage
	}
	s.lastElapsed = stamp.Elapsed
	return err
}
func (s *Store) advance(ctx context.Context, tx *sql.Tx, stamp Stamp) error {
	var raw string
	if tx.QueryRowContext(ctx, `SELECT value FROM metadata WHERE name='last_wall_ns'`).Scan(&raw) != nil {
		return ErrStorage
	}
	last, e := strconv.ParseInt(raw, 10, 64)
	if e != nil || last < 0 || strconv.FormatInt(last, 10) != raw || stamp.Wall.Year() < 2000 || stamp.Wall.Year() > 2100 {
		return ErrStorage
	}
	rollback := stamp.Wall.UnixNano() < last || stamp.Elapsed < s.lastElapsed || stamp.Elapsed < 0
	if rollback {
		if !s.clockUnsafe {
			if _, e = tx.ExecContext(ctx, `UPDATE resources SET phase='QUARANTINED',stopped=0 WHERE owner_id<>''; UPDATE inbox SET phase='QUARANTINED' WHERE phase<>'CLEANED'`); e != nil {
				return ErrStorage
			}
		}
		clear(s.guards)
		s.clockUnsafe = true
		return nil
	}
	if _, e = tx.ExecContext(ctx, `UPDATE metadata SET value=? WHERE name='last_wall_ns'`, strconv.FormatInt(stamp.Wall.UnixNano(), 10)); e != nil {
		return ErrStorage
	}
	for id, g := range s.guards {
		if !stamp.Wall.Before(g.expires) || stamp.Elapsed >= g.until || !time.Now().Before(g.deadline) {
			if _, e = tx.ExecContext(ctx, `UPDATE resources SET phase='QUARANTINED',stopped=0 WHERE owner_id=? AND mode='JOB'`, id); e != nil {
				return ErrStorage
			}
			if _, e = tx.ExecContext(ctx, `UPDATE inbox SET phase='QUARANTINED' WHERE lease_id=? AND phase<>'CLEANED'`, id); e != nil {
				return ErrStorage
			}
			delete(s.guards, id)
		}
	}
	return nil
}
func (s *Store) Tick(ctx context.Context) error {
	return s.transaction(ctx, func(*sql.Tx, Stamp) error {
		if s.clockUnsafe {
			return ErrRecovery
		}
		return nil
	})
}
func (s *Store) Close() {
	if s.gate == nil {
		return
	}
	s.gate <- struct{}{}
	defer func() { <-s.gate }()
	if s.db != nil {
		s.db.Close()
		s.db = nil
	}
	clear(s.guards)
}

func (s *Store) RegisterResource(ctx context.Context, id, physicalDigest string) error {
	if !ids.Valid(id) || !executionlease.Hash(physicalDigest) {
		return ErrFence
	}
	return s.transaction(ctx, func(tx *sql.Tx, _ Stamp) error {
		var existing string
		e := tx.QueryRowContext(ctx, `SELECT physical_digest FROM resources WHERE resource_id=?`, id).Scan(&existing)
		if e == nil {
			if existing != physicalDigest {
				return ErrFence
			}
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return ErrStorage
		}
		var count int
		if tx.QueryRowContext(ctx, `SELECT count(*) FROM resources WHERE physical_digest=?`, physicalDigest).Scan(&count) != nil {
			return ErrStorage
		}
		if count != 0 {
			return ErrFence
		}
		if tx.QueryRowContext(ctx, `SELECT count(*) FROM resources`).Scan(&count) != nil {
			return ErrStorage
		}
		if count >= MaximumResources {
			return ErrCapacity
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO resources(resource_id,physical_digest) VALUES(?,?)`, id, physicalDigest); e != nil {
			return ErrStorage
		}
		return nil
	})
}

func (s *Store) Snapshot(ctx context.Context) (Status, error) {
	result := Status{Profile: Profile, Resources: []ResourceStatus{}}
	err := s.transaction(ctx, func(tx *sql.Tx, _ Stamp) error {
		if tx.QueryRowContext(ctx, `SELECT count(*) FROM receipts`).Scan(&result.Receipts) != nil {
			return ErrStorage
		}
		rows, e := tx.QueryContext(ctx, `SELECT resource_id,phase,mode,remote_high,local_high FROM resources ORDER BY resource_id`)
		if e != nil {
			return ErrStorage
		}
		defer rows.Close()
		for rows.Next() {
			var r ResourceStatus
			if rows.Scan(&r.Resource, &r.Phase, &r.Mode, &r.RemoteHigh, &r.LocalHigh) != nil {
				return ErrStorage
			}
			if _, e = executionlease.Counter(r.RemoteHigh); e != nil {
				return ErrStorage
			}
			if _, e = executionlease.Counter(r.LocalHigh); e != nil {
				return ErrStorage
			}
			r.Recovery = r.Phase == "QUARANTINED"
			result.Resources = append(result.Resources, r)
		}
		if rows.Err() != nil {
			return ErrStorage
		}
		return nil
	})
	return result, err
}
