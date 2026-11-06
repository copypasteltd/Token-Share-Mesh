package payments

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/internal/persistence"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
)

type Machine struct {
	db            *persistence.DB
	vault         *Vault
	receipts      *Vault
	registrations map[string]Registration
	mu            sync.Mutex
	operations    sync.WaitGroup
	life          context.Context
	cancel        context.CancelFunc
	closed        bool
	done          chan struct{}
}

func (s *Machine) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[Machine Pay service redacted]")
}
func (s *Machine) MarshalJSON() ([]byte, error) { return []byte(`{"redacted":true}`), nil }

// UsesDB binds HTTP authentication/ownership checks to this exact store.
func (s *Machine) UsesDB(db *persistence.DB) bool { return s != nil && s.db == db }

// NewMachine installs independently configured channels; it neither opens a
// public payment route nor starts a worker, creates funds or self-approves sales.
// The enclosing application owns channels and two separate keyrings, and
// closes them AFTER Machine. Short-lived proofs must not share receipt keys.
func NewMachine(db *persistence.DB, vault, receipts *Vault, registrations []Registration) (*Machine, error) {
	if db == nil || db.Pool == nil || !separateMachineVaults(vault, receipts) || len(registrations) < 1 || len(registrations) > 128 {
		return nil, ErrBinding
	}
	byID := map[string]Registration{}
	for _, r := range registrations {
		if !ids.Valid(r.CapabilityRef) || r.Channel == nil || !r.Policy.valid() || !ids.Valid(r.Channel.binding.AuthorizationRef) || !ids.Valid(r.Channel.binding.QualificationRef) {
			return nil, ErrBinding
		}
		if _, exists := byID[r.CapabilityRef]; exists {
			return nil, ErrBinding
		}
		byID[r.CapabilityRef] = r
	}
	life, cancel := context.WithCancel(context.Background())
	return &Machine{db: db, vault: vault, receipts: receipts, registrations: byID, life: life, cancel: cancel, done: make(chan struct{})}, nil
}

func separateMachineVaults(proof, receipt *Vault) bool {
	if proof == nil || receipt == nil || proof == receipt {
		return false
	}
	// Lock one keyring at a time; never hold two locks across concurrent Close.
	hashes := func(v *Vault) (map[[32]byte]bool, bool) {
		v.mu.RLock()
		defer v.mu.RUnlock()
		if v.closed || len(v.keys) == 0 {
			return nil, false
		}
		out := map[[32]byte]bool{}
		for _, key := range v.keys {
			out[sha256.Sum256(key)] = true
		}
		return out, true
	}
	a, ok := hashes(proof)
	if !ok {
		return false
	}
	b, ok := hashes(receipt)
	if !ok {
		return false
	}
	for digest := range a {
		if b[digest] {
			return false
		}
	}
	return true
}
func (s *Machine) enter(ctx context.Context) (context.Context, func(), error) {
	if s == nil || ctx == nil || ctx.Err() != nil {
		return nil, nil, ErrUnknown
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, nil, ErrUnknown
	}
	s.operations.Add(1)
	s.mu.Unlock()
	child, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.life, cancel)
	return child, func() { stop(); cancel(); s.operations.Done() }, nil
}
func (s *Machine) Close(ctx context.Context) error {
	if s == nil || ctx == nil {
		return ErrUnknown
	}
	s.mu.Lock()
	first := !s.closed
	if first {
		s.closed = true
		s.cancel()
	}
	s.mu.Unlock()
	if first {
		go func() { s.operations.Wait(); close(s.done) }()
	}
	select {
	case <-ctx.Done():
		return ErrUnknown
	case <-s.done:
		return nil
	}
}
func (s *Machine) registration(r machineRecord) (Registration, error) {
	value, ok := s.registrations[r.Capability]
	if !ok || value.Channel.binding.Digest() != r.Intent.Scope.BindingHash {
		return Registration{}, ErrBinding
	}
	raw, _ := json.Marshal(value.Policy)
	saved, _ := json.Marshal(r.Policy)
	if subtle.ConstantTimeCompare(raw, saved) != 1 {
		return Registration{}, ErrBinding
	}
	return value, nil
}

type IntentInput struct {
	ExpectedOrderVersion int64  `json:"expected_order_version"`
	ChannelCapabilityRef string `json:"channel_capability_ref"`
	ReturnTargetID       string `json:"return_target_id,omitempty"`
}

