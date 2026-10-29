-- Forward-only NB contract guards. 001 remains byte-for-byte immutable.
-- Hourly amounts live in the versioned JSON contract. The legacy rental
-- integer-minute column is NOT migrated by this change.
-- listing_version_immutable already belongs to 001; do not recreate it.
CREATE TRIGGER listing_device_binding_immutable BEFORE UPDATE OR DELETE ON p11.listing_devices
  FOR EACH ROW EXECUTE FUNCTION p11.reject_mutation();

CREATE FUNCTION p11.order_matches_quote() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE q p11.quotes%ROWTYPE;
BEGIN
  SELECT * INTO q FROM p11.quotes WHERE id=NEW.quote_id;
  IF NOT FOUND OR ROW(NEW.buyer_id,NEW.seller_id,NEW.contract_snapshot,NEW.contract_hash,NEW.max_amount_fen)
       IS DISTINCT FROM ROW(q.buyer_id,q.seller_id,q.contract_snapshot,q.contract_hash,q.max_amount_fen)
  THEN RAISE EXCEPTION 'order must preserve the confirmed quote snapshot'; END IF;
  IF NEW.contract_snapshot#>>'{price,price_contract_version}'='NB_PRICE_1' THEN
    IF (NEW.mode=NEW.contract_snapshot#>>'{policy,service_mode}'
        AND NEW.privacy=NEW.contract_snapshot#>>'{policy,privacy_profile}'
        AND NEW.fee_bps=(NEW.contract_snapshot#>>'{price,commission_bps}')::integer
        AND NEW.contract_hash=NEW.contract_snapshot->>'contract_hash'
        AND NEW.max_amount_fen=(NEW.contract_snapshot->>'total_budget_fen')::bigint) IS NOT TRUE
    THEN RAISE EXCEPTION 'order columns must preserve NB contract terms'; END IF;
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER order_validate_snapshot BEFORE INSERT ON p11.orders
  FOR EACH ROW EXECUTE FUNCTION p11.order_matches_quote();
