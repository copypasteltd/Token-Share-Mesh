package rental

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"math"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"p11.local/compute/client/protocol/rentalaccess"
	"p11.local/compute/internal/budget"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/market"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/internal/persistence"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/money"
)

const AuthorityProfile = "P11_RENTAL_SESSION_AUTHORITY_1"
const ManifestProfile = "P11_RENTAL_EXECUTION_MANIFEST_1"

// Config is trusted process configuration. Pins are independently provisioned;
// neither a device report nor an incoming JWT can choose the issuing identity.
type Config struct {
	DB                 *persistence.DB
	PaymentEnvironment string
	Issuer, KeyID      string
	SigningKey         ed25519.PrivateKey
	IssuerPins         map[string]ed25519.PublicKey
}

type Service struct {
	db          *persistence.DB
	environment string
	issuer      string
	keyID       string
	signer      *rentalaccess.Signer
	closed      atomic.Bool
}

func NewService(c Config) (*Service, error) {
	if c.DB == nil || (c.PaymentEnvironment != "SANDBOX" && c.PaymentEnvironment != "PRODUCTION") || len(c.SigningKey) != ed25519.PrivateKeySize || len(c.IssuerPins[c.KeyID]) != ed25519.PublicKeySize {
		return nil, fault.New(fault.Invalid, "Explicit rental database, payment environment and independent issuer pins required")
	}
	derived := ed25519.NewKeyFromSeed(c.SigningKey.Seed())
	defer clear(derived)
	if !bytes.Equal(derived, c.SigningKey) || !bytes.Equal(derived.Public().(ed25519.PublicKey), c.IssuerPins[c.KeyID]) {
		return nil, fault.New(fault.Invalid, "Rental signer does not match the independently configured key")
	}
	signer, err := rentalaccess.NewSigner(c.Issuer, c.KeyID, c.SigningKey)
	if err != nil {
		return nil, fault.New(fault.Invalid, "Explicit bounded rental signing identity required")
	}
	return &Service{db: c.DB, environment: c.PaymentEnvironment, issuer: c.Issuer, keyID: c.KeyID, signer: signer}, nil
}

func (s *Service) Close() {
	if s != nil && s.closed.CompareAndSwap(false, true) {
		s.signer.Close()
	}
}

func (s *Service) configured() error {
	if s == nil || s.db == nil || s.db.Pool == nil || s.closed.Load() {
		return fault.New(fault.Unavailable, "Rental authority is not configured")
	}
	return nil
}

type CreateWrite struct {
	Order        string    `json:"order_id"`
	OrderVersion int64     `json:"expected_order_version"`
	Device       string    `json:"device_id"`
	Environment  string    `json:"environment_revision"`
	MaximumMS    int64     `json:"max_duration_ms"`
	Budget       money.Fen `json:"confirmed_budget_fen"`
	Disconnect   string    `json:"disconnect_policy"`
}

type ReadyWrite struct {
	Expected    int64     `json:"expected_version"`
	Environment string    `json:"environment_revision"`
	Connection  string    `json:"connection_evidence_ref"`
	Readiness   string    `json:"readiness_evidence_ref"`
	Budget      money.Fen `json:"confirmed_budget_fen"`
}

type DeviceReadyWrite struct {
	Expected    int64  `json:"expected_version"`
	Environment string `json:"environment_revision"`
	Generation  string `json:"resource_lock_generation"`
	Connection  string `json:"connection_evidence_ref"`
	Readiness   string `json:"readiness_evidence_ref"`
}

type StopWrite struct {
	Expected int64  `json:"expected_version"`
	Reason   string `json:"reason_code"`
}

// Unconfirmed meter fields are absent, never synthesized as zero. An unstarted
// reservation has no paid hard deadline; its separate preparation TTL is not
// represented as an already started rental clock.
type View struct {
	ID           string     `json:"session_id"`
	Order        string     `json:"order_id"`
	Seller       string     `json:"seller_id"`
	Device       string     `json:"device_id"`
	Version      int64      `json:"version"`
	State        string     `json:"state"`
	Environment  string     `json:"environment_revision"`
	HardDeadline *time.Time `json:"hard_deadline_at,omitempty"`
	Started      *time.Time `json:"started_at,omitempty"`
	Budget       money.Fen  `json:"confirmed_budget_fen"`
	VerifiedMS   *int64     `json:"verified_available_ms,omitempty"`
	Charge       *money.Fen `json:"charge_fen,omitempty"`
	Cleanup      string     `json:"cleanup_state"`
	Replayed     bool       `json:"-"`
}

func timePointer(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	t = t.UTC()
	return &t
}

func viewOf(s Session) View {
	return View{ID: s.ID, Order: s.Contract.Order, Seller: s.Contract.Seller, Device: s.Contract.Device, Version: s.Version, State: s.State, Environment: s.Contract.Environment, HardDeadline: timePointer(s.HardDeadline), Started: timePointer(s.Started), Budget: s.Budget, Cleanup: s.Cleanup}
}

