package integration_test

import (
	"p11.local/compute/internal/endpointregistry"
	"p11.local/compute/internal/testsupport"
	"p11.local/compute/shared/endpointdirectory"
	"p11.local/compute/shared/fault"
	"testing"
)

func TestEndpointDirectoryGlobalPublicKeyClaimCannotBeAliasedByNewEndpoint(t *testing.T) {
	requireDB(t)
	p, _ := directoryBuyer(t)
	s := &endpointregistry.Service{DB: db}
	original := directoryInitial(t, p)
	directoryRegister(t, s, p, original)
	next := directoryInitial(t, p)
	next.Bundle.Content.Public = original.Bundle.Content.Public
	next.Bundle.Content.Fingerprint = original.Bundle.Content.Fingerprint
	testsupport.SignPublicEndpoint(t, &next)
	_, e := s.Register(ctx, p, endpointdirectory.Registration{Bundle: next.Bundle}, "alias_original_public_key")
	assertCode(t, e, fault.Conflict)
	page, e := s.List(ctx, p, "")
	if e != nil || len(page.Items) != 1 || page.Items[0].BundleHash != original.Bundle.Hash() {
		t.Fatal("failed alias changed original registration", page, e)
	}
}
