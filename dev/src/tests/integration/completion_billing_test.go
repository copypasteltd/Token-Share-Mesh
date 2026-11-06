package integration_test

// SOURCE_ONLY. Positive definitions use the full synthetic signed original
// Machine Pay/allocation/native-admission chain. No SETTLED/READY bypass,
// skipped positive assertion, physical device or real meter is substituted.
import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"p11.local/compute/client/protocol/devicecontrol"
	relay "p11.local/compute/gateway/core"
	"p11.local/compute/internal/dispatch/device"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/invocation"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/internal/testsupport"
	"p11.local/compute/internal/usage"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/resultwire"
)

type completionFixture struct {
	claimFixture
	service     *invocation.Completion
	buyer       identity.Principal
	declaration resultwire.Delivery
	statement   resultwire.Statement
}

func completionOriginal(t *testing.T) completionFixture {
	return completionOriginalBuyer(t, nil)
}
func completionOriginalBuyer(t *testing.T, originalBuyer *identity.Principal) completionFixture {
	t.Helper()
	requireDB(t)
	offer := testsupport.SeedNativeOfferForProtocol(t, ctx, db, ids.New("region"), string(relay.ChatText))
	paid := machineFixtureOfferBuyer(t, offer, originalBuyer)
	payment := paid.paidOriginal(t)
	if worked, e := paid.machine.RecoverFundingOne(ctx); e != nil || !worked {
		t.Fatal("full original allocation", worked, e)
	}
	route := fundingRoute(t, paid, payment)
	_, _, caller := routeKey(t, paid.buyer, route.ID)
	request, body := executionBody(relay.ChatText, ids.New("original"))
	config := executionConfig(t)
	issuer := configuredExecution(t, config)
	readyExecution(t, issuer, caller, request, body)
	issued := issueExecution(t, issuer, caller, request)
	claims, e := invocation.NewClaims(db, "SANDBOX", map[string]ed25519.PublicKey{config.SigningKeyID: config.SigningPrivateKey.Public().(ed25519.PublicKey)})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(claims.Close)
	in := devicecontrol.LeaseClaim{Device: offer.Device, Capability: offer.Capability, ClaimKey: ids.New("claim"), Version: 1}
	raw, _ := json.Marshal(in)
	binding := devicecontrol.Binding{Method: "POST", Authority: "127.0.0.1:8787", URI: "/v1/p11/runner/leases/claim", ContentType: "application/json", IdempotencyKey: in.ClaimKey}
	proof, e := devicecontrol.Sign(offer.DeviceKey, offer.Device, binding, raw, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	principal, e := (device.Service{DB: db}).Authenticate(ctx, proof, binding, raw)
	if e != nil {
		t.Fatal(e)
	}
	base := claimFixture{f: routeFixture{buyer: paid.buyer, offer: offer, order: paid.order}, p: principal, buyerKey: caller, in: in, issued: issued, claims: claims, issuer: issuer, request: request, config: config}
	if _, e = base.claim(); e != nil {
		t.Fatal(e)
	}
	meterAuthority, reviewer := user(t), user(t)
	for _, grant := range []struct{ user, role string }{{meterAuthority.UserID, "METER_AUTHORITY"}, {reviewer.UserID, "METER_REVIEWER"}} {
		exec(t, `INSERT INTO p11.role_grants(id,user_id,role_code,scope_ref,granted_by) VALUES($1,$2,$3,'self',$2)`, ids.New("grant"), grant.user, grant.role)
	}
	_, meterKey, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	pin := usage.Pin{KeyID: ids.New("meter"), Public: base64.RawURLEncoding.EncodeToString(meterKey.Public().(ed25519.PublicKey)), Authority: meterAuthority.UserID, Evidence: ids.New("evidence")}
	m := *issued.Manifest
	tokenizer := strings.Repeat("c", 64)
	scope, _ := json.Marshal(map[string]string{"profile_id": resultwire.UsageProfile, "key_id": pin.KeyID, "public_key": pin.Public, "authority_id": pin.Authority, "model_digest": m.ModelDigest, "tokenizer_digest": tokenizer, "template_digest": m.RecipeDigest, "backend_id": m.EngineRelease, "kernel_environment_digest": m.EnvironmentDigest, "counter_profile_id": "OPENAI_TOTALS_1"})
	pin.EvidenceHash = outbox.Digest(scope)
	exec(t, `INSERT INTO p11.evidence(id,evidence_type,content_ref,digest,scope_snapshot,decision,reviewed_by,valid_from,valid_until) VALUES($1,'TOKEN_METER_AUTHORITY_1',$1,$2,$3,'APPROVED',$4,clock_timestamp()-interval '1 minute',clock_timestamp()+interval '10 minutes')`, pin.Evidence, pin.EvidenceHash, scope, reviewer.UserID)
	verifier, e := usage.New([]usage.Pin{pin})
	if e != nil {
		t.Fatal(e)
	}
	service, e := invocation.NewCompletion(claims, verifier)
	if e != nil {
		t.Fatal(e)
	}
	declaration, e := resultwire.SignDelivery(offer.DeviceKey, resultwire.Delivery{Profile: resultwire.DeliveryProfile, Scope: resultwire.Original(*issued.Lease), Result: ids.New("result"), Revision: 1, OutputHash: outbox.Digest([]byte("synthetic_result")), OutputBytes: 16, InputTokens: 13, OutputTokens: 7, Finish: "eog", Completed: time.Now().UTC().Format(time.RFC3339Nano)})
	if e != nil {
		t.Fatal(e)
	}
	input, output := int64(13), int64(7)
	statement, e := resultwire.SignStatement(meterKey, resultwire.Statement{Profile: resultwire.UsageProfile, Scope: declaration.Scope, DeliveryHash: resultwire.Digest(declaration), KeyID: pin.KeyID, Evidence: pin.Evidence, Candidate: resultwire.Candidate{Model: m.ModelDigest, Tokenizer: tokenizer, Template: m.RecipeDigest, Backend: m.EngineRelease, CounterProfile: "OPENAI_TOTALS_1", Raw: resultwire.Counts{Input: &input, Output: &output}}, Recorded: time.Now().UTC().Format(time.RFC3339Nano)})
	if e != nil {
		t.Fatal(e)
	}
	buyer := paid.buyer
	buyer.Scopes = append(buyer.Scopes, "job:read", "job:decide", "job:cancel", "bill:read")
	return completionFixture{base, service, buyer, declaration, statement}
}
func (f completionFixture) register(t *testing.T) invocation.ResultView {
	t.Helper()
	r, e := f.service.Register(ctx, f.p, f.issued.AttemptID, f.declaration, ids.New("delivery"))
	if e != nil {
		t.Fatal(e)
	}
	var v invocation.ResultView
	if e = json.Unmarshal(r.Body, &v); e != nil {
		t.Fatal(e)
	}
	return v
}
func (f completionFixture) job(t *testing.T) invocation.JobView {
	t.Helper()
	v, e := f.service.Job(ctx, f.buyer, f.issued.JobID)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func (f completionFixture) decision(t *testing.T, d invocation.ResultView) invocation.DecisionInput {
	t.Helper()
	j := f.job(t)
	return invocation.DecisionInput{ExpectedVersion: j.Version, Delivery: d.ID, Revision: d.Revision, ManifestHash: d.ManifestHash, ReviewedOutputHash: d.OutputHash, Decision: "ACCEPT"}
}
func (f completionFixture) verify(t *testing.T) {
	t.Helper()
	if _, e := f.service.RecordUsage(ctx, f.p, f.issued.AttemptID, f.statement, ids.New("usage")); e != nil {
		t.Fatal(e)
	}
}
func (f completionFixture) counts(t *testing.T) (qual, journal int) {
	t.Helper()
	if e := db.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM p11.trade_qualifications WHERE order_id=$1),(SELECT count(*) FROM p11.journal_transactions WHERE order_id=$1)`, f.f.order.ID).Scan(&qual, &journal); e != nil {
		t.Fatal(e)
	}
	return
}

func TestCompletionBuyerAcceptanceAloneKeepsOriginalBudgetHeld(t *testing.T) {
	f := completionOriginal(t)
	d := f.register(t)
	in := f.decision(t, d)
	if _, e := f.service.Decide(ctx, f.buyer, f.issued.JobID, in, ids.New("decision")); e != nil {
		t.Fatal(e)
	}
	j := f.job(t)
	q, l := f.counts(t)
	if j.State != "ACCEPTED" || j.Billing.State != "HELD_PENDING_EVIDENCE" || q != 0 || l != 0 {
		t.Fatal(j, q, l)
	}
	var state string
	if e := db.Pool.QueryRow(ctx, `SELECT state FROM p11.budget_reservations WHERE purpose_ref=$1`, j.ID).Scan(&state); e != nil || state != "RESERVED" {
		t.Fatal(state, e)
	}
}
func TestCompletionVerifiedUsageBeforeAcceptanceDoesNotCreateCharge(t *testing.T) {
	f := completionOriginal(t)
	f.register(t)
	f.verify(t)
	q, l := f.counts(t)
	if q != 0 || l != 0 {
		t.Fatal("meter assertion auto-accepted result", q, l)
	}
}
func TestCompletionAcceptanceAndUsageCommitOneBalancedOriginalBill(t *testing.T) {
	f := completionOriginal(t)
	d := f.register(t)
	f.verify(t)
	in := f.decision(t, d)
	key := ids.New("decision")
	if _, e := f.service.Decide(ctx, f.buyer, f.issued.JobID, in, key); e != nil {
		t.Fatal(e)
	}
	if _, e := f.service.Decide(ctx, f.buyer, f.issued.JobID, in, key); e != nil {
		t.Fatal(e)
	}
	j := f.job(t)
	q, l := f.counts(t)
	if q != 1 || l != 1 || j.Billing.Gross < 1 || j.Billing.Journal == "" {
		t.Fatal(j, q, l)
	}
	var lines, sum int64
	if e := db.Pool.QueryRow(ctx, `SELECT count(*),sum(amount_fen)::bigint FROM p11.journal_entries WHERE journal_id=$1`, j.Billing.Journal).Scan(&lines, &sum); e != nil || lines < 2 || sum != 0 {
		t.Fatal(lines, sum, e)
	}
}
func TestCompletionLateVerifiedUsageCanBillAlreadyAcceptedOriginal(t *testing.T) {
	f := completionOriginal(t)
	d := f.register(t)
	if _, e := f.service.Decide(ctx, f.buyer, f.issued.JobID, f.decision(t, d), ids.New("decision")); e != nil {
		t.Fatal(e)
	}
	f.verify(t)
	q, l := f.counts(t)
	if q != 1 || l != 1 {
		t.Fatal("accepted original not resolved exactly once", q, l)
	}
}
func TestCompletionMissingMeterConfigurationNeverPromotesNativeCounts(t *testing.T) {
	f := completionOriginal(t)
	f.register(t)
	service, e := invocation.NewCompletion(f.claims, nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = service.RecordUsage(ctx, f.p, f.issued.AttemptID, f.statement, ids.New("usage")); !fault.Is(e, fault.Unavailable) {
		t.Fatal(e)
	}
	q, l := f.counts(t)
	if q != 0 || l != 0 {
		t.Fatal(q, l)
	}
}
func TestCompletionChangedDeliveryCannotReplaceOriginalWithAnotherKey(t *testing.T) {
	f := completionOriginal(t)
	d := f.register(t)
	changed := f.declaration
	changed.OutputBytes++
	changed, e := resultwire.SignDelivery(f.f.offer.DeviceKey, changed)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.service.Register(ctx, f.p, f.issued.AttemptID, changed, ids.New("delivery")); !fault.Is(e, fault.Conflict) {
		t.Fatal(e)
	}
	j := f.job(t)
	if len(j.Results) != 1 || j.Results[0].ID != d.ID {
		t.Fatal(j)
	}
}
func TestCompletionDecisionRejectsChangedVersionAndLocallyReviewedDigest(t *testing.T) {
	f := completionOriginal(t)
	d := f.register(t)
	in := f.decision(t, d)
	in.ExpectedVersion++
	if _, e := f.service.Decide(ctx, f.buyer, f.issued.JobID, in, ids.New("decision")); !fault.Is(e, fault.Conflict) {
		t.Fatal(e)
	}
	in = f.decision(t, d)
	in.ReviewedOutputHash = strings.Repeat("0", 64)
	if _, e := f.service.Decide(ctx, f.buyer, f.issued.JobID, in, ids.New("decision")); !fault.Is(e, fault.Conflict) {
		t.Fatal(e)
	}
}
func TestCompletionCancelAndAcceptanceHaveOneOriginalWinner(t *testing.T) {
	f := completionOriginal(t)
	d := f.register(t)
	in := f.decision(t, d)
	var w sync.WaitGroup
	results := make(chan error, 2)
	w.Add(2)
	go func() {
		defer w.Done()
		_, e := f.service.Decide(ctx, f.buyer, f.issued.JobID, in, ids.New("decision"))
		results <- e
	}()
	go func() {
		defer w.Done()
		_, e := f.service.Cancel(ctx, f.buyer, f.issued.JobID, invocation.CancelInput{ExpectedVersion: in.ExpectedVersion, Reason: "BUYER_REQUEST"}, ids.New("cancel"))
		results <- e
	}()
	w.Wait()
	close(results)
	wins := 0
	for e := range results {
		if e == nil {
			wins++
		} else if !fault.Is(e, fault.Conflict) {
			t.Fatal(e)
		}
	}
	if wins != 1 {
		t.Fatal("multiple terminal outcomes", wins)
	}
}
func TestCompletionLateResultCannotReviveCanceledJobOrFreePhysicalOwner(t *testing.T) {
	f := completionOriginal(t)
	j := f.job(t)
	if _, e := f.service.Cancel(ctx, f.buyer, j.ID, invocation.CancelInput{ExpectedVersion: j.Version, Reason: "BUYER_REQUEST"}, ids.New("cancel")); e != nil {
		t.Fatal(e)
	}
	d := f.register(t)
	j = f.job(t)
	if d.State != "STALE" || j.State != "CANCELED" {
		t.Fatal(j, d)
	}
	var phase, owner string
	if e := db.Pool.QueryRow(ctx, `SELECT phase,owner_attempt_id FROM p11.resource_locks WHERE resource_id=$1`, f.issued.Lease.Resource).Scan(&phase, &owner); e != nil || phase != "STOPPING" || owner != f.issued.AttemptID {
		t.Fatal(phase, owner, e)
	}
}
func TestCompletionRevokedMeterApprovalKeepsUnbilledAcceptanceHeld(t *testing.T) {
	f := completionOriginal(t)
	d := f.register(t)
	f.verify(t)
	exec(t, `UPDATE p11.evidence SET decision='REVOKED' WHERE id=$1`, f.statement.Evidence)
	if _, e := f.service.Decide(ctx, f.buyer, f.issued.JobID, f.decision(t, d), ids.New("decision")); e != nil {
		t.Fatal(e)
	}
	q, l := f.counts(t)
	if q != 0 || l != 0 || f.job(t).Billing.State != "HELD_PENDING_EVIDENCE" {
		t.Fatal(q, l)
	}
}
func TestCompletionChangedMeterScopeKeepsUnbilledAcceptanceHeld(t *testing.T) {
	for _, mutation := range []struct{ name, key, value string }{
		{"tokenizer", "tokenizer_digest", strings.Repeat("f", 64)},
		{"public_key", "public_key", base64.RawURLEncoding.EncodeToString(make([]byte, ed25519.PublicKeySize))},
		{"authority", "authority_id", ids.New("authority")},
		{"unexpected_field", "unapproved_scope", "extra"},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			f := completionOriginal(t)
			d := f.register(t)
			f.verify(t)
			exec(t, `UPDATE p11.evidence SET scope_snapshot=jsonb_set(scope_snapshot,ARRAY[$2::text],to_jsonb($3::text),true) WHERE id=$1`, f.statement.Evidence, mutation.key, mutation.value)
			if _, e := f.service.Decide(ctx, f.buyer, f.issued.JobID, f.decision(t, d), ids.New("decision")); e != nil {
				t.Fatal(e)
			}
			q, l := f.counts(t)
			if q != 0 || l != 0 || f.job(t).Billing.State != "HELD_PENDING_EVIDENCE" {
				t.Fatal("changed approval scope consumed original reservation", q, l)
			}
		})
	}
}

func TestCompletionRejectedResultNeverCreatesQualificationOrReleasesUnknownUsage(t *testing.T) {
	f := completionOriginal(t)
	d := f.register(t)
	f.verify(t)
	in := f.decision(t, d)
	in.Decision = "REJECT"
	if _, e := f.service.Decide(ctx, f.buyer, f.issued.JobID, in, ids.New("decision")); e != nil {
		t.Fatal(e)
	}
	j := f.job(t)
	q, l := f.counts(t)
	if j.State != "FAILED" || q != 0 || l != 0 {
		t.Fatal(j, q, l)
	}
}
func TestCompletionBillIsParticipantScopedProvisionalAndNotPayout(t *testing.T) {
	f := completionOriginal(t)
	if _, e := f.service.Bill(ctx, user(t), f.f.order.ID); e == nil {
		t.Fatal("foreign bill exposed")
	}
	v, e := f.service.Bill(ctx, f.buyer, f.f.order.ID)
	if e != nil || v.Payout != "NOT_QUALIFIED" || v.ChannelCost != "UNKNOWN" || v.IsFinal || v.Watermark.IsZero() {
		t.Fatal(v, e)
	}
}
func TestCompletionArchivesAndDecisionCannotBeMutatedOrDeleted(t *testing.T) {
	f := completionOriginal(t)
	d := f.register(t)
	f.verify(t)
	if _, e := f.service.Decide(ctx, f.buyer, f.issued.JobID, f.decision(t, d), ids.New("decision")); e != nil {
		t.Fatal(e)
	}
	for _, q := range []string{`DELETE FROM p11.native_result_deliveries WHERE job_id=$1`, `DELETE FROM p11.native_usage_verifications WHERE job_id=$1`, `DELETE FROM p11.acceptances WHERE job_id=$1`, `UPDATE p11.accepted_usage_settlements SET gross_fen=0 WHERE job_id=$1`} {
		if _, e := db.Pool.Exec(ctx, q, f.issued.JobID); e == nil {
			t.Fatal("immutable original financial/result fact changed")
		}
	}
}
func TestCompletionResultCanRegisterAfterCleanupAndPhysicalResourceReuse(t *testing.T) {
	f := completionOriginal(t)
	l := *f.issued.Lease
	report, e := devicecontrol.SignCleanup(f.f.offer.DeviceKey, l, devicecontrol.CleanupWrite{Lease: l.ID, Fence: l.Fence, ManifestHash: strings.Repeat("d", 64), State: "ACKED", WorkStopped: true, TokensRevoked: true, WorkspaceCleared: true, Recorded: time.Now().UTC().Format(time.RFC3339Nano), Profile: devicecontrol.CleanupProfile})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.claims.RecordCleanup(ctx, f.p, l.Attempt, report, ids.New("cleanup")); e != nil {
		t.Fatal(e)
	}
	request, body := executionBody(relay.ChatText, ids.New("different_original"))
	readyExecution(t, f.issuer, f.buyerKey, request, body)
	next := issueExecution(t, f.issuer, f.buyerKey, request)
	d := f.register(t)
	if d.State != "WAITING_BUYER" || next.Lease.Resource != l.Resource {
		t.Fatal("original custody depended on current physical owner", d, next.Lease)
	}
	var owner, phase string
	if e = db.Pool.QueryRow(ctx, `SELECT owner_attempt_id,phase FROM p11.resource_locks WHERE resource_id=$1`, l.Resource).Scan(&owner, &phase); e != nil || owner != next.AttemptID || phase != "ACTIVE" {
		t.Fatal("original publication changed next owner", owner, phase, e)
	}
}
func TestCompletionDeviceKeyCannotForgeIndependentlyPinnedMeter(t *testing.T) {
	f := completionOriginal(t)
	f.register(t)
	fake, e := resultwire.SignStatement(f.f.offer.DeviceKey, f.statement)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.service.RecordUsage(ctx, f.p, f.issued.AttemptID, fake, ids.New("usage")); !fault.Is(e, fault.Forbidden) {
		t.Fatal("device promoted its own counters", e)
	}
	q, l := f.counts(t)
	if q != 0 || l != 0 {
		t.Fatal(q, l)
	}
}
