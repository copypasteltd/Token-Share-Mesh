package integration_test

// SOURCE_ONLY: PostgreSQL transaction definitions with independent synthetic
// reviewers and signed in-memory Alipay responses. Not real channel approval,
// merchant eligibility, refunds or physical cleanup; none are executed now.
import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"p11.local/compute/internal/payments"
	"p11.local/compute/shared/ids"
)

type originalRefundFixture struct {
	payment          *machinePaymentFixture
	paid             payments.Payment
	route            payments.RefundRoute
	routeRef         string
	service          *payments.RefundService
	channel          *payments.AlipayRefund
	submits, queries atomic.Int32
	unknownSubmit    bool
	onCall           func(string, string)
}

func refundIntegrationFixture(t *testing.T) *originalRefundFixture {
	t.Helper()
	f := machineFixture(t)
	pending := f.pending(t)
	if _, e := f.machine.SubmitProof(ctx, f.buyer, pending.ID, f.proof(t, "refund_original_session"), ids.New("idem")); e != nil {
		t.Fatal(e)
	}
	if worked, e := f.machine.RecoverOne(ctx); e != nil || !worked {
		t.Fatal("synthetic original verify", e)
	}
	paid, e := f.machine.Get(ctx, f.buyer, pending.ID)
	if e != nil {
		t.Fatal(e)
	}
	r := &originalRefundFixture{payment: f, paid: paid, routeRef: ids.New("rroute")}
	r.route = payments.RefundRoute{Profile: payments.RefundProfile, Revision: ids.New("rr"), OriginalBindingHash: f.binding.Digest(), EvidenceRef: f.policy.RefundEvidenceRef, EvidenceHash: f.policy.RefundEvidenceHash, ValidFrom: f.binding.ValidFrom, ValidUntil: f.binding.ValidUntil, ResolveUntil: f.binding.ResolveUntil}
	f.buyer.Scopes = append(f.buyer.Scopes, "refund:create", "refund:read")
	extra, _ := json.Marshal(map[string]string{"refund_profile": payments.RefundProfile, "refund_route_revision": r.route.Revision, "refund_route_hash": r.route.Digest(), "refund_methods": "alipay.trade.refund,alipay.trade.fastpay.refund.query"})
	exec(t, `UPDATE p11.evidence SET scope_snapshot=scope_snapshot||$2::jsonb WHERE id=$1`, r.route.EvidenceRef, extra)
	keys, e := machineKeys()
	if e != nil {
		t.Fatal(e)
	}
	r.channel, e = payments.NewAlipayRefund(payments.AlipayRefundConfig{Channel: payments.AlipayConfig{Binding: f.binding, PrivateKey: keys.app, AlipayPublicKey: &keys.provider.PublicKey,
		Transport: machineTestTransport(func(request *http.Request) (*http.Response, error) {
			if request.ParseForm() != nil {
				return nil, errors.New("synthetic refund form")
			}
			method := request.PostForm.Get("method")
			var biz map[string]string
			if json.Unmarshal([]byte(request.PostForm.Get("biz_content")), &biz) != nil {
				return nil, errors.New("synthetic refund business fields")
			}
			var id, original string
			var amount, total int64
			if e := db.Pool.QueryRow(ctx, `SELECT f.id,p.original_request_key,f.amount_fen,p.amount_fen FROM p11.refunds f JOIN p11.payments p ON p.id=f.payment_id WHERE f.payment_id=$1 AND f.original_request_key=$2`, paid.ID, biz["out_request_no"]).Scan(&id, &original, &amount, &total); e != nil {
				return nil, e
			}
			if r.onCall != nil {
				r.onCall(method, id)
			}
			fields := map[string]any{"code": "10000", "trade_no": f.trade, "out_trade_no": original}
			if method == "alipay.trade.refund" {
				r.submits.Add(1)
				if r.unknownSubmit {
					return nil, errors.New("synthetic result lost after original submit")
				}
				fields["refund_fee"] = fmt.Sprintf("%d.%02d", amount/100, amount%100)
				fields["fund_change"] = "Y"
			} else if method == "alipay.trade.fastpay.refund.query" {
				r.queries.Add(1)
				fields["out_request_no"] = biz["out_request_no"]
				fields["refund_amount"] = fmt.Sprintf("%d.%02d", amount/100, amount%100)
				fields["total_amount"] = fmt.Sprintf("%d.%02d", total/100, total%100)
				fields["refund_status"] = "REFUND_SUCCESS"
			} else {
				return nil, errors.New("unexpected synthetic refund method")
			}
			node, _ := json.Marshal(fields)
			sum := sha256.Sum256(node)
			signature, e := rsa.SignPKCS1v15(rand.Reader, keys.provider, crypto.SHA256, sum[:])
			if e != nil {
				return nil, e
			}
			envelope := fmt.Sprintf(`{%q:%s,"sign":%q}`, strings.ReplaceAll(method, ".", "_")+"_response", node, base64.StdEncoding.EncodeToString(signature))
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(envelope))}, nil
		})}, Route: r.route})
	if e != nil {
		t.Fatal(e)
	}
	r.service, e = payments.NewRefundService(f.machine, []payments.RefundRegistration{{Ref: r.routeRef, OriginalCapability: f.capability, Channel: r.channel}})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		closeCtx, end := context.WithTimeout(context.Background(), 5*time.Second)
		defer end()
		if r.service.Close(closeCtx) != nil || r.channel.Close(closeCtx) != nil {
			t.Error("original refund owner did not join")
		}
	})
	return r
}
func (r *originalRefundFixture) decision(t *testing.T, amount, maximum int64) payments.OriginalRefundInput {
	t.Helper()
	var orderVersion, paymentVersion int64
	var paidHash, reviewer, contractHash string
	if e := db.Pool.QueryRow(ctx, `SELECT o.version,p.version,c.last_evidence_hash,e.reviewed_by,o.contract_hash FROM p11.orders o JOIN p11.payments p ON p.order_id=o.id JOIN p11.payment_machine_contexts c ON c.payment_id=p.id JOIN p11.evidence e ON e.id=$3 WHERE o.id=$1 AND p.id=$2`, r.payment.order.ID, r.paid.ID, r.route.EvidenceRef).Scan(&orderVersion, &paymentVersion, &paidHash, &reviewer, &contractHash); e != nil {
		t.Fatal(e)
	}
	b, p := r.payment.binding, r.payment.policy
	scope := map[string]string{"payment_profile": payments.Profile, "seller_id": b.Seller, "app_id": b.AppID, "service_id": b.ServiceID, "environment": b.Environment, "revision": b.Revision, "authority_subject": b.AuthoritySubject, "signing_key_hash": b.SigningKeyHash, "alipay_key_hash": b.AlipayKeyHash, "sku_id": p.SKU, "mode": p.Mode, "privacy": p.Privacy, "collection": p.Collection,
		"refund_profile": payments.RefundProfile, "refund_route_revision": r.route.Revision, "refund_route_hash": r.route.Digest(), "refund_methods": "alipay.trade.refund,alipay.trade.fastpay.refund.query", "refund_policy": "UNUSED_BALANCE_1", "payment_id": r.paid.ID, "order_id": r.payment.order.ID, "buyer_id": r.payment.buyer.UserID, "contract_hash": contractHash, "paid_evidence_hash": paidHash, "order_version": strconv.FormatInt(orderVersion, 10), "payment_version": strconv.FormatInt(paymentVersion, 10), "maximum_refundable_fen": strconv.FormatInt(maximum, 10), "refund_amount_fen": strconv.FormatInt(amount, 10)}
	// The synthetic reviewer predates this request and differs from both parties.
	ref, digest := ids.New("ev"), strings.Repeat("e", 64)
	raw, _ := json.Marshal(scope)
	exec(t, `INSERT INTO p11.evidence(id,evidence_type,seller_id,content_ref,digest,scope_snapshot,decision,reviewed_by,valid_from,valid_until) VALUES($1,'MACHINE_PAY_REFUND_DECISION_1',$2,'synthetic-independent-refund-review',$3,$4,'APPROVED',$5,$6,$7)`, ref, b.Seller, digest, raw, reviewer, b.ValidFrom, b.ResolveUntil)
	return payments.OriginalRefundInput{ExpectedOrderVersion: orderVersion, ExpectedPaymentVersion: paymentVersion, RouteRef: r.routeRef, DecisionRef: ref, DecisionHash: digest}
}
func (r *originalRefundFixture) create(t *testing.T, amount, maximum int64) payments.OriginalRefundView {
	t.Helper()
	value, e := r.service.Create(ctx, r.payment.buyer, r.paid.ID, r.decision(t, amount, maximum), ids.New("idem"))
	if e != nil {
		t.Fatal(e)
	}
	return value
}
func awaitOriginalRefundQuery(t *testing.T, id string) {
	t.Helper()
	var next time.Time
	if e := db.Pool.QueryRow(ctx, `SELECT next_query_at FROM p11.refund_machine_contexts WHERE refund_id=$1`, id).Scan(&next); e != nil {
		t.Fatal(e)
	}
	// Wait the actual production boundary. Do not backdate immutable dispatches,
	// weaken constraints, inject a bypass or change a financial state for speed.
	timer := time.NewTimer(time.Until(next) + 25*time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		t.Fatal("original query boundary context expired")
	case <-timer.C:
	}
}

