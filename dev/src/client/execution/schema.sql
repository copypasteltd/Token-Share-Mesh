-- Native metadata only. Never store prompts, media, plaintext payloads or keys.
CREATE TABLE metadata (
  name TEXT PRIMARY KEY,
  value TEXT NOT NULL
) STRICT;
CREATE TABLE resources (
  resource_id TEXT PRIMARY KEY,
  physical_digest TEXT NOT NULL UNIQUE CHECK(length(physical_digest)=64),
  remote_high TEXT NOT NULL DEFAULT '0',
  local_high TEXT NOT NULL DEFAULT '0',
  owner_id TEXT NOT NULL DEFAULT '',
  mode TEXT NOT NULL DEFAULT '' CHECK(mode IN ('','JOB','LOCAL','MAINTENANCE')),
  phase TEXT NOT NULL DEFAULT 'FREE' CHECK(phase IN ('FREE','PREPARING','EXECUTING','STOPPING','CLEANING','QUARANTINED')),
  stopped INTEGER NOT NULL DEFAULT 0 CHECK(stopped IN (0,1)),
  CHECK((phase='FREE' AND owner_id='' AND mode='' AND stopped=0) OR (phase<>'FREE' AND owner_id<>'' AND mode<>''))
) STRICT;
CREATE TABLE inbox (
  lease_id TEXT PRIMARY KEY,
  resource_id TEXT NOT NULL REFERENCES resources(resource_id),
  local_generation TEXT NOT NULL,
  metadata_json TEXT NOT NULL CHECK(length(metadata_json)<=16384),
  metadata_hash TEXT NOT NULL CHECK(length(metadata_hash)=64),
  phase TEXT NOT NULL CHECK(phase IN ('RECEIVED','SUBMITTING','EXECUTING','STOPPING','CLEANING','QUARANTINED','CLEANED')),
  backend_id TEXT NOT NULL DEFAULT '',
  cleanup_hash TEXT NOT NULL DEFAULT ''
) STRICT;
CREATE TABLE receipts (
  command_id TEXT PRIMARY KEY,
  lease_id TEXT NOT NULL REFERENCES inbox(lease_id),
  metadata_hash TEXT NOT NULL CHECK(length(metadata_hash)=64),
  kind TEXT NOT NULL CHECK(kind IN ('RECEIVE','RENEW'))
) STRICT;
CREATE TABLE job_fences (
  job_id TEXT PRIMARY KEY,
  token TEXT NOT NULL,
  lease_id TEXT NOT NULL REFERENCES inbox(lease_id)
) STRICT;
