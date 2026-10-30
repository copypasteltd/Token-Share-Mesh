package integration_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
	"p11.local/compute/client/execution"
	"p11.local/compute/client/protocol/executionlease"
	relay "p11.local/compute/gateway/core"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/invocation"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/internal/testsupport"
	"p11.local/compute/internal/trade"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
)

const syntheticInput = "R017_SYNTHETIC_PRIVATE_TEXT_NOT_CUSTOMER_CONTENT"

func executionBody(protocol relay.Profile, key string) (invocation.Request, []byte) {
	var body string
	switch protocol {
	case relay.ChatText:
		body = fmt.Sprintf(`{"model":"model_nb1","messages":[{"role":"user","content":%q}],"max_completion_tokens":16,"stream":false}`, syntheticInput)
	case relay.ResponsesText:
		body = fmt.Sprintf(`{"model":"model_nb1","input":%q,"store":false,"max_output_tokens":16,"stream":false}`, syntheticInput)
	case relay.MessagesText:
		body = fmt.Sprintf(`{"model":"model_nb1","messages":[{"role":"user","content":%q}],"max_tokens":16,"stream":false}`, syntheticInput)
	}
	return invocation.Request{Model: "model_nb1", Protocol: protocol, RequestDigest: outbox.Digest([]byte(body)), IdempotencyKey: key, MaxOutputTokens: 16}, []byte(body)
}

