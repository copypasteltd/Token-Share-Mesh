package invocation

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/resultwire"
	"p11.local/compute/shared/strictjson"
)

// ResultContent carries only existing PLAINTEXT/STANDARD deliveries. Its keys
// cannot fund, run, accept, meter or settle a job. All network I/O is outside TX.
type ResultContent struct {
	completion *Completion
	mu         sync.RWMutex
	current    string
	keys       map[string][]byte
}

func NewResultContent(c *Completion, current string, keys map[string][]byte) (*ResultContent, error) {
	if c == nil || c.ready() != nil || !ids.Valid(current) || len(keys) < 1 || len(keys) > 16 || len(keys[current]) != 32 {
		return nil, fault.New(fault.Invalid, "Explicit original completion and independent content-key configuration required")
	}
	s := &ResultContent{completion: c, current: current, keys: map[string][]byte{}}
	for id, key := range keys {
		if !ids.Valid(id) || len(key) != 32 {
			s.Close()
			return nil, fault.New(fault.Invalid, "Bounded content-key ring required")
		}
		s.keys[id] = append([]byte(nil), key...)
	}
	return s, nil
}
func (s *ResultContent) UsesCompletion(c *Completion) bool { return s != nil && s.completion == c }
func (s *ResultContent) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, key := range s.keys {
		clear(key)
		delete(s.keys, id)
	}
}
func (s *ResultContent) key(version string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	k := s.keys[version]
	if len(k) != 32 {
		return nil, fault.New(fault.Unavailable, "Original result content key unavailable")
	}
	return append([]byte(nil), k...), nil
}
func contentSeal(key, plain []byte, aad string) ([]byte, error) {
	block, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	g, e := cipher.NewGCM(block)
	if e != nil {
		return nil, e
	}
	nonce := make([]byte, g.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return nil, e
	}
	return g.Seal(nonce, nonce, plain, []byte(aad)), nil
}
func contentOpen(key, sealed []byte, aad string) ([]byte, error) {
	block, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	g, e := cipher.NewGCM(block)
	if e != nil || len(sealed) < 28 {
		return nil, fault.New(fault.Unavailable, "Original encrypted content unavailable")
	}
	plain, e := g.Open(nil, sealed[:12], sealed[12:], []byte(aad))
	if e != nil {
		return nil, fault.New(fault.Unavailable, "Original encrypted content unavailable")
	}
	return plain, nil
}

type contentRecord struct {
	head                   resultwire.ContentHead
	digest, version, state string
	wrapped, terminal      []byte
	expires                time.Time
}

func (s *ResultContent) contentTx(ctx context.Context, tx pgx.Tx, id string, write bool) (contentRecord, error) {
	var c contentRecord
	var raw []byte
	lock := " FOR SHARE OF c,j,d"
	if write {
		lock = " FOR UPDATE OF c"
	}
	e := tx.QueryRow(ctx, `SELECT c.head_raw,c.head_digest,c.key_version,c.wrapped_key,c.terminal_ciphertext,c.state,c.expires_at FROM p11.native_result_content c JOIN p11.native_result_deliveries n ON n.delivery_id=c.delivery_id JOIN p11.jobs j ON j.id=c.job_id JOIN p11.deliveries d ON d.id=c.delivery_id WHERE c.delivery_id=$1 AND c.state<>'RETIRED' AND c.expires_at>clock_timestamp() AND j.current_attempt_id=n.attempt_id AND j.state IN ('WAITING_BUYER','ACCEPTED') AND d.state IN ('WAITING_BUYER','VALID')`+lock, id).Scan(&raw, &c.digest, &c.version, &c.wrapped, &c.terminal, &c.state, &c.expires)
	if e == pgx.ErrNoRows {
		return c, fault.New(fault.NotFound, "Original live result content unavailable")
	}
	if e != nil {
		return c, e
	}
	if strictjson.Decode(raw, &c.head, 24576) != nil || c.head.Validate() != nil || c.head.Digest() != c.digest || c.head.Delivery != id {
		return c, fault.New(fault.Unavailable, "Original content metadata requires recovery")
	}
	return c, nil
}
func (s *ResultContent) dataKey(c contentRecord) ([]byte, error) {
	key, e := s.key(c.version)
	if e != nil {
		return nil, e
	}
	defer clear(key)
	v, e := contentOpen(key, c.wrapped, "P11_RESULT_KEY_1\n"+c.digest)
	if e != nil || len(v) != 32 {
		clear(v)
		return nil, fault.New(fault.Unavailable, "Original result key unavailable")
	}
	return v, nil
}
func chunkAAD(head string, index int, digest string) string {
	return "P11_RESULT_CHUNK_1\n" + head + "\n" + strconv.Itoa(index) + "\n" + digest
}
func safeContentAck(h resultwire.ContentHead, state string, index *int, digest string) outbox.StoredResult {
	raw, _ := json.Marshal(resultwire.ContentAck{Delivery: h.Delivery, Head: h.Digest(), Index: index, Digest: digest, State: state})
	return outbox.StoredResult{Reference: h.Delivery, Body: raw, HTTPStatus: 200}
}

