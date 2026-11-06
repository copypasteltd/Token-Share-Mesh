package resultwire

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/shared/ids"
)

const ContentProfile = "P11_ORIGINAL_TEXT_CONTENT_1"
const GrantProfile = "P11_ORIGINAL_RESULT_READ_1"
const ReviewProfile = "P11_LOCAL_RESULT_REVIEW_1"
const ChunkBytes = 32768
const MaximumContentBytes = 32 << 20

// End is content, including an optional original matched stop string. It must
// not enter metadata journals, audit/outbox payloads, error bodies or logs.
type End struct {
	Stop string `json:"stop_sequence"`
}
type ContentHead struct {
	Profile      string                  `json:"profile_id"`
	Delivery     string                  `json:"delivery_id"`
	Declaration  Delivery                `json:"declaration"`
	Public       string                  `json:"provider_public_key"`
	Manifest     executionlease.Manifest `json:"execution_manifest"`
	IncludeUsage bool                    `json:"include_usage"`
	ChunkBytes   int                     `json:"chunk_bytes"`
	Chunks       int                     `json:"chunk_count"`
	Expires      string                  `json:"expires_at"`
	State        string                  `json:"state"`
	End          *End                    `json:"terminal,omitempty"`
}

func ChunkCount(n int64) int         { return int((n + ChunkBytes - 1) / ChunkBytes) }
func (h ContentHead) Digest() string { h.State = "UPLOADING"; h.End = nil; return Digest(h) }
func BytesDigest(b []byte) string    { d := sha256.Sum256(b); return hex.EncodeToString(d[:]) }
func (h ContentHead) Validate() error {
	k, e := base64.RawURLEncoding.Strict().DecodeString(h.Public)
	d, m := h.Declaration, h.Manifest
	at, te := time.Parse(time.RFC3339Nano, h.Expires)
	completed, ce := time.Parse(time.RFC3339Nano, d.Completed)
	if h.Profile != ContentProfile || !ids.Valid(h.Delivery) || e != nil || len(k) != ed25519.PublicKeySize || base64.RawURLEncoding.EncodeToString(k) != h.Public || d.Verify(ed25519.PublicKey(k)) != nil || h.ChunkBytes != ChunkBytes || h.Chunks != ChunkCount(d.OutputBytes) || d.OutputBytes > MaximumContentBytes || te != nil || ce != nil || !at.Equal(completed.Add(30*time.Minute)) || !canonicalTime(h.Expires) || m.Profile != executionlease.ManifestProfile || m.Mode != "PLAINTEXT" || m.Privacy != "STANDARD" || m.Digest() != d.Scope.ManifestHash || m.Job != d.Scope.Job || m.Attempt != d.Scope.Attempt || m.Order != d.Scope.Order || m.Seller != d.Scope.Seller || m.Device != d.Scope.Device || m.Resource != d.Scope.Resource || m.Generation != d.Scope.Generation || m.Fence != d.Scope.Fence || m.ContractHash != d.Scope.ContractHash || m.EnvironmentDigest != d.Scope.Environment || d.InputTokens > m.InputTokenLimit || d.OutputTokens > int64(m.Spec.MaxOutputTokens) {
		return errors.New("original bounded signed content head required")
	}
	if h.State != "UPLOADING" && h.State != "READY" && h.State != "RETIRED" {
		return errors.New("invalid original content state")
	}
	if h.End != nil && (h.State != "READY" || h.End.Validate(d.Finish) != nil) {
		return errors.New("original terminal content required")
	}
	return nil
}
func (e End) Validate(finish string) error {
	if finish == "stop" {
		if len(e.Stop) < 1 || len(e.Stop) > 256 || !utf8.ValidString(e.Stop) || strings.ContainsRune(e.Stop, 0) {
			return errors.New("original matched stop required")
		}
	} else if e.Stop != "" {
		return errors.New("unexpected matched stop")
	}
	return nil
}

type BeginContent struct {
	Scope        Scope  `json:"scope"`
	Delivery     string `json:"delivery_id"`
	IncludeUsage bool   `json:"include_usage"`
}
type ContentChunk struct {
	Profile  string `json:"profile_id"`
	Scope    Scope  `json:"scope"`
	Delivery string `json:"delivery_id"`
	Head     string `json:"head_digest"`
	Index    int    `json:"index"`
	Digest   string `json:"content_digest"`
	Content  []byte `json:"content"`
}

func (c ContentChunk) Validate() error {
	if c.Profile != ContentProfile || c.Scope.Validate() != nil || !ids.Valid(c.Delivery) || !executionlease.Hash(c.Head) || c.Index < 0 || c.Index >= MaximumContentBytes/ChunkBytes || len(c.Content) < 1 || len(c.Content) > ChunkBytes || !executionlease.Hash(c.Digest) || BytesDigest(c.Content) != c.Digest {
		return errors.New("bounded original content chunk required")
	}
	return nil
}

type SealContent struct {
	Scope    Scope  `json:"scope"`
	Delivery string `json:"delivery_id"`
	Head     string `json:"head_digest"`
	End      End    `json:"terminal"`
}
type ContentAck struct {
	Delivery string `json:"delivery_id"`
	Head     string `json:"head_digest"`
	Index    *int   `json:"index,omitempty"`
	Digest   string `json:"content_digest,omitempty"`
	State    string `json:"state"`
}
type ReadGrantRequest struct {
	Head string `json:"head_digest"`
}
type ReadGrant struct {
	Profile  string `json:"profile_id"`
	ID       string `json:"grant_id"`
	Order    string `json:"order_id"`
	Job      string `json:"job_id"`
	Seller   string `json:"seller_id"`
	Delivery string `json:"delivery_id"`
	Head     string `json:"head_digest"`
	Expires  string `json:"expires_at"`
}
type IssuedReadGrant struct {
	Grant  ReadGrant `json:"grant"`
	Secret string    `json:"read_secret"`
}
type ReadPacket struct {
	Grant  ReadGrant `json:"grant"`
	Origin string    `json:"api_origin"`
	Secret string    `json:"read_secret"`
}
type LocalReview struct {
	Profile  string `json:"profile_id"`
	Job      string `json:"job_id"`
	Delivery string `json:"delivery_id"`
	Revision int64  `json:"revision"`
	Manifest string `json:"manifest_hash"`
	Output   string `json:"output_digest"`
}
