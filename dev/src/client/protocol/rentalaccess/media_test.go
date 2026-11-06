package rentalaccess_test

// SOURCE_ONLY local synthetic input definitions; never a public SDK/media test.
import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"p11.local/compute/client/internal/rentaltest"
	"p11.local/compute/client/protocol/rentalaccess"
)

func mediaPolicy(f rentaltest.Fixture) rentalaccess.MediaPolicy {
	m := f.Prepared.Details()
	return rentalaccess.MediaPolicy{Profile: rentalaccess.MediaPolicyProfile, Workflow: m.PublicSpec.WorkflowID, Revision: m.PublicSpec.WorkflowRevision, Protocol: m.Protocol, Kind: "PNG", MaximumOutputBytes: 64 << 20}
}
func TestRentalMediaEnvelopeSealsAssetsAndIsOneUseAcrossAliases(t *testing.T) {
	f := rentaltest.Media(t)
	body := []byte(`{"prompt":"private media fixture","seed":7}`)
	asset := bytes.Repeat([]byte{17}, 70001)
	i, e := rentalaccess.SealMediaInput(context.Background(), f.Prepared, f.Verifier, f.Credential, f.Expected, "invoke_media", "1", mediaPolicy(f), body, []rentalaccess.MediaAsset{{ID: "asset_original", Bytes: int64(len(asset)), Source: io.NopCloser(bytes.NewReader(asset))}}, f.Now)
	if e != nil {
		t.Fatal(e)
	}
	defer i.Close()
	alias := *i
	d := i.Descriptor()
	hash := sha256.Sum256(body)
	if d.InputCiphertextDigest == hex.EncodeToString(hash[:]) || d.AssetBytes != int64(len(asset)) || d.Assets != 1 || d.Kind != "PNG" {
		t.Fatal("plaintext fingerprint or missing original metadata")
	}
	plain, e := i.Take(context.Background(), f.Prepared, f.Expected, f.Now)
	if e != nil {
		t.Fatal(e)
	}
	defer plain.Close()
	if _, e = alias.Take(context.Background(), f.Prepared, f.Expected, f.Now); e == nil {
		t.Fatal("copied input replay")
	}
	info, e := plain.Assets(context.Background())
	if e != nil || len(info) != 1 {
		t.Fatal(e)
	}
	expected := sha256.Sum256(asset)
	if info[0].SHA256 != hex.EncodeToString(expected[:]) {
		t.Fatal("asset authentication mismatch")
	}
	var got []byte
	e = plain.StreamAsset(context.Background(), info[0].ID, func(chunk []byte) error {
		if len(chunk) > 65536 {
			t.Fatal("unbounded plaintext scratch")
		}
		got = append(got, chunk...)
		return nil
	})
	if e != nil || !bytes.Equal(got, asset) {
		t.Fatal("asset changed", e)
	}
	clear(got)
	if e = plain.WithParameters(func(raw []byte) error {
		if !bytes.Equal(raw, body) {
			t.Fatal("parameters changed")
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}
func TestRentalMediaRejectsForeignAuthorityAndDoesNotDowngradeText(t *testing.T) {
	f := rentaltest.Media(t)
	policy := mediaPolicy(f)
	wrong := f.Expected
	wrong.Subject = "other_buyer"
	if _, e := rentalaccess.SealMediaInput(context.Background(), f.Prepared, f.Verifier, f.Credential, wrong, "invoke_media", "1", policy, []byte(`{}`), nil, f.Now); e == nil {
		t.Fatal("foreign buyer input admitted")
	}
	text := rentaltest.Text(t, f.Policy.Protocol)
	if policy.Binds(text.Prepared) {
		t.Fatal("media policy bound to a text manifest")
	}
	if _, e := rentalaccess.SealMediaInput(context.Background(), f.Prepared, f.Verifier, f.Credential, f.Expected, "invoke_media", "1", policy, []byte(`{}`), nil, f.Now.Add(3*time.Minute)); e == nil {
		t.Fatal("expired grant revived")
	}
}
func TestRentalMediaParametersAndAssetBoundariesRejectNonScalarsAndLengthDrift(t *testing.T) {
	f := rentaltest.Media(t)
	policy := mediaPolicy(f)
	for _, raw := range []string{`{"graph":{"node":"foreign"}}`, `{"prompt":"one","prompt":"two"}`, `{"value":true}`, `{"array":[1]}`} {
		if _, e := rentalaccess.SealMediaInput(context.Background(), f.Prepared, f.Verifier, f.Credential, f.Expected, "invoke_media", "1", policy, []byte(raw), nil, f.Now); e == nil {
			t.Fatal("non-scalar or duplicate parameters accepted")
		}
	}
	for _, tc := range []struct {
		declared int64
		actual   int
	}{{3, 2}, {1, 2}} {
		source := io.NopCloser(bytes.NewReader(make([]byte, tc.actual)))
		if _, e := rentalaccess.SealMediaInput(context.Background(), f.Prepared, f.Verifier, f.Credential, f.Expected, "invoke_media", "1", policy, []byte(`{}`), []rentalaccess.MediaAsset{{ID: "asset_original", Bytes: tc.declared, Source: source}}, f.Now); e == nil {
			t.Fatal("declared asset length drift accepted")
		}
	}
}
func TestRentalMediaFormattingAndClosedAliasesExposeNoContent(t *testing.T) {
	f := rentaltest.Media(t)
	i, e := rentalaccess.SealMediaInput(context.Background(), f.Prepared, f.Verifier, f.Credential, f.Expected, "invoke_media", "1", mediaPolicy(f), []byte(`{"prompt":"secret-media-prompt"}`), nil, f.Now)
	if e != nil {
		t.Fatal(e)
	}
	alias := *i
	for _, v := range []any{i, *i} {
		raw, e := json.Marshal(v)
		if e != nil || bytes.Contains(raw, []byte("secret-media-prompt")) || strings.Contains(fmt.Sprintf("%+v %#v", v, v), f.Credential) {
			t.Fatal("input exposure", e)
		}
	}
	i.Close()
	if _, e = alias.Take(context.Background(), f.Prepared, f.Expected, f.Now); e == nil {
		t.Fatal("closed copied input revived")
	}
}

type blockingMediaSource struct {
	readStarted, closing, finishClose chan struct{}
	once                              sync.Once
}

func (s *blockingMediaSource) Read([]byte) (int, error) {
	s.once.Do(func() { close(s.readStarted) })
	<-s.closing
	return 0, io.EOF
}
func (s *blockingMediaSource) Close() error { close(s.closing); <-s.finishClose; return nil }
func TestRentalMediaCancellationJoinsActualOwnedSourceClose(t *testing.T) {
	f := rentaltest.Media(t)
	source := &blockingMediaSource{readStarted: make(chan struct{}), closing: make(chan struct{}), finishClose: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, e := rentalaccess.SealMediaInput(ctx, f.Prepared, f.Verifier, f.Credential, f.Expected, "invoke_media", "1", mediaPolicy(f), []byte(`{}`), []rentalaccess.MediaAsset{{ID: "asset_original", Bytes: 1, Source: source}}, f.Now)
		done <- e
	}()
	<-source.readStarted
	cancel()
	<-source.closing
	select {
	case <-done:
		t.Fatal("returned before owned Close callback joined")
	default:
	}
	close(source.finishClose)
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("cancelled input admitted")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("owned input failed to join")
	}
}
