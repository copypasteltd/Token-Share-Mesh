-- Forward-only native migration. schema.sql remains byte-for-byte unchanged.
-- Run with foreign_keys OFF on one exclusive connection before BEGIN; perform
-- foreign_key_check before COMMIT and restore foreign_keys ON before reuse.
-- https://www.sqlite.org/lang_altertable.html#making_other_kinds_of_table_schema_changes
CREATE TABLE resources_next (
  resource_id TEXT PRIMARY KEY,
  physical_digest TEXT NOT NULL UNIQUE CHECK(length(physical_digest)=64),
  remote_high TEXT NOT NULL DEFAULT '0',
  local_high TEXT NOT NULL DEFAULT '0',
  owner_id TEXT NOT NULL DEFAULT '',
  mode TEXT NOT NULL DEFAULT '' CHECK(mode IN ('','JOB','LOCAL','MAINTENANCE','RENTAL')),
  phase TEXT NOT NULL DEFAULT 'FREE' CHECK(phase IN ('FREE','PREPARING','EXECUTING','STOPPING','CLEANING','QUARANTINED')),
  stopped INTEGER NOT NULL DEFAULT 0 CHECK(stopped IN (0,1)),
  CHECK((phase='FREE' AND owner_id='' AND mode='' AND stopped=0) OR (phase<>'FREE' AND owner_id<>'' AND mode<>''))
) STRICT;
INSERT INTO resources_next SELECT resource_id,physical_digest,remote_high,local_high,owner_id,mode,phase,stopped FROM resources;
DROP TABLE resources;
ALTER TABLE resources_next RENAME TO resources;

CREATE TABLE rental_owners (
  session_id TEXT PRIMARY KEY,
  resource_id TEXT NOT NULL REFERENCES resources(resource_id),
  local_generation TEXT NOT NULL,
  metadata_raw TEXT NOT NULL CHECK(length(metadata_raw) BETWEEN 1 AND 16384),
  manifest_hash TEXT NOT NULL CHECK(length(manifest_hash)=64),
  approval_hash TEXT NOT NULL CHECK(length(approval_hash)=64),
  phase TEXT NOT NULL CHECK(phase IN ('PREPARING','ACTIVE','STOPPING','CLEANING','QUARANTINED','CLEANED')),
  prepare_until_ns TEXT NOT NULL,
  hard_deadline_ns TEXT NOT NULL DEFAULT '0',
  grant_id TEXT NOT NULL DEFAULT '',
  grant_digest TEXT NOT NULL DEFAULT '',
  grant_context TEXT NOT NULL DEFAULT '',
  cleanup_hash TEXT NOT NULL DEFAULT ''
) STRICT;
CREATE TABLE rental_commands (
  command_id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES rental_owners(session_id),
  metadata_hash TEXT NOT NULL CHECK(length(metadata_hash)=64),
  kind TEXT NOT NULL CHECK(kind IN ('PREPARE','GRANT'))
) STRICT;
CREATE TABLE rental_grants (
  grant_id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES rental_owners(session_id),
  digest TEXT NOT NULL UNIQUE CHECK(length(digest)=64),
  issuer_key_id TEXT NOT NULL,
  expected_raw TEXT NOT NULL CHECK(length(expected_raw) BETWEEN 1 AND 8192),
  state TEXT NOT NULL CHECK(state IN ('CURRENT','REVOKED','STOPPED')),
  control_sequence TEXT NOT NULL DEFAULT '0',
  control_identity TEXT NOT NULL DEFAULT '',
  UNIQUE(grant_id,session_id)
) STRICT;
CREATE UNIQUE INDEX rental_one_current_grant ON rental_grants(session_id) WHERE state='CURRENT';
CREATE TABLE rental_invocations (
  invocation_id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES rental_owners(session_id),
  original_grant TEXT NOT NULL,
  grant_digest TEXT NOT NULL CHECK(length(grant_digest)=64),
  input_ciphertext_digest TEXT NOT NULL CHECK(length(input_ciphertext_digest)=64),
  recipe_digest TEXT NOT NULL CHECK(length(recipe_digest)=64),
  phase TEXT NOT NULL CHECK(phase IN ('SUBMITTING','EXECUTING','STOPPING','CLEANING','QUARANTINED','CLEANED')),
  backend_id TEXT NOT NULL DEFAULT '',
  cleanup_hash TEXT NOT NULL DEFAULT '',
  FOREIGN KEY(original_grant,session_id) REFERENCES rental_grants(grant_id,session_id)
) STRICT;
CREATE UNIQUE INDEX rental_one_unfinished_invocation ON rental_invocations(session_id) WHERE phase<>'CLEANED';
CREATE INDEX rental_invocation_original_session ON rental_invocations(session_id,invocation_id);
CREATE TRIGGER rental_original_identity BEFORE UPDATE ON rental_owners
WHEN NEW.session_id<>OLD.session_id OR NEW.resource_id<>OLD.resource_id OR NEW.local_generation<>OLD.local_generation
 OR NEW.metadata_raw<>OLD.metadata_raw OR NEW.manifest_hash<>OLD.manifest_hash OR NEW.approval_hash<>OLD.approval_hash
 OR NEW.prepare_until_ns<>OLD.prepare_until_ns
 OR (OLD.hard_deadline_ns<>'0' AND NEW.hard_deadline_ns<>OLD.hard_deadline_ns)
