// Package cryptogateway composes scoped endpoint keys with the original signed
// cipher format. It opens no content listener and supplies no execution grant.
package cryptogateway

import (
	"context"
	"crypto/ed25519"
	"io"
	"p11.local/compute/client/crypto-gateway/endpoint"
	"p11.local/compute/client/protocol/cipherstream"
	"p11.local/compute/gateway/core"
	"time"
)

// Binding comes from an independently confirmed original contract/context,
// never from the untrusted stream header. The header must match it exactly.
type Binding struct {
	Local       endpoint.Scope
	Peer        endpoint.PeerScope
	Context     cipherstream.Context
	Limits      cipherstream.Limits
	PublicModel string
}

// The transport deadline is independent of the original compute deadline.
// Validate bounds before reading INPUT; an untrusted limit must not overflow a
// LimitReader budget or cause an unbounded allocation before NewWriter.
func transferContext(parent context.Context, b Binding, now time.Time) (context.Context, context.CancelFunc, error) {
	deadline, e := cipherstream.CanonicalTime(b.Context.Deadline)
	if parent == nil || parent.Err() != nil || e != nil || !now.Before(deadline) || b.Limits.Validate(b.Context.Purpose) != nil {
		return nil, nil, cipherstream.ErrLimit
	}
	ctx, cancel := context.WithDeadline(parent, deadline)
	if ctx.Err() != nil {
		cancel()
		return nil, nil, endpoint.ErrClosed
	}
	return ctx, cancel, nil
}

func verifyParties(c cipherstream.Context, sender, receiver endpoint.Bundle) error {
	deadline, e := cipherstream.CanonicalTime(c.Deadline)
	senderExpiry, se := time.Parse(time.RFC3339Nano, sender.Expires)
	receiverExpiry, re := time.Parse(time.RFC3339Nano, receiver.Expires)
	if e != nil || se != nil || re != nil || deadline.After(senderExpiry) || deadline.After(receiverExpiry) {
		return endpoint.ErrTrust
	}
	if sender.Endpoint != c.Sender || sender.Identity.ID != c.SenderKey || receiver.Endpoint != c.Receiver || receiver.Content.ID != c.ReceiverKey || receiver.Content.Fingerprint != c.ReceiverFingerprint {
		return endpoint.ErrTrust
	}
	var buyer, seller endpoint.Bundle
	if c.Direction == "BUYER_TO_SELLER" {
		buyer, seller = sender, receiver
	} else if c.Direction == "SELLER_TO_BUYER" {
		buyer, seller = receiver, sender
	} else {
		return endpoint.ErrTrust
	}
	if buyer.Role != "BUYER" || seller.Role != "SELLER" || buyer.Owner != c.Buyer || seller.Owner != c.Seller || buyer.Endpoint != c.BuyerEndpoint || seller.Endpoint != c.SellerEndpoint || buyer.Content.ID != c.BuyerResultKey || buyer.Content.Fingerprint != c.BuyerResultFingerprint {
		return endpoint.ErrTrust
	}
	return nil
}

