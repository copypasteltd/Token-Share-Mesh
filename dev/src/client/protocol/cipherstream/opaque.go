package cipherstream

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"time"
)

// OpaqueSummary proves only the signed original ciphertext and structural
// bounds. It explicitly does not assert AEAD, END, private request validity,
// trusted usage, execution readiness or buyer acceptance.
type OpaqueSummary struct {
	Context      Context
	ManifestHash string
	CipherBytes  int64
	Records      uint64
}

func InspectOpaque(in io.Reader, expected Expected, manifest Manifest, now time.Time) (OpaqueSummary, error) {
	if in == nil || expected.Limits.validate(expected.Context.Purpose) != nil {
		return OpaqueSummary{}, ErrEnvelope
	}
	n, e := Counter(manifest.CipherBytes, false, 63)
	if e != nil || n > uint64(expected.Limits.WireBytes) {
		return OpaqueSummary{}, ErrLimit
	}
	r := wireReader{in: in, wire: sha256.New(), limit: int64(n)}
	h, digest, e := r.header(expected, now)
	if e != nil {
		return OpaqueSummary{}, e
	}
	count := uint64(0)
	for r.bytes < int64(n) {
		if count >= expected.Limits.Records {
			return OpaqueSummary{}, ErrLimit
		}
		if _, _, e = r.frame(count); e != nil {
			return OpaqueSummary{}, e
		}
		count++
	}
	if count < 2 || r.eof() != nil || manifest.Verify(h.Context, hex.EncodeToString(digest), hex.EncodeToString(r.wire.Sum(nil)), r.bytes, expected.SenderPublic, now) != nil {
		return OpaqueSummary{}, ErrEnvelope
	}
	return OpaqueSummary{Context: h.Context, ManifestHash: manifest.Hash(), CipherBytes: r.bytes, Records: count}, nil
}