func executionConfig(t *testing.T) invocation.ExecutionConfig {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	input := make([]byte, 32)
	if _, err = rand.Read(input); err != nil {
		t.Fatal(err)
	}
	return invocation.ExecutionConfig{Invocations: invocationService(t), InputVersion: "input_one", InputKeys: map[string][]byte{"input_one": input}, SigningKeyID: "issuer_one", SigningPrivateKey: key}
}
func configuredExecution(t *testing.T, c invocation.ExecutionConfig) *invocation.Execution {
	t.Helper()
	s, err := invocation.NewExecution(c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}
func executionFixture(t *testing.T, protocol relay.Profile) (routeFixture, identity.Principal, invocation.Request, []byte) {
	t.Helper()
	f := routeOrderOffer(t, buyer(t), testsupport.SeedNativeOfferForProtocol(t, ctx, db, ids.New("region"), string(protocol)))
	c := f.create(t)
	_, _, p := routeKey(t, f.buyer, c.ID)
	in, body := executionBody(protocol, ids.New("original"))
	return f, p, in, body
}
func readyExecution(t *testing.T, s *invocation.Execution, p identity.Principal, in invocation.Request, body []byte) invocation.InputReady {
	t.Helper()
	v, err := s.Ready(ctx, p, in, body)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func issueExecution(t *testing.T, s *invocation.Execution, p identity.Principal, in invocation.Request) invocation.Issuance {
	t.Helper()
	v, err := s.Issue(ctx, p, in)
	if err != nil || v.Lease == nil || v.Manifest == nil || v.Status != "ISSUED" {
		t.Fatal(v, err)
	}
	return v
}
func executionCounts(t *testing.T, job string, assets, attempts, leases, events, tasks int) {
	t.Helper()
	var a, b, c, d, e int
	err := db.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM p11.assets WHERE job_id=$1),(SELECT count(*) FROM p11.attempts WHERE job_id=$1),
(SELECT count(*) FROM p11.execution_leases WHERE job_id=$1),(SELECT count(*) FROM p11.outbox_events WHERE aggregate_id=$1 AND aggregate_type='ROUTE_INVOCATION'),
(SELECT count(*) FROM p11.durable_jobs WHERE target_ref IN (SELECT id FROM p11.execution_leases WHERE job_id=$1))`, job).Scan(&a, &b, &c, &d, &e)
	if err != nil || a != assets || b != attempts || c != leases || d != events || e != tasks {
		t.Fatal("atomic native counts", a, b, c, d, e, err)
	}
}

func TestExecutionInputsAcrossThreeNativeProfilesAreActuallyEncrypted(t *testing.T) {
	requireDB(t)
	for _, protocol := range []relay.Profile{relay.ChatText, relay.ResponsesText, relay.MessagesText} {
		t.Run(string(protocol), func(t *testing.T) {
			f, p, in, body := executionFixture(t, protocol)
			s := configuredExecution(t, executionConfig(t))
			v := readyExecution(t, s, p, in, body)
			if v.Preparation.State != "QUEUED" || v.Replayed || v.Bytes != int64(len(body)) {
				t.Fatal(v)
			}
			var ciphertext []byte
			var meta string
			if err := db.Pool.QueryRow(ctx, `SELECT input_ciphertext,(to_jsonb(a)-'input_ciphertext')::text FROM p11.assets a WHERE id=$1`, v.AssetID).Scan(&ciphertext, &meta); err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(ciphertext, []byte(syntheticInput)) || strings.Contains(meta, syntheticInput) || strings.Contains(meta, in.RequestDigest) || len(ciphertext) != len(body)+28 || outbox.Digest(ciphertext) != v.CipherDigest {
				t.Fatal("plaintext/digest leaked or wrong encrypted body")
			}
			executionCounts(t, v.Preparation.JobID, 1, 0, 0, 2, 0)
			var phase string
			if err := db.Pool.QueryRow(ctx, `SELECT phase FROM p11.resource_locks WHERE resource_id=$1`, f.offer.Resource).Scan(&phase); err != nil || phase != "FREE" {
				t.Fatal(phase, err)
			}
			issued := issueExecution(t, s, p, in)
			if issued.Manifest.Spec.Protocol != protocol || issued.Manifest.InputAsset != v.AssetID || issued.Manifest.InputBytes != v.Bytes {
				t.Fatal(issued)
			}
		})
	}
}

func TestExecutionConcurrentReadyRetainsOneOriginalCiphertext(t *testing.T) {
	requireDB(t)
	_, p, in, body := executionFixture(t, relay.ChatText)
	s := configuredExecution(t, executionConfig(t))
	type result struct {
		v invocation.InputReady
		e error
	}
	ch := make(chan result, 8)
	for range 8 {
		go func() { v, e := s.Ready(ctx, p, in, body); ch <- result{v, e} }()
	}
	first, digest := "", ""
	fresh := 0
	for range 8 {
		r := <-ch
		if r.e != nil {
			t.Fatal(r.e)
		}
		if first == "" {
			first, digest = r.v.AssetID, r.v.CipherDigest
		}
		if first != r.v.AssetID || digest != r.v.CipherDigest {
			t.Fatal("input replaced")
		}
		if !r.v.Replayed {
			fresh++
		}
	}
	v := readyExecution(t, s, p, in, body)
	if fresh != 1 {
		t.Fatal(fresh)
	}
	executionCounts(t, v.Preparation.JobID, 1, 0, 0, 2, 0)
}

func TestExecutionInputRejectsMismatchedBodyAndUnsupportedNativeFields(t *testing.T) {
	requireDB(t)
	f, p, in, body := executionFixture(t, relay.ChatText)
	s := configuredExecution(t, executionConfig(t))
	for _, bad := range [][]byte{append(append([]byte(nil), body...), byte(' ')), []byte(`{"model":"model_nb1","messages":[],"max_tokens":16}`), []byte(`{"model":"model_nb1","messages":[{"role":"user","content":"x"}],"max_tokens":16,"tools":[]}`), bytes.Repeat([]byte{'a'}, (256<<10)+1)} {
		if _, err := s.Ready(ctx, p, in, bad); err == nil {
			t.Fatal("unvalidated body accepted")
		}
	}
	bad := in
	bad.MaxOutputTokens++
	if _, err := s.Ready(ctx, p, bad, body); err == nil {
		t.Fatal("output metadata trusted")
	}
	bad = in
	bad.Stream = true
	if _, err := s.Ready(ctx, p, bad, body); err == nil {
		t.Fatal("stream metadata trusted")
	}
	invocationCounts(t, f, 0, 0, 0)
}

func TestExecutionInputKeyCopiesRotationAndMissingOldKey(t *testing.T) {
	requireDB(t)
	_, p, in, body := executionFixture(t, relay.ChatText)
	c := executionConfig(t)
	s := configuredExecution(t, c)
	v := readyExecution(t, s, p, in, body)
	old := append([]byte(nil), c.InputKeys["input_one"]...)
	clear(c.InputKeys["input_one"])
	clear(c.SigningPrivateKey)
	if r := readyExecution(t, s, p, in, body); r.AssetID != v.AssetID || !r.Replayed {
		t.Fatal(r)
	}
	rotated := executionConfig(t)
	rotated.InputVersion = "input_two"
	rotated.InputKeys = map[string][]byte{"input_one": old, "input_two": bytes.Repeat([]byte{19}, 32)}
	if r := readyExecution(t, configuredExecution(t, rotated), p, in, body); r.CipherDigest != v.CipherDigest {
		t.Fatal("rotation replaced ciphertext")
	}
	delete(rotated.InputKeys, "input_one")
	missing := configuredExecution(t, rotated)
	if _, err := missing.Issue(ctx, p, in); fault.Public(err).Code != fault.Unavailable {
		t.Fatal("missing original input key", err)
	}
	executionCounts(t, v.Preparation.JobID, 1, 0, 0, 2, 0)
}

func TestExecutionExplicitConfigurationAndCloseFailClosed(t *testing.T) {
	requireDB(t)
	c := executionConfig(t)
	for _, change := range []func(*invocation.ExecutionConfig){func(v *invocation.ExecutionConfig) { v.InputVersion = "missing" }, func(v *invocation.ExecutionConfig) {
		v.InputKeys = map[string][]byte{"input_one": bytes.Repeat([]byte{'f'}, 32)}
	}, func(v *invocation.ExecutionConfig) { v.SigningKeyID = "" }, func(v *invocation.ExecutionConfig) { v.SigningPrivateKey = make([]byte, 64) }, func(v *invocation.ExecutionConfig) {
		v.InputKeys = map[string][]byte{"input_one": v.SigningPrivateKey.Seed()}
	}, func(v *invocation.ExecutionConfig) {
		v.VerificationKeys = map[string]ed25519.PublicKey{"issuer_one": make([]byte, 32)}
	}} {
		bad := c
		change(&bad)
		if s, err := invocation.NewExecution(bad); err == nil {
			s.Close()
			t.Fatal("unsafe config accepted")
		}
	}
	f, p, in, body := executionFixture(t, relay.ChatText)
	s := configuredExecution(t, c)
	s.Close()
	if _, err := s.Ready(ctx, p, in, body); err == nil {
		t.Fatal("closed input accepted")
	}
	invocationCounts(t, f, 0, 0, 0)
}

func TestExecutionLastPreparedCallUsesItsOwnFullBudgetHold(t *testing.T) {
	requireDB(t)
	f := routeOrderOffer(t, buyer(t), testsupport.SeedNativeOfferForProtocol(t, ctx, db, ids.New("region"), string(relay.ChatText)))
	f.in.MaxCalls = 1
	f.in.PerCallFen = 500
	c := f.create(t)
	_, _, p := routeKey(t, f.buyer, c.ID)
	in, body := executionBody(relay.ChatText, "last-original-call")
	s := configuredExecution(t, executionConfig(t))
	v := readyExecution(t, s, p, in, body)
	current, err := routeService().Get(ctx, f.buyer, c.ID)
	if err != nil || current.State != "SUSPENDED" || current.UsedCalls != 1 {
		t.Fatal(current, err)
	}
	issued := issueExecution(t, s, p, in)
	if issued.Preparation.ReservedFen != 500 || issued.Preparation.JobID != v.Preparation.JobID {
		t.Fatal(issued)
	}
	other := in
	other.IdempotencyKey = "another-original-call"
	if _, err = s.Ready(ctx, p, other, body); err == nil {
		t.Fatal("new capacity reissued")
	}
	invocationCounts(t, f, 1, 2, 500)
	executionCounts(t, v.Preparation.JobID, 1, 1, 1, 3, 1)
}

func TestExecutionConcurrentOriginalIssueCommitsOneOwnerLeaseAndDelivery(t *testing.T) {
	requireDB(t)
	f, p, in, body := executionFixture(t, relay.ChatText)
	c := executionConfig(t)
	s := configuredExecution(t, c)
	ready := readyExecution(t, s, p, in, body)
	type result struct {
		v invocation.Issuance
		e error
	}
	ch := make(chan result, 8)
	for range 8 {
		go func() { v, e := s.Issue(ctx, p, in); ch <- result{v, e} }()
	}
	first, signature := "", ""
	fresh := 0
	for range 8 {
		r := <-ch
		if r.e != nil || r.v.Lease == nil {
			t.Fatal(r.v, r.e)
		}
		if first == "" {
			first, signature = r.v.LeaseID, r.v.Lease.Signature
		}
		if first != r.v.LeaseID || signature != r.v.Lease.Signature {
			t.Fatal("original lease changed")
		}
		if !r.v.Replayed {
			fresh++
		}
	}
	if fresh != 1 {
		t.Fatal(fresh)
	}
	executionCounts(t, ready.Preparation.JobID, 1, 1, 1, 3, 1)
	issued := issueExecution(t, s, p, in)
	raw, _ := json.Marshal(issued.Lease)
	// Independent SQL manifest + synthetic approved fixture, not incoming Lease.
	var manifest []byte
	if err := db.Pool.QueryRow(ctx, `SELECT execution_manifest FROM p11.execution_leases WHERE id=$1`, first).Scan(&manifest); err != nil {
		t.Fatal(err)
	}
	var approved invocation.Manifest
	if err := json.Unmarshal(manifest, &approved); err != nil || approved.Device != f.offer.Device || approved.Resource != f.offer.Resource || approved.EnvironmentDigest != f.offer.Terms.Bindings[0].EnvironmentDigest {
		t.Fatal("independent plan mismatch", err)
	}
	if _, err := executionlease.Verify(raw, c.SigningPrivateKey.Public().(ed25519.PublicKey), approved.ApprovedPlan(), time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestExecutionDifferentOrdersContendForOnePhysicalResource(t *testing.T) {
	requireDB(t)
	offer := testsupport.SeedNativeOfferForProtocol(t, ctx, db, ids.New("region"), string(relay.ChatText))
	s := configuredExecution(t, executionConfig(t))
	fs := []routeFixture{routeOrderOffer(t, buyer(t), offer), routeOrderOffer(t, buyer(t), offer)}
	type call struct {
		p   identity.Principal
		in  invocation.Request
		job string
	}
	calls := []call{}
	for _, f := range fs {
		c := f.create(t)
		_, _, p := routeKey(t, f.buyer, c.ID)
		in, body := executionBody(relay.ChatText, ids.New("orig"))
		v := readyExecution(t, s, p, in, body)
		calls = append(calls, call{p, in, v.Preparation.JobID})
	}
	type result struct {
		v invocation.Issuance
		e error
	}
	ch := make(chan result, 2)
	for _, v := range calls {
		go func(v call) { r, e := s.Issue(ctx, v.p, v.in); ch <- result{r, e} }(v)
	}
	n := 0
	for range 2 {
		r := <-ch
		if r.e == nil {
			if r.v.Lease == nil {
				t.Fatal(r.v)
			}
			n++
		} else {
			assertCode(t, r.e, fault.Conflict)
		}
	}
	if n != 1 {
		t.Fatal("multiple physical owners", n)
	}
	var owners int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM p11.attempts WHERE resource_id=$1`, offer.Resource).Scan(&owners); err != nil || owners != 1 {
		t.Fatal(owners, err)
	}
}

