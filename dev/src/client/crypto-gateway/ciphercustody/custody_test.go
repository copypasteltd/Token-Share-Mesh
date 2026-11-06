package ciphercustody

// SOURCE_ONLY. This fixture signs opaque structural bytes, deliberately NOT a
// valid secretstream. It tests custody/opaque-boundary/recovery only and cannot
// stand in for native encryption, private END, a device or business acceptance.
import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"p11.local/compute/client/crypto-gateway/sodium"
	"p11.local/compute/client/protocol/cipherstream"
	"p11.local/compute/shared/canonicaljson"
)

type memoryJournal struct {
	mu              sync.Mutex
	raw             []byte
	commits, failAt int
	writeThenFail   bool
}

func (j *memoryJournal) Read() ([]byte, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.raw == nil {
		return nil, os.ErrNotExist
	}
	return bytes.Clone(j.raw), nil
}
func (j *memoryJournal) Commit(raw []byte) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.commits++
	if j.commits == j.failAt && !j.writeThenFail {
		return ErrState
	}
	j.raw = bytes.Clone(raw)
	if j.commits == j.failAt {
		return ErrState
	}
	return nil
}

type memoryObject struct {
	mu  sync.Mutex
	raw []byte
}
type memoryFiles struct {
	mu      sync.Mutex
	objects map[string]*memoryObject
	removed int
}

func newMemoryFiles() *memoryFiles { return &memoryFiles{objects: map[string]*memoryObject{}} }
func (m *memoryFiles) Create(name string) (File, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !FileNameValid(name) || m.objects[name] != nil {
		return nil, ErrState
	}
	o := &memoryObject{}
	m.objects[name] = o
	return &memoryFile{o: o, name: name, writable: true}, nil
}
func (m *memoryFiles) Open(name string) (File, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o := m.objects[name]
	if o == nil {
		return nil, os.ErrNotExist
	}
	return &memoryFile{o: o, name: name}, nil
}
func (m *memoryFiles) Remove(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.objects[name] == nil {
		return os.ErrNotExist
	}
	delete(m.objects, name)
	m.removed++
	return nil
}
func (m *memoryFiles) Names() ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := []string{}
	for name := range m.objects {
		names = append(names, name)
	}
	return names, nil
}

type memoryFile struct {
	mu               sync.Mutex
	o                *memoryObject
	name             string
	at               int64
	writable, closed bool
}

func (f *memoryFile) Read(raw []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return 0, os.ErrClosed
	}
	f.o.mu.Lock()
	defer f.o.mu.Unlock()
	if f.at >= int64(len(f.o.raw)) {
		return 0, io.EOF
	}
	n := copy(raw, f.o.raw[f.at:])
	f.at += int64(n)
	return n, nil
}
func (f *memoryFile) Write(raw []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed || !f.writable {
		return 0, os.ErrClosed
	}
	f.o.mu.Lock()
	defer f.o.mu.Unlock()
	if f.at != int64(len(f.o.raw)) {
		return 0, ErrState
	}
	f.o.raw = append(f.o.raw, raw...)
	f.at += int64(len(raw))
	return len(raw), nil
}
func (f *memoryFile) Seek(at int64, whence int) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed || whence != io.SeekStart || at < 0 {
		return 0, ErrState
	}
	f.at = at
	return at, nil
}
func (f *memoryFile) Sync() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return os.ErrClosed
	}
	return nil
}
func (f *memoryFile) Close() error { f.mu.Lock(); defer f.mu.Unlock(); f.closed = true; return nil }
func (f *memoryFile) Stat() (os.FileInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil, os.ErrClosed
	}
	f.o.mu.Lock()
	defer f.o.mu.Unlock()
	return fixtureInfo{f.name, int64(len(f.o.raw))}, nil
}

type fixtureInfo struct {
	name string
	size int64
}

func (i fixtureInfo) Name() string       { return i.name }
func (i fixtureInfo) Size() int64        { return i.size }
func (i fixtureInfo) Mode() os.FileMode  { return 0600 }
func (i fixtureInfo) ModTime() time.Time { return time.Time{} }
func (i fixtureInfo) IsDir() bool        { return false }
func (i fixtureInfo) Sys() any           { return nil }

