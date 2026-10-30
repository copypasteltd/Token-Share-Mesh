package node

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"net/http"
	"p11.local/compute/client/protocol/devicecontrol"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/strictjson"
	"strconv"
	"time"
)

var ErrPending = errors.New("original control command remains pending; recover it before creating another")
var ErrAuthority = errors.New("device authorization unavailable; inspect account, device and clock before explicitly retrying")
var ErrRejected = errors.New("control command rejected; inspect local control status")
var ErrEnrollment = errors.New("explicit enrollment or original enrollment recovery required")
var ErrCounter = errors.New("device heartbeat counter exhausted; explicitly retire and register a new identity")

// Status is the only printable local projection. It contains neither control
// seeds/pair credentials nor raw requests, response bodies or customer content.
type Status struct {
	ControlBase      string  `json:"control_base"`
	DeviceID         string  `json:"device_id"`
	Model            string  `json:"model_name"`
	Platform         string  `json:"platform"`
	Kind             string  `json:"resource_kind"`
	KeyProfile       string  `json:"key_profile_id"`
	Fingerprint      string  `json:"key_fingerprint"`
	Version          int64   `json:"device_version"`
	RemoteState      string  `json:"last_known_remote_state"`
	Sequence         *string `json:"reserved_sequence"`
	Pending          string  `json:"pending_command"`
	Outcome          string  `json:"last_control_outcome"`
	ExecutionEnabled bool    `json:"execution_enabled"`
}

func (s *Store) Status() Status {
	pending := ""
	if s.s.Pending != nil {
		pending = s.s.Pending.Kind
	}
	var sequence *string
	if s.s.Sequence != nil {
		copy := *s.s.Sequence
		sequence = &copy
	}
	return Status{ControlBase: s.s.Config.Base, DeviceID: s.s.DeviceID, Model: s.s.Config.Model, Platform: s.s.Platform, Kind: s.s.Config.Kind, KeyProfile: devicecontrol.KeyProfile, Fingerprint: s.fingerprint(), Version: s.s.Version, RemoteState: s.s.RemoteState, Sequence: sequence, Pending: pending, Outcome: s.s.LastOutcome}
}
func (s *Store) Enroll(ctx context.Context, token string) error {
	if s.s.DeviceID != "" || s.s.Pending != nil || s.poisoned {
		return ErrPending
	}
	id, err := devicecontrol.PairingID(token)
	if err != nil {
		return ErrEnrollment
	}
	body, err := json.Marshal(s.enrollment(id))
	if err != nil {
		return ErrState
	}
	key, err := randomHex(24)
	if err != nil {
		return err
	}
	s.s.Pending = &command{Kind: "ENROLLMENT", Key: "node_enroll_" + key, Body: body, Credential: token}
	s.s.LastOutcome = "ENROLLMENT_PENDING"
	if err = s.save(); err != nil {
		return err
	}
	return s.Recover(ctx)
}

