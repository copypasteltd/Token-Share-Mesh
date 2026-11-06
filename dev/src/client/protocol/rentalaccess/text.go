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
	"sync"
	"time"

	"p11.local/compute/client/protocol/executionlease"
	relay "p11.local/compute/gateway/core"
)

const TextPolicyProfile = "P11_RENTAL_LOCAL_TEXT_POLICY_1"
const TextInputProfile = "P11_RENTAL_LOCAL_TEXT_INPUT_1"
const MaximumTextInputBytes = 512 << 10

// TextPolicy is independently approved LOCAL model/protocol mapping. The
// network request cannot choose a backend, file, model alias or token ceiling.
// PublicModel may differ from the immutable model reference only by this
// explicit owner-approved mapping. No model name is rewritten in the body.
type TextPolicy struct {
	Profile          string        `json:"profile_id"`
	ModelRef         string        `json:"model_ref"`
	PublicModel      string        `json:"public_model"`
	Protocol         relay.Profile `json:"protocol_profile_id"`
	InputTokenLimit  uint32        `json:"input_token_limit"`
	OutputTokenLimit uint32        `json:"output_token_limit"`
}

func (p TextPolicy) Binds(prepared Prepared) bool {
	m := prepared.Details()
	protocol := p.Protocol == relay.ChatText || p.Protocol == relay.ResponsesText || p.Protocol == relay.MessagesText
	return prepared.Digest() != "" && p.Profile == TextPolicyProfile && protocol && string(p.Protocol) == m.Protocol &&
		m.Engine == "LLAMA_CPP_NATIVE" && m.PublicSpec.WorkflowID == "" && m.PublicSpec.ModelID != "" && p.ModelRef == m.PublicSpec.ModelID &&
		len(p.PublicModel) > 0 && len(p.PublicModel) <= 192 && p.InputTokenLimit > 0 && p.OutputTokenLimit > 0 &&
		p.InputTokenLimit <= 131072 && p.OutputTokenLimit <= 131072 &&
		(m.PublicSpec.InputTokenLimit == 0 || int64(p.InputTokenLimit) <= m.PublicSpec.InputTokenLimit) &&
		(m.PublicSpec.OutputTokenLimit == 0 || int64(p.OutputTokenLimit) <= m.PublicSpec.OutputTokenLimit)
}
func (p TextPolicy) Digest() string {
	raw, _ := json.Marshal(p)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}

type TextSpec struct {
	Protocol        relay.Profile `json:"protocol_profile_id"`
	PublicModel     string        `json:"public_model"`
	MaxOutputTokens int           `json:"max_output_tokens"`
	Stream          bool          `json:"stream"`
}

// TextDescriptor is safe original invocation metadata. There is no job/attempt,
// plaintext fingerprint, prompt, output, content key, path or executable here.
type TextDescriptor struct {
	Profile               string   `json:"profile_id"`
	ID                    string   `json:"invocation_id"`
	Session               string   `json:"session_id"`
	Resource              string   `json:"resource_id"`
	Generation            string   `json:"resource_lock_generation"`
	LocalGeneration       string   `json:"local_generation"`
	Grant                 string   `json:"grant_id"`
	GrantDigest           string   `json:"grant_digest"`
	ManifestHash          string   `json:"manifest_hash"`
	EnvironmentHash       string   `json:"environment_digest"`
	RecipeHash            string   `json:"recipe_digest"`
	PolicyHash            string   `json:"local_policy_digest"`
	InputCiphertextDigest string   `json:"input_ciphertext_digest"`
	InputBytes            int      `json:"input_bytes"`
	InputTokenLimit       uint32   `json:"input_token_limit"`
	Spec                  TextSpec `json:"request_spec"`
}

func (d TextDescriptor) Digest() string {
	raw, _ := json.Marshal(d)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}

// TextInput authenticates an original Bearer against independently loaded
// Expected, strictly parses the standard protocol and seals the bounded body
// locally with a fresh AES-GCM key. Keys/ciphertext exist only in this process.
// This local STANDARD-mode envelope is NOT network E2E, durable recovery or
// hardware confidentiality. A process restart must never regenerate its input.
type TextInput struct{ data *textInputState }
type textInputState struct {
	mu                      sync.Mutex
	descriptor              TextDescriptor
	preparedHash            string
	expected                Expected
	grant                   Verified
	key, nonce, sealed, aad []byte
	taken                   bool
}

