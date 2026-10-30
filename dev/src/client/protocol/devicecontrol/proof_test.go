package devicecontrol_test

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"p11.local/compute/client/protocol/devicecontrol"
	"strings"
	"testing"
	"time"
)

func key() ed25519.PrivateKey {
	seed, _ := hex.DecodeString("9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60")
	return ed25519.NewKeyFromSeed(seed)
}
func binding() devicecontrol.Binding {
	return devicecontrol.Binding{Method: "POST", Authority: "127.0.0.1:8787", URI: "/v1/p11/devices/dev_test/heartbeats", IdempotencyKey: "original_intent", ContentType: "application/json"}
}
func TestDeviceProofGoldenMessageAndRFC8032KeyVector(t *testing.T) {
	k := key()
	if hex.EncodeToString(k.Public().(ed25519.PublicKey)) != "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a" || hex.EncodeToString(ed25519.Sign(k, nil)) != "e5564300c360ac729086e2cc806e828a84877f1eb8e5d974d873e065224901555fb8821590a33bacc61e39701cf9b46bd25bf5f0595bbe24655141438e7a100b" {
		t.Fatal("RFC8032 vector mismatch")
	}
	p := devicecontrol.Proof{DeviceID: "dev_test", Timestamp: "1793325000", Nonce: "AAAAAAAAAAAAAAAAAAAAAA"}
	m, err := devicecontrol.Message(p, binding(), nil)
	want := "P11_DEVICE_ED25519_1\ndev_test\n1793325000\nAAAAAAAAAAAAAAAAAAAAAA\nPOST\n127.0.0.1:8787\n/v1/p11/devices/dev_test/heartbeats\noriginal_intent\n\napplication/json\ne3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855\n"
	if err != nil || string(m) != want {
		t.Fatal("independent byte-format vector drift", err, string(m))
	}
}
func TestDeviceProofBindsEveryRoutingAndCommandField(t *testing.T) {
	b := binding()
	body := []byte(`{"sequence":"1"}`)
	now := time.Now()
	token, err := devicecontrol.Sign(key(), "dev_test", b, body, now)
	if err != nil {
		t.Fatal(err)
	}
	p, err := devicecontrol.Parse(token, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"original", "body", "method", "authority", "uri", "query", "key", "etag", "media", "device", "time", "nonce"} {
		t.Run(name, func(t *testing.T) {
			v := b
			proof := p
			raw := body
			switch name {
			case "body":
				raw = []byte(`{"sequence":"2"}`)
			case "method":
				v.Method = "PATCH"
			case "authority":
				v.Authority = "127.0.0.1:9999"
			case "uri":
				v.URI = "/v1/p11/devices/dev_other/heartbeats"
			case "query":
				v.URI += "?limit=1"
			case "key":
				v.IdempotencyKey = "other_intent"
			case "etag":
				v.IfMatch = `"2"`
			case "media":
				v.ContentType = "application/json; charset=utf-8"
			case "device":
				proof.DeviceID = "dev_other"
			case "time":
				proof.Timestamp = "1793325001"
			case "nonce":
				proof.Nonce = "BBBBBBBBBBBBBBBBBBBBBA"
			}
			m, e := devicecontrol.Message(proof, v, raw)
			if e != nil {
				t.Fatal(e)
			}
			valid := ed25519.Verify(key().Public().(ed25519.PublicKey), m, p.Signature)
			if valid != (name == "original") {
				t.Fatal("unsigned field", name)
			}
		})
	}
}
func TestDeviceProofRejectsExpiredAmbiguousAndUnboundedInputs(t *testing.T) {
	now := time.Now()
	token, _ := devicecontrol.Sign(key(), "dev_test", binding(), nil, now)
	if _, e := devicecontrol.Parse(token, now.Add(61*time.Second)); e == nil {
		t.Fatal("expired accepted")
	}
	parts := strings.Split(token, ".")
	for i, value := range []string{"wrong", "bad/id", "01", "AA=", "AA"} {
		v := append([]string{}, parts...)
		v[i] = value
		if _, e := devicecontrol.Parse(strings.Join(v, "."), now); e == nil {
			t.Fatal("noncanonical proof accepted", i)
		}
	}
	pub := base64.RawURLEncoding.EncodeToString(key().Public().(ed25519.PublicKey))
	for _, v := range []string{pub + "=", "", pub[:42], strings.Repeat("A", 44)} {
		if _, e := devicecontrol.PublicKey(v); e == nil {
			t.Fatal("invalid key accepted")
		}
	}
	p, _ := devicecontrol.Parse(token, now)
	for _, name := range []string{"header", "authority", "encoded", "absolute", "double", "fragment", "oversize"} {
		t.Run(name, func(t *testing.T) {
			b := binding()
			var raw []byte
			switch name {
			case "header":
				b.IdempotencyKey = "x\ny"
			case "authority":
				b.Authority = "user@localhost"
			case "encoded":
				b.URI = "/v1/p11/%64evices"
			case "absolute":
				b.URI = "https://elsewhere/v1/p11/devices"
			case "double":
				b.URI = "/v1/p11//devices"
			case "fragment":
				b.URI = "/v1/p11/devices#other"
			case "oversize":
				raw = make([]byte, 65537)
			}
			if _, e := devicecontrol.Message(p, b, raw); e == nil {
				t.Fatal("invalid signature field accepted")
			}
		})
	}
}
func TestDeviceNativeClientRecoversFrozenCommandWithFreshProof(t *testing.T) {
	var calls int
	var firstBody, firstProof string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		raw, _ := io.ReadAll(r.Body)
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		p, e := devicecontrol.Parse(token, time.Now())
		if e != nil {
			t.Error(e)
			return
		}
		m, e := devicecontrol.Message(p, devicecontrol.Binding{Method: r.Method, Authority: r.Host, URI: r.RequestURI, IdempotencyKey: r.Header.Get("Idempotency-Key"), ContentType: r.Header.Get("Content-Type")}, raw)
		if e != nil || !ed25519.Verify(key().Public().(ed25519.PublicKey), m, p.Signature) {
			t.Error("invalid client proof")
			return
		}
		if calls == 1 {
			firstBody = string(raw)
			firstProof = token
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
			return
		}
		if string(raw) != firstBody || token == firstProof || r.Header.Get("Idempotency-Key") != "original_intent" {
			t.Error("client changed recovery intent or reused proof")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Idempotency-Replayed", "true")
		w.WriteHeader(200)
		io.WriteString(w, `{"status":"RECORDED"}`)
	}))
	defer s.Close()
	c, e := devicecontrol.NewClient(s.URL, "dev_test", key())
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	body := []byte(`{"sequence":"1"}`)
	prepared, e := c.Prepare("POST", "/v1/p11/devices/dev_test/heartbeats", body, "original_intent", "")
	if e != nil {
		t.Fatal(e)
	}
	clear(body)
	_, e = c.Send(context.Background(), prepared)
	var failure *devicecontrol.TransportError
	if !errors.As(e, &failure) || !failure.Unknown || calls != 1 {
		t.Fatal("lost write response was retried or treated definite", e, calls)
	}
	r, e := c.Send(context.Background(), prepared)
	if e != nil || r.Status != 200 || !r.Replayed || calls != 2 {
		t.Fatal("original prepared command recovery failed", e, r, calls)
	}
}
func TestDeviceNativeClientRejectsRedirectsAndUnsafeBases(t *testing.T) {
	for _, base := range []string{"http://example.com", "https://user@example.com", "https://example.com/path", "https://example.com?x=1"} {
		if _, e := devicecontrol.NewClient(base, "dev_test", key()); e == nil {
			t.Fatal("unsafe base accepted")
		}
	}
	var leaks int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaks++ }))
	defer target.Close()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer s.Close()
	c, e := devicecontrol.NewClient(s.URL, "dev_test", key())
	if e != nil {
		t.Fatal(e)
	}
	p, e := c.Prepare("GET", "/v1/p11/devices/dev_test", nil, "", "")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = c.Send(context.Background(), p); e == nil || leaks != 0 {
		t.Fatal("redirect forwarded proof", e, leaks)
	}
	c.Close()
	if _, e = c.Send(context.Background(), p); e == nil {
		t.Fatal("closed identity reused")
	}
}
