// SOURCE_ONLY: these fixtures define future batch validation. They do not
// provide an actual approved PayLauncher, funds, channel or SDK admission.
import test from 'node:test';
import assert from 'node:assert/strict';
import { ApiError, ControlClient, PaymentFlow, PaymentChallenge, PaymentProof,
  type Contract, type Order, type Payment, type PayLauncher, type Session, type Storage, type Transport, type Response } from '../src/index';

const session: Session = { session_id: 'ses_checkout', client_type: 'WEB', expires_at: '2099-01-01T00:00:00Z', csrf_token: 'synthetic_csrf_checkout',
  profile: { user_id: 'usr_buyer', roles: ['BUYER'], permission_scope_ids: ['payment:read','payment:create'], identity_state: 'ACTIVE', version: 1 } };
const contract: Contract = { seller_id: 'usr_seller', listing_id: 'lst_original', listing_revision: 2, device_ids: ['dev_original'], sku_id: 'sku_llm',
  policy: { service_mode: 'PLAINTEXT', privacy_profile: 'STANDARD', region: 'CN', protocol_profile_id: 'proto_original', acceptance_policy_id: 'accept_original', disclosure_version: 'disclosure_original' },
  public_spec: { model_id: 'model_original', input_token_limit: 1000, output_token_limit: 1000 },
  price: { price_contract_version: 'NB_PRICE_1', currency: 'CNY', unit: 'TOKEN_1M', rounding_profile: 'ORDER_CEIL_FEN_1', input_rate: { numerator_fen: 80, denominator_units: 1 }, output_rate: { numerator_fen: 160, denominator_units: 1 }, commission_bps: 0, channel_cost_owner: 'SELLER', fee_policy_version: 'fee_explicit_zero' },
  kernel_bindings: [], total_budget_fen: 500, seller_price_fen: 1, buyer_fee_fen: 0, deadline_sec: 600, same_seller_retries: 0, evidence_refs: [], contract_hash: 'a'.repeat(64), contract_schema_version: 'NB_CONTRACT_1' };
const order: Order = { order_id: 'ord_original', buyer_id: 'usr_buyer', seller_id: 'usr_seller', version: 1, state: 'WAITING_PAYMENT', contract, created_at: '2026-11-06T00:00:00Z', updated_at: '2026-11-06T00:00:00Z' };
const payment = (patch: Partial<Payment> = {}): Payment => ({ payment_id: 'pay_original', order_id: order.order_id, seller_id: order.seller_id, version: 2, state: 'PENDING', environment: 'SANDBOX', amount_fen: 500, settled_fen: 0, channel_action_ref: 'amt_private_reference', updated_at: '2026-11-06T00:00:00Z', ...patch });
const options = { order_id: order.order_id, seller_id: order.seller_id, contract_hash: contract.contract_hash, expected_order_version: 1, amount_fen: 500, currency: 'CNY', environment: 'SANDBOX', items: [{ channel_capability_ref: 'pcap_original', goods_name: '合成原个人卖家服务' }] };
const recovery = { recovery_id: 'mpr_original', payment_id: 'pay_original', state: 'PENDING', expires_at: '2099-01-01T00:00:00Z' };
const challengeBody = { error: { type: 'payment_required', code: 'payment_required', message: 'Original payment required', param: null }, payment_id: 'pay_original', amount: '5.00', currency: 'CNY', pay_before: '2099-01-01T00:00:00Z' };
const encodedProof = Buffer.from(JSON.stringify({ protocol: { payment_proof: 'p'.repeat(64), trade_no: '7'.repeat(32) }, method: { client_session: 'synthetic_checkout_private_session' } })).toString('base64');
const reply = (data: unknown, status = 200, headers: Record<string,string> = {}): Response => ({ status, data, headers });
const missing = () => reply({ error: { code: 'NOT_FOUND' } }, 404);
const challenge = () => reply(challengeBody, 402, { amtPaymentLink: 'amt_private_reference' });
function memory() { const values = new Map<string,string>(); const storage: Storage = { get: k => values.get(k) ?? null, set: (k,v) => { values.set(k,v); }, remove: k => { values.delete(k); } }; return { values, storage }; }
function client(transport: Transport) { const c = new ControlClient('', transport, 'WEB'); c.setSession(session); return c; }
const secure = () => { let count = 0; return async (n: number) => new Uint8Array(n).fill(++count); };
const nextTurn = () => new Promise<void>(resolve => setImmediate(resolve));

