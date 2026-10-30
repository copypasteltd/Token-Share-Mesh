package api_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	controlhttp "p11.local/compute/api/internal/http"
	"p11.local/compute/internal/dispatch/device"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/testsupport"
	"p11.local/compute/shared/ids"
)

func TestNativeNonceAuthorizationIsExplicitSingleUseAndNotWebPKCE(t *testing.T) {
	h := newHarness(t, 1000, 1000)
	web := identity.ChallengeInput{ClientType: "WEB", ProofProfile: "ALIPAY_NATIVE_NONCE_1"}
	expectError(t, h.request(t, "POST", "/auth/challenges", web, http.Header{"Origin": {h.origin}}), 400, "INVALID_REQUEST")
	native := identity.ChallengeInput{ClientType: "MINIAPP", ProofProfile: "ALIPAY_NATIVE_NONCE_1"}
	expectError(t, h.request(t, "POST", "/auth/challenges", json.RawMessage(`{"client_type":"MINIAPP","proof_profile":"ALIPAY_NATIVE_NONCE_1","code_challenge":null}`), nil), 400, "INVALID_REQUEST")
	r := h.request(t, "POST", "/auth/challenges", native, nil)
	c := decode[identity.Challenge](t, r, 201)
	if len(c.NativeVerifier) != 43 || c.ProofProfile != "ALIPAY_NATIVE_NONCE_1" || c.AuthorizationURL != "" || r.header.Get("Cache-Control") != "no-store" {
		t.Fatal("native proof must be explicit, bounded, non-cacheable and distinct from browser PKCE")
	}
	var record []byte
	var digest string
	if err := db.Pool.QueryRow(ctx, "SELECT row_to_json(a),verifier_digest FROM p11.auth_attempts a WHERE id=$1", c.ID).Scan(&record, &digest); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(c.NativeVerifier))
	if digest != hex.EncodeToString(hash[:]) || bytes.Contains(record, []byte(c.NativeVerifier)) {
		t.Fatal("only the proof digest may be persisted")
	}
	in := identity.SessionInput{ChallengeID: c.ID, Verifier: strings.Repeat("x", 43), AuthCode: ids.New("SYNTHETIC_NATIVE")}
	expectError(t, h.request(t, "POST", "/auth/sessions", in, nil), 401, "UNAUTHENTICATED")
	in.Verifier = c.NativeVerifier
	session := decode[identity.SessionView](t, h.request(t, "POST", "/auth/sessions", in, nil), 201)
	if session.ClientType != "MINIAPP" || session.AccessToken == "" || session.CSRF != "" {
		t.Fatal("native proof must issue only native session transport")
	}
	if h.request(t, "POST", "/auth/sessions", in, nil).status == 201 {
		t.Fatal("native proof must not issue another session on replay")
	}
	current := h.request(t, "GET", "/auth/session", nil, bearer(session.AccessToken))
	view := decode[identity.SessionView](t, current, 200)
	if view.AccessToken != "" || bytes.Contains(current.body, []byte(c.NativeVerifier)) {
		t.Fatal("proof and bearer must not be redisclosed by session reads")
	}
}

// Only the browser-test binary installs this synthetic redirect. The RSA2 exchange
// still runs through the real Alipay adapter's independent signed-response fixture.
type syntheticBrowserProvider struct {
	identity.LoginProvider
	base    string
	subject string
}

func (s syntheticBrowserProvider) AuthorizationURL(state string) (string, error) {
	return s.base + "/fixture/authorize?state=" + url.QueryEscape(state), nil
}
func (s syntheticBrowserProvider) Exchange(ctx context.Context, code string) (string, error) {
	subject, err := s.LoginProvider.Exchange(ctx, code)
	if err == nil && s.subject != "" {
		return s.subject, nil
	}
	return subject, err
}

