package devicecontrol

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"time"

	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/strictjson"
)

const CleanupProfile = "P11_DEVICE_CLEANUP_1"

type ProgressWrite struct {
	Lease    string   `json:"lease_id"`
	Fence    string   `json:"fencing_token"`
	Sequence string   `json:"sequence"`
	Phase    string   `json:"phase"`
	Fraction *float64 `json:"fraction,omitempty"`
	Reason   string   `json:"safe_reason_code,omitempty"`
}
type CleanupWrite struct {
	Lease            string `json:"lease_id"`
	Fence            string `json:"fencing_token"`
	ManifestHash     string `json:"cleanup_manifest_hash"`
	State            string `json:"state"`
	WorkStopped      bool   `json:"work_stopped"`
	TokensRevoked    bool   `json:"tokens_revoked"`
	WorkspaceCleared bool   `json:"workspace_cleared"`
	Recorded         string `json:"recorded_at"`
	Profile          string `json:"signature_profile_id"`
	Signature        string `json:"signature"`
}
type ProgressAck struct {
	Ack    Ack    `json:"ack"`
	Action string `json:"action"`
}

// ReportCounter is canonical, positive and within the native issuer's signed
// 64-bit bound. Sequence zero is reserved for the original unreported lease.
func ReportCounter(s string) bool {
	v, e := strconv.ParseUint(s, 10, 63)
	return e == nil && v > 0 && strconv.FormatUint(v, 10) == s
}
func (in ProgressWrite) Validate() error {
	phase := in.Phase == "ACKED" || in.Phase == "RUNNING" || in.Phase == "UPLOADING" || in.Phase == "FAILED" || in.Phase == "CANCELED"
	if !ids.Valid(in.Lease) || !ReportCounter(in.Fence) || !ReportCounter(in.Sequence) || !phase || in.Reason != "" && !ids.Valid(in.Reason) || in.Fraction != nil && (math.IsNaN(*in.Fraction) || math.IsInf(*in.Fraction, 0) || *in.Fraction < 0 || *in.Fraction > 1) {
		return errors.New("bounded original progress required")
	}
	return nil
}
func (in CleanupWrite) Validate() error {
	t, e := time.Parse(time.RFC3339Nano, in.Recorded)
	b, se := base64.RawURLEncoding.Strict().DecodeString(in.Signature)
	if !ids.Valid(in.Lease) || !ReportCounter(in.Fence) || !executionlease.Hash(in.ManifestHash) || (in.State != "ACKED" && in.State != "FAILED") || in.State == "ACKED" && (!in.WorkStopped || !in.TokensRevoked || !in.WorkspaceCleared) || in.Profile != CleanupProfile || e != nil || t.IsZero() || in.Recorded != t.UTC().Format(time.RFC3339Nano) || se != nil || len(b) != ed25519.SignatureSize || base64.RawURLEncoding.EncodeToString(b) != in.Signature {
		return errors.New("original signed cleanup report required")
	}
	return nil
}

// CleanupMessage additionally binds the device, attempt and physical resource
// generation from the independently verified ORIGINAL lease. They cannot be
// replaced by caller-selected cleanup ownership or a later device occupant.
func CleanupMessage(device, attempt, generation string, in CleanupWrite) ([]byte, error) {
	if !ids.Valid(device) || !ids.Valid(attempt) || !ReportCounter(generation) {
		return nil, errors.New("original cleanup owner required")
	}
	unsigned := in
	unsigned.Signature = base64.RawURLEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
	if unsigned.Validate() != nil {
		return nil, errors.New("invalid cleanup facts")
	}
	unsigned.Signature = ""
	b, e := json.Marshal(unsigned)
	if e != nil {
		return nil, e
	}
	return append([]byte(CleanupProfile+"\n"+device+"\n"+attempt+"\n"+generation+"\n"), b...), nil
}
func SignCleanup(key ed25519.PrivateKey, l executionlease.Lease, in CleanupWrite) (CleanupWrite, error) {
	if len(key) != ed25519.PrivateKeySize || in.Lease != l.ID || in.Fence != l.Fence {
		return CleanupWrite{}, errors.New("original cleanup lease required")
	}
	m, e := CleanupMessage(l.Device, l.Attempt, l.Generation, in)
	if e != nil {
		return CleanupWrite{}, e
	}
	in.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, m))
	return in, nil
}
func (c *Client) PrepareProgress(attempt, key string, in ProgressWrite) (Prepared, error) {
	if !ids.Valid(attempt) || in.Validate() != nil {
		return Prepared{}, errors.New("original progress required")
	}
	b, _ := json.Marshal(in)
	return c.Prepare("POST", "/v1/p11/runner/attempts/"+attempt+"/progress", b, key, "")
}
func (c *Client) PrepareCleanup(attempt, key string, in CleanupWrite) (Prepared, error) {
	if !ids.Valid(attempt) || in.Validate() != nil {
		return Prepared{}, errors.New("original cleanup required")
	}
	b, _ := json.Marshal(in)
	return c.Prepare("POST", "/v1/p11/runner/attempts/"+attempt+"/cleanup", b, key, "")
}

// CleanupAck confirms receipt only; it supplies no lease, usage or charge right.
func CleanupAck(r Reply) (Ack, error) {
	var a Ack
	if r.Status != 200 || strictjson.Decode(r.Body, &a, 4096) != nil || !ids.Valid(a.ID) || a.Version != 1 || (a.Status != "CLEANED" && a.Status != "QUARANTINED") {
		return Ack{}, errors.New("bounded cleanup receipt required")
	}
	if a.Recorded.IsZero() {
		return Ack{}, errors.New("cleanup receipt time required")
	}
	return a, nil
}
