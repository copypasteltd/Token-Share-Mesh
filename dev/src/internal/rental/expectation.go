package rental

import (
	"context"

	"github.com/jackc/pgx/v5"
	"p11.local/compute/client/protocol/rentalaccess"
	"p11.local/compute/internal/identity"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
)

// GetGrantExpectation independently reads the committed grant digest for the
// original signed device. The supplied grant ID is only a lookup key. It cannot
// assert a digest, buyer, payment, environment, availability or physical owner.
func (s *Service) GetGrantExpectation(ctx context.Context, p identity.Principal, id, grant string) (rentalaccess.GrantExpectation, error) {
	var result rentalaccess.GrantExpectation
	if err := s.configured(); err != nil {
		return result, err
	}
	if !ids.Valid(id) || !ids.Valid(grant) || p.Kind != identity.Device || p.Require("device:rental", "") != nil {
		return result, fault.New(fault.Invalid, "Original signed device and grant lookup required")
	}
	err := s.db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		if err := s.authorizeSession(ctx, p, id, "device:rental", true)(tx); err != nil {
			return err
		}
		o, err := lockSessionOrder(ctx, tx, id, p, false)
		if err != nil {
			return err
		}
		r, err := loadSessionTx(ctx, tx, id, o)
		if err != nil {
			return err
		}
		in := rentalaccess.ControlRequest{Grant: grant, Sequence: "1"}
		err = tx.QueryRow(ctx, `SELECT digest,authority_session_version,resource_lock_generation::text,terms_revision::text,terms_hash
FROM p11.rental_access_grants WHERE id=$1 AND session_id=$2 AND subject_id=$3 AND authority_profile=$4 FOR SHARE`, grant, id, o.Buyer, rentalaccess.Profile).
			Scan(&in.Digest, &in.ExpectedVersion, &in.Generation, &in.Terms, &in.TermsHash)
		if err == pgx.ErrNoRows {
			return fault.New(fault.NotFound, "Original rental grant not found")
		}
		if err != nil {
			return err
		}
		g, err := loadControlGrantTx(ctx, tx, r, in)
		if err != nil {
			return err
		}
		v := r.Session
		if g.Revoked || v.State != "ACTIVE" || v.Version != g.Version || !o.Now.Before(v.HardDeadline) || g.Metadata.Claims.Issuer != s.issuer || g.Metadata.KeyID != s.keyID {
			return fault.New(fault.Conflict, "Current original rental grant required")
		}
		buyer, err := controlBuyerTx(ctx, tx, o.Buyer)
		if err != nil {
			return err
		}
		if !buyer {
			return fault.New(fault.Forbidden, "Current original buyer required")
		}
		funds, resource, err := s.currentWorkTx(ctx, tx, o, r)
		if err != nil {
			return err
		}
		if resource.Phase != "ACTIVE" || !funds.covers(v.Contract, v.Budget, o.Now, v.HardDeadline) {
			return fault.New(fault.Conflict, "Current original rental capacity and funds required")
		}
		// Bind also validates the original stored claim lifetime/shape. Supplying
		// the independent original tuple here does not issue/recover a Bearer.
		m := g.Metadata
		c := m.Claims
		result = rentalaccess.GrantExpectation{Profile: rentalaccess.ExpectationProfile, Metadata: m, SessionVersion: g.Version, Connection: r.Connection}
		e := rentalaccess.Expected{Issuer: s.issuer, Subject: o.Buyer, Audience: v.Contract.Device, Grant: grant, Order: o.ID, Session: v.ID, Seller: o.Seller, Device: v.Contract.Device, Resource: v.Contract.Resource, Generation: v.Generation, Terms: in.Terms, TermsHash: v.TermsHash, Environment: v.Contract.Environment, EnvironmentHash: v.Contract.EnvironmentHash, Manifest: v.Contract.Manifest, ManifestHash: v.Contract.ManifestHash, HardDeadline: c.HardDeadline}
		if _, err = result.Bind(e, o.Now); err != nil {
			return fault.New(fault.Conflict, "Original stored rental metadata is not currently valid")
		}
		return nil
	})
	if err != nil {
		return rentalaccess.GrantExpectation{}, err
	}
	return result, nil
}
