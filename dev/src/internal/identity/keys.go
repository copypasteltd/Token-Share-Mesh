package identity

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/internal/persistence"
	"p11.local/compute/shared/clock"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type Key struct {
	ID          string    `json:"key_id"`
	Fingerprint string    `json:"fingerprint"`
	Label       string    `json:"label"`
	Scopes      []string  `json:"scopes"`
	RouteIDs    []string  `json:"route_ids"`
	ExpiresAt   time.Time `json:"expires_at"`
	State       string    `json:"state"`
}
type KeyInput struct {
	Label     string    `json:"label"`
	Scopes    []string  `json:"scopes"`
	RouteIDs  []string  `json:"route_ids"`
	ExpiresAt time.Time `json:"expires_at"`
}
type CreatedKey struct {
	Key        Key    `json:"key"`
	SecretOnce string `json:"secret_once"`
	Replayed   bool   `json:"-"`
}
type KeyPage struct {
	Items      []Key   `json:"items"`
	NextCursor *string `json:"next_cursor"`
}
type KeyService struct {
	DB            *persistence.DB
	Clock         clock.Clock
	PepperVersion string
	Peppers       map[string][]byte
	MaxTTL        time.Duration
}

var apiScopes = map[string]string{"ROUTE_INVOKE": "route:invoke", "ORDER_READ": "order:read", "ASSET_READ": "asset:read", "MARKET_READ": "market:read"}