func TestFrontendBrowserJourneysUseNativeControlAndOneOriginalOrder(t *testing.T) {
	if os.Getenv("P11_FRONTEND_BROWSER") != "1" {
		t.Skip("R006 requires compiled frontends and an explicit native browser run")
	}
	root := os.Getenv("P11_PROJECT_ROOT")
	if root == "" || db == nil {
		t.Fatal("native project and temporary database required")
	}
	for _, target := range []string{"web", "miniapp"} {
		t.Run(target, func(t *testing.T) {
			gpu := testsupport.SeedOffer(t, ctx, db, "GPU_RENTAL", "CN")
			phone := testsupport.SeedOffer(t, ctx, db, "E2E", "CN")
			keepSyntheticDevicesOnline(t, gpu.Device, phone.Device)
			code := ids.New("SYNTHETIC_BROWSER")
			providerRound := (os.Getenv("P11_EVIDENCE_ROUND") == "R009" || os.Getenv("P11_EVIDENCE_ROUND") == "R010" || os.Getenv("P11_EVIDENCE_ROUND") == "R011" || os.Getenv("P11_EVIDENCE_ROUND") == "R012" || os.Getenv("P11_EVIDENCE_ROUND") == "R013" || os.Getenv("P11_EVIDENCE_ROUND") == "R014" || os.Getenv("P11_EVIDENCE_ROUND") == "R015" || os.Getenv("P11_EVIDENCE_ROUND") == "R016" || os.Getenv("P11_EVIDENCE_ROUND") == "R017" || os.Getenv("P11_EVIDENCE_ROUND") == "R018" || os.Getenv("P11_EVIDENCE_ROUND") == "R019" || os.Getenv("P11_EVIDENCE_ROUND") == "R020" || os.Getenv("P11_EVIDENCE_ROUND") == "R021")
			var origin string
			h := newHarnessConfigured(t, 10000, 1000, func(c *controlhttp.Config) {
				origin = "http://" + c.AllowedHosts[0]
				c.BrowserOrigin = origin
				provider := syntheticBrowserProvider{LoginProvider: c.Sessions.Provider, base: origin}
				if providerRound {
					provider.subject = ids.New("SYNTHETIC_PROVIDER_SUBJECT")
					c.Devices = device.Service{DB: db, PepperVersion: "synthetic_ui_pair_1", Peppers: map[string][]byte{"synthetic_ui_pair_1": bytes.Repeat([]byte{'p'}, 32)}}
				}
				c.Sessions.Provider = provider
			}, func(control http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if strings.HasPrefix(r.URL.Path, "/v1/p11/") {
						control.ServeHTTP(w, r)
						return
					}
					if r.URL.Path == "/fixture/authorize" {
						http.Redirect(w, r, "/"+target+"/?auth_code="+url.QueryEscape(code)+"&state="+url.QueryEscape(r.URL.Query().Get("state")), http.StatusFound)
						return
					}
					prefix := "/" + target + "/"
					if r.Method != "GET" || !strings.HasPrefix(r.URL.Path, prefix) || strings.Contains(r.URL.Path, "..") {
						http.NotFound(w, r)
						return
					}
					name := strings.TrimPrefix(r.URL.Path, prefix)
					if name == "" {
						name = "index.html"
					}
					folder := target
					if target == "miniapp" {
						folder = "miniapp-h5"
					}
					filename := filepath.Join(root, "runtime/frontend/builds", folder, filepath.FromSlash(name))
					data, err := os.ReadFile(filename)
					if err != nil {
						http.NotFound(w, r)
						return
					}
					w.Header().Set("Cache-Control", "no-store")
					w.Header().Set("Referrer-Policy", "no-referrer")
					w.Header().Set("Content-Type", mime.TypeByExtension(filepath.Ext(filename)))
					if name == "index.html" {
						notice := "<aside style=\"background:#000;color:#fff;padding:6px 12px;font:12px sans-serif\">本地联调 · 合成卖家/授权/心跳 · 无真实支付或设备执行</aside>"
						data = bytes.Replace(data, []byte("<body>"), []byte("<body>"+notice), 1)
					}
					_, _ = w.Write(data)
				})
			})
			if providerRound {
				// Explicit disposable-database approval; production commands cannot install it.
				approved, _ := h.login(t, "MINIAPP")
				grantDeviceSeller(t, approved)
			}
			command := exec.CommandContext(ctx, "node", filepath.Join(root, "dev/tools/frontend/browser.mjs"))
			command.Env = append(os.Environ(), "P11_UI_BASE="+h.server.URL, "P11_UI_TARGET="+target, "P11_UI_GPU="+gpu.ID, "P11_UI_PHONE="+phone.ID)
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("browser journey failed: %v\n%s", err, output)
			}
			var receipt struct {
				Passed         bool   `json:"passed"`
				Buyer          string `json:"buyer_id"`
				Order          string `json:"order_id"`
				BrowserClosed  bool   `json:"browser_closed"`
				RemainingTabs  int    `json:"remaining_owned_tabs"`
				ProviderDevice string `json:"provider_device_id"`
			}
			if err = json.Unmarshal(bytes.TrimSpace(output), &receipt); err != nil || !receipt.Passed || !receipt.BrowserClosed || receipt.RemainingTabs != 0 {
				t.Fatalf("browser receipt invalid: %s", output)
			}
			var orders, events int
			if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM p11.orders WHERE buyer_id=$1", receipt.Buyer).Scan(&orders); err != nil || orders != 1 {
				t.Fatal("lost browser receipt must create exactly one buyer order", err, orders)
			}
			if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM p11.outbox_events WHERE aggregate_id=$1 AND event_type='OrderCreated'", receipt.Order).Scan(&events); err != nil || events != 1 {
				t.Fatal("original order must have exactly one creation event", err, events)
			}
			if providerRound {
				var state string
				var accepting, sharing bool
				if err = db.Pool.QueryRow(ctx, "SELECT state,(resource_policy->>'accepting_new_jobs')::boolean,(resource_policy->>'sharing_enabled')::boolean FROM p11.devices WHERE id=$1 AND seller_id=$2", receipt.ProviderDevice, receipt.Buyer).Scan(&state, &accepting, &sharing); err != nil || state != "REVOKED" || accepting || sharing {
					t.Fatal("UI/native provider did not finish revoked with new admission closed", err)
				}
				for _, action := range []string{"DeviceEnrolled", "DeviceResourcePolicyChanged", "DeviceDrainRequested", "DeviceRevoked"} {
					if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM p11.outbox_events WHERE aggregate_id=$1 AND event_type=$2", receipt.ProviderDevice, action).Scan(&events); err != nil || events != 1 {
						t.Fatal("provider original recovery duplicated or omitted event", action, err, events)
					}
				}
			}
			t.Logf("%s: actual browser/control/DB journey passed; synthetic OAuth and qualification only; one order and one outbox event", target)
		})
	}
}

