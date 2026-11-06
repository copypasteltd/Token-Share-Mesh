package node

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"p11.local/compute/client/execution"
	"p11.local/compute/client/protocol/rentalaccess"
	"p11.local/compute/shared/contractspec"
)

// All identities, native facts and platform replies are synthetic fixtures.
// These definitions exercise the actual journal/gate/HTTP join when the full
// code stage opens testing; they do not qualify physical hardware or payments.
func nodeRentalPreparation(t *testing.T, now time.Time) rentalaccess.Prepared {
	t.Helper()
	m := rentalaccess.Manifest{Profile: rentalaccess.ManifestProfile, Reference: "manifest_test", Session: "session_test", Order: "order_test", Buyer: "buyer_test", Seller: "seller_test", Device: "device_test", Resource: "resource_test", Generation: "4", Capability: "capability_test", Environment: "environment_test", EnvironmentHash: strings.Repeat("b", 64), ContractHash: strings.Repeat("c", 64), Mode: "GPU_RENTAL", Privacy: "STANDARD", Engine: "LLAMA_CPP_NATIVE", Release: "llama.cpp-b11960", ModelHash: strings.Repeat("d", 64), RecipeHash: strings.Repeat("f", 64), Protocol: "openai_nb1", PublicSpec: contractspec.Selection{ModelID: "model_test", DurationMS: 600000, InputTokenLimit: 2048, OutputTokenLimit: 256}}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	p, err := rentalaccess.VerifyPreparation(raw, rentalaccess.LocalApproval{Profile: rentalaccess.ApprovalProfile, Manifest: m, ManifestHash: hostDigest(raw), PhysicalHash: strings.Repeat("e", 64), Connection: "connection_test", Issuer: "issuer_test", PrepareUntil: now.Add(time.Minute)}, now)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func initRentalResource(t *testing.T, s *Store) *execution.Store {
	t.Helper()
	ctx := context.Background()
	ledger, err := s.InitExecution(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = ledger.RegisterResource(ctx, "resource_test", strings.Repeat("e", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.InitRentalJournal(); err != nil {
		t.Fatal(err)
	}
	return ledger
}

func TestNodeRentalPreparationIsEncryptedImmutableAndRecoverable(t *testing.T) {
	s := registeredStore(t)
	j, err := s.InitRentalJournal()
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Minute)
	p := nodeRentalPreparation(t, old)
	if err = j.RecordPreparation(p, old); err != nil {
		t.Fatal(err)
	}
	sealed, err := s.read(rentalJournalFile, rentalJournalLimit)
	if err != nil || bytes.Contains(sealed, []byte(p.Details().Session)) || bytes.Contains(sealed, p.Raw()) || bytes.Contains(sealed, []byte("local_approval")) {
		t.Fatal("owner approval leaked from encrypted journal", err)
	}
	a := p.Approval()
	a.PhysicalHash = strings.Repeat("a", 64)
	changed, err := rentalaccess.VerifyPreparation(p.Raw(), a, old)
	if err != nil {
		t.Fatal(err)
	}
	if err = j.RecordPreparation(changed, old); !errors.Is(err, ErrRentalJournal) {
		t.Fatal("original owner approval was rebound", err)
	}
	s = reopen(t, s)
	j, err = s.OpenRentalJournal()
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := j.ApprovedPreparation(p.Details().Session)
	if err != nil || recovered.ApprovalDigest() != p.ApprovalDigest() || !bytes.Equal(recovered.Raw(), p.Raw()) || recovered.ValidAt(time.Now()) {
		t.Fatal("metadata recovery restored authority or changed original approval", err)
	}
}

func TestNodeRentalSessionReservesPhysicalWithoutInvoking(t *testing.T) {
	s := registeredStore(t)
	ledger := initRentalResource(t, s)
	ctx := context.Background()
	p := nodeRentalPreparation(t, time.Now())
	r, d, err := s.PrepareRentalSession(ctx, "prepare_test", p)
	if err != nil || d != "PREPARATION_RESERVED" {
		t.Fatal(d, err)
	}
	status, err := ledger.Snapshot(ctx)
	if err != nil || len(status.Resources) != 1 || status.Resources[0].Mode != "RENTAL" || status.Resources[0].Phase != "PREPARING" || status.ExecutionEnabled {
		t.Fatal("preparation did not reserve the original shared slot safely", status, err)
	}
	if r.CheckScope(ctx, "INVOKE") == nil {
		t.Fatal("preparation manufactured invocation authority")
	}
	if _, err = ledger.AcquireLocal(ctx, "resource_test", "local_test", "LOCAL"); !errors.Is(err, execution.ErrBusy) {
		t.Fatal("local use bypassed original rental", err)
	}
	if _, d, err = s.PrepareRentalSession(ctx, "prepare_test", p); err != nil || d != "QUERY_ONLY" {
		t.Fatal("replay reserved another owner", d, err)
	}
	if err = r.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	status, err = ledger.Snapshot(ctx)
	if err != nil || status.Resources[0].Mode != "RENTAL" || status.Resources[0].Phase != "STOPPING" {
		t.Fatal("stop released uncleaned physical owner", status, err)
	}
}

func TestNodeRentalControlConnectsToOriginalPhysicalGate(t *testing.T) {
	f := journalRental(t)
	p := nodeRentalPreparation(t, f.now)
	claims := f.metadata.Claims
	claims.ManifestHash = p.Digest()
	issued, err := f.signer.Issue(claims, f.now)
	if err != nil {
		t.Fatal(err)
	}
	defer issued.Close()
	f.metadata = issued.Metadata()
	f.token, err = issued.TakeCredential()
	if err != nil {
		t.Fatal(err)
	}
	f.expected.ManifestHash = p.Digest()
	f.expected.TokenDigest = f.metadata.Digest
	var s *Store
	var reads, controls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && r.URL.Path == "/v1/p11/runner/rental-sessions/session_test/access-grants/grant_test":
			verify(t, s, r, nil)
			reads++
			json.NewEncoder(w).Encode(rentalaccess.GrantExpectation{Profile: rentalaccess.ExpectationProfile, Metadata: f.metadata, SessionVersion: 7, Connection: "connection_test"})
		case r.Method == "POST" && r.URL.Path == "/v1/p11/runner/rental-sessions/session_test/control":
			raw, err := io.ReadAll(io.LimitReader(r.Body, 2049))
			if err != nil || len(raw) > 2048 {
				t.Error("unbounded control request")
				w.WriteHeader(400)
				return
			}
			verify(t, s, r, raw)
			var in rentalaccess.ControlRequest
			if json.Unmarshal(raw, &in) != nil || in.ExpectedVersion != 7 || in.Grant != f.expected.Grant || in.Digest != f.expected.TokenDigest || in.Generation != f.expected.Generation || in.Terms != f.expected.Terms || in.TermsHash != f.expected.TermsHash {
				t.Error("wrong original control tuple")
				w.WriteHeader(400)
				return
			}
			seq, err := strconv.ParseUint(in.Sequence, 10, 64)
			if err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			now := time.Now()
			reply, err := f.signer.SignControl(f.metadata, seq, "ALLOW", now, now.Add(30*time.Second))
			if err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			controls++
			json.NewEncoder(w).Encode(reply)
		default:
			t.Error("unexpected rental route")
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	s = testStore(t, server.URL)
	s.s.DeviceID = "device_test"
	s.s.SellerID = "seller_test"
	s.s.Version = 1
	s.s.RemoteState = "PAIRED"
	if err = s.save(); err != nil {
		t.Fatal(err)
	}
	ledger := initRentalResource(t, s)
	ctx := context.Background()
	r, _, err := s.PrepareRentalSession(ctx, "prepare_test", p)
	if err != nil {
		t.Fatal(err)
	}
	approved := f.expected
	approved.TokenDigest = "" // Must come from the independently read original platform record.
	if err = r.AttachControl(ctx, f.verifier, f.token, approved); err != nil {
		t.Fatal(err)
	}
	if r.CheckScope(ctx, "INVOKE") == nil || ledger.CheckRental(ctx, r.Hold(), "INVOKE") == nil {
		t.Fatal("bind alone manufactured ALLOW")
	}
	if err = r.RefreshControl(ctx); err != nil {
		t.Fatal(err)
	}
	if reads != 1 || controls != 1 {
		t.Fatal("control bypassed actual signed transport", reads, controls)
	}
	if err = r.CheckScope(ctx, "INVOKE"); err != nil {
		t.Fatal(err)
	}
	if err = ledger.CheckRental(ctx, r.Hold(), "INVOKE"); err != nil {
		t.Fatal("verified control did not reach original physical gate", err)
	}
	ticket, v, d, err := r.BeginInvocation(ctx, "invoke_test", strings.Repeat("1", 64), p.Details().RecipeHash)
	if err != nil || ticket == nil || d != "SUBMISSION_RESERVED" || v.Session != p.Details().Session {
		t.Fatal(d, err)
	}
	if err = r.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if r.CheckScope(ctx, "INVOKE") == nil || ledger.CheckRental(ctx, r.Hold(), "INVOKE") == nil {
		t.Fatal("withdrawn control retained native authority")
	}
	status, err := ledger.Snapshot(ctx)
	if err != nil || status.Resources[0].Mode != "RENTAL" || status.Resources[0].Phase != "STOPPING" {
		t.Fatal("control stop released an unqueried invocation", status, err)
	}
}