func (s KeyService) validate() error {
	if s.DB == nil || s.Clock == nil || !ids.Valid(s.PepperVersion) || len(s.Peppers[s.PepperVersion]) < 32 || s.MaxTTL <= 0 {
		return fault.New(fault.Unavailable, "Key service not configured")
	}
	return nil
}
func (s KeyService) userTx(ctx context.Context, tx pgx.Tx, p Principal) error {
	return RequireRoleTx(ctx, tx, p, "key:manage", "BUYER", true)
}
func normalize(input KeyInput) (KeyInput, error) {
	input.Label = strings.TrimSpace(input.Label)
	if !utf8.ValidString(input.Label) || utf8.RuneCountInString(input.Label) < 1 || utf8.RuneCountInString(input.Label) > 512 || len(input.Scopes) < 1 || len(input.Scopes) > 4 || len(input.RouteIDs) > 50 || input.ExpiresAt.IsZero() {
		return input, fault.New(fault.Invalid, "Invalid key label, scopes, routes or expiry")
	}
	input.Scopes = append([]string{}, input.Scopes...)
	input.RouteIDs = append([]string{}, input.RouteIDs...)
	sort.Strings(input.Scopes)
	sort.Strings(input.RouteIDs)
	for i, scope := range input.Scopes {
		if _, ok := apiScopes[scope]; !ok || i > 0 && input.Scopes[i-1] == scope {
			return input, fault.New(fault.Invalid, "Unsupported or duplicate key scope")
		}
		if scope == "ROUTE_INVOKE" && len(input.RouteIDs) == 0 {
			return input, fault.New(fault.Invalid, "Route invocation requires an explicitly bound route")
		}
	}
	for i, route := range input.RouteIDs {
		if !ids.Valid(route) || i > 0 && input.RouteIDs[i-1] == route {
			return input, fault.New(fault.Invalid, "Invalid or duplicate route binding")
		}
	}
	input.ExpiresAt = input.ExpiresAt.UTC().Truncate(time.Microsecond)
	return input, nil
}
func keyDigest(pepper []byte, secret string) string {
	mac := hmac.New(sha256.New, pepper)
	mac.Write([]byte("P11_API_KEY_V1\x00" + secret))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s KeyService) Create(ctx context.Context, p Principal, input KeyInput, idempotencyKey string) (CreatedKey, error) {
	var created CreatedKey
	if err := s.validate(); err != nil {
		return created, err
	}
	if err := p.RequireUser("key:manage"); err != nil {
		return created, err
	}
	input, err := normalize(input)
	if err != nil {
		return created, err
	}
	request, _ := json.Marshal(input)
	result, err := (outbox.Idempotency{DB: s.DB}).DoAuthorized(ctx, outbox.Command{Principal: p.UserID, PrincipalVersion: p.Version, Operation: "createApiKey", Resource: p.UserID, Key: idempotencyKey, Request: request}, func(tx pgx.Tx) error { return s.userTx(ctx, tx, p) }, func(tx pgx.Tx) (outbox.StoredResult, error) {
		if err := s.userTx(ctx, tx, p); err != nil {
			return outbox.StoredResult{}, err
		}
		if !input.ExpiresAt.After(s.Clock.Now()) || input.ExpiresAt.After(s.Clock.Now().Add(s.MaxTTL)) {
			return outbox.StoredResult{}, fault.New(fault.Invalid, "Invalid key expiry")
		}
		for _, route := range input.RouteIDs {
			var expires time.Time
			err := tx.QueryRow(ctx, `SELECT expires_at FROM p11.route_contracts WHERE id=$1 AND buyer_id=$2 AND state='ACTIVE' AND expires_at>clock_timestamp() FOR SHARE`, route, p.UserID).Scan(&expires)
			if err == pgx.ErrNoRows {
				return outbox.StoredResult{}, fault.New(fault.NotFound, "Route not found")
			}
			if err != nil {
				return outbox.StoredResult{}, err
			}
			if input.ExpiresAt.After(expires) {
				return outbox.StoredResult{}, fault.New(fault.Invalid, "Key cannot outlive its route")
			}
		}
		var bytes [32]byte
		if _, err := rand.Read(bytes[:]); err != nil {
			return outbox.StoredResult{}, err
		}
		id := ids.New("key")
		secret := "p11k." + id + "." + base64.RawURLEncoding.EncodeToString(bytes[:])
		created = CreatedKey{Key: Key{ID: id, Fingerprint: outbox.Digest([]byte(secret)), Label: input.Label, Scopes: input.Scopes, RouteIDs: input.RouteIDs, ExpiresAt: input.ExpiresAt, State: "ACTIVE"}, SecretOnce: secret}
		_, err := tx.Exec(ctx, `INSERT INTO p11.api_keys(id,user_id,key_digest,fingerprint,label,pepper_version,scopes,route_ids,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, id, p.UserID, keyDigest(s.Peppers[s.PepperVersion], secret), created.Key.Fingerprint, input.Label, s.PepperVersion, input.Scopes, input.RouteIDs, input.ExpiresAt)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		if err = audit(ctx, tx, p.UserID, "API_KEY_CREATED", id); err != nil {
			return outbox.StoredResult{}, err
		}
		safe, _ := json.Marshal(created.Key)
		return outbox.StoredResult{Reference: id, Body: safe, HTTPStatus: 201}, nil
	})
	if err != nil {
		return CreatedKey{}, err
	}
	if result.Replayed {
		if err = json.Unmarshal(result.Body, &created.Key); err != nil {
			return CreatedKey{}, err
		}
		created.SecretOnce = ""
		created.Replayed = true
	}
	return created, nil
}
func audit(ctx context.Context, tx pgx.Tx, user, action, target string) error {
	_, err := tx.Exec(ctx, `INSERT INTO p11.audit_events(id,actor_id,action,target_ref,request_id,safe_facts) VALUES($1,$2,$3,$4,$5,'{}')`, ids.New("aud"), user, action, target, ids.New("req"))
	return err
}

func (s KeyService) Authenticate(ctx context.Context, secret string) (Principal, error) {
	var p Principal
	if err := s.validate(); err != nil {
		return p, err
	}
	bad := fault.New(fault.Unauthenticated, "Invalid or expired credential")
	parts := strings.Split(secret, ".")
	if len(parts) != 3 || parts[0] != "p11k" || !ids.Valid(parts[1]) || len(secret) > 256 {
		return p, bad
	}
	random, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(random) != 32 {
		return p, bad
	}
	var digest, version, userState string
	var scopes []string
	var expires time.Time
	var revoked *time.Time
	err = s.DB.Pool.QueryRow(ctx, `SELECT k.user_id,k.key_digest,k.pepper_version,k.scopes,k.route_ids,k.expires_at,k.revoked_at,u.state,u.version FROM p11.api_keys k JOIN p11.users u ON u.id=k.user_id WHERE k.id=$1`, parts[1]).Scan(&p.UserID, &digest, &version, &scopes, &p.RouteIDs, &expires, &revoked, &userState, &p.Version)
	if err == pgx.ErrNoRows {
		return Principal{}, bad
	}
	if err != nil {
		return Principal{}, fault.Wrap(fault.Unavailable, "Authentication unavailable", err)
	}
	pepper, ok := s.Peppers[version]
	if !ok || len(pepper) < 32 || subtle.ConstantTimeCompare([]byte(digest), []byte(keyDigest(pepper, secret))) != 1 || revoked != nil || !expires.After(s.Clock.Now()) || userState != "ACTIVE" {
		return Principal{}, bad
	}
	for _, scope := range scopes {
		mapped, ok := apiScopes[scope]
		if !ok {
			return Principal{}, bad
		}
		p.Scopes = append(p.Scopes, mapped)
	}
	p.Kind = APIKey
	p.KeyID = parts[1]
	return p, nil
}

func (s KeyService) List(ctx context.Context, p Principal, cursor string, limit int) (KeyPage, error) {
	page := KeyPage{Items: []Key{}}
	if err := s.validate(); err != nil {
		return page, err
	}
	if err := p.RequireUser("key:manage"); err != nil {
		return page, err
	}
	if limit < 1 || limit > 100 || cursor != "" && !ids.Valid(cursor) {
		return page, fault.New(fault.Invalid, "Invalid pagination")
	}
	err := s.DB.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		if err := s.userTx(ctx, tx, p); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT id,fingerprint,label,scopes,route_ids,expires_at,CASE WHEN revoked_at IS NOT NULL THEN 'REVOKED' WHEN expires_at<=clock_timestamp() THEN 'EXPIRED' ELSE 'ACTIVE' END FROM p11.api_keys WHERE user_id=$1 AND id>$2 ORDER BY id LIMIT $3`, p.UserID, cursor, limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var k Key
			if err = rows.Scan(&k.ID, &k.Fingerprint, &k.Label, &k.Scopes, &k.RouteIDs, &k.ExpiresAt, &k.State); err != nil {
				return err
			}
			page.Items = append(page.Items, k)
		}
		return rows.Err()
	})
	if err != nil {
		return page, err
	}
	if len(page.Items) > limit {
		next := page.Items[limit-1].ID
		page.NextCursor = &next
		page.Items = page.Items[:limit]
	}
	return page, nil
}

