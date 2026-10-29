package identity

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"p11.local/compute/internal/persistence"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"sort"
	"strings"
	"time"
)

type Profile struct {
	UserID        string   `json:"user_id"`
	Roles         []string `json:"roles"`
	Permissions   []string `json:"permission_scope_ids"`
	IdentityState string   `json:"identity_state"`
	Version       int64    `json:"version"`
}
type ChallengeInput struct {
	ClientType    string `json:"client_type"`
	CodeChallenge string `json:"code_challenge"`
}
type Challenge struct {
	ID               string    `json:"challenge_id"`
	ClientType       string    `json:"client_type"`
	ExpiresAt        time.Time `json:"expires_at"`
	AuthorizationURL string    `json:"authorization_url,omitempty"`
	BrowserSecret    string    `json:"-"`
}
type SessionInput struct {
	ChallengeID string `json:"challenge_id"`
	AuthCode    string `json:"auth_code"`
	Verifier    string `json:"code_verifier"`
	WebState    string `json:"web_state,omitempty"`
}
type SessionView struct {
	ID          string    `json:"session_id"`
	ClientType  string    `json:"client_type"`
	ExpiresAt   time.Time `json:"expires_at"`
	Profile     Profile   `json:"profile"`
	CSRF        string    `json:"csrf_token,omitempty"`
	AccessToken string    `json:"access_token,omitempty"`
}
type SessionCredential struct {
	Principal  Principal
	ClientType string
	ExpiresAt  time.Time
	CSRF       string
}
type IssuedSession struct {
	View   SessionView
	Secret string
}
type SessionAck struct {
	ID         string    `json:"id"`
	Version    int64     `json:"version"`
	Status     string    `json:"status"`
	RecordedAt time.Time `json:"recorded_at"`
}
type Sessions struct {
	DB             *persistence.DB
	Provider       LoginProvider
	PepperVersion  string
	Peppers        map[string][]byte
	IdentityPepper []byte
	TTL            time.Duration
}

func digestSecret(pepper []byte, purpose, value string) string {
	m := hmac.New(sha256.New, pepper)
	m.Write([]byte(purpose + "\x00" + value))
	return hex.EncodeToString(m.Sum(nil))
}
func randomSecret() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}
func (s Sessions) configured() error {
	if s.DB == nil || s.Provider == nil || len(s.Provider.Name()) < 1 || len(s.Provider.Name()) > 128 ||
		!ids.Valid(s.PepperVersion) || len(s.Peppers[s.PepperVersion]) < 32 || len(s.IdentityPepper) < 32 ||
		s.TTL < time.Minute || s.TTL > 24*time.Hour {
		return fault.New(fault.Unavailable, "Identity service not configured")
	}
	return nil
}
func clientTypeValid(t string) bool { return t == "WEB" || t == "MINIAPP" }
func (s Sessions) CreateChallenge(ctx context.Context, in ChallengeInput) (Challenge, error) {
	var c Challenge
	if err := s.configured(); err != nil {
		return c, err
	}
	decoded, err := base64.RawURLEncoding.DecodeString(in.CodeChallenge)
	if !clientTypeValid(in.ClientType) || err != nil || len(decoded) != 32 ||
		base64.RawURLEncoding.EncodeToString(decoded) != in.CodeChallenge {
		return c, fault.New(fault.Invalid, "Invalid authorization challenge")
	}
	c.ID, c.ClientType = ids.New("ach"), in.ClientType
	var binding any
	if c.ClientType == "WEB" {
		c.AuthorizationURL, err = s.Provider.AuthorizationURL(c.ID)
		if err != nil {
			return Challenge{}, err
		}
		c.BrowserSecret, err = randomSecret()
		if err != nil {
			return Challenge{}, err
		}
		binding = digestSecret(s.IdentityPepper, "P11_LOGIN_BROWSER_V1", c.ID+"."+c.BrowserSecret)
	}
	err = s.DB.Pool.QueryRow(ctx, "INSERT INTO p11.auth_attempts(id,provider,client_type,verifier_digest,browser_binding_digest,state,expires_at) VALUES($1,$2,$3,$4,$5,'PENDING',clock_timestamp()+interval '5 minutes') RETURNING expires_at",
		c.ID, s.Provider.Name(), c.ClientType, hex.EncodeToString(decoded), binding).Scan(&c.ExpiresAt)
	return c, err
}

