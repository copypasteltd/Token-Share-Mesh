package devicecontrol

import (
	"encoding/json"
	"errors"

	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/resultwire"
)

// PrepareDelivery freezes the exact original signed report. Network recovery
// reuses this body and key; a result ID or signature is never regenerated.
func (c *Client) PrepareDelivery(attempt, key string, in resultwire.Delivery) (Prepared, error) {
	if !ids.Valid(attempt) || in.Scope.Attempt != attempt || in.Scope.Device != c.id || in.Validate() != nil {
		return Prepared{}, errors.New("original device delivery required")
	}
	b, e := json.Marshal(in)
	if e != nil {
		return Prepared{}, e
	}
	return c.Prepare("POST", "/v1/p11/runner/attempts/"+attempt+"/deliveries", b, key, "")
}
func (c *Client) PrepareUsage(attempt, key string, in resultwire.Statement) (Prepared, error) {
	if !ids.Valid(attempt) || in.Scope.Attempt != attempt || in.Scope.Device != c.id {
		return Prepared{}, errors.New("original independent metering owner required")
	}
	if e := in.Validate(); e != nil {
		return Prepared{}, e
	}
	b, e := json.Marshal(in)
	if e != nil {
		return Prepared{}, e
	}
	return c.Prepare("POST", "/v1/p11/runner/attempts/"+attempt+"/usage", b, key, "")
}
