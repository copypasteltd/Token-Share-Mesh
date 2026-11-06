package integration_test

// SOURCE_ONLY. These definitions preserve the full original signed synthetic
// payment/allocation/admission path and use the real session creation service.
import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/invocation"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/resultwire"
)

type contentFixture struct {
	completionFixture
	content  *invocation.ResultContent
	delivery invocation.ResultView
	head     resultwire.ContentHead
	session  identity.Sessions
}

func originalContent(t *testing.T) contentFixture {
	t.Helper()
	requireDB(t)
	sessions, _ := sessionService(t)
	issued := loginSession(t, sessions, "MINIAPP")
	credential, e := sessions.Authenticate(ctx, issued.Secret)
	if e != nil {
		t.Fatal(e)
	}
	f := completionOriginalBuyer(t, &credential.Principal)
	d := f.register(t)
	content, e := invocation.NewResultContent(f.service, "content_v1", map[string][]byte{"content_v1": bytes.Repeat([]byte{93}, 32)})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(content.Close)
	// Use the exact server-produced head below, rather than fabricating READY.
	var raw []byte
	in := resultwire.BeginContent{Scope: f.declaration.Scope, Delivery: d.ID}
	if _, e = content.Begin(ctx, f.p, f.issued.AttemptID, in, ids.New("begin")); e != nil {
		t.Fatal(e)
	}
	if e = db.Pool.QueryRow(ctx, `SELECT head_raw FROM p11.native_result_content WHERE delivery_id=$1`, d.ID).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	var head resultwire.ContentHead
	if e = json.Unmarshal(raw, &head); e != nil {
		t.Fatal(e)
	}
	return contentFixture{f, content, d, head, sessions}
}
func (f contentFixture) chunk() resultwire.ContentChunk {
	b := []byte("synthetic_result")
	return resultwire.ContentChunk{Profile: resultwire.ContentProfile, Scope: f.declaration.Scope, Delivery: f.delivery.ID, Head: f.head.Digest(), Index: 0, Digest: resultwire.BytesDigest(b), Content: b}
}
func (f contentFixture) ready(t *testing.T) {
	t.Helper()
	if _, e := f.content.Put(ctx, f.p, f.issued.AttemptID, f.chunk(), ids.New("chunk")); e != nil {
		t.Fatal(e)
	}
	if _, e := f.content.Seal(ctx, f.p, f.issued.AttemptID, resultwire.SealContent{Scope: f.declaration.Scope, Delivery: f.delivery.ID, Head: f.head.Digest()}, ids.New("seal")); e != nil {
		t.Fatal(e)
	}
}
func (f contentFixture) grant(t *testing.T, key string) (resultwire.IssuedReadGrant, string) {
	t.Helper()
	r, e := f.content.CreateReadGrant(ctx, f.buyer, f.issued.JobID, f.delivery.ID, resultwire.ReadGrantRequest{Head: f.head.Digest()}, key)
	if e != nil {
		t.Fatal(e)
	}
	var issued resultwire.IssuedReadGrant
	if e = json.Unmarshal(r.Body, &issued); e != nil {
		t.Fatal(e)
	}
	return issued, "p11r." + issued.Grant.ID + "." + issued.Secret
}