func opaqueFixture(t *testing.T) (Descriptor, []byte, cipherstream.Manifest, time.Time) {
	t.Helper()
	now := time.Now().UTC()
	expiry := now.Add(time.Minute).Format(time.RFC3339Nano)
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{17}, 32))
	defer clear(private)
	recipient := bytes.Repeat([]byte{23}, 32)
	c := cipherstream.Context{Profile: cipherstream.Profile, Suite: sodium.Suite, Canonicalization: canonicaljson.Profile, Contract: strings.Repeat("a", 64), Order: "order_original", Job: "job_original", Attempt: "attempt_original", Fence: "1", Seller: "seller_original", Buyer: "buyer_original", SellerEndpoint: "seller_endpoint", BuyerEndpoint: "buyer_endpoint", BuyerResultKey: "buyer_result_key", BuyerResultFingerprint: strings.Repeat("c", 64), Sender: "buyer_endpoint", SenderKey: "buyer_signing_key", Receiver: "seller_endpoint", ReceiverKey: "seller_content_key", ReceiverFingerprint: cipherstream.Hash(recipient), Environment: strings.Repeat("b", 64), LocalPolicy: "local_policy", Mode: "E2E", Privacy: "PLATFORM_BLIND", APIProfile: "P11_CHAT_TEXT_NATIVE_1", Direction: "BUYER_TO_SELLER", Purpose: "INPUT", Object: "object_original", Stream: "stream_original", Epoch: "1", Deadline: expiry, HardDeadline: expiry}
	d := Descriptor{Profile: Profile, Origin: "OUTBOUND", Context: c, SenderPublic: base64.RawURLEncoding.EncodeToString(private.Public().(ed25519.PublicKey)), RecipientPublic: base64.RawURLEncoding.EncodeToString(recipient), LocalBundle: strings.Repeat("d", 64), PeerBundle: strings.Repeat("e", 64), PublicModel: "public_model", DataBytes: "2097152", WireBytes: strconv.FormatInt(cipherstream.MaximumObjectBytes, 10), Records: "131072", ManifestExpiry: expiry}
	c.StreamHeader = base64.RawURLEncoding.EncodeToString(make([]byte, 24))
	sealed := bytes.Repeat([]byte{33}, 49)
	u := map[string]any{"context": c, "sealed_key": base64.RawURLEncoding.EncodeToString(sealed), "sealed_key_sha256": cipherstream.Hash(sealed), "signature_profile_id": cipherstream.SignatureProfile}
	unsigned, e := canonicaljson.Encode(u, cipherstream.MaximumHeaderBytes)
	if e != nil {
		t.Fatal(e)
	}
	const domain = "P11/E2E/BINARY_STREAM_1/"
	u["signature"] = base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, append([]byte(domain+"HEADER_SIGNATURE\x00"), unsigned...)))
	header, e := canonicaljson.Encode(u, cipherstream.MaximumHeaderBytes)
	if e != nil {
		t.Fatal(e)
	}
	wire := binary.BigEndian.AppendUint32(nil, uint32(len(header)))
	wire = append(wire, header...)
	for seq := uint64(0); seq < 2; seq++ {
		wire = binary.BigEndian.AppendUint32(wire, 33)
		wire = binary.BigEndian.AppendUint64(wire, seq)
		wire = append(wire, make([]byte, 25)...)
	}
	contextBytes, e := canonicaljson.Encode(c, cipherstream.MaximumHeaderBytes)
	if e != nil {
		t.Fatal(e)
	}
	m := cipherstream.Manifest{Profile: "P11_CIPHER_MANIFEST_1", ContextHash: canonicaljson.Digest(domain+"CONTEXT\x00", contextBytes), HeaderHash: canonicaljson.Digest(domain+"HEADER_HASH\x00", unsigned), CipherBytes: strconv.Itoa(len(wire)), CipherHash: cipherstream.Hash(wire), Signer: c.SenderKey, Expires: expiry, SignatureProfile: cipherstream.SignatureProfile}
	mu := map[string]any{"profile_id": m.Profile, "context_hash": m.ContextHash, "header_hash": m.HeaderHash, "cipher_bytes": m.CipherBytes, "cipher_sha256": m.CipherHash, "signer_key_id": m.Signer, "expires_at": m.Expires, "signature_profile_id": m.SignatureProfile}
	manifestBytes, e := canonicaljson.Encode(mu, 8192)
	if e != nil {
		t.Fatal(e)
	}
	m.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, append([]byte(domain+"MANIFEST_SIGNATURE\x00"), manifestBytes...)))
	return d, wire, m, now
}
func newCustody(t *testing.T) (*Store, *memoryJournal, *memoryFiles) {
	t.Helper()
	j := &memoryJournal{}
	files := newMemoryFiles()
	policy := Policy{Profile: Profile, Bytes: "536870912", Objects: 64}
	s, e := OpenOwned(j, files, bytes.Repeat([]byte{37}, 32), strings.Repeat("f", 64), &policy)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.Close)
	return s, j, files
}
func reopenCustody(t *testing.T, s *Store, j *memoryJournal, files *memoryFiles) *Store {
	t.Helper()
	s.Close()
	j.mu.Lock()
	j.failAt = 0
	j.writeThenFail = false
	j.mu.Unlock()
	next, e := OpenOwned(j, files, bytes.Repeat([]byte{37}, 32), strings.Repeat("f", 64), nil)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(next.Close)
	return next
}
func produceOpaque(t *testing.T, s *Store, d Descriptor, wire []byte, m cipherstream.Manifest, now time.Time) Result {
	t.Helper()
	r, e := s.Produce(context.Background(), d, now, func(_ context.Context, w io.Writer) (cipherstream.Manifest, error) {
		_, e := w.Write(wire)
		return m, e
	})
	if e != nil {
		t.Fatal(e)
	}
	return r
}

