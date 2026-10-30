package device

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"strconv"
	"time"
)

func decimal(value string) (uint64, error) {
	v, err := strconv.ParseUint(value, 10, 64)
	if err != nil || strconv.FormatUint(v, 10) != value {
		return 0, fault.New(fault.Invalid, "Canonical unsigned counter required")
	}
	return v, nil
}
func healthyState(value string) bool {
	switch value {
	case "IDLE", "BUSY", "DRAINING", "HOT", "LOW_BATTERY", "LOW_DISK", "UNKNOWN":
		return true
	}
	return false
}
func (s Service) Heartbeat(ctx context.Context, p identity.Principal, id string, in Heartbeat, key string) (Ack, error) {
	var ack Ack
	if p.Kind != identity.Device || p.DeviceID != id {
		return ack, fault.New(fault.Forbidden, "Heartbeat requires this device's signing identity")
	}
	if !ids.Valid(id) || in.Version < 1 || in.Version >= 2147483647 || !healthyState(in.State) || in.Recorded.IsZero() {
		return ack, fault.New(fault.Invalid, "Invalid heartbeat")
	}
	if _, err := decimal(in.Sequence); err != nil {
		return ack, err
	}
	if (in.Attempt == "") != (in.Fence == "") {
		return ack, fault.New(fault.Invalid, "Attempt and fencing token must be supplied together")
	}
	if in.Attempt != "" {
		if !ids.Valid(in.Attempt) {
			return ack, fault.New(fault.Invalid, "Invalid active attempt")
		}
		v, err := decimal(in.Fence)
		if err != nil || v == 0 {
			return ack, fault.New(fault.Invalid, "Positive current fencing token required")
		}
	}
	in.Recorded = in.Recorded.UTC().Truncate(time.Microsecond)
	r, err := s.command(ctx, p, "recordDeviceHeartbeat", id, key, in, func(tx pgx.Tx) error { return authorizeDevice(ctx, tx, p, "device:heartbeat") }, func(tx pgx.Tx) (outbox.StoredResult, error) {
		v, err := viewTx(ctx, tx, id, p.UserID, true)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		if v.Version != in.Version {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Refresh current device policy/version before a new heartbeat")
		}
		var timely bool
		if err := tx.QueryRow(ctx, `SELECT abs(extract(epoch FROM (clock_timestamp()-$1::timestamptz)))<=300`, in.Recorded).Scan(&timely); err != nil {
			return outbox.StoredResult{}, err
		}
		if !timely {
			return outbox.StoredResult{}, fault.New(fault.Invalid, "Heartbeat recording time outside allowed skew")
		}
		if in.Attempt != "" {
			var found bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM p11.attempts a JOIN p11.jobs j ON j.id=a.job_id JOIN p11.resource_locks lk ON lk.resource_id=a.resource_id WHERE a.id=$1 AND a.device_id=$2 AND a.seller_id=$3 AND a.fencing_token=$4::numeric AND a.state IN ('ISSUED','ACKED','RUNNING','DELIVERED','CLEANING','QUARANTINED') AND j.current_attempt_id=a.id AND j.fencing_token=a.fencing_token AND lk.owner_attempt_id=a.id AND lk.lock_generation=a.resource_lock_generation AND lk.phase<>'FREE')`, in.Attempt, id, p.UserID, in.Fence).Scan(&found); err != nil {
				return outbox.StoredResult{}, err
			}
			if !found {
				return outbox.StoredResult{}, fault.New(fault.Conflict, "Reported attempt does not own current device execution")
			}
		}
		err = tx.QueryRow(ctx, `UPDATE p11.devices SET heartbeat_sequence=$3::numeric,reported_resource_state=$4,last_seen_at=clock_timestamp() WHERE id=$1 AND seller_id=$2 AND (heartbeat_sequence IS NULL OR heartbeat_sequence<$3::numeric) RETURNING last_seen_at`, id, p.UserID, in.Sequence, in.State).Scan(&ack.Recorded)
		if err == pgx.ErrNoRows {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Heartbeat sequence must increase; recover the original command")
		}
		if err != nil {
			return outbox.StoredResult{}, err
		}
		// A liveness report does not qualify hardware, free a lock, acknowledge
		// cleanup, transition ONLINE or provide trusted billable usage.
		ack.ID = id
		ack.Version = v.Version
		ack.Status = "RECORDED"
		return stored(id, 200, ack)
	})
	if err != nil {
		return Ack{}, err
	}
	if r.Replayed {
		if err = json.Unmarshal(r.Body, &ack); err != nil {
			return Ack{}, err
		}
		ack.Replayed = true
	}
	return ack, nil
}
func validReason(in Reason) bool {
	if !ids.Valid(in.Code) || in.Version < 1 || in.Version >= 2147483647 || len(in.Evidence) > 50 {
		return false
	}
	seen := map[string]bool{}
	for _, id := range in.Evidence {
		if !ids.Valid(id) || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}
func (s Service) ChangeState(ctx context.Context, p identity.Principal, id string, in Reason, key string, revoke bool) (View, error) {
	var view View
	if !ids.Valid(id) || !validReason(in) {
		return view, fault.New(fault.Invalid, "Valid device/version/reason required")
	}
	op, action, state := "drainDevice", "DeviceDrainRequested", "DRAINING"
	if revoke {
		op, action, state = "revokeDevice", "DeviceRevoked", "REVOKED"
	}
	auth := func(tx pgx.Tx) error { return authorizeOwner(ctx, tx, p, "device:manage", false, false) }
	r, err := s.command(ctx, p, op, id, key, in, auth, func(tx pgx.Tx) (outbox.StoredResult, error) {
		v, err := viewTx(ctx, tx, id, p.UserID, true)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		if v.Version != in.Version || v.State == "REVOKED" || v.State == "RETIRED" {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Device is terminal or version changed")
		}
		if len(in.Evidence) > 0 {
			var count int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM p11.evidence WHERE id=ANY($1::text[]) AND seller_id=$2`, in.Evidence, p.UserID).Scan(&count); err != nil {
				return outbox.StoredResult{}, err
			}
			if count != len(in.Evidence) {
				return outbox.StoredResult{}, fault.New(fault.NotFound, "Reason evidence not found")
			}
		}
		if !revoke && v.State == "QUARANTINED" {
			state = "QUARANTINED"
		}
		if _, err = tx.Exec(ctx, `UPDATE p11.devices SET state=$3,version=version+1,resource_policy=jsonb_set(jsonb_set(resource_policy,'{accepting_new_jobs}','false'),'{sharing_enabled}','false') WHERE id=$1 AND seller_id=$2`, id, p.UserID, state); err != nil {
			return outbox.StoredResult{}, err
		}
		if err = event(ctx, tx, p, id, v.Version+1, action, map[string]any{"reason_code": in.Code, "evidence_refs": in.Evidence}); err != nil {
			return outbox.StoredResult{}, err
		}
		view, err = viewTx(ctx, tx, id, p.UserID, false)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		return stored(id, 200, view)
	})
	if err != nil {
		return View{}, err
	}
	if r.Replayed {
		if err = json.Unmarshal(r.Body, &view); err != nil {
			return View{}, err
		}
		view.Replayed = true
	}
	return view, nil
}
func normalizePolicy(in Policy) (Policy, error) {
	bad := fault.New(fault.Invalid, "Explicit bounded resource policy required")
	if in.Version < 1 || in.Version >= 2147483647 || in.Enabled == nil || in.FreeDisk == nil || *in.FreeDisk < 0 || *in.FreeDisk > 9007199254740991 || in.Slots != 1 || in.Windows == nil || len(in.Windows) > 100 || in.Stop != "DRAIN" && in.Stop != "FORCE_WITH_RESPONSIBILITY" {
		return in, bad
	}
	if in.Battery != nil && (*in.Battery < 0 || *in.Battery > 100) || in.Temperature != nil && (*in.Temperature < 1 || *in.Temperature > 120) {
		return in, bad
	}
	in.Windows = append([]Window{}, in.Windows...)
	for i, w := range in.Windows {
		w.From = w.From.UTC().Truncate(time.Microsecond)
		w.Until = w.Until.UTC().Truncate(time.Microsecond)
		if w.From.IsZero() || !w.Until.After(w.From) || w.Until.Sub(w.From) > 30*24*time.Hour || i > 0 && w.From.Before(in.Windows[i-1].Until) {
			return in, bad
		}
		in.Windows[i] = w
	}
	if len(in.Windows) > 0 && in.Windows[len(in.Windows)-1].Until.Sub(in.Windows[0].From) > 90*24*time.Hour {
		return in, bad
	}
	if *in.Enabled && len(in.Windows) == 0 {
		return in, bad
	}
	return in, nil
}
func (s Service) UpdatePolicy(ctx context.Context, p identity.Principal, id string, etag int64, in Policy, key string) (Ack, error) {
	var ack Ack
	if !ids.Valid(id) || etag != in.Version {
		return ack, fault.New(fault.Invalid, "Matching strong version required")
	}
	in, err := normalizePolicy(in)
	if err != nil {
		return ack, err
	}
	auth := func(tx pgx.Tx) error { return authorizeOwner(ctx, tx, p, "device:manage", *in.Enabled, false) }
	r, err := s.command(ctx, p, "updateResourcePolicy", id, key, in, auth, func(tx pgx.Tx) (outbox.StoredResult, error) {
		v, err := viewTx(ctx, tx, id, p.UserID, true)
		if err != nil {
			return outbox.StoredResult{}, err
		}
		if v.Version != in.Version || v.State == "REVOKED" || v.State == "RETIRED" || v.State == "QUARANTINED" {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Device unavailable or policy version changed")
		}
		if v.Platform == "ANDROID" && (in.Battery == nil || in.Charging == nil || in.Temperature == nil) {
			return outbox.StoredResult{}, fault.New(fault.Invalid, "Phone battery, charging and temperature conditions required")
		}
		var now time.Time
		if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			return outbox.StoredResult{}, err
		}
		for _, w := range in.Windows {
			if !w.Until.After(now) {
				return outbox.StoredResult{}, fault.New(fault.Invalid, "Policy windows must retain future availability")
			}
		}
		raw, _ := json.Marshal(in)
		var policy map[string]any
		_ = json.Unmarshal(raw, &policy)
		delete(policy, "expected_version")
		// A policy edit requests new admission intent; probe/qualification must
		// independently restore ONLINE. Existing lock owners remain untouched.
		policy["accepting_new_jobs"] = false
		raw, _ = json.Marshal(policy)
		state := "DRAINING"
		if *in.Enabled && v.ActiveSlots == 0 {
			state = "PROBING"
		}
		if _, err = tx.Exec(ctx, `UPDATE p11.devices SET resource_policy=$3,state=$4,version=version+1 WHERE id=$1 AND seller_id=$2`, id, p.UserID, raw, state); err != nil {
			return outbox.StoredResult{}, err
		}
		if err = event(ctx, tx, p, id, v.Version+1, "DeviceResourcePolicyChanged"); err != nil {
			return outbox.StoredResult{}, err
		}
		ack = Ack{ID: id, Version: v.Version + 1, Status: state, Recorded: now}
		return stored(id, 200, ack)
	})
	if err != nil {
		return Ack{}, err
	}
	if r.Replayed {
		if err = json.Unmarshal(r.Body, &ack); err != nil {
			return Ack{}, err
		}
		ack.Replayed = true
	}
	return ack, nil
}