// SealObject writes only ciphertext. Input/output byte readers and writers must
// honor their enclosing context or supply bounded file/pipe I/O. A failed sink
// poisons this writer; recovery must retransmit stored original ciphertext,
// never invoke this function again for the same original stream/epoch.
func SealObject(ctx context.Context, keys *endpoint.Vault, b Binding, headers cipherstream.PrivateHeaders, source io.Reader, destination io.Writer, outcome string, expires, now time.Time) (cipherstream.Manifest, error) {
	if ctx == nil || keys == nil || source == nil || destination == nil || b.Local.Purpose != "SIGN_STREAM" || b.Context.Purpose == "CONTROL" {
		return cipherstream.Manifest{}, endpoint.ErrTrust
	}
	activeTransfer, cancel, e := transferContext(ctx, b, now)
	if e != nil {
		return cipherstream.Manifest{}, e
	}
	defer cancel()
	peer, e := keys.Peer(b.Peer, now)
	if e != nil {
		return cipherstream.Manifest{}, e
	}
	b.Local.PeerBundleHash = b.Peer.BundleHash
	b.Local.PeerAllowRetired = b.Peer.AllowRetired
	var manifest cipherstream.Manifest
	e = keys.Use(activeTransfer, b.Local, now, func(active context.Context, m *endpoint.Material) error {
		if verifyParties(b.Context, m.Bundle, peer) != nil {
			return endpoint.ErrTrust
		}
		public, e := peer.Content.Bytes("X25519_SEALED_BOX")
		if e != nil {
			return e
		}
		// Text input is bounded and completely checked before sealing. It is
		// never reconstructed from a platform plaintext parser.
		if b.Context.Purpose == "INPUT" {
			raw, e := io.ReadAll(io.LimitReader(source, b.Limits.DataBytes+1))
			if e != nil {
				return cipherstream.ErrRecord
			}
			defer clear(raw)
			if int64(len(raw)) > b.Limits.DataBytes || active.Err() != nil {
				return cipherstream.ErrLimit
			}
			if _, e = core.Preflight(core.Profile(b.Context.APIProfile), b.PublicModel, raw); e != nil {
				return e
			}
			w, e := cipherstream.NewWriter(destination, b.Context, public, m.Signing, b.Limits, now)
			if e != nil {
				return e
			}
			defer w.Close()
			if e = w.Headers(headers); e != nil {
				return e
			}
			if e = w.Data(raw); e != nil {
				return e
			}
			if active.Err() != nil {
				return endpoint.ErrClosed
			}
			if e = w.End(outcome); e != nil {
				return e
			}
			manifest, e = w.Manifest(expires, time.Now().UTC())
			return e
		}
		w, e := cipherstream.NewWriter(destination, b.Context, public, m.Signing, b.Limits, now)
		if e != nil {
			return e
		}
		defer w.Close()
		if e = w.Headers(headers); e != nil {
			return e
		}
		buffer := make([]byte, cipherstream.MaximumPayloadBytes)
		defer clear(buffer)
		var total int64
		emptyReads := 0
		for {
			if active.Err() != nil {
				return endpoint.ErrClosed
			}
			n, re := source.Read(buffer)
			if n < 0 || n > len(buffer) {
				return cipherstream.ErrRecord
			}
			if n > 0 {
				if int64(n) > b.Limits.DataBytes-total {
					return cipherstream.ErrLimit
				}
				total += int64(n)
				emptyReads = 0
				if e = w.Data(buffer[:n]); e != nil {
					return e
				}
				clear(buffer[:n])
			} else if re == nil {
				emptyReads++
				if emptyReads > 8 {
					return io.ErrNoProgress
				}
			}
			if re == io.EOF {
				break
			}
			if re != nil {
				return cipherstream.ErrRecord
			}
		}
		if active.Err() != nil {
			return endpoint.ErrClosed
		}
		if e = w.End(outcome); e != nil {
			return e
		}
		manifest, e = w.Manifest(expires, time.Now().UTC())
		return e
	})
	return manifest, e
}

