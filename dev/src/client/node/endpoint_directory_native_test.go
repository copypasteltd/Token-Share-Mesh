//go:build p11_sodium && cgo && (windows || linux)

package node

// Real endpoint-vault/native cipher composition definitions; not executed.
// The synthetic HTTP authority proves no real platform or device qualification.
import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"p11.local/compute/shared/endpointdirectory"
	"p11.local/compute/shared/strictjson"
	"sync"
	"testing"
	"time"
)

func TestEndpointDirectoryLostReplyReopensOriginalAEADIntentAndKey(t *testing.T) {
	var mu sync.Mutex
	var bodies [][]byte
	var keys, proofs []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/p11/endpoint-bundles" {
			t.Error("unexpected directory request")
			w.WriteHeader(400)
			return
		}
		raw, e := io.ReadAll(io.LimitReader(r.Body, 16385))
		if e != nil || len(raw) > 16384 {
			w.WriteHeader(400)
			return
		}
		var in endpointdirectory.Registration
		if strictjson.Decode(raw, &in, 16384) != nil {
			w.WriteHeader(400)
			return
		}
		mu.Lock()
		bodies = append(bodies, bytes.Clone(raw))
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		proofs = append(proofs, r.Header.Get("Authorization"))
		attempt := len(bodies)
		mu.Unlock()
		if attempt == 1 {
			drop(w)
			return
		}
		writeReply(w, 201, endpointdirectory.Record{Profile: endpointdirectory.Profile, Recipient: in.Bundle.Content.ID, Bundle: in.Bundle, BundleHash: in.Bundle.Hash(), State: "ACTIVE", IndependentTrustRequired: true, Replayed: true})
	}))
	defer server.Close()
	s := testStore(t, server.URL)
	if e := s.adopt(view(s)); e != nil {
		t.Fatal(e)
	}
	if e := s.save(); e != nil {
		t.Fatal(e)
	}
	if _, e := s.InitEndpointKeys(); e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	bundle, e := s.CreateLocalEndpoint(s.s.SellerID, s.s.DeviceID, "SELLER", now, now.Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.PublishEndpointDirectory(context.Background(), bundle.Hash()); e == nil || s.s.Pending == nil {
		t.Fatal("lost reply cleared original intent", e)
	}
	originalKey := s.s.Pending.Key
	s = reopen(t, s)
	result, e := s.RecoverEndpointDirectory(context.Background())
	if e != nil || !result.Replayed || result.BundleHash != bundle.Hash() || s.s.Pending != nil {
		t.Fatal(result, e)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 || !bytes.Equal(bodies[0], bodies[1]) || keys[0] != originalKey || keys[1] != originalKey || proofs[0] == proofs[1] {
		t.Fatal("original bytes/key or fresh proof nonce lost")
	}
	v, e := s.OpenEndpointKeys()
	if e != nil {
		t.Fatal(e)
	}
	public, e := v.Public()
	if e != nil || len(public) != 1 || public[0].Bundle.Hash() != bundle.Hash() {
		t.Fatal("recovery minted endpoint material", e)
	}
}
func TestEndpointDirectoryRotationPublishesCommittedOriginalPublicProof(t *testing.T) {
	var received endpointdirectory.Rotation
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 16385))
		if strictjson.Decode(raw, &received, 16384) != nil {
			w.WriteHeader(400)
			return
		}
		b := received.Transition.Next
		recipient := r.URL.Path[len("/v1/p11/endpoint-bundles/"):]
		recipient = recipient[:len(recipient)-len("/rotations")]
		writeReply(w, 201, endpointdirectory.Record{Profile: endpointdirectory.Profile, Recipient: b.Content.ID, Bundle: b, BundleHash: b.Hash(), State: "ACTIVE", Previous: recipient, TransitionHash: received.Transition.Hash(), Rotation: &received.Transition, IndependentTrustRequired: true})
	}))
	defer server.Close()
	s := testStore(t, server.URL)
	if e := s.adopt(view(s)); e != nil {
		t.Fatal(e)
	}
	if e := s.save(); e != nil {
		t.Fatal(e)
	}
	keys, e := s.InitEndpointKeys()
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	old, e := s.CreateLocalEndpoint(s.s.SellerID, s.s.DeviceID, "SELLER", now.Add(-time.Minute), now.Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	transition, e := keys.RotateLocal(old.Hash(), now, now.Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	v, e := s.PublishEndpointDirectory(context.Background(), transition.Next.Hash())
	if e != nil || v.TransitionHash != transition.Hash() || received.Transition.Hash() != transition.Hash() {
		t.Fatal("original rotation proof was regenerated", v, e)
	}
	if v.VerifySuccessor(old, now) != nil {
		t.Fatal("provider rotation lost original trusted predecessor")
	}
}
