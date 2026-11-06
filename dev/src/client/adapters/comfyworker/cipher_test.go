package comfyworker

// SOURCE_ONLY: these definitions are for the later full-scope validation.
// They use synthetic authenticated bytes, never a model, GPU or worker.
import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

type cipherFixture struct {
	root                  *os.Root
	scope                 Scope
	raw, plain, disk, key []byte
	name                  string
}

// This fixture spells out the Python-compatible format independently of the
// reader's constants: 48-byte header, 9-byte records, 48-byte END and 16-byte
// GCM tags. A wrong END size in either implementation must fail the tests.
func mediaFixture(t *testing.T, size int) *cipherFixture {
	t.Helper()
	s := Scope{Session: "session_original", Invocation: "invoke_original", LocalGeneration: "19", Environment: strings.Repeat("a", 64), Recipe: strings.Repeat("b", 64)}
	canonical := []byte(`{"environment_digest":"` + s.Environment + `","invocation_id":"invoke_original","local_generation":"19","recipe_digest":"` + s.Recipe + `","session_id":"session_original"}`)
	scopeHash := sha256.Sum256(canonical)
	key := bytes.Repeat([]byte{0x93}, 32)
	block, e := aes.NewCipher(key)
	if e != nil {
		t.Fatal(e)
	}
	aead, e := cipher.NewGCM(block)
	if e != nil {
		t.Fatal(e)
	}
	header := append([]byte("P11CS01\x00"), []byte{1, 3, 5, 7, 9, 11, 13, 15}...)
	header = append(header, scopeHash[:]...)
	disk := append([]byte(nil), header...)
	plain := make([]byte, size)
	for n := range plain {
		plain[n] = byte(n % 251)
	}
	count := uint32(0)
	record := func(kind byte, data []byte) {
		rh := make([]byte, 9)
		binary.LittleEndian.PutUint32(rh[:4], count)
		rh[4] = kind
		binary.LittleEndian.PutUint32(rh[5:], uint32(len(data)))
		nonce := append([]byte(nil), header[8:16]...)
		nonce = binary.LittleEndian.AppendUint32(nonce, count)
		aad := append(append([]byte(nil), header...), rh...)
		disk = append(disk, rh...)
		disk = append(disk, aead.Seal(nil, nonce, data, aad)...)
		count++
	}
	for offset := 0; offset < len(plain); offset += 65536 {
		end := min(offset+65536, len(plain))
		record(1, plain[offset:end])
	}
	dataCount := count
	digest := sha256.Sum256(plain)
	end := make([]byte, 48)
	binary.LittleEndian.PutUint64(end[:8], uint64(dataCount))
	binary.LittleEndian.PutUint64(end[8:16], uint64(size))
	copy(end[16:], digest[:])
	record(2, end)
	name := "cipher-" + strings.Repeat("c", 32) + ".bin"
	raw, e := json.Marshal(map[string]any{"basename": name, "key": base64.RawURLEncoding.EncodeToString(key), "scope_sha256": hex.EncodeToString(scopeHash[:]), "plaintext_bytes": size, "data_records": dataCount, "plaintext_sha256": hex.EncodeToString(digest[:]), "media_kind": "PNG", "invocation_id": s.Invocation})
	if e != nil {
		t.Fatal(e)
	}
	root, e := os.OpenRoot(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { root.Close() })
	f, e := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.Write(disk); e != nil {
		t.Fatal(e)
	}
	if e = f.Close(); e != nil {
		t.Fatal(e)
	}
	return &cipherFixture{root: root, scope: s, raw: raw, plain: plain, disk: disk, key: key, name: name}
}
func (f *cipherFixture) receipt(t *testing.T) Receipt {
	t.Helper()
	r, e := DecodeReceipt(f.raw, f.scope, "PNG")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(r.Close)
	return r
}
func (f *cipherFixture) replace(t *testing.T, disk []byte) {
	t.Helper()
	file, e := f.root.OpenFile(f.name, os.O_WRONLY|os.O_TRUNC, 0600)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = file.Write(disk); e != nil {
		t.Fatal(e)
	}
	if e = file.Close(); e != nil {
		t.Fatal(e)
	}
}

