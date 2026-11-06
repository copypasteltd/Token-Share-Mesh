// Package endpointdirectory carries public directory metadata only. An account
// binding is not an independent trust pin, private-key proof or execution grant.
package endpointdirectory

import (
	"errors"
	"p11.local/compute/shared/endpointwire"
	"p11.local/compute/shared/ids"
	"strconv"
	"time"
)

const Profile = "P11_ACCOUNT_ENDPOINT_DIRECTORY_1"
const MaximumEndpoints = 32
const MaximumVersions = 32

// Two maximum-sized bundle+rotation records fit inside the 64KiB reply bound.
const PageSize = 2
const MaximumReply = 65536

type Registration struct {
	Bundle endpointwire.Bundle `json:"bundle"`
}
type Rotation struct {
	Transition endpointwire.Transition `json:"transition"`
}
type Revocation struct {
	BundleHash string `json:"expected_bundle_hash"`
}
type Record struct {
	Profile                  string                   `json:"profile_id"`
	Recipient                string                   `json:"recipient_id"`
	Bundle                   endpointwire.Bundle      `json:"bundle"`
	BundleHash               string                   `json:"bundle_hash"`
	State                    string                   `json:"state"`
	Previous                 string                   `json:"previous_recipient_id,omitempty"`
	TransitionHash           string                   `json:"rotation_hash,omitempty"`
	Rotation                 *endpointwire.Transition `json:"rotation,omitempty"`
	IndependentTrustRequired bool                     `json:"independent_trust_required"`
	Replayed                 bool                     `json:"replayed"`
}
type Page struct {
	Items []Record `json:"items"`
	Next  *string  `json:"next_cursor"`
}

func (r Record) Validate() error {
	n, e := strconv.Atoi(r.Bundle.Version)
	if r.Profile != Profile || !r.IndependentTrustRequired || r.Bundle.Validate() != nil || r.Recipient != r.Bundle.Content.ID || r.BundleHash != r.Bundle.Hash() || e != nil || n < 1 || n > MaximumVersions || r.State != "ACTIVE" && r.State != "RETIRED" && r.State != "REVOKED" && r.State != "EXPIRED" {
		return errors.New("original public directory record required")
	}
	if n == 1 {
		if r.Previous != "" || r.TransitionHash != "" || r.Rotation != nil {
			return errors.New("unexpected initial endpoint rotation")
		}
	} else {
		if !ids.Valid(r.Previous) || r.Rotation == nil || r.Rotation.Next.Hash() != r.BundleHash || r.Rotation.Hash() != r.TransitionHash {
			return errors.New("original endpoint rotation metadata required")
		}
	}
	return nil
}

// VerifySuccessor requires the caller's independently trusted old bundle. This
// checks continuity only; it does not pin the endpoint or enable new work.
func (r Record) VerifySuccessor(old endpointwire.Bundle, now time.Time) error {
	if r.Validate() != nil || r.Rotation == nil || r.Previous != old.Content.ID || r.Rotation.Verify(old, now) != nil {
		return endpointwire.ErrTrust
	}
	return nil
}
