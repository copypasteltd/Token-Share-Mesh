package cipherstream

import (
	"crypto/ed25519"
	"crypto/rand"
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

type Writer struct {
	out                  io.Writer
	cipher               *sodium.State
	context              Context
	signer               ed25519.PrivateKey
	headerHash           [32]byte
	state                sequenceState
	limits               Limits
	wire, data           hash.Hash
	wireBytes, dataBytes int64
	next, dataRecords    uint64
	closed, manifested   bool
}

func NewWriter(out io.Writer, c Context, recipient []byte, signer ed25519.PrivateKey, l Limits, now time.Time) (_ *Writer, answer error) {
	if out == nil || c.StreamHeader != "" || len(signer) != ed25519.PrivateKeySize || len(recipient) != sodium.KeyBytes || Hash(recipient) != c.ReceiverFingerprint || l.validate(c.Purpose) != nil {
		return nil, ErrEnvelope
	}
	key := make([]byte, sodium.KeyBytes)
	defer clear(key)
	if _, e := rand.Read(key); e != nil {
		return nil, ErrEnvelope
	}
	state, header, e := sodium.NewPush(key)
	if e != nil {
		return nil, e
	}
	w := &Writer{out: out, cipher: state, signer: append(ed25519.PrivateKey(nil), signer...), wire: sha256.New(), data: sha256.New(), limits: l}
	defer func() {
		if answer != nil {
			w.Close()
		}
	}()
	c.StreamHeader = base64.RawURLEncoding.EncodeToString(header)
	if c.Validate(now) != nil {
		return nil, ErrEnvelope
	}
	w.context = c
	private, e := canonicaljson.Encode(envelopePayload{"P11_SEALED_STREAM_KEY_1", base64.RawURLEncoding.EncodeToString(key), contextHash(c), c.StreamHeader}, 4096-sodium.SealBytes)
	if e != nil {
		return nil, ErrEnvelope
	}
	defer clear(private)
	sealed, e := sodium.Seal(private, recipient)
	if e != nil {
		return nil, e
	}
	h := Header{Context: c, Sealed: base64.RawURLEncoding.EncodeToString(sealed), SealedHash: Hash(sealed), SignatureProfile: SignatureProfile}
	msg, e := signedBytes("HEADER_SIGNATURE", h.unsigned(), MaximumHeaderBytes)
	if e != nil {
		return nil, e
	}
	h.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(signer, msg))
	raw, e := canonicaljson.Encode(h, MaximumHeaderBytes)
	if e != nil {
		return nil, ErrEnvelope
	}
	unsigned, e := canonicaljson.Encode(h.unsigned(), MaximumHeaderBytes)
	if e != nil {
		return nil, ErrEnvelope
	}
	headerDigest := canonicaljson.Digest(domain+"HEADER_HASH\x00", unsigned)
	hashBytes, _ := hex.DecodeString(headerDigest)
	copy(w.headerHash[:], hashBytes)
	prefix := binary.BigEndian.AppendUint32(nil, uint32(len(raw)))
	if w.write(prefix) != nil || w.write(raw) != nil {
		return nil, ErrRecord
	}
	return w, nil
}
func (w *Writer) Context() Context { return w.context }
func (w *Writer) write(raw []byte) error {
	if int64(len(raw)) > w.limits.WireBytes-w.wireBytes {
		return ErrLimit
	}
	for len(raw) > 0 {
		n, e := w.out.Write(raw)
		if n < 0 || n > len(raw) {
			return ErrRecord
		}
		if n > 0 {
			_, _ = w.wire.Write(raw[:n])
			w.wireBytes += int64(n)
			raw = raw[n:]
		}
		if e != nil || n == 0 {
			return ErrRecord
		}
	}
	return nil
}
func (w *Writer) put(typ byte, payload []byte) (answer error) {
	if w == nil || w.closed || w.state.ended {
		return ErrClosed
	}
	defer func() {
		if answer != nil {
			w.Close()
		}
	}()
	if w.next >= w.limits.Records || w.next == ^uint64(0) || len(payload) > MaximumPayloadBytes {
		return ErrLimit
	}
	nextState := w.state
	if _, e := nextState.accept(w.context, typ, payload, typ == EndRecord); e != nil {
		return e
	}
	if typ == DataRecord && int64(len(payload)) > w.limits.DataBytes-w.dataBytes {
		return ErrLimit
	}
	bodyLength := uint32(8 + 8 + len(payload) + sodium.AuthenticationBytes)
	if int64(4+bodyLength) > w.limits.WireBytes-w.wireBytes {
		return ErrLimit
	}
	inner := innerRecord(typ, payload)
	defer clear(inner)
	sealed, e := w.cipher.Push(inner, recordAAD(w.headerHash, bodyLength, w.next), typ == EndRecord)
	if e != nil {
		return ErrRecord
	}
	wire := binary.BigEndian.AppendUint32(nil, bodyLength)
	wire = binary.BigEndian.AppendUint64(wire, w.next)
	wire = append(wire, sealed...)
	if e = w.write(wire); e != nil {
		return e
	}
	w.state = nextState
	w.next++
	if typ == DataRecord {
		_, _ = w.data.Write(payload)
		w.dataBytes += int64(len(payload))
		w.dataRecords++
	}
	return nil
}
func (w *Writer) Headers(h PrivateHeaders) error {
	raw, e := canonicaljson.Encode(h, 4096)
	if e != nil {
		return e
	}
	defer clear(raw)
	return w.put(HeadersRecord, raw)
}
func (w *Writer) Data(payload []byte) error {
	if len(payload) == 0 {
		return ErrRecord
	}
	for len(payload) > 0 {
		n := min(len(payload), MaximumPayloadBytes)
		if e := w.put(DataRecord, payload[:n]); e != nil {
			return e
		}
		payload = payload[n:]
	}
	return nil
}
func (w *Writer) Usage(u PrivateUsage) error {
	raw, e := canonicaljson.Encode(u, 4096)
	if e != nil {
		return e
	}
	defer clear(raw)
	return w.put(UsageRecord, raw)
}
func (w *Writer) Error(v PrivateError) error {
	raw, e := canonicaljson.Encode(v, 8192)
	if e != nil {
		return e
	}
	defer clear(raw)
	return w.put(ErrorRecord, raw)
}
func (w *Writer) Cancel(v PrivateCancel) error {
	raw, e := canonicaljson.Encode(v, 4096)
	if e != nil {
		return e
	}
	defer clear(raw)
	return w.put(CancelRecord, raw)
}
func (w *Writer) End(outcome string) error {
	if w == nil {
		return ErrClosed
	}
	v := End{"P11_PRIVATE_END_1", outcome, strconv.FormatUint(w.next, 10), strconv.FormatUint(w.dataRecords, 10), strconv.FormatInt(w.dataBytes, 10), hex.EncodeToString(w.data.Sum(nil))}
	raw, e := canonicaljson.Encode(v, 4096)
	if e != nil {
		return e
	}
	defer clear(raw)
	return w.put(EndRecord, raw)
}
func (w *Writer) Close() {
	if w != nil && !w.closed {
		w.closed = true
		if w.cipher != nil {
			w.cipher.Close()
		}
		clear(w.signer)
		w.signer = nil
	}
}