func TestExecutionPendingAndQueuedCallsNeverAcquirePhysicalOwner(t *testing.T) {
	requireDB(t)
	f, p, in, body := executionFixture(t, relay.ChatText)
	c := executionConfig(t)
	prepared, err := c.Invocations.Prepare(ctx, p, in)
	if err != nil {
		t.Fatal(err)
	}
	s := configuredExecution(t, c)
	if _, err = s.Issue(ctx, p, in); err == nil {
		t.Fatal("metadata-only pending input executed")
	}
	executionCounts(t, prepared.JobID, 0, 0, 0, 1, 0)
	readyExecution(t, s, p, in, body)
	executionCounts(t, prepared.JobID, 1, 0, 0, 2, 0)
	var owner *string
	if err = db.Pool.QueryRow(ctx, `SELECT owner_attempt_id FROM p11.resource_locks WHERE resource_id=$1`, f.offer.Resource).Scan(&owner); err != nil || owner != nil {
		t.Fatal(owner, err)
	}
}

func TestExecutionRequiresNativeIdentityAndExplicitFreeResource(t *testing.T) {
	requireDB(t)
	for _, scenario := range []string{"missing", "quarantine", "busy", "draining", "stale", "expired_nonfree", "legacy"} {
		t.Run(scenario, func(t *testing.T) {
			var f routeFixture
			if scenario == "legacy" {
				f = routeOrder(t, buyer(t), relay.ChatText)
			} else {
				f = routeOrderOffer(t, buyer(t), testsupport.SeedNativeOfferForProtocol(t, ctx, db, ids.New("region"), string(relay.ChatText)))
			}
			c := f.create(t)
			_, _, p := routeKey(t, f.buyer, c.ID)
			in, body := executionBody(relay.ChatText, ids.New("orig"))
			s := configuredExecution(t, executionConfig(t))
			v := readyExecution(t, s, p, in, body)
			switch scenario {
			case "missing":
				exec(t, `DELETE FROM p11.resource_locks WHERE resource_id=$1`, f.offer.Resource)
			case "quarantine", "expired_nonfree":
				exec(t, `UPDATE p11.resource_locks SET phase='QUARANTINED',expires_at=clock_timestamp()-interval '1 hour' WHERE resource_id=$1`, f.offer.Resource)
			case "busy":
				exec(t, `UPDATE p11.devices SET reported_resource_state='BUSY' WHERE id=$1`, f.offer.Device)
			case "draining":
				exec(t, `UPDATE p11.devices SET reported_resource_state='DRAINING' WHERE id=$1`, f.offer.Device)
			case "stale":
				exec(t, `UPDATE p11.devices SET last_seen_at=clock_timestamp()-interval '1 minute' WHERE id=$1`, f.offer.Device)
			case "legacy":
				exec(t, `INSERT INTO p11.resource_locks(resource_id,phase) VALUES($1,'FREE')`, f.offer.Resource)
			}
			if _, err := s.Issue(ctx, p, in); err == nil {
				t.Fatal("unqualified owner admitted", scenario)
			}
			executionCounts(t, v.Preparation.JobID, 1, 0, 0, 2, 0)
		})
	}
}

