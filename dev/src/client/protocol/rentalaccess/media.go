package rentalaccess

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sync"
	"time"
	"unicode/utf8"

	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/shared/strictjson"
)

const MediaPolicyProfile = "P11_RENTAL_LOCAL_MEDIA_POLICY_1"
const MediaInputProfile = "P11_RENTAL_LOCAL_MEDIA_INPUT_1"
const MaximumMediaParametersBytes = 1 << 20
const MaximumMediaAssetsBytes = 64 << 20
const mediaBlockBytes = 65536

// This local mapping is independently approved, not copied from a request.
// Protocol is the existing manifest's public dialect; this private normalized
// envelope adds no public media endpoint to either OpenAI or Anthropic.
type MediaPolicy struct {
	Profile            string `json:"profile_id"`
	Workflow           string `json:"workflow_id"`
	Revision           int64  `json:"workflow_revision"`
	Protocol           string `json:"protocol_profile_id"`
	Kind               string `json:"media_kind"`
	MaximumOutputBytes int64  `json:"maximum_output_bytes"`
}

func (p MediaPolicy) Binds(prepared Prepared) bool {
	m := prepared.Details()
	return prepared.Digest() != "" && p.Profile == MediaPolicyProfile && m.Engine == "COMFYUI" && p.Protocol == m.Protocol && identifier(p.Workflow) && p.Workflow == m.PublicSpec.WorkflowID && p.Revision > 0 && p.Revision == m.PublicSpec.WorkflowRevision && m.PublicSpec.ModelID == "" && (p.Kind == "PNG" || p.Kind == "MP4" || p.Kind == "WAV" || p.Kind == "GLB") && p.MaximumOutputBytes > 0 && p.MaximumOutputBytes <= 64<<20
}
func (p MediaPolicy) Digest() string {
	b, _ := json.Marshal(p)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

type MediaDescriptor struct {
	Profile, ID, Session, Resource, Generation, LocalGeneration               string
	Grant, GrantDigest, ManifestHash, EnvironmentHash, RecipeHash, PolicyHash string
	InputCiphertextDigest, Kind                                               string
	ParametersBytes, Assets                                                   int
	AssetBytes                                                                int64
}

// Local RAM-only input ownership; descriptor excludes prompt/plaintext hashes.
// Source implementations must permit Close to interrupt an outstanding Read.
type MediaAsset struct {
	ID     string
	Bytes  int64
	Source io.ReadCloser
}
type mediaAssetState struct {
	id     string
	bytes  int64
	sealed [][]byte
}
type mediaInputState struct {
	mu                      sync.Mutex
	descriptor              MediaDescriptor
	preparedHash            string
	expected                Expected
	grant                   Verified
	key, nonce, sealed, aad []byte
	assets                  []mediaAssetState
	taken                   bool
}
type MediaInput struct{ state *mediaInputState }

func (i MediaInput) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, "[sealed rental media input redacted]")
}
func (i MediaInput) MarshalJSON() ([]byte, error) { return []byte(`{"redacted":true}`), nil }
func (i *MediaInput) Descriptor() MediaDescriptor {
	if i == nil || i.state == nil {
		return MediaDescriptor{}
	}
	i.state.mu.Lock()
	defer i.state.mu.Unlock()
	return i.state.descriptor
}

type mediaParameters map[string]json.RawMessage

func validParameters(raw []byte) bool {
	var fields mediaParameters
	if len(raw) < 2 || len(raw) > MaximumMediaParametersBytes || strictjson.Decode(raw, &fields, MaximumMediaParametersBytes) != nil || fields == nil || len(fields) > 32 {
		return false
	}
	for name, value := range fields {
		if !identifier(name) || len(name) > 64 {
			return false
		}
		var v any
		if json.Unmarshal(value, &v) != nil {
			return false
		}
		switch item := v.(type) {
		case string:
			if !utf8.ValidString(item) || len(item) > 65536 {
				return false
			}
			for _, r := range item {
				if r == 0 {
					return false
				}
			}
		case float64:
			if math.IsInf(item, 0) || math.IsNaN(item) {
				return false
			}
		default:
			return false
		}
	}
	return true
}
func assetAAD(base []byte, index, block int) []byte {
	b, _ := json.Marshal(struct{ Index, Block int }{index, block})
	return append(append(append([]byte(nil), base...), byte('\n')), b...)
}

