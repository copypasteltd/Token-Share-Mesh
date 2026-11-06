package endpointdirectory_test

import (
	"p11.local/compute/internal/testsupport"
	"p11.local/compute/shared/canonicaljson"
	"p11.local/compute/shared/endpointdirectory"
	"strings"
	"testing"
	"time"
)

func directoryRecord(t *testing.T) endpointdirectory.Record {
	t.Helper()
	now := time.Now().UTC()
	f := testsupport.PublicEndpoint(t, "owner_public", "endpoint_public", "BUYER", 1, now, now.Add(time.Hour))
	return endpointdirectory.Record{Profile: endpointdirectory.Profile, Recipient: f.Bundle.Content.ID, Bundle: f.Bundle, BundleHash: f.Bundle.Hash(), State: "ACTIVE", IndependentTrustRequired: true}
}
func TestDirectoryPublicRecordCannotClaimIndependentTrust(t *testing.T) {
	v := directoryRecord(t)
	if v.Validate() != nil {
		t.Fatal("valid public directory metadata rejected")
	}
	v.IndependentTrustRequired = false
	if v.Validate() == nil {
		t.Fatal("directory promoted itself into endpoint trust")
	}
}
func TestDirectoryRecordBindsExactRecipientAndRotationMetadata(t *testing.T) {
	for _, kind := range []string{"recipient", "hash", "profile", "state", "initial_rotation"} {
		t.Run(kind, func(t *testing.T) {
			v := directoryRecord(t)
			switch kind {
			case "recipient":
				v.Recipient = "recipient_other"
			case "hash":
				v.BundleHash = strings.Repeat("b", 64)
			case "profile":
				v.Profile = "P11_OTHER_1"
			case "state":
				v.State = "EXECUTION_QUALIFIED"
			case "initial_rotation":
				v.Previous = "recipient_other"
			}
			if v.Validate() == nil {
				t.Fatal("unbound directory metadata accepted")
			}
		})
	}
}
func TestDirectoryTwoLargestShapedRecordsStayWithinReplyBudget(t *testing.T) {
	now := time.Now().UTC()
	old := testsupport.PublicEndpoint(t, strings.Repeat("o", 128), strings.Repeat("e", 128), "BUYER", 1, now.Add(-time.Hour), now.Add(time.Hour))
	next, proof := testsupport.RotatePublicEndpoint(t, old, now, now.Add(time.Hour))
	v := endpointdirectory.Record{Profile: endpointdirectory.Profile, Recipient: next.Bundle.Content.ID, Bundle: next.Bundle, BundleHash: next.Bundle.Hash(), State: "ACTIVE", Previous: old.Bundle.Content.ID, TransitionHash: proof.Hash(), Rotation: &proof, IndependentTrustRequired: true}
	if v.VerifySuccessor(old.Bundle, now) != nil {
		t.Fatal("original caller trust continuity rejected")
	}
	page := endpointdirectory.Page{Items: []endpointdirectory.Record{v, v}}
	if _, e := canonicaljson.Encode(page, endpointdirectory.MaximumReply); e != nil {
		t.Fatal("bounded page exceeds transport limit", e)
	}
}
