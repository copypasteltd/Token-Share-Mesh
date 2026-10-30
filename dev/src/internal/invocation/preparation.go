// Package invocation persists original routed calls, bounded encrypted native
// input and qualified initial lease admission. Service prepares metadata only;
// Execution needs separately provisioned input/issuer keys. Neither mounts a
// model endpoint, issues a relay Grant or delivers input to an actual engine.
package invocation

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"p11.local/compute/client/protocol/executionlease"
	relay "p11.local/compute/gateway/core"
	"p11.local/compute/internal/budget"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/internal/persistence"
	"p11.local/compute/internal/pricing"
	"p11.local/compute/internal/routecontract"
	"p11.local/compute/internal/trade"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/money"
	"p11.local/compute/shared/strictjson"
)

const Profile = "ROUTE_PREPARE_1"

type Config struct {
	DB                 *persistence.DB
	PaymentEnvironment string
	FingerprintVersion string
	FingerprintKeys    map[string][]byte
}

type Service struct {
	db      *persistence.DB
	routes  routecontract.Service
	version string
	mu      sync.RWMutex
	keys    map[string][]byte
}

// Request is supplied by trusted protocol preflight, not decoded as extra
// fields in standard SDK JSON. RequestDigest is a transient exact-body
// digest; only its scoped, versioned HMAC is persisted, never the raw digest.
// The host must retain the ORIGINAL request key when resolving an unknown
// commit. An absent key is not silently replaced or automatically retried.
type Request struct {
	Model, RequestDigest, IdempotencyKey string
	Protocol                             relay.Profile
	MaxOutputTokens                      int
	Stream                               bool
}

type Spec = executionlease.Spec

// Preparation is safe recovery metadata, deliberately not relay.Grant or an
// executionlease. Unknown, expired and canceled work keeps its original hold.
type Preparation struct {
	JobID, OrderID, RouteID, SellerID, State string
	Deadline                                 time.Time
	ReservationID, BudgetState               string
	ReservedFen, RemainingFen                money.Fen
	UsedCalls                                int64
	Replayed                                 bool
}

func New(c Config) (*Service, error) {
	if c.DB == nil || (c.PaymentEnvironment != "SANDBOX" && c.PaymentEnvironment != "PRODUCTION") ||
		!ids.Valid(c.FingerprintVersion) || len(c.FingerprintKeys) < 1 || len(c.FingerprintKeys) > 16 || len(c.FingerprintKeys[c.FingerprintVersion]) < 32 {
		return nil, fault.New(fault.Unavailable, "Explicit invocation database, environment and fingerprint keys required")
	}
	s := &Service{db: c.DB, routes: routecontract.Service{DB: c.DB, PaymentEnvironment: c.PaymentEnvironment}, version: c.FingerprintVersion, keys: map[string][]byte{}}
	for version, key := range c.FingerprintKeys {
		if !ids.Valid(version) || len(key) < 32 || len(key) > 64 {
			s.Close()
			return nil, fault.New(fault.Invalid, "Invalid invocation fingerprint key configuration")
		}
		s.keys[version] = append([]byte(nil), key...)
	}
	return s, nil
}

// Close drops this service's key copies. It never releases a call or budget.
func (s *Service) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for version, key := range s.keys {
		clear(key)
		delete(s.keys, version)
	}
}

func validate(in Request) error {
	if !ids.Valid(in.Model) || len(in.RequestDigest) != 64 || in.MaxOutputTokens < 1 || in.MaxOutputTokens > 32768 ||
		len(in.IdempotencyKey) < 8 || len(in.IdempotencyKey) > 128 ||
		(in.Protocol != relay.ChatText && in.Protocol != relay.ResponsesText && in.Protocol != relay.MessagesText) {
		return fault.New(fault.Invalid, "Explicit bounded native request and original key required")
	}
	for _, ch := range in.RequestDigest {
		if ch < '0' || ch > '9' && ch < 'a' || ch > 'f' {
			return fault.New(fault.Invalid, "Invalid transient request digest")
		}
	}
	for _, ch := range in.IdempotencyKey {
		if ch < 33 || ch > 126 {
			return fault.New(fault.Invalid, "Invalid original request key")
		}
	}
	return nil
}

func (s *Service) fingerprint(version string, p identity.Principal, route string, in Request) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key := s.keys[version]
	if len(key) < 32 {
		return "", fault.New(fault.Unavailable, "Original invocation fingerprint key unavailable; recover without reissuing")
	}
	data, err := json.Marshal(struct {
		Domain, Buyer, Route, Key, BodyDigest string
		Spec                                  Spec
	}{"P11_INVOCATION_FINGERPRINT_1", p.UserID, route, p.KeyID, in.RequestDigest, Spec{Protocol: in.Protocol, Model: in.Model, MaxOutputTokens: in.MaxOutputTokens, Stream: in.Stream}})
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(data)
	clear(data)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func scope(c routecontract.Contract) budget.Scope {
	return budget.Scope{OrderID: c.Definition.OrderID, BuyerID: c.BuyerID, SellerID: c.Definition.SellerID, ContractHash: c.ContractHash}
}

func preparation(c routecontract.Contract, job, state string, deadline time.Time, b budget.Result, replay bool) Preparation {
	return Preparation{JobID: job, OrderID: c.Definition.OrderID, RouteID: c.ID, SellerID: c.Definition.SellerID,
		State: state, Deadline: deadline, ReservationID: b.Reservation.ID, BudgetState: b.Reservation.State,
		ReservedFen: b.Reservation.MaximumFen, RemainingFen: b.Capacity.Remaining, UsedCalls: c.UsedCalls, Replayed: replay}
}

