package cryptogateway

import (
	"context"
	"encoding/base64"
	"io"
	"strconv"
	"time"

	"p11.local/compute/client/crypto-gateway/ciphercustody"
	"p11.local/compute/client/crypto-gateway/endpoint"
	"p11.local/compute/client/protocol/cipherstream"
)

func custodyDescriptor(keys *endpoint.Vault, b Binding, origin string, expiry time.Time, inbound string, now time.Time) (ciphercustody.Descriptor, error) {
	if keys == nil || origin != "OUTBOUND" && origin != "INBOUND" || origin == "OUTBOUND" && b.Local.Purpose != "SIGN_STREAM" || origin == "INBOUND" && b.Local.Purpose != "OPEN_CONTENT" {
		return ciphercustody.Descriptor{}, endpoint.ErrTrust
	}
	peer, e := keys.Peer(b.Peer, now)
	if e != nil {
		return ciphercustody.Descriptor{}, e
	}
	public, e := keys.Public()
	if e != nil {
		return ciphercustody.Descriptor{}, e
	}
	var local endpoint.Bundle
	for _, candidate := range public {
		v := candidate.Bundle
		if v.Hash() == b.Local.BundleHash && v.Owner == b.Local.Owner && v.Endpoint == b.Local.Endpoint && v.Role == b.Local.Role &&
			(candidate.Status == "ACTIVE" || candidate.Status == "RETIRED" && b.Local.AllowRetired) && v.Current(now) == nil {
			local = v
			break
		}
	}
	if local.Profile == "" {
		return ciphercustody.Descriptor{}, endpoint.ErrTrust
	}
	sender, receiver := local, peer
	if origin == "INBOUND" {
		sender, receiver = peer, local
	}
	if verifyParties(b.Context, sender, receiver) != nil {
		return ciphercustody.Descriptor{}, endpoint.ErrTrust
	}
	sp, e := sender.Identity.Bytes("Ed25519")
	if e != nil {
		return ciphercustody.Descriptor{}, e
	}
	rp, e := receiver.Content.Bytes("X25519_SEALED_BOX")
	if e != nil {
		return ciphercustody.Descriptor{}, e
	}
	d := ciphercustody.Descriptor{Profile: ciphercustody.Profile, Origin: origin, Context: b.Context,
		SenderPublic: base64.RawURLEncoding.EncodeToString(sp), RecipientPublic: base64.RawURLEncoding.EncodeToString(rp),
		LocalBundle: b.Local.BundleHash, PeerBundle: b.Peer.BundleHash, PublicModel: b.PublicModel,
		DataBytes: strconv.FormatInt(b.Limits.DataBytes, 10), WireBytes: strconv.FormatInt(b.Limits.WireBytes, 10), Records: strconv.FormatUint(b.Limits.Records, 10),
		ManifestExpiry: expiry.UTC().Format(time.RFC3339Nano), InboundManifest: inbound}
	if d.Validate(now) != nil {
		return ciphercustody.Descriptor{}, endpoint.ErrTrust
	}
	return d, nil
}

// SealStored journals one original encryption intent before opening a stream.
// A READY retry never rereads source or recreates encryption; interrupted state
// fails closed. The producer remains subject to original scoped keys and input
// preflight. No listener, execution grant or network publisher is created.
func SealStored(ctx context.Context, keys *endpoint.Vault, store *ciphercustody.Store, b Binding, headers cipherstream.PrivateHeaders, source io.Reader, outcome string, expires, now time.Time) (ciphercustody.Result, error) {
	if store == nil || source == nil {
		return ciphercustody.Result{}, ciphercustody.ErrScope
	}
	d, e := custodyDescriptor(keys, b, "OUTBOUND", expires, "", now)
	if e != nil {
		return ciphercustody.Result{}, e
	}
	return store.Produce(ctx, d, now, func(active context.Context, destination io.Writer) (cipherstream.Manifest, error) {
		return SealObject(active, keys, b, headers, source, destination, outcome, expires, now)
	})
}

// ImportStored proves only the original opaque ciphertext. ConsumeStored is
// still required for complete INPUT authentication and private semantic checks.
func ImportStored(ctx context.Context, keys *endpoint.Vault, store *ciphercustody.Store, b Binding, source io.Reader, m cipherstream.Manifest, now time.Time) (ciphercustody.Result, error) {
	if store == nil {
		return ciphercustody.Result{}, ciphercustody.ErrScope
	}
	expiry, e := cipherstream.CanonicalTime(m.Expires)
	if e != nil {
		return ciphercustody.Result{}, e
	}
	d, e := custodyDescriptor(keys, b, "INBOUND", expiry, m.Hash(), now)
	if e != nil {
		return ciphercustody.Result{}, e
	}
	scope := b.Local
	scope.Purpose = "CIPHER_CUSTODY"
	scope.PeerBundleHash = b.Peer.BundleHash
	scope.PeerAllowRetired = b.Peer.AllowRetired
	var imported ciphercustody.Result
	e = keys.Use(ctx, scope, now, func(active context.Context, _ *endpoint.Material) error {
		var ie error
		imported, ie = store.Import(active, d, m, source, now)
		return ie
	})
	return imported, e
}

func ConsumeStored(ctx context.Context, keys *endpoint.Vault, store *ciphercustody.Store, b Binding, h ciphercustody.Handle, m cipherstream.Manifest, now time.Time, consume func(context.Context, cipherstream.Record) error) error {
	if store == nil || h.ManifestHash != m.Hash() {
		return ciphercustody.ErrScope
	}
	expiry, e := cipherstream.CanonicalTime(m.Expires)
	if e != nil {
		return e
	}
	d, e := custodyDescriptor(keys, b, "INBOUND", expiry, m.Hash(), now)
	if e != nil {
		return e
	}
	return store.WithOriginal(ctx, d, h, now, func(active context.Context, in io.Reader) error {
		return ConsumeObject(active, keys, b, in, m, now, consume)
	})
}

// CopyStoredRange needs only public identity custody. No content/signing key is
// copied, and it never invokes a model. The business transport must separately
// validate its current original upload/download grant and remote byte offset.
func CopyStoredRange(ctx context.Context, keys *endpoint.Vault, store *ciphercustody.Store, b Binding, origin string, h ciphercustody.Handle, m cipherstream.Manifest, offset, length int64, destination io.Writer, now time.Time) (int64, error) {
	if keys == nil || store == nil || h.ManifestHash != m.Hash() {
		return 0, ciphercustody.ErrScope
	}
	expiry, e := cipherstream.CanonicalTime(m.Expires)
	if e != nil {
		return 0, e
	}
	inbound := ""
	if origin == "INBOUND" {
		inbound = m.Hash()
	}
	d, e := custodyDescriptor(keys, b, origin, expiry, inbound, now)
	if e != nil {
		return 0, e
	}
	scope := b.Local
	scope.Purpose = "CIPHER_CUSTODY"
	scope.PeerBundleHash = b.Peer.BundleHash
	scope.PeerAllowRetired = b.Peer.AllowRetired
	var copied int64
	e = keys.Use(ctx, scope, now, func(active context.Context, _ *endpoint.Material) error {
		var ce error
		copied, ce = store.CopyRange(active, d, h, offset, length, destination, now)
		return ce
	})
	return copied, e
}
