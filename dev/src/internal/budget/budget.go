// Package budget implements SQL-only order capacity accounting. It is not a
// wallet, payment verifier, route authorizer, execution grant or GPU lock. A
// trusted application must authenticate the caller and independently check
// funding/route/lease qualification in the SAME transaction before execution.
package budget

import (
	"context"
	"github.com/jackc/pgx/v5"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/money"
	"regexp"
	"time"
)

const Profile = "ORDER_CAPACITY_1"

// Scope is obtained from authenticated, immutable order/route facts. Never
// decode it from a model request or copy it out of an unverified lease.
type Scope struct{ OrderID, BuyerID, SellerID, ContractHash string }

type Request struct {
	PurposeRef, PurposeKind string
	MaximumFen              money.Fen
}

// Capacity describes an order ceiling, not available or withdrawable funds.
// Legacy records are fully held in Unverified until independent recovery.
type Capacity struct {
	Maximum, Reserved, Consumed, Unverified, Remaining money.Fen
}

type Reservation struct {
	ID, PurposeRef, PurposeKind, State, Profile string
	MaximumFen                                  money.Fen
	ConsumedFen                                 *money.Fen
	QualificationID                             *string
	Version                                     int64
	ResolvedAt                                  *time.Time
}

// New reports a newly applied reservation or completion transition in THIS
// transaction. Replays are false. Neither outcome grants permission to submit.
type Result struct {
	Reservation Reservation
	Capacity    Capacity
	New         bool
}

var hash = regexp.MustCompile(`^[0-9a-f]{64}$`)

func validateScope(s Scope) error {
	if !ids.Valid(s.OrderID) || !ids.Valid(s.BuyerID) || !ids.Valid(s.SellerID) || !hash.MatchString(s.ContractHash) {
		return fault.New(fault.Invalid, "Invalid budget scope")
	}
	return nil
}

// lockOrder fixes the shared lock order: order -> purpose reservation ->
// qualification/source. The parent is also UPDATED by the SQL guard, so a
// repeatable-read stale snapshot cannot overbook after waiting on this lock.
func lockOrder(ctx context.Context, tx pgx.Tx, s Scope, newCall bool) (Capacity, error) {
	var c Capacity
	if err := validateScope(s); err != nil {
		return c, err
	}
	var digest, state string
	var expired bool
	err := tx.QueryRow(ctx, `SELECT contract_hash,state,deadline<=clock_timestamp(),max_amount_fen,
budget_reserved_fen,budget_consumed_fen,budget_unverified_fen
FROM p11.orders WHERE id=$1 AND buyer_id=$2 AND seller_id=$3 FOR UPDATE`, s.OrderID, s.BuyerID, s.SellerID).
		Scan(&digest, &state, &expired, &c.Maximum, &c.Reserved, &c.Consumed, &c.Unverified)
	if err == pgx.ErrNoRows {
		return c, fault.New(fault.NotFound, "Budget order not found")
	}
	if err != nil {
		return c, err
	}
	if digest != s.ContractHash {
		return c, fault.New(fault.Conflict, "Budget contract differs")
	}
	if newCall && (expired || state != "READY" && state != "FULFILLING") {
		return c, fault.New(fault.Conflict, "Order is not eligible for new capacity")
	}
	c.Remaining = c.Maximum - c.Reserved - c.Consumed - c.Unverified
	if c.Remaining < 0 {
		return c, fault.New(fault.Unavailable, "Budget requires reconciliation")
	}
	return c, nil
}

func read(ctx context.Context, tx pgx.Tx, order, purpose string) (Reservation, error) {
	var r Reservation
	err := tx.QueryRow(ctx, `SELECT id,purpose_ref,coalesce(purpose_kind,''),state,accounting_profile,
amount_fen,consumed_fen,qualification_id,version,resolved_at
FROM p11.budget_reservations WHERE order_id=$1 AND purpose_ref=$2 FOR UPDATE`, order, purpose).
		Scan(&r.ID, &r.PurposeRef, &r.PurposeKind, &r.State, &r.Profile, &r.MaximumFen,
			&r.ConsumedFen, &r.QualificationID, &r.Version, &r.ResolvedAt)
	return r, err
}

// SnapshotTx is historical accounting, including expired/closed orders. It
// grants no execution authority and performs no timeout-based release.
func SnapshotTx(ctx context.Context, tx pgx.Tx, s Scope) (Capacity, error) {
	return lockOrder(ctx, tx, s, false)
}

// LookupTx resolves a possibly committed original request without reserving
// again, even after the original deadline. Missing is not permission to retry
// an inference or a channel request under another purpose.
func LookupTx(ctx context.Context, tx pgx.Tx, s Scope, purpose string) (Result, error) {
	var out Result
	if !ids.Valid(purpose) {
		return out, fault.New(fault.Invalid, "Invalid budget purpose")
	}
	c, err := lockOrder(ctx, tx, s, false)
	if err != nil {
		return out, err
	}
	r, err := read(ctx, tx, s.OrderID, purpose)
	if err == pgx.ErrNoRows {
		return out, fault.New(fault.NotFound, "Budget reservation not found")
	}
	return Result{Reservation: r, Capacity: c}, err
}

// ReserveTx must compose with durable invocation/dispatch intent in the
// caller's transaction. An original purpose is never reused for another call.
// This primitive neither issues an execution grant nor commits independently.
func ReserveTx(ctx context.Context, tx pgx.Tx, s Scope, in Request) (Result, error) {
	return reserveTx(ctx, tx, s, in, "")
}

