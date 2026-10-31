package node

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"p11.local/compute/client/execution"
	"p11.local/compute/client/protocol/devicecontrol"
	"p11.local/compute/client/supervisor"
	"p11.local/compute/shared/strictjson"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

//go:embed host-schema.sql
var hostSchema string

const hostFile = "execution-host.db"
const hostApplicationID = 0x50313148
const hostConfigLimit = 131072
const hostCommandLimit = 65536

// Host owns a separate authenticated metadata journal under the node OS lock.
// The parent Store must outlive it. Start it before the control loop, then use
// only Host methods concurrently; they never mutate the control journal.
type Host struct {
	node           *Store
	db             *sql.DB
	key            []byte
	binding        string
	device, seller string
	config         HostConfig
	client         *devicecontrol.Client
	ledger         *execution.Store
	gate           chan struct{}
	mu             sync.Mutex
	closed         bool
	done           chan struct{}
	tickets        map[string]*PreparedExecution
	poisoned       atomic.Bool
	busy           map[string]context.CancelFunc
	workers        sync.WaitGroup
	watchdogs      map[*supervisor.Supervisor]bool
	nativeSession  *NativeSession
}
type hostCommand struct {
	ID       string                   `json:"command_id"`
	Claim    devicecontrol.LeaseClaim `json:"original_claim"`
	Phase    string                   `json:"phase"`
	Lease    json.RawMessage          `json:"lease,omitempty"`
	Manifest json.RawMessage          `json:"manifest,omitempty"`
}
type HostCommandStatus struct {
	ID    string `json:"command_id"`
	Phase string `json:"phase"`
}
type HostStatus struct {
	Profile          string              `json:"profile_id"`
	Commands         []HostCommandStatus `json:"commands"`
	ApprovedOrders   int                 `json:"approved_order_rules"`
	ExecutionEnabled bool                `json:"execution_enabled"`
}

func hostDigest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func (s *Store) hostBinding() string {
	return hostDigest([]byte(HostProfile + "\n" + s.s.Installation + "\n" + s.s.ExecutionID + "\n" + s.s.HostID + "\n" + s.s.DeviceID + "\n" + s.s.SellerID + "\n" + s.s.Config.Base + "\n"))
}
func (s *Store) checkHostFiles(missing bool) error {
	for _, name := range []string{hostFile, hostFile + "-wal", hostFile + "-shm", hostFile + "-journal"} {
		if s.checkFile(name, missing || name != hostFile) != nil {
			return ErrHost
		}
	}
	if info, e := s.root.Lstat(hostFile); e == nil && info.Size() > 64<<20 {
		return ErrHost
	}
	return nil
}