func verifierDigest(v string) (string, error) {
	if len(v) < 43 || len(v) > 128 {
		return "", fault.New(fault.Invalid, "Invalid authorization verifier")
	}
	for _, r := range v {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-._~", r)) {
			return "", fault.New(fault.Invalid, "Invalid authorization verifier")
		}
	}
	h := sha256.Sum256([]byte(v))
	return hex.EncodeToString(h[:]), nil
}

func (s Sessions) Create(ctx context.Context, in SessionInput, browserSecret string) (IssuedSession, error) {
	var issued IssuedSession
	if err := s.configured(); err != nil {
		return issued, err
	}
	proof, err := verifierDigest(in.Verifier)
	if err != nil || !ids.Valid(in.ChallengeID) || !oauthCodeValid(in.AuthCode) {
		return issued, fault.New(fault.Invalid, "Invalid authorization confirmation")
	}
	var clientType string
	err = s.DB.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		var expected, provider, state string
		var binding *string
		var expires, now time.Time
		err := tx.QueryRow(ctx, "SELECT client_type,verifier_digest,browser_binding_digest,provider,state,expires_at,clock_timestamp() FROM p11.auth_attempts WHERE id=$1 FOR UPDATE", in.ChallengeID).
			Scan(&clientType, &expected, &binding, &provider, &state, &expires, &now)
		if err == pgx.ErrNoRows {
			return fault.New(fault.Unauthenticated, "Invalid authorization challenge")
		}
		if err != nil {
			return err
		}
		if provider != s.Provider.Name() || !expires.After(now) || subtle.ConstantTimeCompare([]byte(expected), []byte(proof)) != 1 {
			return fault.New(fault.Unauthenticated, "Invalid authorization challenge")
		}
		if clientType == "WEB" && (binding == nil || in.WebState != in.ChallengeID ||
			subtle.ConstantTimeCompare([]byte(*binding), []byte(digestSecret(s.IdentityPepper, "P11_LOGIN_BROWSER_V1", in.ChallengeID+"."+browserSecret))) != 1) ||
			clientType == "MINIAPP" && (in.WebState != "" || browserSecret != "") {
			return fault.New(fault.Unauthenticated, "Authorization client binding failed")
		}
		if state != "PENDING" {
			return fault.New(fault.Conflict, "Authorization already attempted; start fresh authorization")
		}
		_, err = tx.Exec(ctx, "UPDATE p11.auth_attempts SET state='EXCHANGING',code_digest=$2 WHERE id=$1", in.ChallengeID,
			digestSecret(s.IdentityPepper, "P11_AUTHORIZATION_CODE_V1", s.Provider.Name()+"\x00"+in.AuthCode))
		var pg *pgconn.PgError
		if err != nil && errors.As(err, &pg) && pg.Code == "23505" {
			return fault.New(fault.Conflict, "Authorization already attempted; start fresh authorization")
		}
		return err
	})
	if err != nil {
		return issued, err
	}
	subject, err := s.Provider.Exchange(ctx, in.AuthCode)
	if err != nil {
		state := "FAILED"
		if fault.Public(err).Code == fault.Unavailable {
			state = "UNKNOWN"
		}
		s.finishFailure(in.ChallengeID, state)
		return issued, err
	}
	// The provider adapter must authenticate the subject before this point.
	if subject == "" || len(subject) > 128 {
		s.finishFailure(in.ChallengeID, "FAILED")
		return issued, fault.New(fault.Unauthenticated, "Authorization verification failed")
	}
	random, err := randomSecret()
	if err != nil {
		s.finishFailure(in.ChallengeID, "FAILED")
		return issued, err
	}
	sessionID := ids.New("ses")
	secret := "p11s." + sessionID + "." + random
	err = s.DB.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		var state string
		var expires, now time.Time
		if err := tx.QueryRow(ctx, "SELECT state,expires_at,clock_timestamp() FROM p11.auth_attempts WHERE id=$1 FOR UPDATE", in.ChallengeID).Scan(&state, &expires, &now); err != nil {
			return err
		}
		if state != "EXCHANGING" || !expires.After(now) {
			return fault.New(fault.Conflict, "Authorization expired; start fresh authorization")
		}
		digest := digestSecret(s.IdentityPepper, "P11_EXTERNAL_SUBJECT_V1", s.Provider.Name()+"\x00"+subject)
		userID := ids.New("usr")
		tag, err := tx.Exec(ctx, "INSERT INTO p11.users(id,identity_provider,external_subject_digest,state) VALUES($1,$2,$3,'ACTIVE') ON CONFLICT(identity_provider,external_subject_digest) DO NOTHING", userID, s.Provider.Name(), digest)
		if err != nil {
			return err
		}
		created := tag.RowsAffected() == 1
		var version int64
		if err = tx.QueryRow(ctx, "SELECT id,state,version FROM p11.users WHERE identity_provider=$1 AND external_subject_digest=$2 FOR UPDATE", s.Provider.Name(), digest).Scan(&userID, &state, &version); err != nil {
			return err
		}
		if state != "ACTIVE" || version < 1 || version >= 2147483647 {
			return fault.New(fault.Forbidden, "Account cannot establish a session")
		}
		if created {
			// Registration grants BUYER only. Existing revoked roles stay revoked.
			if _, err = tx.Exec(ctx, "INSERT INTO p11.role_grants(id,user_id,role_code,scope_ref,granted_by) VALUES($1,$2,'BUYER','self',$2)", ids.New("grant"), userID); err != nil {
				return err
			}
		}
		version++
		if _, err = tx.Exec(ctx, "UPDATE p11.users SET version=$2 WHERE id=$1", userID, version); err != nil {
			return err
		}
		view := SessionView{ID: sessionID, ClientType: clientType, ExpiresAt: now.Add(s.TTL)}
		if _, err = tx.Exec(ctx, "INSERT INTO p11.auth_sessions(id,user_id,user_version,client_type,token_digest,pepper_version,created_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)",
			sessionID, userID, version, clientType, digestSecret(s.Peppers[s.PepperVersion], "P11_SESSION_V1", secret), s.PepperVersion, now, view.ExpiresAt); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "UPDATE p11.auth_attempts SET state='SUCCEEDED',session_id=$2 WHERE id=$1", in.ChallengeID, sessionID); err != nil {
			return err
		}
		p := Principal{UserID: userID, Version: version, Kind: UserSession, SessionID: sessionID}
		p, err = rolesTx(ctx, tx, p)
		if err != nil {
			return err
		}
		view.Profile = profileOf(p)
		if clientType == "WEB" {
			view.CSRF = digestSecret(s.Peppers[s.PepperVersion], "P11_CSRF_V1", secret)
		} else {
			view.AccessToken = secret
		}
		issued = IssuedSession{View: view, Secret: secret}
		return audit(ctx, tx, userID, "SESSION_ESTABLISHED", sessionID)
	})
	if err != nil {
		s.finishFailure(in.ChallengeID, "UNKNOWN")
		// No secret is returned if commit certainty was lost.
		return IssuedSession{}, err
	}
	return issued, nil
}

