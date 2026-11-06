//go:build p11_sodium && cgo && (windows || linux)

package sodium

/*
#cgo windows CFLAGS: -DSODIUM_STATIC
#cgo windows LDFLAGS: -ladvapi32 -lbcrypt
#cgo linux LDFLAGS: -pthread
#cgo LDFLAGS: -Wl,-Bstatic -lsodium -Wl,-Bdynamic
#include <sodium.h>
#include <string.h>
static int p11_sodium_init(void) {
    if (sodium_init() < 0 ||
        strcmp(SODIUM_VERSION_STRING, "1.0.22") ||
        strcmp(sodium_version_string(), "1.0.22") ||
        crypto_box_publickeybytes() != 32 || crypto_box_secretkeybytes() != 32 ||
        crypto_box_sealbytes() != 48 ||
        crypto_secretstream_xchacha20poly1305_keybytes() != 32 ||
        crypto_secretstream_xchacha20poly1305_headerbytes() != 24 ||
        crypto_secretstream_xchacha20poly1305_abytes() != 17 ||
        crypto_secretstream_xchacha20poly1305_statebytes() !=
            sizeof(crypto_secretstream_xchacha20poly1305_state)) return -1;
    return 0;
}
static void *p11_state_new(void) {
    return sodium_malloc(sizeof(crypto_secretstream_xchacha20poly1305_state));
}
static int p11_push_init(void *s, unsigned char *h, const unsigned char *k) {
    return crypto_secretstream_xchacha20poly1305_init_push(s, h, k);
}
static int p11_pull_init(void *s, const unsigned char *h, const unsigned char *k) {
    return crypto_secretstream_xchacha20poly1305_init_pull(s, h, k);
}
static int p11_push(void *s, unsigned char *out, const unsigned char *m,
                    unsigned long long n, const unsigned char *ad,
                    unsigned long long an, int final) {
    return crypto_secretstream_xchacha20poly1305_push(s, out, NULL, m, n, ad, an,
        final ? crypto_secretstream_xchacha20poly1305_TAG_FINAL :
                crypto_secretstream_xchacha20poly1305_TAG_MESSAGE);
}
static int p11_pull(void *s, unsigned char *out, unsigned char *tag,
                    const unsigned char *c, unsigned long long n,
                    const unsigned char *ad, unsigned long long an) {
    return crypto_secretstream_xchacha20poly1305_pull(s, out, NULL, tag, c, n, ad, an);
}
static int p11_message_tag(unsigned char t) {
    return t == crypto_secretstream_xchacha20poly1305_TAG_MESSAGE;
}
static int p11_final_tag(unsigned char t) {
    return t == crypto_secretstream_xchacha20poly1305_TAG_FINAL;
}
*/
import "C"

import (
	"runtime"
	"sync"
	"unsafe"
)

var initialize = sync.OnceValue(func() bool { return C.p11_sodium_init() == 0 })

func Available() error {
	if !initialize() {
		return ErrUnavailable
	}
	return nil
}
func pointer(b []byte) *C.uchar {
	if len(b) == 0 {
		return nil
	}
	return (*C.uchar)(unsafe.Pointer(&b[0]))
}
func Keypair() (public, secret []byte, err error) {
	if err = Available(); err != nil {
		return
	}
	public, secret = make([]byte, KeyBytes), make([]byte, KeyBytes)
	if C.crypto_box_keypair(pointer(public), pointer(secret)) != 0 {
		clear(public)
		clear(secret)
		return nil, nil, ErrCipher
	}
	return
}
func Seal(message, public []byte) ([]byte, error) {
	if Available() != nil {
		return nil, ErrUnavailable
	}
	if len(public) != KeyBytes || len(message) == 0 || len(message) > 4096-SealBytes {
		return nil, ErrCipher
	}
	out := make([]byte, len(message)+SealBytes)
	if C.crypto_box_seal(pointer(out), pointer(message), C.ulonglong(len(message)), pointer(public)) != 0 {
		clear(out)
		return nil, ErrCipher
	}
	runtime.KeepAlive(message)
	runtime.KeepAlive(public)
	return out, nil
}
func Unseal(sealed, public, secret []byte) ([]byte, error) {
	if Available() != nil {
		return nil, ErrUnavailable
	}
	if len(public) != KeyBytes || len(secret) != KeyBytes || len(sealed) <= SealBytes || len(sealed) > 4096 {
		return nil, ErrCipher
	}
	out := make([]byte, len(sealed)-SealBytes)
	if C.crypto_box_seal_open(pointer(out), pointer(sealed), C.ulonglong(len(sealed)), pointer(public), pointer(secret)) != 0 {
		clear(out)
		return nil, ErrCipher
	}
	runtime.KeepAlive(secret)
	runtime.KeepAlive(public)
	runtime.KeepAlive(sealed)
	return out, nil
}

