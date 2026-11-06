package invocation

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"time"

	"github.com/jackc/pgx/v5"
	relay "p11.local/compute/gateway/core"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/persistence"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/resultwire"
	"p11.local/compute/shared/strictjson"
)

// SDK connects an explicitly funded original text route to admission and
// read-only original results. It cannot accept results, sign trusted usage,
// create payments or switch a seller. It owns only its issuer's copied keys.
type SDK struct {
	execution *Execution
	content   *ResultContent
}

type SDKConfig struct {
	Delivery          *Delivery
	Content           *ResultContent
	InputVersion      string
	SigningKeyID      string
	SigningPrivateKey ed25519.PrivateKey
}

func NewSDK(c SDKConfig) (*SDK, error) {
	d, content := c.Delivery, c.Content
	if d == nil || d.claims == nil || d.invocations == nil || content == nil || content.completion == nil ||
		!content.completion.UsesClaims(d.claims) || !content.completion.UsesDB(d.invocations.db) ||
		len(c.SigningPrivateKey) != ed25519.PrivateKeySize {
		return nil, fault.New(fault.Invalid, "SDK requires the same original delivery, completion and independently pinned issuer")
	}
	d.claims.mu.RLock()
	pins := make(map[string]ed25519.PublicKey, len(d.claims.verifiers))
	for id, pub := range d.claims.verifiers {
		pins[id] = append(ed25519.PublicKey(nil), pub...)
	}
	d.claims.mu.RUnlock()
	if !bytes.Equal(pins[c.SigningKeyID], c.SigningPrivateKey.Public().(ed25519.PublicKey)) {
		return nil, fault.New(fault.Invalid, "SDK issuer must already have an independently configured claim pin")
	}
	d.mu.RLock()
	input := make(map[string][]byte, len(d.keys))
	for id, k := range d.keys {
		input[id] = append([]byte(nil), k...)
	}
	d.mu.RUnlock()
	defer func() {
		for _, k := range input {
			clear(k)
		}
	}()
	// Content wrapping, input encryption, HMAC fingerprints and signing retain
	// separate purposes even when the optional SDK services share one host.
	content.mu.RLock()
	d.invocations.mu.RLock()
	reused := len(content.keys) == 0
	for _, k := range content.keys {
		if bytes.Equal(k, c.SigningPrivateKey.Seed()) {
			reused = true
		}
		for _, v := range input {
			if bytes.Equal(k, v) {
				reused = true
			}
		}
		for _, v := range d.invocations.keys {
			if bytes.Equal(k, v) {
				reused = true
			}
		}
	}
	d.invocations.mu.RUnlock()
	content.mu.RUnlock()
	if reused {
		return nil, fault.New(fault.Invalid, "Independent SDK key purposes required")
	}
	e, err := NewExecution(ExecutionConfig{Invocations: d.invocations, InputVersion: c.InputVersion, InputKeys: input,
		SigningKeyID: c.SigningKeyID, SigningPrivateKey: c.SigningPrivateKey, VerificationKeys: pins})
	if err != nil {
		return nil, err
	}
	return &SDK{execution: e, content: content}, nil
}

func (s *SDK) Close() {
	if s != nil && s.execution != nil {
		s.execution.Close()
	}
}
func (s *SDK) UsesDB(db *persistence.DB) bool {
	return s != nil && s.execution != nil && s.execution.invocations != nil && s.execution.invocations.db == db &&
		s.content != nil && s.content.completion.UsesDB(db)
}

func sdkPrincipal(p identity.Principal) error {
	if p.Kind != identity.APIKey || !ids.Valid(p.KeyID) || len(p.RouteIDs) < 1 || len(p.RouteIDs) > 50 {
		return fault.New(fault.Forbidden, "Original route-bound SDK credential required")
	}
	return p.Require("route:invoke", "")
}

// Begin validates the unmodified native body before one atomic preparation,
// encrypted input and first issuance transaction. A lost commit is recovered
// with the SAME key and body. Replays never mint a replacement lease or buyer
// acceptance. Live SSE requires a separate authenticated live transport; it is
// deliberately rejected before admission rather than replaying a finished file.
func (s *SDK) Begin(ctx context.Context, p identity.Principal, profile relay.Profile, key string, body []byte) (Preparation, error) {
	var result Preparation
	if s == nil || s.execution == nil || !s.UsesDB(s.execution.invocations.db) {
		return result, fault.New(fault.Unavailable, "Original SDK service unavailable")
	}
	if err := sdkPrincipal(p); err != nil {
		return result, err
	}
	model, meta, err := relay.Inspect(profile, body)
	if err != nil || meta.Stream {
		return result, fault.New(fault.Invalid, "Supported bounded non-streaming native text request required")
	}
	in := Request{Model: model, Protocol: profile, RequestDigest: meta.Digest, MaxOutputTokens: meta.MaxOutputTokens, Stream: false, IdempotencyKey: key}
	if err = validate(in); err != nil {
		return result, err
	}
	err = s.execution.invocations.db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		var e error
		result, e = s.execution.invocations.PrepareTx(ctx, tx, p, in)
		if e != nil {
			return e
		}
		originalReplay := result.Replayed
		if result.State == "INPUT_PENDING" {
			r, e := s.execution.ReadyTx(ctx, tx, p, in, body)
			if e != nil {
				return e
			}
			result = r.Preparation
			result.State = "QUEUED"
		}
		if result.State == "QUEUED" {
			r, e := s.execution.IssueTx(ctx, tx, p, in)
			if e != nil {
				return e
			}
			result = r.Preparation
		}
		result.Replayed = originalReplay
		return nil
	})
	if err != nil {
		return Preparation{}, err
	}
	return result, nil
}

