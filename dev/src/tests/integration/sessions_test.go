package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/trade"
	"p11.local/compute/shared/clock"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type loginProvider struct {
	name, subject string
	err           error
	calls         atomic.Int32
}

func (p *loginProvider) Name() string { return p.name }
func (p *loginProvider) AuthorizationURL(state string) (string, error) {
	return "https://identity.invalid/authorize?state=" + state, nil
}
func (p *loginProvider) Exchange(context.Context, string) (string, error) {
	p.calls.Add(1)
	return p.subject, p.err
}
func sessionService(t *testing.T) (identity.Sessions, *loginProvider) {
	t.Helper()
	p := &loginProvider{name: ids.New("SYNTHETIC_AUTH"), subject: ids.New("subject")}
	return identity.Sessions{DB: db, Provider: p, PepperVersion: "auth_test_v1", Peppers: map[string][]byte{"auth_test_v1": []byte(strings.Repeat("s", 32))},
		IdentityPepper: []byte(strings.Repeat("i", 32)), TTL: time.Hour}, p
}
func authChallenge(t *testing.T, s identity.Sessions, client string) (identity.Challenge, identity.SessionInput) {
	t.Helper()
	verifier := strings.Repeat("v", 43)
	hash := sha256.Sum256([]byte(verifier))
	c, err := s.CreateChallenge(ctx, identity.ChallengeInput{ClientType: client, CodeChallenge: base64.RawURLEncoding.EncodeToString(hash[:])})
	if err != nil {
		t.Fatal(err)
	}
	in := identity.SessionInput{ChallengeID: c.ID, AuthCode: ids.New("code"), Verifier: verifier}
	if client == "WEB" {
		in.WebState = c.ID
	}
	return c, in
}
func loginSession(t *testing.T, s identity.Sessions, client string) identity.IssuedSession {
	t.Helper()
	c, in := authChallenge(t, s, client)
	issued, err := s.Create(ctx, in, c.BrowserSecret)
	if err != nil {
		t.Fatal(err)
	}
	return issued
}
func TestAuthorizationChallengeIsSingleUseAndStoresNoOriginalCredentials(t *testing.T) {
	requireDB(t)
	s, provider := sessionService(t)
	c, in := authChallenge(t, s, "MINIAPP")
	var successes atomic.Int32
	var wg sync.WaitGroup
	results := make(chan identity.IssuedSession, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			issued, err := s.Create(ctx, in, "")
			if err == nil {
				successes.Add(1)
				results <- issued
			} else if fault.Public(err).Code != fault.Conflict {
				t.Errorf("unexpected challenge result %s", fault.Public(err).Code)
			}
		}()
	}
	wg.Wait()
	close(results)
	if successes.Load() != 1 || provider.calls.Load() != 1 {
		t.Fatal("challenge exchanged or issued more than once")
	}
	issued := <-results
	credential, err := s.Authenticate(ctx, issued.Secret)
	if err != nil || credential.Principal.SessionID != issued.View.ID || credential.ClientType != "MINIAPP" {
		t.Fatal("issued credential not authenticated", err)
	}
	profile, err := s.Profile(ctx, credential.Principal)
	if err != nil || profile.IdentityState != "UNVERIFIED" || len(profile.Roles) != 1 || profile.Roles[0] != "BUYER" {
		t.Fatal("registration granted unexpected identity or role", err)
	}
	var state, digest, attempt string
	if err = db.Pool.QueryRow(ctx, "SELECT a.state,s.token_digest,a.code_digest FROM p11.auth_attempts a JOIN p11.auth_sessions s ON s.id=a.session_id WHERE a.id=$1", c.ID).Scan(&state, &digest, &attempt); err != nil {
		t.Fatal(err)
	}
	if state != "SUCCEEDED" || len(digest) != 64 || len(attempt) != 64 || strings.Contains(digest, issued.Secret) || strings.Contains(attempt, in.AuthCode) {
		t.Fatal("original credential persisted")
	}
	var facts string
	if err = db.Pool.QueryRow(ctx, "SELECT string_agg(safe_facts::text,' ') FROM p11.audit_events WHERE actor_id=$1", profile.UserID).Scan(&facts); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{issued.Secret, in.AuthCode, in.Verifier, provider.subject} {
		if strings.Contains(facts, secret) {
			t.Fatal("credential or external subject exposed in audit")
		}
	}
	_, reuse := authChallenge(t, s, "MINIAPP")
	reuse.AuthCode = in.AuthCode
	_, err = s.Create(ctx, reuse, "")
	assertCode(t, err, fault.Conflict)
	if provider.calls.Load() != 1 {
		t.Fatal("consumed authorization code exchanged again")
	}
}
func TestBrowserAuthorizationBindingAndUnknownExchangeFailClosed(t *testing.T) {
	requireDB(t)
	s, provider := sessionService(t)
	c, in := authChallenge(t, s, "WEB")
	_, err := s.Create(ctx, in, "wrong-browser-secret")
	assertCode(t, err, fault.Unauthenticated)
	in.WebState = ids.New("other")
	_, err = s.Create(ctx, in, c.BrowserSecret)
	assertCode(t, err, fault.Unauthenticated)
	if provider.calls.Load() != 0 {
		t.Fatal("invalid browser binding reached provider")
	}
	in.WebState = c.ID
	provider.err = fault.New(fault.Unavailable, "Synthetic transport outcome unknown")
	_, err = s.Create(ctx, in, c.BrowserSecret)
	assertCode(t, err, fault.Unavailable)
	var state string
	if err = db.Pool.QueryRow(ctx, "SELECT state FROM p11.auth_attempts WHERE id=$1", c.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "UNKNOWN" {
		t.Fatal("unknown exchange was not retained")
	}
	var sessions int
	if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM p11.auth_sessions WHERE id=(SELECT session_id FROM p11.auth_attempts WHERE id=$1)", c.ID).Scan(&sessions); err != nil || sessions != 0 {
		t.Fatal("unknown exchange issued session")
	}
	_, err = s.Create(ctx, in, c.BrowserSecret)
	assertCode(t, err, fault.Conflict)
	if provider.calls.Load() != 1 {
		t.Fatal("unknown authorization was blindly retried")
	}
}
func TestSessionVersionsExpiryAndRevocationApplyBeforeTradeReplay(t *testing.T) {
	requireDB(t)
	s, _ := sessionService(t)
	first := loginSession(t, s, "MINIAPP")
	old, err := s.Authenticate(ctx, first.Secret)
	if err != nil {
		t.Fatal(err)
	}
	second := loginSession(t, s, "MINIAPP")
	_, err = s.Authenticate(ctx, first.Secret)
	assertCode(t, err, fault.Unauthenticated)
	current, err := s.Authenticate(ctx, second.Secret)
	if err != nil {
		t.Fatal(err)
	}
	f := offer(t, "GPU_RENTAL", ids.New("region"))
	_, err = (trade.Service{DB: db}).CreateQuote(ctx, old.Principal, f.input(), "stale-session-quote")
	assertCode(t, err, fault.Unauthenticated)
	_, err = (trade.Service{DB: db}).CreateQuote(ctx, current.Principal, f.input(), "current-session-quote")
	if err != nil {
		t.Fatal(err)
	}
	// Preserve user.version to prove the credential itself is checked in the
	// business transaction, even after the HTTP adapter has authenticated it.
	exec(t, "UPDATE p11.auth_sessions SET revoked_at=clock_timestamp() WHERE id=$1", second.View.ID)
	_, err = (trade.Service{DB: db}).CreateQuote(ctx, current.Principal, f.input(), "current-session-quote")
	assertCode(t, err, fault.Unauthenticated)
	third := loginSession(t, s, "WEB")
	active, err := s.Authenticate(ctx, third.Secret)
	if err != nil || active.CSRF == "" {
		t.Fatal("web credential missing", err)
	}
	ack, err := s.Revoke(ctx, active.Principal)
	if err != nil || ack.Status != "REVOKED" {
		t.Fatal("logout failed", err)
	}
	_, err = s.Authenticate(ctx, third.Secret)
	assertCode(t, err, fault.Unauthenticated)
	fourth := loginSession(t, s, "MINIAPP")
	exec(t, "UPDATE p11.auth_sessions SET created_at=clock_timestamp()-interval '2 hours',expires_at=clock_timestamp()-interval '1 hour' WHERE id=$1", fourth.View.ID)
	_, err = s.Authenticate(ctx, fourth.Secret)
	assertCode(t, err, fault.Unauthenticated)
}
func TestRoleRevocationBlocksKeyReplayAndLoginDoesNotRestoreRevokedRoles(t *testing.T) {
	requireDB(t)
	s, _ := sessionService(t)
	issued := loginSession(t, s, "MINIAPP")
	current, err := s.Authenticate(ctx, issued.Secret)
	if err != nil {
		t.Fatal(err)
	}
	keys := identity.KeyService{DB: db, Clock: clock.System{}, PepperVersion: "api_test", Peppers: map[string][]byte{"api_test": []byte(strings.Repeat("k", 32))}, MaxTTL: 24 * time.Hour}
	input := identity.KeyInput{Label: "Synthetic bounded key", Scopes: []string{"MARKET_READ"}, ExpiresAt: time.Now().Add(time.Hour)}
	created, err := keys.Create(ctx, current.Principal, input, "role-sensitive-create")
	if err != nil || created.SecretOnce == "" {
		t.Fatal("key creation failed", err)
	}
	exec(t, "UPDATE p11.role_grants SET revoked_at=clock_timestamp() WHERE user_id=$1 AND role_code='BUYER'", current.Principal.UserID)
	_, err = keys.Create(ctx, current.Principal, input, "role-sensitive-create")
	assertCode(t, err, fault.Forbidden)
	relogin := loginSession(t, s, "MINIAPP")
	credential, err := s.Authenticate(ctx, relogin.Secret)
	if err != nil || credential.Principal.UserID != current.Principal.UserID || len(credential.Principal.Roles) != 0 {
		t.Fatal("relogin restored revoked role", err)
	}
	exec(t, "UPDATE p11.users SET state='SUSPENDED' WHERE id=$1", credential.Principal.UserID)
	c, in := authChallenge(t, s, "MINIAPP")
	_, err = s.Create(ctx, in, c.BrowserSecret)
	assertCode(t, err, fault.Forbidden)
}
