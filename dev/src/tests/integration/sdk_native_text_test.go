package integration_test

// SOURCE_ONLY. Positive cases retain actual original synthetic Machine Pay,
// allocation, API-key creation, lease issuance and signed content services.
// They do not use financial SETTLED/READY fixture bypasses or device execution.
import (
	"bytes"
	"crypto/ed25519"
	"strings"
	"sync"
	"testing"

	relay "p11.local/compute/gateway/core"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/invocation"
	"p11.local/compute/internal/testsupport"
	"p11.local/compute/shared/ids"
)

func contentSDK(t *testing.T, f contentFixture) *invocation.SDK {
	t.Helper()
	d, err := invocation.NewDelivery(invocation.DeliveryConfig{Claims: f.claims, Invocations: f.config.Invocations, InputKeys: f.config.InputKeys})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	s, err := invocation.NewSDK(invocation.SDKConfig{Delivery: d, Content: f.content, InputVersion: f.config.InputVersion, SigningKeyID: f.config.SigningKeyID, SigningPrivateKey: f.config.SigningPrivateKey})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}
func admissionSDK(t *testing.T, p relay.Profile) (*invocation.SDK, identity.Principal, invocation.Request, []byte) {
	t.Helper()
	requireDB(t)
	offer := testsupport.SeedNativeOfferForProtocol(t, ctx, db, ids.New("region"), string(p))
	paid := machineFixtureOfferBuyer(t, offer, nil)
	payment := paid.paidOriginal(t)
	if worked, err := paid.machine.RecoverFundingOne(ctx); err != nil || !worked {
		t.Fatal(worked, err)
	}
	route := fundingRoute(t, paid, payment)
	_, _, caller := routeKey(t, paid.buyer, route.ID)
	c := executionConfig(t)
	claims, err := invocation.NewClaims(db, "SANDBOX", map[string]ed25519.PublicKey{c.SigningKeyID: c.SigningPrivateKey.Public().(ed25519.PublicKey)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(claims.Close)
	d, err := invocation.NewDelivery(invocation.DeliveryConfig{Claims: claims, Invocations: c.Invocations, InputKeys: c.InputKeys})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	completion, err := invocation.NewCompletion(claims, nil)
	if err != nil {
		t.Fatal(err)
	}
	content, err := invocation.NewResultContent(completion, "content_sdk", map[string][]byte{"content_sdk": bytes.Repeat([]byte{94}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(content.Close)
	s, err := invocation.NewSDK(invocation.SDKConfig{Delivery: d, Content: content, InputVersion: c.InputVersion, SigningKeyID: c.SigningKeyID, SigningPrivateKey: c.SigningPrivateKey})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	in, body := executionBody(p, ids.New("sdkcall"))
	return s, caller, in, body
}
func TestSDKAdmissionAtomicallyWiresThreeOriginalNativeProfiles(t *testing.T) {
	for _, p := range []relay.Profile{relay.ChatText, relay.ResponsesText, relay.MessagesText} {
		t.Run(string(p), func(t *testing.T) {
			s, caller, in, body := admissionSDK(t, p)
			got, err := s.Begin(ctx, caller, p, in.IdempotencyKey, body)
			if err != nil || got.State != "RUNNING" || got.Replayed {
				t.Fatal(got, err)
			}
			executionCounts(t, got.JobID, 1, 1, 1, 3, 1)
			var cipher []byte
			if err = db.Pool.QueryRow(ctx, `SELECT input_ciphertext FROM p11.assets WHERE job_id=$1`, got.JobID).Scan(&cipher); err != nil || bytes.Contains(cipher, []byte(syntheticInput)) {
				t.Fatal("input privacy", err)
			}
			again, err := s.Begin(ctx, caller, p, in.IdempotencyKey, body)
			if err != nil || again.JobID != got.JobID || !again.Replayed {
				t.Fatal(again, err)
			}
			executionCounts(t, got.JobID, 1, 1, 1, 3, 1)
		})
	}
}
func TestSDKConcurrentSameRequestHasOneAdmissionAndOneLease(t *testing.T) {
	s, caller, in, body := admissionSDK(t, relay.ChatText)
	type answer struct {
		p invocation.Preparation
		e error
	}
	answers := make(chan answer, 4)
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, e := s.Begin(ctx, caller, relay.ChatText, in.IdempotencyKey, body)
			answers <- answer{p, e}
		}()
	}
	wg.Wait()
	close(answers)
	job := ""
	fresh := 0
	for a := range answers {
		if a.e != nil {
			t.Fatal(a.e)
		}
		if job == "" {
			job = a.p.JobID
		}
		if a.p.JobID != job {
			t.Fatal("duplicate job")
		}
		if !a.p.Replayed {
			fresh++
		}
	}
	if fresh != 1 {
		t.Fatal("fresh count", fresh)
	}
	executionCounts(t, job, 1, 1, 1, 3, 1)
}
func TestSDKChangedBodyCannotReplaceOriginalCall(t *testing.T) {
	s, caller, in, body := admissionSDK(t, relay.ChatText)
	got, err := s.Begin(ctx, caller, relay.ChatText, in.IdempotencyKey, body)
	if err != nil {
		t.Fatal(err)
	}
	changed := []byte(strings.Replace(string(body), syntheticInput, "synthetic_changed", 1))
	if _, err = s.Begin(ctx, caller, relay.ChatText, in.IdempotencyKey, changed); err == nil {
		t.Fatal("changed body accepted")
	}
	executionCounts(t, got.JobID, 1, 1, 1, 3, 1)
}
func TestSDKResultUsesOriginalKeyWithoutSessionReadGrant(t *testing.T) {
	f := originalContent(t)
	s := contentSDK(t, f)
	f.ready(t)
	v, err := s.State(ctx, f.buyerKey, f.issued.JobID)
	if err != nil || v.Head == nil || v.Head.Digest() != f.head.Digest() {
		t.Fatal(v, err)
	}
	c, err := s.Chunk(ctx, f.buyerKey, f.issued.JobID, f.delivery.ID, f.head.Digest(), 0)
	if err != nil || string(c.Content) != "synthetic_result" {
		t.Fatal(err)
	}
	clear(c.Content)
	var grants int
	if err = db.Pool.QueryRow(ctx, `SELECT count(*) FROM p11.native_result_read_grants WHERE job_id=$1`, f.issued.JobID).Scan(&grants); err != nil || grants != 0 {
		t.Fatal("session grant minted", err)
	}
	q, l := f.counts(t)
	if q != 0 || l != 0 {
		t.Fatal("read accepted/metered/billed")
	}
}
func TestSDKPendingUploadDoesNotBecomeResultReady(t *testing.T) {
	f := originalContent(t)
	s := contentSDK(t, f)
	v, err := s.State(ctx, f.buyerKey, f.issued.JobID)
	if err != nil || v.Head != nil {
		t.Fatal("incomplete content readable", err)
	}
}
func TestSDKOtherKeyEvenOnSameRouteCannotReadOriginalCall(t *testing.T) {
	f := originalContent(t)
	s := contentSDK(t, f)
	f.ready(t)
	_, _, other := routeKey(t, f.buyer, f.issued.RouteID)
	if _, err := s.State(ctx, other, f.issued.JobID); err == nil {
		t.Fatal("substitute API key read original")
	}
}
func TestSDKSessionCannotImpersonateOriginalAPIKey(t *testing.T) {
	f := originalContent(t)
	s := contentSDK(t, f)
	f.ready(t)
	if _, err := s.State(ctx, f.buyer, f.issued.JobID); err == nil {
		t.Fatal("session used as SDK key")
	}
}
func TestSDKRevokedKeyFailsEveryChunkAndFinalRecheck(t *testing.T) {
	f := originalContent(t)
	s := contentSDK(t, f)
	f.ready(t)
	if err := s.Recheck(ctx, f.buyerKey, f.issued.JobID, f.delivery.ID, f.head.Digest()); err != nil {
		t.Fatal(err)
	}
	exec(t, `UPDATE p11.api_keys SET revoked_at=clock_timestamp() WHERE id=$1`, f.buyerKey.KeyID)
	if _, err := s.Chunk(ctx, f.buyerKey, f.issued.JobID, f.delivery.ID, f.head.Digest(), 0); err == nil {
		t.Fatal("revoked key read")
	}
	if err := s.Recheck(ctx, f.buyerKey, f.issued.JobID, f.delivery.ID, f.head.Digest()); err == nil {
		t.Fatal("revoked final response")
	}
}
func TestSDKWrongHeadAndChunkIndexCannotSelectAnotherContent(t *testing.T) {
	f := originalContent(t)
	s := contentSDK(t, f)
	f.ready(t)
	for _, bad := range []struct {
		head  string
		index int
	}{{strings.Repeat("f", 64), 0}, {f.head.Digest(), 1}, {f.head.Digest(), -1}} {
		if _, err := s.Chunk(ctx, f.buyerKey, f.issued.JobID, f.delivery.ID, bad.head, bad.index); err == nil {
			t.Fatal("content reference substituted")
		}
	}
}
func TestSDKCancellationRetiresReadWithoutReleasingCompute(t *testing.T) {
	f := originalContent(t)
	s := contentSDK(t, f)
	f.ready(t)
	v := f.job(t)
	if _, err := f.service.Cancel(ctx, f.buyer, f.issued.JobID, invocation.CancelInput{ExpectedVersion: v.Version, Reason: "BUYER_REQUEST"}, ids.New("cancel")); err != nil {
		t.Fatal(err)
	}
	if err := s.Recheck(ctx, f.buyerKey, f.issued.JobID, f.delivery.ID, f.head.Digest()); err == nil {
		t.Fatal("canceled result readable")
	}
	q, l := f.counts(t)
	if q != 0 || l != 0 {
		t.Fatal("cancel synthesized billing")
	}
	var phase string
	if err := db.Pool.QueryRow(ctx, `SELECT phase FROM p11.resource_locks WHERE resource_id=$1`, f.declaration.Scope.Resource).Scan(&phase); err != nil || phase == "FREE" {
		t.Fatal("read cancellation freed compute", err)
	}
}
