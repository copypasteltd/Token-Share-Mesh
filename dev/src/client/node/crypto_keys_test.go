package node

// SOURCE_ONLY: original OS-lock/AEAD journal boundary; no database or cipher ABI.
import (
	"context"
	"errors"
	"os"
	"p11.local/compute/client/crypto-gateway/endpoint"
	"testing"
	"time"
)

func TestEndpointNodeAnchorSurvivesReopenAndMissingJournalCannotReinitialize(t *testing.T) {
	s := testStore(t, "https://api.example.invalid")
	if _, e := s.InitEndpointKeys(); e != nil {
		t.Fatal(e)
	}
	anchor := s.s.CryptoID
	s = reopen(t, s)
	v, e := s.OpenEndpointKeys()
	if e != nil || s.s.CryptoID != anchor {
		t.Fatal("original endpoint anchor changed", e)
	}
	if public, e := v.Public(); e != nil || len(public) != 0 {
		t.Fatal("empty key root manufactured identities", e)
	}
	path := s.path
	s.Close()
	if e = os.Remove(path + string(os.PathSeparator) + endpointKeyFile); e != nil {
		t.Fatal(e)
	}
	restored, e := openWith(context.Background(), path, testLoader)
	if e != nil {
		t.Fatal(e)
	}
	defer restored.Close()
	if _, e = restored.OpenEndpointKeys(); !errors.Is(e, endpoint.ErrState) {
		t.Fatal("missing original keys restored", e)
	}
	if _, e = restored.InitEndpointKeys(); !errors.Is(e, endpoint.ErrState) {
		t.Fatal("anchored identity replaced", e)
	}
}
func TestEndpointNodeRejectsForeignSellerBeforeCipherInitialization(t *testing.T) {
	s := testStore(t, "https://api.example.invalid")
	if _, e := s.InitEndpointKeys(); e != nil {
		t.Fatal(e)
	}
	if _, e := s.CreateLocalEndpoint("seller_foreign", "device_foreign", "SELLER", time.Now().UTC(), time.Now().UTC().Add(time.Hour)); !errors.Is(e, endpoint.ErrTrust) {
		t.Fatal("unregistered foreign seller identity created", e)
	}
}
