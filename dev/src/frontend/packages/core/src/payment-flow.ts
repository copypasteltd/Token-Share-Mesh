import { ApiError, ControlClient, errorText } from './client';
import { type Order, object } from './contracts';
import { newKey, type SecureBytes, type Storage } from './auth';
import { originalCheckout, paymentOf, paymentOptionsOf, proofRecoveryOf, PaymentChallenge, PaymentProof,
  type OriginalCheckout, type Payment, type PaymentOptions, type ProofRecovery, type PayLauncher } from './payment';

interface CheckoutRecord {
  profile: 'TSM_ORIGINAL_CHECKOUT_1'; owner: string; order: string; seller: string; contractHash: string;
  amount: number; version: number; capability: string; createKey: string; proofKey: string;
  payment?: string; launchRequested: boolean;
}
export interface PaymentState {
  order: string; payment: Payment | null; options: PaymentOptions | null; recovery: ProofRecovery | null;
  busy: boolean; error: string; notice: string; launchAvailable: boolean; originalCreatePending: boolean;
}
const initial = (): PaymentState => ({ order: '', payment: null, options: null, recovery: null, busy: false, error: '', notice: '', launchAvailable: false, originalCreatePending: false });
const keyOf = (o: OriginalCheckout) => 'checkout:' + o.owner + ':' + o.order;
const safeId = (v: unknown): v is string => typeof v === 'string' && /^[A-Za-z0-9_-]{1,128}$/.test(v);
const commandKey = (v: unknown): v is string => typeof v === 'string' && /^cmd_[A-Za-z0-9_-]{1,120}$/.test(v);

/** One original buyer/order at a time; no timers, auto charge, model call,
 * cross-seller selection or credential persistence. It owns no SDK authority. */