// SDKState carries original read-only result authority, never an execution
// lease or access token. Head.End can contain the private matched stop string;
// it stays inside this plaintext content domain and must not enter journals.
type SDKState struct {
	Job, Order, Route, Seller, State string
	Spec                             Spec
	Head                             *resultwire.ContentHead
}

// sdkJobTx follows identity -> original order -> job. It rechecks the current
// API key's stored route membership on EVERY poll/chunk, without impersonating
// a user session or borrowing the session-only result-read grant mechanism.
func (s *SDK) sdkJobTx(ctx context.Context, tx pgx.Tx, p identity.Principal, job string) (SDKState, error) {
	var out SDKState
	if err := sdkPrincipal(p); err != nil {
		return out, err
	}
	if !ids.Valid(job) {
		return out, fault.New(fault.Invalid, "Original job required")
	}
	if err := identity.RequireRoleTx(ctx, tx, p, "route:invoke", "BUYER", false); err != nil {
		return out, err
	}
	var order, route string
	err := tx.QueryRow(ctx, `SELECT j.order_id,j.route_contract_id FROM p11.jobs j JOIN p11.orders o ON o.id=j.order_id JOIN p11.route_contracts r ON r.id=j.route_contract_id WHERE j.id=$1 AND j.invoker_key_id=$2 AND j.invocation_profile=$3 AND o.buyer_id=$4 AND r.buyer_id=$4 AND j.route_contract_id=ANY($5::text[])`, job, p.KeyID, Profile, p.UserID, p.RouteIDs).Scan(&order, &route)
	if err == pgx.ErrNoRows {
		return out, fault.New(fault.NotFound, "Original SDK job unavailable")
	}
	if err != nil {
		return out, err
	}
	var id string
	if err = tx.QueryRow(ctx, `SELECT id FROM p11.orders WHERE id=$1 AND buyer_id=$2 FOR SHARE`, order, p.UserID).Scan(&id); err != nil {
		return out, err
	}
	out.Job, out.Order, out.Route = job, order, route
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT j.state,j.seller_id,j.invocation_spec FROM p11.jobs j JOIN p11.route_contracts r ON r.id=j.route_contract_id JOIN p11.orders o ON o.id=j.order_id WHERE j.id=$1 AND j.order_id=$2 AND j.route_contract_id=$3 AND j.invoker_key_id=$4 AND j.invocation_profile=$5 AND r.binding_profile='CONFIRMED_ROUTE_1' AND r.buyer_id=$6 AND o.buyer_id=$6 AND o.seller_id=j.seller_id FOR SHARE OF j,r`, job, order, route, p.KeyID, Profile, p.UserID).Scan(&out.State, &out.Seller, &raw)
	if err != nil {
		return SDKState{}, err
	}
	var spec Spec
	if strictjson.Decode(raw, &spec, 4096) != nil || spec.Stream || (spec.Protocol != relay.ChatText && spec.Protocol != relay.ResponsesText && spec.Protocol != relay.MessagesText) {
		return SDKState{}, fault.New(fault.Unavailable, "Original native SDK contract unavailable")
	}
	out.Spec = spec
	return out, nil
}

func (s *SDK) readyContentTx(ctx context.Context, tx pgx.Tx, state SDKState) (contentRecord, bool, error) {
	var zero contentRecord
	if state.State != "WAITING_BUYER" && state.State != "ACCEPTED" {
		return zero, false, nil
	}
	rows, err := tx.Query(ctx, `SELECT d.id,c.state,c.expires_at>clock_timestamp() FROM p11.deliveries d JOIN p11.jobs j ON j.id=d.job_id JOIN p11.native_result_content c ON c.delivery_id=d.id WHERE j.id=$1 AND d.attempt_id=j.current_attempt_id AND d.state IN('WAITING_BUYER','VALID') ORDER BY d.id LIMIT 2`, state.Job)
	if err != nil {
		return zero, false, err
	}
	var delivery, status string
	var live bool
	n := 0
	for rows.Next() {
		n++
		if err = rows.Scan(&delivery, &status, &live); err != nil {
			break
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return zero, false, err
	}
	if n == 0 {
		return zero, false, nil
	}
	if n != 1 || !live || status == "RETIRED" {
		return zero, false, fault.New(fault.Conflict, "Original SDK content requires recovery")
	}
	if status != "READY" {
		return zero, false, nil
	}
	c, err := s.content.contentTx(ctx, tx, delivery, false)
	if err != nil {
		return zero, false, err
	}
	m := c.head.Manifest
	if m.Job != state.Job || m.Order != state.Order || m.Route != state.Route || m.Seller != state.Seller || m.Spec != state.Spec || m.Spec.Stream {
		return zero, false, fault.New(fault.Unavailable, "Original SDK result binding unavailable")
	}
	key, err := s.content.dataKey(c)
	if err != nil {
		return zero, false, err
	}
	defer clear(key)
	plain, err := contentOpen(key, c.terminal, "P11_RESULT_END_1\n"+c.digest)
	if err != nil {
		return zero, false, err
	}
	defer clear(plain)
	var end resultwire.End
	if strictjson.Decode(plain, &end, 1024) != nil || end.Validate(c.head.Declaration.Finish) != nil {
		return zero, false, fault.New(fault.Unavailable, "Original SDK terminal unavailable")
	}
	c.head.State, c.head.End = "READY", &end
	return c, true, nil
}

func (s *SDK) State(ctx context.Context, p identity.Principal, job string) (SDKState, error) {
	var out SDKState
	if s == nil || s.content == nil {
		return out, fault.New(fault.Unavailable, "SDK unavailable")
	}
	err := s.content.completion.db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		var e error
		out, e = s.sdkJobTx(ctx, tx, p, job)
		if e != nil {
			return e
		}
		c, ready, e := s.readyContentTx(ctx, tx, out)
		if e == nil && ready {
			h := c.head
			out.Head = &h
		}
		return e
	})
	if err != nil {
		return SDKState{}, err
	}
	return out, nil
}

// Chunk decrypts one bounded chunk after current original API-key authority
// and head identity checks. Caller clears Content. No read delegation is minted.
func (s *SDK) Chunk(ctx context.Context, p identity.Principal, job, delivery, head string, index int) (resultwire.ContentChunk, error) {
	var out resultwire.ContentChunk
	if s == nil || s.content == nil || !ids.Valid(delivery) || index < 0 || index >= 1024 {
		return out, fault.New(fault.Invalid, "Bounded SDK content reference required")
	}
	err := s.content.completion.db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		state, e := s.sdkJobTx(ctx, tx, p, job)
		if e != nil {
			return e
		}
		c, ready, e := s.readyContentTx(ctx, tx, state)
		if e != nil {
			return e
		}
		if !ready || c.head.Delivery != delivery || c.digest != head || index >= c.head.Chunks {
			return fault.New(fault.NotFound, "Original SDK content changed or unavailable")
		}
		var digest string
		var sealed []byte
		if e = tx.QueryRow(ctx, `SELECT content_digest,ciphertext FROM p11.native_result_content_chunks WHERE delivery_id=$1 AND chunk_index=$2`, delivery, index).Scan(&digest, &sealed); e != nil {
			return e
		}
		key, e := s.content.dataKey(c)
		if e != nil {
			return e
		}
		defer clear(key)
		plain, e := contentOpen(key, sealed, chunkAAD(c.digest, index, digest))
		if e != nil {
			return e
		}
		out = resultwire.ContentChunk{Profile: resultwire.ContentProfile, Scope: c.head.Declaration.Scope, Delivery: delivery, Head: head, Index: index, Digest: digest, Content: plain}
		if out.Validate() != nil {
			clear(plain)
			return fault.New(fault.Unavailable, "Original SDK chunk integrity unavailable")
		}
		return nil
	})
	if err != nil {
		clear(out.Content)
		return resultwire.ContentChunk{}, err
	}
	return out, nil
}

// Recheck is called after complete local rendering and before the first byte
// is sent, so logout/revocation/cancellation during result assembly fails closed.
func (s *SDK) Recheck(ctx context.Context, p identity.Principal, job, delivery, head string) error {
	v, err := s.State(ctx, p, job)
	if err != nil {
		return err
	}
	if v.Head == nil || v.Head.Delivery != delivery || v.Head.Digest() != head {
		return fault.New(fault.NotFound, "Original SDK result no longer readable")
	}
	expires, err := time.Parse(time.RFC3339Nano, v.Head.Expires)
	if err != nil || !expires.After(time.Now()) {
		return fault.New(fault.NotFound, "Original SDK result expired")
	}
	return nil
}