BEGIN SELECT RAISE(ABORT,'original rental identity is immutable'); END;
CREATE TRIGGER rental_owner_no_delete BEFORE DELETE ON rental_owners
BEGIN SELECT RAISE(ABORT,'retain original rental owner history'); END;
CREATE TRIGGER rental_owner_no_revive BEFORE UPDATE ON rental_owners
WHEN (OLD.phase IN ('STOPPING','CLEANING','QUARANTINED','CLEANED') AND NEW.phase IN ('PREPARING','ACTIVE'))
 OR (OLD.phase='CLEANED' AND NEW.phase<>'CLEANED')
BEGIN SELECT RAISE(ABORT,'stopped rental owner cannot regain execution'); END;
CREATE TRIGGER rental_grant_immutable BEFORE UPDATE ON rental_grants
WHEN NEW.grant_id<>OLD.grant_id OR NEW.session_id<>OLD.session_id OR NEW.digest<>OLD.digest
 OR NEW.expected_raw<>OLD.expected_raw OR NEW.issuer_key_id<>OLD.issuer_key_id
 OR (OLD.state IN ('REVOKED','STOPPED') AND NEW.state<>OLD.state)
BEGIN SELECT RAISE(ABORT,'original stopped rental grant cannot revive'); END;
CREATE TRIGGER rental_grant_no_delete BEFORE DELETE ON rental_grants
BEGIN SELECT RAISE(ABORT,'retain original rental grants'); END;
CREATE TRIGGER rental_command_no_update BEFORE UPDATE ON rental_commands
BEGIN SELECT RAISE(ABORT,'original rental command is immutable'); END;
CREATE TRIGGER rental_command_no_delete BEFORE DELETE ON rental_commands
BEGIN SELECT RAISE(ABORT,'retain original rental commands'); END;
CREATE TRIGGER rental_invocation_no_rebind BEFORE UPDATE ON rental_invocations
WHEN NEW.invocation_id<>OLD.invocation_id OR NEW.session_id<>OLD.session_id OR NEW.original_grant<>OLD.original_grant
 OR NEW.grant_digest<>OLD.grant_digest OR NEW.input_ciphertext_digest<>OLD.input_ciphertext_digest OR NEW.recipe_digest<>OLD.recipe_digest
 OR (OLD.backend_id<>'' AND NEW.backend_id<>OLD.backend_id)
 OR (OLD.phase IN ('STOPPING','CLEANING','QUARANTINED','CLEANED') AND NEW.phase IN ('SUBMITTING','EXECUTING'))
 OR (OLD.phase='CLEANED' AND NEW.phase<>'CLEANED')
BEGIN SELECT RAISE(ABORT,'original rental invocation is immutable'); END;
CREATE TRIGGER rental_invocation_no_delete BEFORE DELETE ON rental_invocations
BEGIN SELECT RAISE(ABORT,'retain original rental invocation history'); END;