// Only these explicitly synthetic fixture devices receive test heartbeats.
// Browser recording can exceed the real catalog's 45s freshness window; the
// runtime check remains unchanged and no future timestamps are installed.
func keepSyntheticDevicesOnline(t *testing.T, devices ...string) {
	t.Helper()
	heartbeatCtx, cancel := context.WithCancel(ctx)
	// Real device heartbeats own one device. A synthetic multi-row UPDATE can
	// lock its rows in heap order while a market transaction locks the same
	// devices in listing order. Keep each fixture heartbeat in its own commit;
	// never hold one device row while waiting for a second one.
	query := "UPDATE p11.devices SET last_seen_at=clock_timestamp() WHERE id=$1 AND client_release='SYNTHETIC_TEST'"
	heartbeat := func() error {
		for _, device := range devices {
			if _, err := db.Pool.Exec(heartbeatCtx, query, device); err != nil {
				return err
			}
		}
		return nil
	}
	if err := heartbeat(); err != nil {
		cancel()
		t.Fatal(err)
	}
	done := make(chan struct{})
	errors := make(chan error, 1)
	go func() {
		defer close(done)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				if err := heartbeat(); err != nil {
					if heartbeatCtx.Err() == nil {
						errors <- err
					}
					return
				}
			}
		}
	}()
	t.Cleanup(func() {
		cancel()
		<-done
		select {
		case err := <-errors:
			t.Error("synthetic device heartbeat failed", err)
		default:
		}
	})
}