func (s *ResultContent) Begin(ctx context.Context, p identity.Principal, attempt string, in resultwire.BeginContent, key string) (ReportResult, error) {
	if in.Scope.Validate() != nil || !ids.Valid(in.Delivery) || attempt != in.Scope.Attempt {
		return ReportResult{}, fault.New(fault.Invalid, "Original result upload binding required")
	}
	return s.completion.nativeCommand(ctx, p, "beginOriginalResultContent", attempt, in.Scope.Lease, in.Scope.Fence, key, in, func(tx pgx.Tx, a reportAuthority) (outbox.StoredResult, error) {
		var declared, manifest []byte
		e := tx.QueryRow(ctx, `SELECT n.payload_raw,l.execution_manifest FROM p11.native_result_deliveries n JOIN p11.execution_leases l ON l.id=n.lease_id JOIN p11.deliveries d ON d.id=n.delivery_id JOIN p11.jobs j ON j.id=n.job_id WHERE n.delivery_id=$1 AND n.attempt_id=$2 AND j.current_attempt_id=n.attempt_id AND j.state IN ('WAITING_BUYER','ACCEPTED') AND d.state IN ('WAITING_BUYER','VALID')`, in.Delivery, attempt).Scan(&declared, &manifest)
		if e == pgx.ErrNoRows {
			return outbox.StoredResult{}, fault.New(fault.NotFound, "Original current delivery required")
		}
		if e != nil {
			return outbox.StoredResult{}, e
		}
		var d resultwire.Delivery
		var m executionlease.Manifest
		if strictjson.Decode(declared, &d, 8192) != nil || strictjson.Decode(manifest, &m, 16384) != nil || d.Scope != in.Scope || in.Scope != resultwire.Original(a.lease) || d.Verify(a.public) != nil {
			return outbox.StoredResult{}, fault.New(fault.Forbidden, "Exact original signed delivery required")
		}
		completed, _ := time.Parse(time.RFC3339Nano, d.Completed)
		h := resultwire.ContentHead{Profile: resultwire.ContentProfile, Delivery: in.Delivery, Declaration: d, Public: base64.RawURLEncoding.EncodeToString(a.public), Manifest: m, IncludeUsage: in.IncludeUsage, ChunkBytes: resultwire.ChunkBytes, Chunks: resultwire.ChunkCount(d.OutputBytes), Expires: completed.Add(30 * time.Minute).UTC().Format(time.RFC3339Nano), State: "UPLOADING"}
		if h.Validate() != nil || !a.now.Before(completed.Add(30*time.Minute)) {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Original content scope or retention is unavailable")
		}
		var existing string
		e = tx.QueryRow(ctx, `SELECT head_digest FROM p11.native_result_content WHERE delivery_id=$1 FOR UPDATE`, in.Delivery).Scan(&existing)
		if e == nil {
			if existing != h.Digest() {
				return outbox.StoredResult{}, fault.New(fault.Conflict, "Original result upload cannot be replaced")
			}
			return safeContentAck(h, "UPLOADING", nil, ""), nil
		}
		if e != pgx.ErrNoRows {
			return outbox.StoredResult{}, e
		}
		data := make([]byte, 32)
		if _, e = rand.Read(data); e != nil {
			return outbox.StoredResult{}, e
		}
		defer clear(data)
		master, e := s.key(s.current)
		if e != nil {
			return outbox.StoredResult{}, e
		}
		defer clear(master)
		wrapped, e := contentSeal(master, data, "P11_RESULT_KEY_1\n"+h.Digest())
		if e != nil {
			return outbox.StoredResult{}, e
		}
		raw, _ := json.Marshal(h)
		_, e = tx.Exec(ctx, `INSERT INTO p11.native_result_content(delivery_id,job_id,order_id,head_raw,head_digest,key_version,wrapped_key,state,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,'UPLOADING',$8)`, h.Delivery, in.Scope.Job, in.Scope.Order, raw, h.Digest(), s.current, wrapped, completed.Add(30*time.Minute))
		return safeContentAck(h, "UPLOADING", nil, ""), e
	})
}
func (s *ResultContent) Put(ctx context.Context, p identity.Principal, attempt string, in resultwire.ContentChunk, key string) (ReportResult, error) {
	if in.Validate() != nil || in.Scope.Attempt != attempt {
		return ReportResult{}, fault.New(fault.Invalid, "Original bounded chunk required")
	}
	return s.completion.nativeCommand(ctx, p, "putOriginalResultChunk", attempt, in.Scope.Lease, in.Scope.Fence, key, in, func(tx pgx.Tx, a reportAuthority) (outbox.StoredResult, error) {
		c, e := s.contentTx(ctx, tx, in.Delivery, true)
		if e != nil {
			return outbox.StoredResult{}, e
		}
		if c.digest != in.Head || c.head.Declaration.Scope != in.Scope || in.Scope != resultwire.Original(a.lease) {
			return outbox.StoredResult{}, fault.New(fault.Forbidden, "Original chunk scope differs")
		}
		expected := int64(resultwire.ChunkBytes)
		if left := c.head.Declaration.OutputBytes - int64(in.Index)*resultwire.ChunkBytes; left < expected {
			expected = left
		}
		if in.Index >= c.head.Chunks || int64(len(in.Content)) != expected {
			return outbox.StoredResult{}, fault.New(fault.Invalid, "Original fixed chunk boundary required")
		}
		var old string
		e = tx.QueryRow(ctx, `SELECT content_digest FROM p11.native_result_content_chunks WHERE delivery_id=$1 AND chunk_index=$2`, in.Delivery, in.Index).Scan(&old)
		if e == nil {
			if old != in.Digest {
				return outbox.StoredResult{}, fault.New(fault.Conflict, "Original chunk cannot change")
			}
			return safeContentAck(c.head, c.state, &in.Index, in.Digest), nil
		}
		if e != pgx.ErrNoRows {
			return outbox.StoredResult{}, e
		}
		if c.state != "UPLOADING" {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Original upload is sealed")
		}
		data, e := s.dataKey(c)
		if e != nil {
			return outbox.StoredResult{}, e
		}
		defer clear(data)
		sealed, e := contentSeal(data, in.Content, chunkAAD(c.digest, in.Index, in.Digest))
		if e != nil {
			return outbox.StoredResult{}, e
		}
		_, e = tx.Exec(ctx, `INSERT INTO p11.native_result_content_chunks(delivery_id,chunk_index,plaintext_bytes,content_digest,ciphertext) VALUES($1,$2,$3,$4,$5)`, in.Delivery, in.Index, len(in.Content), in.Digest, sealed)
		return safeContentAck(c.head, "UPLOADING", &in.Index, in.Digest), e
	})
}
func (s *ResultContent) Seal(ctx context.Context, p identity.Principal, attempt string, in resultwire.SealContent, key string) (ReportResult, error) {
	if in.Scope.Validate() != nil || in.Scope.Attempt != attempt || !ids.Valid(in.Delivery) || !executionlease.Hash(in.Head) {
		return ReportResult{}, fault.New(fault.Invalid, "Original content seal binding required")
	}
	return s.completion.nativeCommand(ctx, p, "sealOriginalResultContent", attempt, in.Scope.Lease, in.Scope.Fence, key, in, func(tx pgx.Tx, a reportAuthority) (outbox.StoredResult, error) {
		c, e := s.contentTx(ctx, tx, in.Delivery, true)
		if e != nil {
			return outbox.StoredResult{}, e
		}
		if c.digest != in.Head || c.head.Declaration.Scope != in.Scope || in.Scope != resultwire.Original(a.lease) || in.End.Validate(c.head.Declaration.Finish) != nil {
			return outbox.StoredResult{}, fault.New(fault.Invalid, "Original bounded terminal content required")
		}
		data, e := s.dataKey(c)
		if e != nil {
			return outbox.StoredResult{}, e
		}
		defer clear(data)
		if c.state == "READY" {
			old, e := contentOpen(data, c.terminal, "P11_RESULT_END_1\n"+c.digest)
			if e != nil {
				return outbox.StoredResult{}, e
			}
			defer clear(old)
			var prior resultwire.End
			if strictjson.Decode(old, &prior, 1024) != nil || prior != in.End {
				return outbox.StoredResult{}, fault.New(fault.Conflict, "Original terminal cannot change")
			}
			return safeContentAck(c.head, "READY", nil, ""), nil
		}
		rows, e := tx.Query(ctx, `SELECT chunk_index,content_digest,ciphertext FROM p11.native_result_content_chunks WHERE delivery_id=$1 ORDER BY chunk_index`, in.Delivery)
		if e != nil {
			return outbox.StoredResult{}, e
		}
		sum := sha256.New()
		var total int64
		index := 0
		var tail []byte
		for rows.Next() {
			var i int
			var digest string
			var sealed []byte
			if e = rows.Scan(&i, &digest, &sealed); e != nil {
				break
			}
			var b []byte
			b, e = contentOpen(data, sealed, chunkAAD(c.digest, i, digest))
			if e != nil {
				break
			}
			if i != index || resultwire.BytesDigest(b) != digest {
				clear(b)
				e = fault.New(fault.Conflict, "Original chunk sequence differs")
				break
			}
			total += int64(len(b))
			_, _ = sum.Write(b)
			joined := append(tail, b...)
			clear(b)
			tail = nil
			at := 0
			for at < len(joined) && utf8.FullRune(joined[at:]) {
				r, n := utf8.DecodeRune(joined[at:])
				if r == utf8.RuneError && n == 1 {
					e = fault.New(fault.Invalid, "Original output is not UTF-8")
					break
				}
				at += n
			}
			if e == nil {
				tail = append([]byte(nil), joined[at:]...)
			}
			clear(joined)
			if e != nil {
				break
			}
			index++
		}
		if e == nil {
			e = rows.Err()
		}
		rows.Close()
		defer clear(tail)
		if e != nil {
			return outbox.StoredResult{}, e
		}
		if index != c.head.Chunks || total != c.head.Declaration.OutputBytes || len(tail) != 0 || hex.EncodeToString(sum.Sum(nil)) != c.head.Declaration.OutputHash {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Complete original content digest required")
		}
		end, _ := json.Marshal(in.End)
		defer clear(end)
		sealed, e := contentSeal(data, end, "P11_RESULT_END_1\n"+c.digest)
		if e != nil {
			return outbox.StoredResult{}, e
		}
		_, e = tx.Exec(ctx, `UPDATE p11.native_result_content SET state='READY',terminal_ciphertext=$2 WHERE delivery_id=$1 AND state='UPLOADING'`, in.Delivery, sealed)
		if e == nil {
			e = completionEvent(ctx, tx, in.Scope.Job, "OriginalResultContentReady", map[string]any{"delivery_id": in.Delivery, "head_digest": c.digest})
		}
		return safeContentAck(c.head, "READY", nil, ""), e
	})
}

