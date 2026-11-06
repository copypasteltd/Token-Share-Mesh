// Package rentaltest preserves client source-test imports through the shared
// synthetic fixture. It is never a production execution/qualification source.
package rentaltest

import (
	"testing"

	relay "p11.local/compute/gateway/core"
	shared "p11.local/compute/internal/testsupport/rentaltest"
)

const Body = shared.Body

type Fixture = shared.Fixture

func Text(t *testing.T, profile relay.Profile) Fixture  { return shared.Text(t, profile) }
func Media(t *testing.T) Fixture                        { return shared.Media(t) }
func MediaProfile(t *testing.T, profile string) Fixture { return shared.MediaProfile(t, profile) }
