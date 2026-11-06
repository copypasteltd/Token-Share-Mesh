package integration_test

// SOURCE_ONLY. Native SQL and synthetic RSA/channel fixtures. These definitions
// have not run and confer no real personal-seller refund qualification.
import (
	"encoding/json"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/payments"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/money"
	"strings"
	"sync"
	"testing"
)

func refundReviewer(t *testing.T, r *originalRefundFixture) identity.Principal {
	t.Helper()
	p := identity.Principal{Kind: identity.UserSession, Scopes: []string{"refund:review"}}
	if e := db.Pool.QueryRow(ctx, `SELECT u.id,u.version FROM p11.evidence e JOIN p11.users u ON u.id=e.reviewed_by WHERE e.id=$1`, r.route.EvidenceRef).Scan(&p.UserID, &p.Version); e != nil {
		t.Fatal(e)
	}
	return p
}
func refundApplicationInput(t *testing.T, r *originalRefundFixture, amount int64) payments.RefundApplicationInput {
	t.Helper()
	v := payments.RefundApplicationInput{Reason: "NO_LONGER_NEEDED", Requested: 100, Evidence: []string{}}
	if e := db.Pool.QueryRow(ctx, `SELECT version FROM p11.orders WHERE id=$1`, r.payment.order.ID).Scan(&v.ExpectedOrderVersion); e != nil {
		t.Fatal(e)
	}
	v.Requested = money.Fen(amount)
	return v
}
func applyRefund(t *testing.T, r *originalRefundFixture) payments.RefundApplication {
	t.Helper()
	v, e := r.service.Apply(ctx, r.payment.buyer, r.payment.order.ID, refundApplicationInput(t, r, 100), ids.New("idem"))
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func refundApproval(t *testing.T, r *originalRefundFixture, a payments.RefundApplication) payments.RefundReviewInput {
	t.Helper()
	v, e := r.service.ReviewDetails(ctx, refundReviewer(t, r), a.ID)
	if e != nil {
		t.Fatal(e)
	}
	return payments.RefundReviewInput{ExpectedReviewVersion: v.ReviewVersion, ExpectedOrderVersion: v.OrderVersion, ExpectedPaymentVersion: v.PaymentVersion, Decision: "APPROVED", Reason: "UNUSED_BALANCE_CONFIRMED", Route: r.routeRef, Maximum: 500}
}
func approvedApplication(t *testing.T, r *originalRefundFixture) payments.RefundApplication {
	t.Helper()
	a := applyRefund(t, r)
	v, e := r.service.Review(ctx, refundReviewer(t, r), a.ID, refundApproval(t, r, a), ids.New("idem"))
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func refundRowCount(t *testing.T, table, id string) int {
	t.Helper()
	allowed := map[string]bool{"refunds": true, "refund_applications": true, "refund_machine_contexts": true}
	if !allowed[table] {
		t.Fatal("synthetic table bound")
	}
	field := "id"
	if table == "refund_machine_contexts" {
		field = "refund_id"
	}
	var n int
	if e := db.Pool.QueryRow(ctx, "SELECT count(*) FROM p11."+table+" WHERE "+field+"=$1", id).Scan(&n); e != nil {
		t.Fatal(e)
	}
	return n
}
func TestRefundApplicationIsAnIndependentRequestWithoutFinancialSideEffects(t *testing.T) {
	r := refundIntegrationFixture(t)
	a := applyRefund(t, r)
	if a.State != "REQUESTED" || a.ReviewState != "PENDING" || a.Confirmed != 0 || refundRowCount(t, "refunds", a.ID) != 0 || r.submits.Load() != 0 {
		t.Fatal("buyer request became financial action")
	}
	var events int
	if e := db.Pool.QueryRow(ctx, `SELECT count(*) FROM p11.outbox_events WHERE aggregate_id=$1 AND event_type='RefundApplicationCreated'`, a.ID).Scan(&events); e != nil || events != 1 {
		t.Fatal("request event not atomic", e)
	}
}
func TestRefundApplicationSameRequestAndNewKeyReturnSamePendingApplication(t *testing.T) {
	r := refundIntegrationFixture(t)
	in := refundApplicationInput(t, r, 100)
	key := ids.New("idem")
	a, e := r.service.Apply(ctx, r.payment.buyer, r.payment.order.ID, in, key)
	if e != nil {
		t.Fatal(e)
	}
	b, e := r.service.Apply(ctx, r.payment.buyer, r.payment.order.ID, in, key)
	if e != nil || !b.Replayed || b.ID != a.ID {
		t.Fatal("original replay changed request", e)
	}
	b, e = r.service.Apply(ctx, r.payment.buyer, r.payment.order.ID, in, ids.New("idem"))
	if e != nil || b.ID != a.ID {
		t.Fatal("new key duplicated pending application", e)
	}
	in.Requested = 101
	if _, e = r.service.Apply(ctx, r.payment.buyer, r.payment.order.ID, in, ids.New("idem")); e == nil {
		t.Fatal("different request replaced pending application")
	}
}
func TestRefundApplicationAnotherBuyerCannotReadListOrApply(t *testing.T) {
	r := refundIntegrationFixture(t)
	a := applyRefund(t, r)
	other := user(t)
	other.Scopes = append(other.Scopes, "refund:read", "refund:create")
	if _, e := r.service.Application(ctx, other, a.ID); e == nil {
		t.Fatal("cross-buyer read")
	}
	if _, e := r.service.ListApplications(ctx, other, a.Order, "", 20); e == nil {
		t.Fatal("cross-buyer list")
	}
	if _, e := r.service.Apply(ctx, other, a.Order, refundApplicationInput(t, r, 100), ids.New("idem")); e == nil {
		t.Fatal("cross-buyer application")
	}
}
func TestRefundApplicationRejectsUnrelatedEvidenceWithoutExposingContent(t *testing.T) {
	r := refundIntegrationFixture(t)
	in := refundApplicationInput(t, r, 100)
	in.Evidence = []string{r.route.EvidenceRef}
	if _, e := r.service.Apply(ctx, r.payment.buyer, r.payment.order.ID, in, ids.New("idem")); e == nil {
		t.Fatal("evidence without original buyer/order accepted")
	}
}
func TestRefundApplicationReviewerCannotApproveOwnBuyerOrder(t *testing.T) {
	r := refundIntegrationFixture(t)
	a := applyRefund(t, r)
	p := r.payment.buyer
	p.Scopes = append(p.Scopes, "refund:review")
	exec(t, `INSERT INTO p11.role_grants(id,user_id,role_code,scope_ref,granted_by) VALUES($1,$2,'PAYMENT_REVIEWER','self',$2)`, ids.New("role"), p.UserID)
	if _, e := r.service.Review(ctx, p, a.ID, refundApproval(t, r, a), ids.New("idem")); e == nil {
		t.Fatal("buyer approved own financial request")
	}
}
func TestRefundApplicationApprovalCreatesExactOriginalFinancialChainAtomically(t *testing.T) {
	r := refundIntegrationFixture(t)
	a := approvedApplication(t, r)
	if a.ReviewState != "APPROVED" || a.State != "REQUESTED" || a.ReviewVersion != 2 || refundRowCount(t, "refunds", a.ID) != 1 || refundRowCount(t, "refund_machine_contexts", a.ID) != 1 || r.submits.Load() != 0 {
		t.Fatal("approval not composed with original refund")
	}
	var same bool
	if e := db.Pool.QueryRow(ctx, `SELECT e.reviewed_by=a.reviewed_by AND e.scope_snapshot->>'refund_application_id'=a.id AND c.decision_evidence_id=e.id AND f.amount_fen=a.requested_fen FROM p11.refund_applications a JOIN p11.evidence e ON e.id=a.decision_evidence_id JOIN p11.refund_machine_contexts c ON c.refund_id=a.id JOIN p11.refunds f ON f.id=a.id WHERE a.id=$1`, a.ID).Scan(&same); e != nil || !same {
		t.Fatal("review/source native chain differs", e)
	}
}
func TestRefundApplicationRejectedReviewNeverCreatesChannelFailureOrReleasesAnotherHold(t *testing.T) {
	r := refundIntegrationFixture(t)
	a := applyRefund(t, r)
	in := refundApproval(t, r, a)
	in.Decision = "REJECTED"
	in.Reason = "INSUFFICIENT_EVIDENCE"
	in.Route = ""
	in.Maximum = 0
	v, e := r.service.Review(ctx, refundReviewer(t, r), a.ID, in, ids.New("idem"))
	if e != nil || v.ReviewState != "REJECTED" || v.State != "REQUESTED" || v.Confirmed != 0 || refundRowCount(t, "refunds", a.ID) != 0 {
		t.Fatal("rejection fabricated channel outcome", e)
	}
}
func TestRefundApplicationFailedCapacityRollsBackApprovalEvidenceAndFinancialIntent(t *testing.T) {
	r := refundIntegrationFixture(t)
	a := applyRefund(t, r)
	in := refundApproval(t, r, a)
	exec(t, `UPDATE p11.orders SET budget_unverified_fen=1 WHERE id=$1`, a.Order)
	if _, e := r.service.Review(ctx, refundReviewer(t, r), a.ID, in, ids.New("idem")); e == nil {
		t.Fatal("unverified work became refundable consideration")
	}
	v, e := r.service.Application(ctx, r.payment.buyer, a.ID)
	if e != nil || v.ReviewState != "PENDING" || refundRowCount(t, "refunds", a.ID) != 0 {
		t.Fatal("failed approval partially committed", e)
	}
	var n int
	if e = db.Pool.QueryRow(ctx, `SELECT count(*) FROM p11.evidence WHERE scope_snapshot->>'refund_application_id'=$1`, a.ID).Scan(&n); e != nil || n != 0 {
		t.Fatal("failed approval left independent evidence", e)
	}
}
func TestRefundApplicationStaleReviewOrderOrPaymentVersionsCannotApprove(t *testing.T) {
	r := refundIntegrationFixture(t)
	a := applyRefund(t, r)
	for n := 0; n < 3; n++ {
		in := refundApproval(t, r, a)
		switch n {
		case 0:
			in.ExpectedReviewVersion++
		case 1:
			in.ExpectedOrderVersion++
		case 2:
			in.ExpectedPaymentVersion++
		}
		if _, e := r.service.Review(ctx, refundReviewer(t, r), a.ID, in, ids.New("idem")); e == nil {
			t.Fatal("stale approval version accepted")
		}
	}
}
func TestRefundApplicationRevokedReviewerCannotReplayFinancialApproval(t *testing.T) {
	r := refundIntegrationFixture(t)
	a := applyRefund(t, r)
	p := refundReviewer(t, r)
	in, key := refundApproval(t, r, a), ids.New("idem")
	if _, e := r.service.Review(ctx, p, a.ID, in, key); e != nil {
		t.Fatal(e)
	}
	exec(t, `UPDATE p11.role_grants SET revoked_at=clock_timestamp() WHERE user_id=$1 AND role_code='PAYMENT_REVIEWER'`, p.UserID)
	if _, e := r.service.Review(ctx, p, a.ID, in, key); e == nil {
		t.Fatal("revoked reviewer replay bypassed current authority")
	}
}
func TestRefundApplicationRevocationBeforeDispatchHoldsOriginalAmount(t *testing.T) {
	r := refundIntegrationFixture(t)
	a := approvedApplication(t, r)
	v, e := r.service.RevokeReview(ctx, refundReviewer(t, r), a.ID, payments.RefundRevokeInput{ExpectedReviewVersion: a.ReviewVersion, Reason: "AUTHORITY_REVOKED"}, ids.New("idem"))
	if e != nil || v.ReviewState != "REVOKED" || v.State != "REQUESTED" {
		t.Fatal("revocation rewrote original financial outcome", e)
	}
	if worked, e := r.service.RecoverOne(ctx); e != nil || !worked || r.submits.Load() != 0 {
		t.Fatal("revoked decision submitted", e)
	}
	v, e = r.service.Application(ctx, r.payment.buyer, a.ID)
	if e != nil || !v.ReviewRequired || refundRowCount(t, "refunds", a.ID) != 1 {
		t.Fatal("revocation silently freed held money", e)
	}
}
func TestRefundApplicationRevocationAfterDispatchDoesNotUndoSubmittedMoney(t *testing.T) {
	r := refundIntegrationFixture(t)
	a := approvedApplication(t, r)
	if _, e := r.service.RecoverOne(ctx); e != nil {
		t.Fatal(e)
	}
	v, e := r.service.RevokeReview(ctx, refundReviewer(t, r), a.ID, payments.RefundRevokeInput{ExpectedReviewVersion: a.ReviewVersion, Reason: "AUTHORITY_REVOKED"}, ids.New("idem"))
	if e != nil || v.State != "PROCESSING" || v.ReviewState != "REVOKED" || r.submits.Load() != 1 || v.Confirmed != 0 {
		t.Fatal("review revocation erased original dispatch", e)
	}
}
func TestRefundApplicationConcurrentReviewCreatesOnlyOneNativeRefund(t *testing.T) {
	r := refundIntegrationFixture(t)
	a := applyRefund(t, r)
	in, p := refundApproval(t, r, a), refundReviewer(t, r)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := r.service.Review(ctx, p, a.ID, in, ids.New("idem")); results <- e }()
	}
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		}
	}
	if success != 1 || refundRowCount(t, "refunds", a.ID) != 1 {
		t.Fatal("two reviews duplicated original financial intent")
	}
}
func TestRefundApplicationSellerCanReadButCannotSubmitBuyerApplication(t *testing.T) {
	r := refundIntegrationFixture(t)
	a := applyRefund(t, r)
	p := identity.Principal{UserID: r.payment.binding.Seller, Kind: identity.UserSession, Scopes: []string{"refund:read", "refund:create"}}
	if e := db.Pool.QueryRow(ctx, `SELECT version FROM p11.users WHERE id=$1`, p.UserID).Scan(&p.Version); e != nil {
		t.Fatal(e)
	}
	if _, e := r.service.Application(ctx, p, a.ID); e != nil {
		t.Fatal("original seller could not read aftersale", e)
	}
	if _, e := r.service.Apply(ctx, p, a.Order, refundApplicationInput(t, r, 100), ids.New("idem")); e == nil {
		t.Fatal("seller impersonated original buyer")
	}
}
func TestRefundApplicationQueueIsBoundedAndRequiresCurrentIndependentRole(t *testing.T) {
	r := refundIntegrationFixture(t)
	a := applyRefund(t, r)
	p := refundReviewer(t, r)
	page, e := r.service.ListReviews(ctx, p, "", 1)
	if e != nil || len(page.Items) != 1 {
		t.Fatal("bounded queue unavailable", e)
	}
	if _, e = r.service.ReviewDetails(ctx, p, a.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = r.service.ListReviews(ctx, p, "", 51); e == nil {
		t.Fatal("unbounded queue accepted")
	}
	p.Kind = identity.APIKey
	if _, e = r.service.ListReviews(ctx, p, "", 1); e == nil {
		t.Fatal("API key approved financial review scope")
	}
}
func TestRefundApplicationOriginalRequestAndApprovalEvidenceAreImmutable(t *testing.T) {
	r := refundIntegrationFixture(t)
	a := approvedApplication(t, r)
	for _, q := range []string{`UPDATE p11.refund_applications SET requested_fen=requested_fen+1 WHERE id=$1`, `DELETE FROM p11.refund_applications WHERE id=$1`, `UPDATE p11.evidence SET scope_snapshot=scope_snapshot||'{"refund_amount_fen":"101"}'::jsonb WHERE id=(SELECT decision_evidence_id FROM p11.refund_applications WHERE id=$1)`} {
		if _, e := db.Pool.Exec(ctx, q, a.ID); e == nil {
			t.Fatal("original request or approval was mutable")
		}
	}
}
func TestRefundApplicationManualApprovalWithoutNativeContextCannotCommit(t *testing.T) {
	r := refundIntegrationFixture(t)
	a := applyRefund(t, r)
	p := refundReviewer(t, r)
	if _, e := db.Pool.Exec(ctx, `UPDATE p11.refund_applications SET review_state='APPROVED',review_version=review_version+1,review_reason_code='UNUSED_BALANCE_CONFIRMED',reviewed_by=$2,decision_evidence_id=$3,decision_digest=$4 WHERE id=$1`, a.ID, p.UserID, r.route.EvidenceRef, r.route.EvidenceHash); e == nil {
		t.Fatal("manual approval omitted exact native financial chain")
	}
}
func TestRefundApplicationMetadataContainsNoRawReceiptOrGeneratedContent(t *testing.T) {
	r := refundIntegrationFixture(t)
	a := approvedApplication(t, r)
	v, e := r.service.ReviewDetails(ctx, refundReviewer(t, r), a.ID)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	for _, text := range []string{"payment_proof", "client_session", "private_key", "encrypted_raw", "prompt", "data:image", "alipay_trade_refund_response"} {
		if strings.Contains(string(raw), text) {
			t.Fatal("content or private financial envelope escaped public metadata")
		}
	}
}
func TestRefundApplicationRejectsInvalidAmountReasonAndDuplicateEvidence(t *testing.T) {
	r := refundIntegrationFixture(t)
	for n := 0; n < 4; n++ {
		in := refundApplicationInput(t, r, 100)
		switch n {
		case 0:
			in.Requested = 0
		case 1:
			in.Requested = 5001
		case 2:
			in.Reason = "arbitrary_private_prompt"
		case 3:
			in.Evidence = []string{"ev_original", "ev_original"}
		}
		if _, e := r.service.Apply(ctx, r.payment.buyer, r.payment.order.ID, in, ids.New("idem")); e == nil {
			t.Fatal("unbounded or unsupported application accepted")
		}
	}
}