func (s Sessions) finishFailure(id, state string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _ = s.DB.Pool.Exec(ctx, "UPDATE p11.auth_attempts SET state=$2 WHERE id=$1 AND state='EXCHANGING'", id, state)
}

func (s Sessions) Authenticate(ctx context.Context, secret string) (SessionCredential, error) {
	var credential SessionCredential
	if err := s.configured(); err != nil {
		return credential, err
	}
	bad := fault.New(fault.Unauthenticated, "Invalid or expired credential")
	parts := strings.Split(secret, ".")
	if len(parts) != 3 || parts[0] != "p11s" || !ids.Valid(parts[1]) || len(secret) > 256 {
		return credential, bad
	}
	random, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(random) != 32 || base64.RawURLEncoding.EncodeToString(random) != parts[2] {
		return credential, bad
	}
	err = s.DB.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		p := Principal{Kind: UserSession, SessionID: parts[1]}
		var digest, pepperVersion, state string
		var version int64
		var now time.Time
		var revoked *time.Time
		if err := tx.QueryRow(ctx, "SELECT s.user_id,s.user_version,s.client_type,s.token_digest,s.pepper_version,s.expires_at,s.revoked_at,u.state,u.version,clock_timestamp() FROM p11.auth_sessions s JOIN p11.users u ON u.id=s.user_id WHERE s.id=$1 FOR SHARE OF u", parts[1]).
			Scan(&p.UserID, &p.Version, &credential.ClientType, &digest, &pepperVersion, &credential.ExpiresAt, &revoked, &state, &version, &now); err == pgx.ErrNoRows {
			return bad
		} else if err != nil {
			return err
		}
		pepper := s.Peppers[pepperVersion]
		if len(pepper) < 32 || state != "ACTIVE" || version != p.Version || revoked != nil || !credential.ExpiresAt.After(now) ||
			subtle.ConstantTimeCompare([]byte(digest), []byte(digestSecret(pepper, "P11_SESSION_V1", secret))) != 1 {
			return bad
		}
		if err := RequireCurrentTx(ctx, tx, p); err != nil {
			return err
		}
		var err error
		credential.Principal, err = rolesTx(ctx, tx, p)
		if credential.ClientType == "WEB" {
			credential.CSRF = digestSecret(pepper, "P11_CSRF_V1", secret)
		}
		return err
	})
	return credential, err
}