// ReserveIdentifiedTx lets a composing SQL transaction freeze its opaque
// reservation ID before inserting a mutually referenced original purpose. It
// changes no scope, capacity or replay rules and grants no execution authority.
func ReserveIdentifiedTx(ctx context.Context, tx pgx.Tx, s Scope, in Request, reservationID string) (Result, error) {
	if !ids.Valid(reservationID) {
		return Result{}, fault.New(fault.Invalid, "Explicit opaque reservation ID required")
	}
	return reserveTx(ctx, tx, s, in, reservationID)
}

func reserveTx(ctx context.Context, tx pgx.Tx, s Scope, in Request, reservationID string) (Result, error) {
	var out Result
	if !ids.Valid(in.PurposeRef) || in.PurposeKind != "JOB" && in.PurposeKind != "RENTAL_SESSION" ||
		in.MaximumFen < 0 || int64(in.MaximumFen) > money.MaxFen {
		return out, fault.New(fault.Invalid, "Invalid bounded budget request")
	}
	c, err := lockOrder(ctx, tx, s, true)
	if err != nil {
		return out, err
	}
	r, err := read(ctx, tx, s.OrderID, in.PurposeRef)
	if err == nil {
		if r.Profile != Profile {
			return out, fault.New(fault.Unavailable, "Legacy budget requires reconciliation")
		}
		if r.MaximumFen != in.MaximumFen || r.PurposeKind != in.PurposeKind {
			return out, fault.New(fault.Conflict, "Original budget intent differs")
		}
		if reservationID != "" && r.ID != reservationID {
			return out, fault.New(fault.Conflict, "Original reservation ID differs")
		}
		return Result{Reservation: r, Capacity: c}, nil
	}
	if err != pgx.ErrNoRows {
		return out, err
	}
	if in.MaximumFen > c.Remaining {
		return out, fault.New(fault.Conflict, "Order capacity exhausted")
	}
	if reservationID == "" {
		reservationID = ids.New("bgt")
	}
	r = Reservation{ID: reservationID, PurposeRef: in.PurposeRef, PurposeKind: in.PurposeKind,
		State: "RESERVED", Profile: Profile, MaximumFen: in.MaximumFen, Version: 1}
	_, err = tx.Exec(ctx, `INSERT INTO p11.budget_reservations
(id,order_id,purpose_ref,amount_fen,state,accounting_profile,purpose_kind)
VALUES($1,$2,$3,$4,'RESERVED',$5,$6)`, r.ID, s.OrderID, in.PurposeRef, in.MaximumFen, Profile, in.PurposeKind)
	if err != nil {
		return out, err
	}
	c, err = lockOrder(ctx, tx, s, false)
	return Result{Reservation: r, Capacity: c, New: true}, err
}

// CompleteTx consumes only an existing, immutable trade qualification linked
// to this original purpose. It accepts NO caller amount, usage candidate,
// "stopped" boolean or unknown-as-zero fallback. A qualified zero releases;
// otherwise the actual amount consumes capacity and unused ceiling returns.
// Missing/unknown evidence keeps the full hold, including after disconnection,
// cancellation, expiry or order closure. No payment/refund is sent here.
func CompleteTx(ctx context.Context, tx pgx.Tx, s Scope, purpose, qualification string) (Result, error) {
	var out Result
	if !ids.Valid(purpose) || !ids.Valid(qualification) {
		return out, fault.New(fault.Invalid, "Invalid budget resolution reference")
	}
	c, err := lockOrder(ctx, tx, s, false)
	if err != nil {
		return out, err
	}
	r, err := read(ctx, tx, s.OrderID, purpose)
	if err == pgx.ErrNoRows {
		return out, fault.New(fault.NotFound, "Budget reservation not found")
	}
	if err != nil {
		return out, err
	}
	if r.Profile != Profile {
		return out, fault.New(fault.Unavailable, "Legacy budget requires reconciliation")
	}
	if r.State != "RESERVED" {
		if r.QualificationID == nil || *r.QualificationID != qualification {
			return out, fault.New(fault.Conflict, "Budget already resolved by another original source")
		}
		return Result{Reservation: r, Capacity: c}, nil
	}
	var actual money.Fen
	err = tx.QueryRow(ctx, `SELECT gross_fen FROM p11.trade_qualifications WHERE id=$1 AND order_id=$2 FOR SHARE`, qualification, s.OrderID).Scan(&actual)
	if err == pgx.ErrNoRows {
		return out, fault.New(fault.Unavailable, "Qualified budget outcome is not known")
	}
	if err != nil {
		return out, err
	}
	if actual > r.MaximumFen {
		return out, fault.New(fault.Conflict, "Qualified usage exceeds reserved ceiling")
	}
	state := "CONSUMED"
	if actual == 0 {
		state = "RELEASED"
	}
	_, err = tx.Exec(ctx, `UPDATE p11.budget_reservations SET state=$1,consumed_fen=$2,
qualification_id=$3,resolved_at=clock_timestamp(),version=version+1 WHERE id=$4 AND state='RESERVED'`, state, actual, qualification, r.ID)
	if err != nil {
		return out, err
	}
	r, err = read(ctx, tx, s.OrderID, purpose)
	if err != nil {
		return out, err
	}
	c, err = lockOrder(ctx, tx, s, false)
	return Result{Reservation: r, Capacity: c, New: true}, err
}