func TestExecutionCurrentAuthorizationFundsAndQualificationRemainRequired(t *testing.T) {
	requireDB(t)
	for _, scenario := range []string{"key", "role", "user", "funds", "gate", "route", "input"} {
		t.Run(scenario, func(t *testing.T) {
			f, p, in, body := executionFixture(t, relay.ChatText)
			s := configuredExecution(t, executionConfig(t))
			v := readyExecution(t, s, p, in, body)
			switch scenario {
			case "key":
				exec(t, `UPDATE p11.api_keys SET revoked_at=clock_timestamp() WHERE id=$1`, p.KeyID)
			case "role":
				exec(t, `UPDATE p11.role_grants SET revoked_at=clock_timestamp() WHERE user_id=$1 AND role_code='BUYER'`, p.UserID)
			case "user":
				exec(t, `UPDATE p11.users SET state='SUSPENDED',version=version+1 WHERE id=$1`, p.UserID)
			case "funds":
				exec(t, `UPDATE p11.payments SET state='UNKNOWN' WHERE id=$1`, f.in.PaymentID)
			case "gate":
				exec(t, `UPDATE p11.feature_gates SET enabled=false WHERE id=$1`, f.offer.Gate)
			case "route":
				exec(t, `UPDATE p11.route_contracts SET state='SUSPENDED',version=version+1 WHERE id=$1`, v.Preparation.RouteID)
			case "input":
				exec(t, `UPDATE p11.assets SET state='QUARANTINED',version=version+1 WHERE id=$1`, v.AssetID)
			}
			if _, err := s.Issue(ctx, p, in); err == nil {
				t.Fatal("current eligibility bypassed", scenario)
			}
			executionCounts(t, v.Preparation.JobID, 1, 0, 0, 2, 0)
		})
	}
}

