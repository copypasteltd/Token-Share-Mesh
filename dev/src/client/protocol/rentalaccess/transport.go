package rentalaccess

import (
	"strconv"
)

// ControlRequest contains original authority metadata only, never the Bearer.
// ExpectedVersion is the committed version returned by access issuance. A new
// control receipt does not increment it, renew paid terms or release a device.
type ControlRequest struct {
	ExpectedVersion int64  `json:"expected_version"`
	Grant           string `json:"grant_id"`
	Digest          string `json:"grant_digest"`
	Generation      string `json:"resource_lock_generation"`
	Terms           string `json:"terms_revision"`
	TermsHash       string `json:"terms_hash"`
	Sequence        string `json:"sequence"`
}

// SequenceNumber explicitly limits the SQL transport to signed BIGINT. The
// independent in-memory codec supports uint64; that does not widen SQL fences.
func (r ControlRequest) SequenceNumber() (int64, error) {
	n, err := strconv.ParseInt(r.Sequence, 10, 64)
	if err != nil || n < 1 || strconv.FormatInt(n, 10) != r.Sequence || r.ExpectedVersion < 1 || !identifier(r.Grant) || !positive(r.Generation) || !positive(r.Terms) {
		return 0, ErrAccess
	}
	for _, v := range []string{r.Generation, r.Terms} {
		if value, e := strconv.ParseInt(v, 10, 64); e != nil || value < 1 {
			return 0, ErrAccess
		}
	}
	if len(r.Digest) != 64 || len(r.TermsHash) != 64 {
		return 0, ErrAccess
	}
	for _, v := range r.Digest + r.TermsHash {
		if !(v >= '0' && v <= '9' || v >= 'a' && v <= 'f') {
			return 0, ErrAccess
		}
	}
	return n, nil
}