export class PaymentFlow {
  private state = initial();
  private original: OriginalCheckout | null = null;
  private epoch = 0;
  private listeners = new Set<() => void>();
  private unsubscribe: () => void;
  private proof: PaymentProof | null = null;
  private challenge: PaymentChallenge | null = null;
  constructor(private client: ControlClient, private storage: Storage, private random: SecureBytes,
    private launcher?: PayLauncher, private authFailed?: (e: ApiError) => void) {
    this.unsubscribe = client.onSessionChange(() => { this.invalidate(); });
  }
  subscribe = (listener: () => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; };
  snapshot = () => this.state;
  private update(patch: Partial<PaymentState>): void { this.state = { ...this.state, ...patch }; this.listeners.forEach(fn => fn()); }
  private invalidate(): void {
    this.epoch++; this.original = null; this.proof?.close(); this.challenge?.close(); this.proof = null; this.challenge = null;
    this.state = initial(); this.listeners.forEach(fn => fn());
  }
  dispose(): void { this.unsubscribe(); this.invalidate(); this.listeners.clear(); }
  select(order: Order | null): void {
    if (!order) { this.invalidate(); return; }
    const owner = this.client.current()?.profile.user_id;
    if (!owner) { this.invalidate(); return; }
    let next: OriginalCheckout;
    try { next = originalCheckout(order, owner); }
    catch (error) { this.invalidate(); this.update({ error: errorText(error) }); return; }
    const old = this.original;
    if (old && old.owner === next.owner && old.order === next.order && old.contractHash === next.contractHash && old.version === next.version) return;
    this.invalidate(); this.original = next; this.update({ order: next.order });
  }
  private current(epoch: number, original: OriginalCheckout): void {
    if (epoch !== this.epoch || this.original !== original || this.client.current()?.profile.user_id !== original.owner) throw new ApiError('CHECKOUT_CHANGED');
  }
  private record(o: OriginalCheckout): CheckoutRecord | null {
    const raw = this.storage.get(keyOf(o)); if (!raw) return null;
    if (raw.length > 4096) throw new Error('原付款恢复记录需核对，请保留记录并联系支持。');
    const r = object(JSON.parse(raw));
    if (Object.keys(r).some(k => !['profile','owner','order','seller','contractHash','amount','version','capability','createKey','proofKey','payment','launchRequested'].includes(k)) ||
      r.profile !== 'TSM_ORIGINAL_CHECKOUT_1' || r.owner !== o.owner || r.order !== o.order || r.seller !== o.seller || r.contractHash !== o.contractHash || r.amount !== o.amount ||
      !Number.isSafeInteger(r.version) || Number(r.version) < 1 || Number(r.version) > o.version || !safeId(r.capability) || !commandKey(r.createKey) || !commandKey(r.proofKey) ||
      (r.payment !== undefined && !safeId(r.payment)) || typeof r.launchRequested !== 'boolean') throw new Error('原付款恢复记录需核对，请保留记录并联系支持。');
    return r as unknown as CheckoutRecord;
  }
  private save(o: OriginalCheckout, record: CheckoutRecord): void {
    // Safe original metadata only. Never write Payment-Proof, client_session,
    // prepay references, auth tokens, challenge bodies or wallet callback data.
    this.storage.set(keyOf(o), JSON.stringify(record));
  }
  private boundPayment(raw: unknown, o: OriginalCheckout, expected?: string): Payment {
    const p = paymentOf(raw);
    if (p.order_id !== o.order || p.seller_id !== o.seller || p.amount_fen !== o.amount || expected && p.payment_id !== expected) throw new ApiError('INVALID_RESPONSE');
    // The prepay ref has a separate one-use in-memory challenge container.
    const { channel_action_ref: _reference, ...safe } = p; return safe;
  }
  private boundRecovery(raw: unknown, payment: string): ProofRecovery { const r = proofRecoveryOf(raw); if (r.payment_id !== payment) throw new ApiError('INVALID_RESPONSE'); return r; }
  private available(p: Payment, r: CheckoutRecord | null): boolean {
    return Boolean(p.state === 'PENDING' && r && r.payment === p.payment_id && !r.launchRequested && this.launcher?.supports(p.environment));
  }
  private async run(action: (o: OriginalCheckout, epoch: number) => Promise<void>): Promise<void> {
    if (this.state.busy || !this.original) return;
    const o = this.original, epoch = this.epoch; this.update({ busy: true, error: '' });
    try { this.current(epoch, o); await action(o, epoch); }
    catch (e) {
      if (epoch !== this.epoch) return;
      if (e instanceof ApiError && e.status === 401) { this.authFailed?.(e); if (epoch === this.epoch) this.client.setSession(null); return; }
      this.update({ error: e instanceof ApiError && e.code === 'CHECKOUT_CHANGED' ? '付款对象已变化，请重新核对原订单。' : errorText(e) });
    } finally { if (epoch === this.epoch) this.update({ busy: false }); }
  }
  private async read(o: OriginalCheckout, epoch: number): Promise<void> {
    let p: Payment;
    try { p = await this.client.request('GET', '/orders/' + o.order + '/payment', raw => this.boundPayment(raw, o)); }
    catch (e) {
      this.current(epoch, o);
      if (!(e instanceof ApiError) || e.status !== 404) throw e;
      const record = this.record(o);
      if (record?.payment) throw new ApiError('INVALID_RESPONSE');
      if (record) {
        this.update({ payment: null, options: null, recovery: null, originalCreatePending: true, launchAvailable: false,
          notice: '原账单创建结果尚未确认，只能恢复这一次原请求。' });
        return;
      }
      const options = await this.client.request('GET', '/orders/' + o.order + '/payment-options', paymentOptionsOf);
      this.current(epoch, o);
      if (options.order_id !== o.order || options.seller_id !== o.seller || options.contract_hash !== o.contractHash || options.amount_fen !== o.amount || options.expected_order_version !== o.version) throw new ApiError('INVALID_RESPONSE');
      this.update({ payment: null, options, recovery: null, originalCreatePending: false, launchAvailable: false,
        notice: '请核对原卖家、原合同和金额，再选择付款方式。' });
      return;
    }
    this.current(epoch, o);
    const record = this.record(o);
    if (record?.payment && record.payment !== p.payment_id) throw new ApiError('INVALID_RESPONSE');
    if (record && !record.payment) { record.payment = p.payment_id; this.save(o, record); }
    this.update({ payment: p, options: null, originalCreatePending: p.state === 'CREATED' && Boolean(record), launchAvailable: this.available(p, record),
      notice: p.state === 'SETTLED' ? '原付款已由服务端核验；订单资格和任务状态仍需另行确认。' : '请保留原付款号。未确认结果前不要另建一笔付款。' });
    let recovery: ProofRecovery | null = null;
    try { recovery = await this.client.request('GET', '/payments/' + p.payment_id + '/proof-recovery', raw => this.boundRecovery(raw, p.payment_id)); }
    catch (e) { if (!(e instanceof ApiError) || e.status !== 404) throw e; }
    this.current(epoch, o); this.update({ recovery });
  }
  refresh = async (): Promise<void> => this.run((o, epoch) => this.read(o, epoch));
  prepare = async (capability?: string): Promise<void> => this.run(async (o, epoch) => {
    let record = this.record(o);
    if (this.state.payment && this.state.payment.state !== 'CREATED') { await this.read(o, epoch); return; }
    if (!record) {
      const options = this.state.options;
      if (!options || !capability || !options.items.some(item => item.channel_capability_ref === capability)) throw new Error('请先核对并选择当前付款方式。');
      const createKey = await newKey(this.random), proofKey = await newKey(this.random); this.current(epoch, o);
      record = { profile: 'TSM_ORIGINAL_CHECKOUT_1', ...o, capability, createKey, proofKey, launchRequested: false };
      this.save(o, record); // Persist original recovery keys BEFORE intent I/O.
    } else if (capability && capability !== record.capability) throw new Error('已有原付款请求，请先恢复，不能更换付款方式。');
    this.current(epoch, o);
    const p = await this.client.request('POST', '/orders/' + o.order + '/payment-intents', raw => this.boundPayment(raw, o, record?.payment),
      { expected_order_version: record.version, channel_capability_ref: record.capability }, record.createKey);
    this.current(epoch, o); record.payment = p.payment_id; this.save(o, record);
    this.update({ payment: p, options: null, originalCreatePending: p.state === 'CREATED', launchAvailable: this.available(p, record), notice: '原账单已建立，尚未因此取得执行资格。' });
  });
  launch = async (): Promise<void> => this.run(async (o, epoch) => {
    const p = this.state.payment, launcher = this.launcher;
    if (!p || p.state !== 'PENDING' || !launcher?.supports(p.environment)) throw new Error('当前环境暂不支持发起这笔付款，请保留原付款号。');
    const record = this.record(o);
    if (record?.launchRequested) throw new Error('原付款操作结果待核对，请查询原付款，不要重复发起。');
    // A payment created by another authorized client can be read, but this
    // client lacks its creation journal. Do not invent recovery keys or charge.
    if (!record || record.payment !== p.payment_id) throw new Error('请从原付款发起端继续操作，或核对原付款记录。');
    const challenge = await this.client.paymentChallenge(p.payment_id, (raw, headers) => new PaymentChallenge(raw, headers));
    try {
      this.current(epoch, o); this.challenge = challenge;
      if (challenge.payment !== p.payment_id || challenge.amount !== o.amount) throw new ApiError('INVALID_RESPONSE');
      record.launchRequested = true; this.save(o, record); this.update({ launchAvailable: false });
      let proof: PaymentProof | null;
      try { proof = await launcher.launch(Object.freeze({ ...o, payment: p.payment_id, environment: p.environment }), challenge); }
      catch { throw new ApiError('TRANSPORT_UNKNOWN', 0, true); } // Never expose a wallet exception/credential to UI.
      if (proof !== null && !(proof instanceof PaymentProof)) throw new ApiError('INVALID_RESPONSE', 0, true);
      if (epoch !== this.epoch || this.client.current()?.profile.user_id !== o.owner) { proof?.close(); return; }
      if (!proof) { this.update({ notice: '付款操作结果尚未确认，请核对原付款。' }); return; }
      this.proof = proof; this.current(epoch, o);
      const recovery = await proof.use(header => this.client.submitPaymentProof(p.payment_id, header, record.proofKey, raw => this.boundRecovery(raw, p.payment_id)));
      this.current(epoch, o); this.update({ recovery, notice: '凭据已提交，正在核对原付款；尚未开始执行。' });
      await this.read(o, epoch);
    } finally {
      challenge.close();
      if (epoch === this.epoch) { this.proof?.close(); this.proof = null; this.challenge = null; }
    }
  });
}