func TestExecutionRecoveryCannotRenewOrReplaceWithdrawnAuthority(t *testing.T) {
	requireDB(t)
	f, p, in, body := executionFixture(t, relay.ChatText)
	s := configuredExecution(t, executionConfig(t))
	v := readyExecution(t, s, p, in, body)
	issued := issueExecution(t, s, p, in)
	again := issueExecution(t, s, p, in)
	if !again.Replayed || again.Lease.Expires != issued.Lease.Expires || again.Lease.Signature != issued.Lease.Signature {
		t.Fatal("lease extended", again)
	}
	exec(t, `UPDATE p11.execution_leases SET state='REVOKED' WHERE id=$1`, issued.LeaseID)
	recovered, err := s.Issue(ctx, p, in)
	if err != nil || recovered.Lease != nil || recovered.Manifest != nil || recovered.Status != "RECOVERED_UNAVAILABLE" || recovered.AttemptID != issued.AttemptID {
		t.Fatal(recovered, err)
	}
	exec(t, `UPDATE p11.feature_gates SET enabled=false WHERE id=$1`, f.offer.Gate)
	recovered, err = s.Issue(ctx, p, in)
	if err != nil || recovered.Lease != nil {
		t.Fatal(recovered, err)
	}
	executionCounts(t, v.Preparation.JobID, 1, 1, 1, 3, 1)
}

func TestExecutionActualExpiryRetainsOriginalBudgetAndPhysicalOwner(t *testing.T) {
	requireDB(t)
	f, p, in, body := executionFixture(t, relay.ChatText)
	s := configuredExecution(t, executionConfig(t))
	ready := readyExecution(t, s, p, in, body)
	exec(t, `UPDATE p11.payments SET funds_expires_at=clock_timestamp()+interval '5 seconds' WHERE id=$1`, f.in.PaymentID)
	issued := issueExecution(t, s, p, in)
	expiry, err := time.Parse(time.RFC3339Nano, issued.Lease.Expires)
	if err != nil || time.Until(expiry) > 5*time.Second {
		t.Fatal(err)
	}
	time.Sleep(time.Until(expiry) + 20*time.Millisecond)
	recovered, err := s.Issue(ctx, p, in)
	if err != nil || recovered.Lease != nil || recovered.AttemptID != issued.AttemptID {
		t.Fatal(recovered, err)
	}
	var phase string
	var amount int64
	if err = db.Pool.QueryRow(ctx, `SELECT rl.phase,o.budget_reserved_fen FROM p11.resource_locks rl JOIN p11.orders o ON o.id=$2 WHERE rl.resource_id=$1`, f.offer.Resource, f.order.ID).Scan(&phase, &amount); err != nil || phase != "ACTIVE" || amount != 100 {
		t.Fatal(phase, amount, err)
	}
	executionCounts(t, ready.Preparation.JobID, 1, 1, 1, 3, 1)
}

func TestExecutionPhysicalGenerationPreservesSignedSQLBoundaryWithoutWrap(t *testing.T) {
	requireDB(t)
	for _, last := range []bool{false, true} {
		t.Run(fmt.Sprint(last), func(t *testing.T) {
			f, p, in, body := executionFixture(t, relay.ChatText)
			s := configuredExecution(t, executionConfig(t))
			v := readyExecution(t, s, p, in, body)
			g := "9223372036854775806"
			if last {
				g = "9223372036854775807"
			}
			exec(t, `UPDATE p11.resource_locks SET lock_generation=$2 WHERE resource_id=$1`, f.offer.Resource, g)
			if last {
				if _, err := s.Issue(ctx, p, in); err == nil {
					t.Fatal("signed SQL generation overflow accepted")
				}
				executionCounts(t, v.Preparation.JobID, 1, 0, 0, 2, 0)
			} else {
				issued := issueExecution(t, s, p, in)
				if issued.Lease.Generation != "9223372036854775807" {
					t.Fatal(issued.Lease.Generation)
				}
			}
		})
	}
}