func (s *Store) client() (*devicecontrol.Client, error) {
	k := s.key()
	defer clear(k)
	return devicecontrol.NewClient(s.s.Config.Base, s.s.DeviceID, k)
}
func (s *Store) validView(v devicecontrol.Device) bool {
	return ids.Valid(v.ID) && ids.Valid(v.SellerID) && (s.s.DeviceID == "" || v.ID == s.s.DeviceID && v.SellerID == s.s.SellerID) && v.IdentityProfile == devicecontrol.KeyProfile && v.KeyFingerprint == s.fingerprint() && v.Platform == s.s.Platform && v.Kind == s.s.Config.Kind && v.Model != nil && *v.Model == s.s.Config.Model && v.ClientRelease == s.s.Config.Release && v.Version > 0 && v.Version < 2147483647
}
func (s *Store) adopt(v devicecontrol.Device) error {
	if !s.validView(v) || v.Version < s.s.Version {
		return ErrState
	}
	if v.HeartbeatSequence != nil {
		remote, err := counter(*v.HeartbeatSequence)
		if err != nil {
			return ErrState
		}
		var local uint64
		if s.s.Sequence != nil {
			local, err = counter(*s.s.Sequence)
			if err != nil {
				return ErrState
			}
		}
		if s.s.Sequence == nil || remote > local {
			value := *v.HeartbeatSequence
			s.s.Sequence = &value
		}
	}
	s.s.DeviceID = v.ID
	s.s.SellerID = v.SellerID
	s.s.Version = v.Version
	s.s.RemoteState = v.State
	return nil
}
func (s *Store) fetch(ctx context.Context) (devicecontrol.Device, error) {
	var v devicecontrol.Device
	if s.s.DeviceID == "" {
		return v, ErrEnrollment
	}
	c, err := s.client()
	if err != nil {
		return v, ErrState
	}
	defer c.Close()
	p, err := c.Prepare(http.MethodGet, "/v1/p11/devices/"+s.s.DeviceID, nil, "", "")
	if err != nil {
		return v, ErrState
	}
	r, err := c.Send(ctx, p)
	if err != nil {
		return v, err
	}
	if r.Status != 200 {
		return v, classify(r)
	}
	if strictjson.Decode(r.Body, &v, 1048576) != nil || !s.validView(v) {
		return v, ErrState
	}
	return v, nil
}
func (s *Store) refresh(ctx context.Context) error {
	v, err := s.fetch(ctx)
	if err != nil {
		return err
	}
	if s.adopt(v) != nil {
		return ErrState
	}
	return s.save()
}

// Refresh checks current authority/configuration, but never clears a pending
// command. Recovery must first establish that command's original result.
func (s *Store) Refresh(ctx context.Context) error {
	if s.s.Pending != nil {
		return ErrPending
	}
	return s.refresh(ctx)
}

func classify(r devicecontrol.Reply) error {
	if r.Status == 401 || r.Status == 403 || r.Status == 404 {
		return ErrAuthority
	}
	if r.Status >= 400 && r.Status < 500 && r.Status != 408 && r.Status != 429 {
		return ErrRejected
	}
	return &devicecontrol.TransportError{Unknown: true}
}
func knownRejection(r devicecontrol.Reply) bool {
	if r.Status != 400 && r.Status != 409 {
		return false
	}
	var e struct {
		Error struct {
			Code      string         `json:"code"`
			Message   string         `json:"message"`
			Retryable bool           `json:"retryable"`
			RequestID string         `json:"request_id"`
			Details   map[string]any `json:"details"`
		} `json:"error"`
	}
	return strictjson.Decode(r.Body, &e, 1048576) == nil && ((r.Status == 400 && e.Error.Code == "INVALID_REQUEST") || (r.Status == 409 && e.Error.Code == "CONFLICT"))
}

