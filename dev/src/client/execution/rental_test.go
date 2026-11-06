package execution

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"p11.local/compute/client/protocol/rentalaccess"
	"p11.local/compute/shared/contractspec"
)

// These native SQLite/crypto fixtures are synthetic metadata. No GPU, engine,
// operator readiness, payment, physical stopped-work or cleanup is qualified.
type nativeRentalFixture struct {
	s        *Store
	clock    *testClock
	p        rentalaccess.Prepared
	g        rentalaccess.Verified
	expected rentalaccess.Expected
	signer   *rentalaccess.Signer
	verifier *rentalaccess.Verifier
	metadata rentalaccess.Metadata
}

func nativeRental(t *testing.T, s *Store, c *testClock) nativeRentalFixture {
	t.Helper()
	now := c.stamp.Wall
	m := rentalaccess.Manifest{Profile: rentalaccess.ManifestProfile, Reference: "manifest_rental", Session: "session_rental", Order: "order_rental", Buyer: "buyer_test", Seller: "seller_test", Device: "device_test", Resource: "resource_test", Generation: "1", Capability: "capability_test", Environment: "environment_test", EnvironmentHash: strings.Repeat("b", 64), ContractHash: strings.Repeat("c", 64), Mode: "GPU_RENTAL", Privacy: "STANDARD", Engine: "LLAMA_CPP_NATIVE", Release: "llama.cpp-b11960", ModelHash: strings.Repeat("d", 64), RecipeHash: strings.Repeat("f", 64), Protocol: "openai_nb1", PublicSpec: contractspec.Selection{ModelID: "model_test", DurationMS: 120000, InputTokenLimit: 2048, OutputTokenLimit: 256}}
	raw, _ := json.Marshal(m)
	p, err := rentalaccess.VerifyPreparation(raw, rentalaccess.LocalApproval{Profile: rentalaccess.ApprovalProfile, Manifest: m, ManifestHash: digest(raw), PhysicalHash: strings.Repeat("e", 64), Connection: "connection_test", Issuer: "issuer_test", PrepareUntil: now.Add(time.Minute)}, now)
	if err != nil {
		t.Fatal(err)
	}
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{94}, 32))
	defer clear(key)
	signer, err := rentalaccess.NewSigner("issuer_test", "key_test", key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(signer.Close)
	verifier, err := rentalaccess.NewVerifier(map[string]ed25519.PublicKey{"key_test": key.Public().(ed25519.PublicKey)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(verifier.Close)
	claims := rentalaccess.Claims{Profile: rentalaccess.Profile, Issuer: "issuer_test", Subject: m.Buyer, Audience: m.Device, ID: "grant_rental", Issued: now.Unix(), NotBefore: now.Unix(), Expires: now.Add(2 * time.Minute).Unix(), Order: m.Order, Session: m.Session, Seller: m.Seller, Device: m.Device, Resource: m.Resource, Generation: m.Generation, Terms: "1", TermsHash: strings.Repeat("a", 64), Environment: m.Environment, EnvironmentHash: m.EnvironmentHash, Manifest: m.Reference, ManifestHash: p.Digest(), Scopes: []string{"INVOKE", "CANCEL_SELF"}, HardDeadline: now.Add(2 * time.Minute).Format(time.RFC3339Nano), Mode: "GPU_RENTAL", Privacy: "STANDARD"}
	i, err := signer.Issue(claims, now)
	if err != nil {
		t.Fatal(err)
	}
	defer i.Close()
	metadata := i.Metadata()
	token, err := i.TakeCredential()
	if err != nil {
		t.Fatal(err)
	}
	e := rentalaccess.Expected{TokenDigest: metadata.Digest, Issuer: claims.Issuer, Subject: claims.Subject, Audience: claims.Audience, Grant: claims.ID, Order: claims.Order, Session: claims.Session, Seller: claims.Seller, Device: claims.Device, Resource: claims.Resource, Generation: claims.Generation, Terms: claims.Terms, TermsHash: claims.TermsHash, Environment: claims.Environment, EnvironmentHash: claims.EnvironmentHash, Manifest: claims.Manifest, ManifestHash: claims.ManifestHash, HardDeadline: claims.HardDeadline}
	g, err := verifier.Verify(token, e, now)
	if err != nil {
		t.Fatal(err)
	}
	return nativeRentalFixture{s, c, p, g, e, signer, verifier, metadata}
}
func (f nativeRentalFixture) reserve(t *testing.T) Hold {
	t.Helper()
	h, disposition, err := f.s.ReceiveRentalPreparation(context.Background(), "prepare_rental", f.p)
	if err != nil || disposition != "PREPARATION_RESERVED" {
		t.Fatal(disposition, err)
	}
	return h
}
func (f nativeRentalFixture) control(t *testing.T, seq uint64, decision string, d time.Duration) rentalaccess.VerifiedControl {
	t.Helper()
	raw, err := f.signer.SignControl(f.metadata, seq, decision, f.clock.stamp.Wall, f.clock.stamp.Wall.Add(d))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(raw)
	proof, err := f.verifier.VerifyControl(body, f.g, f.expected, f.clock.stamp.Wall)
	if err != nil {
		t.Fatal(err)
	}
	return proof
}
func (f nativeRentalFixture) active(t *testing.T) Hold {
	t.Helper()
	h := f.reserve(t)
	if err := f.s.BindRentalGrant(context.Background(), "bind_rental", h, f.p, f.g, f.expected); err != nil {
		t.Fatal(err)
	}
	if err := f.s.ApplyRentalControl(context.Background(), h, f.control(t, 1, "ALLOW", 30*time.Second)); err != nil {
		t.Fatal(err)
	}
	return h
}

func TestNativeRentalUpgradePreservesLegacyOwnerFencesAndForeignKeys(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	if filepath.VolumeName(path) != "" {
		u.Path = "/" + u.Path
	}
	legacy, err := sql.Open("sqlite", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Close()
	tx, err := legacy.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, schema); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `PRAGMA application_id=1345401157; PRAGMA user_version=1`); err != nil {
		t.Fatal(err)
	}
	live, err := schemaDigest(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{"profile": Profile, "binding_hash": bindingHash(testBinding()), "schema_source_hash": digest([]byte(schema)), "schema_live_hash": live, "last_wall_ns": "0"} {
		if _, err = tx.ExecContext(ctx, `INSERT INTO metadata(name,value) VALUES(?,?)`, k, v); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO resources(resource_id,physical_digest,remote_high,local_high,owner_id,mode,phase) VALUES('resource_test',?,'12','7','lease_original','JOB','EXECUTING')`, strings.Repeat("e", 64)); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(lease(newClock(), "lease_original", "job_original", "12", "4"))
	if _, err = tx.ExecContext(ctx, `INSERT INTO inbox(lease_id,resource_id,local_generation,metadata_json,metadata_hash,phase,backend_id) VALUES('lease_original','resource_test','7',?,?,'EXECUTING','backend_original')`, string(raw), digest(raw)); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO receipts(command_id,lease_id,metadata_hash,kind) VALUES('command_original','lease_original',?,'RECEIVE')`, digest(raw)); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO job_fences(job_id,token,lease_id) VALUES('job_original','4','lease_original')`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	legacy.Close()
	s, err := Open(ctx, path, testBinding(), false, newClock())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var version int
	if s.db.QueryRow(`PRAGMA user_version`).Scan(&version) != nil || version != 2 {
		t.Fatal("legacy database not forward-migrated")
	}
	status, err := s.Snapshot(ctx)
	if err != nil || len(status.Resources) != 1 || status.Resources[0].Phase != "QUARANTINED" || status.Resources[0].Mode != "JOB" || status.Resources[0].RemoteHigh != "12" || status.Resources[0].LocalHigh != "7" {
		t.Fatal("legacy occupancy/fences were lost", status, err)
	}
	var kept, backend string
	if s.db.QueryRow(`SELECT metadata_json,backend_id FROM inbox WHERE lease_id='lease_original'`).Scan(&kept, &backend) != nil || kept != string(raw) || backend != "backend_original" {
		t.Fatal("legacy original request was lost")
	}
	check, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer check.Rollback()
	if err = foreignKeysValid(ctx, check); err != nil {
		t.Fatal("rebuild broke original foreign keys", err)
	}
}

func TestNativeRentalSharesOriginalLocalAndJobGate(t *testing.T) {
	s, c, _ := newStore(t)
	f := nativeRental(t, s, c)
	ctx := context.Background()
	local, err := s.AcquireLocal(ctx, "resource_test", "local_holder", "LOCAL")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.ReceiveRentalPreparation(ctx, "prepare_rental", f.p); !errors.Is(err, ErrBusy) {
		t.Fatal("rental bypassed local holder", err)
	}
	if err = s.RequestStop(ctx, local); err != nil {
		t.Fatal(err)
	}
	if err = s.ObserveStopped(ctx, local, Observation{RequestID: "local_holder", State: "ABSENT"}); err != nil {
		t.Fatal(err)
	}
	if err = s.FinishCleanup(ctx, local, Cleanup{WorkStopped: true, TokensRevoked: true, WorkspaceCleared: true, ManifestHash: strings.Repeat("9", 64)}); err != nil {
		t.Fatal(err)
	}
	h := f.reserve(t)
	if _, err = s.AcquireLocal(ctx, "resource_test", "other_local", "LOCAL"); !errors.Is(err, ErrBusy) {
		t.Fatal("local bypassed rental holder", err)
	}
	job := verified(t, lease(c, "lease_other", "job_other", "2", "1"), c)
	if _, _, err = s.Receive(ctx, "command_other", job); !errors.Is(err, ErrBusy) {
		t.Fatal("JOB bypassed rental holder", err)
	}
	if h.LocalGeneration() != "2" {
		t.Fatal("local owner generation reset")
	}
	if _, d, err := s.ReceiveRentalPreparation(ctx, "prepare_rental", f.p); err != nil || d != "QUERY_ONLY" {
		t.Fatal("original preparation re-acquired", d, err)
	}
}

func TestNativeRentalRequiresControlAndDoesNotExtendDuplicate(t *testing.T) {
	s, c, _ := newStore(t)
	f := nativeRental(t, s, c)
	ctx := context.Background()
	h := f.reserve(t)
	if err := s.BindRentalGrant(ctx, "bind_rental", h, f.p, f.g, f.expected); err != nil {
		t.Fatal(err)
	}
	if s.CheckRental(ctx, h, "INVOKE") == nil {
		t.Fatal("grant alone manufactured live control")
	}
	proof := f.control(t, 1, "ALLOW", 20*time.Second)
	if err := s.ApplyRentalControl(ctx, h, proof); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckRental(ctx, h, "INVOKE"); err != nil {
		t.Fatal(err)
	}
	if s.CheckRental(ctx, h, "EXPORT_SELF") == nil {
		t.Fatal("control elevated absent scope")
	}
	c.add(15 * time.Second)
	if err := s.ApplyRentalControl(ctx, h, proof); err != nil {
		t.Fatal(err)
	}
	c.add(6 * time.Second)
	if s.CheckRental(ctx, h, "INVOKE") == nil {
		t.Fatal("duplicate refreshed original absolute control expiry")
	}
	status, err := s.Snapshot(ctx)
	if err != nil || status.Resources[0].Phase != "QUARANTINED" || status.Resources[0].Mode != "RENTAL" {
		t.Fatal("expired control released owner", status, err)
	}
}

func TestNativeRentalChildIsOnceOnlySerialAndRetainsParent(t *testing.T) {
	s, c, _ := newStore(t)
	f := nativeRental(t, s, c)
	ctx := context.Background()
	h := f.active(t)
	ticket, v, d, err := s.BeginRentalInvocation(ctx, h, "invoke_first", strings.Repeat("1", 64), f.p.Details().RecipeHash)
	if err != nil || d != "SUBMISSION_RESERVED" || ticket == nil {
		t.Fatal(d, err)
	}
	if got, err := ticket.Take(); err != nil || got.ID != v.ID {
		t.Fatal(err)
	}
	if _, err = ticket.Take(); !errors.Is(err, ErrQuery) {
		t.Fatal("ticket submitted twice", err)
	}
	if again, _, d, err := s.BeginRentalInvocation(ctx, h, "invoke_first", strings.Repeat("1", 64), f.p.Details().RecipeHash); err != nil || again != nil || d != "QUERY_ONLY" {
		t.Fatal("duplicate granted another submit", d, err)
	}
	if _, _, _, err = s.BeginRentalInvocation(ctx, h, "invoke_first", strings.Repeat("2", 64), f.p.Details().RecipeHash); !errors.Is(err, ErrFence) {
		t.Fatal("original input was rebound", err)
	}
	if _, _, _, err = s.BeginRentalInvocation(ctx, h, "invoke_second", strings.Repeat("2", 64), f.p.Details().RecipeHash); !errors.Is(err, ErrBusy) {
		t.Fatal("parallel child bypassed serial slot", err)
	}
	if err = s.MarkRentalSubmitted(ctx, h, v.ID, "backend_first"); err != nil {
		t.Fatal(err)
	}
	if err = s.StopRentalInvocation(ctx, h, v.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.ObserveRentalInvocationStopped(ctx, h, v.ID, Observation{RequestID: v.ID, State: "ABSENT"}); !errors.Is(err, ErrEvidence) {
		t.Fatal("known backend became absent", err)
	}
	if err = s.ObserveRentalInvocationStopped(ctx, h, v.ID, Observation{RequestID: v.ID, BackendID: "backend_first", State: "STOPPED"}); err != nil {
		t.Fatal(err)
	}
	if err = s.FinishRentalInvocationCleanup(ctx, h, v.ID, RentalChildCleanup{Session: h.Owner(), Invocation: v.ID, LocalGeneration: h.LocalGeneration(), BackendID: "backend_first", ManifestHash: strings.Repeat("3", 64), WorkStopped: true, FutureStartRevoked: true, InputCleared: true, OutputKeysHandled: true}); err != nil {
		t.Fatal(err)
	}
	status, err := s.Snapshot(ctx)
	if err != nil || status.Resources[0].Mode != "RENTAL" || status.Resources[0].Phase != "EXECUTING" {
		t.Fatal("child cleanup released parent", status, err)
	}
	if _, _, d, err = s.BeginRentalInvocation(ctx, h, "invoke_second", strings.Repeat("2", 64), f.p.Details().RecipeHash); err != nil || d != "SUBMISSION_RESERVED" {
		t.Fatal("clean child did not allow next serial invocation", d, err)
	}
}

func TestNativeRentalStopCleanupCannotUseGenericRelease(t *testing.T) {
	s, c, _ := newStore(t)
	f := nativeRental(t, s, c)
	ctx := context.Background()
	h := f.active(t)
	if err := s.StopRental(ctx, h); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishCleanup(ctx, h, Cleanup{WorkStopped: true, TokensRevoked: true, WorkspaceCleared: true, ManifestHash: strings.Repeat("1", 64)}); !errors.Is(err, ErrEvidence) {
		t.Fatal("generic cleanup bypassed rental proof", err)
	}
	if err := s.ObserveRentalSessionStopped(ctx, h, RentalSessionStop{Session: h.Owner(), LocalGeneration: h.LocalGeneration(), ScopeDigest: strings.Repeat("2", 64), AllRequestsStopped: true, FutureStartsRevoked: true}); err != nil {
		t.Fatal(err)
	}
	cleanup := RentalSessionCleanup{Session: h.Owner(), LocalGeneration: h.LocalGeneration(), ManifestHash: strings.Repeat("3", 64), WorkStopped: true, AllGrantsRevoked: true, WorkspaceCleared: true}
	if err := s.FinishRentalSessionCleanup(ctx, h, cleanup); !errors.Is(err, ErrEvidence) {
		t.Fatal("missing output/key cleanup accepted", err)
	}
	cleanup.OutputKeysHandled = true
	if err := s.FinishRentalSessionCleanup(ctx, h, cleanup); err != nil {
		t.Fatal(err)
	}
	local, err := s.AcquireLocal(ctx, "resource_test", "new_local", "LOCAL")
	if err != nil {
		t.Fatal(err)
	}
	history, err := s.RentalHistory(ctx, h)
	if err != nil || history.Phase != "CLEANED" || history.CleanupHash != cleanup.ManifestHash {
		t.Fatal("original cleanup receipt lost after owner changed", err)
	}
	if err = s.StopRental(ctx, h); !errors.Is(err, ErrFence) {
		t.Fatal("old rental controlled new owner", err)
	}
	if local.LocalGeneration() == h.LocalGeneration() {
		t.Fatal("cleanup reset local fence")
	}
}

func TestNativeRentalReopenAndClockRollbackRetainQuarantinedOwner(t *testing.T) {
	for _, mode := range []string{"reopen", "rollback"} {
		t.Run(mode, func(t *testing.T) {
			s, c, path := newStore(t)
			f := nativeRental(t, s, c)
			ctx := context.Background()
			h := f.active(t)
			if mode == "reopen" {
				s.Close()
				var err error
				s, err = Open(ctx, path, testBinding(), false, c)
				if err != nil {
					t.Fatal(err)
				}
				defer s.Close()
			} else {
				c.stamp.Wall = c.stamp.Wall.Add(-time.Second)
			}
			if s.CheckRental(ctx, h, "INVOKE") == nil {
				t.Fatal("restart/clock rollback restored ALLOW")
			}
			status, err := s.Snapshot(ctx)
			if err != nil || status.Resources[0].Phase != "QUARANTINED" || status.Resources[0].Mode != "RENTAL" {
				t.Fatal("unsafe owner was released", status, err)
			}
			if err = s.BindRentalGrant(ctx, "bind_retry", h, f.p, f.g, f.expected); err == nil {
				t.Fatal("quarantined rental silently revived")
			}
		})
	}
}