// CreateIntent atomically freezes original metadata, payment, PAY intent and
// outbox before ANY external call. Idempotent replay never issues a channel call.
func (s *Machine) CreateIntent(ctx context.Context, p identity.Principal, order string, in IntentInput, key string) (Payment, error) {
	ctx, end, e := s.enter(ctx)
	if e != nil {
		return Payment{}, e
	}
	defer end()
	if p.RequireUser("payment:create") != nil || !ids.Valid(order) || in.ExpectedOrderVersion < 1 {
		return Payment{}, ErrIntent
	}
	r, ok := s.registrations[in.ChannelCapabilityRef]
	if !ok || in.ReturnTargetID != "" && in.ReturnTargetID != r.Policy.ReturnTargetID {
		return Payment{}, ErrBinding
	}
	request, _ := json.Marshal(in)
	result, e := (outbox.Idempotency{DB: s.db}).DoAuthorized(ctx, outbox.Command{Principal: p.UserID, PrincipalVersion: p.Version, Operation: "createPaymentIntent", Resource: order, Key: key, Request: request},
		func(tx pgx.Tx) error {
			if e := identity.RequireRoleTx(ctx, tx, p, "payment:create", "BUYER", true); e != nil {
				return e
			}
			o, e := machineOrderTx(ctx, tx, order)
			if e != nil {
				return e
			}
			if o.Buyer != p.UserID {
				return fault.New(fault.NotFound, "Original payment order not found")
			}
			return nil
		}, func(tx pgx.Tx) (outbox.StoredResult, error) {
			o, e := machineOrderTx(ctx, tx, order)
			if e != nil {
				return outbox.StoredResult{}, e
			}
			if o.Version != in.ExpectedOrderVersion {
				return outbox.StoredResult{}, ErrConflict
			}
			if e = admitMachineSaleTx(ctx, tx, o, r); e != nil {
				return outbox.StoredResult{}, e
			}
			var previous bool
			if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM p11.payments WHERE order_id=$1 AND purpose='ORDER' AND purpose_ref=$1)`, order).Scan(&previous); e != nil {
				return outbox.StoredResult{}, e
			}
			if previous {
				return outbox.StoredResult{}, ErrConflict
			}
			number, e := NewOutTradeNo()
			if e != nil {
				return outbox.StoredResult{}, e
			}
			b := r.Channel.binding
			i := Intent{Scope: Scope{Payment: ids.New("pay"), Order: o.ID, Buyer: o.Buyer, Seller: o.Seller, Purpose: "ORDER", PurposeRef: o.ID, ContractHash: o.Hash, BindingHash: b.Digest(), OutTradeNo: number},
				Amount: o.Maximum, GoodsName: r.Policy.GoodsName, CreatedAt: o.Now, PayBefore: o.Now.Add(time.Duration(r.Policy.MaximumWindowSeconds) * time.Second), ContractDeadline: o.Deadline}
			if i.PayBefore.After(o.Deadline) {
				i.PayBefore = o.Deadline
			}
			if i.PayBefore.After(b.ValidUntil) {
				i.PayBefore = b.ValidUntil
			}
			if !i.ValidAt(b, o.Now) {
				return outbox.StoredResult{}, ErrIntent
			}
			raw, _ := json.Marshal(i)
			policy, _ := json.Marshal(r.Policy)
			binding, _ := json.Marshal(b)
			channel := ids.New("crq")
			_, e = tx.Exec(ctx, `INSERT INTO p11.payments(id,order_id,purpose,purpose_ref,provider,environment,original_request_key,product_profile_revision,channel_binding_snapshot,amount_fen,state,funds_expires_at,created_at)
VALUES($1,$2,'ORDER',$2,'ALIPAY',$3,$4,$5,$6,$7,'CREATED',$8,$9)`, i.Scope.Payment, o.ID, b.Environment, number, Profile, binding, i.Amount, i.ContractDeadline, o.Now)
			if e != nil {
				return outbox.StoredResult{}, e
			}
			_, e = tx.Exec(ctx, `INSERT INTO p11.channel_requests(id,operation,target_ref,provider,environment,original_request_key,request_digest,state)
VALUES($1,'PAY',$2,'ALIPAY',$3,$4,$5,'INTENT')`, channel, i.Scope.Payment, b.Environment, number, outbox.Digest(raw))
			if e != nil {
				return outbox.StoredResult{}, e
			}
			_, e = tx.Exec(ctx, `INSERT INTO p11.payment_machine_contexts(payment_id,order_id,buyer_id,seller_id,capability_ref,scope_hash,intent_raw,intent_digest,policy_raw,policy_digest,precreate_request_id,updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, i.Scope.Payment, o.ID, o.Buyer, o.Seller, r.CapabilityRef, i.Scope.Digest(), raw, outbox.Digest(raw), policy, outbox.Digest(policy), channel, o.Now)
			if e != nil {
				return outbox.StoredResult{}, e
			}
			if _, e = machineEventTx(ctx, tx, i.Scope.Payment, 1, "PaymentMachineIntentCreated", ""); e != nil {
				return outbox.StoredResult{}, e
			}
			body, _ := json.Marshal(map[string]string{"payment_id": i.Scope.Payment})
			return outbox.StoredResult{Reference: i.Scope.Payment, Body: body, HTTPStatus: 201}, nil
		})
	if e != nil {
		return Payment{}, e
	}
	value, e := s.read(ctx, p, result.Reference, "payment:create", false)
	value.Replayed = result.Replayed
	return value, e
}
func (s *Machine) read(ctx context.Context, p identity.Principal, id, scope string, sellerRead bool) (Payment, error) {
	var result Payment
	e := s.db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		if e := p.RequireUser(scope); e != nil {
			return e
		}
		if e := identity.RequireCurrentTx(ctx, tx, p); e != nil {
			return e
		}
		o, r, e := loadMachineTx(ctx, tx, id)
		if e != nil {
			return e
		}
		role := "BUYER"
		if p.UserID != o.Buyer {
			if !sellerRead || p.UserID != o.Seller {
				return fault.New(fault.NotFound, "Original payment not found")
			}
			role = "SELLER"
		}
		if e = identity.RequireRoleTx(ctx, tx, p, scope, role, true); e != nil {
			return e
		}
		result = r.public()
		return nil
	})
	return result, e
}
func (s *Machine) Get(ctx context.Context, p identity.Principal, id string) (Payment, error) {
	ctx, end, e := s.enter(ctx)
	if e != nil {
		return Payment{}, e
	}
	defer end()
	if !ids.Valid(id) {
		return Payment{}, ErrIntent
	}
	return s.read(ctx, p, id, "payment:read", true)
}

