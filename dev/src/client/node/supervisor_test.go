package node

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/client/supervisor"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type nodeBackend struct {
	running                  atomic.Bool
	submits, cancels, cleans atomic.Int32
}

func (b *nodeBackend) Submit(_ context.Context, w supervisor.Work) (supervisor.Observation, error) {
	b.submits.Add(1)
	b.running.Store(true)
	return supervisor.Observation{RequestID: w.RequestID, BackendID: "backend_test", State: "RUNNING"}, nil
}
func (b *nodeBackend) Query(_ context.Context, w supervisor.Work) (supervisor.Observation, error) {
	state := "STOPPED"
	if b.running.Load() {
		state = "RUNNING"
	}
	return supervisor.Observation{RequestID: w.RequestID, BackendID: "backend_test", State: state}, nil
}
func (b *nodeBackend) Cancel(context.Context, supervisor.Work) error {
	b.cancels.Add(1)
	b.running.Store(false)
	return nil
}
func (b *nodeBackend) Cleanup(_ context.Context, w supervisor.Work, o supervisor.Observation) (supervisor.Cleaned, error) {
	b.cleans.Add(1)
	return supervisor.Cleaned{RequestID: w.RequestID, BackendID: o.BackendID, LocalGeneration: w.LocalGeneration, ManifestHash: strings.Repeat("f", 64), WorkStopped: true, TokensRevoked: true, WorkspaceCleared: true}, nil
}
func registrations(b *nodeBackend) []supervisor.Registration {
	return []supervisor.Registration{{Environment: strings.Repeat("2", 64), Capability: "cap_test", ManifestHash: strings.Repeat("1", 64), Backend: b}}
}
func supervisedOptions() supervisor.Options {
	return supervisor.Options{Poll: 10 * time.Millisecond, CallTimeout: 150 * time.Millisecond, ShutdownTimeout: time.Second}
}

func TestNodeSupervisorRequiresExplicitLedgerAndSameOwnership(t *testing.T) {
	s := registeredStore(t)
	ctx := context.Background()
	b := &nodeBackend{}
	if _, e := s.NewSupervisor(ctx, registrations(b), supervisedOptions()); !errors.Is(e, ErrState) {
		t.Fatal("implicit execution init", e)
	}
	if _, e := s.InitExecution(ctx); e != nil {
		t.Fatal(e)
	}
	watch, e := s.NewSupervisor(ctx, registrations(b), supervisedOptions())
	if e != nil {
		t.Fatal(e)
	}
	other := registeredStore(t)
	if _, e = other.InitExecution(ctx); e != nil {
		t.Fatal(e)
	}
	if e = other.RunSupervised(ctx, watch, nil); !errors.Is(e, ErrState) {
		t.Fatal("supervisor crossed node OS lock", e)
	}
	s.s.RemoteState = "DRAINING"
	drain, e := s.NewSupervisor(ctx, registrations(b), supervisedOptions())
	if e != nil {
		t.Fatal("draining state blocked recovery", e)
	}
	run, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- drain.Run(run) }()
	<-drain.Ready()
	if _, e = drain.Submit(ctx, "command_test", executionlease.Verified{}); !errors.Is(e, supervisor.ErrClosed) {
		t.Fatal("draining admission", e)
	}
	cancel()
	select {
	case e = <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("drain watchdog did not stop")
	}
}
func TestNodeControlDrainStopsSupervisedOriginalOwner(t *testing.T) {
	allow := make(chan struct{})
	var current any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-allow:
		case <-r.Context().Done():
			return
		}
		if r.Method == http.MethodGet {
			writeReply(w, 200, current)
			return
		}
		writeReply(w, 403, map[string]string{"error": "synthetic revoked authority"})
	}))
	defer server.Close()
	s := testStore(t, server.URL)
	s.s.DeviceID = "device_test"
	s.s.SellerID = "seller_test"
	s.s.Version = 1
	s.s.RemoteState = "PAIRED"
	vCurrent := view(s)
	vCurrent.ID = s.s.DeviceID
	vCurrent.SellerID = s.s.SellerID
	vCurrent.State = "DRAINING"
	vCurrent.Version = 2
	current = vCurrent
	if e := s.save(); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ledger, e := s.InitExecution(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = ledger.RegisterResource(ctx, "resource_test", strings.Repeat("e", 64)); e != nil {
		t.Fatal(e)
	}
	b := &nodeBackend{}
	watch, e := s.NewSupervisor(ctx, registrations(b), supervisedOptions())
	if e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { done <- s.RunSupervised(ctx, watch, nil) }()
	select {
	case <-watch.Ready():
	case <-time.After(time.Second):
		t.Fatal("watchdog not ready")
	}
	now := time.Now().UTC()
	l := executionlease.Lease{ID: "lease_test", Job: "job_test", Attempt: "attempt_test", Order: "order_test", Seller: "seller_test", Device: "device_test", Resource: "resource_test", Generation: "1", Capability: "cap_test", Manifest: "manifest_test", ManifestHash: strings.Repeat("1", 64), Environment: strings.Repeat("2", 64), Revision: 1, Issued: now.Format(time.RFC3339Nano), NotBefore: now.Format(time.RFC3339Nano), HardDeadline: now.Add(time.Minute).Format(time.RFC3339Nano), Fence: "1", Expires: now.Add(45 * time.Second).Format(time.RFC3339Nano), ContractHash: strings.Repeat("3", 64), Profile: executionlease.Profile}
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{51}, 32))
	message, e := executionlease.Message(l)
	if e != nil {
		t.Fatal(e)
	}
	l.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, message))
	raw, _ := json.Marshal(l)
	plan := executionlease.Plan{Job: l.Job, Attempt: l.Attempt, Order: l.Order, Seller: l.Seller, Device: l.Device, Resource: l.Resource, Generation: l.Generation, Capability: l.Capability, Manifest: l.Manifest, ManifestHash: l.ManifestHash, Environment: l.Environment, Fence: l.Fence, ContractHash: l.ContractHash, HardDeadline: now.Add(time.Minute)}
	v, e := executionlease.Verify(raw, key.Public().(ed25519.PublicKey), plan, now)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = watch.Submit(ctx, "command_test", v); e != nil {
		t.Fatal(e)
	}
	close(allow)
	select {
	case e = <-done:
		if !errors.Is(e, ErrAuthority) {
			t.Fatal("control failure lost", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("control failure abandoned watchdog")
	}
	status, e := ledger.Snapshot(context.Background())
	if e != nil || status.Resources[0].Phase != "FREE" || b.submits.Load() != 1 || b.cancels.Load() < 1 || b.cleans.Load() < 1 || s.Status().ExecutionEnabled || s.Status().RemoteState != "DRAINING" {
		t.Fatal("control failure skipped original stop/cleanup", e)
	}
}
