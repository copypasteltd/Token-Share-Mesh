package devicecontrol

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"p11.local/compute/client/protocol/rentalaccess"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/strictjson"
)

// PrepareRentalControl freezes metadata and a deterministic original-request
// key. SendBounded still signs each transport nonce independently. Retrying an
// uncertain request cannot allocate another control sequence or another key.
func (c *Client) PrepareRentalControl(session string, in rentalaccess.ControlRequest) (Prepared, error) {
	if !ids.Valid(session) {
		return Prepared{}, rentalaccess.ErrAccess
	}
	if _, err := in.SequenceNumber(); err != nil {
		return Prepared{}, err
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return Prepared{}, err
	}
	digest := sha256.Sum256(append([]byte("P11_RENTAL_CONTROL_REQUEST_1\n"+session+"\n"), raw...))
	key := "rctrl_" + hex.EncodeToString(digest[:])
	return c.Prepare("POST", "/v1/p11/runner/rental-sessions/"+session+"/control", raw, key, "")
}

// RentalGrantExpectation reads stored metadata using the original signed
// device and explicit control origin. Incoming bearer claims/digests are not
// used to populate this response. It is never an environment approval source.
func (c *Client) RentalGrantExpectation(ctx context.Context, session, grant string) (rentalaccess.GrantExpectation, error) {
	var result rentalaccess.GrantExpectation
	if !ids.Valid(session) || !ids.Valid(grant) {
		return result, rentalaccess.ErrAccess
	}
	p, err := c.Prepare("GET", "/v1/p11/runner/rental-sessions/"+session+"/access-grants/"+grant, nil, "", "")
	if err != nil {
		return result, err
	}
	reply, err := c.SendBounded(ctx, p, 16384)
	if err != nil {
		return result, err
	}
	defer clear(reply.Body)
	if reply.Status != 200 || strictjson.Decode(reply.Body, &result, 16384) != nil || result.Profile != rentalaccess.ExpectationProfile {
		return rentalaccess.GrantExpectation{}, rentalaccess.ErrAccess
	}
	return result, nil
}