// Precreate claims an INTENT exactly once, commits SENT/UNKNOWN, then calls the
// original channel. SENT/UNKNOWN is never replayed as a fresh financial create.
func (s *Machine) Precreate(ctx context.Context, p identity.Principal, payment string) (Payment, error) {
	ctx, end, e := s.enter(ctx)
	if e != nil {
		return Payment{}, e
	}
	defer end()
	var record machineRecord
	var registration Registration
	e = s.db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		if e := identity.RequireRoleTx(ctx, tx, p, "payment:create", "BUYER", true); e != nil {
			return e
		}
		o, r, e := loadMachineTx(ctx, tx, payment)
		if e != nil {
			return e
		}
		if e = requireMachineBuyer(ctx, tx, p, o, "payment:create"); e != nil {
			return e
		}
		registration, e = s.registration(r)
		if e != nil {
			return e
		}
		if e = admitMachineSaleTx(ctx, tx, o, registration); e != nil {
			return e
		}
		if !r.Intent.ValidAt(r.Binding, o.Now) {
			return ErrIntent
		}
		if r.Snapshot.State != "CREATED" {
			return ErrUnknown
		}
		tag, e := tx.Exec(ctx, `UPDATE p11.channel_requests SET state='SENT',version=version+1 WHERE id=$1 AND target_ref=$2 AND state='INTENT' AND request_digest=$3`, r.PrecreateRequest, payment, outbox.Digest(mustIntent(r.Intent)))
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return ErrUnknown
		}
		_, e = tx.Exec(ctx, `UPDATE p11.payments SET state='UNKNOWN',version=version+1 WHERE id=$1`, payment)
		if e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `UPDATE p11.payment_machine_contexts SET updated_at=clock_timestamp() WHERE payment_id=$1`, payment)
		if e != nil {
			return e
		}
		_, e = machineEventTx(ctx, tx, payment, r.Version+1, "PaymentMachineDispatchCommitted", "")
		record = r
		return e
	})
	if e != nil {
		return Payment{}, e
	}
	observation, channelErr := registration.Channel.Precreate(ctx, record.Intent)
	defer observation.Close()
	persistCtx, persistEnd := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	e = s.recordObservation(persistCtx, record.Intent.Scope.Payment, record.PrecreateRequest, observation)
	persistEnd()
	if e != nil {
		return Payment{}, e
	}
	value, e := s.read(ctx, p, payment, "payment:create", false)
	if e != nil {
		return Payment{}, e
	}
	if channelErr != nil {
		return value, ErrUnknown
	}
	return value, nil
}
func mustIntent(i Intent) []byte { raw, _ := json.Marshal(i); return raw }

type Recovery struct {
	Replayed bool      `json:"-"`
	ID       string    `json:"recovery_id"`
	Payment  string    `json:"payment_id"`
	State    string    `json:"state"`
	Expires  time.Time `json:"expires_at"`
}

func sameMachineProof(vault *Vault, scope Scope, encrypted []byte, proof *Proof, now time.Time) bool {
	old, e := vault.Open(scope, encrypted, now)
	if e != nil {
		return false
	}
	defer old.Close()
	a, e := old.copyRaw()
	if e != nil {
		return false
	}
	defer clear(a)
	b, e := proof.copyRaw()
	if e != nil {
		return false
	}
	defer clear(b)
	return subtle.ConstantTimeCompare(a, b) == 1
}

