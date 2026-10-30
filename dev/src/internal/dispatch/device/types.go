// Package device registers provider identities and records control liveness.
// Enrollment/heartbeats never qualify capabilities or release execution locks.
package device

import (
	"p11.local/compute/client/protocol/devicecontrol"
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
type Enrollment = devicecontrol.Enrollment
type View = devicecontrol.Device
type Page struct {
	Items      []View  `json:"items"`
	NextCursor *string `json:"next_cursor"`
}
type Heartbeat = devicecontrol.Heartbeat
type Ack = devicecontrol.Ack
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