func TestExecutionReadinessRollbackAndMissingEventLeaveNoInputOrHold(t *testing.T) {
	requireDB(t)
	f, p, in, body := executionFixture(t, relay.ChatText)
	s := configuredExecution(t, executionConfig(t))
	for _, missing := range []bool{false, true} {
		reached := false
		err := db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
			v, e := s.ReadyTx(ctx, tx, p, in, body)
			if e != nil {
				return e
			}
			reached = true
			if missing {
				_, e = tx.Exec(ctx, `DELETE FROM p11.outbox_events WHERE aggregate_id=$1 AND event_type='RouteInvocationInputReady'`, v.Preparation.JobID)
				return e
			}
			return errors.New("synthetic rollback")
		})
		if err == nil || !reached {
			t.Fatal("readiness did not reach the intended rollback/constraint", err)
		}
		invocationCounts(t, f, 0, 0, 0)
	}
}

func TestExecutionIssuanceRollbackOrMissingAtomicMemberRetainsQueuedCall(t *testing.T) {
	requireDB(t)
	f, p, in, body := executionFixture(t, relay.ChatText)
	s := configuredExecution(t, executionConfig(t))
	ready := readyExecution(t, s, p, in, body)
	for _, missing := range []string{"rollback", "event", "delivery", "owner", "budget"} {
		reached := false
		err := db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
			v, e := s.IssueTx(ctx, tx, p, in)
			if e != nil {
				return e
			}
			reached = true
			switch missing {
			case "rollback":
				return errors.New("synthetic rollback")
			case "event":
				_, e = tx.Exec(ctx, `DELETE FROM p11.outbox_events WHERE aggregate_id=$1 AND event_type='RouteInvocationAttemptIssued'`, v.Preparation.JobID)
			case "delivery":
				_, e = tx.Exec(ctx, `DELETE FROM p11.durable_jobs WHERE target_ref=$1`, v.LeaseID)
			case "owner":
				_, e = tx.Exec(ctx, `UPDATE p11.resource_locks SET phase='FREE',owner_attempt_id=NULL WHERE resource_id=$1`, f.offer.Resource)
			case "budget":
				_, e = tx.Exec(ctx, `UPDATE p11.budget_reservations SET state='RELEASED' WHERE id=$1`, v.Preparation.ReservationID)
			}
			return e
		})
		if err == nil || !reached {
			t.Fatal("issuance did not reach the intended rollback/constraint", missing, err)
		}
		executionCounts(t, ready.Preparation.JobID, 1, 0, 0, 2, 0)
	}
}

func TestExecutionSQLBindingsCannotReplaceInputLeaseOrInitialOwner(t *testing.T) {
	requireDB(t)
	_, p, in, body := executionFixture(t, relay.ChatText)
	s := configuredExecution(t, executionConfig(t))
	ready := readyExecution(t, s, p, in, body)
	issued := issueExecution(t, s, p, in)
	for _, q := range []string{
		`UPDATE p11.assets SET input_ciphertext=decode(repeat('00',bytes::integer+28),'hex') WHERE job_id=$1`,
		`UPDATE p11.assets SET input_key_version='changed' WHERE job_id=$1`,
		`DELETE FROM p11.assets WHERE job_id=$1`,
		`UPDATE p11.jobs SET input_asset_id=NULL WHERE id=$1`,
		`UPDATE p11.jobs SET state='QUEUED' WHERE id=$1`,
		`UPDATE p11.jobs SET current_attempt_id=NULL,fencing_token=0 WHERE id=$1`,
		`UPDATE p11.execution_leases SET expires_at=expires_at+interval '1 second' WHERE job_id=$1`,
		`UPDATE p11.execution_leases SET signed_payload='{}' WHERE job_id=$1`,
		`UPDATE p11.execution_leases SET execution_manifest='{}' WHERE job_id=$1`,
		`DELETE FROM p11.execution_leases WHERE job_id=$1`,
	} {
		if _, err := db.Pool.Exec(ctx, q, ready.Preparation.JobID); err == nil {
			t.Fatal("immutable native binding changed", q)
		}
	}
	exec(t, `UPDATE p11.execution_leases SET state='REVOKED' WHERE id=$1`, issued.LeaseID)
	if _, err := db.Pool.Exec(ctx, `UPDATE p11.execution_leases SET state='ISSUED' WHERE id=$1`, issued.LeaseID); err == nil {
		t.Fatal("revoked lease restored")
	}
}