// Manifest contains identifiers/digests and public limits only. It never carries
// a prompt, arbitrary executable, user path, remote URL or downloaded model.
// Raw bytes, rather than a later JSONB serialization, define ManifestHash.
type Manifest = rentalaccess.Manifest

func userAuthority(ctx context.Context, p identity.Principal, scope string) func(pgx.Tx) error {
	return func(tx pgx.Tx) error { return identity.RequireRoleTx(ctx, tx, p, scope, "BUYER", true) }
}

func command(p identity.Principal, op, resource, key string, in any) (outbox.Command, error) {
	raw, err := json.Marshal(in)
	return outbox.Command{Principal: p.UserID, PrincipalVersion: p.Version, Operation: op, Resource: resource, Key: key, Request: raw}, err
}

func storedView(v Session, status int) (outbox.StoredResult, error) {
	raw, err := json.Marshal(viewOf(v))
	return outbox.StoredResult{Reference: v.ID, Body: raw, HTTPStatus: status}, err
}

func decodeView(r outbox.StoredResult, err error) (View, error) {
	var v View
	if err == nil {
		err = json.Unmarshal(r.Body, &v)
		v.Replayed = r.Replayed
	}
	return v, err
}

func (s *Service) Create(ctx context.Context, p identity.Principal, in CreateWrite, key string) (View, error) {
	if err := s.configured(); err != nil {
		return View{}, err
	}
	if err := p.RequireUser("rental:create"); err != nil {
		return View{}, err
	}
	if !ids.Valid(in.Order) || !ids.Valid(in.Device) || !ids.Valid(in.Environment) || in.OrderVersion < 1 || in.MaximumMS < 1 || in.MaximumMS > MaximumSessionDurationMS || in.Budget <= 0 || int64(in.Budget) > money.MaxFen || (in.Disconnect != "STOP_AFTER_GRACE" && in.Disconnect != "CONTINUE_WITHIN_CONFIRMED_LIMIT") {
		return View{}, fault.New(fault.Invalid, "Exact original rental limits required")
	}
	c, err := command(p, "createRentalSession", in.Order, key, in)
	if err != nil {
		return View{}, err
	}
	result, err := (outbox.Idempotency{DB: s.db}).DoAuthorized(ctx, c, userAuthority(ctx, p, "rental:create"), func(tx pgx.Tx) (outbox.StoredResult, error) {
		o, err := lockOrder(ctx, tx, in.Order, p.UserID)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		if o.Version != in.OrderVersion || o.Contract.Spec.DurationMS != in.MaximumMS || o.Contract.BudgetFen != in.Budget {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Original confirmed rental contract changed")
		}
		if err = o.admit(); err != nil {
			return outbox.StoredResult{}, err
		}
		var exists bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM p11.rental_sessions WHERE order_id=$1)`, o.ID).Scan(&exists); err != nil {
			return outbox.StoredResult{}, err
		}
		if exists {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Original order already owns a rental; recover that session")
		}
		funding, err := s.fundingTx(ctx, tx, o, "", o.Now)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		offer, err := (market.Catalog{DB: s.db}).LoadConfirmedTx(ctx, tx, o.Contract.ListingID, o.Contract.ListingRevision)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		if offer.Availability != "AVAILABLE" || offer.SellerID != o.Seller {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Original rental device is currently unavailable")
		}
		binding, err := o.binding(in.Device, in.Environment)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		manifest := Manifest{Profile: ManifestProfile, Reference: ids.New("rmanifest"), Session: ids.New("rental"), Order: o.ID, Buyer: o.Buyer, Seller: o.Seller, Device: binding.DeviceID, Capability: binding.CapabilityID, Environment: binding.EnvironmentRef, EnvironmentHash: binding.EnvironmentDigest, ContractHash: o.Hash, Mode: "GPU_RENTAL", Privacy: "STANDARD", Protocol: o.Contract.Policy.Protocol, PublicSpec: o.Contract.Spec}
		if err = loadCapabilityTx(ctx, tx, binding, o.Seller, &manifest); err != nil {
			return outbox.StoredResult{}, err
		}
		resource, err := lockResource(ctx, tx, manifest.Resource, o.Seller, manifest.Device)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		generation, err := strconv.ParseInt(resource.Generation, 10, 64)
		if err != nil || generation < 0 || generation == math.MaxInt64 || resource.Phase != "FREE" || resource.AttemptOwner != "" || resource.SessionOwner != "" {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Explicit free original physical slot required")
		}
		manifest.Generation = strconv.FormatInt(generation+1, 10)
		manifestRaw, err := json.Marshal(manifest)
		if err != nil || len(manifestRaw) > 16384 {
			return outbox.StoredResult{}, fault.New(fault.Unavailable, "Bounded rental manifest unavailable")
		}
		contract := Contract{Order: o.ID, ContractHash: o.Hash, Buyer: o.Buyer, Seller: o.Seller, Device: manifest.Device, Resource: manifest.Resource, Environment: manifest.Environment, EnvironmentHash: manifest.EnvironmentHash, Manifest: manifest.Reference, ManifestHash: outbox.Digest(manifestRaw), Price: o.Contract.Price, Budget: in.Budget, MaximumDurationMS: in.MaximumMS, OriginalDeadline: o.Deadline, DisconnectPolicy: in.Disconnect, PaymentEnvironment: s.environment}
		session, err := Reserve(manifest.Session, ids.New("rterms"), contract, resource, o.Now)
		if err != nil || session.Generation != manifest.Generation || !funding.covers(contract, session.Budget, o.Now, minTime(o.Deadline, funding.ValidUntil)) {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Current original funding and physical reservation required")
		}
		if err = insertSessionTx(ctx, tx, session, binding, manifestRaw, funding.Payment, ids.New("rconnect")); err != nil {
			return outbox.StoredResult{}, err
		}
		if _, err = budget.ReserveTx(ctx, tx, budget.Scope{OrderID: o.ID, BuyerID: o.Buyer, SellerID: o.Seller, ContractHash: o.Hash}, budget.Request{PurposeRef: session.ID, PurposeKind: "RENTAL_SESSION", MaximumFen: session.Budget}); err != nil {
			return outbox.StoredResult{}, err
		}
		if err = reservePhysicalTx(ctx, tx, session, resource.Generation); err != nil {
			return outbox.StoredResult{}, err
		}
		if err = recordHistoryTx(ctx, tx, session, p.UserID, "createRentalSession", "RentalReserved"); err != nil {
			return outbox.StoredResult{}, err
		}
		return storedView(session, 201)
	})
	return decodeView(result, err)
}

func (s *Service) Get(ctx context.Context, p identity.Principal, id string) (View, error) {
	if err := s.configured(); err != nil {
		return View{}, err
	}
	if err := p.RequireUser("rental:read"); err != nil {
		return View{}, err
	}
	if !ids.Valid(id) {
		return View{}, fault.New(fault.Invalid, "Original session reference required")
	}
	var v View
	err := s.db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		if err := identity.RequireCurrentTx(ctx, tx, p); err != nil {
			return err
		}
		if err := requireReaderRoleTx(ctx, tx, p, id); err != nil {
			return err
		}
		o, err := lockSessionOrder(ctx, tx, id, p, true)
		if err != nil {
			return err
		}
		r, err := loadSessionTx(ctx, tx, id, o)
		if err != nil {
			return err
		}
		v = viewOf(r.Session)
		// Legacy is_final alone is not independently confirmed provenance. The
		// qualified meter repository is not wired yet, so omit accounting even
		// when an old unqualified statement happens to exist in this database.
		return nil
	})
	return v, err
}

// Stop does not depend on current funding/qualification remaining valid: losing
// permission may never make the original buyer unable to request a stop.
func (s *Service) Stop(ctx context.Context, p identity.Principal, id string, in StopWrite, key string) (View, error) {
	if err := s.configured(); err != nil {
		return View{}, err
	}
	if err := p.RequireUser("rental:stop"); err != nil {
		return View{}, err
	}
	if !ids.Valid(id) || !ids.Valid(in.Reason) || in.Expected < 1 {
		return View{}, fault.New(fault.Invalid, "Original rental stop and version required")
	}
	c, err := command(p, "stopRentalSession", id, key, in)
	if err != nil {
		return View{}, err
	}
	result, err := (outbox.Idempotency{DB: s.db}).DoAuthorized(ctx, c, s.authorizeSession(ctx, p, id, "rental:stop", false), func(tx pgx.Tx) (outbox.StoredResult, error) {
		o, err := lockSessionOrder(ctx, tx, id, p, false)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		r, err := loadSessionTx(ctx, tx, id, o)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		resource, err := lockResource(ctx, tx, r.Session.Contract.Resource, o.Seller, r.Session.Contract.Device)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		next, err := Stop(r.Session, in.Expected, resource, in.Reason, o.Now)
		if err != nil {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Current original rental stop unavailable")
		}
		if next.Version == r.Session.Version {
			return storedView(next, 200)
		}
		if _, err = tx.Exec(ctx, `UPDATE p11.rental_access_grants SET revoked_at=$2 WHERE session_id=$1 AND revoked_at IS NULL`, id, o.Now); err != nil {
			return outbox.StoredResult{}, err
		}
		if err = updateSessionTx(ctx, tx, r.Session, next); err != nil {
			return outbox.StoredResult{}, err
		}
		if err = updatePhysicalTx(ctx, tx, next); err != nil {
			return outbox.StoredResult{}, err
		}
		if err = recordHistoryTx(ctx, tx, next, p.UserID, "stopRentalSession", "RentalStopRequested"); err != nil {
			return outbox.StoredResult{}, err
		}
		return storedView(next, 200)
	})
	return decodeView(result, err)
}