// InitHost persists its random anchor in the OS-protected node journal before
// creating any host DB. Interrupted/missing state is never silently recreated.
// Rules/pins are explicit local input, never fetched from a task response.
func (s *Store) InitHost(ctx context.Context, c HostConfig) (*Host, error) {
	if s.root == nil || s.poisoned || s.host != nil || s.s.HostID != "" || s.s.ExecutionID == "" || s.s.Pending != nil || s.s.RemoteState == "REVOKED" || c.validate(s.s.DeviceID, s.s.SellerID) != nil {
		return nil, ErrHostPolicy
	}
	for _, name := range []string{hostFile, hostFile + "-wal", hostFile + "-shm", hostFile + "-journal"} {
		if _, e := s.root.Lstat(name); !errors.Is(e, os.ErrNotExist) {
			return nil, ErrHost
		}
	}
	id, e := randomHex(32)
	if e != nil {
		return nil, ErrHost
	}
	s.s.HostID = id
	if s.save() != nil {
		return nil, ErrHost
	}
	f, e := createPrivateFile(s, hostFile)
	if e != nil {
		return nil, ErrHost
	}
	e = f.Sync()
	ce := f.Close()
	if e != nil || ce != nil || s.checkHostFiles(false) != nil || syncDirectory(s.path) != nil {
		return nil, ErrHost
	}
	return s.openHost(ctx, &c)
}
func (s *Store) OpenHost(ctx context.Context) (*Host, error) { return s.openHost(ctx, nil) }
func (s *Store) openHost(ctx context.Context, create *HostConfig) (*Host, error) {
	if s.root == nil || s.poisoned || s.s.HostID == "" || s.s.ExecutionID == "" || s.checkHostFiles(false) != nil {
		return nil, ErrHost
	}
	if s.host != nil {
		return s.host, nil
	}
	ledger, e := s.OpenExecution(ctx)
	if e != nil {
		return nil, ErrHost
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(filepath.Join(s.path, hostFile))}
	if volume := filepath.VolumeName(s.path); volume != "" {
		if len(volume) != 2 || volume[1] != ':' {
			return nil, ErrHost
		}
		u.Path = "/" + u.Path
	}
	q := u.Query()
	q.Set("mode", "rw")
	q.Set("_txlock", "immediate")
	for _, p := range []string{"busy_timeout(1500)", "foreign_keys(ON)", "trusted_schema(OFF)", "journal_mode(WAL)", "synchronous(FULL)", "temp_store(MEMORY)", "cache_size(-2048)", "mmap_size(0)", "wal_autocheckpoint(64)", "secure_delete(ON)", "max_page_count(16384)"} {
		q.Add("_pragma", p)
	}
	u.RawQuery = q.Encode()
	db, e := sql.Open("sqlite", u.String())
	if e != nil {
		return nil, ErrHost
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	mac := hmac.New(sha256.New, s.master)
	mac.Write([]byte("P11_HOST_METADATA_KEY_1\n" + s.hostBinding()))
	h := &Host{node: s, db: db, key: mac.Sum(nil), binding: s.hostBinding(), device: s.s.DeviceID, seller: s.s.SellerID, ledger: ledger, gate: make(chan struct{}, 1), busy: map[string]context.CancelFunc{}, watchdogs: map[*supervisor.Supervisor]bool{}, done: make(chan struct{}), tickets: map[string]*PreparedExecution{}}
	if e = h.initialize(ctx, create); e != nil {
		db.Close()
		clear(h.key)
		return nil, ErrHost
	}
	if h.config.validate(s.s.DeviceID, s.s.SellerID) != nil {
		db.Close()
		clear(h.key)
		return nil, ErrHost
	}
	if e = h.transaction(ctx, func(tx *sql.Tx) error { return h.visitCommands(ctx, tx, func(hostCommand) error { return nil }) }); e != nil {
		db.Close()
		clear(h.key)
		return nil, ErrHost
	}
	for _, r := range h.config.Rules {
		if ledger.RegisterResource(ctx, r.Approval.Resource, r.PhysicalDigest) != nil {
			db.Close()
			clear(h.key)
			return nil, ErrHost
		}
	}
	h.client, e = s.client()
	if e != nil {
		db.Close()
		clear(h.key)
		return nil, ErrHost
	}
	s.host = h
	return h, nil
}
func hostSchemaDigest(ctx context.Context, tx *sql.Tx) (string, error) {
	rows, e := tx.QueryContext(ctx, `SELECT type,name,tbl_name,coalesce(sql,'') FROM sqlite_master WHERE name NOT LIKE 'sqlite_%' ORDER BY type,name`)
	if e != nil {
		return "", ErrHost
	}
	defer rows.Close()
	var fields []string
	for rows.Next() {
		var a, b, c, d string
		if rows.Scan(&a, &b, &c, &d) != nil {
			return "", ErrHost
		}
		fields = append(fields, a, b, c, d)
	}
	if rows.Err() != nil {
		return "", ErrHost
	}
	return hostDigest([]byte(strings.Join(fields, "\x00"))), nil
}
func (h *Host) seal(label string, plain []byte) ([]byte, error) {
	a, e := makeCipher(h.key)
	if e != nil {
		return nil, ErrHost
	}
	nonce := make([]byte, a.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return nil, ErrHost
	}
	return a.Seal(nonce, nonce, plain, []byte(HostProfile+"\n"+h.binding+"\n"+label)), nil
}
func (h *Host) unseal(label string, sealed []byte) ([]byte, error) {
	a, e := makeCipher(h.key)
	if e != nil || len(sealed) < 28 {
		return nil, ErrHost
	}
	plain, e := a.Open(nil, sealed[:a.NonceSize()], sealed[a.NonceSize():], []byte(HostProfile+"\n"+h.binding+"\n"+label))
	if e != nil {
		return nil, ErrHost
	}
	return plain, nil
}
func (h *Host) initialize(ctx context.Context, create *HostConfig) error {
	tx, e := h.db.BeginTx(ctx, nil)
	if e != nil {
		return ErrHost
	}
	defer tx.Rollback()
	if create != nil {
		if _, e = tx.ExecContext(ctx, hostSchema); e != nil {
			return ErrHost
		}
		if _, e = tx.ExecContext(ctx, `PRAGMA application_id=1345401160; PRAGMA user_version=1`); e != nil {
			return ErrHost
		}
		live, e := hostSchemaDigest(ctx, tx)
		if e != nil {
			return e
		}
		for k, v := range map[string]string{"profile": HostProfile, "binding_hash": h.binding, "schema_source_hash": hostDigest([]byte(hostSchema)), "schema_live_hash": live} {
			if _, e = tx.ExecContext(ctx, `INSERT INTO metadata(name,value) VALUES(?,?)`, k, v); e != nil {
				return ErrHost
			}
		}
		plain := canonicalHostJSON(*create)
		defer clear(plain)
		sealed, e := h.seal("CONFIG", plain)
		if e != nil || len(sealed) > hostConfigLimit {
			return ErrHostPolicy
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO configuration(id,sealed) VALUES(1,?)`, sealed); e != nil {
			return ErrHost
		}
	}
	var app, version int
	if tx.QueryRowContext(ctx, `PRAGMA application_id`).Scan(&app) != nil || app != hostApplicationID || tx.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version) != nil || version != 1 {
		return ErrHost
	}
	for pragma, want := range map[string]int{"foreign_keys": 1, "trusted_schema": 0, "synchronous": 2, "cache_size": -2048, "mmap_size": 0, "max_page_count": 16384} {
		var got int
		if tx.QueryRowContext(ctx, "PRAGMA "+pragma).Scan(&got) != nil || got != want {
			return ErrHost
		}
	}
	var journal, integrity string
	if tx.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&journal) != nil || journal != "wal" || tx.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&integrity) != nil || integrity != "ok" {
		return ErrHost
	}
	live, e := hostSchemaDigest(ctx, tx)
	if e != nil {
		return e
	}
	for k, want := range map[string]string{"profile": HostProfile, "binding_hash": h.binding, "schema_source_hash": hostDigest([]byte(hostSchema)), "schema_live_hash": live} {
		var got string
		if tx.QueryRowContext(ctx, `SELECT value FROM metadata WHERE name=?`, k).Scan(&got) != nil || got != want {
			return ErrHost
		}
	}
	var sealed []byte
	if tx.QueryRowContext(ctx, `SELECT sealed FROM configuration WHERE id=1 AND length(sealed) BETWEEN 28 AND 131072`).Scan(&sealed) != nil {
		return ErrHost
	}
	plain, e := h.unseal("CONFIG", sealed)
	if e != nil {
		return e
	}
	defer clear(plain)
	if strictjson.Decode(plain, &h.config, hostConfigLimit) != nil {
		return ErrHost
	}
	return tx.Commit()
}
func (h *Host) transaction(ctx context.Context, fn func(*sql.Tx) error) error {
	select {
	case h.gate <- struct{}{}:
	case <-ctx.Done():
		return ErrHost
	}
	defer func() { <-h.gate }()
	h.mu.Lock()
	closed := h.closed
	h.mu.Unlock()
	if closed || h.poisoned.Load() || h.db == nil || ctx.Err() != nil {
		return ErrHost
	}
	tx, e := h.db.BeginTx(ctx, nil)
	if e != nil {
		h.fail()
		return ErrHost
	}
	defer tx.Rollback()
	if e = fn(tx); e != nil {
		if errors.Is(e, ErrHost) {
			h.fail()
		}
		return e
	}
	if tx.Commit() != nil {
		h.fail()
		return ErrHost
	}
	return nil
}
func (h *Host) fail() {
	h.poisoned.Store(true)
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, cancel := range h.busy {
		cancel()
	}
	for watch := range h.watchdogs {
		watch.Drain()
	}
}
func (h *Host) Close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		<-h.done
		return
	}
	h.closed = true
	var tickets []*PreparedExecution
	for _, p := range h.tickets {
		tickets = append(tickets, p)
	}
	for _, cancel := range h.busy {
		cancel()
	}
	for watch := range h.watchdogs {
		watch.Drain()
	}
	h.mu.Unlock()
	for _, p := range tickets {
		p.Close()
	}
	h.workers.Wait()
	h.mu.Lock()
	native := h.nativeSession
	h.mu.Unlock()
	if native != nil {
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if native.Close(shutdown) != nil {
			h.poisoned.Store(true)
		}
		cancel()
	}
	h.gate <- struct{}{}
	if h.client != nil {
		h.client.Close()
	}
	if h.db != nil {
		h.db.Close()
		h.db = nil
	}
	clear(h.key)
	<-h.gate
	close(h.done)
}