func (s KeyService) Revoke(ctx context.Context, p Principal, id, idempotencyKey string) error {
	if err := s.validate(); err != nil {
		return err
	}
	if !ids.Valid(id) {
		return fault.New(fault.Invalid, "Invalid key identifier")
	}
	if err := p.RequireUser("key:manage"); err != nil {
		return err
	}
	_, err := (outbox.Idempotency{DB: s.DB}).DoAuthorized(ctx, outbox.Command{Principal: p.UserID, PrincipalVersion: p.Version, Operation: "revokeApiKey", Resource: id, Key: idempotencyKey, Request: []byte(id)}, func(tx pgx.Tx) error { return s.userTx(ctx, tx, p) }, func(tx pgx.Tx) (outbox.StoredResult, error) {
		if err := s.userTx(ctx, tx, p); err != nil {
			return outbox.StoredResult{}, err
		}
		tag, err := tx.Exec(ctx, `UPDATE p11.api_keys SET revoked_at=coalesce(revoked_at,clock_timestamp()) WHERE id=$1 AND user_id=$2`, id, p.UserID)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		if tag.RowsAffected() != 1 {
			return outbox.StoredResult{}, fault.New(fault.NotFound, "Key not found")
		}
		if err = audit(ctx, tx, p.UserID, "API_KEY_REVOKED", id); err != nil {
			return outbox.StoredResult{}, err
		}
		return outbox.StoredResult{Reference: id, Body: json.RawMessage(`{"accepted":true}`), HTTPStatus: 200}, nil
	})
	return err
}
