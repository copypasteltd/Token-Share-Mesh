package node

import (
	"context"
	"net/http"
	"net/url"
	"p11.local/compute/client/protocol/devicecontrol"
	"p11.local/compute/shared/canonicaljson"
	"p11.local/compute/shared/endpointdirectory"
	"p11.local/compute/shared/endpointwire"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/strictjson"
)

// Explicit metadata management shares the original node's single control
// owner. Call before the mutable control loop, never concurrently with Step.
func (s *Store) directoryReady() bool {
	return s.canOpenEndpointKeys() && s.s.DeviceID != "" && s.s.SellerID != "" && s.s.RemoteState != "REVOKED" && s.host == nil && len(s.nativeRentals)+len(s.comfyRentals) == 0
}
func (s *Store) localDirectoryBundle(hash string, active bool) (endpointwire.Bundle, error) {
	v, e := s.OpenEndpointKeys()
	if e != nil {
		return endpointwire.Bundle{}, e
	}
	keys, e := v.Public()
	if e != nil {
		return endpointwire.Bundle{}, e
	}
	for _, k := range keys {
		b := k.Bundle
		if b.Hash() == hash && b.Role == "SELLER" && b.Owner == s.s.SellerID && b.Endpoint == s.s.DeviceID && (!active || k.Status == "ACTIVE") {
			return b, nil
		}
	}
	return endpointwire.Bundle{}, ErrAuthority
}
func (s *Store) PublishEndpointDirectory(ctx context.Context, hash string) (endpointdirectory.Record, error) {
	if !s.directoryReady() || s.s.Pending != nil {
		return endpointdirectory.Record{}, ErrPending
	}
	b, e := s.localDirectoryBundle(hash, true)
	if e != nil {
		return endpointdirectory.Record{}, e
	}
	kind, recipient := devicecontrol.EndpointRegister, b.Content.ID
	var payload any = endpointdirectory.Registration{Bundle: b}
	if b.Version != "1" {
		t, e := s.cryptoKeys.OriginalLocalRotation(hash)
		if e != nil {
			return endpointdirectory.Record{}, e
		}
		old, e := s.localDirectoryBundle(t.Previous, false)
		if e != nil {
			return endpointdirectory.Record{}, e
		}
		if t.Next.Hash() != hash {
			return endpointdirectory.Record{}, ErrState
		}
		kind, recipient, payload = devicecontrol.EndpointRotate, old.Content.ID, endpointdirectory.Rotation{Transition: t}
	}
	return s.queueEndpointDirectory(ctx, kind, recipient, payload)
}
func (s *Store) RevokeEndpointDirectory(ctx context.Context, hash string) (endpointdirectory.Record, error) {
	if !s.directoryReady() || s.s.Pending != nil {
		return endpointdirectory.Record{}, ErrPending
	}
	b, e := s.localDirectoryBundle(hash, false)
	if e != nil {
		return endpointdirectory.Record{}, e
	}
	return s.queueEndpointDirectory(ctx, devicecontrol.EndpointRevoke, b.Content.ID, endpointdirectory.Revocation{BundleHash: hash})
}
func (s *Store) queueEndpointDirectory(ctx context.Context, kind, recipient string, value any) (endpointdirectory.Record, error) {
	raw, e := canonicaljson.Encode(value, 16384)
	if e != nil {
		return endpointdirectory.Record{}, ErrState
	}
	s.s.Pending = &command{Kind: kind, Key: ids.New("endpoint_intent"), Resource: recipient, Body: raw}
	s.s.LastOutcome = "PUBLIC_ENDPOINT_PENDING"
	// Persist exact method selector, URI resource, bytes and original key BEFORE
	// network I/O. A failed/unknown local commit is never followed by a send.
	if e = s.save(); e != nil {
		return endpointdirectory.Record{}, e
	}
	return s.RecoverEndpointDirectory(ctx)
}
func (s *Store) RecoverEndpointDirectory(ctx context.Context) (endpointdirectory.Record, error) {
	var empty endpointdirectory.Record
	p := s.s.Pending
	if !s.directoryReady() || s.poisoned || p == nil || !devicecontrol.EndpointCommandKind(p.Kind) || s.validate() != nil {
		return empty, ErrPending
	}
	c, e := s.client()
	if e != nil {
		return empty, e
	}
	defer c.Close()
	prepared, e := c.PrepareEndpointDirectory(p.Kind, p.Resource, s.s.SellerID, p.Body, p.Key)
	if e != nil {
		return empty, ErrState
	}
	reply, e := c.SendBounded(ctx, prepared, endpointdirectory.MaximumReply)
	if e != nil {
		return empty, e
	}
	if reply.Status != 200 && reply.Status != 201 {
		return empty, classify(reply)
	}
	result, e := devicecontrol.EndpointDirectoryReply(reply)
	if e != nil {
		return empty, e
	}
	if result.Bundle.Owner != s.s.SellerID || result.Bundle.Role != "SELLER" || result.Bundle.Endpoint != s.s.DeviceID {
		return empty, &devicecontrol.TransportError{Unknown: true}
	}
	if p.Kind == devicecontrol.EndpointRevoke {
		var in endpointdirectory.Revocation
		if canonicaljson.DecodeExact(p.Body, &in, 16384) != nil || result.Recipient != p.Resource || result.BundleHash != in.BundleHash || result.State != "REVOKED" {
			return empty, &devicecontrol.TransportError{Unknown: true}
		}
	} else {
		b, e := devicecontrol.EndpointCommandBundle(p.Kind, p.Body)
		if e != nil || result.BundleHash != b.Hash() || result.Recipient != b.Content.ID {
			return empty, &devicecontrol.TransportError{Unknown: true}
		}
	}
	s.s.Pending = nil
	s.s.LastOutcome = "PUBLIC_ENDPOINT_RECORDED_KEYS_RETAINED"
	if e = s.save(); e != nil {
		s.s.Pending = p
		return empty, e
	}
	clear(p.Body)
	return result, nil
}
func (s *Store) GetEndpointDirectory(ctx context.Context, recipient string) (endpointdirectory.Record, error) {
	if !s.directoryReady() || !ids.Valid(recipient) {
		return endpointdirectory.Record{}, ErrState
	}
	c, e := s.client()
	if e != nil {
		return endpointdirectory.Record{}, e
	}
	defer c.Close()
	p, e := c.Prepare(http.MethodGet, "/v1/p11/endpoint-bundles/"+recipient, nil, "", "")
	if e != nil {
		return endpointdirectory.Record{}, e
	}
	r, e := c.SendBounded(ctx, p, endpointdirectory.MaximumReply)
	if e != nil {
		return endpointdirectory.Record{}, e
	}
	if r.Status != 200 {
		return endpointdirectory.Record{}, classify(r)
	}
	v, e := devicecontrol.EndpointDirectoryReply(r)
	if e != nil {
		return v, e
	}
	if v.Recipient != recipient || v.Bundle.Owner != s.s.SellerID || v.Bundle.Endpoint != s.s.DeviceID || v.Bundle.Role != "SELLER" {
		return endpointdirectory.Record{}, ErrState
	}
	return v, nil
}
func (s *Store) ListEndpointDirectory(ctx context.Context, cursor string) (endpointdirectory.Page, error) {
	var result endpointdirectory.Page
	if !s.directoryReady() || len(cursor) > 512 {
		return result, ErrState
	}
	c, e := s.client()
	if e != nil {
		return result, e
	}
	defer c.Close()
	uri := "/v1/p11/endpoint-bundles"
	if cursor != "" {
		uri += "?cursor=" + url.QueryEscape(cursor)
	}
	p, e := c.Prepare(http.MethodGet, uri, nil, "", "")
	if e != nil {
		return result, e
	}
	r, e := c.SendBounded(ctx, p, endpointdirectory.MaximumReply)
	if e != nil {
		return result, e
	}
	if r.Status != 200 {
		return result, classify(r)
	}
	if strictjson.Decode(r.Body, &result, endpointdirectory.MaximumReply) != nil || len(result.Items) > endpointdirectory.PageSize || result.Next != nil && (len(*result.Next) < 1 || len(*result.Next) > 512) {
		return endpointdirectory.Page{}, ErrState
	}
	for _, v := range result.Items {
		if v.Validate() != nil || v.Bundle.Owner != s.s.SellerID || v.Bundle.Endpoint != s.s.DeviceID || v.Bundle.Role != "SELLER" {
			return endpointdirectory.Page{}, ErrState
		}
	}
	return result, nil
}