func SealTextInput(prepared Prepared, verifier *Verifier, credential string, current Expected, id, localGeneration string, policy TextPolicy, raw []byte, now time.Time) (*TextInput, error) {
	if verifier == nil || !prepared.BindsGrant(current) || !identifier(id) || !policy.Binds(prepared) || len(raw) < 1 || len(raw) > MaximumTextInputBytes {
		return nil, ErrAccess
	}
	local, e := executionlease.Counter(localGeneration)
	if e != nil || local < 1 {
		return nil, ErrAccess
	}
	grant, e := verifier.Verify(credential, current, now)
	if e != nil || !grant.Allows("INVOKE", now) {
		return nil, ErrAccess
	}
	meta, e := relay.Preflight(policy.Protocol, policy.PublicModel, raw)
	if e != nil || meta.MaxOutputTokens < 1 || uint64(meta.MaxOutputTokens) > uint64(policy.OutputTokenLimit) {
		return nil, ErrAccess
	}
	m := prepared.Details()
	d := TextDescriptor{Profile: TextInputProfile, ID: id, Session: m.Session, Resource: m.Resource, Generation: m.Generation, LocalGeneration: localGeneration, Grant: current.Grant, GrantDigest: current.TokenDigest, ManifestHash: prepared.Digest(), EnvironmentHash: m.EnvironmentHash, RecipeHash: m.RecipeHash, PolicyHash: policy.Digest(), InputBytes: len(raw), InputTokenLimit: policy.InputTokenLimit, Spec: TextSpec{policy.Protocol, policy.PublicModel, meta.MaxOutputTokens, meta.Stream}}
	aad, e := json.Marshal(d)
	if e != nil {
		return nil, ErrAccess
	}
	key := make([]byte, 32)
	if _, e = rand.Read(key); e != nil {
		clear(key)
		return nil, ErrAccess
	}
	block, e := aes.NewCipher(key)
	if e != nil {
		clear(key)
		return nil, ErrAccess
	}
	aead, e := cipher.NewGCM(block)
	if e != nil {
		clear(key)
		return nil, ErrAccess
	}
	nonce := make([]byte, aead.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		clear(key)
		return nil, ErrAccess
	}
	sealed := aead.Seal(nil, nonce, raw, aad)
	h := sha256.New()
	h.Write(nonce)
	h.Write(sealed)
	d.InputCiphertextDigest = hex.EncodeToString(h.Sum(nil))
	return &TextInput{data: &textInputState{descriptor: d, preparedHash: prepared.Digest(), expected: current, grant: grant, key: key, nonce: nonce, sealed: sealed, aad: aad}}, nil
}

func (i *TextInput) Descriptor() TextDescriptor {
	if i == nil || i.data == nil {
		return TextDescriptor{}
	}
	i.data.mu.Lock()
	defer i.data.mu.Unlock()
	return i.data.descriptor
}
func (i TextInput) String() string               { return "[sealed rental input]" }
func (i TextInput) GoString() string             { return i.String() }
func (i TextInput) Format(s fmt.State, _ rune)   { io.WriteString(s, i.String()) }
func (i TextInput) MarshalJSON() ([]byte, error) { return []byte(`{"input":"REDACTED"}`), nil }

// Take decrypts once after the caller rechecks current control/physical owner.
// The returned body belongs to that caller and must be cleared after native
// normalization. A consumed/lost input is QUERY_ONLY, never reconstructed.
func (i *TextInput) Take(ctx context.Context, prepared Prepared, current Expected, now time.Time) ([]byte, error) {
	if i == nil || i.data == nil || ctx.Err() != nil {
		return nil, ErrAccess
	}
	s := i.data
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.taken || s.preparedHash != prepared.Digest() || s.expected != current || !prepared.BindsGrant(current) || !s.grant.Matches(current, now) || !s.grant.Allows("INVOKE", now) {
		return nil, ErrAccess
	}
	s.taken = true
	defer s.clearLocked()
	block, e := aes.NewCipher(s.key)
	if e != nil {
		return nil, ErrAccess
	}
	aead, e := cipher.NewGCM(block)
	if e != nil {
		return nil, ErrAccess
	}
	raw, e := aead.Open(nil, s.nonce, s.sealed, s.aad)
	if e != nil || ctx.Err() != nil || len(raw) != s.descriptor.InputBytes {
		clear(raw)
		return nil, ErrAccess
	}
	return raw, nil
}
func (s *textInputState) clearLocked() {
	clear(s.key)
	clear(s.nonce)
	clear(s.sealed)
	clear(s.aad)
	s.key = nil
	s.nonce = nil
	s.sealed = nil
	s.aad = nil
}
func (i *TextInput) Close() {
	if i == nil || i.data == nil {
		return
	}
	s := i.data
	s.mu.Lock()
	defer s.mu.Unlock()
	s.taken = true
	s.clearLocked()
}
