package comfyworker

// SOURCE_ONLY private frame metadata. No process, engine, GPU or buyer ACK.
import (
	"encoding/json"
	"testing"

	"p11.local/compute/client/execution"
)

func mediaFrameCall() *MediaCall {
	a := &RentalMediaAdapter{}
	c := &MediaCall{adapter: a, invocation: execution.RentalInvocation{ID: "invoke_original", Session: "session_original", LocalGeneration: "1"}, backend: "comfy_original", senderDone: true, done: make(chan struct{})}
	a.active = c
	return c
}
func TestRentalMediaStopFrameCannotAssertSuccessWithoutOwnedResult(t *testing.T) {
	c := mediaFrameCall()
	raw := []byte(`{"invocation_id":"invoke_original","work_stopped":true,"future_start_revoked":true,"input_keys_cleared":true,"output_keys":"PRIVATE_HOST_TRANSFERRED","result_succeeded":true}`)
	if c.adapter.acceptFrame(Stopped, raw) == nil {
		t.Fatal("manufactured successful result")
	}
	if c.Observation().State == "STOPPED" {
		t.Fatal("invalid frame generated stop proof")
	}
}
func TestRentalMediaStopFrameBindsOriginalIDAndAllCleanupFacts(t *testing.T) {
	for _, mutate := range []func(map[string]any){func(v map[string]any) { v["invocation_id"] = "foreign_invocation" }, func(v map[string]any) { v["work_stopped"] = false }, func(v map[string]any) { v["future_start_revoked"] = false }, func(v map[string]any) { v["input_keys_cleared"] = false }, func(v map[string]any) { v["output_keys"] = "UNKNOWN" }} {
		c := mediaFrameCall()
		v := map[string]any{"invocation_id": "invoke_original", "work_stopped": true, "future_start_revoked": true, "input_keys_cleared": true, "output_keys": "REVOKED", "result_succeeded": false}
		mutate(v)
		raw, _ := json.Marshal(v)
		if c.adapter.acceptFrame(Stopped, raw) == nil {
			t.Fatal("foreign/incomplete stop evidence accepted")
		}
	}
}
func TestRentalMediaResultBeforeAcceptedOrAfterStopIsRejected(t *testing.T) {
	c := mediaFrameCall()
	if c.adapter.acceptFrame(Result, []byte(`{}`)) == nil {
		t.Fatal("result before accepted")
	}
	c.stopped = true
	if c.adapter.acceptFrame(Result, []byte(`{}`)) == nil {
		t.Fatal("result after stop")
	}
}
func TestRentalMediaSenderAndWorkerStopMustBothJoin(t *testing.T) {
	c := mediaFrameCall()
	c.senderDone = false
	raw := []byte(`{"invocation_id":"invoke_original","work_stopped":true,"future_start_revoked":true,"input_keys_cleared":true,"output_keys":"REVOKED","result_succeeded":false}`)
	if c.adapter.acceptFrame(Stopped, raw) != nil {
		t.Fatal("synthetic stop")
	}
	select {
	case <-c.done:
		t.Fatal("joined before content sender released input keys")
	default:
	}
	c.adapter.mu.Lock()
	c.senderDone = true
	c.adapter.tryJoined(c)
	c.adapter.mu.Unlock()
	select {
	case <-c.done:
	default:
		t.Fatal("actual metadata joins missing")
	}
	if c.Observation().State != "STOPPED" {
		t.Fatal("original stop observation unavailable")
	}
}
func TestRentalMediaUnknownDoesNotBecomeAbsenceOrResultAuthority(t *testing.T) {
	c := mediaFrameCall()
	c.unknown = true
	c.adapter.processJoined = true
	c.adapter.tryJoined(c)
	if c.Observation().State != "UNKNOWN" {
		t.Fatal("process absence fabricated stopped work")
	}
	if _, e := c.Result(); e == nil {
		t.Fatal("unknown state delivered content")
	}
}
