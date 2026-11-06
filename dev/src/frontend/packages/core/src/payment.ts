import { object, text, safeInteger, type Order, contractOf } from './contracts';
import { ApiError } from './client';

const entity = (v: unknown): string => { const s = text(v, 128); if (!/^[A-Za-z0-9_-]+$/.test(s)) throw new Error('INVALID_RESPONSE'); return s; };
const hash = (v: unknown): string => { const s = text(v, 64); if (!/^[a-f0-9]{64}$/.test(s)) throw new Error('INVALID_RESPONSE'); return s; };
const instant = (v: unknown): string => { const s = text(v, 40); if (!/^\d{4}-\d\d-\d\dT/.test(s) || !Number.isFinite(Date.parse(s))) throw new Error('INVALID_RESPONSE'); return s; };
const exact = (v: Record<string, unknown>, fields: string[]) => { if (Object.keys(v).some(key => !fields.includes(key))) throw new Error('INVALID_RESPONSE'); };
export type PaymentEnvironment = 'SANDBOX' | 'PRODUCTION';
export interface Payment {
  payment_id: string; order_id: string; seller_id: string; version: number; state: 'CREATED' | 'PENDING' | 'AUTHORIZED' | 'SETTLED' | 'FAILED' | 'UNKNOWN' | 'CLOSED';
  environment: PaymentEnvironment; amount_fen: number; settled_fen: number; channel_reference?: string; channel_action_ref?: string; updated_at: string;
}
export interface PaymentOption { channel_capability_ref: string; goods_name: string }
export interface PaymentOptions { order_id: string; seller_id: string; contract_hash: string; expected_order_version: number; amount_fen: number; currency: 'CNY'; environment: PaymentEnvironment; items: PaymentOption[] }
export interface ProofRecovery { recovery_id: string; payment_id: string; state: 'PENDING' | 'VERIFYING' | 'VERIFIED' | 'UNKNOWN' | 'RETIRED'; expires_at: string }
export interface OriginalCheckout { owner: string; order: string; seller: string; contractHash: string; amount: number; version: number }

function environment(v: unknown): PaymentEnvironment { if (v !== 'SANDBOX' && v !== 'PRODUCTION') throw new Error('INVALID_RESPONSE'); return v; }
export function originalCheckout(o: Order, owner: string): OriginalCheckout {
  const c = contractOf(o.contract);
  if (o.buyer_id !== owner || o.seller_id !== c.seller_id || owner === o.seller_id) throw new ApiError('FORBIDDEN', 403);
  entity(owner); entity(o.order_id); entity(o.seller_id); hash(c.contract_hash);
  safeInteger(o.version, 1); safeInteger(c.total_budget_fen, 1);
  return { owner, order: o.order_id, seller: o.seller_id, contractHash: c.contract_hash, amount: c.total_budget_fen, version: o.version };
}
export function paymentOf(raw: unknown): Payment {
  const v = object(raw); exact(v, ['payment_id','order_id','seller_id','version','state','environment','amount_fen','settled_fen','channel_reference','channel_action_ref','updated_at']);
  entity(v.payment_id); entity(v.order_id); entity(v.seller_id); safeInteger(v.version, 1); environment(v.environment); instant(v.updated_at);
  if (!['CREATED','PENDING','AUTHORIZED','SETTLED','FAILED','UNKNOWN','CLOSED'].includes(String(v.state)) || safeInteger(v.amount_fen, 1) > 5000 || safeInteger(v.settled_fen) > Number(v.amount_fen)) throw new Error('INVALID_RESPONSE');
  if ((v.state === 'SETTLED' ? v.settled_fen !== v.amount_fen : v.settled_fen !== 0) || v.state === 'AUTHORIZED') throw new Error('INVALID_RESPONSE');
  if (v.channel_reference !== undefined && (typeof v.channel_reference !== 'string' || !/^\d{32}$/.test(v.channel_reference))) throw new Error('INVALID_RESPONSE');
  if (v.state === 'SETTLED' && v.channel_reference === undefined) throw new Error('INVALID_RESPONSE');
  if (v.channel_action_ref !== undefined && (typeof v.channel_action_ref !== 'string' || !/^[A-Za-z0-9_]{1,128}$/.test(v.channel_action_ref))) throw new Error('INVALID_RESPONSE');
  return v as unknown as Payment;
}
export function paymentOptionsOf(raw: unknown): PaymentOptions {
  const v = object(raw); exact(v, ['order_id','seller_id','contract_hash','expected_order_version','amount_fen','currency','environment','items']);
  entity(v.order_id); entity(v.seller_id); hash(v.contract_hash); safeInteger(v.expected_order_version, 1); environment(v.environment);
  if (safeInteger(v.amount_fen, 1) > 5000 || v.currency !== 'CNY' || !Array.isArray(v.items) || v.items.length < 1 || v.items.length > 8) throw new Error('INVALID_RESPONSE');
  const ids = new Set<string>();
  for (const rawItem of v.items) { const i = object(rawItem); exact(i, ['channel_capability_ref','goods_name']); const id = entity(i.channel_capability_ref); text(i.goods_name, 128); if (ids.has(id)) throw new Error('INVALID_RESPONSE'); ids.add(id); }
  return v as unknown as PaymentOptions;
}
export function proofRecoveryOf(raw: unknown): ProofRecovery {
  const v = object(raw); exact(v, ['recovery_id','payment_id','state','expires_at']); entity(v.recovery_id); entity(v.payment_id); instant(v.expires_at);
  if (!['PENDING','VERIFYING','VERIFIED','UNKNOWN','RETIRED'].includes(String(v.state))) throw new Error('INVALID_RESPONSE'); return v as unknown as ProofRecovery;
}

