-- Native forward migration 003. Original schema.sql and migration 002 stay frozen.
-- Metadata only: never persist prompts, plaintext hashes, receipts or content keys.
CREATE TABLE media_namespace (
  singleton INTEGER PRIMARY KEY CHECK(singleton=1),
  namespace_id TEXT NOT NULL UNIQUE CHECK(length(namespace_id)=64),
  anchor_hash TEXT NOT NULL CHECK(length(anchor_hash)=64),
  phase TEXT NOT NULL CHECK(phase IN ('CREATING','READY','QUARANTINED'))
) STRICT;
CREATE TABLE rental_media_workspaces (
  invocation_id TEXT PRIMARY KEY REFERENCES rental_invocations(invocation_id),
  session_id TEXT NOT NULL REFERENCES rental_owners(session_id),
  namespace_id TEXT NOT NULL REFERENCES media_namespace(namespace_id),
  resource_id TEXT NOT NULL REFERENCES resources(resource_id),
  local_generation TEXT NOT NULL,
  grant_id TEXT NOT NULL,
  grant_digest TEXT NOT NULL CHECK(length(grant_digest)=64),
  manifest_hash TEXT NOT NULL CHECK(length(manifest_hash)=64),
  environment_hash TEXT NOT NULL CHECK(length(environment_hash)=64),
  recipe_hash TEXT NOT NULL CHECK(length(recipe_hash)=64),
  input_ciphertext_digest TEXT NOT NULL CHECK(length(input_ciphertext_digest)=64),
  basename TEXT NOT NULL UNIQUE CHECK(length(basename)=35 AND basename GLOB 'ws_*'),
  anchor_hash TEXT NOT NULL CHECK(length(anchor_hash)=64),
  reserved_bytes INTEGER NOT NULL CHECK(reserved_bytes>0 AND reserved_bytes<=136314880),
  phase TEXT NOT NULL CHECK(phase IN ('CREATING','READY','ACTIVE','RETIRED','REMOVING','CLEANED','QUARANTINED')),
  cleanup_hash TEXT NOT NULL DEFAULT '',
  FOREIGN KEY(grant_id,session_id) REFERENCES rental_grants(grant_id,session_id)
) STRICT;
CREATE INDEX rental_media_original_session ON rental_media_workspaces(session_id,invocation_id);
CREATE TRIGGER media_namespace_identity BEFORE UPDATE ON media_namespace
WHEN NEW.singleton<>OLD.singleton OR NEW.namespace_id<>OLD.namespace_id OR NEW.anchor_hash<>OLD.anchor_hash
 OR (OLD.phase='READY' AND NEW.phase='CREATING') OR (OLD.phase='QUARANTINED' AND NEW.phase<>'QUARANTINED')
BEGIN SELECT RAISE(ABORT,'original media namespace is immutable'); END;
CREATE TRIGGER media_namespace_no_delete BEFORE DELETE ON media_namespace
BEGIN SELECT RAISE(ABORT,'retain original media namespace'); END;
CREATE TRIGGER rental_media_identity BEFORE UPDATE ON rental_media_workspaces
WHEN NEW.invocation_id<>OLD.invocation_id OR NEW.session_id<>OLD.session_id OR NEW.namespace_id<>OLD.namespace_id
 OR NEW.resource_id<>OLD.resource_id OR NEW.local_generation<>OLD.local_generation OR NEW.grant_id<>OLD.grant_id
 OR NEW.grant_digest<>OLD.grant_digest OR NEW.manifest_hash<>OLD.manifest_hash OR NEW.environment_hash<>OLD.environment_hash
 OR NEW.recipe_hash<>OLD.recipe_hash OR NEW.input_ciphertext_digest<>OLD.input_ciphertext_digest
 OR NEW.basename<>OLD.basename OR NEW.anchor_hash<>OLD.anchor_hash OR NEW.reserved_bytes<>OLD.reserved_bytes
 OR (OLD.cleanup_hash<>'' AND NEW.cleanup_hash<>OLD.cleanup_hash)
 OR (NEW.phase<>OLD.phase AND NOT (
       (OLD.phase='CREATING' AND NEW.phase IN ('READY','QUARANTINED','RETIRED'))
    OR (OLD.phase='READY' AND NEW.phase IN ('ACTIVE','QUARANTINED','RETIRED'))
    OR (OLD.phase='ACTIVE' AND NEW.phase IN ('QUARANTINED','RETIRED'))
    OR (OLD.phase='QUARANTINED' AND NEW.phase='RETIRED')
    OR (OLD.phase='RETIRED' AND NEW.phase IN ('REMOVING','QUARANTINED'))
    OR (OLD.phase='REMOVING' AND NEW.phase IN ('CLEANED','QUARANTINED'))))
BEGIN SELECT RAISE(ABORT,'original media workspace is immutable'); END;
CREATE TRIGGER rental_media_no_delete BEFORE DELETE ON rental_media_workspaces
BEGIN SELECT RAISE(ABORT,'retain original media workspace history'); END;
