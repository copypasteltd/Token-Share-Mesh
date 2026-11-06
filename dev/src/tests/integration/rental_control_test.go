package integration_test

import (
	"bytes"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"p11.local/compute/client/protocol/rentalaccess"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/internal/rental"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
)

// All funds/qualification/readiness facts in this file are synthetic native-DB
// fixtures. No receipt here qualifies real hardware, a seller or channel funds.
func rentalControlGrant(t *testing.T, f rentalFixture, active rental.View) (rentalaccess.ControlRequest, rentalaccess.Metadata) {
	t.Helper()
	first, err := f.s.CreateAccess(ctx, f.p, active.ID, rental.AccessWrite{Expected: active.Version, Scopes: []string{"INVOKE"}}, ids.New("access"))
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	var raw []byte
	if err = db.Pool.QueryRow(ctx, `SELECT safe_claims FROM p11.rental_access_grants WHERE id=$1`, first.Metadata().Grant).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var m rentalaccess.Metadata
	if err = json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return rentalaccess.ControlRequest{ExpectedVersion: first.Metadata().SessionVersion, Grant: m.Claims.ID, Digest: m.Digest, Generation: m.Claims.Generation, Terms: m.Claims.Terms, TermsHash: m.Claims.TermsHash, Sequence: "1"}, m
}

func TestRentalControlAtomicReplayDoesNotRenewTermsOrMoney(t *testing.T) {
	f := newRentalFixture(t, "SETTLED", "SANDBOX")
	active := f.activate(t)
	in, m := rentalControlGrant(t, f, active)
	key := ids.New("control")
	first, err := f.s.ConfirmControl(ctx, f.device, active.ID, in, key)
	if err != nil {
		t.Fatal(err)
	}
	var reply rentalaccess.Control
	if json.Unmarshal(first.Receipt, &reply) != nil || reply.Decision != "ALLOW" || reply.GrantDigest != m.Digest {
		t.Fatal("missing original ALLOW")
	}
	issued, err := time.Parse(time.RFC3339Nano, reply.Issued)
	if err != nil {
		t.Fatal(err)
	}
	expires, err := time.Parse(time.RFC3339Nano, reply.Expires)
	if err != nil || !expires.After(issued) || expires.Sub(issued) > 45*time.Second {
		t.Fatal("control exceeded its signed bound", err)
	}
	for _, retry := range []string{key, ids.New("duplicate")} {
		again, err := f.s.ConfirmControl(ctx, f.device, active.ID, in, retry)
		if err != nil || !again.Replayed || !bytes.Equal(first.Receipt, again.Receipt) {
			t.Fatal("duplicate changed original signed reply", err)
		}
	}
	current, err := f.s.Get(ctx, f.p, active.ID)
	if err != nil || current.Version != in.ExpectedVersion || current.HardDeadline == nil || !current.HardDeadline.Equal(*active.HardDeadline) || current.Budget != active.Budget {
		t.Fatal("control changed paid session", err)
	}
	var count int
	if err = db.Pool.QueryRow(ctx, `SELECT count(*) FROM p11.rental_control_history WHERE grant_id=$1`, in.Grant).Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate created another control history", err)
	}
	var stored, request []byte
	if err = db.Pool.QueryRow(ctx, `SELECT reply_raw,request_raw FROM p11.rental_control_history WHERE grant_id=$1`, in.Grant).Scan(&stored, &request); err != nil || !bytes.Equal(stored, first.Receipt) || bytes.Contains(request, []byte("credential")) {
		t.Fatal("incorrect safe durable control history", err)
	}
	changed := in
	changed.Sequence = "3"
	if _, err = f.s.ConfirmControl(ctx, f.device, active.ID, changed, ids.New("control")); err != nil {
		t.Fatal("restart sequence gap was refused", err)
	}
	changed.Sequence = "2"
	_, err = f.s.ConfirmControl(ctx, f.device, active.ID, changed, ids.New("control"))
	wantCode(t, err, fault.Conflict)
}