func TestExecutionCiphertextMustAuthenticateEvenWhenSQLShapeIsReady(t *testing.T) {
	requireDB(t)
	f, p, in, body := executionFixture(t, relay.ChatText)
	c := executionConfig(t)
	s := configuredExecution(t, c)
	prepared, err := c.Invocations.Prepare(ctx, p, in)
	if err != nil {
		t.Fatal(err)
	}
	asset := ids.New("ast")
	garbage := bytes.Repeat([]byte{37}, len(body)+28)
	err = db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO p11.assets(id,order_id,owner_id,job_id,visibility,state,storage_key,storage_digest,bytes,media_type,expires_at,input_profile,input_key_version,input_ciphertext)
VALUES($1::text,$2,$3,$4,'PLAINTEXT','READY',$1::text,$5,$6,'application/json',$7,$8,'input_one',$9)`, asset, f.order.ID, p.UserID, prepared.JobID, outbox.Digest(garbage), len(body), prepared.Deadline, invocation.InputProfile, garbage)
		if e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `UPDATE p11.jobs SET input_asset_id=$2,state='QUEUED',version=version+1 WHERE id=$1`, prepared.JobID, asset)
		if e != nil {
			return e
		}
		var hash string
		if e = tx.QueryRow(ctx, `SELECT contract_hash FROM p11.orders WHERE id=$1`, f.order.ID).Scan(&hash); e != nil {
			return e
		}
		safe, _ := json.Marshal(map[string]string{"job_id": prepared.JobID, "asset_id": asset, "order_id": f.order.ID, "contract_hash": hash})
		_, e = tx.Exec(ctx, `INSERT INTO p11.outbox_events(id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,safe_payload,state) VALUES($1,'ROUTE_INVOCATION',$2,2,'RouteInvocationInputReady','3.0',$3,'PENDING')`, ids.New("evt"), prepared.JobID, safe)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Issue(ctx, p, in); fault.Public(err).Code != fault.Conflict {
		t.Fatal("SQL shape mistaken for authenticated input", err)
	}
	executionCounts(t, prepared.JobID, 1, 0, 0, 2, 0)
}

func TestExecutionUpgradeLeavesHistoricalAssetsAndLeasesUnbound(t *testing.T) {
	requireDB(t)
	local, _, source := budgetUpgradeDB(t)
	pre := fstest.MapFS{}
	paths, err := fs.Glob(source, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range paths {
		if filepath.Base(name)[:3] >= "009" {
			continue
		}
		body, e := fs.ReadFile(source, name)
		if e != nil {
			t.Fatal(e)
		}
		pre[name] = &fstest.MapFile{Data: body}
	}
	if len(pre) != 8 {
		t.Fatal(len(pre))
	}
	if err = local.MigrateFS(ctx, pre); err != nil {
		t.Fatal(err)
	}
	f := testsupport.SeedOffer(t, ctx, local, "PLAINTEXT", ids.New("region"))
	p := identity.Principal{UserID: ids.New("usr"), Kind: identity.UserSession, Scopes: []string{"quote:create", "order:create"}, Version: 1}
	run := func(q string, args ...any) {
		t.Helper()
		if _, e := local.Pool.Exec(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	run(`INSERT INTO p11.users(id,identity_provider,external_subject_digest,state) VALUES($1,'NATIVE_TEST',$2,'ACTIVE')`, p.UserID, outbox.Digest([]byte(p.UserID)))
	run(`INSERT INTO p11.role_grants(id,user_id,role_code,scope_ref,granted_by) VALUES($1,$2,'BUYER','self',$2)`, ids.New("grant"), p.UserID)
	q, err := (trade.Service{DB: local}).CreateQuote(ctx, p, f.Input(), ids.New("quote"))
	if err != nil {
		t.Fatal(err)
	}
	o, err := (trade.Service{DB: local}).CreateOrder(ctx, p, confirm(q), ids.New("order"))
	if err != nil {
		t.Fatal(err)
	}
	asset, job, attempt, lease := ids.New("ast"), ids.New("job"), ids.New("att"), ids.New("lease")
	run(`INSERT INTO p11.jobs(id,order_id,seller_id,caller_key,request_digest,state,deadline) VALUES($1,$2,$3,'historical-native-input',repeat('a',64),'CANCELED',clock_timestamp()+interval '1 hour')`, job, o.ID, f.Seller.UserID)
	run(`INSERT INTO p11.assets(id,order_id,owner_id,job_id,visibility,state,storage_key,storage_digest,bytes,media_type,expires_at)
VALUES($1::text,$2,$3,$4,'PLAINTEXT','READY',$1::text,repeat('b',64),12,'application/json',clock_timestamp()+interval '1 hour')`, asset, o.ID, p.UserID, job)
	run(`INSERT INTO p11.attempts(id,job_id,order_id,seller_id,device_id,resource_id,capability_id,fencing_token,resource_lock_generation,state)
VALUES($1,$2,$3,$4,$5,$6,$7,1,1,'EXPIRED')`, attempt, job, o.ID, f.Seller.UserID, f.Device, f.Resource, f.Capability)
	run(`INSERT INTO p11.execution_leases(id,attempt_id,job_id,fencing_token,resource_id,resource_lock_generation,issued_at,not_before,hard_deadline,execution_manifest_ref,execution_manifest_hash,state,expires_at,signed_payload,signature_profile_id,signature)
VALUES($1,$2,$3,1,$4,1,clock_timestamp()-interval '1 minute',clock_timestamp()-interval '2 minutes',clock_timestamp()+interval '1 hour',$5,repeat('c',64),'EXPIRED',clock_timestamp()-interval '30 seconds','{}','LEGACY_UNKNOWN','RETAINED_ORIGINAL_SIGNATURE')`, lease, attempt, job, f.Resource, ids.New("manifest"))
	if err = local.MigrateFS(ctx, source); err != nil {
		t.Fatal(err)
	}
	if err = local.VerifyMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	var profile, signature string
	var absent bool
	if err = local.Pool.QueryRow(ctx, `SELECT a.input_profile,l.admission_profile='UNBOUND_LEASE' AND l.issuer_key_id IS NULL AND l.execution_manifest IS NULL AND a.input_ciphertext IS NULL AND a.input_key_version IS NULL AND j.input_asset_id IS NULL,l.signature
FROM p11.assets a JOIN p11.jobs j ON j.id=a.job_id JOIN p11.execution_leases l ON l.job_id=j.id WHERE a.id=$1`, asset).Scan(&profile, &absent, &signature); err != nil || profile != "UNBOUND_ASSET" || !absent || signature != "RETAINED_ORIGINAL_SIGNATURE" {
		t.Fatal(profile, absent, signature, err)
	}
	if _, err = local.Pool.Exec(ctx, `UPDATE p11.assets SET input_profile=$2,input_key_version='input_one',input_ciphertext=decode(repeat('00',40),'hex') WHERE id=$1`, asset, invocation.InputProfile); err == nil {
		t.Fatal("historical asset retrofitted")
	}
	if _, err = local.Pool.Exec(ctx, `UPDATE p11.execution_leases SET admission_profile=$2,issuer_key_id='issuer_one',execution_manifest='{}',signature_profile_id=$3 WHERE id=$1`, lease, invocation.AdmissionProfile, executionlease.Profile); err == nil {
		t.Fatal("historical lease retrofitted")
	}
	var bound int
	if err = local.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM p11.assets WHERE input_profile<>'UNBOUND_ASSET')+(SELECT count(*) FROM p11.execution_leases WHERE admission_profile<>'UNBOUND_LEASE')`).Scan(&bound); err != nil || bound != 0 {
		t.Fatal(bound, err)
	}
}

