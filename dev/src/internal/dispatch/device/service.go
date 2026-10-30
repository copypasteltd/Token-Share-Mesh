package device

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"p11.local/compute/client/protocol/devicecontrol"
	"p11.local/compute/internal/identity"
	"p11.local/compute/internal/outbox"
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

func text(s string, max int) bool {
	if !utf8.ValidString(s) || s != strings.TrimSpace(s) || utf8.RuneCountInString(s) < 1 || utf8.RuneCountInString(s) > max {
		return false
	}
	for _, c := range s {
		if unicode.IsControl(c) {
			return false
		}
	}
	return true
}
func model(s string) bool {
	if !text(s, 128) {
		return false
	}
	compact := strings.ToLower(strings.ReplaceAll(s, " ", ""))
	for _, generic := range []string{"android设备", "androiddevice", "android设别", "gpu", "手机", "显卡", "windows", "linux", "设备", "computer", "phone"} {
		if compact == generic {
			return false
		}
	}
	return true
}
func (s Service) configured(pairing bool) error {
	if s.DB == nil || pairing && (!ids.Valid(s.PepperVersion) || len(s.Peppers[s.PepperVersion]) < 32) {
		return fault.New(fault.Unavailable, "Device identity service not configured")
	}
	return nil
}
func pairDigest(pepper []byte, token string) string {
	h := hmac.New(sha256.New, pepper)
	h.Write([]byte("P11_PAIR_TOKEN_1\x00" + token))
	return hex.EncodeToString(h.Sum(nil))
}
func authorizeOwner(ctx context.Context, tx pgx.Tx, p identity.Principal, scope string, newAdmission, update bool) error {
	if err := identity.RequireRoleTx(ctx, tx, p, scope, "SELLER", true); err != nil {
		return err
	}
	lock := "FOR SHARE"
	if update {
		lock = "FOR UPDATE"
	}
	var state string
	err := tx.QueryRow(ctx, "SELECT state FROM p11.seller_profiles WHERE id=$1 "+lock, p.UserID).Scan(&state)
	if err == pgx.ErrNoRows || err == nil && (newAdmission && state != "APPROVED" || !newAdmission && state != "APPROVED" && state != "EXITING") {
		return fault.New(fault.Forbidden, "Current seller qualification required")
	}
	return err
}
func (s Service) command(ctx context.Context, p identity.Principal, op, resource, key string, input any, authorize func(pgx.Tx) error, apply func(pgx.Tx) (outbox.StoredResult, error)) (outbox.StoredResult, error) {
	if err := s.configured(false); err != nil {
		return outbox.StoredResult{}, err
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return outbox.StoredResult{}, err
	}
	return (outbox.Idempotency{DB: s.DB}).DoAuthorized(ctx, outbox.Command{Principal: p.UserID, PrincipalVersion: p.Version, Operation: op, Resource: resource, Key: key, Request: raw}, authorize, apply)
}
func stored(id string, status int, value any) (outbox.StoredResult, error) {
	raw, err := json.Marshal(value)
	return outbox.StoredResult{Reference: id, HTTPStatus: status, Body: raw}, err
}
func event(ctx context.Context, tx pgx.Tx, p identity.Principal, id string, version int64, action string, extra ...map[string]any) error {
	facts := map[string]any{"device_id": id, "version": version}
	for _, fields := range extra {
		for k, v := range fields {
			facts[k] = v
		}
	}
	raw, _ := json.Marshal(facts)
	if _, err := tx.Exec(ctx, `INSERT INTO p11.audit_events(id,actor_id,action,target_ref,request_id,safe_facts) VALUES($1,$2,$3,$4,$5,$6)`, ids.New("aud"), p.UserID, action, id, ids.New("req"), raw); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO p11.outbox_events(id,aggregate_type,aggregate_id,aggregate_version,event_type,schema_version,safe_payload,state) VALUES($1,'DEVICE',$2,$3,$4,'3.0',$5,'PENDING')`, ids.New("evt"), id, version, action, raw)
	return err
}

func (s Service) CreatePairing(ctx context.Context, p identity.Principal, in PairInput, key string) (Pairing, error) {
	var result Pairing
	if err := s.configured(true); err != nil {
		return result, err
	}
	if !in.Confirmed || !text(in.Label, 512) || in.Platform != "WINDOWS" && in.Platform != "LINUX" && in.Platform != "ANDROID" {
		return result, fault.New(fault.Invalid, "Explicit owner confirmation, platform and device label required")
	}
	auth := func(tx pgx.Tx) error { return authorizeOwner(ctx, tx, p, "device:pair", true, true) }
	r, err := s.command(ctx, p, "createPairingIntent", p.UserID, key, in, auth, func(tx pgx.Tx) (outbox.StoredResult, error) {
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM p11.pairing_intents WHERE seller_id=$1 AND consumed_at IS NULL AND expires_at>clock_timestamp() AND failed_attempts<5`, p.UserID).Scan(&count); err != nil {
			return outbox.StoredResult{}, err
		}
		if count >= 3 {
			return outbox.StoredResult{}, fault.New(fault.Conflict, "Finish or let existing pairing intents expire first")
		}
		var random [32]byte
		if _, err := rand.Read(random[:]); err != nil {
			return outbox.StoredResult{}, err
		}
		result.ID = ids.New("pair")
		result.SellerID = p.UserID
		result.Token = "p11p." + result.ID + "." + base64.RawURLEncoding.EncodeToString(random[:])
		if err := tx.QueryRow(ctx, `INSERT INTO p11.pairing_intents(id,seller_id,token_digest,expires_at,platform,device_label,user_version,pepper_version) VALUES($1,$2,$3,clock_timestamp()+interval '5 minutes',$4,$5,$6,$7) RETURNING expires_at`, result.ID, p.UserID, pairDigest(s.Peppers[s.PepperVersion], result.Token), in.Platform, in.Label, p.Version, s.PepperVersion).Scan(&result.Expires); err != nil {
			return outbox.StoredResult{}, err
		}
		safe := result
		safe.Token = "" // A lost first response cannot redisclose a secret.
		if _, err := tx.Exec(ctx, `INSERT INTO p11.audit_events(id,actor_id,action,target_ref,request_id,safe_facts) VALUES($1,$2,'PairingIntentCreated',$3,$4,'{}')`, ids.New("aud"), p.UserID, result.ID, ids.New("req")); err != nil {
			return outbox.StoredResult{}, err
		}
		return stored(result.ID, 201, safe)
	})
	if err != nil {
		return Pairing{}, err
	}
	if r.Replayed {
		if err = json.Unmarshal(r.Body, &result); err != nil {
			return Pairing{}, err
		}
		result.Replayed = true
	}
	return result, nil
}

