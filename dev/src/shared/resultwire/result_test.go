package resultwire

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"
)

func count(n int64) *int64 { return &n }
func testCandidate() Candidate {
	return Candidate{Model: strings.Repeat("a", 64), Tokenizer: strings.Repeat("b", 64), Template: strings.Repeat("c", 64), Backend: "llama_native", CounterProfile: "OPENAI_TOTALS_1", Raw: Counts{Input: count(13), Output: count(7), Cached: count(4), Reasoning: count(2)}}
}
func testDeclaration(t *testing.T) (Delivery, ed25519.PrivateKey) {
	t.Helper()
	_, k, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	d := Delivery{Profile: DeliveryProfile, Scope: Scope{Job: "job_original", Attempt: "attempt_original", Lease: "lease_original", Order: "order_original", Seller: "user_seller", Device: "device_original", Resource: "resource_original", Generation: "9", Fence: "1", ContractHash: strings.Repeat("a", 64), ManifestHash: strings.Repeat("b", 64), Environment: strings.Repeat("c", 64)}, Result: "result_original", Revision: 1, OutputHash: strings.Repeat("d", 64), OutputBytes: 4, InputTokens: 13, OutputTokens: 7, Finish: "eog", Completed: time.Now().UTC().Format(time.RFC3339Nano)}
	d, e = SignDelivery(k, d)
	if e != nil {
		t.Fatal(e)
	}
	return d, k
}