func TestRentalControlReplacementAndStopNeverReleasePhysicalOwner(t *testing.T) {
	f := newRentalFixture(t, "SETTLED", "SANDBOX")
	active := f.activate(t)
	in, _ := rentalControlGrant(t, f, active)
	if _, err := f.s.ConfirmControl(ctx, f.device, active.ID, in, ids.New("control")); err != nil {
		t.Fatal(err)
	}
	replacement, err := f.s.CreateAccess(ctx, f.p, active.ID, rental.AccessWrite{Expected: in.ExpectedVersion, Scopes: []string{"INVOKE"}}, ids.New("replace"))
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Close()
	in.Sequence = "2"
	stop, err := f.s.ConfirmControl(ctx, f.device, active.ID, in, ids.New("control"))
	if err != nil {
		t.Fatal(err)
	}
	var reply rentalaccess.Control
	if json.Unmarshal(stop.Receipt, &reply) != nil || reply.Decision != "STOP" {
		t.Fatal("revoked original grant was allowed")
	}
	// Explicit replacement is another grant; old STOP remains append-only.
	in.Sequence = "3"
	stop, err = f.s.ConfirmControl(ctx, f.device, active.ID, in, ids.New("control"))
	if err != nil {
		t.Fatal(err)
	}
	if json.Unmarshal(stop.Receipt, &reply) != nil || reply.Decision != "STOP" {
		t.Fatal("stopped original grant revived")
	}
	var owner, phase string
	var held bool
	if err = db.Pool.QueryRow(ctx, `SELECT owner_session_id,phase FROM p11.resource_locks WHERE resource_id=$1`, f.f.Resource).Scan(&owner, &phase); err != nil || owner != active.ID || phase != "ACTIVE" {
		t.Fatal("control released physical owner", err)
	}
	if err = db.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM p11.budget_reservations WHERE purpose_ref=$1 AND state='RESERVED' AND amount_fen=$2)`, active.ID, active.Budget).Scan(&held); err != nil || !held {
		t.Fatal("control released budget", err)
	}
	if _, err = db.Pool.Exec(ctx, `DELETE FROM p11.rental_control_history WHERE grant_id=$1`, in.Grant); err == nil {
		t.Fatal("append-only control history deleted")
	}
}

func TestRentalControlQualificationLossIsStopAndNotADeviceFallback(t *testing.T) {
	f := newRentalFixture(t, "SETTLED", "SANDBOX")
	active := f.activate(t)
	in, _ := rentalControlGrant(t, f, active)
	exec(t, `UPDATE p11.capabilities SET state='REVOKED',version=version+1 WHERE id=$1`, f.f.Capability)
	v, err := f.s.ConfirmControl(ctx, f.device, active.ID, in, ids.New("control"))
	if err != nil {
		t.Fatal(err)
	}
	var reply rentalaccess.Control
	if json.Unmarshal(v.Receipt, &reply) != nil || reply.Decision != "STOP" || reply.Device != f.f.Device || reply.Generation != in.Generation {
		t.Fatal("qualification loss continued or rebound original work")
	}
	wrong := f.device
	wrong.DeviceID = ids.New("otherDevice")
	_, err = f.s.ConfirmControl(ctx, wrong, active.ID, in, ids.New("control"))
	wantCode(t, err, fault.NotFound)
	for _, sequence := range []string{"0", "01", "-1", "9223372036854775808"} {
		in.Sequence = sequence
		_, err = f.s.ConfirmControl(ctx, f.device, active.ID, in, ids.New("control"))
		wantCode(t, err, fault.Invalid)
	}
}

func TestRentalControlHistoryCannotCommitWithoutOriginalEvent(t *testing.T) {
	f := newRentalFixture(t, "SETTLED", "SANDBOX")
	active := f.activate(t)
	in, _ := rentalControlGrant(t, f, active)
	first, err := f.s.ConfirmControl(ctx, f.device, active.ID, in, ids.New("control"))
	if err != nil {
		t.Fatal(err)
	}
	var reply rentalaccess.Control
	if json.Unmarshal(first.Receipt, &reply) != nil {
		t.Fatal("invalid control fixture")
	}
	// Altering sequence invalidates the signature too. SQL cannot verify that
	// signature; it must still reject missing atomic event/provenance on commit.
	in.Sequence = "2"
	reply.Sequence = "2"
	request, _ := json.Marshal(in)
	raw, _ := json.Marshal(reply)
	issued, _ := time.Parse(time.RFC3339Nano, reply.Issued)
	expires, _ := time.Parse(time.RFC3339Nano, reply.Expires)
	err = db.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO p11.rental_control_history(grant_id,sequence,session_id,device_id,grant_digest,grant_session_version,resource_lock_generation,terms_revision,terms_hash,request_raw,request_digest,reply_raw,reply_digest,decision,issued_at,expires_at,event_id)
VALUES($1,2,$2,$3,$4,$5,$6::bigint,$7::bigint,$8,$9,$10,$11,$12,'ALLOW',$13,$14,$15)`, in.Grant, active.ID, f.f.Device, in.Digest, in.ExpectedVersion, in.Generation, in.Terms, in.TermsHash, request, outbox.Digest(request), raw, outbox.Digest(raw), issued, expires, ids.New("missingEvent"))
		return e
	})
	if err == nil {
		t.Fatal("control committed without matching atomic event")
	}
	var high string
	if err = db.Pool.QueryRow(ctx, `SELECT max(sequence)::text FROM p11.rental_control_history WHERE grant_id=$1`, in.Grant).Scan(&high); err != nil || high != strconv.Itoa(1) {
		t.Fatal("failed commit advanced control high water", err)
	}
}

func TestRentalExpectationReadIsOriginalDeviceMetadataOnly(t *testing.T) {
	f := newRentalFixture(t, "SETTLED", "SANDBOX")
	active := f.activate(t)
	in, m := rentalControlGrant(t, f, active)
	got, err := f.s.GetGrantExpectation(ctx, f.device, active.ID, in.Grant)
	if err != nil || got.Metadata.Digest != m.Digest || got.SessionVersion != in.ExpectedVersion || got.Metadata.Claims.Device != f.f.Device {
		t.Fatal("original stored expectation unavailable", err)
	}
	raw, _ := json.Marshal(got)
	if bytes.Contains(raw, []byte("credential_once")) || bytes.Contains(raw, []byte("private_key")) {
		t.Fatal("expectation response exposed credential")
	}
	_, err = f.s.GetGrantExpectation(ctx, f.p, active.ID, in.Grant)
	wantCode(t, err, fault.Invalid)
	wrong := f.device
	wrong.DeviceID = ids.New("otherDevice")
	_, err = f.s.GetGrantExpectation(ctx, wrong, active.ID, in.Grant)
	wantCode(t, err, fault.NotFound)
	current, err := f.s.Get(ctx, f.p, active.ID)
	if err != nil || current.Version != in.ExpectedVersion {
		t.Fatal("metadata read changed session", err)
	}
	if _, err = f.s.Stop(ctx, f.p, active.ID, rental.StopWrite{Expected: current.Version, Reason: "BUYER_STOP"}, ids.New("stop")); err != nil {
		t.Fatal(err)
	}
	_, err = f.s.GetGrantExpectation(ctx, f.device, active.ID, in.Grant)
	wantCode(t, err, fault.Conflict)
}