// ConsumeObject sends authenticated prefixes only to a trusted in-process
// consumer. It is NOT a UI/seller plaintext export API. The consumer must not
// call a model before full INPUT authentication/semantic preflight and actual
// original lease/gate checks. OUTPUT prefixes are not acceptance or billing.
func ConsumeObject(ctx context.Context, keys *endpoint.Vault, b Binding, source io.Reader, manifest cipherstream.Manifest, now time.Time, consume func(context.Context, cipherstream.Record) error) error {
	if ctx == nil || keys == nil || source == nil || consume == nil || b.Local.Purpose != "OPEN_CONTENT" {
		return endpoint.ErrTrust
	}
	activeTransfer, cancel, e := transferContext(ctx, b, now)
	if e != nil {
		return e
	}
	defer cancel()
	peer, e := keys.Peer(b.Peer, now)
	if e != nil {
		return e
	}
	b.Local.PeerBundleHash = b.Peer.BundleHash
	b.Local.PeerAllowRetired = b.Peer.AllowRetired
	return keys.Use(activeTransfer, b.Local, now, func(active context.Context, m *endpoint.Material) error {
		if verifyParties(b.Context, peer, m.Bundle) != nil {
			return endpoint.ErrTrust
		}
		sp, e := peer.Identity.Bytes("Ed25519")
		if e != nil {
			return e
		}
		rp, e := m.Bundle.Content.Bytes("X25519_SEALED_BOX")
		if e != nil {
			return e
		}
		expected := cipherstream.Expected{Context: b.Context, SenderPublic: ed25519.PublicKey(sp), RecipientPublic: rp, Limits: b.Limits}
		r, e := cipherstream.NewReader(source, expected, m.ContentSecret, now)
		if e != nil {
			return e
		}
		defer r.Close()
		if b.Context.Purpose != "OUTPUT" {
			// No INPUT or CONTROL side effect can observe an authenticated
			// prefix. Buffer only bounded input bytes and the two small control
			// records; authenticate FINAL/manifest/EOF before invoking consumer.
			body := make([]byte, 0, int(b.Limits.DataBytes))
			defer func() { clear(body[:cap(body)]) }()
			var headers, extra cipherstream.Record
			defer headers.Clear()
			defer extra.Clear()
			for {
				if active.Err() != nil {
					return endpoint.ErrClosed
				}
				record, e := r.Next()
				if e != nil {
					return e
				}
				switch record.Type {
				case cipherstream.HeadersRecord:
					headers = record
					continue
				case cipherstream.DataRecord:
					if int64(len(record.Payload)) > b.Limits.DataBytes-int64(len(body)) {
						record.Clear()
						return cipherstream.ErrLimit
					}
					body = append(body, record.Payload...)
					record.Clear()
					continue
				case cipherstream.ErrorRecord, cipherstream.CancelRecord:
					extra.Clear()
					extra = record
					continue
				case cipherstream.EndRecord:
					defer record.Clear()
					if e = r.Finish(manifest, time.Now().UTC()); e != nil {
						return e
					}
					if active.Err() != nil {
						return endpoint.ErrClosed
					}
					if b.Context.Purpose == "INPUT" {
						if record.End.Outcome != "SUCCESS" {
							return cipherstream.ErrRecord
						}
						if _, e = core.Preflight(core.Profile(b.Context.APIProfile), b.PublicModel, body); e != nil {
							return e
						}
					}
					if e = consume(active, headers); e != nil {
						return e
					}
					for offset := 0; offset < len(body); {
						n := min(cipherstream.MaximumPayloadBytes, len(body)-offset)
						piece := cipherstream.Record{Type: cipherstream.DataRecord, Payload: body[offset : offset+n]}
						if e = consume(active, piece); e != nil {
							return e
						}
						offset += n
					}
					if extra.Type != 0 {
						if e = consume(active, extra); e != nil {
							return e
						}
					}
					return consume(active, record)
				default:
					record.Clear()
					return cipherstream.ErrRecord
				}
			}
		}
		for {
			if active.Err() != nil {
				return endpoint.ErrClosed
			}
			record, e := r.Next()
			if e != nil {
				return e
			}
			last := record.Type == cipherstream.EndRecord
			// END is presented only after the complete outer manifest and EOF.
			if last {
				if e = r.Finish(manifest, time.Now().UTC()); e != nil {
					record.Clear()
					return e
				}
				if active.Err() != nil {
					record.Clear()
					return endpoint.ErrClosed
				}
			}
			e = consume(active, record)
			record.Clear()
			if e != nil {
				return e
			}
			if last {
				return nil
			}
		}
	})
}
