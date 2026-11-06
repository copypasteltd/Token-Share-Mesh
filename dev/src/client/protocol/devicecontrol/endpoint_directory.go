package devicecontrol

import (
	"errors"
	"net/http"
	"p11.local/compute/shared/endpointdirectory"
	"p11.local/compute/shared/endpointwire"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/strictjson"
)

const EndpointRegister = "ENDPOINT_REGISTER"
const EndpointRotate = "ENDPOINT_ROTATE"
const EndpointRevoke = "ENDPOINT_REVOKE"

func EndpointCommandKind(kind string) bool {
	return kind == EndpointRegister || kind == EndpointRotate || kind == EndpointRevoke
}
func EndpointCommandBundle(kind string, raw []byte) (endpointwire.Bundle, error) {
	switch kind {
	case EndpointRegister:
		var in endpointdirectory.Registration
		if strictjson.Decode(raw, &in, 16384) != nil || in.Bundle.Validate() != nil || in.Bundle.Version != "1" {
			return endpointwire.Bundle{}, errors.New("original initial public bundle required")
		}
		return in.Bundle, nil
	case EndpointRotate:
		var in endpointdirectory.Rotation
		if strictjson.Decode(raw, &in, 16384) != nil || in.Transition.Next.Validate() != nil || !executionHash(in.Transition.Previous) || in.Transition.Hash() == "" {
			return endpointwire.Bundle{}, errors.New("original public rotation required")
		}
		return in.Transition.Next, nil
	}
	return endpointwire.Bundle{}, errors.New("public bundle command required")
}
func executionHash(v string) bool {
	if len(v) != 64 {
		return false
	}
	for _, c := range v {
		if c < '0' || c > '9' && c < 'a' || c > 'f' {
			return false
		}
	}
	return true
}
func EndpointCommandValid(kind, recipient, owner, device string, raw []byte) bool {
	if !ids.Valid(recipient) || !ids.Valid(owner) || !ids.Valid(device) {
		return false
	}
	if kind == EndpointRevoke {
		var in endpointdirectory.Revocation
		return strictjson.Decode(raw, &in, 16384) == nil && executionHash(in.BundleHash)
	}
	b, e := EndpointCommandBundle(kind, raw)
	return e == nil && b.Role == "SELLER" && b.Owner == owner && b.Endpoint == device && (kind != EndpointRegister || b.Content.ID == recipient)
}
func (c *Client) PrepareEndpointDirectory(kind, recipient, owner string, raw []byte, key string) (Prepared, error) {
	if !EndpointCommandValid(kind, recipient, owner, c.id, raw) {
		return Prepared{}, errors.New("original provider directory command required")
	}
	uri := "/v1/p11/endpoint-bundles/" + recipient
	switch kind {
	case EndpointRegister:
		uri = "/v1/p11/endpoint-bundles"
	case EndpointRotate:
		uri += "/rotations"
	case EndpointRevoke:
		uri += "/revoke"
	default:
		return Prepared{}, errors.New("unsupported public directory command")
	}
	return c.Prepare(http.MethodPost, uri, raw, key, "")
}
func EndpointDirectoryReply(reply Reply) (endpointdirectory.Record, error) {
	var v endpointdirectory.Record
	if reply.Status != 200 && reply.Status != 201 || len(reply.Body) > endpointdirectory.MaximumReply || strictjson.Decode(reply.Body, &v, endpointdirectory.MaximumReply) != nil || v.Validate() != nil {
		return v, &TransportError{Unknown: true}
	}
	return v, nil
}