// SealMediaInput takes ownership of every Source even when admission fails.
// It buffers ciphertext, not full plaintext; bounded 64-KiB scratch is cleared.
// This is local STANDARD custody, not network E2E or durable replay storage.
func SealMediaInput(ctx context.Context, prepared Prepared, verifier *Verifier, credential string, current Expected, id, localGeneration string, policy MediaPolicy, parameters []byte, assets []MediaAsset, now time.Time) (*MediaInput, error) {
	owned := make([]MediaAsset, len(assets))
	copy(owned, assets)
	assets = owned
	for index, a := range assets {
		if a.Source != nil {
			source := &mediaSource{source: a.Source}
			assets[index].Source = source
			defer source.Close()
		}
	}
	if ctx == nil || ctx.Err() != nil || verifier == nil || !prepared.BindsGrant(current) || !identifier(id) || !policy.Binds(prepared) || !validParameters(parameters) || len(assets) > 4 {
		return nil, ErrAccess
	}
	sealContext, cancelSeal := context.WithTimeout(ctx, 2*time.Minute)
	defer cancelSeal()
	ctx = sealContext
	n, e := executionlease.Counter(localGeneration)
	if e != nil || n < 1 {
		return nil, ErrAccess
	}
	grant, e := verifier.Verify(credential, current, now)
	if e != nil || !grant.Allows("INVOKE", now) {
		return nil, ErrAccess
	}
	seen := map[string]bool{}
	var total int64
	for _, a := range assets {
		if !identifier(a.ID) || len(a.ID) > 128 || seen[a.ID] || a.Source == nil || a.Bytes < 1 || a.Bytes > MaximumMediaAssetsBytes || total > MaximumMediaAssetsBytes-a.Bytes {
			return nil, ErrAccess
		}
		seen[a.ID] = true
		total += a.Bytes
	}
	m := prepared.Details()
	d := MediaDescriptor{Profile: MediaInputProfile, ID: id, Session: m.Session, Resource: m.Resource, Generation: m.Generation, LocalGeneration: localGeneration, Grant: current.Grant, GrantDigest: current.TokenDigest, ManifestHash: prepared.Digest(), EnvironmentHash: m.EnvironmentHash, RecipeHash: m.RecipeHash, PolicyHash: policy.Digest(), Kind: policy.Kind, ParametersBytes: len(parameters), Assets: len(assets), AssetBytes: total}
	aad, e := json.Marshal(d)
	if e != nil {
		return nil, ErrAccess
	}
	key := make([]byte, 32)
	if _, e = rand.Read(key); e != nil {
		clear(key)
		return nil, ErrAccess
	}
	state := &mediaInputState{descriptor: d, preparedHash: prepared.Digest(), expected: current, grant: grant, key: key, aad: aad}
	success := false
	defer func() {
		if !success {
			state.clearLocked()
		}
	}()
	block, e := aes.NewCipher(key)
	if e != nil {
		return nil, ErrAccess
	}
	aead, e := cipher.NewGCM(block)
	if e != nil {
		return nil, ErrAccess
	}
	nonce := make([]byte, aead.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return nil, ErrAccess
	}
	state.nonce = nonce
	state.sealed = aead.Seal(nil, nonce, parameters, aad)
	h := sha256.New()
	_, _ = h.Write([]byte(MediaInputProfile + "\x00"))
	_, _ = h.Write(nonce)
	_, _ = h.Write(state.sealed)
	scratch := make([]byte, mediaBlockBytes)
	defer clear(scratch)
	for index, asset := range assets {
		// Cancellation actively closes the owned source; it does not spawn detached
		// read goroutines or pretend a timed-out external source has joined.
		stop := cancelMediaSource(ctx, asset.Source)
		defer stop()
		item := mediaAssetState{id: asset.ID, bytes: asset.Bytes}
		state.assets = append(state.assets, item)
		for remaining, part := asset.Bytes, 0; remaining > 0; part++ {
			size := int64(len(scratch))
			if remaining < size {
				size = remaining
			}
			got, e := readMediaFull(ctx, asset.Source, scratch[:int(size)])
			if e != nil || got != int(size) || ctx.Err() != nil {
				stop()
				return nil, ErrAccess
			}
			nonce := make([]byte, aead.NonceSize())
			if _, e = rand.Read(nonce); e != nil {
				stop()
				return nil, ErrAccess
			}
			recordAAD := assetAAD(aad, index, part)
			sealed := aead.Seal(nonce, nonce, scratch[:got], recordAAD)
			clear(recordAAD)
			clear(scratch[:got])
			state.assets[index].sealed = append(state.assets[index].sealed, sealed)
			_, _ = h.Write(sealed)
			remaining -= int64(got)
		}
		// Sources must end at the declared length; unbounded live readers are not
		// eligible assets. Closing/cancellation remains owned by this call.
		var extra [1]byte
		got, end := asset.Source.Read(extra[:])
		clear(extra[:])
		stop()
		if got != 0 || end != io.EOF || ctx.Err() != nil {
			return nil, ErrAccess
		}
	}
	state.descriptor.InputCiphertextDigest = hex.EncodeToString(h.Sum(nil))
	success = true
	return &MediaInput{state: state}, nil
}

