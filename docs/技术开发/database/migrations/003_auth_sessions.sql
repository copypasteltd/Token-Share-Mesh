-- R004: native control-plane identity records. Never store OAuth codes/tokens.
-- 001 and 002 remain immutable; no real channel approval follows from this DDL.
CREATE TABLE p11.auth_sessions (
  id p11.entity_id PRIMARY KEY,
  user_id p11.entity_id NOT NULL REFERENCES p11.users(id),
  user_version p11.counter NOT NULL CHECK(user_version BETWEEN 1 AND 2147483647),
  client_type text NOT NULL CHECK(client_type IN ('WEB','MINIAPP')),
  token_digest p11.sha256_hex NOT NULL UNIQUE,
  pepper_version p11.entity_id NOT NULL,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  expires_at timestamptz NOT NULL,
  revoked_at timestamptz,
  CHECK(expires_at>created_at),
  UNIQUE(id,user_id)
);
CREATE INDEX auth_sessions_user ON p11.auth_sessions(user_id,expires_at);

CREATE TABLE p11.auth_attempts (
  id p11.entity_id PRIMARY KEY,
  provider text NOT NULL CHECK(char_length(provider) BETWEEN 1 AND 128),
  client_type text NOT NULL CHECK(client_type IN ('WEB','MINIAPP')),
  verifier_digest p11.sha256_hex NOT NULL,
  browser_binding_digest p11.sha256_hex,
  code_digest p11.sha256_hex UNIQUE,
  state text NOT NULL CHECK(state IN ('PENDING','EXCHANGING','SUCCEEDED','FAILED','UNKNOWN')),
  session_id p11.entity_id REFERENCES p11.auth_sessions(id),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  expires_at timestamptz NOT NULL,
  CHECK(expires_at>created_at),
  CHECK((client_type='WEB')=(browser_binding_digest IS NOT NULL)),
  CHECK((state='PENDING')=(code_digest IS NULL)),
  CHECK((state='SUCCEEDED')=(session_id IS NOT NULL))
);
CREATE INDEX auth_attempts_expiry ON p11.auth_attempts(expires_at);