// Recover signs anew but preserves the exact persisted command and key. The
// journal is cleared only after a validated acknowledgement or an authoritative
// heartbeat rejection followed by a current signed device read.
func (s *Store) Recover(ctx context.Context) error {
	p := s.s.Pending
	if p == nil {
		return nil
	}
	if s.poisoned || s.validate() != nil {
		return ErrState
	}
	if p.Kind == "ENROLLMENT" {
		c, err := devicecontrol.NewEnrollmentClient(s.s.Config.Base)
		if err != nil {
			return ErrState
		}
		defer c.Close()
		r, err := c.Send(ctx, p.Body, p.Credential, p.Key)
		if err != nil {
			return err
		}
		if r.Status != 201 {
			return classify(r)
		}
		var v devicecontrol.Device
		if strictjson.Decode(r.Body, &v, 1048576) != nil || !s.validView(v) {
			return &devicecontrol.TransportError{Unknown: true}
		}
		s.s.Pending = nil
		if err = s.adopt(v); err != nil {
			s.s.Pending = p
			return err
		}
		s.s.LastOutcome = "REGISTERED_EXECUTION_DISABLED"
		if err = s.save(); err != nil {
			return err
		}
		p.Credential = ""
		clear(p.Body)
		return nil
	}
	c, err := s.client()
	if err != nil {
		return ErrState
	}
	defer c.Close()
	prepared, err := c.Prepare(http.MethodPost, "/v1/p11/devices/"+s.s.DeviceID+"/heartbeats", p.Body, p.Key, "")
	if err != nil {
		return ErrState
	}
	r, err := c.Send(ctx, prepared)
	if err != nil {
		return err
	}
	if r.Status != 200 {
		if knownRejection(r) {
			// No new command until the explicit original rejection is followed
			// by a successful current read. Keep the reserved local high water.
			v, e := s.fetch(ctx)
			if e != nil {
				return e
			}
			s.s.Pending = nil
			if s.adopt(v) != nil {
				s.s.Pending = p
				return ErrState
			}
			s.s.LastOutcome = "HEARTBEAT_REJECTED_REFRESHED"
			return s.save()
		}
		return classify(r)
	}
	var ack devicecontrol.Ack
	var h devicecontrol.Heartbeat
	if strictjson.Decode(p.Body, &h, devicecontrol.MaximumBody) != nil || strictjson.Decode(r.Body, &ack, 1048576) != nil || ack.ID != s.s.DeviceID || ack.Version != h.Version || ack.Status != "RECORDED" || ack.Recorded.IsZero() {
		return &devicecontrol.TransportError{Unknown: true}
	}
	s.s.Pending = nil
	s.s.LastOutcome = "HEARTBEAT_RECORDED"
	if err = s.save(); err != nil {
		return err
	}
	clear(p.Body)
	return nil
}

// Step emits UNKNOWN only: no qualified probe/engine exists in this agent yet.
// It cannot falsely advertise IDLE, produce usage, unlock a GPU or acknowledge
// a task's cleanup. A recovered old heartbeat is not treated as fresh liveness.
func (s *Store) Step(ctx context.Context) error {
	if s.s.DeviceID == "" {
		return ErrEnrollment
	}
	if s.s.Pending != nil {
		return s.Recover(ctx)
	}
	if err := s.refresh(ctx); err != nil {
		return err
	}
	var sequence uint64
	if s.s.Sequence != nil {
		v, err := counter(*s.s.Sequence)
		if err != nil {
			return err
		}
		if v == math.MaxUint64 {
			return ErrCounter
		}
		sequence = v + 1
	}
	key, err := randomHex(24)
	if err != nil {
		return err
	}
	value := strconv.FormatUint(sequence, 10)
	h := devicecontrol.Heartbeat{Sequence: value, Version: s.s.Version, Recorded: time.Now().UTC().Truncate(time.Microsecond), State: "UNKNOWN"}
	body, err := json.Marshal(h)
	if err != nil {
		return ErrState
	}
	s.s.Sequence = &value
	s.s.Pending = &command{Kind: "HEARTBEAT", Key: "node_heartbeat_" + key, Body: body}
	s.s.LastOutcome = "HEARTBEAT_PENDING"
	if err = s.save(); err != nil {
		return err
	}
	return s.Recover(ctx)
}

func retryable(err error) bool {
	var transport *devicecontrol.TransportError
	return errors.As(err, &transport)
}

// Run is foreground/outbound-only. It uses bounded jitter/backoff, rechecks
// current authority and stops on key/state/auth failures. It starts no engine.
func (s *Store) Run(ctx context.Context, report func(Status)) error {
	delay := 15 * time.Second
	failures := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		err := s.Step(ctx)
		if report != nil {
			report(s.Status())
		}
		if err != nil && !retryable(err) {
			return err
		}
		if err != nil {
			failures++
			seconds := min(60, 5*(1<<min(failures-1, 4)))
			n, e := rand.Int(rand.Reader, big.NewInt(int64(seconds)))
			if e != nil {
				return ErrState
			}
			delay = time.Duration(1+n.Int64()) * time.Second
		} else {
			failures = 0
			delay = 15 * time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}