// MediaPlain exists only while the trusted adapter constructs private frames.
// Metadata hashes here stay in memory/pipe; none enters the execution ledger.
type MediaPlain struct{ state *mediaPlainState }
type mediaPlainState struct {
	mu         sync.Mutex
	parameters []byte
	key, aad   []byte
	assets     []mediaAssetState
	closed     bool
}
type MediaAssetInfo struct {
	ID     string
	Bytes  int64
	SHA256 string
}

func (p MediaPlain) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, "[private media plaintext redacted]")
}
func (p MediaPlain) MarshalJSON() ([]byte, error) { return []byte(`{"redacted":true}`), nil }
func (i *MediaInput) Take(ctx context.Context, prepared Prepared, current Expected, now time.Time) (MediaPlain, error) {
	if i == nil || i.state == nil || ctx == nil || ctx.Err() != nil {
		return MediaPlain{}, ErrAccess
	}
	s := i.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.taken || s.preparedHash != prepared.Digest() || s.expected != current || !prepared.BindsGrant(current) || !s.grant.Matches(current, now) || !s.grant.Allows("INVOKE", now) {
		return MediaPlain{}, ErrAccess
	}
	s.taken = true
	block, e := aes.NewCipher(s.key)
	if e != nil {
		s.clearLocked()
		return MediaPlain{}, ErrAccess
	}
	aead, e := cipher.NewGCM(block)
	if e != nil {
		s.clearLocked()
		return MediaPlain{}, ErrAccess
	}
	raw, e := aead.Open(nil, s.nonce, s.sealed, s.aad)
	if e != nil || len(raw) != s.descriptor.ParametersBytes || !validParameters(raw) || ctx.Err() != nil {
		clear(raw)
		s.clearLocked()
		return MediaPlain{}, ErrAccess
	}
	out := MediaPlain{state: &mediaPlainState{parameters: raw, key: s.key, aad: s.aad, assets: s.assets}}
	s.key = nil
	s.aad = nil
	s.assets = nil
	s.clearLocked()
	return out, nil
}
func (s *mediaInputState) clearLocked() {
	clear(s.key)
	clear(s.nonce)
	clear(s.sealed)
	clear(s.aad)
	for _, a := range s.assets {
		for _, b := range a.sealed {
			clear(b)
		}
	}
	s.key = nil
	s.nonce = nil
	s.sealed = nil
	s.aad = nil
	s.assets = nil
}
func (i *MediaInput) Close() {
	if i == nil || i.state == nil {
		return
	}
	i.state.mu.Lock()
	defer i.state.mu.Unlock()
	i.state.taken = true
	i.state.clearLocked()
}