func TestComfyCipherIndependentFixtureAndBoundedCrossRecordSeek(t *testing.T) {
	f := mediaFixture(t, 65536*2+37)
	receipt := f.receipt(t)
	r, e := OpenCipher(context.Background(), f.root, f.scope, &receipt)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	for _, offset := range []int64{0, 65530, 65536, 131065, int64(len(f.plain) - 1)} {
		if _, e = r.Seek(offset, io.SeekStart); e != nil {
			t.Fatal(e)
		}
		got := make([]byte, 31)
		n, e := r.Read(got)
		want := min(31, len(f.plain)-int(offset))
		if n != want || !bytes.Equal(got[:n], f.plain[offset:int(offset)+want]) || e != nil && e != io.EOF {
			t.Fatal("original plaintext range mismatch")
		}
	}
	if _, e = r.Seek(-1, io.SeekStart); e == nil {
		t.Fatal("negative seek accepted")
	}
	if _, e = r.Seek(1, io.SeekEnd); e == nil {
		t.Fatal("out-of-range seek accepted")
	}
}
func TestComfyCipherRejectsTruncationTrailingBytesAndMissingEnd(t *testing.T) {
	for _, change := range []func([]byte) []byte{func(b []byte) []byte { return b[:len(b)-1] }, func(b []byte) []byte { return append(b, 0) }, func(b []byte) []byte { return b[:len(b)-73] }} {
		f := mediaFixture(t, 71)
		receipt := f.receipt(t)
		f.replace(t, change(append([]byte(nil), f.disk...)))
		if r, e := OpenCipher(context.Background(), f.root, f.scope, &receipt); e == nil {
			r.Close()
			t.Fatal("unconfirmed END accepted")
		}
	}
}
func TestComfyCipherAuthenticatesHeaderEndAndIndividualData(t *testing.T) {
	for _, offset := range []int{0, 8, 16, 48, 58, -1} {
		f := mediaFixture(t, 65537)
		receipt := f.receipt(t)
		disk := append([]byte(nil), f.disk...)
		if offset < 0 {
			offset = len(disk) - 1
		}
		disk[offset] ^= 1
		f.replace(t, disk)
		r, e := OpenCipher(context.Background(), f.root, f.scope, &receipt)
		if e == nil {
			defer r.Close()
			_, e = io.Copy(io.Discard, r)
		}
		if e == nil {
			t.Fatal("tampered original ciphertext accepted")
		}
	}
}
func TestComfyReceiptCannotSelectAnotherOriginalScopeKindOrPath(t *testing.T) {
	f := mediaFixture(t, 81)
	for _, field := range []string{"session", "invocation", "generation", "environment", "recipe"} {
		s := f.scope
		switch field {
		case "session":
			s.Session = "session_other"
		case "invocation":
			s.Invocation = "invoke_other"
		case "generation":
			s.LocalGeneration = "20"
		case "environment":
			s.Environment = strings.Repeat("d", 64)
		case "recipe":
			s.Recipe = strings.Repeat("e", 64)
		}
		if _, e := DecodeReceipt(f.raw, s, "PNG"); e == nil {
			t.Fatal("foreign scope accepted")
		}
	}
	if _, e := DecodeReceipt(f.raw, f.scope, "MP4"); e == nil {
		t.Fatal("foreign media kind accepted")
	}
	var data map[string]any
	if e := json.Unmarshal(f.raw, &data); e != nil {
		t.Fatal(e)
	}
	data["basename"] = "../" + f.name
	raw, _ := json.Marshal(data)
	if _, e := DecodeReceipt(raw, f.scope, "PNG"); e == nil {
		t.Fatal("foreign path accepted")
	}
}
func TestComfyReceiptIsConsumedAcrossCopiedAliasesIncludingFailure(t *testing.T) {
	f := mediaFixture(t, 45)
	a := f.receipt(t)
	b := a
	wrong := f.scope
	wrong.Invocation = "invoke_wrong"
	if _, e := OpenCipher(context.Background(), f.root, wrong, &a); e == nil {
		t.Fatal("foreign original scope opened")
	}
	if _, e := OpenCipher(context.Background(), f.root, f.scope, &b); e == nil {
		t.Fatal("consumed alias reopened")
	}
	a.Close()
	b.Close()
}
func TestComfyReceiptCloseRevokesAllAliasesAndFormattingRedacts(t *testing.T) {
	f := mediaFixture(t, 91)
	receipt := f.receipt(t)
	alias := receipt
	for _, v := range []any{receipt, &receipt} {
		for _, format := range []string{"%v", "%+v", "%#v"} {
			text := fmt.Sprintf(format, v)
			if strings.Contains(text, f.name) || strings.Contains(text, base64.RawURLEncoding.EncodeToString(f.key)) || !strings.Contains(text, "redacted") {
				t.Fatal("private receipt exposed")
			}
		}
		encoded, e := json.Marshal(v)
		if e != nil || string(encoded) != `{"redacted":true}` {
			t.Fatal("private receipt JSON exposed")
		}
	}
	receipt.Close()
	if _, e := OpenCipher(context.Background(), f.root, f.scope, &alias); e == nil {
		t.Fatal("closed receipt alias opened")
	}
}
func TestComfyCipherContextCancellationAndCloseRevokeCopiedReaders(t *testing.T) {
	f := mediaFixture(t, 65537)
	receipt := f.receipt(t)
	r, e := OpenCipher(context.Background(), f.root, f.scope, &receipt)
	if e != nil {
		t.Fatal(e)
	}
	alias := *r
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := r.ReadContext(ctx, make([]byte, 1)); e == nil {
		t.Fatal("cancelled read succeeded")
	}
	if e := r.Close(); e != nil {
		t.Fatal(e)
	}
	if _, e := alias.Read(make([]byte, 1)); e == nil {
		t.Fatal("reader alias retained key access")
	}
	if len(r.state.key) != 0 || r.state.aead != nil || len(r.state.cache) != 0 {
		t.Fatal("owned reader references retained")
	}
}
func TestComfyReceiptRejectsDuplicateUnknownAndNonCanonicalKeyFields(t *testing.T) {
	f := mediaFixture(t, 41)
	for _, raw := range [][]byte{append([]byte(`{"extra":1,`), f.raw[1:]...), append([]byte(`{"media_kind":"PNG",`), f.raw[1:]...), bytes.Replace(f.raw, []byte(base64.RawURLEncoding.EncodeToString(f.key)), []byte(base64.URLEncoding.EncodeToString(f.key)), 1)} {
		if _, e := DecodeReceipt(raw, f.scope, "PNG"); e == nil {
			t.Fatal("ambiguous private receipt accepted")
		}
	}
}