/** Credential object is memory-only. Clearing references is not forensic erasure
 * of JS strings, native bridge buffers, browser history or external wallet data. */
export class PaymentProof {
  private encoded: string;
  constructor(encoded: string) {
    if (typeof encoded !== 'string' || encoded.length < 4 || encoded.length > 16384 || encoded.length % 4 !== 0 || !/^[A-Za-z0-9+/]+={0,2}$/.test(encoded)) throw new ApiError('INVALID_REQUEST');
    this.encoded = encoded;
  }
  async use<T>(consumer: (header: string) => Promise<T>): Promise<T> {
    if (!this.encoded) throw new ApiError('INVALID_REQUEST');
    const value = this.encoded; this.encoded = '';
    try { return await consumer(value); } finally { this.close(); }
  }
  close(): void { this.encoded = ''; }
  toJSON(): object { return { redacted: true }; }
  toString(): string { return '[payment proof redacted]'; }
}

/** The prepay reference is not a URL or a miniapp trade_no. It must only be
 * passed to an independently approved actual Machine Pay client integration. */
export class PaymentChallenge {
  private reference: string;
  readonly payment: string; readonly amount: number; readonly payBefore: string;
  constructor(raw: unknown, headers: Record<string, string>) {
    const v = object(raw); exact(v, ['error','payment_id','amount','currency','pay_before']);
    const e = object(v.error); exact(e, ['type','code','message','param']);
    if (e.type !== 'payment_required' || e.code !== 'payment_required' || e.param !== null || typeof v.amount !== 'string' || !/^(?:0|[1-9]\d?)\.\d{2}$/.test(v.amount) || v.currency !== 'CNY') throw new ApiError('INVALID_RESPONSE');
    text(e.message, 256); this.payment = entity(v.payment_id); this.payBefore = instant(v.pay_before);
    const parts = v.amount.split('.'); this.amount = Number(parts[0]) * 100 + Number(parts[1]);
    const refs = Object.entries(headers).filter(([name]) => name.toLowerCase() === 'amtpaymentlink');
    if (refs.length !== 1 || !/^[A-Za-z0-9_]{1,128}$/.test(refs[0][1]) || this.amount < 1 || this.amount > 5000 || Date.parse(this.payBefore) <= Date.now()) throw new ApiError('INVALID_RESPONSE');
    this.reference = refs[0][1];
  }
  useReference(): string { if (!this.reference || Date.parse(this.payBefore) <= Date.now()) throw new ApiError('INVALID_RESPONSE'); const v = this.reference; this.reference = ''; return v; }
  close(): void { this.reference = ''; }
  toJSON(): object { return { redacted: true }; }
  toString(): string { return '[original payment challenge redacted]'; }
}
export interface PayLauncher {
  supports(environment: PaymentEnvironment): boolean;
  launch(original: Readonly<OriginalCheckout & { payment: string; environment: PaymentEnvironment }>, challenge: PaymentChallenge): Promise<PaymentProof | null>;
}
export const paymentStateLabel = (state: Payment['state']): string => ({ CREATED: '账单已建立', PENDING: '等待付款核对', AUTHORIZED: '授权状态待核对', SETTLED: '服务端已核验实付', FAILED: '付款未完成', UNKNOWN: '原付款结果待核对', CLOSED: '原付款已关闭' })[state];
export const recoveryStateLabel = (state: ProofRecovery['state']): string => ({ PENDING: '原凭据已提交', VERIFYING: '正在核对原凭据', VERIFIED: '原凭据已核验', UNKNOWN: '原凭据结果待核对', RETIRED: '原凭据已到期退役' })[state];
