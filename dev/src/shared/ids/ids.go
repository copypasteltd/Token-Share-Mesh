package ids

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
)

var valid = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{2,127}$`)

func Valid(value string) bool { return valid.MatchString(value) }
func New(prefix string) string {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		panic("secure randomness unavailable")
	}
	value := prefix + "_" + hex.EncodeToString(data[:])
	if !Valid(value) {
		panic("invalid identifier prefix")
	}
	return value
}
