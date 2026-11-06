package payments

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/money"
)

// RefundRegistration must refer to a retained exact original Machine channel.
// The enclosing host owns this service, then Machine, channels and keyrings in
// that close order. Construction performs no migration, approval or IO.
type RefundRegistration struct {
	Ref                string
	OriginalCapability string
	Channel            *AlipayRefund
}
type RefundService struct {
	machine       *Machine
	registrations map[string]RefundRegistration
	mu            sync.Mutex
	operations    sync.WaitGroup
	life          context.Context
	cancel        context.CancelFunc
	closed        bool
	done          chan struct{}
}

func (s *RefundService) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[original refund service redacted]")
}
func (s *RefundService) MarshalJSON() ([]byte, error) { return []byte(`{"redacted":true}`), nil }
func NewRefundService(machine *Machine, registrations []RefundRegistration) (*RefundService, error) {
	if machine == nil || machine.db == nil || machine.db.Pool == nil || len(registrations) < 1 || len(registrations) > 128 {
		return nil, ErrBinding
	}
	machine.mu.Lock()
	closed := machine.closed
	machine.mu.Unlock()
	if closed {
		return nil, ErrBinding
	}
	byID := map[string]RefundRegistration{}
	seen := map[string]bool{}
	for _, r := range registrations {
		original, ok := machine.registrations[r.OriginalCapability]
		if !ok || !ids.Valid(r.Ref) || r.Channel == nil || r.Channel.channel == nil || !r.Channel.channel.refundOnly ||
			r.Channel.channel.binding.Digest() != original.Channel.binding.Digest() || r.Channel.route.EvidenceRef != original.Policy.RefundEvidenceRef ||
			r.Channel.route.EvidenceHash != original.Policy.RefundEvidenceHash || byID[r.Ref].Channel != nil || seen[r.OriginalCapability+":"+r.Channel.route.Digest()] {
			return nil, ErrBinding
		}
		byID[r.Ref] = r
		seen[r.OriginalCapability+":"+r.Channel.route.Digest()] = true
	}
	life, cancel := context.WithCancel(context.Background())
	return &RefundService{machine: machine, registrations: byID, life: life, cancel: cancel, done: make(chan struct{})}, nil
}
func (s *RefundService) UsesMachine(machine *Machine) bool { return s != nil && s.machine == machine }
func (s *RefundService) enter(ctx context.Context) (context.Context, func(), error) {
	if s == nil {
		return nil, nil, ErrUnknown
	}
	child, finish, e := s.machine.enter(ctx)
	if e != nil {
		return nil, nil, e
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		finish()
		return nil, nil, ErrUnknown
	}
	s.operations.Add(1)
	s.mu.Unlock()
	bounded, cancel := context.WithCancel(child)
	stop := context.AfterFunc(s.life, cancel)
	return bounded, func() { stop(); cancel(); s.operations.Done(); finish() }, nil
}
func (s *RefundService) Close(ctx context.Context) error {
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
func (s *RefundService) registration(r refundRecord) (RefundRegistration, Registration, error) {
	value, ok := s.registrations[r.RouteRef]
	if !ok || value.OriginalCapability != r.Payment.Capability || value.Channel.route.Digest() != r.Intent.Scope.RouteHash {
		return RefundRegistration{}, Registration{}, ErrBinding
	}
	original, e := s.machine.registration(r.Payment)
	if e != nil {
		return RefundRegistration{}, Registration{}, e
	}
	if !r.Intent.bound(value.Channel.route, original.Channel.binding) {
		return RefundRegistration{}, Registration{}, ErrBinding
	}
	return value, original, nil
}

// Request values name a reviewer decision; they neither choose an amount nor
// establish qualification. The decision is unique across refund contexts.
type OriginalRefundInput struct {
	ExpectedOrderVersion   int64  `json:"expected_order_version"`
	ExpectedPaymentVersion int64  `json:"expected_payment_version"`
	RouteRef               string `json:"refund_route_ref"`
	DecisionRef            string `json:"decision_ref"`
	DecisionHash           string `json:"decision_hash"`
}
type OriginalRefundView struct {
	Replayed       bool      `json:"-"`
	ID             string    `json:"refund_id"`
	Order          string    `json:"order_id"`
	Payment        string    `json:"payment_id"`
	Version        int64     `json:"version"`
	State          string    `json:"state"`
	Amount         money.Fen `json:"amount_fen"`
	Updated        time.Time `json:"updated_at"`
	ReviewRequired bool      `json:"review_required"`
}

// Create commits original reservation/context/REFUND+QUERY/job/outbox and safe
// idempotent metadata together. It never invokes the financial channel.
func (s *RefundService) Create(ctx context.Context, p identity.Principal, payment string, in OriginalRefundInput, key string) (OriginalRefundView, error) {
	ctx, end, e := s.enter(ctx)
	if e != nil {
		return OriginalRefundView{}, e
	}
	defer end()
	if p.RequireUser("refund:create") != nil || !ids.Valid(payment) || !ids.Valid(in.RouteRef) || !ids.Valid(in.DecisionRef) || !hashPattern.MatchString(in.DecisionHash) || in.ExpectedOrderVersion < 1 || in.ExpectedPaymentVersion < 1 {
		return OriginalRefundView{}, ErrIntent
	}
	registration, ok := s.registrations[in.RouteRef]
	if !ok {
		return OriginalRefundView{}, ErrBinding
	}
	request, _ := json.Marshal(in)
	result, e := (outbox.Idempotency{DB: s.machine.db}).DoAuthorized(ctx, outbox.Command{Principal: p.UserID, PrincipalVersion: p.Version, Operation: "createRefund", Resource: payment, Key: key, Request: request},
		func(tx pgx.Tx) error {
			o, _, e := loadMachineTx(ctx, tx, payment)
			if e != nil {
				return e
			}
			return requireMachineBuyer(ctx, tx, p, o, "refund:create")
		},
		func(tx pgx.Tx) (outbox.StoredResult, error) {
			o, paid, e := loadMachineTx(ctx, tx, payment)
			if e != nil {
				return outbox.StoredResult{}, e
			}
			var existing string
			e = tx.QueryRow(ctx, `SELECT refund_id FROM p11.refund_machine_contexts WHERE decision_evidence_id=$1`, in.DecisionRef).Scan(&existing)
			if e == nil {
				_, old, e := loadRefundTx(ctx, tx, existing)
				if e != nil {
					return outbox.StoredResult{}, e
				}
				if old.Intent.Scope.Original.Payment != payment || old.RouteRef != in.RouteRef || old.Intent.Scope.DecisionHash != in.DecisionHash {
					return outbox.StoredResult{}, ErrConflict
				}
				body, _ := json.Marshal(old.public())
				return outbox.StoredResult{Reference: existing, Body: body, HTTPStatus: 202}, nil
			}
			if e != pgx.ErrNoRows {
				return outbox.StoredResult{}, e
			}
			original, e := s.machine.registration(paid)
			if e != nil {
				return outbox.StoredResult{}, e
			}
			if registration.OriginalCapability != paid.Capability || o.Version != in.ExpectedOrderVersion || paid.Version != in.ExpectedPaymentVersion || paid.Intent.Scope.Purpose != "ORDER" || paid.Snapshot.State != "SETTLED" {
				return outbox.StoredResult{}, ErrConflict
			}
			if e = requireRefundRouteTx(ctx, tx, o, original, registration, true); e != nil {
				return outbox.StoredResult{}, e
			}
			authority, amount, decision, e := refundDecisionTx(ctx, tx, o, paid, original, registration, in.DecisionRef, in.DecisionHash)
			if e != nil {
				return outbox.StoredResult{}, e
			}
			if e = refundCapacityTx(ctx, tx, o, paid, authority.Maximum); e != nil {
				return outbox.StoredResult{}, e
			}
			rows, e := originalRefundsTx(ctx, tx, paid.Snapshot)
			if e != nil {
				return outbox.StoredResult{}, e
			}
			id := ids.New("refund")
			number, e := NewOutTradeNo()
			if e != nil {
				return outbox.StoredResult{}, e
			}
			reserved, _, e := ReserveRefund(paid.Snapshot, rows, authority, id, number, amount)
			if e != nil {
				return outbox.StoredResult{}, e
			}
			snapshot, receipt, e := s.reverifyOriginalPaidTx(ctx, tx, paid, original, "")
			if e != nil {
				return outbox.StoredResult{}, e
			}
			before := o.Now.Add(10 * time.Minute)
			if registration.Channel.route.ValidUntil.Before(before) {
				before = registration.Channel.route.ValidUntil
			}
			intent, e := NewRefundIntent(snapshot, reserved, authority, registration.Channel.route, paid.Binding, o.Now, before)
			if e != nil {
				return outbox.StoredResult{}, e
			}
			view, e := insertRefundTx(ctx, tx, o, paid, registration, intent, decision, receipt)
			if e != nil {
				return outbox.StoredResult{}, e
			}
			body, _ := json.Marshal(view)
			return outbox.StoredResult{Reference: id, Body: body, HTTPStatus: 202}, nil
		})
	if e != nil {
		return OriginalRefundView{}, e
	}
	var view OriginalRefundView
	if json.Unmarshal(result.Body, &view) != nil {
		return view, ErrConflict
	}
	view.Replayed = result.Replayed
	return view, nil
}

func (s *RefundService) Get(ctx context.Context, p identity.Principal, refund string) (OriginalRefundView, error) {
	ctx, end, e := s.enter(ctx)
	if e != nil {
		return OriginalRefundView{}, e
	}
	defer end()
	if !ids.Valid(refund) {
		return OriginalRefundView{}, ErrIntent
	}
	var view OriginalRefundView
	e = s.machine.db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		o, r, e := loadRefundTx(ctx, tx, refund)
		if e != nil {
			return e
		}
		if e = requireMachineBuyer(ctx, tx, p, o, "refund:read"); e != nil {
			return e
		}
		view = r.public()
		return nil
	})
	return view, e
}
