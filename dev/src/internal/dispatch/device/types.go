// Package device registers provider identities and records control liveness.
// Enrollment/heartbeats never qualify capabilities or release execution locks.
package device

import (
	"encoding/json"
	"p11.local/compute/internal/persistence"
	"time"
)

type Service struct {
	DB            *persistence.DB
	PepperVersion string
	Peppers       map[string][]byte
}
type PairInput struct {
	Platform  string `json:"platform"`
	Label     string `json:"device_label"`
	Confirmed bool   `json:"owner_confirmed"`
}
type Pairing struct {
	ID       string    `json:"pairing_id"`
	Token    string    `json:"one_time_token,omitempty"`
	Expires  time.Time `json:"expires_at"`
	SellerID string    `json:"seller_id"`
	Replayed bool      `json:"-"`
}
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
type View struct {
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
type Page struct {
	Items      []View  `json:"items"`
	NextCursor *string `json:"next_cursor"`
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
type Reason struct {
	Code     string   `json:"reason_code"`
	Version  int64    `json:"expected_version"`
	Evidence []string `json:"evidence_refs,omitempty"`
}
type Window struct {
	From  time.Time `json:"starts_at"`
	Until time.Time `json:"ends_at"`
}
type Policy struct {
	Version     int64    `json:"expected_version"`
	Windows     []Window `json:"windows"`
	Enabled     *bool    `json:"sharing_enabled"`
	Slots       int      `json:"active_slots"`
	FreeDisk    *int64   `json:"minimum_free_disk_bytes"`
	Battery     *int     `json:"minimum_battery_percent,omitempty"`
	Temperature *float64 `json:"maximum_temperature_celsius,omitempty"`
	Charging    *bool    `json:"charging_required,omitempty"`
	Stop        string   `json:"stop_policy"`
}