test('CK-UI-001 lost intent restores identical original key without another choice or RNG', async () => {
  const store = memory(), requests: { key: string; body?: string }[] = []; let fail = true;
  const c = client(async request => {
    if (request.url.endsWith('/payment-options')) return reply(options);
    if (request.url.endsWith('/payment') || request.url.endsWith('/proof-recovery')) return missing();
    if (request.url.endsWith('/payment-intents')) {
      const raw = [...store.values.values()][0]; assert.ok(raw, 'persist before financial intent I/O');
      requests.push({ key: request.headers['Idempotency-Key'], body: request.body });
      if (fail) { fail = false; throw new Error('synthetic lost intent response'); }
      return reply(payment(), 201);
    }
    throw new Error('unexpected operation');
  });
  const first = new PaymentFlow(c, store.storage, secure()); first.select(order); await first.refresh(); await first.prepare('pcap_original'); assert.match(first.snapshot().error, /尚未确认/); first.dispose();
  const restored = new PaymentFlow(c, store.storage, async () => { throw new Error('must reuse original keys'); });
  restored.select(order); await restored.refresh(); assert.equal(restored.snapshot().originalCreatePending, true); await restored.prepare();
  assert.deepEqual(requests[1], requests[0]); assert.equal(restored.snapshot().payment?.payment_id, 'pay_original');
  assert.equal(restored.snapshot().payment?.channel_action_ref, undefined); restored.dispose();
});
test('CK-UI-002 unavailable durable journal blocks the intent before transport', async () => {
  let writes = 0; const c = client(async r => { if (r.method !== 'GET') writes++; return r.url.endsWith('/payment-options') ? reply(options) : missing(); });
  const flow = new PaymentFlow(c, { get: () => null, set: () => { throw new Error('原请求不能安全保存'); }, remove: () => {} }, secure());
  flow.select(order); await flow.refresh(); await flow.prepare('pcap_original'); assert.equal(writes, 0); assert.match(flow.snapshot().error, /不能安全保存/); flow.dispose();
});
test('CK-UI-003 changed seller or original amount never becomes a payable UI projection', async () => {
  for (const patch of [{ seller_id: 'usr_other' }, { amount_fen: 499 }]) {
    const store = memory(), c = client(async r => r.method === 'POST' ? reply(payment(patch), 201) : r.url.endsWith('/payment-options') ? reply(options) : missing());
    const flow = new PaymentFlow(c, store.storage, secure()); flow.select(order); await flow.refresh(); await flow.prepare('pcap_original');
    assert.equal(flow.snapshot().payment, null); assert.match(flow.snapshot().error, /停止后续操作/); assert.equal(store.values.size, 1); flow.dispose();
  }
});
test('CK-UI-004 proof and prepay reference are redacted one-use memory values', async () => {
  const proof = new PaymentProof(encodedProof), ch = new PaymentChallenge(challengeBody, { AMTPAYMENTLINK: 'amt_private_reference' });
  assert.equal(JSON.stringify(proof), '{"redacted":true}'); assert.equal(JSON.stringify(ch), '{"redacted":true}');
  assert.equal(ch.useReference(), 'amt_private_reference'); assert.throws(() => ch.useReference());
  assert.equal(await proof.use(async header => header === encodedProof), true); await assert.rejects(proof.use(async () => true));
  assert.doesNotMatch(String(proof), /synthetic_checkout_private_session/); ch.close();
});
test('CK-UI-005 ambiguous, expired and URL-shaped 402 references fail closed', () => {
  for (const headers of [{ amtPaymentLink: 'https://untrusted.invalid/pay' }, { amtPaymentLink: 'amt_a', AMTPAYMENTLINK: 'amt_b' }, {}]) assert.throws(() => new PaymentChallenge(challengeBody, headers));
  assert.throws(() => new PaymentChallenge({ ...challengeBody, pay_before: '2000-01-01T00:00:00Z' }, { amtPaymentLink: 'amt_a' }));
  assert.throws(() => new PaymentChallenge({ ...challengeBody, amount: '5e0' }, { amtPaymentLink: 'amt_a' }));
});
test('CK-UI-006 explicit launcher submits one header and cannot turn its callback into paid', async () => {
  const store = memory(); let created = false, submitted = false, launches = 0;
  const c = client(async r => {
    if (r.url.endsWith('/payment-options')) return reply(options);
    if (r.url.endsWith('/payment-intents')) { created = true; return reply(payment(), 201); }
    if (r.url.endsWith('/payment')) return created ? reply(payment()) : missing();
    if (r.url.endsWith('/challenge')) return challenge();
    if (r.url.endsWith('/proofs')) { assert.equal(r.body, '{}'); assert.equal(r.headers['Payment-Proof'], encodedProof); assert.equal(r.headers['X-CSRF-Token'], session.csrf_token); submitted = true; return reply(recovery, 202); }
    if (r.url.endsWith('/proof-recovery')) return submitted ? reply(recovery) : missing();
    throw new Error('unexpected execution or billing');
  });
  const launcher: PayLauncher = { supports: env => env === 'SANDBOX', async launch(original, ch) { launches++; assert.equal(original.seller, 'usr_seller'); assert.equal(original.contractHash, contract.contract_hash); assert.equal(ch.useReference(), 'amt_private_reference'); return new PaymentProof(encodedProof); } };
  const flow = new PaymentFlow(c, store.storage, secure(), launcher); flow.select(order); await flow.refresh(); await flow.prepare('pcap_original'); await flow.launch();
  assert.equal(submitted, true); assert.equal(launches, 1); assert.equal(flow.snapshot().payment?.state, 'PENDING'); assert.equal(flow.snapshot().recovery?.state, 'PENDING'); assert.equal(flow.snapshot().launchAvailable, false);
  for (const raw of [...store.values.values(), JSON.stringify(flow.snapshot())]) for (const secret of [encodedProof, 'synthetic_checkout_private_session', 'amt_private_reference', session.csrf_token!]) assert.ok(!raw.includes(secret));
  await flow.launch(); assert.equal(launches, 1); flow.dispose();
});
test('CK-UI-007 unknown wallet result persists original launch and never automatically retries', async () => {
  for (const throws of [false,true]) {
  const store = memory(); let created = false, launches = 0, proofPosts = 0;
  const c = client(async r => {
    if (r.url.endsWith('/payment-options')) return reply(options);
    if (r.url.endsWith('/payment-intents')) { created = true; return reply(payment(), 201); }
    if (r.url.endsWith('/payment')) return created ? reply(payment()) : missing();
    if (r.url.endsWith('/challenge')) return challenge();
    if (r.url.endsWith('/proofs')) proofPosts++;
    return missing();
  });
  const launcher: PayLauncher = { supports: () => true, async launch() { launches++; if (throws) throw new Error('synthetic_wallet_private_exception'); return null; } };
  const flow = new PaymentFlow(c, store.storage, secure(), launcher); flow.select(order); await flow.refresh(); await flow.prepare('pcap_original'); await flow.launch(); await flow.refresh(); await flow.launch();
  assert.equal(launches, 1); assert.equal(proofPosts, 0); assert.equal(flow.snapshot().payment?.state, 'PENDING'); assert.equal(flow.snapshot().launchAvailable, false);
  assert.ok(!JSON.stringify(flow.snapshot()).includes('synthetic_wallet_private_exception')); flow.dispose();
  }
});
test('CK-UI-008 late proof or null wallet callback cannot enter another session', async () => {
  for (const returnsProof of [false,true]) {
    const store = memory(); let created = false, proofPosts = 0; let finish!: (v: PaymentProof | null) => void;
    const c = client(async r => {
      if (r.url.endsWith('/payment-options')) return reply(options);
      if (r.url.endsWith('/payment-intents')) { created = true; return reply(payment(), 201); }
      if (r.url.endsWith('/payment')) return created ? reply(payment()) : missing();
      if (r.url.endsWith('/challenge')) return challenge();
      if (r.url.endsWith('/proofs')) proofPosts++;
      return missing();
    });
    const flow = new PaymentFlow(c, store.storage, secure(), { supports: () => true, launch: () => new Promise(resolve => { finish = resolve; }) });
    flow.select(order); await flow.refresh(); await flow.prepare('pcap_original'); const pending = flow.launch(); await nextTurn();
    c.setSession({ ...session, session_id: 'ses_other', profile: { ...session.profile, user_id: 'usr_other' } });
    const proof = returnsProof ? new PaymentProof(encodedProof) : null; finish(proof); await pending;
    assert.equal(proofPosts, 0); assert.equal(flow.snapshot().order, ''); assert.equal(flow.snapshot().notice, '');
    if (proof) await assert.rejects(proof.use(async () => true)); flow.dispose();
  }
});
test('CK-UI-009 replaced session of the same owner discards old in-flight metadata', async () => {
  let finish!: (v: Response) => void; const c = client(() => new Promise(resolve => { finish = resolve; })), store = memory();
  const flow = new PaymentFlow(c, store.storage, secure()); flow.select(order); const pending = flow.refresh();
  c.setSession({ ...session, session_id: 'ses_replaced' }); finish(reply(payment())); await pending;
  assert.equal(flow.snapshot().order, ''); assert.equal(flow.snapshot().payment, null); assert.equal(store.values.size, 0); flow.dispose();
});
test('CK-UI-010 dedicated client methods reject wrong statuses and malformed proof before transport', async () => {
  let calls = 0; const c = client(async () => { calls++; return reply(recovery); });
  await assert.rejects(c.submitPaymentProof('../other', encodedProof, 'cmd_original', x => x));
  await assert.rejects(c.submitPaymentProof('pay_original', 'bad\r\nheader', 'cmd_original', x => x)); assert.equal(calls, 0);
  await assert.rejects(c.submitPaymentProof('pay_original', encodedProof, 'cmd_original', x => x), (e: unknown) => e instanceof ApiError && e.code === 'INVALID_RESPONSE' && e.uncertain);
  await assert.rejects(c.paymentChallenge('pay_original', x => x), (e: unknown) => e instanceof ApiError && e.code === 'INVALID_RESPONSE');
});
test('CK-UI-011 settled metadata does not create jobs, funding, keys or another charge', async () => {
  let mutations = 0; const c = client(async r => { if (r.method !== 'GET') mutations++; return r.url.endsWith('/proof-recovery') ? reply({ ...recovery, state: 'VERIFIED' }) : reply(payment({ state: 'SETTLED', settled_fen: 500, channel_reference: '7'.repeat(32), channel_action_ref: undefined })); });
  const flow = new PaymentFlow(c, memory().storage, secure()); flow.select(order); await flow.refresh();
  assert.equal(flow.snapshot().payment?.state, 'SETTLED'); assert.equal(order.state, 'WAITING_PAYMENT'); assert.equal(mutations, 0); assert.equal(flow.snapshot().launchAvailable, false); flow.dispose();
});
test('CK-UI-012 a retained original payment cannot launch without approved runtime and original journal', async () => {
  let reads = 0; const c = client(async r => { reads++; return r.url.endsWith('/proof-recovery') ? missing() : reply(payment()); });
  const flow = new PaymentFlow(c, memory().storage, secure()); flow.select(order); await flow.refresh(); const before = reads;
  assert.equal(flow.snapshot().launchAvailable, false); await flow.launch(); assert.equal(reads, before); assert.equal(flow.snapshot().payment?.state, 'PENDING'); flow.dispose();
});