func (s *Service) Prepare(ctx context.Context, p identity.Principal, in Request) (Preparation, error) {
	var result Preparation
	if s == nil || s.db == nil {
		return result, fault.New(fault.Unavailable, "Invocation preparation unavailable")
	}
	err := s.db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		var err error
		result, err = s.PrepareTx(ctx, tx, p, in)
		return err
	})
	if err != nil {
		return Preparation{}, err
	}
	return result, nil
}

// PrepareTx composes ONLY SQL writes. The caller owns commit and must not treat
// its uncommitted return value as permission for any backend/network effect.
func (s *Service) PrepareTx(ctx context.Context, tx pgx.Tx, p identity.Principal, in Request) (Preparation, error) {
	var result Preparation
	if s == nil || s.db == nil || tx == nil {
		return result, fault.New(fault.Unavailable, "Invocation preparation unavailable")
	}
	if err := validate(in); err != nil {
		return result, err
	}
	c, err := s.routes.LockForPreparationTx(ctx, tx, p, in.Model, in.Protocol)
	if err != nil {
		return result, err
	}
	// Scope follows the original buyer/route, so switching API keys cannot mint
	// another purchase for the same original request key. Different keys conflict.
	caller := outbox.Digest([]byte("P11_ORIGINAL_CALL_1\x00" + p.UserID + "\x00" + c.ID + "\x00" + in.IdempotencyKey))
	var job, originalKey, version, digest, state string
	var deadline time.Time
	err = tx.QueryRow(ctx, `SELECT id,invoker_key_id,fingerprint_version,request_digest,state,deadline FROM p11.jobs
WHERE order_id=$1 AND caller_key=$2 AND invocation_profile=$3 AND route_contract_id=$4 FOR UPDATE`, c.Definition.OrderID, caller, Profile, c.ID).
		Scan(&job, &originalKey, &version, &digest, &state, &deadline)
	if err == nil {
		if originalKey != p.KeyID {
			return result, fault.New(fault.Conflict, "Original request belongs to another bound key")
		}
		expected, err := s.fingerprint(version, p, c.ID, in)
		if err != nil {
			return result, err
		}
		if !hmac.Equal([]byte(digest), []byte(expected)) {
			return result, fault.New(fault.Conflict, "Original invocation input differs")
		}
		b, err := budget.LookupTx(ctx, tx, scope(c), job)
		if err != nil {
			return result, err
		}
		return preparation(c, job, state, deadline, b, true), nil
	}
	if err != pgx.ErrNoRows {
		return result, err
	}
	if c.State != "ACTIVE" {
		return result, fault.New(fault.Conflict, "Original route has no new call capacity")
	}
	var contract trade.Contract
	if strictjson.Decode(c.OrderContract, &contract, 65536) != nil || contract.Spec.InputTokenLimit < 1 || int64(in.MaxOutputTokens) > contract.Spec.OutputTokenLimit {
		return result, fault.New(fault.Invalid, "Request exceeds original token bounds")
	}
	output := int64(in.MaxOutputTokens)
	maximum, err := contract.Price.Amount(pricing.Usage{InputTokens: &contract.Spec.InputTokenLimit, OutputTokens: &output})
	if err != nil {
		return result, err
	}
	if maximum > c.Definition.PerCallFen {
		return result, fault.New(fault.Conflict, "Original per-call budget cannot cover frozen maximum usage")
	}
	var keyExpiry time.Time
	if err = tx.QueryRow(ctx, `SELECT expires_at FROM p11.api_keys WHERE id=$1 AND user_id=$2 FOR SHARE`, p.KeyID, p.UserID).Scan(&keyExpiry); err != nil {
		return result, err
	}
	deadline = c.CurrentDeadline
	if keyExpiry.Before(deadline) {
		deadline = keyExpiry
	}
	digest, err = s.fingerprint(s.version, p, c.ID, in)
	if err != nil {
		return result, err
	}
	job, reservation := ids.New("job"), ids.New("bgt")
	spec, _ := json.Marshal(Spec{Protocol: in.Protocol, Model: in.Model, MaxOutputTokens: in.MaxOutputTokens, Stream: in.Stream})
	_, err = tx.Exec(ctx, `INSERT INTO p11.jobs(id,order_id,seller_id,caller_key,request_digest,state,deadline,
invocation_profile,route_contract_id,invoker_key_id,fingerprint_version,invocation_spec,reservation_id)
VALUES($1,$2,$3,$4,$5,'INPUT_PENDING',$6,$7,$8,$9,$10,$11,$12)`, job, c.Definition.OrderID, c.Definition.SellerID, caller, digest, deadline, Profile, c.ID, p.KeyID, s.version, spec, reservation)
	if err != nil {
		return result, err
	}
	// Freeze both IDs before the first write; the deferred FK closes at commit.
	// The immutable job binding never needs a privileged patch or placeholder.
	b, err := budget.ReserveIdentifiedTx(ctx, tx, scope(c), budget.Request{PurposeRef: job, PurposeKind: "JOB", MaximumFen: c.Definition.PerCallFen}, reservation)
	if err != nil {
		return result, err
	}
	payload, _ := json.Marshal(map[string]string{"job_id": job, "route_id": c.ID, "order_id": c.Definition.OrderID, "contract_hash": c.ContractHash})
	_, err = tx.Exec(ctx, `INSERT INTO p11.outbox_events(id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,safe_payload,state)
VALUES($1,'ROUTE_INVOCATION',$2,1,'RouteInvocationPrepared','3.0',$3,'PENDING')`, ids.New("evt"), job, payload)
	if err != nil {
		return result, err
	}
	c.UsedCalls++
	return preparation(c, job, "INPUT_PENDING", deadline, b, false), nil
}
