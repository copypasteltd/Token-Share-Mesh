package cipherstream

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"hash"
	"io"
	"p11.local/compute/client/crypto-gateway/sodium"
	"p11.local/compute/shared/canonicaljson"
	"strconv"
	"time"
)

func decimalBytes(n int64) string { return strconv.FormatInt(n, 10) }

type wireReader struct {
	in    io.Reader
	wire  hash.Hash
	bytes int64
	limit int64
}

func (r *wireReader) read(raw []byte) error {
	if int64(len(raw)) > r.limit-r.bytes {
		return ErrLimit
	}
	n, e := io.ReadFull(r.in, raw)
	if n > 0 {
		_, _ = r.wire.Write(raw[:n])
		r.bytes += int64(n)
	}
	if e != nil {
		return ErrRecord
	}
	return nil
}
func (r *wireReader) header(expected Expected, now time.Time) (Header, []byte, error) {
	var prefix [4]byte
	if e := r.read(prefix[:]); e != nil {
		return Header{}, nil, e
	}
	n := binary.BigEndian.Uint32(prefix[:])
	if n < 2 || n > MaximumHeaderBytes || int64(n) > r.limit-r.bytes {
		return Header{}, nil, ErrLimit
	}
	raw := make([]byte, int(n))
	if e := r.read(raw); e != nil {
		return Header{}, nil, e
	}
	h, e := ParseHeader(raw, expected, now)
	if e != nil {
		return Header{}, nil, e
	}
	unsigned, e := canonicaljson.Encode(h.unsigned(), MaximumHeaderBytes)
	if e != nil {
		return Header{}, nil, ErrEnvelope
	}
	hashHex := canonicaljson.Digest(domain+"HEADER_HASH\x00", unsigned)
	digest, _ := hex.DecodeString(hashHex)
	return h, digest, nil
}
func (r *wireReader) frame(sequence uint64) ([]byte, uint32, error) {
	var prefix [4]byte
	if e := r.read(prefix[:]); e != nil {
		return nil, 0, e
	}
	n := binary.BigEndian.Uint32(prefix[:])
	if n < MinimumRecordBody || n > MaximumRecordBody || int64(n) > r.limit-r.bytes {
		return nil, 0, ErrLimit
	}
	raw := make([]byte, int(n))
	if e := r.read(raw); e != nil {
		return nil, 0, e
	}
	if binary.BigEndian.Uint64(raw[:8]) != sequence {
		return nil, 0, ErrRecord
	}
	return raw, n, nil
}
func (r *wireReader) eof() error {
	var one [1]byte
	n, e := io.ReadFull(r.in, one[:])
	if n != 0 || e != io.EOF {
		return ErrRecord
	}
	return nil
}

type Record struct {
	Type    byte
	Payload []byte
	End     *End
}

func (r *Record) Clear() { clear(r.Payload); r.Payload = nil; r.End = nil }

type Reader struct {
	wire              wireReader
	cipher            *sodium.State
	header            Header
	headerHash        [32]byte
	expected          Expected
	state             sequenceState
	next, dataRecords uint64
	dataBytes         int64
	data              hash.Hash
	closed, complete  bool
}

func NewReader(in io.Reader, expected Expected, secret []byte, now time.Time) (_ *Reader, answer error) {
	if in == nil || len(secret) != sodium.KeyBytes || expected.Limits.validate(expected.Context.Purpose) != nil {
		return nil, ErrEnvelope
	}
	r := &Reader{wire: wireReader{in: in, wire: sha256.New(), limit: expected.Limits.WireBytes}, expected: expected, data: sha256.New()}
	defer func() {
		if answer != nil {
			r.Close()
		}
	}()
	h, digest, e := r.wire.header(expected, now)
	if e != nil {
		return nil, e
	}
	r.header = h
	copy(r.headerHash[:], digest)
	sealed, _ := base64.RawURLEncoding.Strict().DecodeString(h.Sealed)
	private, e := sodium.Unseal(sealed, expected.RecipientPublic, secret)
	if e != nil {
		return nil, ErrEnvelope
	}
	defer clear(private)
	var payload envelopePayload
	if canonicaljson.DecodeExact(private, &payload, 4096) != nil || payload.Profile != "P11_SEALED_STREAM_KEY_1" || payload.ContextHash != contextHash(h.Context) || payload.StreamHeader != h.Context.StreamHeader {
		return nil, ErrEnvelope
	}
	key, e := decode64(payload.Key, sodium.KeyBytes)
	if e != nil {
		return nil, e
	}
	defer clear(key)
	streamHeader, _ := decode64(payload.StreamHeader, sodium.HeaderBytes)
	r.cipher, e = sodium.NewPull(key, streamHeader)
	if e != nil {
		return nil, e
	}
	return r, nil
}
func (r *Reader) Context() Context { return r.header.Context }
func (r *Reader) Complete() bool   { return r != nil && r.complete }
func (r *Reader) Next() (_ Record, answer error) {
	if r == nil || r.closed || r.state.ended {
		return Record{}, ErrClosed
	}
	defer func() {
		if answer != nil {
			r.Close()
		}
	}()
	if r.next >= r.expected.Limits.Records || r.next == ^uint64(0) {
		return Record{}, ErrLimit
	}
	body, length, e := r.wire.frame(r.next)
	if e != nil {
		return Record{}, e
	}
	plain, final, e := r.cipher.Pull(body[8:], recordAAD(r.headerHash, length, r.next))
	if e != nil {
		return Record{}, ErrRecord
	}
	defer clear(plain)
	if len(plain) < 8 || plain[1] != 0 || binary.BigEndian.Uint16(plain[2:4]) != 0 || uint64(binary.BigEndian.Uint32(plain[4:8])) != uint64(len(plain)-8) {
		return Record{}, ErrRecord
	}
	typ, payload := plain[0], plain[8:]
	nextState := r.state
	end, e := nextState.accept(r.header.Context, typ, payload, final)
	if e != nil {
		return Record{}, e
	}
	if end != nil && verifyEnd(*end, r.next, r.dataRecords, r.dataBytes, hex.EncodeToString(r.data.Sum(nil))) != nil {
		return Record{}, ErrRecord
	}
	if typ == DataRecord {
		if int64(len(payload)) > r.expected.Limits.DataBytes-r.dataBytes {
			return Record{}, ErrLimit
		}
		r.dataBytes += int64(len(payload))
		r.dataRecords++
		_, _ = r.data.Write(payload)
	}
	r.state = nextState
	r.next++
	return Record{Type: typ, Payload: append([]byte(nil), payload...), End: end}, nil
}

// Finish requires the matching signed outer manifest and actual object EOF.
// Previously authenticated prefixes are not a successful complete response.
func (r *Reader) Finish(manifest Manifest, now time.Time) error {
	if r == nil || r.closed || !r.state.ended {
		return ErrRecord
	}
	defer r.Close()
	if r.wire.eof() != nil || manifest.Verify(r.header.Context, hex.EncodeToString(r.headerHash[:]), hex.EncodeToString(r.wire.wire.Sum(nil)), r.wire.bytes, r.expected.SenderPublic, now) != nil {
		return ErrRecord
	}
	r.complete = true
	return nil
}
func (r *Reader) Close() {
	if r != nil && !r.closed {
		r.closed = true
		if r.cipher != nil {
			r.cipher.Close()
		}
	}
}