func TestOpenAIInclusiveCacheAndReasoningNeverAddedTwice(t *testing.T) {
	c := testCandidate()
	n, e := c.Normalize()
	if e != nil || n.Input != 13 || n.Output != 7 {
		t.Fatal(n, e)
	}
}
func TestAnthropicDisjointInputIncludesReadAndCreationExactlyOnce(t *testing.T) {
	c := testCandidate()
	c.CounterProfile = "ANTHROPIC_DISJOINT_1"
	c.Raw.CacheCreation = count(3)
	n, e := c.Normalize()
	if e != nil || n.Input != 20 || n.Output != 7 {
		t.Fatal(n, e)
	}
}
func TestMissingTotalsAreNotZeroUsage(t *testing.T) {
	c := testCandidate()
	c.Raw.Output = nil
	if _, e := c.Normalize(); e == nil {
		t.Fatal("unknown output became zero")
	}
	c = testCandidate()
	c.Raw.Input = nil
	if _, e := c.Normalize(); e == nil {
		t.Fatal("unknown input became zero")
	}
}
func TestTokenCounterOverlapAndUnsupportedProfilesFail(t *testing.T) {
	for _, mutate := range []func(*Candidate){func(c *Candidate) { c.Raw.Cached = count(14) }, func(c *Candidate) { c.Raw.Reasoning = count(8) }, func(c *Candidate) { c.Raw.CacheCreation = count(1) }, func(c *Candidate) { c.CounterProfile = "ESTIMATED" }} {
		c := testCandidate()
		mutate(&c)
		if _, e := c.Normalize(); e == nil {
			t.Fatal("unsupported or overlapping quantities accepted")
		}
	}
}
func TestDisjointCacheRequiresExplicitCountsAndBounds(t *testing.T) {
	c := testCandidate()
	c.CounterProfile = "ANTHROPIC_DISJOINT_1"
	if _, e := c.Normalize(); e == nil {
		t.Fatal("unknown cache creation accepted")
	}
	c.Raw.CacheCreation = count(MaxTokens)
	if _, e := c.Normalize(); e == nil {
		t.Fatal("input overflow accepted")
	}
}
func TestFixedModelTokenizerTemplateAndBackendRequired(t *testing.T) {
	for _, mutate := range []func(*Candidate){func(c *Candidate) { c.Tokenizer = "" }, func(c *Candidate) { c.Template = "approximate" }, func(c *Candidate) { c.Model = "model_name" }, func(c *Candidate) { c.Backend = "" }} {
		c := testCandidate()
		mutate(&c)
		if _, e := c.Normalize(); e == nil {
			t.Fatal("unfixed counting recipe accepted")
		}
	}
}
func TestDeliverySignatureBindsOriginalOwnerAndPhysicalFence(t *testing.T) {
	d, k := testDeclaration(t)
	if d.Verify(k.Public().(ed25519.PublicKey)) != nil {
		t.Fatal("original rejected")
	}
	for _, mutate := range []func(*Delivery){func(d *Delivery) { d.Scope.Device = "device_other" }, func(d *Delivery) { d.Scope.Attempt = "attempt_other" }, func(d *Delivery) { d.Scope.Generation = "10" }, func(d *Delivery) { d.Scope.Fence = "2" }, func(d *Delivery) { d.Scope.Order = "order_other" }, func(d *Delivery) { d.Scope.ContractHash = strings.Repeat("0", 64) }} {
		x := d
		mutate(&x)
		if x.Verify(k.Public().(ed25519.PublicKey)) == nil {
			t.Fatal("changed owner verified")
		}
	}
}
func TestDeliverySignatureBindsAllCompletedOutputMetadata(t *testing.T) {
	d, k := testDeclaration(t)
	for _, mutate := range []func(*Delivery){func(d *Delivery) { d.OutputBytes++ }, func(d *Delivery) { d.OutputHash = strings.Repeat("0", 64) }, func(d *Delivery) { d.InputTokens++ }, func(d *Delivery) { d.Finish = "limit" }} {
		x := d
		mutate(&x)
		if x.Verify(k.Public().(ed25519.PublicKey)) == nil {
			t.Fatal("changed result verified")
		}
	}
}
func TestIncompleteFailureAndUnboundedOutputCannotBeSigned(t *testing.T) {
	d, k := testDeclaration(t)
	d.Finish = "failed"
	if _, e := SignDelivery(k, d); e == nil {
		t.Fatal("failed stream signed complete")
	}
	d.Finish = "eog"
	d.OutputBytes = 33 << 20
	if _, e := SignDelivery(k, d); e == nil {
		t.Fatal("unbounded output signed")
	}
}
func TestResultSignatureRejectsNonCanonicalCounterTimeAndSignature(t *testing.T) {
	d, _ := testDeclaration(t)
	d.Scope.Fence = "01"
	if d.Validate() == nil {
		t.Fatal("counter alias accepted")
	}
	d, _ = testDeclaration(t)
	d.Completed = "2026-11-06T12:00:00+00:00"
	if d.Validate() == nil {
		t.Fatal("time alias accepted")
	}
	d, _ = testDeclaration(t)
	d.Signature += "="
	if d.Validate() == nil {
		t.Fatal("signature alias accepted")
	}
}
func TestIndependentMeterSignatureCannotBeReplacedByDeviceSignature(t *testing.T) {
	d, device := testDeclaration(t)
	_, meter, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	s := Statement{Profile: UsageProfile, Scope: d.Scope, DeliveryHash: Digest(d), KeyID: "meter_original", Evidence: "evidence_original", Candidate: testCandidate(), Recorded: d.Completed}
	s, e = SignStatement(meter, s)
	if e != nil || s.Verify(meter.Public().(ed25519.PublicKey)) != nil {
		t.Fatal(e)
	}
	if s.Verify(device.Public().(ed25519.PublicKey)) == nil {
		t.Fatal("seller key became an independent meter")
	}
	s.DeliveryHash = strings.Repeat("0", 64)
	if s.Verify(meter.Public().(ed25519.PublicKey)) == nil {
		t.Fatal("meter reused for another delivery")
	}
}
func TestMeterSignatureCoversRawTokenProfileAndRecipe(t *testing.T) {
	d, k := testDeclaration(t)
	s := Statement{Profile: UsageProfile, Scope: d.Scope, DeliveryHash: Digest(d), KeyID: "meter_original", Evidence: "evidence_original", Candidate: testCandidate(), Recorded: d.Completed}
	s, e := SignStatement(k, s)
	if e != nil {
		t.Fatal(e)
	}
	s.Candidate.Raw.Input = count(14)
	if s.Verify(k.Public().(ed25519.PublicKey)) == nil {
		t.Fatal("changed input count verified")
	}
}
