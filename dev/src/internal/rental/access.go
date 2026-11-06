package rental

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"p11.local/compute/client/protocol/rentalaccess"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
)

type AccessWrite struct {
	Expected int64    `json:"expected_version"`
	Scopes   []string `json:"scopes"`
}

type AccessMetadata struct {
	Grant           string    `json:"grant_id"`
	Session         string    `json:"session_id"`
	SessionVersion  int64     `json:"session_version"`
	Scopes          []string  `json:"scopes"`
	Expires         time.Time `json:"expires_at"`
	Connection      string    `json:"connection_ref"`
	EnvironmentHash string    `json:"kernel_environment_digest"`
	CredentialState string    `json:"credential_state"`
}

// AccessResult's ordinary serialization is always safe metadata. Only the
// explicit HTTP writer consumes the committed first response once. A failed
// socket write does not persist/reissue the token: the original key replays
// metadata and an explicit new grant revokes the lost credential.
type AccessResult struct {
	mu       sync.Mutex
	metadata AccessMetadata
	issued   *rentalaccess.Issued
	Replayed bool `json:"-"`
}

func (r *AccessResult) Metadata() AccessMetadata {
	if r == nil {
		return AccessMetadata{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	v := r.metadata
	v.Scopes = append([]string(nil), v.Scopes...)
	return v
}
func (r *AccessResult) MarshalJSON() ([]byte, error) { return json.Marshal(r.Metadata()) }
func (r *AccessResult) String() string               { return "[REDACTED rental access response]" }
func (r *AccessResult) GoString() string             { return r.String() }
func (r *AccessResult) Close() {
	if r != nil {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.issued != nil {
			r.issued.Close()
			r.issued = nil
		}
	}
}

func (r *AccessResult) WriteJSONOnce(w io.Writer) error {
	if r == nil || w == nil {
		return fault.New(fault.Unavailable, "Rental response unavailable")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	v := struct {
		AccessMetadata
		Credential string `json:"credential_once,omitempty"`
	}{AccessMetadata: r.metadata}
	if r.issued != nil {
		credential, err := r.issued.TakeCredential()
		r.issued.Close()
		r.issued = nil
		if err != nil {
			return fault.New(fault.Unavailable, "First rental credential is no longer available")
		}
		v.Credential = credential
		v.CredentialState = "ISSUED_ONCE"
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	defer clear(raw)
	n, err := w.Write(raw)
	if err == nil && n != len(raw) {
		err = io.ErrShortWrite
	}
	return err
}

func validAccessScopes(scopes []string) bool {
	if len(scopes) < 1 || len(scopes) > 4 {
		return false
	}
	seen := map[string]bool{}
	for _, scope := range scopes {
		if seen[scope] || (scope != "INVOKE" && scope != "QUEUE_SELF" && scope != "CANCEL_SELF" && scope != "EXPORT_SELF") {
			return false
		}
		seen[scope] = true
	}
	return true
}

func (s *Service) CreateAccess(ctx context.Context, p identity.Principal, id string, in AccessWrite, key string) (*AccessResult, error) {
	if err := s.configured(); err != nil {
		return nil, err
	}
	if err := p.RequireUser("rental:access"); err != nil {
		return nil, err
	}
	if !ids.Valid(id) || in.Expected < 1 || !validAccessScopes(in.Scopes) {
		return nil, fault.New(fault.Invalid, "Bounded original rental scopes and version required")
	}
	in.Scopes = append([]string(nil), in.Scopes...)
	c, err := command(p, "createRentalAccessGrant", id, key, in)
	if err != nil {
		return nil, err
	}
	var issued *rentalaccess.Issued
	result, err := (outbox.Idempotency{DB: s.db}).DoAuthorized(ctx, c, s.authorizeSession(ctx, p, id, "rental:access", false), func(tx pgx.Tx) (outbox.StoredResult, error) {
		o, err := lockSessionOrder(ctx, tx, id, p, false)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		r, err := loadSessionTx(ctx, tx, id, o)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		v := r.Session
		if v.Version != in.Expected || v.Version == math.MaxInt64 || v.State != "ACTIVE" || !o.Now.Before(v.HardDeadline) {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Current active original rental terms required")
		}
		funding, resource, err := s.currentWorkTx(ctx, tx, o, r)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		if resource.Phase != "ACTIVE" || !funding.covers(v.Contract, v.Budget, o.Now, v.HardDeadline) {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Current original rental funds and physical owner required")
		}
		claims := rentalaccess.Claims{Profile: rentalaccess.Profile, Issuer: s.issuer, Subject: o.Buyer, Audience: v.Contract.Device, ID: ids.New("rgrant"), Issued: o.Now.Unix(), NotBefore: o.Now.Unix(), Expires: v.HardDeadline.Unix(), Order: o.ID, Session: v.ID, Seller: o.Seller, Device: v.Contract.Device, Resource: v.Contract.Resource, Generation: v.Generation, Terms: strconv.FormatInt(v.Terms, 10), TermsHash: v.TermsHash, Environment: v.Contract.Environment, EnvironmentHash: v.Contract.EnvironmentHash, Manifest: v.Contract.Manifest, ManifestHash: v.Contract.ManifestHash, Scopes: in.Scopes, HardDeadline: v.HardDeadline.UTC().Format(time.RFC3339Nano), Mode: "GPU_RENTAL", Privacy: "STANDARD"}
		issued, err = s.signer.Issue(claims, o.Now)
		if err != nil {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Bounded original rental credential unavailable")
		}
		metadata := issued.Metadata()
		safeClaims, err := json.Marshal(metadata)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		if _, err = tx.Exec(ctx, `UPDATE p11.rental_access_grants SET revoked_at=$3 WHERE session_id=$1 AND subject_id=$2 AND revoked_at IS NULL`, v.ID, o.Buyer, o.Now); err != nil {
			return outbox.StoredResult{}, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO p11.rental_access_grants(id,session_id,subject_id,digest,resource_lock_generation,terms_revision,terms_hash,safe_claims,scopes,expires_at,authority_profile,authority_session_version)
VALUES($1,$2,$3,$4,$5::bigint,$6,$7,$8,$9,$10,$11,$12)`, claims.ID, v.ID, o.Buyer, metadata.Digest, v.Generation, v.Terms, v.TermsHash, safeClaims, in.Scopes, time.Unix(claims.Expires, 0).UTC(), rentalaccess.Profile, v.Version+1)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		next := v.clone()
		next.Version++
		if err = updateSessionTx(ctx, tx, v, next); err != nil {
			return outbox.StoredResult{}, err
		}
		if err = recordHistoryTx(ctx, tx, next, p.UserID, "createRentalAccessGrant", "RentalAccessIssued"); err != nil {
			return outbox.StoredResult{}, err
		}
		body, err := json.Marshal(AccessMetadata{Grant: claims.ID, Session: v.ID, SessionVersion: next.Version, Scopes: in.Scopes, Expires: time.Unix(claims.Expires, 0).UTC(), Connection: r.Connection, EnvironmentHash: v.Contract.EnvironmentHash, CredentialState: "NOT_REPLAYABLE"})
		return outbox.StoredResult{Reference: claims.ID, Body: body, HTTPStatus: 201}, err
	})
	if err != nil {
		if issued != nil {
			issued.Close()
		}
		return nil, err
	}
	var metadata AccessMetadata
	if err = json.Unmarshal(result.Body, &metadata); err != nil {
		if issued != nil {
			issued.Close()
		}
		return nil, err
	}
	if result.Replayed && issued != nil {
		issued.Close()
		issued = nil
	}
	return &AccessResult{metadata: metadata, issued: issued, Replayed: result.Replayed}, nil
}
