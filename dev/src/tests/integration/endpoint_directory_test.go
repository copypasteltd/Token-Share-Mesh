package integration_test

import (
	"encoding/json"
	"p11.local/compute/internal/dispatch/device"
	"p11.local/compute/internal/endpointregistry"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/testsupport"
	"p11.local/compute/internal/trade"
	"p11.local/compute/shared/endpointdirectory"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"sync"
	"testing"
	"time"
)

func directoryBuyer(t *testing.T) (identity.Principal, identity.Sessions) {
	t.Helper()
	s, _ := sessionService(t)
	issued := loginSession(t, s, "WEB")
	credential, e := s.Authenticate(ctx, issued.Secret)
	if e != nil {
		t.Fatal(e)
	}
	return credential.Principal, s
}
func directoryInitial(t *testing.T, p identity.Principal) testsupport.PublicEndpointFixture {
	t.Helper()
	role, endpoint := "BUYER", ids.New("endpoint")
	if p.Kind == identity.Device {
		role, endpoint = "SELLER", p.DeviceID
	}
	now := time.Now().UTC().Add(-time.Minute)
	return testsupport.PublicEndpoint(t, p.UserID, endpoint, role, 1, now, now.Add(time.Hour))
}
func directoryRegister(t *testing.T, s *endpointregistry.Service, p identity.Principal, f testsupport.PublicEndpointFixture) endpointdirectory.Record {
	t.Helper()
	v, e := s.Register(ctx, p, endpointdirectory.Registration{Bundle: f.Bundle}, ids.New("register"))
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func TestEndpointDirectoryOriginalRegistrationAndReferenceOnlyReplay(t *testing.T) {
	requireDB(t)
	p, _ := directoryBuyer(t)
	s := &endpointregistry.Service{DB: db}
	f := directoryInitial(t, p)
	key := ids.New("original")
	v, e := s.Register(ctx, p, endpointdirectory.Registration{Bundle: f.Bundle}, key)
	if e != nil || v.Validate() != nil || v.State != "ACTIVE" || v.Replayed || !v.IndependentTrustRequired {
		t.Fatal(v, e)
	}
	replay, e := s.Register(ctx, p, endpointdirectory.Registration{Bundle: f.Bundle}, key)
	if e != nil || !replay.Replayed || replay.BundleHash != v.BundleHash {
		t.Fatal("original registration replaced", replay, e)
	}
	var response []byte
	var proof *string
	if e = db.Pool.QueryRow(ctx, `SELECT safe_response FROM p11.idempotency_records WHERE principal_id=$1 AND operation_id='registerEndpointBundle'`, p.UserID).Scan(&response); e != nil {
		t.Fatal(e)
	}
	var body map[string]json.RawMessage
	if json.Unmarshal(response, &body) != nil || len(body) != 1 || body["recipient_id"] == nil {
		t.Fatal("historical ACTIVE view stored in replay")
	}
	if e = db.Pool.QueryRow(ctx, `SELECT trust_evidence_id FROM p11.recipient_keys WHERE id=$1`, v.Recipient).Scan(&proof); e != nil || proof != nil {
		t.Fatal("account binding promoted to independent trust", e)
	}
}
func TestEndpointDirectoryDifferentInitialKeyCannotReplaceOriginal(t *testing.T) {
	requireDB(t)
	p, _ := directoryBuyer(t)
	s := &endpointregistry.Service{DB: db}
	f := directoryInitial(t, p)
	directoryRegister(t, s, p, f)
	replacement := testsupport.PublicEndpoint(t, p.UserID, f.Bundle.Endpoint, "BUYER", 1, time.Now().UTC(), time.Now().UTC().Add(time.Hour))
	_, e := s.Register(ctx, p, endpointdirectory.Registration{Bundle: replacement.Bundle}, ids.New("replace"))
	assertCode(t, e, fault.Conflict)
}
func TestEndpointDirectoryRotationRetainsOriginalAndRejectsFork(t *testing.T) {
	requireDB(t)
	p, _ := directoryBuyer(t)
	s := &endpointregistry.Service{DB: db}
	old := directoryInitial(t, p)
	directoryRegister(t, s, p, old)
	now := time.Now().UTC()
	next, proof := testsupport.RotatePublicEndpoint(t, old, now, now.Add(time.Hour))
	v, e := s.Rotate(ctx, p, old.Bundle.Content.ID, endpointdirectory.Rotation{Transition: proof}, ids.New("rotate"))
	if e != nil || v.BundleHash != next.Bundle.Hash() || v.Previous != old.Bundle.Content.ID || v.VerifySuccessor(old.Bundle, now) != nil {
		t.Fatal(v, e)
	}
	previous, e := s.Get(ctx, p, old.Bundle.Content.ID)
	if e != nil || previous.State != "RETIRED" {
		t.Fatal(previous, e)
	}
	again, e := s.Rotate(ctx, p, old.Bundle.Content.ID, endpointdirectory.Rotation{Transition: proof}, ids.New("recover"))
	if e != nil || again.BundleHash != v.BundleHash {
		t.Fatal("original transition not recoverable", again, e)
	}
	_, fork := testsupport.RotatePublicEndpoint(t, old, now, now.Add(time.Hour))
	_, e = s.Rotate(ctx, p, old.Bundle.Content.ID, endpointdirectory.Rotation{Transition: fork}, ids.New("fork"))
	assertCode(t, e, fault.Conflict)
}
func TestEndpointDirectoryConcurrentRotationsHaveOneSuccessor(t *testing.T) {
	requireDB(t)
	p, _ := directoryBuyer(t)
	s := &endpointregistry.Service{DB: db}
	old := directoryInitial(t, p)
	directoryRegister(t, s, p, old)
	now := time.Now().UTC()
	_, a := testsupport.RotatePublicEndpoint(t, old, now, now.Add(time.Hour))
	_, b := testsupport.RotatePublicEndpoint(t, old, now, now.Add(time.Hour))
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, in := range []endpointdirectory.Rotation{{Transition: a}, {Transition: b}} {
		wg.Add(1)
		go func(in endpointdirectory.Rotation) {
			defer wg.Done()
			_, e := s.Rotate(ctx, p, old.Bundle.Content.ID, in, ids.New("race"))
			errs <- e
		}(in)
	}
	wg.Wait()
	close(errs)
	ok, conflicts := 0, 0
	for e := range errs {
		if e == nil {
			ok++
		} else if fault.Public(e).Code == fault.Conflict {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if ok != 1 || conflicts != 1 {
		t.Fatal("endpoint lineage forked", ok, conflicts)
	}
}
func TestEndpointDirectoryRevocationIsCurrentOnOriginalReplay(t *testing.T) {
	requireDB(t)
	p, _ := directoryBuyer(t)
	s := &endpointregistry.Service{DB: db}
	f := directoryInitial(t, p)
	key := ids.New("original")
	v, e := s.Register(ctx, p, endpointdirectory.Registration{Bundle: f.Bundle}, key)
	if e != nil {
		t.Fatal(e)
	}
	revoked, e := s.Revoke(ctx, p, v.Recipient, endpointdirectory.Revocation{BundleHash: v.BundleHash}, ids.New("revoke"))
	if e != nil || revoked.State != "REVOKED" {
		t.Fatal(revoked, e)
	}
	replay, e := s.Register(ctx, p, endpointdirectory.Registration{Bundle: f.Bundle}, key)
	if e != nil || !replay.Replayed || replay.State != "REVOKED" {
		t.Fatal("historical ACTIVE response resurrected endpoint", replay, e)
	}
}
func TestEndpointDirectoryHistoryNeverFallsBackAfterCurrentRevocation(t *testing.T) {
	requireDB(t)
	p, _ := directoryBuyer(t)
	s := &endpointregistry.Service{DB: db}
	old := directoryInitial(t, p)
	directoryRegister(t, s, p, old)
	now := time.Now().UTC()
	next, proof := testsupport.RotatePublicEndpoint(t, old, now, now.Add(time.Hour))
	v, e := s.Rotate(ctx, p, old.Bundle.Content.ID, endpointdirectory.Rotation{Transition: proof}, ids.New("rotate"))
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.Revoke(ctx, p, v.Recipient, endpointdirectory.Revocation{BundleHash: next.Bundle.Hash()}, ids.New("revoke"))
	if e != nil {
		t.Fatal(e)
	}
	page, e := s.List(ctx, p, "")
	if e != nil || len(page.Items) != 1 || page.Items[0].BundleHash != next.Bundle.Hash() || page.Items[0].State != "REVOKED" {
		t.Fatal("revoked version fell back to predecessor", page, e)
	}
}
func TestEndpointDirectoryAccountIsolationAndCurrentSessionRechecked(t *testing.T) {
	requireDB(t)
	p, sessions := directoryBuyer(t)
	other, _ := directoryBuyer(t)
	s := &endpointregistry.Service{DB: db}
	f := directoryInitial(t, p)
	key := ids.New("original")
	v, e := s.Register(ctx, p, endpointdirectory.Registration{Bundle: f.Bundle}, key)
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.Get(ctx, other, v.Recipient)
	assertCode(t, e, fault.NotFound)
	if _, e = sessions.Revoke(ctx, p); e != nil {
		t.Fatal(e)
	}
	_, e = s.Register(ctx, p, endpointdirectory.Registration{Bundle: f.Bundle}, key)
	assertCode(t, e, fault.Unauthenticated)
}
func TestEndpointDirectoryDeviceProofOwnsExactProviderEndpoint(t *testing.T) {
	requireDB(t)
	owner := deviceOwner(t)
	d, key := enrolled(t, owner, "WINDOWS")
	p, _, _ := signedDevice(t, d, key)
	s := &endpointregistry.Service{DB: db}
	f := directoryInitial(t, p)
	directoryRegister(t, s, p, f)
	other, otherKey := enrolled(t, owner, "WINDOWS")
	op, _, _ := signedDevice(t, other, otherKey)
	_, e := s.Register(ctx, op, endpointdirectory.Registration{Bundle: f.Bundle}, ids.New("foreign"))
	assertCode(t, e, fault.Forbidden)
	owner.SessionID = ids.New("not_a_current_session")
	owner.Scopes = append(owner.Scopes, "recipient:manage", "recipient:read")
	_, e = s.Register(ctx, owner, endpointdirectory.Registration{Bundle: f.Bundle}, ids.New("browser"))
	assertCode(t, e, fault.Forbidden)
	exec(t, `UPDATE p11.devices SET state='REVOKED' WHERE id=$1`, d.ID)
	_, e = s.Get(ctx, p, f.Bundle.Content.ID)
	assertCode(t, e, fault.Unauthenticated)
}
func TestEndpointDirectoryDatabaseRejectsRebindingHistoryAndOrphanProjection(t *testing.T) {
	requireDB(t)
	p, _ := directoryBuyer(t)
	s := &endpointregistry.Service{DB: db}
	v := directoryRegister(t, s, p, directoryInitial(t, p))
	for _, sql := range []string{`UPDATE p11.endpoint_public_bundles SET endpoint_id='endpoint_other' WHERE recipient_id=$1`, `DELETE FROM p11.endpoint_public_bundles WHERE recipient_id=$1`, `DELETE FROM p11.endpoint_public_key_claims WHERE recipient_id=$1`, `UPDATE p11.recipient_keys SET encryption_public_key='replacement' WHERE id=$1`, `UPDATE p11.recipient_keys SET revoked_at=clock_timestamp() WHERE id=$1`} {
		if _, e := db.Pool.Exec(ctx, sql, v.Recipient); e == nil {
			t.Fatal("immutable/deferred public projection guard bypassed", sql)
		}
	}
	_, e := db.Pool.Exec(ctx, `INSERT INTO p11.recipient_keys(id,owner_id,key_version,encryption_public_key,signing_public_key,profile_id,expires_at) VALUES($1,$2,1,'orphan','orphan','P11_ENDPOINT_PUBLIC_1',clock_timestamp()+interval '1 hour')`, ids.New("orphan"), p.UserID)
	if e == nil {
		t.Fatal("orphan public recipient committed without bundle and claims")
	}
}
func TestEndpointDirectoryBoundedPaginationAndAccountQuota(t *testing.T) {
	requireDB(t)
	p, _ := directoryBuyer(t)
	s := &endpointregistry.Service{DB: db}
	for range endpointdirectory.MaximumEndpoints {
		directoryRegister(t, s, p, directoryInitial(t, p))
	}
	_, e := s.Register(ctx, p, endpointdirectory.Registration{Bundle: directoryInitial(t, p).Bundle}, ids.New("overflow"))
	assertCode(t, e, fault.Conflict)
	seen := map[string]bool{}
	cursor := ""
	for {
		page, e := s.List(ctx, p, cursor)
		if e != nil || len(page.Items) > endpointdirectory.PageSize {
			t.Fatal(page, e)
		}
		for _, v := range page.Items {
			if seen[v.Recipient] {
				t.Fatal("duplicate history page")
			}
			seen[v.Recipient] = true
		}
		if page.Next == nil {
			break
		}
		cursor = *page.Next
	}
	if len(seen) != endpointdirectory.MaximumEndpoints {
		t.Fatal("endpoint page omitted original metadata")
	}
}
func TestEndpointDirectoryOrderDiscoveryRetainsOriginalSellerAndDevice(t *testing.T) {
	requireDB(t)
	p, _ := directoryBuyer(t)
	f := testsupport.SeedNativeBlindOffer(t, ctx, db, ids.New("region"), "openai_nb1")
	dp, _, _ := signedDevice(t, device.View{ID: f.Device}, f.DeviceKey)
	s := &endpointregistry.Service{DB: db}
	v := directoryRegister(t, s, dp, directoryInitial(t, dp))
	trades := trade.Service{DB: db}
	q, e := trades.CreateQuote(ctx, p, f.Input(), ids.New("quote"))
	if e != nil {
		t.Fatal(e)
	}
	order, e := trades.CreateOrder(ctx, p, confirm(q), ids.New("order"))
	if e != nil {
		t.Fatal(e)
	}
	found, e := s.GetForOrder(ctx, p, order.ID, v.Recipient)
	if e != nil || found.BundleHash != v.BundleHash || !found.IndependentTrustRequired {
		t.Fatal(found, e)
	}
	other, _ := directoryBuyer(t)
	_, e = s.GetForOrder(ctx, other, order.ID, v.Recipient)
	assertCode(t, e, fault.NotFound)
	foreign := deviceOwner(t)
	d, key := enrolled(t, foreign, "WINDOWS")
	fp, _, _ := signedDevice(t, d, key)
	unrelated := directoryRegister(t, s, fp, directoryInitial(t, fp))
	_, e = s.GetForOrder(ctx, p, order.ID, unrelated.Recipient)
	assertCode(t, e, fault.NotFound)
	exec(t, `UPDATE p11.devices SET state='REVOKED' WHERE id=$1`, f.Device)
	_, e = s.GetForOrder(ctx, p, order.ID, v.Recipient)
	assertCode(t, e, fault.Conflict)
}
