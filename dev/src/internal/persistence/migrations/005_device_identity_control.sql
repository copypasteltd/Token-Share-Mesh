-- R007: forward-only provider identity/control; no device or capability approval.
ALTER TABLE p11.pairing_intents
  ADD COLUMN platform text CHECK (platform IN ('WINDOWS','LINUX','ANDROID')),
  ADD COLUMN device_label text CHECK (char_length(device_label) BETWEEN 1 AND 512),
  ADD COLUMN user_version p11.counter,
  ADD COLUMN pepper_version text,
  ADD COLUMN failed_attempts integer NOT NULL DEFAULT 0 CHECK (failed_attempts BETWEEN 0 AND 5),
  ADD COLUMN enrollment_key_digest p11.sha256_hex,
  ADD COLUMN enrollment_request_digest p11.sha256_hex,
  ADD CONSTRAINT pairing_recovery_fields CHECK
    ((enrollment_key_digest IS NULL)=(enrollment_request_digest IS NULL));

ALTER TABLE p11.devices
  ADD COLUMN device_label text NOT NULL DEFAULT '未命名设备' CHECK (char_length(device_label) BETWEEN 1 AND 512),
  ADD COLUMN identity_profile text NOT NULL DEFAULT 'LEGACY_UNVERIFIED'
    CHECK (identity_profile IN ('LEGACY_UNVERIFIED','ED25519_DEVICE_1')),
  ADD COLUMN device_public_key text,
  ADD COLUMN os_build text CHECK (char_length(os_build) BETWEEN 1 AND 512),
  ADD COLUMN installation_fingerprint p11.sha256_hex,
  ADD COLUMN heartbeat_sequence numeric(20,0) CHECK (heartbeat_sequence BETWEEN 0 AND 18446744073709551615),
  ADD COLUMN reported_resource_state text NOT NULL DEFAULT 'UNKNOWN'
    CHECK (reported_resource_state IN ('IDLE','BUSY','DRAINING','HOT','LOW_BATTERY','LOW_DISK','UNKNOWN')),
  ADD CONSTRAINT device_identity_fields CHECK
    (identity_profile='LEGACY_UNVERIFIED' OR
      (device_public_key IS NOT NULL AND length(device_public_key)=43 AND
       model_name IS NOT NULL AND os_build IS NOT NULL AND installation_fingerprint IS NOT NULL));

CREATE TABLE p11.device_request_nonces (
  device_id p11.entity_id NOT NULL REFERENCES p11.devices(id),
  nonce_digest p11.sha256_hex NOT NULL,
  request_digest p11.sha256_hex NOT NULL,
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(device_id,nonce_digest)
);
CREATE INDEX device_request_nonces_expiry ON p11.device_request_nonces(expires_at);

-- A signature identity cannot be replaced or resurrected by a stale writer.
-- Reinstallation/transfer uses a new pairing and a freshly generated key.
CREATE FUNCTION p11.protect_device_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF ROW(NEW.id,NEW.seller_id,NEW.platform,NEW.resource_kind,NEW.auth_key_digest,
         NEW.identity_profile,NEW.device_public_key,NEW.installation_fingerprint)
     IS DISTINCT FROM
     ROW(OLD.id,OLD.seller_id,OLD.platform,OLD.resource_kind,OLD.auth_key_digest,
         OLD.identity_profile,OLD.device_public_key,OLD.installation_fingerprint)
  THEN RAISE EXCEPTION 'device identity is immutable'; END IF;
  IF OLD.state IN ('REVOKED','RETIRED') AND NEW.state IS DISTINCT FROM OLD.state
  THEN RAISE EXCEPTION 'terminal device identity cannot be restored'; END IF;
  IF NEW.version<OLD.version OR (OLD.heartbeat_sequence IS NOT NULL AND
     (NEW.heartbeat_sequence IS NULL OR NEW.heartbeat_sequence<OLD.heartbeat_sequence))
  THEN RAISE EXCEPTION 'device counters cannot move backwards'; END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER device_identity_immutable BEFORE UPDATE ON p11.devices
  FOR EACH ROW EXECUTE FUNCTION p11.protect_device_identity();
