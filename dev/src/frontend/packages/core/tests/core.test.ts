import test from 'node:test';
import assert from 'node:assert/strict';
import { sha256 } from '@noble/hashes/sha256';
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