func TestExecutionIssuedLeaseVerifiesAgainstIndependentPlanAndNativeInbox(t *testing.T) {
	requireDB(t)
	f, p, in, body := executionFixture(t, relay.ChatText)
	c := executionConfig(t)
	s := configuredExecution(t, c)
	readyExecution(t, s, p, in, body)
	issued := issueExecution(t, s, p, in)
	var stored []byte
	if err := db.Pool.QueryRow(ctx, `SELECT execution_manifest FROM p11.execution_leases WHERE id=$1`, issued.LeaseID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	var approved invocation.Manifest
	if err := json.Unmarshal(stored, &approved); err != nil || approved.Order != f.order.ID || approved.Device != f.offer.Device || approved.Resource != f.offer.Resource || approved.Capability != f.offer.Capability || approved.EnvironmentDigest != f.offer.Terms.Bindings[0].EnvironmentDigest || approved.InputTokenLimit != f.offer.Terms.Spec.InputTokenLimit {
		t.Fatal("independent original/kernel fixture approval failed", err)
	}
	raw, _ := json.Marshal(issued.Lease)
	v, err := executionlease.Verify(raw, c.SigningPrivateKey.Public().(ed25519.PublicKey), approved.ApprovedPlan(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "execution.db")
	if err = os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := execution.Open(ctx, path, execution.Binding{Installation: strings.Repeat("a", 64), LedgerID: strings.Repeat("b", 64), Device: f.offer.Device, Seller: f.offer.Seller.UserID}, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.RegisterResource(ctx, f.offer.Resource, strings.Repeat("c", 64)); err != nil {
		t.Fatal(err)
	}
	command := ids.New("cmd")
	hold, disposition, err := store.Receive(ctx, command, v)
	if err != nil || disposition != "RECEIVED" || hold.Resource() != f.offer.Resource {
		t.Fatal(disposition, err)
	}
	_, disposition, err = store.Receive(ctx, command, v)
	if err != nil || disposition != "QUERY_ONLY" {
		t.Fatal(disposition, err)
	}
	// No supervisor, model, GPU, phone, payment or network delivery is executed.
}
