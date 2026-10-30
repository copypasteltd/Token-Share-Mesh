-- Native host metadata only. Content, prompts, media and engine credentials
-- never enter this database. Config and command metadata use separate AEAD AAD.
CREATE TABLE metadata (
  name TEXT PRIMARY KEY,
  value TEXT NOT NULL
) STRICT;
CREATE TABLE configuration (
  id INTEGER PRIMARY KEY CHECK(id=1),
  sealed BLOB NOT NULL CHECK(length(sealed) BETWEEN 28 AND 131072)
) STRICT;
CREATE TABLE commands (
  command_id TEXT PRIMARY KEY CHECK(length(command_id) BETWEEN 8 AND 128),
  capability_id TEXT NOT NULL,
  expected_device_version INTEGER NOT NULL CHECK(expected_device_version BETWEEN 1 AND 2147483646),
  phase TEXT NOT NULL CHECK(phase IN ('CLAIM_PENDING','CLAIMED','VERIFIED','SUBMITTING','SUBMITTED','CLEANED')),
  sealed BLOB NOT NULL CHECK(length(sealed) BETWEEN 28 AND 65536)
) STRICT;
CREATE INDEX commands_capability_phase ON commands(capability_id,phase);
