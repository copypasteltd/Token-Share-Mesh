package node

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"p11.local/compute/client/protocol/devicecontrol"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/strictjson"
)

func validHostPhase(p string) bool {
	switch p {
	case "CLAIM_PENDING", "CLAIMED", "VERIFIED", "SUBMITTING", "SUBMITTED", "CLEANED":
		return true
	}
	return false
}
func (h *Host) loadCommand(ctx context.Context, tx *sql.Tx, id string) (hostCommand, error) {
	var c hostCommand
	var cap, phase string
	var version int64
	var sealed []byte
	e := tx.QueryRowContext(ctx, `SELECT capability_id,expected_device_version,phase,CASE WHEN length(sealed) BETWEEN 28 AND 65536 THEN sealed ELSE NULL END FROM commands WHERE command_id=?`, id).Scan(&cap, &version, &phase, &sealed)
	if errors.Is(e, sql.ErrNoRows) {
		return c, ErrHostPending
	}
	if e != nil {
		return c, ErrHost
	}
	plain, e := h.unseal("COMMAND\n"+id, sealed)
	if e != nil {
		return c, e
	}
	defer clear(plain)
	if strictjson.Decode(plain, &c, hostCommandLimit) != nil || c.ID != id || c.Claim.Device != h.device || c.Claim.Capability != cap || c.Claim.Version != version || c.Claim.Validate(id) != nil || c.Phase != phase || !validHostPhase(c.Phase) || len(c.Lease) > 16384 || len(c.Manifest) > 16384 || c.Phase != "CLAIM_PENDING" && len(c.Lease) == 0 || c.Phase != "CLAIM_PENDING" && c.Phase != "CLAIMED" && len(c.Manifest) == 0 {
		return hostCommand{}, ErrHost
	}
	if c.Phase == "CLAIM_PENDING" && (len(c.Lease) != 0 || len(c.Manifest) != 0) || c.Phase == "CLAIMED" && len(c.Manifest) != 0 {
		return hostCommand{}, ErrHost
	}
	return c, nil
}
func (h *Host) saveCommand(ctx context.Context, tx *sql.Tx, c hostCommand, insert bool) error {
	plain := canonicalHostJSON(c)
	defer clear(plain)
	sealed, e := h.seal("COMMAND\n"+c.ID, plain)
	if e != nil || len(sealed) > hostCommandLimit {
		return ErrHost
	}
	if insert {
		_, e = tx.ExecContext(ctx, `INSERT INTO commands(command_id,capability_id,expected_device_version,phase,sealed) VALUES(?,?,?,?,?)`, c.ID, c.Claim.Capability, c.Claim.Version, c.Phase, sealed)
	} else {
		var result sql.Result
		result, e = tx.ExecContext(ctx, `UPDATE commands SET phase=?,sealed=? WHERE command_id=? AND capability_id=? AND expected_device_version=?`, c.Phase, sealed, c.ID, c.Claim.Capability, c.Claim.Version)
		if e == nil {
			if n, err := result.RowsAffected(); err != nil || n != 1 {
				return ErrHost
			}
		}
	}
	if e != nil {
		return ErrHost
	}
	return nil
}

// Indexed phase/capability fields are query hints, never authenticated facts.
// Before admission or recovery selection, verify every bounded original row;
// a corrupted selector must not hide an unfinished command from that decision.
func (h *Host) visitCommands(ctx context.Context, tx *sql.Tx, visit func(hostCommand) error) error {
	rows, e := tx.QueryContext(ctx, `SELECT command_id FROM commands ORDER BY command_id LIMIT ?`, MaximumHostCommands+1)
	if e != nil {
		return ErrHost
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			rows.Close()
			return ErrHost
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil || len(ids) > MaximumHostCommands {
		return ErrHost
	}
	for _, id := range ids {
		c, e := h.loadCommand(ctx, tx, id)
		if e != nil {
			return e
		}
		if e = visit(c); e != nil {
			return e
		}
	}
	return nil
}

// Begin durably freezes the original claim before any network call. A changed
// key cannot bypass an unfinished command for that same capability. Reusing an
// ID with changed parameters is refused even after cleanup.
func (h *Host) Begin(ctx context.Context, id, capability string, version int64) error {
	in := devicecontrol.LeaseClaim{Device: h.device, Capability: capability, ClaimKey: id, Version: version}
	if in.Validate(id) != nil {
		return ErrHostPolicy
	}
	approved := false
	for _, r := range h.config.Rules {
		if r.Approval.Capability == capability {
			approved = true
		}
	}
	if !approved {
		return ErrHostPolicy
	}
	return h.transaction(ctx, func(tx *sql.Tx) error {
		old, e := h.loadCommand(ctx, tx, id)
		if e == nil {
			if old.Claim != in {
				return ErrHostPolicy
			}
			return nil
		}
		if !errors.Is(e, ErrHostPending) {
			return e
		}
		var total, pending int
		if e = h.visitCommands(ctx, tx, func(c hostCommand) error {
			total++
			if c.Claim.Capability == capability && c.Phase != "CLEANED" {
				pending++
			}
			return nil
		}); e != nil {
			return e
		}
		if total >= MaximumHostCommands || pending != 0 {
			return ErrHostPending
		}
		return h.saveCommand(ctx, tx, hostCommand{ID: id, Claim: in, Phase: "CLAIM_PENDING"}, true)
	})
}
func (h *Host) command(ctx context.Context, id string) (hostCommand, error) {
	var c hostCommand
	if !ids.Valid(id) {
		return c, ErrHostPolicy
	}
	e := h.transaction(ctx, func(tx *sql.Tx) error { var e error; c, e = h.loadCommand(ctx, tx, id); return e })
	return c, e
}
func (h *Host) update(ctx context.Context, id string, fn func(*hostCommand) error) error {
	return h.transaction(ctx, func(tx *sql.Tx) error {
		c, e := h.loadCommand(ctx, tx, id)
		if e != nil {
			return e
		}
		if e = fn(&c); e != nil {
			return e
		}
		return h.saveCommand(ctx, tx, c, false)
	})
}
func (h *Host) Snapshot(ctx context.Context) (HostStatus, error) {
	s := HostStatus{Profile: HostProfile, Commands: []HostCommandStatus{}, ApprovedOrders: len(h.config.Rules)}
	e := h.transaction(ctx, func(tx *sql.Tx) error {
		return h.visitCommands(ctx, tx, func(c hostCommand) error {
			s.Commands = append(s.Commands, HostCommandStatus{c.ID, c.Phase})
			return nil
		})
	})
	return s, e
}

// ReconcileCleanup projects only the exact existing SQLite receipt. It never
// frees resources, sends an ACK, bills or treats elapsed time as cleanup.
func (h *Host) ReconcileCleanup(ctx context.Context, id string) error {
	c, e := h.command(ctx, id)
	if e != nil {
		return e
	}
	var l struct {
		ID string `json:"lease_id"`
	}
	if len(c.Lease) == 0 || json.Unmarshal(c.Lease, &l) != nil {
		return ErrHostPending
	}
	o, e := h.ledger.CommandStatus(ctx, id)
	if e != nil {
		h.fail()
		return ErrHost
	}
	if !o.Found || o.LeaseID != l.ID || o.MetadataHash != hostDigest(c.Lease) || o.Phase != "CLEANED" {
		return ErrHostPending
	}
	return h.update(ctx, id, func(current *hostCommand) error {
		if string(current.Lease) != string(c.Lease) {
			return ErrHost
		}
		current.Phase = "CLEANED"
		return nil
	})
}
