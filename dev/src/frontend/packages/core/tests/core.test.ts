import test from 'node:test';
import assert from 'node:assert/strict';
import { sha256 } from '@noble/hashes/sha256';
import { type Device, type PolicyInput, pairingOf, deviceOf, providerCommand, policyFromForm, policyForm } from '../src/provider';
import { ControlClient, ApiError, CommandJournal, base64url, beginLogin, finishLogin, AppModel,
  estimate, parseYuan, yuan, splitIncludedFee, rateText, offerOf, priceOf, beginNativeLogin, type Price, type Offer, type Storage, type Session, type Transport, object } from '../src/index';

const session: Session = { session_id: 'ses_fixture', client_type: 'WEB', expires_at: '2099-01-01T00:00:00Z', csrf_token: 'fixture_csrf_not_a_real_secret', profile: { user_id: 'usr_buyer', roles: ['BUYER'], permission_scope_ids: ['market:read'], identity_state: 'ACTIVE', version: 1 } };
const rate = (n: number, d = 1) => ({ numerator_fen: n, denominator_units: d });
const hourly: Price = { price_contract_version: 'NB_PRICE_1', currency: 'CNY', unit: 'AVAILABLE_HOUR', rounding_profile: 'ORDER_CEIL_FEN_1', hour_rate: rate(360), commission_bps: 500, channel_cost_owner: 'SELLER', fee_policy_version: 'fee_nb1' };
const tokens: Price = { ...hourly, unit: 'TOKEN_1M', input_rate: rate(80), output_rate: rate(160) }; delete tokens.hour_rate;
const offer = (): Offer => ({ listing_id: 'lst_fixture', listing_revision: 1, seller_id: 'usr_seller', seller_display_name: '测试个人卖家', sku_id: 'sku_gpu', policy: { service_mode: 'GPU_RENTAL', privacy_profile: 'STANDARD', region: 'CN', protocol_profile_id: 'profile_fixture', acceptance_policy_id: 'accept_fixture', disclosure_version: 'disclosure_fixture' }, public_spec: { duration_ms: 3600000 }, price: hourly, kernel_bindings: [], device_displays: [{ device_id: 'dev_fixture', model_name: 'RTX 4090 · 24GB' }], deadline_sec: 3600, availability: 'AVAILABLE', evidence_refs: [] });
function store(): Storage { const map = new Map<string, string>(); return { get: k => map.get(k) ?? null, set: (k, v) => { map.set(k, v); }, remove: k => { map.delete(k); } }; }
const random = async (n: number) => new Uint8Array(n).fill(7);
const ok = (data: unknown) => ({ status: 200, headers: {}, data });
function client(transport: Transport) { const c = new ControlClient('', transport, 'WEB'); c.setSession(session); return c; }