func TestOriginalRefundCreateCommitsReservationIntentJobsAndOutboxWithoutIO(t *testing.T) {
	r := refundIntegrationFixture(t)
	value := r.create(t, 200, 500)
	if value.State != "REQUESTED" || value.Amount != 200 || r.submits.Load() != 0 || r.queries.Load() != 0 {
		t.Fatal("refund request performed financial action")
	}
	var rows int
	if e := db.Pool.QueryRow(ctx, `SELECT count(*) FROM p11.refund_machine_contexts c JOIN p11.channel_requests s ON s.id=c.submit_request_id AND s.state='INTENT' JOIN p11.channel_requests q ON q.id=c.query_request_id AND q.state='INTENT' JOIN p11.durable_jobs j ON j.id=c.durable_job_id AND j.state='PENDING' WHERE c.refund_id=$1 AND EXISTS(SELECT 1 FROM p11.outbox_events e WHERE e.aggregate_id=c.refund_id AND e.event_type='OriginalRefundReserved')`, value.ID).Scan(&rows); e != nil || rows != 1 {
		t.Fatal("original refund reservation was not atomic", e)
	}
}
func TestOriginalRefundOneDecisionCannotCreateAnotherRefundThroughNewKey(t *testing.T) {
	r := refundIntegrationFixture(t)
	in := r.decision(t, 200, 500)
	first, e := r.service.Create(ctx, r.payment.buyer, r.paid.ID, in, ids.New("idem"))
	if e != nil {
		t.Fatal(e)
	}
	again, e := r.service.Create(ctx, r.payment.buyer, r.paid.ID, in, ids.New("idem"))
	if e != nil || again.ID != first.ID {
		t.Fatal("same independent decision allocated another original refund", e)
	}
	var count int
	db.Pool.QueryRow(ctx, `SELECT count(*) FROM p11.refunds WHERE payment_id=$1`, r.paid.ID).Scan(&count)
	if count != 1 || r.submits.Load() != 0 {
		t.Fatal("decision replay duplicated reservation or money movement")
	}
}
func TestOriginalRefundIndependentDecisionAndRouteRevocationBlockNewIntent(t *testing.T) {
	for _, kind := range []string{"decision", "route"} {
		t.Run(kind, func(t *testing.T) {
			r := refundIntegrationFixture(t)
			in := r.decision(t, 200, 500)
			ref := in.DecisionRef
			if kind == "route" {
				ref = r.route.EvidenceRef
			}
			exec(t, `UPDATE p11.evidence SET decision='REVOKED' WHERE id=$1`, ref)
			if _, e := r.service.Create(ctx, r.payment.buyer, r.paid.ID, in, ids.New("idem")); e == nil {
				t.Fatal("revoked financial approval admitted original refund")
			}
			if r.submits.Load() != 0 {
				t.Fatal("revoked request reached channel")
			}
		})
	}
}
func TestOriginalRefundAnotherBuyerCannotReadOrCreateFromOriginalDecision(t *testing.T) {
	r := refundIntegrationFixture(t)
	in := r.decision(t, 200, 500)
	value, e := r.service.Create(ctx, r.payment.buyer, r.paid.ID, in, ids.New("idem"))
	if e != nil {
		t.Fatal(e)
	}
	other := buyer(t)
	other.Scopes = append(other.Scopes, "refund:create", "refund:read")
	if _, e := r.service.Get(ctx, other, value.ID); e == nil {
		t.Fatal("foreign buyer read original refund")
	}
	if _, e := r.service.Create(ctx, other, r.paid.ID, in, ids.New("idem")); e == nil {
		t.Fatal("foreign buyer reused original financial decision")
	}
}
func TestOriginalRefundCumulativeUnknownReservationsCannotExceedCollectedAmount(t *testing.T) {
	r := refundIntegrationFixture(t)
	r.create(t, 300, 500)
	if _, e := r.service.Create(ctx, r.payment.buyer, r.paid.ID, r.decision(t, 201, 500), ids.New("idem")); e == nil {
		t.Fatal("original refund cumulative reservation exceeded collected consideration")
	}
	if r.submits.Load() != 0 {
		t.Fatal("over-reservation performed external refund")
	}
}
func TestOriginalRefundExternalSubmitObservesCommittedUnknownAndClaim(t *testing.T) {
	r := refundIntegrationFixture(t)
	value := r.create(t, 200, 500)
	var observed bool
	r.onCall = func(method, id string) {
		if method != "alipay.trade.refund" {
			return
		}
		var state, request, job string
		var generation int64
		e := db.Pool.QueryRow(ctx, `SELECT f.state,s.state,j.state,c.query_generation FROM p11.refunds f JOIN p11.refund_machine_contexts c ON c.refund_id=f.id JOIN p11.channel_requests s ON s.id=c.submit_request_id JOIN p11.durable_jobs j ON j.id=c.durable_job_id WHERE f.id=$1`, id).Scan(&state, &request, &job, &generation)
		observed = e == nil && state == "UNKNOWN" && request == "SENT" && job == "CLAIMED" && generation == 1
	}
	worked, e := r.service.RecoverOne(ctx)
	if e != nil || !worked || !observed {
		t.Fatal("external refund preceded its committed original intent", e)
	}
	after, e := r.service.Get(ctx, r.payment.buyer, value.ID)
	if e != nil || after.State != "PROCESSING" || r.submits.Load() != 1 {
		t.Fatal("signed submit was treated as confirmed own partial refund", e)
	}
}
func TestOriginalRefundUnknownSubmitRecoversBySameQueryWithoutFinancialResubmit(t *testing.T) {
	r := refundIntegrationFixture(t)
	r.unknownSubmit = true
	value := r.create(t, 200, 500)
	if worked, e := r.service.RecoverOne(ctx); !worked || e == nil {
		t.Fatal("synthetic lost submit not retained as unknown")
	}
	after, e := r.service.Get(ctx, r.payment.buyer, value.ID)
	if e != nil || after.State != "UNKNOWN" {
		t.Fatal("unknown financial result was dropped", e)
	}
	if _, e := r.service.RecoverOne(ctx); e != nil {
		t.Fatal(e)
	}
	if r.submits.Load() != 1 || r.queries.Load() != 0 {
		t.Fatal("immediate recovery resubmitted or bypassed query interval")
	}
	awaitOriginalRefundQuery(t, value.ID)
	if worked, e := r.service.RecoverOne(ctx); e != nil || !worked {
		t.Fatal("original query recovery", e)
	}
	after, e = r.service.Get(ctx, r.payment.buyer, value.ID)
	if e != nil || after.State != "SUCCEEDED" || r.submits.Load() != 1 || r.queries.Load() != 1 {
		t.Fatal("recovery changed original refund or failed to resolve", e)
	}
}
func TestOriginalRefundLateSuccessDoesNotAcknowledgeNewWorkerGeneration(t *testing.T) {
	r := refundIntegrationFixture(t)
	value := r.create(t, 200, 500)
	if _, e := r.service.RecoverOne(ctx); e != nil {
		t.Fatal(e)
	}
	awaitOriginalRefundQuery(t, value.ID)
	r.onCall = func(method, id string) {
		if method == "alipay.trade.fastpay.refund.query" {
			// Explicit database fault injection models a newer operator/worker claim.
			// No production feature, signature bypass or alternative refund is added.
			exec(t, `UPDATE p11.durable_jobs SET claim_generation=claim_generation+1,claimed_until=clock_timestamp()+interval '30 seconds' WHERE id=(SELECT durable_job_id FROM p11.refund_machine_contexts WHERE refund_id=$1)`, id)
		}
	}
	if worked, e := r.service.RecoverOne(ctx); e != nil || !worked {
		t.Fatal(e)
	}
	view, e := r.service.Get(ctx, r.payment.buyer, value.ID)
	if e != nil || view.State != "SUCCEEDED" {
		t.Fatal("late signed financial fact not archived", e)
	}
	var state string
	var generation int64
	if e = db.Pool.QueryRow(ctx, `SELECT j.state,j.claim_generation FROM p11.durable_jobs j JOIN p11.refund_machine_contexts c ON c.durable_job_id=j.id WHERE c.refund_id=$1`, value.ID).Scan(&state, &generation); e != nil || state != "CLAIMED" || generation != 3 {
		t.Fatal("old signed response acknowledged another generation", e, state, generation)
	}
}
func TestOriginalRefundChangedIntentOrManualSuccessCannotCommit(t *testing.T) {
	r := refundIntegrationFixture(t)
	value := r.create(t, 200, 500)
	for _, statement := range []string{`UPDATE p11.refund_machine_contexts SET route_ref='other_route' WHERE refund_id=$1`, `DELETE FROM p11.refund_machine_contexts WHERE refund_id=$1`, `UPDATE p11.refunds SET state='SUCCEEDED' WHERE id=$1`, `UPDATE p11.refunds SET state='FAILED' WHERE id=$1`} {
		if _, e := db.Pool.Exec(ctx, statement, value.ID); e == nil {
			t.Fatal("original refund scope/obligation rewritten without original signed query")
		}
	}
}
func TestOriginalRefundRevokedDecisionBeforeDispatchPreservesHeldObligation(t *testing.T) {
	r := refundIntegrationFixture(t)
	in := r.decision(t, 200, 500)
	value, e := r.service.Create(ctx, r.payment.buyer, r.paid.ID, in, ids.New("idem"))
	if e != nil {
		t.Fatal(e)
	}
	exec(t, `UPDATE p11.evidence SET decision='REVOKED' WHERE id=$1`, in.DecisionRef)
	if worked, e := r.service.RecoverOne(ctx); e != nil || !worked {
		t.Fatal(e)
	}
	after, e := r.service.Get(ctx, r.payment.buyer, value.ID)
	if e != nil || !after.ReviewRequired || after.State != "REQUESTED" || r.submits.Load() != 0 {
		t.Fatal("revoked dispatch freed money or reached channel", e)
	}
}
func TestOriginalRefundTwoPartialDispatchesRespectOriginalThreeSecondWatermark(t *testing.T) {
	r := refundIntegrationFixture(t)
	r.create(t, 200, 500)
	r.create(t, 300, 500)
	if _, e := r.service.RecoverOne(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e := r.service.RecoverOne(ctx); e != nil {
		t.Fatal(e)
	}
	if r.submits.Load() != 1 {
		t.Fatal("same original trade received two immediate partial refund submissions")
	}
	var requested, unknown int
	if e := db.Pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE state='REQUESTED'),count(*) FILTER(WHERE state='UNKNOWN') FROM p11.refunds WHERE payment_id=$1`, r.paid.ID).Scan(&requested, &unknown); e != nil || requested != 1 || unknown != 0 {
		t.Fatal("postponement changed unsent financial state", e)
	}
}
func TestOriginalRefundStoppedServiceCannotCreateOrConsumeOriginalJob(t *testing.T) {
	r := refundIntegrationFixture(t)
	in := r.decision(t, 200, 500)
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if r.service.Close(closeCtx) != nil {
		t.Fatal("refund service did not close")
	}
	if _, e := r.service.Create(ctx, r.payment.buyer, r.paid.ID, in, ids.New("idem")); e == nil {
		t.Fatal("closed refund service created financial intent")
	}
	if _, e := r.service.RecoverOne(ctx); e == nil {
		t.Fatal("closed refund service consumed job")
	}
}
func TestOriginalRefundConcurrentIndependentDecisionsSerializeOriginalCapacity(t *testing.T) {
	r := refundIntegrationFixture(t)
	first, second := r.decision(t, 200, 500), r.decision(t, 301, 500)
	results := make(chan error, 2)
	for _, input := range []payments.OriginalRefundInput{first, second} {
		go func(in payments.OriginalRefundInput) {
			_, e := r.service.Create(ctx, r.payment.buyer, r.paid.ID, in, ids.New("idem"))
			results <- e
		}(input)
	}
	successes := 0
	for range 2 {
		if <-results == nil {
			successes++
		}
	}
	if successes != 1 || r.submits.Load() != 0 {
		t.Fatal("concurrent original reservations overspent or sent money")
	}
	var held int64
	if e := db.Pool.QueryRow(ctx, `SELECT coalesce(sum(amount_fen),0) FROM p11.refunds WHERE payment_id=$1 AND state<>'FAILED'`, r.paid.ID).Scan(&held); e != nil || held < 1 || held > 500 {
		t.Fatal("original cumulative capacity corrupted", e)
	}
}
func TestOriginalRefundReservationBlocksActualOriginalFundingWorker(t *testing.T) {
	r := refundIntegrationFixture(t)
	r.create(t, 200, 500)
	_, _ = r.payment.machine.RecoverFundingOne(ctx)
	var state string
	var allocations int
	if e := db.Pool.QueryRow(ctx, `SELECT o.state,(SELECT count(*) FROM p11.order_fundings f WHERE f.order_id=o.id) FROM p11.orders o WHERE o.id=$1`, r.payment.order.ID).Scan(&state, &allocations); e != nil || state == "READY" || allocations != 0 {
		t.Fatal("refund hold admitted spendable original funds", e)
	}
	if r.submits.Load() != 0 {
		t.Fatal("funding worker triggered a refund")
	}
}
func TestOriginalRefundSignedArchivesCannotBeDeletedOrRetagged(t *testing.T) {
	r := refundIntegrationFixture(t)
	value := r.create(t, 200, 500)
	if _, e := r.service.RecoverOne(ctx); e != nil {
		t.Fatal(e)
	}
	for _, statement := range []string{`DELETE FROM p11.refund_machine_receipts WHERE refund_id=$1`, `UPDATE p11.refund_machine_receipts SET kind='REFUND_CONFIRMED',method='alipay.trade.fastpay.refund.query',cumulative_fen=0 WHERE refund_id=$1`} {
		if _, e := db.Pool.Exec(ctx, statement, value.ID); e == nil {
			t.Fatal("signed submit archive deleted or retagged as financial success")
		}
	}
}
