-- R005 forward-only records. Never rewrite migrations 001 through 003.
-- Submitted references are claims, not identity/channel/device approvals.
ALTER TABLE p11.seller_profiles
  ADD COLUMN display_name text NOT NULL DEFAULT '个人卖家',
  ADD COLUMN approved_regions text[] NOT NULL DEFAULT '{}';
ALTER TABLE p11.devices ADD COLUMN model_name text;
ALTER TABLE p11.listings ADD COLUMN updated_at timestamptz NOT NULL DEFAULT clock_timestamp();
ALTER TABLE p11.consents
  ADD COLUMN purpose text CHECK (purpose IN ('TRADE','PRIVACY','LAN','SELECTIVE_DISCLOSURE')),
  ADD COLUMN confirmed_at timestamptz;
CREATE TRIGGER consent_record_immutable BEFORE UPDATE OR DELETE ON p11.consents
  FOR EACH ROW EXECUTE FUNCTION p11.reject_mutation();

CREATE TABLE p11.seller_applications (
  id p11.entity_id PRIMARY KEY,
  seller_id p11.entity_id NOT NULL REFERENCES p11.seller_profiles(id),
  profile_version p11.counter NOT NULL CHECK(profile_version>0),
  submitted_claims jsonb NOT NULL CHECK(jsonb_typeof(submitted_claims)='object'),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  UNIQUE(seller_id,profile_version)
);
CREATE TRIGGER seller_application_immutable BEFORE UPDATE OR DELETE ON p11.seller_applications
  FOR EACH ROW EXECUTE FUNCTION p11.reject_mutation();

CREATE TABLE p11.listing_definitions (
  listing_id p11.entity_id NOT NULL,
  revision p11.counter NOT NULL,
  seller_id p11.entity_id NOT NULL,
  definition jsonb NOT NULL CHECK(jsonb_typeof(definition)='object'),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(listing_id,revision),
  FOREIGN KEY(listing_id,revision,seller_id) REFERENCES p11.listing_versions(listing_id,revision,seller_id)
);
CREATE TRIGGER listing_definition_immutable BEFORE UPDATE OR DELETE ON p11.listing_definitions
  FOR EACH ROW EXECUTE FUNCTION p11.reject_mutation();
