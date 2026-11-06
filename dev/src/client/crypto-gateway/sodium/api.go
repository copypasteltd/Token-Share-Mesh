// Package sodium is a deliberately small native libsodium boundary. There is
// no alternative algorithm or plaintext fallback when the pinned build is absent.
package sodium

import "errors"

const Version = "1.0.22"
const Suite = "P11_SODIUM_1_0_22_SEAL_SECRETSTREAM_ED25519_JCS_1"
const KeyBytes = 32
const PublicKeyBytes = 32
const HeaderBytes = 24
const AuthenticationBytes = 17
const SealBytes = 48
const MaximumMessageBytes = 65536
const MaximumAADBytes = 32768

var ErrUnavailable = errors.New("pinned native cipher suite unavailable")
var ErrCipher = errors.New("cipher operation failed")
