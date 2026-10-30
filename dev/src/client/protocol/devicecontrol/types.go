package devicecontrol

import (
	"encoding/json"
	"time"
)

// These control DTOs are shared by the API and native providers. They contain
// no customer prompts, billable usage or content keys. Signed lease delivery
// is separate metadata and never enables an execution backend automatically.
type Enrollment struct {
	PairingID     string `json:"pairing_id"`
	PublicKey     string `json:"device_public_key"`
	KeyProfile    string `json:"key_profile_id"`
	OSBuild       string `json:"os_build"`
	ClientRelease string `json:"client_release_id"`
	Fingerprint   string `json:"device_fingerprint_hash"`
	Kind          string `json:"resource_kind"`
	Model         string `json:"model_name"`
}
type Device struct {
	ID                string          `json:"device_id"`
	SellerID          string          `json:"seller_id"`
	Platform          string          `json:"platform"`
	Kind              string          `json:"resource_kind"`
	State             string          `json:"state"`
	Version           int64           `json:"version"`
	LastSeen          *time.Time      `json:"last_seen_at"`
	HeartbeatSequence *string         `json:"heartbeat_sequence"`
	ReportedState     string          `json:"reported_resource_state"`
	ActiveSlots       int64           `json:"active_slots"`
	Capabilities      []string        `json:"capability_ids"`
	ClientRelease     string          `json:"client_release_id"`
	Label             string          `json:"device_label"`
	Model             *string         `json:"model_name"`
	IdentityProfile   string          `json:"identity_profile_id"`
	KeyFingerprint    string          `json:"key_fingerprint"`
	Policy            json.RawMessage `json:"resource_policy"`
	Replayed          bool            `json:"-"`
}
type Heartbeat struct {
	Sequence string    `json:"sequence"`
	Version  int64     `json:"device_version"`
	Recorded time.Time `json:"recorded_at"`
	Attempt  string    `json:"active_attempt_id,omitempty"`
	Fence    string    `json:"fencing_token,omitempty"`
	State    string    `json:"resource_state"`
}
type Ack struct {
	ID       string    `json:"id"`
	Version  int64     `json:"version"`
	Status   string    `json:"status"`
	Recorded time.Time `json:"recorded_at"`
	Replayed bool      `json:"-"`
}