func validKey(key string) bool {
	if len(key) < 8 || len(key) > 128 {
		return false
	}
	for _, c := range key {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}
func validEnrollment(in Enrollment) bool {
	_, err := devicecontrol.PublicKey(in.PublicKey)
	_, hashErr := hex.DecodeString(in.Fingerprint)
	return err == nil && ids.Valid(in.PairingID) && in.KeyProfile == devicecontrol.KeyProfile && text(in.OSBuild, 512) && ids.Valid(in.ClientRelease) && len(in.Fingerprint) == 64 && strings.ToLower(in.Fingerprint) == in.Fingerprint && hashErr == nil && model(in.Model) && (in.Kind == "GPU_DEVICE" || in.Kind == "ANDROID_DEVICE" || in.Kind == "AUTHORIZED_RELAY")
}
func (s Service) Enroll(ctx context.Context, token string, in Enrollment, key string) (View, error) {
	var view View
	if err := s.configured(true); err != nil {
		return view, err
	}
	if !validEnrollment(in) || !validKey(key) {
		return view, fault.New(fault.Invalid, "Invalid explicit device enrollment")
	}
	bad := fault.New(fault.Unauthenticated, "Invalid, expired or consumed pairing credential")
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != "p11p" || parts[1] != in.PairingID {
		return view, bad
	}
	random, err := base64.RawURLEncoding.Strict().DecodeString(parts[2])
	if err != nil || len(random) != 32 || base64.RawURLEncoding.EncodeToString(random) != parts[2] {
		return view, bad
	}
	raw, _ := json.Marshal(in)
	requestDigest := outbox.Digest(raw)
	keyDigest := outbox.Digest([]byte(key))
	var rejected error
	err = s.DB.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		var owner string
		var version int64
		// Resolve the owner without a row lock, then use user -> seller -> intent
		// locking order. No authority is accepted from the enrollment JSON.
		if err := tx.QueryRow(ctx, `SELECT seller_id,user_version FROM p11.pairing_intents WHERE id=$1 AND platform IS NOT NULL AND user_version IS NOT NULL`, in.PairingID).Scan(&owner, &version); err == pgx.ErrNoRows {
			return bad
		} else if err != nil {
			return err
		}
		p := identity.Principal{UserID: owner, Kind: identity.UserSession, Version: version, Scopes: []string{"device:pair"}}
		if err := authorizeOwner(ctx, tx, p, "device:pair", true, false); err != nil {
			return bad
		}
		var digest, pepperVersion, platform, label string
		var failed int
		var expires time.Time
		var consumed *time.Time
		var deviceID, oldKey, oldRequest *string
		var now time.Time
		err := tx.QueryRow(ctx, `SELECT token_digest,pepper_version,platform,device_label,failed_attempts,expires_at,consumed_at,device_id,enrollment_key_digest,enrollment_request_digest,clock_timestamp() FROM p11.pairing_intents WHERE id=$1 FOR UPDATE`, in.PairingID).Scan(&digest, &pepperVersion, &platform, &label, &failed, &expires, &consumed, &deviceID, &oldKey, &oldRequest, &now)
		if err != nil {
			return err
		}
		pepper := s.Peppers[pepperVersion]
		if len(pepper) < 32 || failed >= 5 {
			return bad
		}
		if subtle.ConstantTimeCompare([]byte(digest), []byte(pairDigest(pepper, token))) != 1 {
			if consumed == nil && expires.After(now) {
				if _, err := tx.Exec(ctx, `UPDATE p11.pairing_intents SET failed_attempts=least(5,failed_attempts+1) WHERE id=$1`, in.PairingID); err != nil {
					return err
				}
			}
			rejected = bad
			return nil // Commit the failed-attempt counter.
		}
		if consumed != nil {
			if deviceID == nil || oldKey == nil || oldRequest == nil || *oldKey != keyDigest || *oldRequest != requestDigest || consumed.Add(7*24*time.Hour).Before(now) {
				return bad
			}
			view, err = viewTx(ctx, tx, *deviceID, owner, false)
			if err != nil {
				return err
			}
			if view.State == "REVOKED" || view.State == "RETIRED" {
				return bad
			}
			view.Replayed = true
			return nil
		}
		if !expires.After(now) {
			return bad
		}
		if platform == "ANDROID" && in.Kind != "ANDROID_DEVICE" || platform != "ANDROID" && in.Kind == "ANDROID_DEVICE" {
			return fault.New(fault.Invalid, "Resource kind does not match paired platform")
		}
		public, _ := devicecontrol.PublicKey(in.PublicKey)
		fingerprint := outbox.Digest(public)
		id := ids.New("dev")
		err = tx.QueryRow(ctx, `INSERT INTO p11.devices(id,seller_id,platform,resource_kind,state,auth_key_digest,client_release,resource_policy,model_name,device_label,identity_profile,device_public_key,os_build,installation_fingerprint)
VALUES($1,$2,$3,$4,'PAIRED',$5,$6,'{"sharing_enabled":false,"accepting_new_jobs":false,"active_slots":1,"windows":[],"minimum_free_disk_bytes":0,"stop_policy":"DRAIN"}',$7,$8,$9,$10,$11,$12) ON CONFLICT(auth_key_digest) DO NOTHING RETURNING id`, id, owner, platform, in.Kind, fingerprint, in.ClientRelease, in.Model, label, devicecontrol.KeyProfile, in.PublicKey, in.OSBuild, in.Fingerprint).Scan(&id)
		if err == pgx.ErrNoRows {
			return fault.New(fault.Conflict, "A fresh installation identity key is required")
		}
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE p11.pairing_intents SET consumed_at=clock_timestamp(),device_id=$2,enrollment_key_digest=$3,enrollment_request_digest=$4 WHERE id=$1`, in.PairingID, id, keyDigest, requestDigest); err != nil {
			return err
		}
		if err = event(ctx, tx, p, id, 1, "DeviceEnrolled"); err != nil {
			return err
		}
		view, err = viewTx(ctx, tx, id, owner, false)
		return err
	})
	if err != nil {
		return View{}, err
	}
	if rejected != nil {
		return View{}, rejected
	}
	return view, nil
}

func viewTx(ctx context.Context, tx pgx.Tx, id, owner string, update bool) (View, error) {
	v := View{Capabilities: []string{}}
	lock := "FOR SHARE"
	if update {
		lock = "FOR UPDATE"
	}
	err := tx.QueryRow(ctx, `SELECT id,seller_id,platform,resource_kind,state,version,last_seen_at,client_release,device_label,model_name,identity_profile,auth_key_digest,resource_policy,heartbeat_sequence::text,reported_resource_state FROM p11.devices WHERE id=$1 AND seller_id=$2 `+lock, id, owner).Scan(&v.ID, &v.SellerID, &v.Platform, &v.Kind, &v.State, &v.Version, &v.LastSeen, &v.ClientRelease, &v.Label, &v.Model, &v.IdentityProfile, &v.KeyFingerprint, &v.Policy, &v.HeartbeatSequence, &v.ReportedState)
	if err == pgx.ErrNoRows {
		return v, fault.New(fault.NotFound, "Device not found")
	}
	if err != nil {
		return v, err
	}
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM p11.resource_locks lk JOIN p11.resources r ON r.id=lk.resource_id WHERE r.device_id=$1 AND r.seller_id=$2 AND (lk.owner_attempt_id IS NOT NULL OR lk.owner_session_id IS NOT NULL)`, id, owner).Scan(&v.ActiveSlots); err != nil {
		return v, err
	}
	rows, err := tx.Query(ctx, `SELECT id FROM p11.capabilities WHERE device_id=$1 AND seller_id=$2 ORDER BY id LIMIT 100`, id, owner)
	if err != nil {
		return v, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return v, err
		}
		v.Capabilities = append(v.Capabilities, id)
	}
	return v, rows.Err()
}
func (s Service) authorizeRead(ctx context.Context, tx pgx.Tx, p identity.Principal, id string) error {
	if p.Kind == identity.Device {
		if p.DeviceID != id {
			return fault.New(fault.NotFound, "Device not found")
		}
		return authorizeDevice(ctx, tx, p, "device:read")
	}
	return authorizeOwner(ctx, tx, p, "device:read", false, false)
}
func (s Service) Get(ctx context.Context, p identity.Principal, id string) (View, error) {
	var v View
	if err := s.configured(false); err != nil {
		return v, err
	}
	if !ids.Valid(id) {
		return v, fault.New(fault.Invalid, "Invalid device identifier")
	}
	err := s.DB.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		if err := s.authorizeRead(ctx, tx, p, id); err != nil {
			return err
		}
		var err error
		v, err = viewTx(ctx, tx, id, p.UserID, false)
		return err
	})
	return v, err
}
func (s Service) List(ctx context.Context, p identity.Principal, cursor string, limit int) (Page, error) {
	page := Page{Items: []View{}}
	if err := s.configured(false); err != nil {
		return page, err
	}
	if limit < 1 || limit > 100 || cursor != "" && !ids.Valid(cursor) {
		return page, fault.New(fault.Invalid, "Invalid device pagination")
	}
	err := s.DB.Transaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		if err := authorizeOwner(ctx, tx, p, "seller:read", false, false); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT id FROM p11.devices WHERE seller_id=$1 AND id>$2 ORDER BY id LIMIT $3`, p.UserID, cursor, limit+1)
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(ids) > limit {
			next := ids[limit-1]
			page.NextCursor = &next
			ids = ids[:limit]
		}
		for _, id := range ids {
			v, err := viewTx(ctx, tx, id, p.UserID, false)
			if err != nil {
				return err
			}
			page.Items = append(page.Items, v)
		}
		return nil
	})
	return page, err
}