test('UI-UNIT-001 decimal currency never accepts exponent, truncation or an unsafe integer', () => {
  assert.equal(parseYuan('5.00'), 500); assert.equal(parseYuan('0.01'), 1); assert.equal(yuan(9007199254740991), '¥90071992547409.91');
  for (const value of ['', '.5', '01', '1.001', '1e3', '-1', ' 5', '90071992547409.92']) assert.throws(() => parseYuan(value));
});
test('UI-UNIT-002 rational hourly and dual-token previews round only once after summation', () => {
  assert.equal(estimate(hourly, { duration_ms: 1800000 }), 180);
  assert.equal(estimate(tokens, { input_token_limit: 1250, output_token_limit: 1250 }), 1);
  assert.deepEqual(splitIncludedFee(180, 500), { commission: 9, seller: 171 });
  assert.equal(estimate({ ...hourly, hour_rate: rate(1, 3) }, { duration_ms: 1 }), 1);
  assert.throws(() => estimate(tokens, { input_token_limit: 0 }));
});
test('UI-UNIT-003 rate labels preserve fractions and never turn a small positive rate into free', () => {
  assert.equal(rateText(rate(360)), '¥3.60'); assert.equal(rateText(rate(0)), '¥0.00');
  assert.match(rateText(rate(1, 3)), /^约 /); assert.equal(rateText(rate(1, 1_000_000)), '< ¥0.000001');
});
test('UI-UNIT-004 unqualified price, unit, privacy and generic phone data fail closed', () => {
  assert.equal(offerOf(offer()).listing_id, 'lst_fixture');
  const bad = [ { ...offer(), price: { ...hourly, commission_bps: null } }, { ...offer(), device_displays: [{ device_id: 'd', model_name: 'Android 设备' }] },
    { ...offer(), policy: { ...offer().policy, service_mode: 'E2E' } }, { ...offer(), listing_revision: Number.MAX_SAFE_INTEGER + 1 } ];
  bad.forEach(value => assert.throws(() => offerOf(value)));
  assert.throws(() => priceOf({ ...tokens, cached_input_rate: rate(5) }));
});
test('UI-UNIT-005 PKCE uses the RFC 7636 SHA-256 vector and refuses a failed random source', async () => {
  assert.equal(base64url(sha256('dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk')), 'E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM');
  let calls = 0; const c = new ControlClient('', async request => { calls++; const body = JSON.parse(request.body!); assert.equal(body.code_challenge.length, 43); assert.equal(body.client_type, 'WEB'); return ok({ challenge_id: 'ach_fixture', client_type: 'WEB', expires_at: '2099-01-01T00:00:00Z', authorization_url: 'https://openauth.alipay.com/test' }); }, 'WEB');
  const p = await beginLogin(c, random); assert.equal(p.verifier.length, 43);
  await assert.rejects(beginLogin(c, async () => { throw new Error('No secure RNG'); })); assert.equal(calls, 1);
  await assert.rejects(finishLogin(c, p.challenge, p.verifier, 'synthetic_code', 'wrong_state')); assert.equal(calls, 1);
});
test('UI-UNIT-006 WEB mutations use CSRF and native sessions only use the bearer transport', async () => {
  const web = client(async request => { assert.equal(request.headers['X-CSRF-Token'], session.csrf_token); assert.equal(request.headers.Authorization, undefined); return ok({}); });
  await web.request('POST', '/quotes', object, {}, 'original_key');
  const native = new ControlClient('https://api.example.invalid', async request => { assert.equal(request.headers.Authorization, 'Bearer fixture_native'); assert.equal(request.headers['X-CSRF-Token'], undefined); return ok({}); }, 'MINIAPP');
  native.setSession({ ...session, client_type: 'MINIAPP', csrf_token: undefined, access_token: 'fixture_native' }); await native.request('POST', '/quotes', object, {}, 'original_key');
  assert.throws(() => web.setSession({ ...session, access_token: 'must_not_be_exposed' }));
});
test('UI-UNIT-007 a lost mutation receipt survives reload and retries the identical command', async () => {
  const storage = store(), seen: { key: string; body: string }[] = []; let fail = true;
  const transport: Transport = async request => { seen.push({ key: request.headers['Idempotency-Key'], body: request.body! }); if (fail) { fail = false; throw new Error('receipt lost after commit'); } return ok({ id: 'ord_original' }); };
  const first = new CommandJournal(client(transport), storage, random);
  await assert.rejects(first.run('/orders', { quote_id: 'quo_1', caller_key: 'caller_1', disclosure_accepted_at: '2026-10-30T00:00:00Z' }, object), (e: unknown) => e instanceof ApiError && e.uncertain);
  const restored = new CommandJournal(client(transport), storage, async () => { throw new Error('must reuse original key'); });
  const p = restored.pending()!; assert.equal(p.owner, 'usr_buyer'); await restored.run('/orders', p.body, object);
  assert.deepEqual(seen[0], seen[1]); assert.equal(restored.pending(), null);
});
test('UI-UNIT-008 unknown results block changed parameters, unavailable storage and simultaneous writes', async () => {
  const storage = store(); let release: (x: ReturnType<typeof ok>) => void = () => {};
  const journal = new CommandJournal(client(() => new Promise(resolve => { release = resolve; })), storage, random);
  const first = journal.run('/quotes', { budget_fen: 500 }, object);
  await new Promise(resolve => setImmediate(resolve)); await assert.rejects(journal.run('/quotes', { budget_fen: 600 }, object));
  release(ok({ id: 'quote' })); await first;
  let calls = 0; const inaccessible: Storage = { ...store(), set() { throw new Error('storage unavailable'); } };
  await assert.rejects(new CommandJournal(client(async () => { calls++; return ok({}); }), inaccessible, random).run('/orders', {}, object)); assert.equal(calls, 0);
});
test('UI-UNIT-009 role revocation and a malformed success preserve pending evidence', async () => {
  const storage = store(); let status = 503;
  const journal = new CommandJournal(client(async () => ({ status, headers: {}, data: status === 200 ? null : { error: { code: 'FORBIDDEN' } } })), storage, random);
  await assert.rejects(journal.run('/orders', {}, object)); const key = journal.pending()!.key;
  status = 403; await assert.rejects(journal.run('/orders', {}, object)); assert.equal(journal.pending()!.key, key);
  status = 200; await assert.rejects(journal.run('/orders', {}, object)); assert.equal(journal.pending()!.key, key);
});
test('UI-UNIT-010 changing authenticated owner while preparing a command cannot send it under a new account', async () => {
  let calls = 0, release: ((x: Uint8Array) => void) | undefined;
  const c = client(async () => { calls++; return ok({}); }); const storage = store();
  const journal = new CommandJournal(c, storage, () => new Promise(resolve => { release = resolve; }));
  const result = journal.run('/orders', { quote_id: 'quo_owner' }, object);
  await new Promise(resolve => setImmediate(resolve)); c.setSession({ ...session, profile: { ...session.profile, user_id: 'usr_other' } }); release!(new Uint8Array(32));
  await assert.rejects(result); assert.equal(calls, 0); assert.equal(journal.pending(), null); c.setSession(session); assert.equal(journal.pending()!.owner, 'usr_buyer');
});
test('UI-UNIT-011 stale market responses cannot overwrite a newer mode or refill data after logout', async () => {
  const requests: { request: string; resolve: (x: ReturnType<typeof ok>) => void }[] = [];
  const c = client(async request => request.method === 'DELETE' ? ok({}) : new Promise(resolve => requests.push({ request: request.url, resolve })));
  const model = new AppModel(c, store(), random, { async login() {}, async resume() { return session; } });
  const start = model.start(); await new Promise(resolve => setImmediate(resolve)); requests.shift()!.resolve(ok({ items: [offer()], next_cursor: null })); await start;
  const older = model.loadMarket('PLAINTEXT'), newer = model.loadMarket('GPU_RENTAL');
  const old = requests.shift()!, next = requests.shift()!; next.resolve(ok({ items: [offer()], next_cursor: null })); await newer;
  old.resolve(ok({ items: [], next_cursor: null })); await older; assert.equal(model.snapshot().offers.length, 1); assert.equal(model.snapshot().mode, 'GPU_RENTAL');
  const pending = model.loadMarket(); const delayed = requests.shift()!; await model.logout(); delayed.resolve(ok({ items: [offer()], next_cursor: null })); await pending;
  assert.equal(model.snapshot().session, null); assert.deepEqual(model.snapshot().offers, []);
});
test('UI-UNIT-012 E2E stays gated without a verified local gateway and has no Prompt transport', async () => {
  let writes = 0; const c = client(async request => { if (request.method !== 'GET') writes++; return ok({ items: [], next_cursor: null }); });
  const model = new AppModel(c, store(), random, { async login() {}, async resume() { return session; } }); await model.start();
  model.select({ ...offer(), policy: { ...offer().policy, service_mode: 'E2E', privacy_profile: 'PLATFORM_BLIND' }, price: tokens, public_spec: { model_id: 'qwen_3b', input_token_limit: 1000, output_token_limit: 1000 } });
  await model.makeQuote({ model_id: 'qwen_3b', input_token_limit: 100, output_token_limit: 100 }, 500);
  assert.equal(writes, 0); assert.match(model.snapshot().error, /网关配对/);
});
test('UI-UNIT-013 native nonce is an explicit profile and command IDs use a separate memory-only PRF', async () => {
  let calls = 0;
  const c = new ControlClient('https://api.example.invalid', async request => {
    calls++; assert.deepEqual(JSON.parse(request.body!), { client_type: 'MINIAPP', proof_profile: 'ALIPAY_NATIVE_NONCE_1' });
    return ok({ challenge_id: 'ach_native', client_type: 'MINIAPP', proof_profile: 'ALIPAY_NATIVE_NONCE_1', native_code_verifier: 'A'.repeat(43), expires_at: '2099-01-01T00:00:00Z' });
  }, 'MINIAPP');
  const descriptor = Object.getOwnPropertyDescriptor(globalThis, 'TextEncoder')!;
  Object.defineProperty(globalThis, 'TextEncoder', { value: undefined, configurable: true });
  try {
    const p = await beginNativeLogin(c); assert.equal(p.verifier, 'A'.repeat(43));
    const first = await p.commandBytes(32), second = await p.commandBytes(32);
    assert.equal(first.length, 32); assert.notDeepEqual(first, second); assert.notEqual(base64url(first), p.verifier);
    assert.equal(calls, 1); await assert.rejects(p.commandBytes(16));
  } finally { Object.defineProperty(globalThis, 'TextEncoder', descriptor); }
  await assert.rejects(beginNativeLogin(client(async () => { throw new Error('WEB must not request a native proof'); })));
});

