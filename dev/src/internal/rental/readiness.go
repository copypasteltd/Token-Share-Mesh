package rental

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
)

type PreparationWrite struct {
	Expected int64 `json:"expected_version"`
}

// Preparation is authenticated control metadata, not a work grant. A native
// host may prepare only the already locally approved environment; INVOKE still
// requires the independently verified current buyer grant, live control and
// the original session's physical fence. It cannot download arbitrary assets.
type Preparation struct {
	Session        View      `json:"session"`
	ManifestRef    string    `json:"manifest_ref"`
	ManifestHash   string    `json:"manifest_hash"`
	ManifestBase64 string    `json:"manifest_base64url"`
	Connection     string    `json:"connection_ref"`
	Generation     string    `json:"resource_lock_generation"`
	PrepareUntil   time.Time `json:"prepare_until"`
	Replayed       bool      `json:"-"`
}

func (s *Service) ClaimPreparation(ctx context.Context, p identity.Principal, id string, in PreparationWrite, key string) (Preparation, error) {
	var v Preparation
	if err := s.configured(); err != nil {
		return v, err
	}
	if !ids.Valid(id) || in.Expected < 1 {
		return v, fault.New(fault.Invalid, "Original preparation session and version required")
	}
	c, err := command(p, "claimRentalPreparation", id, key, in)
	if err != nil {
		return v, err
	}
	result, err := (outbox.Idempotency{DB: s.db}).DoAuthorized(ctx, c, s.authorizeSession(ctx, p, id, "device:rental", true), func(tx pgx.Tx) (outbox.StoredResult, error) {
		o, err := lockSessionOrder(ctx, tx, id, p, false)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		r, err := loadSessionTx(ctx, tx, id, o)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		_, resource, err := s.currentWorkTx(ctx, tx, o, r)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		next, err := BeginPreparing(r.Session, in.Expected, resource, o.Now)
		if err != nil {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Original preparation window or physical owner unavailable")
		}
		if err = updateSessionTx(ctx, tx, r.Session, next); err != nil {
			return outbox.StoredResult{}, err
		}
		if err = updatePhysicalTx(ctx, tx, next); err != nil {
			return outbox.StoredResult{}, err
		}
		if err = recordHistoryTx(ctx, tx, next, p.UserID, "beginRentalPreparation", "RentalPreparationClaimed"); err != nil {
			return outbox.StoredResult{}, err
		}
		v = Preparation{Session: viewOf(next), ManifestRef: next.Contract.Manifest, ManifestHash: next.Contract.ManifestHash, ManifestBase64: base64.RawURLEncoding.EncodeToString(r.ManifestRaw), Connection: r.Connection, Generation: next.Generation, PrepareUntil: next.PrepareUntil}
		raw, err := json.Marshal(v)
		return outbox.StoredResult{Reference: id, Body: raw, HTTPStatus: 200}, err
	})
	if err == nil {
		err = json.Unmarshal(result.Body, &v)
		v.Replayed = result.Replayed
	}
	return v, err
}

func (s *Service) DeviceReady(ctx context.Context, p identity.Principal, id string, in DeviceReadyWrite, key string) (View, error) {
	if err := s.configured(); err != nil {
		return View{}, err
	}
	if !ids.Valid(id) || in.Expected < 1 || !ids.Valid(in.Environment) {
		return View{}, fault.New(fault.Invalid, "Original readiness session and environment required")
	}
	c, err := command(p, "recordRentalDeviceReady", id, key, in)
	if err != nil {
		return View{}, err
	}
	result, err := (outbox.Idempotency{DB: s.db}).DoAuthorized(ctx, c, s.authorizeSession(ctx, p, id, "device:rental", true), func(tx pgx.Tx) (outbox.StoredResult, error) {
		o, err := lockSessionOrder(ctx, tx, id, p, false)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		r, err := loadSessionTx(ctx, tx, id, o)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		if in.Environment != r.Session.Contract.Environment || in.Generation != r.Session.Generation {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Readiness environment or physical generation differs")
		}
		_, resource, err := s.currentWorkTx(ctx, tx, o, r)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		proof, err := readinessTx(ctx, tx, r, p.UserID, in.Connection, in.Readiness, "RENTAL_DEVICE_READY", o.Now)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		next, err := RecordDeviceReady(r.Session, in.Expected, resource, proof, o.Now)
		if err != nil {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Current original device preparation unavailable")
		}
		if err = updateSessionTx(ctx, tx, r.Session, next); err != nil {
			return outbox.StoredResult{}, err
		}
		if err = updatePhysicalTx(ctx, tx, next); err != nil {
			return outbox.StoredResult{}, err
		}
		if err = recordHistoryTx(ctx, tx, next, p.UserID, "recordRentalDeviceReady", "RentalDeviceReady"); err != nil {
			return outbox.StoredResult{}, err
		}
		return storedView(next, 200)
	})
	return decodeView(result, err)
}

func (s *Service) ConfirmReady(ctx context.Context, p identity.Principal, id string, in ReadyWrite, key string) (View, error) {
	if err := s.configured(); err != nil {
		return View{}, err
	}
	if err := p.RequireUser("rental:ready"); err != nil {
		return View{}, err
	}
	if !ids.Valid(id) || in.Expected < 1 || !ids.Valid(in.Environment) {
		return View{}, fault.New(fault.Invalid, "Original buyer readiness required")
	}
	c, err := command(p, "confirmRentalReady", id, key, in)
	if err != nil {
		return View{}, err
	}
	result, err := (outbox.Idempotency{DB: s.db}).DoAuthorized(ctx, c, s.authorizeSession(ctx, p, id, "rental:ready", false), func(tx pgx.Tx) (outbox.StoredResult, error) {
		o, err := lockSessionOrder(ctx, tx, id, p, false)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		r, err := loadSessionTx(ctx, tx, id, o)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		if in.Environment != r.Session.Contract.Environment || r.Session.DeviceReady == nil {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Original prepared device required")
		}
		funding, resource, err := s.currentWorkTx(ctx, tx, o, r)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		// Recheck the device's previously approved references too. A revocation
		// between the two parties' confirmations cannot start the paid clock.
		d := r.Session.DeviceReady
		if _, err = readinessTx(ctx, tx, r, o.Seller, d.Connection, d.Readiness, "RENTAL_DEVICE_READY", o.Now); err != nil {
			return outbox.StoredResult{}, err
		}
		proof, err := readinessTx(ctx, tx, r, p.UserID, in.Connection, in.Readiness, "RENTAL_BUYER_READY", o.Now)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		next, err := ConfirmBuyer(r.Session, in.Expected, resource, proof, in.Budget, funding, o.Now)
		if err != nil {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Both readiness proofs, current funding and exact budget required")
		}
		if err = updateSessionTx(ctx, tx, r.Session, next); err != nil {
			return outbox.StoredResult{}, err
		}
		if err = updatePhysicalTx(ctx, tx, next); err != nil {
			return outbox.StoredResult{}, err
		}
		if err = recordHistoryTx(ctx, tx, next, p.UserID, "confirmRentalReady", "RentalStarted"); err != nil {
			return outbox.StoredResult{}, err
		}
		return storedView(next, 200)
	})
	return decodeView(result, err)
}