// SubmitProof commits ciphertext, a QUERY intent, a dedicated durable job and
// safe replay metadata in ONE transaction. No credential enters safe_response.
func (s *Machine) SubmitProof(ctx context.Context, p identity.Principal, payment string, proof *Proof, key string) (Recovery, error) {
	ctx, end, e := s.enter(ctx)
	if e != nil {
		return Recovery{}, e
	}
	defer end()
	var original machineRecord
	var order machineOrder
	var existing Recovery
	request, _ := json.Marshal(map[string]string{"payment_id": payment})
	result, e := (outbox.Idempotency{DB: s.db}).DoAuthorized(ctx, outbox.Command{Principal: p.UserID, PrincipalVersion: p.Version, Operation: "submitMachinePaymentProof", Resource: payment, Key: key, Request: request}, func(tx pgx.Tx) error {
		if e := identity.RequireRoleTx(ctx, tx, p, "payment:create", "BUYER", true); e != nil {
			return e
		}
		o, r, e := loadMachineTx(ctx, tx, payment)
		if e != nil {
			return e
		}
		if e = requireMachineBuyer(ctx, tx, p, o, "payment:create"); e != nil {
			return e
		}
		if r.Snapshot.State != "PENDING" && r.Snapshot.State != "UNKNOWN" {
			return ErrConflict
		}
		reg, e := s.registration(r)
		if e != nil {
			return e
		}
		if e = requireMachineAuthorityTx(ctx, tx, reg, o.Buyer, o.Now, false); e != nil {
			return e
		}
		var cipher []byte
		e = tx.QueryRow(ctx, `SELECT id,state,expires_at,proof_ciphertext FROM p11.payment_machine_recoveries WHERE payment_id=$1 AND state IN ('PENDING','VERIFYING','UNKNOWN') FOR UPDATE`, payment).Scan(&existing.ID, &existing.State, &existing.Expires, &cipher)
		if e != nil && e != pgx.ErrNoRows {
			return e
		}
		defer clear(cipher)
		if e == nil && !sameMachineProof(s.vault, r.Intent.Scope, cipher, proof, o.Now) {
			return ErrConflict
		}
		original = r
		order = o
		return nil
	}, func(tx pgx.Tx) (outbox.StoredResult, error) {
		recovery := existing
		recovery.Payment = payment
		if recovery.ID == "" {
			expires := order.Now.Add(30 * time.Minute)
			if expires.After(original.Binding.ResolveUntil) {
				expires = original.Binding.ResolveUntil
			}
			cipher, e := s.vault.Seal(original.Intent.Scope, proof, expires, order.Now)
			if e != nil {
				return outbox.StoredResult{}, e
			}
			defer clear(cipher)
			recovery = Recovery{ID: ids.New("mpr"), Payment: payment, State: "PENDING", Expires: expires}
			channel, job := ids.New("crq"), ids.New("bg")
			_, e = tx.Exec(ctx, `INSERT INTO p11.channel_requests(id,operation,target_ref,provider,environment,original_request_key,request_digest,state)
VALUES($1,'QUERY',$2,'ALIPAY',$3,$4,$5,'INTENT')`, channel, payment, original.Binding.Environment, recovery.ID, outbox.Digest(cipher))
			if e != nil {
				return outbox.StoredResult{}, e
			}
			_, e = tx.Exec(ctx, `INSERT INTO p11.durable_jobs(id,kind,dedupe_key,target_ref,state,run_at) VALUES($1,'VERIFY_MACHINE_PAYMENT',$2,$3,'PENDING',$4)`, job, "machine-proof:"+recovery.ID, recovery.ID, order.Now)
			if e != nil {
				return outbox.StoredResult{}, e
			}
			_, e = tx.Exec(ctx, `INSERT INTO p11.payment_machine_recoveries(id,payment_id,scope_hash,buyer_id,proof_ciphertext,proof_cipher_digest,expires_at,channel_request_id,durable_job_id,state,created_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'PENDING',$10)`, recovery.ID, payment, original.Intent.Scope.Digest(), order.Buyer, cipher, outbox.Digest(cipher), expires, channel, job, order.Now)
			if e != nil {
				return outbox.StoredResult{}, e
			}
		}
		body, _ := json.Marshal(recovery)
		return outbox.StoredResult{Reference: recovery.ID, Body: body, HTTPStatus: 202}, nil
	})
	if e != nil {
		return Recovery{}, e
	}
	var recovery Recovery
	if json.Unmarshal(result.Body, &recovery) != nil {
		return Recovery{}, ErrUnknown
	}
	recovery.Replayed = result.Replayed || existing.ID != ""
	return recovery, nil
}
