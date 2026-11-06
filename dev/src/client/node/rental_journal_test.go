package node

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"p11.local/compute/client/protocol/rentalaccess"
)

type rentalJournalFixture struct {
	now      time.Time
	signer   *rentalaccess.Signer
	verifier *rentalaccess.Verifier
	metadata rentalaccess.Metadata
	token    string
	expected rentalaccess.Expected
}

func journalRental(t *testing.T) rentalJournalFixture {
	t.Helper()
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{93}, 32)) // Synthetic test identity only.
	defer clear(key)
	signer, err := rentalaccess.NewSigner("issuer_test", "key_test", key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(signer.Close)
	verifier, err := rentalaccess.NewVerifier(map[string]ed25519.PublicKey{"key_test": key.Public().(ed25519.PublicKey)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(verifier.Close)
	now := time.Now()
	c := rentalaccess.Claims{Profile: rentalaccess.Profile, Issuer: "issuer_test", Subject: "buyer_test", Audience: "device_test", ID: "grant_test", Issued: now.Unix(), NotBefore: now.Unix(), Expires: now.Add(10 * time.Minute).Unix(), Order: "order_test", Session: "session_test", Seller: "seller_test", Device: "device_test", Resource: "resource_test", Generation: "4", Terms: "1", TermsHash: strings.Repeat("a", 64), Environment: "environment_test", EnvironmentHash: strings.Repeat("b", 64), Manifest: "manifest_test", ManifestHash: strings.Repeat("c", 64), Scopes: []string{"INVOKE"}, HardDeadline: now.Add(10 * time.Minute).UTC().Format(time.RFC3339Nano), Mode: "GPU_RENTAL", Privacy: "STANDARD"}
	i, err := signer.Issue(c, now)
	if err != nil {
		t.Fatal(err)
	}
	defer i.Close()
	m := i.Metadata()
	token, err := i.TakeCredential()
	if err != nil {
		t.Fatal(err)
	}
	e := rentalaccess.Expected{TokenDigest: m.Digest, Issuer: c.Issuer, Subject: c.Subject, Audience: c.Audience, Grant: c.ID, Order: c.Order, Session: c.Session, Seller: c.Seller, Device: c.Device, Resource: c.Resource, Generation: c.Generation, Terms: c.Terms, TermsHash: c.TermsHash, Environment: c.Environment, EnvironmentHash: c.EnvironmentHash, Manifest: c.Manifest, ManifestHash: c.ManifestHash, HardDeadline: c.HardDeadline}
	return rentalJournalFixture{now, signer, verifier, m, token, e}
}

func TestRentalJournalEncryptsAndRestartRequiresHigherControl(t *testing.T) {
	s := registeredStore(t)
	j, err := s.InitRentalJournal()
	if err != nil {
		t.Fatal(err)
	}
	f := journalRental(t)
	w, _, err := j.openWindow(f.verifier, f.token, f.expected, 7, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if w.Check(f.expected, f.now) == nil {
		t.Fatal("new journal manufactured ALLOW")
	}
	seq, err := w.Request(f.expected, f.now)
	if err != nil || seq != "1" {
		t.Fatal(seq, err)
	}
	reply, err := f.signer.SignControl(f.metadata, 1, "ALLOW", f.now, f.now.Add(30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(reply)
	if err = w.Accept(f.verifier, raw, f.expected, f.now); err != nil {
		t.Fatal(err)
	}
	sealed, err := s.read(rentalJournalFile, rentalJournalLimit)
	if err != nil || bytes.Contains(sealed, []byte(f.expected.Grant)) || bytes.Contains(sealed, []byte(f.token)) || bytes.Contains(sealed, []byte("original_expected")) {
		t.Fatal("plaintext journal metadata/credential", err)
	}
	// Parent close models process exit; it must not restore a live deadline.
	s = reopen(t, s)
	j, err = s.OpenRentalJournal()
	if err != nil {
		t.Fatal(err)
	}
	w, _, err = j.openWindow(f.verifier, f.token, f.expected, 7, f.now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if w.Check(f.expected, f.now.Add(time.Second)) == nil || w.Accept(f.verifier, raw, f.expected, f.now.Add(time.Second)) == nil {
		t.Fatal("cached control restored permission")
	}
	seq, err = w.Request(f.expected, f.now.Add(time.Second))
	if err != nil || seq != "2" {
		t.Fatal("restart did not advance", seq, err)
	}
	if w.Accept(f.verifier, raw, f.expected, f.now.Add(time.Second)) == nil {
		t.Fatal("old response satisfied higher request")
	}
	if _, _, err = j.openWindow(f.verifier, f.token, f.expected, 7, f.now.Add(time.Second)); err == nil {
		t.Fatal("concurrent duplicate window bypassed high water")
	}
}

func TestRentalJournalMissingOrTamperedStateNeverReinitializes(t *testing.T) {
	for _, mode := range []string{"missing", "tampered"} {
		t.Run(mode, func(t *testing.T) {
			s := registeredStore(t)
			if _, err := s.InitRentalJournal(); err != nil {
				t.Fatal(err)
			}
			if mode == "missing" {
				if err := s.root.Remove(rentalJournalFile); err != nil {
					t.Fatal(err)
				}
			} else {
				raw, err := s.read(rentalJournalFile, rentalJournalLimit)
				if err != nil {
					t.Fatal(err)
				}
				raw[len(raw)-1] ^= 1
				file, err := s.root.OpenFile(rentalJournalFile, os.O_RDWR, 0)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = file.WriteAt(raw, 0); err != nil {
					t.Fatal(err)
				}
				if err = file.Sync(); err != nil {
					t.Fatal(err)
				}
				file.Close()
			}
			s = reopen(t, s)
			if _, err := s.OpenRentalJournal(); !errors.Is(err, ErrRentalJournal) {
				t.Fatal("damaged original state opened", err)
			}
			if _, err := s.InitRentalJournal(); !errors.Is(err, ErrRentalJournal) {
				t.Fatal("damaged original state recreated", err)
			}
		})
	}
}

func TestRentalJournalUncertainReplacementPoisonsAllAdmission(t *testing.T) {
	s := registeredStore(t)
	j, err := s.InitRentalJournal()
	if err != nil {
		t.Fatal(err)
	}
	f := journalRental(t)
	w, _, err := j.openWindow(f.verifier, f.token, f.expected, 7, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.root.Remove(rentalJournalFile); err != nil {
		t.Fatal(err)
	}
	if err = s.root.Mkdir(rentalJournalFile, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = w.Request(f.expected, f.now); err == nil || j.available() || w.Check(f.expected, f.now) == nil {
		t.Fatal("uncertain journal admitted work")
	}
}

func TestRentalJournalStoppedGrantSurvivesRestart(t *testing.T) {
	s := registeredStore(t)
	j, err := s.InitRentalJournal()
	if err != nil {
		t.Fatal(err)
	}
	f := journalRental(t)
	c, err := j.OpenControl(f.verifier, f.token, f.expected, 7, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if c.OriginalExpected() != f.expected {
		t.Fatal("original independent expectation changed")
	}
	s = reopen(t, s) // Parent close must permanently stop the child and close its key.
	if c.available() || c.Close() != nil {
		t.Fatal("parent close retained live child control")
	}
	j, err = s.OpenRentalJournal()
	if err != nil {
		t.Fatal(err)
	}
	w, _, err := j.openWindow(f.verifier, f.token, f.expected, 7, f.now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Request(f.expected, f.now.Add(time.Second)); err == nil {
		t.Fatal("stopped original grant revived")
	}
}

func TestRentalControlTransportRetryKeepsOriginalSequenceAndKey(t *testing.T) {
	f := journalRental(t)
	var s *Store
	var originalBody []byte
	var originalKey string
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 2049))
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if r.Method != "POST" || r.URL.Path != "/v1/p11/runner/rental-sessions/session_test/control" || len(raw) > 2048 {
			t.Error("wrong original control route")
			w.WriteHeader(400)
			return
		}
		verify(t, s, r, raw)
		if requests == 0 {
			originalBody = bytes.Clone(raw)
			originalKey = r.Header.Get("Idempotency-Key")
		} else if !bytes.Equal(originalBody, raw) || originalKey != r.Header.Get("Idempotency-Key") {
			t.Error("retry changed original sequence/key")
		}
		requests++
		if requests == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(503)
			w.Write([]byte(`{"error":"synthetic temporary failure"}`))
			return
		}
		var in rentalaccess.ControlRequest
		if json.Unmarshal(raw, &in) != nil {
			t.Error("invalid request")
			return
		}
		seq, err := strconv.ParseUint(in.Sequence, 10, 64)
		if err != nil {
			t.Error(err)
			return
		}
		now := time.Now()
		reply, err := f.signer.SignControl(f.metadata, seq, "ALLOW", now, now.Add(30*time.Second))
		if err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(reply)
	}))
	defer server.Close()
	s = testStore(t, server.URL)
	s.s.DeviceID = "device_test"
	s.s.SellerID = "seller_test"
	s.s.Version = 1
	s.s.RemoteState = "PAIRED"
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	j, err := s.InitRentalJournal()
	if err != nil {
		t.Fatal(err)
	}
	c, err := j.OpenControl(f.verifier, f.token, f.expected, 7, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	load := func(context.Context) (rentalaccess.Expected, error) { return f.expected, nil }
	if c.Refresh(context.Background(), load) == nil || c.CheckScope(context.Background(), "INVOKE", load) == nil {
		t.Fatal("temporary HTTP error manufactured authority")
	}
	if err = c.Refresh(context.Background(), load); err != nil {
		t.Fatal(err)
	}
	if err = c.CheckScope(context.Background(), "INVOKE", load); err != nil {
		t.Fatal(err)
	}
	if c.CheckScope(context.Background(), "EXPORT_SELF", load) == nil {
		t.Fatal("control elevated an absent scope")
	}
	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
	if c.CheckScope(context.Background(), "INVOKE", load) == nil {
		t.Fatal("closed controller admitted work")
	}
}

func TestRentalResolveControlReadsOriginalStoredDigestAndRejectsRebinding(t *testing.T) {
	for _, mode := range []string{"original", "wrong_digest", "wrong_key", "foreign_manifest"} {
		t.Run(mode, func(t *testing.T) {
			f := journalRental(t)
			metadata := rentalaccess.GrantExpectation{Profile: rentalaccess.ExpectationProfile, Metadata: f.metadata, SessionVersion: 7, Connection: "connection_test"}
			switch mode {
			case "wrong_digest":
				metadata.Metadata.Digest = strings.Repeat("f", 64)
			case "wrong_key":
				metadata.Metadata.KeyID = "key_other"
			case "foreign_manifest":
				metadata.Metadata.Claims.ManifestHash = strings.Repeat("d", 64)
			}
			var s *Store
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/v1/p11/runner/rental-sessions/session_test/access-grants/grant_test" {
					t.Error("wrong metadata read route")
					w.WriteHeader(400)
					return
				}
				verify(t, s, r, nil)
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(metadata)
			}))
			defer server.Close()
			s = testStore(t, server.URL)
			s.s.DeviceID = "device_test"
			s.s.SellerID = "seller_test"
			s.s.Version = 1
			s.s.RemoteState = "PAIRED"
			if err := s.save(); err != nil {
				t.Fatal(err)
			}
			j, err := s.InitRentalJournal()
			if err != nil {
				t.Fatal(err)
			}
			approved := f.expected
			approved.TokenDigest = "" // Digest must be read, not caller asserted.
			c, err := j.ResolveControl(context.Background(), f.verifier, f.token, approved, "connection_test")
			if mode != "original" {
				if err == nil {
					c.Close()
					t.Fatal("untrusted metadata rebound an original grant")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			load := func(context.Context) (rentalaccess.Expected, error) { return f.expected, nil }
			if c.CheckScope(context.Background(), "INVOKE", load) == nil {
				t.Fatal("metadata read manufactured live control")
			}
		})
	}
}