func (s *ResultContent) readSecret(version, id string) ([]byte, error) {
	master, e := s.key(version)
	if e != nil {
		return nil, e
	}
	defer clear(master)
	mac := hmac.New(sha256.New, master)
	_, _ = mac.Write([]byte("P11_RESULT_READ_SECRET_1\n" + id))
	return mac.Sum(nil), nil
}
func (s *ResultContent) CreateReadGrant(ctx context.Context, p identity.Principal, job, delivery string, in resultwire.ReadGrantRequest, key string) (ReportResult, error) {
	if !ids.Valid(job) || !ids.Valid(delivery) || !ids.Valid(p.SessionID) || !executionlease.Hash(in.Head) {
		return ReportResult{}, fault.New(fault.Invalid, "Original buyer session and content digest required")
	}
	raw, _ := json.Marshal(in)
	v, e := (outbox.Idempotency{DB: s.completion.db}).DoAuthorized(ctx, outbox.Command{Principal: p.UserID, PrincipalVersion: p.Version, Operation: "createOriginalResultReadGrant", Resource: job + "/" + delivery, Key: key, Request: raw}, func(tx pgx.Tx) error { _, e := s.completion.participantTx(ctx, tx, p, job, "job:read", true); return e }, func(tx pgx.Tx) (outbox.StoredResult, error) {
		c, e := s.contentTx(ctx, tx, delivery, true)
		if e != nil {
			return outbox.StoredResult{}, e
		}
		if c.head.Declaration.Scope.Job != job || c.digest != in.Head || c.state != "READY" {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Original ready result required")
		}
		var now time.Time
		var active int
		if e = tx.QueryRow(ctx, `SELECT clock_timestamp(),(SELECT count(*) FROM p11.native_result_read_grants WHERE delivery_id=$1 AND revoked_at IS NULL AND expires_at>clock_timestamp())`, delivery).Scan(&now, &active); e != nil {
			return outbox.StoredResult{}, e
		}
		if active >= 8 {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Read delegation capacity reached; wait for existing credentials to expire")
		}
		expires := now.Add(5 * time.Minute)
		if c.expires.Before(expires) {
			expires = c.expires
		}
		g := resultwire.ReadGrant{Profile: resultwire.GrantProfile, ID: ids.New("read"), Order: c.head.Declaration.Scope.Order, Job: job, Seller: c.head.Declaration.Scope.Seller, Delivery: delivery, Head: c.digest, Expires: expires.UTC().Format(time.RFC3339Nano)}
		secret, e := s.readSecret(c.version, g.ID)
		if e != nil {
			return outbox.StoredResult{}, e
		}
		defer clear(secret)
		_, e = tx.Exec(ctx, `INSERT INTO p11.native_result_read_grants(id,delivery_id,job_id,order_id,buyer_id,session_id,buyer_version,head_digest,token_digest,key_version,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, g.ID, delivery, job, g.Order, p.UserID, p.SessionID, p.Version, g.Head, outbox.Digest(secret), c.version, expires)
		safe, _ := json.Marshal(g)
		return outbox.StoredResult{Reference: g.ID, Body: safe, HTTPStatus: 200}, e
	})
	if e != nil {
		return ReportResult{}, e
	}
	// Only safe metadata enters outbox. Regenerate this limited read secret after
	// commit; do not persist the issued response or a full account credential.
	var g resultwire.ReadGrant
	if strictjson.Decode(v.Body, &g, 4096) != nil {
		return ReportResult{}, fault.New(fault.Unavailable, "Original read grant metadata unavailable")
	}
	var version string
	e = s.completion.db.Pool.QueryRow(ctx, `SELECT key_version FROM p11.native_result_read_grants WHERE id=$1 AND buyer_id=$2 AND session_id=$3 AND revoked_at IS NULL AND expires_at>clock_timestamp()`, g.ID, p.UserID, p.SessionID).Scan(&version)
	if e == pgx.ErrNoRows {
		return ReportResult{}, fault.New(fault.Conflict, "Original read grant expired; request a new read delegation")
	}
	if e != nil {
		return ReportResult{}, e
	}
	secret, e := s.readSecret(version, g.ID)
	if e != nil {
		return ReportResult{}, e
	}
	defer clear(secret)
	body, _ := json.Marshal(resultwire.IssuedReadGrant{Grant: g, Secret: base64.RawURLEncoding.EncodeToString(secret)})
	return ReportResult{Body: body, Replayed: v.Replayed}, nil
}

// readGrantTx delegates READ ONLY. It never constructs a UserSession principal
// or accepts a session/token/scope from request JSON. Logout invalidates it.
func (s *ResultContent) readGrantTx(ctx context.Context, tx pgx.Tx, token, delivery string) (contentRecord, error) {
	var zero contentRecord
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != "p11r" || !ids.Valid(parts[1]) || len(parts[2]) != 43 || !ids.Valid(delivery) {
		return zero, fault.New(fault.Unauthenticated, "Original read-only credential required")
	}
	secret, e := base64.RawURLEncoding.Strict().DecodeString(parts[2])
	if e != nil || len(secret) != 32 || base64.RawURLEncoding.EncodeToString(secret) != parts[2] {
		clear(secret)
		return zero, fault.New(fault.Unauthenticated, "Original read-only credential required")
	}
	defer clear(secret)
	var job, order string
	e = tx.QueryRow(ctx, `SELECT job_id,order_id FROM p11.native_result_read_grants WHERE id=$1 AND delivery_id=$2 AND token_digest=$3`, parts[1], delivery, outbox.Digest(secret)).Scan(&job, &order)
	if e == pgx.ErrNoRows {
		return zero, fault.New(fault.Unauthenticated, "Original read-only credential unavailable")
	}
	if e != nil {
		return zero, e
	}
	for _, lock := range []struct{ sql, id string }{{`SELECT id FROM p11.orders WHERE id=$1 FOR SHARE`, order}, {`SELECT id FROM p11.jobs WHERE id=$1 FOR SHARE`, job}} {
		var id string
		if e = tx.QueryRow(ctx, lock.sql, lock.id).Scan(&id); e != nil {
			return zero, e
		}
	}
	var id, head string
	e = tx.QueryRow(ctx, `SELECT g.id,g.head_digest FROM p11.native_result_read_grants g JOIN p11.orders o ON o.id=g.order_id AND o.buyer_id=g.buyer_id JOIN p11.users u ON u.id=g.buyer_id AND u.state='ACTIVE' AND u.version=g.buyer_version JOIN p11.auth_sessions ss ON ss.id=g.session_id AND ss.user_id=g.buyer_id AND ss.user_version=g.buyer_version JOIN p11.role_grants r ON r.user_id=g.buyer_id AND r.role_code='BUYER' AND r.scope_ref='self' WHERE g.id=$1 AND g.delivery_id=$2 AND g.token_digest=$3 AND g.revoked_at IS NULL AND g.expires_at>clock_timestamp() AND ss.revoked_at IS NULL AND ss.expires_at>clock_timestamp() AND r.revoked_at IS NULL AND (r.expires_at IS NULL OR r.expires_at>clock_timestamp()) FOR SHARE OF g,u,ss,r`, parts[1], delivery, outbox.Digest(secret)).Scan(&id, &head)
	if e == pgx.ErrNoRows {
		return zero, fault.New(fault.Unauthenticated, "Original read-only delegation no longer active")
	}
	if e != nil {
		return zero, e
	}
	c, e := s.contentTx(ctx, tx, delivery, false)
	if e == nil && (c.digest != head || c.state != "READY") {
		e = fault.New(fault.NotFound, "Original ready content unavailable")
	}
	return c, e
}
func (s *ResultContent) ReadHead(ctx context.Context, token, delivery string) (resultwire.ContentHead, error) {
	var h resultwire.ContentHead
	e := s.completion.db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		c, e := s.readGrantTx(ctx, tx, token, delivery)
		if e != nil {
			return e
		}
		key, e := s.dataKey(c)
		if e != nil {
			return e
		}
		defer clear(key)
		plain, e := contentOpen(key, c.terminal, "P11_RESULT_END_1\n"+c.digest)
		if e != nil {
			return e
		}
		defer clear(plain)
		var end resultwire.End
		if strictjson.Decode(plain, &end, 1024) != nil || end.Validate(c.head.Declaration.Finish) != nil {
			return fault.New(fault.Unavailable, "Original terminal content unavailable")
		}
		h = c.head
		h.State = "READY"
		h.End = &end
		return nil
	})
	return h, e
}
func (s *ResultContent) ReadChunk(ctx context.Context, token, delivery string, index int) (resultwire.ContentChunk, error) {
	var out resultwire.ContentChunk
	if index < 0 || index >= 1024 {
		return out, fault.New(fault.Invalid, "Bounded original chunk index required")
	}
	e := s.completion.db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		c, e := s.readGrantTx(ctx, tx, token, delivery)
		if e != nil {
			return e
		}
		var digest string
		var sealed []byte
		e = tx.QueryRow(ctx, `SELECT content_digest,ciphertext FROM p11.native_result_content_chunks WHERE delivery_id=$1 AND chunk_index=$2`, delivery, index).Scan(&digest, &sealed)
		if e == pgx.ErrNoRows {
			return fault.New(fault.NotFound, "Original chunk unavailable")
		}
		if e != nil {
			return e
		}
		key, e := s.dataKey(c)
		if e != nil {
			return e
		}
		defer clear(key)
		plain, e := contentOpen(key, sealed, chunkAAD(c.digest, index, digest))
		if e != nil {
			return e
		}
		out = resultwire.ContentChunk{Profile: resultwire.ContentProfile, Scope: c.head.Declaration.Scope, Delivery: delivery, Head: c.digest, Index: index, Digest: digest, Content: plain}
		if out.Validate() != nil {
			clear(plain)
			return fault.New(fault.Unavailable, "Original chunk integrity unavailable")
		}
		return nil
	})
	if e != nil {
		clear(out.Content)
		out = resultwire.ContentChunk{}
	}
	return out, e
}
func retireResultContentTx(ctx context.Context, tx pgx.Tx, job string) error {
	_, e := retireResultRowsTx(ctx, tx, job, false)
	return e
}
func retireResultRowsTx(ctx context.Context, tx pgx.Tx, job string, expiredOnly bool) (int, error) {
	tag, e := tx.Exec(ctx, `UPDATE p11.native_result_content SET state='RETIRED',wrapped_key=NULL,terminal_ciphertext=NULL WHERE job_id=$1 AND state<>'RETIRED' AND (NOT $2::boolean OR expires_at<=clock_timestamp())`, job, expiredOnly)
	if e != nil {
		return 0, e
	}
	if _, e = tx.Exec(ctx, `DELETE FROM p11.native_result_content_chunks WHERE delivery_id IN(SELECT delivery_id FROM p11.native_result_content WHERE job_id=$1 AND state='RETIRED')`, job); e != nil {
		return 0, e
	}
	_, e = tx.Exec(ctx, `UPDATE p11.native_result_read_grants SET revoked_at=clock_timestamp() WHERE job_id=$1 AND revoked_at IS NULL AND delivery_id IN(SELECT delivery_id FROM p11.native_result_content WHERE job_id=$1 AND state='RETIRED')`, job)
	return int(tag.RowsAffected()), e
}

// SweepExpired retires only confirmed expired original content. It has no
// native runtime or resource-release path and never infers buyer acceptance.
func (s *ResultContent) SweepExpired(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 32 {
		return 0, fault.New(fault.Invalid, "Bounded result retirement batch required")
	}
	rows, e := s.completion.db.Pool.Query(ctx, `SELECT DISTINCT job_id,order_id FROM p11.native_result_content WHERE state<>'RETIRED' AND expires_at<=clock_timestamp() ORDER BY job_id,order_id LIMIT $1`, limit)
	if e != nil {
		return 0, e
	}
	type item struct{ job, order string }
	var items []item
	for rows.Next() {
		var it item
		if e = rows.Scan(&it.job, &it.order); e != nil {
			break
		}
		items = append(items, it)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return 0, e
	}
	count := 0
	for _, it := range items {
		retired := 0
		e = s.completion.db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
			for _, v := range []struct{ q, id string }{{`SELECT id FROM p11.orders WHERE id=$1 FOR UPDATE`, it.order}, {`SELECT id FROM p11.jobs WHERE id=$1 FOR UPDATE`, it.job}} {
				var id string
				if e := tx.QueryRow(ctx, v.q, v.id).Scan(&id); e != nil {
					return e
				}
			}
			var e error
			retired, e = retireResultRowsTx(ctx, tx, it.job, true)
			return e
		})
		if e != nil {
			return count, e
		}
		count += retired
	}
	return count, nil
}