func TestCipherCustodyIntentIsDurableBeforeProducerAndReplayDoesNotReencrypt(t *testing.T) {
	s, j, _ := newCustody(t)
	d, wire, m, now := opaqueFixture(t)
	calls := 0
	build := func(_ context.Context, w io.Writer) (cipherstream.Manifest, error) {
		calls++
		raw, _ := j.Read()
		block, _ := aes.NewCipher(bytes.Repeat([]byte{37}, 32))
		g, _ := cipher.NewGCM(block)
		plain, e := g.Open(nil, raw[:g.NonceSize()], raw[g.NonceSize():], []byte(Profile+"\x00"+strings.Repeat("f", 64)))
		if e != nil {
			t.Fatal(e)
		}
		defer clear(plain)
		var original diskState
		if json.Unmarshal(plain, &original) != nil || len(original.Objects) != 1 || original.Objects[0].State != "WRITING" {
			t.Fatal("no durable original intent")
		}
		_, e = w.Write(wire)
		return m, e
	}
	first, e := s.Produce(context.Background(), d, now, build)
	if e != nil {
		t.Fatal(e)
	}
	second, e := s.Produce(context.Background(), d, now, build)
	if e != nil || calls != 1 || !second.Replayed || first.Handle != second.Handle || first.Manifest != second.Manifest {
		t.Fatal("retry recreated original cipher", e)
	}
}
func TestCipherCustodyUnknownIntentCommitNeverCallsProducerOrRecreatesAfterReopen(t *testing.T) {
	s, j, files := newCustody(t)
	d, _, _, now := opaqueFixture(t)
	j.failAt = 2
	j.writeThenFail = true
	called := false
	build := func(context.Context, io.Writer) (cipherstream.Manifest, error) {
		called = true
		return cipherstream.Manifest{}, nil
	}
	if _, e := s.Produce(context.Background(), d, now, build); e == nil || called {
		t.Fatal("unknown intent commit called producer")
	}
	s = reopenCustody(t, s, j, files)
	if _, e := s.Produce(context.Background(), d, now, build); !errors.Is(e, ErrInterrupted) || called {
		t.Fatal("lost original intent regenerated", e)
	}
}
func TestCipherCustodyInterruptedBodyStaysUnpublishableAndKeepsReservation(t *testing.T) {
	s, j, files := newCustody(t)
	d, wire, _, now := opaqueFixture(t)
	_, e := s.Produce(context.Background(), d, now, func(_ context.Context, w io.Writer) (cipherstream.Manifest, error) {
		_, _ = w.Write(wire[:37])
		j.mu.Lock()
		j.failAt = j.commits + 1
		j.mu.Unlock()
		return cipherstream.Manifest{}, io.ErrUnexpectedEOF
	})
	if e == nil {
		t.Fatal("partial original published")
	}
	s = reopenCustody(t, s, j, files)
	snapshot, e := s.Snapshot()
	if e != nil || len(snapshot.Objects) != 1 || snapshot.Objects[0].State != "INTERRUPTED" || snapshot.Objects[0].Bytes != "37" || snapshot.Reserved != d.WireBytes {
		t.Fatal("partial custody lost quota or became READY", e)
	}
	if _, e = s.Produce(context.Background(), d, now, func(context.Context, io.Writer) (cipherstream.Manifest, error) {
		t.Fatal("partial stream recreated")
		return cipherstream.Manifest{}, nil
	}); !errors.Is(e, ErrInterrupted) {
		t.Fatal(e)
	}
}
func TestCipherCustodyExactOriginalBytesSurviveReopenAndRangeRetransmit(t *testing.T) {
	s, j, files := newCustody(t)
	d, wire, m, now := opaqueFixture(t)
	record := produceOpaque(t, s, d, wire, m, now)
	s = reopenCustody(t, s, j, files)
	var output bytes.Buffer
	offset := int64(17)
	length := int64(len(wire)) - offset
	n, e := s.CopyRange(context.Background(), d, record.Handle, offset, length, &output, now)
	if e != nil || n != length || !bytes.Equal(output.Bytes(), wire[offset:]) {
		t.Fatal("original bytes changed", e)
	}
}
func TestCipherCustodyChangedOwnerScopeManifestAndBoundsExposeNoBytes(t *testing.T) {
	s, _, _ := newCustody(t)
	d, wire, m, now := opaqueFixture(t)
	record := produceOpaque(t, s, d, wire, m, now)
	foreign := d
	foreign.Context.Seller = "seller_foreign"
	for _, candidate := range []struct {
		d              Descriptor
		h              Handle
		offset, length int64
	}{{foreign, record.Handle, 0, 1}, {d, Handle{record.Handle.ID, record.Handle.DescriptorHash, strings.Repeat("0", 64)}, 0, 1}, {d, record.Handle, -1, 1}, {d, record.Handle, 0, int64(len(wire)) + 1}} {
		var output bytes.Buffer
		if _, e := s.CopyRange(context.Background(), candidate.d, candidate.h, candidate.offset, candidate.length, &output, now); e == nil || output.Len() != 0 {
			t.Fatal("foreign or out-of-range bytes exposed")
		}
	}
}
func TestCipherCustodyChangedOnDiskCipherFailsBeforeDestination(t *testing.T) {
	s, _, files := newCustody(t)
	d, wire, m, now := opaqueFixture(t)
	record := produceOpaque(t, s, d, wire, m, now)
	names, _ := files.Names()
	o := files.objects[names[0]]
	o.mu.Lock()
	o.raw[len(o.raw)-1] ^= 1
	o.mu.Unlock()
	var output bytes.Buffer
	if _, e := s.CopyRange(context.Background(), d, record.Handle, 0, int64(len(wire)), &output, now); e == nil || output.Len() != 0 {
		t.Fatal("changed cipher reached destination")
	}
}
func TestCipherCustodyImportCannotClaimPrivateENDOrReplaceOriginalManifest(t *testing.T) {
	s, _, _ := newCustody(t)
	d, wire, m, now := opaqueFixture(t)
	d.Origin = "INBOUND"
	d.InboundManifest = m.Hash()
	r, e := s.Import(context.Background(), d, m, bytes.NewReader(wire), now)
	if e != nil || r.Manifest != m {
		t.Fatal("opaque original rejected", e)
	}
	// The dummy ciphertext is intentionally not AEAD-valid. This success is
	// only signed opaque custody and cannot replace ConsumeStored/Finish.
	m.Expires = now.Add(30 * time.Second).Format(time.RFC3339Nano)
	if _, e = s.Import(context.Background(), d, m, bytes.NewReader(wire), now); e == nil {
		t.Fatal("original manifest replaced")
	}
}
func TestCipherCustodyReservesFullOriginalBoundBeforeOpeningFile(t *testing.T) {
	s, _, files := newCustody(t)
	d, wire, m, now := opaqueFixture(t)
	s.mu.Lock()
	s.state.Policy.Bytes = d.WireBytes
	s.mu.Unlock()
	_ = produceOpaque(t, s, d, wire, m, now)
	other := d
	other.Context.Object = "object_other"
	called := false
	if _, e := s.Produce(context.Background(), other, now, func(context.Context, io.Writer) (cipherstream.Manifest, error) { called = true; return m, nil }); !errors.Is(e, ErrCapacity) || called {
		t.Fatal("quota bypass", e)
	}
	names, _ := files.Names()
	if len(names) != 1 {
		t.Fatal("quota failure created a second file")
	}
}
func TestCipherCustodyExpiresOnlyAfterCancelAndActualReaderJoin(t *testing.T) {
	s, _, files := newCustody(t)
	d, wire, m, now := opaqueFixture(t)
	record := produceOpaque(t, s, d, wire, m, now)
	entered, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	readDone := make(chan error, 1)
	go func() {
		readDone <- s.WithOriginal(context.Background(), d, record.Handle, now, func(ctx context.Context, _ io.Reader) error {
			close(entered)
			<-ctx.Done()
			close(canceled)
			<-release
			return ctx.Err()
		})
	}()
	<-entered
	expired := make(chan error, 1)
	go func() { _, e := s.Expire(context.Background(), now.Add(2*time.Minute)); expired <- e }()
	<-canceled
	select {
	case <-expired:
		t.Fatal("expiry returned before actual callback exit")
	default:
	}
	files.mu.Lock()
	removed := files.removed
	files.mu.Unlock()
	if removed != 0 {
		t.Fatal("cipher deleted under an owned reader")
	}
	releaseOnce.Do(func() { close(release) })
	<-readDone
	if e := <-expired; e != nil {
		t.Fatal(e)
	}
	snapshot, e := s.Snapshot()
	if e != nil || len(snapshot.Objects) != 0 || snapshot.Reserved != "0" {
		t.Fatal("expired quota not reclaimed", e)
	}
	if _, e = s.Produce(context.Background(), d, now.Add(2*time.Minute), func(context.Context, io.Writer) (cipherstream.Manifest, error) {
		t.Fatal("expired descriptor recreated")
		return m, nil
	}); e == nil {
		t.Fatal("expired original resurrected")
	}
}
func TestCipherCustodyCloseCancelsThenJoinsWithoutDroppingJournalKeyEarly(t *testing.T) {
	s, _, _ := newCustody(t)
	d, wire, m, now := opaqueFixture(t)
	record := produceOpaque(t, s, d, wire, m, now)
	entered, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	readDone := make(chan error, 1)
	go func() {
		readDone <- s.WithOriginal(context.Background(), d, record.Handle, now, func(ctx context.Context, _ io.Reader) error {
			close(entered)
			<-ctx.Done()
			close(canceled)
			<-release
			return ctx.Err()
		})
	}()
	<-entered
	closed := make(chan struct{})
	go func() { s.Close(); close(closed) }()
	<-canceled
	s.mu.Lock()
	keyPresent := len(s.key) == 32
	s.mu.Unlock()
	if !keyPresent {
		t.Fatal("journal key cleared under owned callback")
	}
	select {
	case <-closed:
		t.Fatal("fake join")
	default:
	}
	releaseOnce.Do(func() { close(release) })
	<-readDone
	<-closed
}
func TestCipherCustodyUnknownFilesAndMissingJournalCannotBeAdopted(t *testing.T) {
	s, j, files := newCustody(t)
	s.Close()
	name := "cipher-object-" + strings.Repeat("1", 32) + ".bin"
	f, e := files.Create(name)
	if e != nil {
		t.Fatal(e)
	}
	f.Close()
	if _, e = OpenOwned(j, files, bytes.Repeat([]byte{37}, 32), strings.Repeat("f", 64), nil); e == nil {
		t.Fatal("unknown original file adopted")
	}
	j.raw = nil
	policy := Policy{Profile: Profile, Bytes: "536870912", Objects: 64}
	if _, e = OpenOwned(j, files, bytes.Repeat([]byte{37}, 32), strings.Repeat("f", 64), &policy); e == nil {
		t.Fatal("missing original journal recreated around orphan file")
	}
	names, _ := files.Names()
	if len(names) != 1 {
		t.Fatal("unknown cipher silently erased")
	}
}
func TestCipherCustodyWrongInstallationOrMasterCannotOpenMetadata(t *testing.T) {
	s, j, files := newCustody(t)
	s.Close()
	for _, candidate := range []struct {
		key     []byte
		binding string
	}{{bytes.Repeat([]byte{38}, 32), strings.Repeat("f", 64)}, {bytes.Repeat([]byte{37}, 32), strings.Repeat("e", 64)}} {
		if _, e := OpenOwned(j, files, candidate.key, candidate.binding, nil); e == nil {
			t.Fatal("foreign root accepted")
		}
	}
}