var knownRoles = map[string]bool{"BUYER": true, "SELLER": true, "SUPPORT": true, "OPERATOR": true, "FINANCE": true, "AUDITOR": true}

func rolesTx(ctx context.Context, tx pgx.Tx, p Principal) (Principal, error) {
	rows, err := tx.Query(ctx, "SELECT role_code FROM p11.role_grants WHERE user_id=$1 AND scope_ref='self' AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>clock_timestamp()) ORDER BY role_code FOR SHARE", p.UserID)
	if err != nil {
		return Principal{}, err
	}
	defer rows.Close()
	p.Roles = []string{}
	for rows.Next() {
		var role string
		if err = rows.Scan(&role); err != nil {
			return Principal{}, err
		}
		if knownRoles[role] {
			p.Roles = append(p.Roles, role)
		}
	}
	if err = rows.Err(); err != nil {
		return Principal{}, err
	}
	p.Scopes = []string{"identity:read", "session:manage", "market:read"}
	for _, role := range p.Roles {
		switch role {
		case "BUYER":
			p.Scopes = append(p.Scopes, "key:manage", "quote:create", "quote:read", "order:create", "order:read", "seller:apply", "seller:read", "seller:manage", "identity:consent", "identity:delete")
		case "SELLER":
			p.Scopes = append(p.Scopes, "order:read", "seller:read", "seller:manage", "listing:write", "identity:consent", "identity:delete")
		}
	}
	sort.Strings(p.Scopes)
	p.Scopes = uniqueStrings(p.Scopes)
	return p, nil
}
func uniqueStrings(in []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if len(out) == 0 || out[len(out)-1] != v {
			out = append(out, v)
		}
	}
	return out
}
func profileOf(p Principal) Profile {
	permissions := make([]string, 0, len(p.Scopes))
	for _, scope := range p.Scopes {
		permissions = append(permissions, "perm_"+strings.ReplaceAll(scope, ":", "_"))
	}
	return Profile{UserID: p.UserID, Version: p.Version, Roles: append([]string{}, p.Roles...), Permissions: permissions, IdentityState: "UNVERIFIED"}
}
func (s Sessions) Profile(ctx context.Context, p Principal) (Profile, error) {
	var profile Profile
	if err := s.configured(); err != nil {
		return profile, err
	}
	err := s.DB.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		if err := p.RequireUser("identity:read"); err != nil {
			return err
		}
		if err := RequireCurrentTx(ctx, tx, p); err != nil {
			return err
		}
		current, err := rolesTx(ctx, tx, p)
		if err != nil {
			return err
		}
		profile = profileOf(current)
		return nil
	})
	return profile, err
}
func (s Sessions) Revoke(ctx context.Context, p Principal) (SessionAck, error) {
	var ack SessionAck
	if err := s.configured(); err != nil {
		return ack, err
	}
	if err := p.RequireUser("session:manage"); err != nil {
		return ack, err
	}
	if !ids.Valid(p.SessionID) {
		return ack, fault.New(fault.Unauthenticated, "Current session required")
	}
	err := s.DB.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		var version int64
		// Lock user before session everywhere, including logout and login.
		if err := tx.QueryRow(ctx, "SELECT version FROM p11.users WHERE id=$1 FOR UPDATE", p.UserID).Scan(&version); err != nil {
			return err
		}
		if err := RequireCurrentTx(ctx, tx, p); err != nil {
			return err
		}
		if version >= 2147483647 {
			return fault.New(fault.Unavailable, "Account version requires recovery")
		}
		if _, err := tx.Exec(ctx, "UPDATE p11.auth_sessions SET revoked_at=clock_timestamp() WHERE id=$1 AND user_id=$2", p.SessionID, p.UserID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE p11.users SET version=version+1 WHERE id=$1", p.UserID); err != nil {
			return err
		}
		ack = SessionAck{ID: p.SessionID, Version: version + 1, Status: "REVOKED"}
		if err := tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&ack.RecordedAt); err != nil {
			return err
		}
		return audit(ctx, tx, p.UserID, "SESSION_REVOKED", p.SessionID)
	})
	return ack, err
}
