package execution

import (
	"context"
	"p11.local/compute/client/protocol/executionlease"
	"testing"
)

func TestNativeResultBindingSurvivesNextOwnerWithoutGrant(t *testing.T) {
	s, c, _ := newStore(t)
	ctx := context.Background()
	l := lease(c, "lease_result", "job_result", "1", "1")
	m := executionlease.Manifest{ID: l.Manifest, Resource: l.Resource, EnvironmentDigest: l.Environment, Capability: l.Capability}
	l.ManifestHash = m.Digest()
	h := receive(t, s, c, l)
	if s.ResultBinding(ctx, h, m) == nil {
		t.Fatal("unsubmitted inbox yielded result scope")
	}
	if _, e := s.BeginSubmit(ctx, h); e != nil {
		t.Fatal(e)
	}
	if e := s.RecordBackend(ctx, h, "backend_result"); e != nil {
		t.Fatal(e)
	}
	if e := s.ResultBinding(ctx, h, m); e != nil {
		t.Fatal(e)
	}
	clean(t, s, h, "backend_result")
	next, e := s.AcquireLocal(ctx, l.Resource, "local_result_next", "LOCAL")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ResultBinding(ctx, h, m); e != nil {
		t.Fatal("new owner replaced original result identity", e)
	}
	if s.ResultBinding(ctx, next, m) == nil {
		t.Fatal("managed local owner borrowed JOB result")
	}
	mutated := m
	mutated.Capability = "another_capability"
	if s.ResultBinding(ctx, h, mutated) == nil {
		t.Fatal("manifest substitution")
	}
	if _, e = s.BeginSubmit(ctx, h); e == nil {
		t.Fatal("result check renewed original execution")
	}
	clean(t, s, next, "local_backend")
}