func TestOriginalContentUsesEncryptedRowsAndExactBuyerRead(t *testing.T) {
	f := originalContent(t)
	f.ready(t)
	g, token := f.grant(t, ids.New("grant"))
	head, e := f.content.ReadHead(ctx, token, f.delivery.ID)
	if e != nil || head.Validate() != nil || head.State != "READY" || head.Digest() != g.Grant.Head {
		t.Fatal(head, e)
	}
	chunk, e := f.content.ReadChunk(ctx, token, f.delivery.ID, 0)
	if e != nil || string(chunk.Content) != "synthetic_result" {
		t.Fatal(e)
	}
	var cipher []byte
	if e = db.Pool.QueryRow(ctx, `SELECT ciphertext FROM p11.native_result_content_chunks WHERE delivery_id=$1`, f.delivery.ID).Scan(&cipher); e != nil || bytes.Contains(cipher, chunk.Content) {
		t.Fatal("plaintext persisted", e)
	}
	q, l := f.counts(t)
	if q != 0 || l != 0 {
		t.Fatal("read granted acceptance or billing")
	}
}
func TestOriginalContentMissingChunkCannotSealOrGrant(t *testing.T) {
	f := originalContent(t)
	_, e := f.content.Seal(ctx, f.p, f.issued.AttemptID, resultwire.SealContent{Scope: f.declaration.Scope, Delivery: f.delivery.ID, Head: f.head.Digest()}, ids.New("seal"))
	if e == nil {
		t.Fatal("missing content sealed")
	}
	if _, e = f.content.CreateReadGrant(ctx, f.buyer, f.issued.JobID, f.delivery.ID, resultwire.ReadGrantRequest{Head: f.head.Digest()}, ids.New("grant")); e == nil {
		t.Fatal("incomplete content delegated")
	}
}
func TestOriginalContentChangedChunkCannotReplaceOriginal(t *testing.T) {
	f := originalContent(t)
	c := f.chunk()
	if _, e := f.content.Put(ctx, f.p, f.issued.AttemptID, c, ids.New("chunk")); e != nil {
		t.Fatal(e)
	}
	c.Content = []byte("different_result")
	c.Digest = resultwire.BytesDigest(c.Content)
	if _, e := f.content.Put(ctx, f.p, f.issued.AttemptID, c, ids.New("changed")); e == nil {
		t.Fatal("original content overwritten")
	}
}
func TestOriginalContentSealRejectsWrongAggregateEvenWithValidChunkDigest(t *testing.T) {
	f := originalContent(t)
	c := f.chunk()
	c.Content = []byte("different_result")
	c.Digest = resultwire.BytesDigest(c.Content)
	if _, e := f.content.Put(ctx, f.p, f.issued.AttemptID, c, ids.New("chunk")); e != nil {
		t.Fatal(e)
	}
	if _, e := f.content.Seal(ctx, f.p, f.issued.AttemptID, resultwire.SealContent{Scope: f.declaration.Scope, Delivery: f.delivery.ID, Head: f.head.Digest()}, ids.New("seal")); e == nil {
		t.Fatal("wrong aggregate sealed")
	}
}
func TestOriginalContentGrantReplaysWithoutPersistingReadSecret(t *testing.T) {
	f := originalContent(t)
	f.ready(t)
	key := ids.New("grant")
	a, _ := f.grant(t, key)
	b, _ := f.grant(t, key)
	if a != b {
		t.Fatal("read recovery changed credential")
	}
	var safe []byte
	if e := db.Pool.QueryRow(ctx, `SELECT safe_response FROM p11.idempotency_records WHERE operation_id='createOriginalResultReadGrant' AND response_reference=$1`, a.Grant.ID).Scan(&safe); e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(safe, []byte(a.Secret)) || bytes.Contains(safe, []byte("read_secret")) {
		t.Fatal("read secret entered replay metadata")
	}
}
func TestOriginalContentGrantCannotChangeDeliveryOrSecret(t *testing.T) {
	f := originalContent(t)
	f.ready(t)
	_, token := f.grant(t, ids.New("grant"))
	if _, e := f.content.ReadHead(ctx, token, ids.New("other")); e == nil {
		t.Fatal("credential crossed result")
	}
	parts := strings.Split(token, ".")
	parts[2] = base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	if _, e := f.content.ReadHead(ctx, strings.Join(parts, "."), f.delivery.ID); e == nil {
		t.Fatal("changed read secret accepted")
	}
}
func TestOriginalContentLogoutImmediatelyRevokesDelegatedReads(t *testing.T) {
	f := originalContent(t)
	f.ready(t)
	_, token := f.grant(t, ids.New("grant"))
	if _, e := f.session.Revoke(ctx, f.buyer); e != nil {
		t.Fatal(e)
	}
	if _, e := f.content.ReadHead(ctx, token, f.delivery.ID); e == nil {
		t.Fatal("logged-out read delegation remained live")
	}
}
func TestOriginalContentSellerCannotIssueBuyerDelegation(t *testing.T) {
	f := originalContent(t)
	f.ready(t)
	if _, e := f.content.CreateReadGrant(ctx, f.p, f.issued.JobID, f.delivery.ID, resultwire.ReadGrantRequest{Head: f.head.Digest()}, ids.New("grant")); e == nil {
		t.Fatal("device obtained account read delegation")
	}
}
func TestOriginalContentCancelRetiresKeysChunksAndReadGrantWithoutFreeingGPU(t *testing.T) {
	f := originalContent(t)
	f.ready(t)
	_, token := f.grant(t, ids.New("grant"))
	job := f.job(t)
	if _, e := f.service.Cancel(ctx, f.buyer, job.ID, invocation.CancelInput{ExpectedVersion: job.Version, Reason: "BUYER_REQUEST"}, ids.New("cancel")); e != nil {
		t.Fatal(e)
	}
	if _, e := f.content.ReadHead(ctx, token, f.delivery.ID); e == nil {
		t.Fatal("canceled content remained readable")
	}
	var state string
	var retained bool
	var chunks int
	if e := db.Pool.QueryRow(ctx, `SELECT state,wrapped_key IS NOT NULL,(SELECT count(*) FROM p11.native_result_content_chunks WHERE delivery_id=$1) FROM p11.native_result_content WHERE delivery_id=$1`, f.delivery.ID).Scan(&state, &retained, &chunks); e != nil || state != "RETIRED" || retained || chunks != 0 {
		t.Fatal(state, retained, chunks, e)
	}
	var phase string
	if e := db.Pool.QueryRow(ctx, `SELECT phase FROM p11.resource_locks WHERE resource_id=$1`, f.declaration.Scope.Resource).Scan(&phase); e != nil || phase == "IDLE" {
		t.Fatal("content retirement freed compute", phase, e)
	}
}
func TestOriginalContentSQLCannotRewriteFrozenBindingOrChunk(t *testing.T) {
	f := originalContent(t)
	f.ready(t)
	if _, e := db.Pool.Exec(ctx, `UPDATE p11.native_result_content SET head_digest=$2 WHERE delivery_id=$1`, f.delivery.ID, strings.Repeat("f", 64)); e == nil {
		t.Fatal("head rewritten")
	}
	if _, e := db.Pool.Exec(ctx, `DELETE FROM p11.native_result_content_chunks WHERE delivery_id=$1`, f.delivery.ID); e == nil {
		t.Fatal("live encrypted chunks deleted")
	}
}