// WithParameters exposes a borrowed view only for the duration of action.
func (p MediaPlain) WithParameters(action func([]byte) error) error {
	if p.state == nil || action == nil {
		return ErrAccess
	}
	s := p.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrAccess
	}
	return action(s.parameters)
}
func (p MediaPlain) each(ctx context.Context, index int, action func([]byte) error) error {
	s := p.state
	if s == nil || ctx == nil {
		return ErrAccess
	}
	block, e := aes.NewCipher(s.key)
	if e != nil {
		return ErrAccess
	}
	aead, e := cipher.NewGCM(block)
	if e != nil {
		return ErrAccess
	}
	a := s.assets[index]
	var count int64
	for part, sealed := range a.sealed {
		if ctx.Err() != nil || len(sealed) <= aead.NonceSize()+aead.Overhead() {
			return ErrAccess
		}
		aad := assetAAD(s.aad, index, part)
		raw, e := aead.Open(nil, sealed[:aead.NonceSize()], sealed[aead.NonceSize():], aad)
		clear(aad)
		if e != nil || len(raw) > mediaBlockBytes {
			clear(raw)
			return ErrAccess
		}
		count += int64(len(raw))
		e = action(raw)
		clear(raw)
		if e != nil {
			return ErrAccess
		}
	}
	if count != a.bytes {
		return ErrAccess
	}
	return nil
}
func (p MediaPlain) Assets(ctx context.Context) ([]MediaAssetInfo, error) {
	if p.state == nil {
		return nil, ErrAccess
	}
	s := p.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrAccess
	}
	result := make([]MediaAssetInfo, 0, len(s.assets))
	for index, a := range s.assets {
		h := sha256.New()
		if p.each(ctx, index, func(b []byte) error { _, e := h.Write(b); return e }) != nil {
			return nil, ErrAccess
		}
		result = append(result, MediaAssetInfo{ID: a.id, Bytes: a.bytes, SHA256: hex.EncodeToString(h.Sum(nil))})
	}
	return result, nil
}
func (p MediaPlain) StreamAsset(ctx context.Context, id string, action func([]byte) error) error {
	if p.state == nil || action == nil {
		return ErrAccess
	}
	s := p.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrAccess
	}
	for index, a := range s.assets {
		if a.id == id {
			return p.each(ctx, index, action)
		}
	}
	return ErrAccess
}
func (p MediaPlain) Close() {
	if p.state == nil {
		return
	}
	s := p.state
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	clear(s.parameters)
	clear(s.key)
	clear(s.aad)
	for _, a := range s.assets {
		for _, b := range a.sealed {
			clear(b)
		}
	}
	s.parameters = nil
	s.key = nil
	s.aad = nil
	s.assets = nil
}

// Cancellation owns and joins its Close callback. No detached input I/O.
type mediaSource struct {
	source io.ReadCloser
	once   sync.Once
	err    error
}

func (s *mediaSource) Read(b []byte) (int, error) { return s.source.Read(b) }
func (s *mediaSource) Close() error               { s.once.Do(func() { s.err = s.source.Close() }); return s.err }
func cancelMediaSource(ctx context.Context, source io.ReadCloser) func() {
	joined := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { defer close(joined); _ = source.Close() })
	var once sync.Once
	return func() {
		once.Do(func() {
			if !stop() {
				<-joined
			}
		})
	}
}

func readMediaFull(ctx context.Context, source io.Reader, dst []byte) (int, error) {
	read := 0
	for read < len(dst) {
		if ctx.Err() != nil {
			return read, ErrAccess
		}
		n, e := source.Read(dst[read:])
		if n < 0 || n > len(dst)-read {
			return read, ErrAccess
		}
		read += n
		if e != nil {
			if read == len(dst) {
				return read, nil
			}
			return read, ErrAccess
		}
		if n == 0 {
			return read, ErrAccess
		}
	}
	return read, nil
}