func TestCipherCustodySingleCleanupCanCancelBothFullPayloadSlots(t *testing.T) {
	s, _, _ := newCustody(t)
	d, wire, m, now := opaqueFixture(t)
	record := produceOpaque(t, s, d, wire, m, now)
	entered, canceled, release := make(chan struct{}, 2), make(chan struct{}, 2), make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	reads := make(chan error, 2)
	for i := 0; i < MaximumActiveUses; i++ {
		go func() {
			reads <- s.WithOriginal(context.Background(), d, record.Handle, now, func(ctx context.Context, _ io.Reader) error {
				entered <- struct{}{}
				<-ctx.Done()
				canceled <- struct{}{}
				<-release
				return ctx.Err()
			})
		}()
	}
	for i := 0; i < MaximumActiveUses; i++ {
		<-entered
	}
	expired := make(chan error, 1)
	go func() { _, e := s.Expire(context.Background(), now.Add(2*time.Minute)); expired <- e }()
	for i := 0; i < MaximumActiveUses; i++ {
		<-canceled
	}
	if _, e := s.Expire(context.Background(), now.Add(2*time.Minute)); !errors.Is(e, ErrPending) {
		t.Fatal("concurrent cleanup raced the original maintenance owner", e)
	}
	select {
	case <-expired:
		t.Fatal("cleanup did not wait for both actual readers")
	default:
	}
	releaseOnce.Do(func() { close(release) })
	for i := 0; i < MaximumActiveUses; i++ {
		<-reads
	}
	if e := <-expired; e != nil {
		t.Fatal(e)
	}
}

type failingDestination struct{}

func (failingDestination) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestCipherCustodyFailedTransportRetainsOriginalReadyBytesForRetry(t *testing.T) {
	s, j, files := newCustody(t)
	d, wire, m, now := opaqueFixture(t)
	record := produceOpaque(t, s, d, wire, m, now)
	if n, e := s.CopyRange(context.Background(), d, record.Handle, 0, int64(len(wire)), failingDestination{}, now); e == nil || n != 0 {
		t.Fatal("failed transport reported success")
	}
	s = reopenCustody(t, s, j, files)
	var output bytes.Buffer
	if _, e := s.CopyRange(context.Background(), d, record.Handle, 0, int64(len(wire)), &output, now); e != nil || !bytes.Equal(output.Bytes(), wire) {
		t.Fatal("failed transport lost or changed original ciphertext", e)
	}
	if _, e := s.Produce(context.Background(), d, now, func(context.Context, io.Writer) (cipherstream.Manifest, error) {
		t.Fatal("transport retry invoked encryption producer")
		return m, nil
	}); e != nil {
		t.Fatal(e)
	}
}