// State owns C-allocated, sodium-cleared key state. It is single-use, not
// serializable and not safe for concurrent record calls. An authentication
// error poisons the stream; callers cannot continue or retry a push.
type State struct {
	value        unsafe.Pointer
	pull, closed bool
}

func NewPush(key []byte) (*State, []byte, error) {
	if Available() != nil {
		return nil, nil, ErrUnavailable
	}
	if len(key) != KeyBytes {
		return nil, nil, ErrCipher
	}
	s := &State{value: C.p11_state_new()}
	if s.value == nil {
		return nil, nil, ErrCipher
	}
	header := make([]byte, HeaderBytes)
	if C.p11_push_init(s.value, pointer(header), pointer(key)) != 0 {
		s.Close()
		return nil, nil, ErrCipher
	}
	runtime.KeepAlive(key)
	return s, header, nil
}
func NewPull(key, header []byte) (*State, error) {
	if Available() != nil {
		return nil, ErrUnavailable
	}
	if len(key) != KeyBytes || len(header) != HeaderBytes {
		return nil, ErrCipher
	}
	s := &State{value: C.p11_state_new(), pull: true}
	if s.value == nil {
		return nil, ErrCipher
	}
	if C.p11_pull_init(s.value, pointer(header), pointer(key)) != 0 {
		s.Close()
		return nil, ErrCipher
	}
	runtime.KeepAlive(key)
	runtime.KeepAlive(header)
	return s, nil
}
func (s *State) Push(message, aad []byte, final bool) ([]byte, error) {
	if s == nil || s.closed || s.pull || len(message) > MaximumMessageBytes || len(aad) > MaximumAADBytes {
		return nil, ErrCipher
	}
	out := make([]byte, len(message)+AuthenticationBytes)
	f := C.int(0)
	if final {
		f = 1
	}
	if C.p11_push(s.value, pointer(out), pointer(message), C.ulonglong(len(message)), pointer(aad), C.ulonglong(len(aad)), f) != 0 {
		clear(out)
		s.Close()
		return nil, ErrCipher
	}
	runtime.KeepAlive(message)
	runtime.KeepAlive(aad)
	if final {
		s.Close()
	}
	return out, nil
}
func (s *State) Pull(sealed, aad []byte) ([]byte, bool, error) {
	if s == nil || s.closed || !s.pull || len(sealed) < AuthenticationBytes || len(sealed) > MaximumMessageBytes+AuthenticationBytes || len(aad) > MaximumAADBytes {
		return nil, false, ErrCipher
	}
	out := make([]byte, len(sealed)-AuthenticationBytes)
	var tag C.uchar
	if C.p11_pull(s.value, pointer(out), &tag, pointer(sealed), C.ulonglong(len(sealed)), pointer(aad), C.ulonglong(len(aad))) != 0 ||
		(C.p11_message_tag(tag) == 0 && C.p11_final_tag(tag) == 0) {
		clear(out)
		s.Close()
		return nil, false, ErrCipher
	}
	runtime.KeepAlive(sealed)
	runtime.KeepAlive(aad)
	final := C.p11_final_tag(tag) != 0
	if final {
		s.Close()
	}
	return out, final, nil
}
func (s *State) Close() {
	if s != nil && !s.closed {
		s.closed = true
		if s.value != nil {
			C.sodium_free(s.value)
			s.value = nil
		}
	}
}
