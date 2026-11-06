package rentalaccess

import "time"

const ExpectationProfile = "P11_RENTAL_GRANT_EXPECTATION_1"

// GrantExpectation is read from the authenticated, explicitly configured
// control origin by the original provider device, not from a caller's Bearer.
// It contains only stored metadata. It cannot approve an environment or grant
// physical ownership. Public issuer pins remain separately provisioned.
type GrantExpectation struct {
	Profile        string   `json:"profile_id"`
	Metadata       Metadata `json:"grant_metadata"`
	SessionVersion int64    `json:"session_version"`
	Connection     string   `json:"connection_ref"`
}

// Bind requires the complete independently approved local original tuple. The
// only field it fills is the original stored token digest obtained from this
// authenticated server record. Hashing the incoming Bearer is never a substitute.
func (g GrantExpectation) Bind(approved Expected, now time.Time) (Expected, error) {
	m := g.Metadata
	if g.Profile != ExpectationProfile || g.SessionVersion < 1 || !identifier(g.Connection) || m.Profile != Profile || !identifier(m.KeyID) || !m.Claims.validAt(now) || approved.TokenDigest != "" && approved.TokenDigest != m.Digest {
		return Expected{}, ErrAccess
	}
	approved.TokenDigest = m.Digest
	if !approved.matches(m.Claims) {
		return Expected{}, ErrAccess
	}
	return approved, nil
}