const sellerSession: Session = { ...session, profile: { ...session.profile, roles: ['BUYER', 'SELLER'] } };
const providerDevice = (): Device => ({ device_id: 'dev_provider', seller_id: 'usr_buyer', platform: 'WINDOWS', resource_kind: 'GPU_DEVICE', state: 'PAIRED', version: 1, model_name: 'RTX 4090 24GB', device_label: '本人书房显卡', identity_profile_id: 'ED25519_DEVICE_1', key_fingerprint: 'a'.repeat(64), client_release_id: 'SYNTHETIC_TEST', last_seen_at: null, heartbeat_sequence: null, reported_resource_state: 'UNKNOWN', active_slots: 0, capability_ids: [], resource_policy: { sharing_enabled: false, accepting_new_jobs: false, active_slots: 1, windows: [], minimum_free_disk_bytes: 0, stop_policy: 'DRAIN' } });
const pairInput = { platform: 'WINDOWS' as const, device_label: '本人书房显卡', owner_confirmed: true };
const pairReceipt = () => ({ pairing_id: 'pair_synthetic', seller_id: session.profile.user_id, expires_at: '2099-01-01T00:00:00Z', one_time_token: 'p11p.pair_synthetic.' + 'A'.repeat(43) });
const providerPolicy = (): PolicyInput => ({ expected_version: 1, sharing_enabled: true, windows: [{ starts_at: '2090-01-01T00:00:00Z', ends_at: '2090-01-01T02:00:00Z' }], active_slots: 1, minimum_free_disk_bytes: 1000000, stop_policy: 'DRAIN' });
async function providerModel(route: Transport, storage = store()) {
  const c = client(async request => {
    if (request.url.endsWith('/seller/profile')) return ok({ seller_id: session.profile.user_id, display_name: '合成已审核卖家', qualification_state: 'APPROVED', public_identity_notice: 'SYNTHETIC', payee_eligibility_state: 'UNVERIFIED', approved_regions: [], version: 1, evidence_refs: [] });
    if (request.url.includes('/seller/listings') || request.url.includes('/services?')) return ok({ items: [], next_cursor: null });
    if (request.url.includes('/seller/devices?')) return ok({ items: [providerDevice()], next_cursor: null });
    return route(request);
  });
  const m = new AppModel(c, storage, random, { async login() {}, async resume() { return sellerSession; } });
  await m.start(); await m.loadSeller(); return m;
}
test('UI-UNIT-014 provider projections reject cross-protocol secrets, generic phones and unsafe sequence values', () => {
  assert.equal(deviceOf(providerDevice()).device_id, 'dev_provider');
  for (const value of [{ ...providerDevice(), heartbeat_sequence: '18446744073709551616' }, { ...providerDevice(), model_name: 'Android 设别' }, { ...providerDevice(), resource_policy: { ...providerDevice().resource_policy, accepting_new_jobs: null } }]) assert.throws(() => deviceOf(value));
  assert.throws(() => pairingOf({ ...pairReceipt(), access_token: 'forbidden' }));
  assert.throws(() => pairingOf({ ...pairReceipt(), one_time_token: 'p11p.pair_other.' + 'A'.repeat(43) }));
});
test('UI-UNIT-015 original PATCH method, ETag and body survive reload without containing a credential', async () => {
  const storage = store(), seen: unknown[] = []; let fail = true;
  const c = client(async r => { seen.push([r.method, r.body, r.headers['Idempotency-Key'], r.headers['If-Match']]); if (fail) { fail = false; throw new Error('committed receipt lost'); } return ok({ id: 'dev_provider' }); });
  const a = new CommandJournal(c, storage, random), p = providerPolicy();
  await assert.rejects(a.run('/devices/dev_provider/resource-policy', p, object, 'PATCH', '"1"'));
  const b = new CommandJournal(c, storage, async () => { throw new Error('must retain original key'); }), original = b.pending()!;
  await b.run(original.path, original.body, object, original.method, original.ifMatch);
  assert.deepEqual(seen[0], seen[1]); assert.equal(b.pending(), null);
});
test('UI-UNIT-016 provider journal refuses token-bearing payloads, wrong methods, weak ETags and arbitrary routes before saving or sending', async () => {
  let calls = 0, saved = 0; const storage = { ...store(), set() { saved++; } };
  const j = new CommandJournal(client(async () => { calls++; return ok({}); }), storage, random);
  await assert.rejects(j.run('/devices/pairing-intents', { ...pairInput, one_time_token: pairReceipt().one_time_token }, object));
  await assert.rejects(j.run('/device-enrollments', {}, object));
  await assert.rejects(j.run('/devices/dev_provider/resource-policy', providerPolicy(), object, 'PATCH', 'W/"1"'));
  await assert.rejects(j.run('/devices/dev_provider/revoke', { expected_version: 1, reason_code: 'OWNER_REVOKE', content: 'forbidden' }, object));
  assert.equal(calls, 0); assert.equal(saved, 0);
});
test('UI-UNIT-017 lost pairing reply restores the same intent without redisclosing or persisting its one-time token', async () => {
  const backing = store(), saved: string[] = [], requests: unknown[] = []; const storage = { ...backing, set(k: string, v: string) { saved.push(v); backing.set(k, v); } }; let lost = true;
  const m = await providerModel(async r => { requests.push([r.body, r.headers['Idempotency-Key']]); if (lost) { lost = false; throw new Error('first response lost'); } const { one_time_token: _secret, ...safe } = pairReceipt(); return ok(safe); }, storage);
  await m.createPairing(pairInput); assert.equal(m.snapshot().pending, true); assert.equal(m.snapshot().pairing, null);
  await m.recover(); assert.deepEqual(requests[0], requests[1]); assert.equal(m.snapshot().pairing?.one_time_token, undefined); assert.equal(m.snapshot().pending, false);
  assert(!saved.join('').includes('p11p.')); assert.match(m.snapshot().providerNotice, /凭据不再显示/);
});
test('UI-UNIT-018 clearing or hiding during an in-flight pairing also suppresses a late secret; expired receipts stay hidden', async () => {
  let resolve: (x: ReturnType<typeof ok>) => void = () => {};
  const m = await providerModel(() => new Promise(r => { resolve = r; }));
  const pending = m.createPairing(pairInput); await new Promise(r => setImmediate(r)); m.clearPairingSecret(); resolve(ok(pairReceipt())); await pending;
  assert.equal(m.snapshot().pairing?.one_time_token, undefined);
  const next = m.createPairing(pairInput); await new Promise(r => setImmediate(r)); resolve(ok(pairReceipt())); await next;
  m.revealPairing(); assert(m.snapshot().pairingVisible); m.clearPairingSecret(); assert.equal(m.snapshot().pairingVisible, false); assert.equal(m.snapshot().pairing?.one_time_token, undefined);
  const expired = await providerModel(async () => ok({ ...pairReceipt(), expires_at: '2000-01-01T00:00:00Z' })); await expired.createPairing(pairInput); assert.equal(expired.snapshot().pairing?.one_time_token, undefined);
});
test('UI-UNIT-019 shared time form preserves Beijing timezone, rejects invalid dates/overlaps and requires explicit phone conditions', () => {
  const d = providerDevice(), f = policyForm(d); f.enabled = true; f.windows = [{ start: '2090-01-01 08:00', end: '2090-01-01 10:00' }]; f.freeDiskMB = '1024';
  assert.throws(() => policyFromForm(d, f, false));
  const p = policyFromForm(d, f, true); assert.equal(p.windows[0].starts_at, '2090-01-01T00:00:00.000Z'); assert.equal(p.minimum_free_disk_bytes, 1024000000);
  assert.throws(() => policyFromForm(d, { ...f, windows: [{ start: '2090-02-30 08:00', end: '2090-03-01 09:00' }] }, true));
  assert.throws(() => policyFromForm(d, { ...f, windows: [...f.windows, ...f.windows] }, true));
  const phone = policyFromForm({ ...d, platform: 'ANDROID', model_name: '摩托罗拉 X30' }, f, true);
  assert.equal(phone.charging_required, true); assert.equal(phone.minimum_battery_percent, 30); assert.equal(phone.maximum_temperature_celsius, 45);
  assert.throws(() => providerCommand('/devices/dev_provider/resource-policy', 'PATCH', { ...p, accepting_new_jobs: true }, '"1"'));
});
test('UI-UNIT-020 policy acknowledgement does not claim device execution; stale versions require a fresh owner read', async () => {
  let writes = 0; const m = await providerModel(async r => { writes++; assert.equal(r.headers['If-Match'], '"1"'); return ok({ id: 'dev_provider', version: 2, status: 'PROBING', recorded_at: '2090-01-01T00:00:00Z' }); });
  await m.savePolicy(providerDevice(), providerPolicy()); assert.equal(m.snapshot().deviceAck?.version, 2); assert.equal(m.snapshot().devices[0].state, 'PAIRED'); assert.equal(m.snapshot().devices[0].resource_policy.accepting_new_jobs, false);
  assert.match(m.snapshot().providerNotice, /设备执行与资格仍待确认/); assert.equal(writes, 1);
  await m.savePolicy(providerDevice(), providerPolicy()); await m.changeDevice(providerDevice(), 'revoke', true); assert.equal(writes, 1); assert.match(m.snapshot().error, /刷新本人设备/);
  const wrong = await providerModel(async () => ok({ id: 'dev_other', version: 2, status: 'PROBING', recorded_at: '2090-01-01T00:00:00Z' })); await wrong.savePolicy(providerDevice(), providerPolicy()); assert.equal(wrong.snapshot().pending, true); assert.equal(wrong.snapshot().deviceAck, null);
});
test('UI-UNIT-021 unconfirmed/foreign device actions never send; permission revocation clears the ephemeral pairing secret', async () => {
  let writes = 0, denied = false;
  const m = await providerModel(async () => { writes++; return denied ? { status: 403, headers: {}, data: { error: { code: 'FORBIDDEN' } } } : ok(pairReceipt()); });
  await m.createPairing(pairInput); assert(m.snapshot().pairing?.one_time_token); const before = writes;
  await m.changeDevice(providerDevice(), 'drain', false); await m.changeDevice({ ...providerDevice(), seller_id: 'usr_other' }, 'revoke', true); assert.equal(writes, before);
  denied = true; await m.changeDevice(providerDevice(), 'drain', true); assert.equal(m.snapshot().pairing?.one_time_token, undefined); assert(m.snapshot().pending);
  for (const reply of [{ ...providerDevice(), state: 'REVOKED', version: 1 }, { ...providerDevice(), state: 'PAIRED', version: 2 }]) {
    const wrong = await providerModel(async () => ok(reply)); await wrong.changeDevice(providerDevice(), 'revoke', true); assert(wrong.snapshot().pending); assert.equal(wrong.snapshot().devices[0].state, 'PAIRED');
  }
});
