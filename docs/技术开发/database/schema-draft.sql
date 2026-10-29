-- P11 technical v1.0 / requirements v3.0 / 2026-10-29.
-- PostgreSQL 17 DESIGN ONLY. No database has been created or migrated.
-- Schema parsing does not validate PL/pgSQL execution, permissions or concurrency.
BEGIN;
CREATE SCHEMA p11;
CREATE DOMAIN p11.entity_id AS text
  CHECK (VALUE ~ '^[A-Za-z][A-Za-z0-9_-]{2,127}$');
CREATE DOMAIN p11.money_fen AS bigint
  CHECK (VALUE BETWEEN 0 AND 9007199254740991);
CREATE DOMAIN p11.counter AS bigint CHECK (VALUE >= 0);
CREATE DOMAIN p11.sha256_hex AS text CHECK (VALUE ~ '^[a-f0-9]{64}$');

CREATE TABLE p11.users (
  id p11.entity_id PRIMARY KEY,
  identity_provider text NOT NULL,
  external_subject_digest p11.sha256_hex NOT NULL,
  state text NOT NULL CHECK (state IN ('ACTIVE','SUSPENDED','CLOSING','CLOSED')),
  version p11.counter NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (identity_provider, external_subject_digest)
);
CREATE TABLE p11.role_grants (
  id p11.entity_id PRIMARY KEY,
  user_id p11.entity_id NOT NULL REFERENCES p11.users(id),
  role_code text NOT NULL,
  scope_ref text NOT NULL,
  granted_by p11.entity_id NOT NULL REFERENCES p11.users(id),
  expires_at timestamptz,
  revoked_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX role_grants_active ON p11.role_grants(user_id,role_code,scope_ref)
  WHERE revoked_at IS NULL;
CREATE TABLE p11.api_keys (
  id p11.entity_id PRIMARY KEY,
  user_id p11.entity_id NOT NULL REFERENCES p11.users(id),
  key_digest p11.sha256_hex NOT NULL UNIQUE,
  fingerprint p11.sha256_hex NOT NULL UNIQUE,
  label text NOT NULL CHECK (char_length(label) BETWEEN 1 AND 512),
  pepper_version text NOT NULL,
  scopes text[] NOT NULL,
  route_ids text[] NOT NULL DEFAULT '{}' CHECK (cardinality(route_ids)<=50),
  expires_at timestamptz NOT NULL,
  revoked_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE p11.consents (
  id p11.entity_id PRIMARY KEY,
  user_id p11.entity_id NOT NULL REFERENCES p11.users(id),
  policy_id p11.entity_id NOT NULL,
  policy_version text NOT NULL,
  decision text NOT NULL CHECK (decision IN ('ACCEPT','WITHDRAW')),
  context_ref p11.entity_id,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE p11.seller_profiles (
  id p11.entity_id PRIMARY KEY REFERENCES p11.users(id),
  state text NOT NULL,
  legal_scope_ref p11.entity_id,
  payee_binding_ref p11.entity_id,
  version p11.counter NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE p11.devices (
  id p11.entity_id PRIMARY KEY,
  seller_id p11.entity_id NOT NULL REFERENCES p11.seller_profiles(id),
  platform text NOT NULL CHECK (platform IN ('WINDOWS','LINUX','ANDROID')),
  resource_kind text NOT NULL CHECK (resource_kind IN ('GPU_DEVICE','ANDROID_DEVICE','AUTHORIZED_RELAY')),
  state text NOT NULL,
  auth_key_digest p11.sha256_hex NOT NULL UNIQUE,
  client_release text NOT NULL,
  resource_policy jsonb NOT NULL CHECK (jsonb_typeof(resource_policy)='object'),
  version p11.counter NOT NULL DEFAULT 1,
  last_seen_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(id,seller_id)
);
CREATE TABLE p11.pairing_intents (
  id p11.entity_id PRIMARY KEY,
  seller_id p11.entity_id NOT NULL REFERENCES p11.seller_profiles(id),
  token_digest p11.sha256_hex NOT NULL UNIQUE,
  expires_at timestamptz NOT NULL,
  consumed_at timestamptz,
  device_id p11.entity_id REFERENCES p11.devices(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK ((consumed_at IS NULL)=(device_id IS NULL))
);
CREATE TABLE p11.resources (
  id p11.entity_id PRIMARY KEY,
  device_id p11.entity_id NOT NULL,
  seller_id p11.entity_id NOT NULL,
  local_resource_key text NOT NULL,
  kind text NOT NULL CHECK (kind IN ('GPU_DEVICE','ANDROID_DEVICE','AUTHORIZED_RELAY')),
  version p11.counter NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY(device_id,seller_id) REFERENCES p11.devices(id,seller_id),
  UNIQUE(device_id,local_resource_key),
  UNIQUE(id,device_id,seller_id)
);
CREATE TABLE p11.evidence (
  id p11.entity_id PRIMARY KEY,
  evidence_type text NOT NULL,
  seller_id p11.entity_id REFERENCES p11.seller_profiles(id),
  device_id p11.entity_id REFERENCES p11.devices(id),
  content_ref text NOT NULL,
  digest p11.sha256_hex NOT NULL,
  scope_snapshot jsonb NOT NULL,
  decision text NOT NULL CHECK (decision IN ('PENDING','APPROVED','REJECTED','REVOKED')),
  reviewed_by p11.entity_id REFERENCES p11.users(id),
  valid_from timestamptz NOT NULL,
  valid_until timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK(valid_until > valid_from)
);
CREATE TABLE p11.capabilities (
  id p11.entity_id PRIMARY KEY,
  device_id p11.entity_id NOT NULL,
  seller_id p11.entity_id NOT NULL,
  resource_id p11.entity_id NOT NULL,
  engine_kind text NOT NULL,
  engine_release text NOT NULL,
  kernel_environment_ref p11.entity_id NOT NULL,
  kernel_environment_digest p11.sha256_hex NOT NULL,
  model_digest p11.sha256_hex NOT NULL,
  recipe_digest p11.sha256_hex NOT NULL,
  protocol_profile text NOT NULL,
  mode text NOT NULL CHECK(mode IN ('PLAINTEXT','E2E','GPU_RENTAL')),
  privacy text NOT NULL CHECK(privacy IN ('STANDARD','PLATFORM_BLIND','ATTESTED_CONFIDENTIAL')),
  evidence_ids text[] NOT NULL,
  state text NOT NULL CHECK(state IN ('DRAFT','TESTING','QUALIFIED','REVOKED','EXPIRED')),
  version p11.counter NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY(resource_id,device_id,seller_id) REFERENCES p11.resources(id,device_id,seller_id),
  UNIQUE(id,device_id,seller_id),
  CHECK((mode='PLAINTEXT' AND privacy='STANDARD') OR
        (mode='E2E' AND privacy IN ('PLATFORM_BLIND','ATTESTED_CONFIDENTIAL')) OR
        (mode='GPU_RENTAL' AND privacy IN ('STANDARD','ATTESTED_CONFIDENTIAL')))
);
CREATE TABLE p11.listings (
  id p11.entity_id PRIMARY KEY,
  seller_id p11.entity_id NOT NULL REFERENCES p11.seller_profiles(id),
  state text NOT NULL,
  current_revision p11.counter NOT NULL DEFAULT 1,
  version p11.counter NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(id,seller_id)
);
CREATE TABLE p11.listing_versions (
  listing_id p11.entity_id NOT NULL,
  revision p11.counter NOT NULL,
  seller_id p11.entity_id NOT NULL,
  region text NOT NULL,
  contract_template jsonb NOT NULL CHECK(jsonb_typeof(contract_template)='object'),
  price_version text NOT NULL,
  valid_from timestamptz NOT NULL,
  valid_until timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(listing_id,revision),
  UNIQUE(listing_id,revision,seller_id),
  FOREIGN KEY(listing_id,seller_id) REFERENCES p11.listings(id,seller_id),
  CHECK(valid_until>valid_from)
);
CREATE TABLE p11.listing_devices (
  listing_id p11.entity_id NOT NULL,
  revision p11.counter NOT NULL,
  seller_id p11.entity_id NOT NULL,
  device_id p11.entity_id NOT NULL,
  capability_id p11.entity_id NOT NULL,
  PRIMARY KEY(listing_id,revision,device_id,capability_id),
  FOREIGN KEY(listing_id,revision,seller_id) REFERENCES p11.listing_versions(listing_id,revision,seller_id),
  FOREIGN KEY(capability_id,device_id,seller_id) REFERENCES p11.capabilities(id,device_id,seller_id)
);
CREATE TABLE p11.quotes (
  id p11.entity_id PRIMARY KEY,
  buyer_id p11.entity_id NOT NULL REFERENCES p11.users(id),
  seller_id p11.entity_id NOT NULL,
  listing_id p11.entity_id NOT NULL,
  listing_revision p11.counter NOT NULL,
  contract_snapshot jsonb NOT NULL CHECK(jsonb_typeof(contract_snapshot)='object'),
  contract_hash p11.sha256_hex NOT NULL,
  max_amount_fen p11.money_fen NOT NULL,
  expires_at timestamptz NOT NULL,
  consumed_by p11.entity_id,
  consumption_kind text CHECK(consumption_kind IN ('ORDER','RENTAL_RENEWAL')),
  created_at timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY(listing_id,listing_revision,seller_id) REFERENCES p11.listing_versions(listing_id,revision,seller_id),
  UNIQUE(id,buyer_id,seller_id),
  CHECK((consumed_by IS NULL)=(consumption_kind IS NULL))
);
CREATE TABLE p11.orders (
  id p11.entity_id PRIMARY KEY,
  quote_id p11.entity_id NOT NULL UNIQUE,
  buyer_id p11.entity_id NOT NULL,
  seller_id p11.entity_id NOT NULL,
  parent_order_id p11.entity_id REFERENCES p11.orders(id),
  state text NOT NULL CHECK(state IN ('CREATED','WAITING_PAYMENT','READY','FULFILLING','DELIVERY_PENDING','COMPLETED','CLOSED','REVIEW')),
  mode text NOT NULL CHECK(mode IN ('PLAINTEXT','E2E','GPU_RENTAL')),
  privacy text NOT NULL CHECK(privacy IN ('STANDARD','PLATFORM_BLIND','ATTESTED_CONFIDENTIAL')),
  currency text NOT NULL DEFAULT 'CNY' CHECK(currency='CNY'),
  max_amount_fen p11.money_fen NOT NULL,
  fee_bps integer NOT NULL CHECK(fee_bps BETWEEN 0 AND 10000),
  contract_snapshot jsonb NOT NULL CHECK(jsonb_typeof(contract_snapshot)='object'),
  contract_hash p11.sha256_hex NOT NULL,
  deadline timestamptz NOT NULL,
  version p11.counter NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY(quote_id,buyer_id,seller_id) REFERENCES p11.quotes(id,buyer_id,seller_id),
  UNIQUE(id,seller_id), UNIQUE(id,buyer_id,seller_id),
  CHECK((mode='PLAINTEXT' AND privacy='STANDARD') OR
        (mode='E2E' AND privacy IN ('PLATFORM_BLIND','ATTESTED_CONFIDENTIAL')) OR
        (mode='GPU_RENTAL' AND privacy IN ('STANDARD','ATTESTED_CONFIDENTIAL')))
);
CREATE INDEX orders_buyer_cursor ON p11.orders(buyer_id,created_at DESC,id DESC);
CREATE INDEX orders_seller_cursor ON p11.orders(seller_id,created_at DESC,id DESC);
CREATE TABLE p11.route_contracts (
  id p11.entity_id PRIMARY KEY,
  order_id p11.entity_id NOT NULL,
  buyer_id p11.entity_id NOT NULL,
  seller_id p11.entity_id NOT NULL,
  contract_hash p11.sha256_hex NOT NULL,
  protocol_profile text NOT NULL,
  state text NOT NULL CHECK(state IN ('ACTIVE','SUSPENDED','EXPIRED','REVOKED')),
  expires_at timestamptz NOT NULL,
  version p11.counter NOT NULL DEFAULT 1,
  FOREIGN KEY(order_id,buyer_id,seller_id) REFERENCES p11.orders(id,buyer_id,seller_id)
);
-- API keys may bind multiple routes. The identity application locks/checks
-- ownership and ACTIVE state for every route; invocation rechecks the route.
CREATE TABLE p11.budget_reservations (
  id p11.entity_id PRIMARY KEY,
  order_id p11.entity_id NOT NULL REFERENCES p11.orders(id),
  purpose_ref p11.entity_id NOT NULL,
  amount_fen p11.money_fen NOT NULL,
  state text NOT NULL CHECK(state IN ('RESERVED','CONSUMED','RELEASED')),
  version p11.counter NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(order_id,purpose_ref)
);
CREATE TABLE p11.payments (
  id p11.entity_id PRIMARY KEY,
  order_id p11.entity_id NOT NULL REFERENCES p11.orders(id),
  purpose text NOT NULL CHECK(purpose IN ('ORDER','RENTAL_RENEWAL')),
  purpose_ref p11.entity_id NOT NULL,
  provider text NOT NULL,
  environment text NOT NULL CHECK(environment IN ('SANDBOX','PRODUCTION')),
  original_request_key text NOT NULL,
  channel_reference text,
  product_profile_revision text NOT NULL,
  channel_binding_snapshot jsonb NOT NULL CHECK(jsonb_typeof(channel_binding_snapshot)='object'),
  prepay_reference text,
  amount_fen p11.money_fen NOT NULL,
  state text NOT NULL CHECK(state IN ('CREATED','PENDING','AUTHORIZED','SETTLED','FAILED','UNKNOWN','CLOSED')),
  funds_expires_at timestamptz,
  version p11.counter NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(provider,environment,original_request_key),
  UNIQUE(provider,environment,channel_reference),
  UNIQUE(id,order_id)
);
CREATE UNIQUE INDEX payments_live_purpose ON p11.payments(order_id,purpose,purpose_ref)
  WHERE state NOT IN ('FAILED','CLOSED');
CREATE TABLE p11.refunds (
  id p11.entity_id PRIMARY KEY,
  payment_id p11.entity_id NOT NULL,
  order_id p11.entity_id NOT NULL,
  original_request_key text NOT NULL UNIQUE,
  channel_reference text,
  amount_fen p11.money_fen NOT NULL CHECK(amount_fen>0),
  state text NOT NULL CHECK(state IN ('REQUESTED','PROCESSING','SUCCEEDED','FAILED','UNKNOWN')),
  version p11.counter NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY(payment_id,order_id) REFERENCES p11.payments(id,order_id)
);
CREATE TABLE p11.channel_requests (
  id p11.entity_id PRIMARY KEY,
  operation text NOT NULL CHECK(operation IN ('PAY','REFUND','SETTLEMENT','QUERY')),
  target_ref p11.entity_id NOT NULL,
  provider text NOT NULL,
  environment text NOT NULL CHECK(environment IN ('SANDBOX','PRODUCTION')),
  original_request_key text NOT NULL,
  request_digest p11.sha256_hex NOT NULL,
  state text NOT NULL CHECK(state IN ('INTENT','SENT','SUCCEEDED','FAILED','UNKNOWN')),
  safe_result jsonb,
  next_query_at timestamptz,
  version p11.counter NOT NULL DEFAULT 1,
  UNIQUE(provider,environment,operation,original_request_key)
);
CREATE TABLE p11.jobs (
  id p11.entity_id PRIMARY KEY,
  order_id p11.entity_id NOT NULL,
  seller_id p11.entity_id NOT NULL,
  caller_key text NOT NULL,
  request_digest p11.sha256_hex NOT NULL,
  state text NOT NULL CHECK(state IN ('INPUT_PENDING','QUEUED','RUNNING','VALIDATING','WAITING_BUYER','ACCEPTED','FAILED','CANCELED','UNVERIFIED')),
  fencing_token p11.counter NOT NULL DEFAULT 0,
  current_attempt_id p11.entity_id,
  accepted_delivery_id p11.entity_id,
  deadline timestamptz NOT NULL,
  version p11.counter NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY(order_id,seller_id) REFERENCES p11.orders(id,seller_id),
  UNIQUE(id,order_id,seller_id), UNIQUE(order_id,caller_key)
);
CREATE TABLE p11.recipient_keys (
  id p11.entity_id PRIMARY KEY,
  owner_id p11.entity_id NOT NULL REFERENCES p11.users(id),
  device_id p11.entity_id REFERENCES p11.devices(id),
  key_version p11.counter NOT NULL,
  encryption_public_key text NOT NULL,
  signing_public_key text NOT NULL,
  profile_id text NOT NULL,
  trust_evidence_id p11.entity_id REFERENCES p11.evidence(id),
  expires_at timestamptz NOT NULL,
  revoked_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(id,key_version)
);
CREATE TABLE p11.encryption_intents (
  id p11.entity_id PRIMARY KEY,
  quote_id p11.entity_id NOT NULL REFERENCES p11.quotes(id),
  buyer_id p11.entity_id NOT NULL REFERENCES p11.users(id),
  recipient_id p11.entity_id NOT NULL,
  recipient_version p11.counter NOT NULL,
  context_hash p11.sha256_hex NOT NULL,
  expires_at timestamptz NOT NULL,
  FOREIGN KEY(recipient_id,recipient_version) REFERENCES p11.recipient_keys(id,key_version)
);
CREATE TABLE p11.attempts (
  id p11.entity_id PRIMARY KEY,
  job_id p11.entity_id NOT NULL,
  order_id p11.entity_id NOT NULL,
  seller_id p11.entity_id NOT NULL,
  device_id p11.entity_id NOT NULL,
  resource_id p11.entity_id NOT NULL,
  capability_id p11.entity_id NOT NULL,
  fencing_token p11.counter NOT NULL,
  recipient_id p11.entity_id REFERENCES p11.recipient_keys(id),
  resource_lock_generation p11.counter,
  state text NOT NULL CHECK(state IN ('PREPARED','ISSUED','ACKED','RUNNING','DELIVERED','FAILED','CANCELED','EXPIRED','CLEANING','CLEANED','QUARANTINED')),
  engine_reference text,
  cleanup_state text NOT NULL DEFAULT 'PENDING' CHECK(cleanup_state IN ('PENDING','ACKED','FAILED')),
  version p11.counter NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY(job_id,order_id,seller_id) REFERENCES p11.jobs(id,order_id,seller_id),
  FOREIGN KEY(resource_id,device_id,seller_id) REFERENCES p11.resources(id,device_id,seller_id),
  FOREIGN KEY(capability_id,device_id,seller_id) REFERENCES p11.capabilities(id,device_id,seller_id),
  UNIQUE(job_id,fencing_token), UNIQUE(id,job_id),
  UNIQUE(id,job_id,fencing_token), UNIQUE(id,resource_id),
  UNIQUE(id,resource_id,resource_lock_generation),
  CHECK(state NOT IN ('ISSUED','ACKED','RUNNING','DELIVERED','CLEANING') OR resource_lock_generation IS NOT NULL)
);
ALTER TABLE p11.jobs ADD CONSTRAINT jobs_current_attempt_fk
  FOREIGN KEY(current_attempt_id,id,fencing_token) REFERENCES p11.attempts(id,job_id,fencing_token)
  DEFERRABLE INITIALLY DEFERRED;
CREATE TABLE p11.execution_leases (
  id p11.entity_id PRIMARY KEY,
  attempt_id p11.entity_id NOT NULL UNIQUE,
  job_id p11.entity_id NOT NULL,
  fencing_token p11.counter NOT NULL,
  resource_id p11.entity_id NOT NULL,
  resource_lock_generation p11.counter NOT NULL,
  lease_revision integer NOT NULL DEFAULT 1 CHECK(lease_revision>0),
  issued_at timestamptz NOT NULL,
  not_before timestamptz NOT NULL,
  hard_deadline timestamptz NOT NULL,
  execution_manifest_ref p11.entity_id NOT NULL,
  execution_manifest_hash p11.sha256_hex NOT NULL,
  state text NOT NULL CHECK(state IN ('ISSUED','ACKED','REVOKED','EXPIRED','CLOSED')),
  expires_at timestamptz NOT NULL,
  signed_payload jsonb NOT NULL,
  signature_profile_id text NOT NULL,
  signature text NOT NULL,
  last_progress_sequence p11.counter NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK(not_before<=issued_at AND issued_at<expires_at AND expires_at<=hard_deadline),
  FOREIGN KEY(attempt_id,job_id,fencing_token) REFERENCES p11.attempts(id,job_id,fencing_token),
  FOREIGN KEY(attempt_id,resource_id,resource_lock_generation) REFERENCES p11.attempts(id,resource_id,resource_lock_generation)
);
CREATE TABLE p11.assets (
  id p11.entity_id PRIMARY KEY,
  order_id p11.entity_id NOT NULL REFERENCES p11.orders(id),
  owner_id p11.entity_id NOT NULL REFERENCES p11.users(id),
  job_id p11.entity_id REFERENCES p11.jobs(id),
  attempt_id p11.entity_id REFERENCES p11.attempts(id),
  visibility text NOT NULL CHECK(visibility IN ('PLAINTEXT','CIPHERTEXT')),
  state text NOT NULL CHECK(state IN ('REGISTERED','UPLOADING','READY','QUARANTINED','DELETE_PENDING','DELETED')),
  storage_key text NOT NULL UNIQUE,
  storage_digest p11.sha256_hex,
  bytes bigint NOT NULL CHECK(bytes BETWEEN 0 AND 67108864),
  media_type text NOT NULL,
  expires_at timestamptz NOT NULL,
  deleted_at timestamptz,
  version p11.counter NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(id,order_id)
);
CREATE TABLE p11.envelopes (
  id p11.entity_id PRIMARY KEY,
  job_id p11.entity_id NOT NULL,
  attempt_id p11.entity_id NOT NULL,
  fencing_token p11.counter NOT NULL,
  recipient_id p11.entity_id NOT NULL REFERENCES p11.recipient_keys(id),
  asset_id p11.entity_id NOT NULL REFERENCES p11.assets(id),
  context_hash p11.sha256_hex NOT NULL,
  profile_id text NOT NULL,
  sealed_key text NOT NULL,
  stream_header text NOT NULL,
  sender_signature text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY(attempt_id,job_id,fencing_token) REFERENCES p11.attempts(id,job_id,fencing_token),
  UNIQUE(attempt_id,asset_id,recipient_id)
);
CREATE TABLE p11.cipher_channels (
  id p11.entity_id PRIMARY KEY,
  job_id p11.entity_id NOT NULL,
  attempt_id p11.entity_id NOT NULL,
  fencing_token p11.counter NOT NULL,
  direction text NOT NULL CHECK(direction IN ('BUYER_TO_SELLER','SELLER_TO_BUYER')),
  auth_digest p11.sha256_hex NOT NULL,
  epoch p11.counter NOT NULL,
  expires_at timestamptz NOT NULL,
  state text NOT NULL CHECK(state IN ('OPEN','CLOSED','REVOKED')),
  FOREIGN KEY(attempt_id,job_id,fencing_token) REFERENCES p11.attempts(id,job_id,fencing_token),
  UNIQUE(attempt_id,direction,epoch)
);
CREATE TABLE p11.deliveries (
  id p11.entity_id PRIMARY KEY,
  job_id p11.entity_id NOT NULL,
  attempt_id p11.entity_id NOT NULL,
  fencing_token p11.counter NOT NULL,
  revision p11.counter NOT NULL,
  manifest_hash p11.sha256_hex NOT NULL,
  asset_ids text[] NOT NULL,
  validation_evidence_id p11.entity_id REFERENCES p11.evidence(id),
  state text NOT NULL CHECK(state IN ('REGISTERED','VALIDATING','WAITING_BUYER','VALID','INVALID','STALE')),
  created_at timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY(attempt_id,job_id,fencing_token) REFERENCES p11.attempts(id,job_id,fencing_token),
  UNIQUE(attempt_id,revision), UNIQUE(id,job_id,attempt_id)
);
ALTER TABLE p11.jobs ADD CONSTRAINT jobs_accepted_delivery_fk
  FOREIGN KEY(accepted_delivery_id,id,current_attempt_id) REFERENCES p11.deliveries(id,job_id,attempt_id)
  DEFERRABLE INITIALLY DEFERRED;
CREATE TABLE p11.acceptances (
  id p11.entity_id PRIMARY KEY,
  job_id p11.entity_id NOT NULL UNIQUE,
  delivery_id p11.entity_id NOT NULL,
  attempt_id p11.entity_id NOT NULL,
  receipt_id p11.entity_id UNIQUE,
  signer_id p11.entity_id NOT NULL REFERENCES p11.users(id),
  evidence_hash p11.sha256_hex NOT NULL,
  decision text NOT NULL CHECK(decision IN ('ACCEPT','REJECT')),
  signature text,
  created_at timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY(delivery_id,job_id,attempt_id) REFERENCES p11.deliveries(id,job_id,attempt_id)
);
CREATE TABLE p11.resource_locks (
  resource_id p11.entity_id PRIMARY KEY REFERENCES p11.resources(id),
  lock_generation p11.counter NOT NULL DEFAULT 0,
  phase text NOT NULL CHECK(phase IN ('FREE','RESERVED','ACTIVE','STOPPING','CLEANING','QUARANTINED')),
  owner_attempt_id p11.entity_id REFERENCES p11.attempts(id),
  owner_session_id p11.entity_id,
  expires_at timestamptz,
  version p11.counter NOT NULL DEFAULT 1,
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK((phase='FREE' AND owner_attempt_id IS NULL AND owner_session_id IS NULL) OR
        (phase IN ('RESERVED','ACTIVE','STOPPING','CLEANING') AND
         ((owner_attempt_id IS NOT NULL)::int+(owner_session_id IS NOT NULL)::int)=1) OR
        (phase='QUARANTINED' AND
         ((owner_attempt_id IS NOT NULL)::int+(owner_session_id IS NOT NULL)::int)<=1))
);
CREATE TABLE p11.transfer_grants (
  id p11.entity_id PRIMARY KEY,
  asset_id p11.entity_id NOT NULL REFERENCES p11.assets(id),
  subject_id p11.entity_id NOT NULL REFERENCES p11.users(id),
  token_digest p11.sha256_hex NOT NULL UNIQUE,
  purpose text NOT NULL CHECK(purpose IN ('UPLOAD','DOWNLOAD','SUPPORT')),
  expires_at timestamptz NOT NULL,
  revoked_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE p11.deletion_tasks (
  id p11.entity_id PRIMARY KEY,
  asset_id p11.entity_id REFERENCES p11.assets(id),
  target_ref text NOT NULL,
  tombstone_at timestamptz NOT NULL,
  hold_evidence_id p11.entity_id REFERENCES p11.evidence(id),
  state text NOT NULL CHECK(state IN ('PENDING','HELD','ACKED','FAILED')),
  attempts integer NOT NULL DEFAULT 0 CHECK(attempts>=0),
  last_safe_error text,
  UNIQUE(target_ref,tombstone_at)
);
CREATE TABLE p11.rental_sessions (
  id p11.entity_id PRIMARY KEY,
  order_id p11.entity_id NOT NULL UNIQUE,
  seller_id p11.entity_id NOT NULL,
  device_id p11.entity_id NOT NULL,
  resource_id p11.entity_id NOT NULL,
  resource_lock_generation p11.counter NOT NULL,
  state text NOT NULL CHECK(state IN ('REQUESTED','RESERVED','PREPARING','READY_WAIT_BUYER','ACTIVE','STOPPING','FAULTED','RECONCILING','CLEANING','CLOSED','QUARANTINED')),
  cleanup_state text NOT NULL CHECK(cleanup_state IN ('NOT_STARTED','PENDING','ACKED','FAILED')),
  rate_fen_per_minute p11.money_fen NOT NULL,
  budget_fen p11.money_fen NOT NULL,
  terms_revision p11.counter NOT NULL DEFAULT 1,
  reserved_until timestamptz,
  prepare_until timestamptz,
  buyer_ready_until timestamptz,
  started_at timestamptz,
  ends_at timestamptz,
  stopped_at timestamptz,
  version p11.counter NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY(order_id,seller_id) REFERENCES p11.orders(id,seller_id),
  FOREIGN KEY(resource_id,device_id,seller_id) REFERENCES p11.resources(id,device_id,seller_id),
  UNIQUE(id,order_id), UNIQUE(id,resource_id),
  CHECK(ends_at IS NULL OR started_at IS NULL OR ends_at>started_at)
);
ALTER TABLE p11.resource_locks ADD CONSTRAINT resource_lock_session_fk
  FOREIGN KEY(owner_session_id,resource_id) REFERENCES p11.rental_sessions(id,resource_id);
ALTER TABLE p11.resource_locks ADD CONSTRAINT resource_lock_attempt_fk
  FOREIGN KEY(owner_attempt_id,resource_id) REFERENCES p11.attempts(id,resource_id);
CREATE TABLE p11.rental_renewals (
  id p11.entity_id PRIMARY KEY,
  session_id p11.entity_id NOT NULL,
  order_id p11.entity_id NOT NULL,
  quote_id p11.entity_id NOT NULL UNIQUE,
  buyer_id p11.entity_id NOT NULL,
  seller_id p11.entity_id NOT NULL,
  terms_revision p11.counter NOT NULL,
  terms_snapshot jsonb NOT NULL CHECK(jsonb_typeof(terms_snapshot)='object'),
  terms_hash p11.sha256_hex NOT NULL,
  confirmed_at timestamptz NOT NULL,
  payment_id p11.entity_id NOT NULL,
  added_budget_fen p11.money_fen NOT NULL CHECK(added_budget_fen>0),
  proposed_ends_at timestamptz NOT NULL,
  state text NOT NULL CHECK(state IN ('PENDING_PAYMENT','APPLIED','REJECTED','REFUNDING','UNKNOWN')),
  created_at timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY(session_id,order_id) REFERENCES p11.rental_sessions(id,order_id),
  FOREIGN KEY(order_id,buyer_id,seller_id) REFERENCES p11.orders(id,buyer_id,seller_id),
  FOREIGN KEY(quote_id,buyer_id,seller_id) REFERENCES p11.quotes(id,buyer_id,seller_id),
  FOREIGN KEY(payment_id,order_id) REFERENCES p11.payments(id,order_id),
  UNIQUE(payment_id), UNIQUE(session_id,terms_revision)
);
CREATE TABLE p11.rental_access_grants (
  id p11.entity_id PRIMARY KEY,
  session_id p11.entity_id NOT NULL REFERENCES p11.rental_sessions(id),
  subject_id p11.entity_id NOT NULL REFERENCES p11.users(id),
  digest p11.sha256_hex NOT NULL UNIQUE,
  resource_lock_generation p11.counter NOT NULL,
  terms_revision p11.counter NOT NULL,
  terms_hash p11.sha256_hex NOT NULL,
  safe_claims jsonb NOT NULL CHECK(jsonb_typeof(safe_claims)='object'),
  scopes text[] NOT NULL,
  expires_at timestamptz NOT NULL,
  revoked_at timestamptz
);
CREATE TABLE p11.availability_intervals (
  id p11.entity_id PRIMARY KEY,
  session_id p11.entity_id NOT NULL REFERENCES p11.rental_sessions(id),
  sequence p11.counter NOT NULL,
  resource_lock_generation p11.counter NOT NULL,
  begins_at timestamptz NOT NULL,
  ends_at timestamptz NOT NULL,
  duration_ms bigint NOT NULL CHECK(duration_ms>0),
  local_monotonic_begin bigint NOT NULL,
  local_monotonic_end bigint NOT NULL,
  state text NOT NULL CHECK(state IN ('CLAIMED','CONFIRMED','REJECTED','UNKNOWN')),
  evidence_hash p11.sha256_hex NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(session_id,sequence),
  CHECK(ends_at>begins_at AND local_monotonic_end>local_monotonic_begin)
);
CREATE INDEX intervals_session_time ON p11.availability_intervals(session_id,begins_at,ends_at);
CREATE TABLE p11.meter_statements (
  id p11.entity_id PRIMARY KEY,
  session_id p11.entity_id NOT NULL REFERENCES p11.rental_sessions(id),
  revision p11.counter NOT NULL,
  confirmed_duration_ms bigint NOT NULL CHECK(confirmed_duration_ms>=0),
  amount_fen p11.money_fen NOT NULL,
  evidence_hash p11.sha256_hex NOT NULL,
  is_final boolean NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(session_id,revision)
);
CREATE UNIQUE INDEX meter_statement_final ON p11.meter_statements(session_id) WHERE is_final;
CREATE TABLE p11.usage_evidence (
  id p11.entity_id PRIMARY KEY,
  job_id p11.entity_id NOT NULL,
  attempt_id p11.entity_id NOT NULL,
  fencing_token p11.counter NOT NULL,
  sequence p11.counter NOT NULL,
  tokenizer_profile text NOT NULL,
  model_digest p11.sha256_hex NOT NULL,
  input_tokens p11.counter,
  output_tokens p11.counter,
  cached_tokens p11.counter,
  status text NOT NULL CHECK(status IN ('VERIFIED','UNVERIFIED','REJECTED')),
  evidence_hash p11.sha256_hex NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY(attempt_id,job_id,fencing_token) REFERENCES p11.attempts(id,job_id,fencing_token),
  UNIQUE(attempt_id,sequence)
);
CREATE TABLE p11.trade_qualifications (
  id p11.entity_id PRIMARY KEY,
  order_id p11.entity_id NOT NULL REFERENCES p11.orders(id),
  source_type text NOT NULL CHECK(source_type IN ('ACCEPTANCE','FINAL_RENTAL_METER','VERIFIED_USAGE')),
  source_id p11.entity_id NOT NULL,
  gross_fen p11.money_fen NOT NULL,
  currency text NOT NULL DEFAULT 'CNY' CHECK(currency='CNY'),
  evidence_hash p11.sha256_hex NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(source_type,source_id)
);
CREATE TABLE p11.ledger_accounts (
  id p11.entity_id PRIMARY KEY,
  account_code text NOT NULL UNIQUE,
  classification text NOT NULL,
  currency text NOT NULL DEFAULT 'CNY' CHECK(currency='CNY'),
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE p11.journal_transactions (
  id p11.entity_id PRIMARY KEY,
  order_id p11.entity_id NOT NULL REFERENCES p11.orders(id),
  source_type text NOT NULL,
  source_id p11.entity_id NOT NULL,
  reverses_id p11.entity_id REFERENCES p11.journal_transactions(id),
  currency text NOT NULL DEFAULT 'CNY' CHECK(currency='CNY'),
  writer_xact xid8 NOT NULL DEFAULT pg_current_xact_id(),
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(source_type,source_id), UNIQUE(id,order_id)
);
CREATE TABLE p11.journal_entries (
  id p11.entity_id PRIMARY KEY,
  journal_id p11.entity_id NOT NULL REFERENCES p11.journal_transactions(id),
  account_id p11.entity_id NOT NULL REFERENCES p11.ledger_accounts(id),
  -- Signed debit (+) / credit (-); never a withdrawable user balance.
  amount_fen bigint NOT NULL CHECK(amount_fen<>0 AND amount_fen BETWEEN -9007199254740991 AND 9007199254740991),
  line_number integer NOT NULL CHECK(line_number>0),
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(journal_id,line_number)
);
CREATE TABLE p11.settlements (
  id p11.entity_id PRIMARY KEY,
  seller_id p11.entity_id NOT NULL REFERENCES p11.seller_profiles(id),
  state text NOT NULL CHECK(state IN ('PENDING_EVIDENCE','DISPUTED','CHANNEL_ELIGIBLE','SUBMITTED','SETTLED','UNKNOWN','REVERSED','FAILED')),
  version p11.counter NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(id,seller_id)
);
CREATE TABLE p11.settlement_items (
  id p11.entity_id PRIMARY KEY,
  settlement_id p11.entity_id NOT NULL,
  seller_id p11.entity_id NOT NULL,
  order_id p11.entity_id NOT NULL,
  journal_id p11.entity_id NOT NULL UNIQUE,
  gross_fen p11.money_fen NOT NULL,
  commission_fen p11.money_fen NOT NULL,
  channel_cost_fen p11.money_fen NOT NULL,
  seller_net_fen p11.money_fen NOT NULL,
  state text NOT NULL CHECK(state IN ('HELD','ELIGIBLE','BATCHED','SETTLED','REVERSED')),
  FOREIGN KEY(settlement_id,seller_id) REFERENCES p11.settlements(id,seller_id),
  FOREIGN KEY(order_id,seller_id) REFERENCES p11.orders(id,seller_id),
  FOREIGN KEY(journal_id,order_id) REFERENCES p11.journal_transactions(id,order_id),
  CHECK(gross_fen::numeric=commission_fen::numeric+channel_cost_fen::numeric+seller_net_fen::numeric)
);
CREATE TABLE p11.settlement_batches (
  id p11.entity_id PRIMARY KEY,
  state text NOT NULL CHECK(state IN ('DRAFT','REVIEWING','APPROVED','SUBMITTED','SETTLED','UNKNOWN','FAILED')),
  prepared_by p11.entity_id NOT NULL REFERENCES p11.users(id),
  approved_by p11.entity_id REFERENCES p11.users(id),
  original_request_key text NOT NULL UNIQUE,
  provider text NOT NULL,
  environment text NOT NULL CHECK(environment IN ('SANDBOX','PRODUCTION')),
  evidence_hash p11.sha256_hex,
  version p11.counter NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK(approved_by IS NULL OR approved_by<>prepared_by)
);
CREATE TABLE p11.batch_items (
  batch_id p11.entity_id NOT NULL REFERENCES p11.settlement_batches(id),
  item_id p11.entity_id NOT NULL UNIQUE REFERENCES p11.settlement_items(id),
  PRIMARY KEY(batch_id,item_id)
);
CREATE TABLE p11.reviews (
  id p11.entity_id PRIMARY KEY,
  order_id p11.entity_id NOT NULL REFERENCES p11.orders(id),
  author_id p11.entity_id NOT NULL REFERENCES p11.users(id),
  rating integer NOT NULL CHECK(rating BETWEEN 1 AND 5),
  safe_text text NOT NULL CHECK(length(safe_text)<=2000),
  state text NOT NULL CHECK(state IN ('VISIBLE','HELD','REMOVED')),
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(order_id,author_id)
);
CREATE TABLE p11.disputes (
  id p11.entity_id PRIMARY KEY,
  order_id p11.entity_id NOT NULL REFERENCES p11.orders(id),
  opened_by p11.entity_id NOT NULL REFERENCES p11.users(id),
  state text NOT NULL CHECK(state IN ('OPEN','WAITING_PARTIES','REVIEWING','DECIDED','APPEALED','CLOSED')),
  reason_code text NOT NULL,
  decision_code text,
  version p11.counter NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE p11.appeals (
  id p11.entity_id PRIMARY KEY,
  dispute_id p11.entity_id NOT NULL REFERENCES p11.disputes(id),
  author_id p11.entity_id NOT NULL REFERENCES p11.users(id),
  reason_code text NOT NULL,
  statement_ref text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE p11.evidence_grants (
  id p11.entity_id PRIMARY KEY,
  order_id p11.entity_id NOT NULL REFERENCES p11.orders(id),
  granted_by p11.entity_id NOT NULL REFERENCES p11.users(id),
  subject_id p11.entity_id NOT NULL REFERENCES p11.users(id),
  evidence_ref p11.entity_id NOT NULL,
  purpose text NOT NULL,
  expires_at timestamptz NOT NULL,
  revoked_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE p11.idempotency_records (
  principal_id p11.entity_id NOT NULL REFERENCES p11.users(id),
  operation_id text NOT NULL,
  resource_scope text NOT NULL,
  key_digest p11.sha256_hex NOT NULL,
  request_digest p11.sha256_hex NOT NULL,
  response_reference p11.entity_id,
  safe_response jsonb CHECK(safe_response IS NULL OR jsonb_typeof(safe_response)='object'),
  response_http_status integer CHECK(response_http_status BETWEEN 100 AND 599),
  state text NOT NULL CHECK(state IN ('PENDING','SUCCEEDED','FAILED','UNKNOWN')),
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(principal_id,operation_id,resource_scope,key_digest)
);
CREATE TABLE p11.outbox_events (
  id p11.entity_id PRIMARY KEY,
  aggregate_type text NOT NULL,
  aggregate_id p11.entity_id NOT NULL,
  aggregate_version p11.counter NOT NULL,
  event_type text NOT NULL,
  schema_version text NOT NULL,
  safe_payload jsonb NOT NULL CHECK(jsonb_typeof(safe_payload)='object'),
  state text NOT NULL CHECK(state IN ('PENDING','CLAIMED','DELIVERED','DEAD')),
  available_at timestamptz NOT NULL DEFAULT now(),
  claim_generation p11.counter NOT NULL DEFAULT 0,
  claimed_until timestamptz,
  tries integer NOT NULL DEFAULT 0 CHECK(tries>=0),
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(aggregate_type,aggregate_id,aggregate_version,event_type)
);
CREATE INDEX outbox_ready ON p11.outbox_events(state,available_at,id);
CREATE TABLE p11.inbox_events (
  consumer text NOT NULL,
  event_id p11.entity_id NOT NULL,
  payload_digest p11.sha256_hex NOT NULL,
  state text NOT NULL CHECK(state IN ('CLAIMED','DONE','DEAD')),
  claim_generation p11.counter NOT NULL DEFAULT 0,
  claimed_until timestamptz,
  processed_at timestamptz,
  PRIMARY KEY(consumer,event_id)
);
CREATE TABLE p11.durable_jobs (
  id p11.entity_id PRIMARY KEY,
  kind text NOT NULL,
  dedupe_key text NOT NULL UNIQUE,
  target_ref p11.entity_id NOT NULL,
  state text NOT NULL CHECK(state IN ('PENDING','CLAIMED','DONE','DEAD')),
  run_at timestamptz NOT NULL,
  claim_generation p11.counter NOT NULL DEFAULT 0,
  claimed_until timestamptz,
  tries integer NOT NULL DEFAULT 0 CHECK(tries>=0),
  last_safe_error text,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX durable_ready ON p11.durable_jobs(state,run_at,id);
CREATE TABLE p11.audit_events (
  id p11.entity_id PRIMARY KEY,
  actor_id p11.entity_id REFERENCES p11.users(id),
  action text NOT NULL,
  target_ref text NOT NULL,
  request_id p11.entity_id NOT NULL,
  safe_facts jsonb NOT NULL CHECK(jsonb_typeof(safe_facts)='object'),
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE p11.feature_gates (
  id p11.entity_id PRIMARY KEY,
  scope_key text NOT NULL UNIQUE,
  enabled boolean NOT NULL DEFAULT false,
  evidence_ids text[] NOT NULL DEFAULT '{}',
  approved_by p11.entity_id REFERENCES p11.users(id),
  version p11.counter NOT NULL DEFAULT 1,
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK(NOT enabled OR (cardinality(evidence_ids)>0 AND approved_by IS NOT NULL))
);
CREATE TABLE p11.releases (
  id p11.entity_id PRIMARY KEY,
  capability_id p11.entity_id NOT NULL REFERENCES p11.capabilities(id),
  gate_id p11.entity_id NOT NULL REFERENCES p11.feature_gates(id),
  artifact_digest p11.sha256_hex NOT NULL,
  scope_snapshot jsonb NOT NULL,
  evidence_ids text[] NOT NULL,
  state text NOT NULL CHECK(state IN ('PROPOSED','APPROVED','ACTIVE','REVOKED','EXPIRED')),
  approved_by p11.entity_id REFERENCES p11.users(id),
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE p11.notifications (
  id p11.entity_id PRIMARY KEY,
  user_id p11.entity_id NOT NULL REFERENCES p11.users(id),
  source_event_id p11.entity_id NOT NULL,
  target_ref p11.entity_id NOT NULL,
  safe_code text NOT NULL,
  read_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(user_id,source_event_id)
);
CREATE TABLE p11.catalog_entries (
  id p11.entity_id NOT NULL,
  revision p11.counter NOT NULL,
  kind text NOT NULL,
  manifest_digest p11.sha256_hex NOT NULL,
  manifest_ref text NOT NULL,
  license_evidence_id p11.entity_id NOT NULL REFERENCES p11.evidence(id),
  state text NOT NULL CHECK(state IN ('REVIEWING','APPROVED','REVOKED','EXPIRED')),
  PRIMARY KEY(id,revision)
);

-- Immutable financial journals. Deferred balance includes the header so an empty
-- header also fails at commit. These function bodies still need native PG tests.
CREATE FUNCTION p11.reject_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'append-only record: %', TG_TABLE_NAME;
END;
$$;
CREATE FUNCTION p11.check_journal_balance() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE journal_ref text; entry_count bigint; signed_sum numeric;
BEGIN
  IF TG_TABLE_NAME='journal_transactions' THEN journal_ref:=NEW.id;
  ELSE journal_ref:=NEW.journal_id; END IF;
  SELECT count(*),coalesce(sum(amount_fen::numeric),0)
    INTO entry_count,signed_sum FROM p11.journal_entries WHERE journal_id=journal_ref;
  IF entry_count<2 OR signed_sum<>0 THEN
    RAISE EXCEPTION 'unbalanced or empty journal: %', journal_ref;
  END IF;
  RETURN NULL;
END;
$$;
CREATE FUNCTION p11.stamp_journal_writer() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  NEW.writer_xact:=pg_current_xact_id();
  RETURN NEW;
END;
$$;
CREATE FUNCTION p11.guard_journal_entry_insert() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE header_xact xid8;
BEGIN
  SELECT writer_xact INTO header_xact FROM p11.journal_transactions WHERE id=NEW.journal_id;
  IF header_xact IS DISTINCT FROM pg_current_xact_id() THEN
    RAISE EXCEPTION 'entries must be inserted in the journal creation transaction';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER journal_stamp BEFORE INSERT ON p11.journal_transactions
  FOR EACH ROW EXECUTE FUNCTION p11.stamp_journal_writer();
CREATE TRIGGER journal_new_entries_only BEFORE INSERT ON p11.journal_entries
  FOR EACH ROW EXECUTE FUNCTION p11.guard_journal_entry_insert();
CREATE TRIGGER journal_header_immutable BEFORE UPDATE OR DELETE ON p11.journal_transactions
  FOR EACH ROW EXECUTE FUNCTION p11.reject_mutation();
CREATE TRIGGER journal_entries_immutable BEFORE UPDATE OR DELETE ON p11.journal_entries
  FOR EACH ROW EXECUTE FUNCTION p11.reject_mutation();
CREATE CONSTRAINT TRIGGER journal_header_balanced AFTER INSERT ON p11.journal_transactions
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION p11.check_journal_balance();
CREATE CONSTRAINT TRIGGER journal_entries_balanced AFTER INSERT ON p11.journal_entries
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION p11.check_journal_balance();
CREATE TRIGGER listing_version_immutable BEFORE UPDATE OR DELETE ON p11.listing_versions
  FOR EACH ROW EXECUTE FUNCTION p11.reject_mutation();
CREATE FUNCTION p11.protect_quote_contract() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF ROW(NEW.id,NEW.buyer_id,NEW.seller_id,NEW.listing_id,NEW.listing_revision,NEW.contract_snapshot,
         NEW.contract_hash,NEW.max_amount_fen,NEW.expires_at,NEW.created_at)
     IS DISTINCT FROM
     ROW(OLD.id,OLD.buyer_id,OLD.seller_id,OLD.listing_id,OLD.listing_revision,OLD.contract_snapshot,
         OLD.contract_hash,OLD.max_amount_fen,OLD.expires_at,OLD.created_at)
  THEN RAISE EXCEPTION 'quote terms are immutable'; END IF;
  IF OLD.consumed_by IS NOT NULL AND
     ROW(NEW.consumed_by,NEW.consumption_kind) IS DISTINCT FROM ROW(OLD.consumed_by,OLD.consumption_kind)
  THEN RAISE EXCEPTION 'quote consumption cannot change'; END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER quote_terms_immutable BEFORE UPDATE ON p11.quotes
  FOR EACH ROW EXECUTE FUNCTION p11.protect_quote_contract();
CREATE TRIGGER quote_no_delete BEFORE DELETE ON p11.quotes
  FOR EACH ROW EXECUTE FUNCTION p11.reject_mutation();
CREATE FUNCTION p11.consume_quote_once() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE consume_kind text; changed_count bigint;
BEGIN
  IF TG_TABLE_NAME='orders' THEN consume_kind:='ORDER'; ELSE consume_kind:='RENTAL_RENEWAL'; END IF;
  UPDATE p11.quotes SET consumed_by=NEW.id,consumption_kind=consume_kind
    WHERE id=NEW.quote_id AND buyer_id=NEW.buyer_id AND seller_id=NEW.seller_id
      AND consumed_by IS NULL AND expires_at>clock_timestamp();
  GET DIAGNOSTICS changed_count=ROW_COUNT;
  IF changed_count<>1 THEN RAISE EXCEPTION 'quote expired, mismatched or already consumed'; END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER order_consume_quote BEFORE INSERT ON p11.orders
  FOR EACH ROW EXECUTE FUNCTION p11.consume_quote_once();
CREATE TRIGGER renewal_consume_quote BEFORE INSERT ON p11.rental_renewals
  FOR EACH ROW EXECUTE FUNCTION p11.consume_quote_once();
CREATE FUNCTION p11.protect_renewal_terms() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF ROW(NEW.id,NEW.session_id,NEW.order_id,NEW.quote_id,NEW.buyer_id,NEW.seller_id,NEW.terms_revision,
         NEW.terms_snapshot,NEW.terms_hash,NEW.confirmed_at,NEW.payment_id,NEW.added_budget_fen,NEW.proposed_ends_at)
     IS DISTINCT FROM
     ROW(OLD.id,OLD.session_id,OLD.order_id,OLD.quote_id,OLD.buyer_id,OLD.seller_id,OLD.terms_revision,
         OLD.terms_snapshot,OLD.terms_hash,OLD.confirmed_at,OLD.payment_id,OLD.added_budget_fen,OLD.proposed_ends_at)
  THEN RAISE EXCEPTION 'renewal terms are immutable'; END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER renewal_terms_immutable BEFORE UPDATE ON p11.rental_renewals
  FOR EACH ROW EXECUTE FUNCTION p11.protect_renewal_terms();
CREATE TRIGGER renewal_no_delete BEFORE DELETE ON p11.rental_renewals
  FOR EACH ROW EXECUTE FUNCTION p11.reject_mutation();
CREATE FUNCTION p11.protect_order_contract() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF ROW(NEW.quote_id,NEW.buyer_id,NEW.seller_id,NEW.parent_order_id,NEW.mode,NEW.privacy,
         NEW.currency,NEW.max_amount_fen,NEW.fee_bps,NEW.contract_snapshot,NEW.contract_hash,NEW.deadline)
     IS DISTINCT FROM
     ROW(OLD.quote_id,OLD.buyer_id,OLD.seller_id,OLD.parent_order_id,OLD.mode,OLD.privacy,
         OLD.currency,OLD.max_amount_fen,OLD.fee_bps,OLD.contract_snapshot,OLD.contract_hash,OLD.deadline)
  THEN RAISE EXCEPTION 'order contract is immutable'; END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER order_contract_immutable BEFORE UPDATE ON p11.orders
  FOR EACH ROW EXECUTE FUNCTION p11.protect_order_contract();
CREATE TRIGGER order_no_delete BEFORE DELETE ON p11.orders
  FOR EACH ROW EXECUTE FUNCTION p11.reject_mutation();
-- Channel product/app/service/seller binding is immutable after intent creation.
-- Store secret references only; proof/session/app_auth_token must not be embedded.
CREATE FUNCTION p11.protect_payment_binding() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF ROW(NEW.id,NEW.order_id,NEW.purpose,NEW.purpose_ref,NEW.provider,NEW.environment,
         NEW.original_request_key,NEW.amount_fen,NEW.product_profile_revision,NEW.channel_binding_snapshot)
     IS DISTINCT FROM
     ROW(OLD.id,OLD.order_id,OLD.purpose,OLD.purpose_ref,OLD.provider,OLD.environment,
         OLD.original_request_key,OLD.amount_fen,OLD.product_profile_revision,OLD.channel_binding_snapshot)
  THEN RAISE EXCEPTION 'payment binding is immutable'; END IF;
  IF (OLD.prepay_reference IS NOT NULL AND NEW.prepay_reference IS DISTINCT FROM OLD.prepay_reference)
     OR (OLD.channel_reference IS NOT NULL AND NEW.channel_reference IS DISTINCT FROM OLD.channel_reference)
  THEN RAISE EXCEPTION 'channel references cannot be replaced'; END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER payment_binding_immutable BEFORE UPDATE ON p11.payments
  FOR EACH ROW EXECUTE FUNCTION p11.protect_payment_binding();
CREATE TRIGGER payment_no_delete BEFORE DELETE ON p11.payments
  FOR EACH ROW EXECUTE FUNCTION p11.reject_mutation();
CREATE TRIGGER audit_no_mutation BEFORE UPDATE OR DELETE ON p11.audit_events
  FOR EACH ROW EXECUTE FUNCTION p11.reject_mutation();
COMMIT;
