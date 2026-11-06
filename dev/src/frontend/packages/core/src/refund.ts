import { object, type Order } from './contracts';
import { ControlClient, ApiError, errorText } from './client';
import { newKey, type SecureBytes, type Storage } from './auth';

export const refundReasons = { NO_LONGER_NEEDED: '不再需要剩余服务', SERVICE_UNAVAILABLE: '服务无法继续使用', BILLING_REVIEW: '申请核对剩余价款' } as const;
export type RefundReason = keyof typeof refundReasons;
export interface RefundApplication {
  refund_id: string; order_id: string; payment_id: string; version: number;
  state: 'REQUESTED' | 'PROCESSING' | 'UNKNOWN' | 'SUCCEEDED' | 'FAILED'; requested_fen: number; confirmed_fen: number;
  updated_at: string; review_state: 'PENDING' | 'APPROVED' | 'REJECTED' | 'REVOKED'; review_version: number;
  reason_code: RefundReason; review_reason_code?: string; review_required: boolean;
}
export interface RefundReview extends RefundApplication {
  buyer_id: string; seller_id: string; order_version: number; payment_version: number;
  evidence_refs: string[]; refund_route_refs: string[]; collected_fen: number; held_refund_fen: number; maximum_refundable_fen: number;
}
interface RefundPage { items: RefundApplication[]; next_cursor: string | null }
export interface RefundState {
  order: string; items: RefundApplication[]; cursor: string | null; queue: RefundApplication[]; queueCursor: string | null;
  review: RefundReview | null; busy: boolean; pending: boolean; error: string; notice: string;
}
type Pending = { profile: 'TSM_REFUND_COMMAND_1'; owner: string; path: string; key: string; body: Record<string, unknown> };
const id = (v: unknown): v is string => typeof v === 'string' && /^[A-Za-z][A-Za-z0-9_-]{2,127}$/.test(v);
const integer = (v: unknown, min = 0, max = 2147483647): v is number => typeof v === 'number' && Number.isSafeInteger(v) && v >= min && v <= max;
const exact = (v: Record<string, unknown>, keys: readonly string[]) => { if (Object.keys(v).some(k => !keys.includes(k))) throw new ApiError('INVALID_RESPONSE'); };
const ids = (v: unknown, max = 50): string[] => { if (!Array.isArray(v) || v.length > max || v.some(x => !id(x)) || new Set(v).size !== v.length) throw new ApiError('INVALID_RESPONSE'); return [...v]; };
const baseKeys = ['refund_id','order_id','payment_id','version','state','requested_fen','confirmed_fen','updated_at','review_state','review_version','reason_code','review_reason_code','review_required'];
export function refundApplicationOf(value: unknown): RefundApplication {
  const v = object(value); exact(v, baseKeys);
  if (!id(v.refund_id) || !id(v.order_id) || !id(v.payment_id) || !integer(v.version, 1) || !integer(v.review_version, 1) ||
    typeof v.state !== 'string' || !['REQUESTED','PROCESSING','UNKNOWN','SUCCEEDED','FAILED'].includes(v.state) || typeof v.review_state !== 'string' || !['PENDING','APPROVED','REJECTED','REVOKED'].includes(v.review_state) ||
    !integer(v.requested_fen, 1, 5000) || !integer(v.confirmed_fen, 0, 5000) || (v.state === 'SUCCEEDED' ? v.confirmed_fen !== v.requested_fen : v.confirmed_fen !== 0) ||
    typeof v.updated_at !== 'string' || !/^\d{4}-\d\d-\d\dT/.test(v.updated_at) || !Number.isFinite(Date.parse(v.updated_at)) ||
    typeof v.reason_code !== 'string' || !Object.prototype.hasOwnProperty.call(refundReasons, v.reason_code) || typeof v.review_required !== 'boolean' ||
    (v.review_reason_code !== undefined && !id(v.review_reason_code)) || (['PENDING','REJECTED'].includes(String(v.review_state)) && v.state !== 'REQUESTED')) throw new ApiError('INVALID_RESPONSE');
  return { ...v } as unknown as RefundApplication;
}
export function refundReviewOf(value: unknown): RefundReview {
  const v = object(value), extra = ['buyer_id','seller_id','order_version','payment_version','evidence_refs','refund_route_refs','collected_fen','held_refund_fen','maximum_refundable_fen']; exact(v, [...baseKeys, ...extra]);
  const base = refundApplicationOf(Object.fromEntries(Object.entries(v).filter(([key]) => baseKeys.includes(key))));
  if (!id(v.buyer_id) || !id(v.seller_id) || v.buyer_id === v.seller_id || !integer(v.order_version, 1) || !integer(v.payment_version, 1) ||
    !integer(v.collected_fen, 1, 5000) || !integer(v.held_refund_fen, 0, Number(v.collected_fen)) || !integer(v.maximum_refundable_fen, 0, Number(v.collected_fen))) throw new ApiError('INVALID_RESPONSE');
  return { ...base, buyer_id: v.buyer_id, seller_id: v.seller_id, order_version: Number(v.order_version), payment_version: Number(v.payment_version),
    evidence_refs: ids(v.evidence_refs), refund_route_refs: ids(v.refund_route_refs, 128), collected_fen: Number(v.collected_fen), held_refund_fen: Number(v.held_refund_fen), maximum_refundable_fen: Number(v.maximum_refundable_fen) };
}
function pageOf(value: unknown): RefundPage {
  const v = object(value); exact(v, ['items','next_cursor']);
  if (!Array.isArray(v.items) || v.items.length > 50 || (v.next_cursor !== null && !id(v.next_cursor))) throw new ApiError('INVALID_RESPONSE');
  const items = v.items.map(refundApplicationOf); if (new Set(items.map(x => x.refund_id)).size !== items.length) throw new ApiError('INVALID_RESPONSE');
  return { items, next_cursor: v.next_cursor as string | null };
}
export function refundReviewLabel(v: RefundApplication): string {
  return ({ PENDING: '待独立审核，尚未提交退款', APPROVED: '已批准，请核对资金状态', REJECTED: '申请未获批准，未提交退款', REVOKED: '批准已撤销，请核对原退款义务' })[v.review_state];
}
export function refundFinancialLabel(v: RefundApplication): string {
  if (v.review_state === 'PENDING' || v.review_state === 'REJECTED') return '未创建渠道退款';
  return ({ REQUESTED: '退款意图已保存，尚未提交', PROCESSING: '已提交，等待原渠道确认', UNKNOWN: '渠道结果未确认，保留原退款号', SUCCEEDED: '原渠道已确认退款成功', FAILED: '需独立复核渠道失败事实' })[v.state];
}
const initial = (): RefundState => ({ order: '', items: [], cursor: null, queue: [], queueCursor: null, review: null, busy: false, pending: false, error: '', notice: '' });
function validateCommand(p: Pending): void {
  if (p.profile !== 'TSM_REFUND_COMMAND_1' || !id(p.owner) || !/^cmd_[A-Za-z0-9_-]{1,120}$/.test(p.key)) throw new Error('原售后记录需要核对，请保留记录。');
  const b = object(p.body), apply = /^\/orders\/([A-Za-z][A-Za-z0-9_-]{2,127})\/refunds$/.exec(p.path), decide = /^\/finance\/refund-reviews\/([A-Za-z][A-Za-z0-9_-]{2,127})\/decisions$/.exec(p.path), revoke = /^\/finance\/refund-reviews\/([A-Za-z][A-Za-z0-9_-]{2,127})\/revocations$/.exec(p.path);
  if (apply) {
    exact(b, ['expected_order_version','reason_code','requested_fen','evidence_refs']);
    if (!integer(b.expected_order_version, 1) || !integer(b.requested_fen, 1, 5000) || typeof b.reason_code !== 'string' || !Object.prototype.hasOwnProperty.call(refundReasons, b.reason_code)) throw new ApiError('INVALID_REQUEST');
    if (b.evidence_refs !== undefined) ids(b.evidence_refs); return;
  }
  if (decide) {
    exact(b, ['expected_review_version','expected_order_version','expected_payment_version','decision','review_reason_code','refund_route_ref','maximum_refundable_fen']);
    if (!integer(b.expected_review_version, 1, 2147483646) || !integer(b.expected_order_version, 1) || !integer(b.expected_payment_version, 1)) throw new ApiError('INVALID_REQUEST');
    if (b.decision === 'APPROVED' && b.review_reason_code === 'UNUSED_BALANCE_CONFIRMED' && id(b.refund_route_ref) && integer(b.maximum_refundable_fen, 1, 5000)) return;
    if (b.decision === 'REJECTED' && ['INSUFFICIENT_EVIDENCE','OUTSIDE_CONTRACT','DUPLICATE_APPLICATION','REQUIRES_FURTHER_REVIEW'].includes(String(b.review_reason_code)) && b.refund_route_ref === undefined && b.maximum_refundable_fen === undefined) return;
  }
  if (revoke) { exact(b, ['expected_review_version','review_reason_code']); if (integer(b.expected_review_version, 1, 2147483646) && b.review_reason_code === 'AUTHORITY_REVOKED') return; }
  throw new ApiError('INVALID_REQUEST');
}

/** Only safe aftersale metadata is journaled. Session/context changes clear
 * memory and reject late replies; UNKNOWN reuses the exact stored command. */
export class RefundFlow {
  private state = initial(); private epoch = 0; private order: Order | null = null; private listeners = new Set<() => void>(); private unsubscribe: () => void;
  constructor(private client: ControlClient, private storage: Storage, private random: SecureBytes) {
    this.unsubscribe = client.onSessionChange(() => { this.epoch++; this.order = null; this.state = initial(); this.emit(); });
  }
  subscribe = (fn: () => void) => { this.listeners.add(fn); return () => { this.listeners.delete(fn); }; };
  snapshot = () => this.state;
  private emit(): void { this.listeners.forEach(fn => fn()); }
  private update(patch: Partial<RefundState>): void { this.state = { ...this.state, ...patch }; this.emit(); }
  dispose(): void { this.unsubscribe(); this.epoch++; this.order = null; this.state = initial(); this.listeners.clear(); }
  select(order: Order | null): void {
    const user = this.client.current()?.profile.user_id;
    if (order && user !== order.buyer_id && user !== order.seller_id) order = null;
    if (order === this.order) return; this.epoch++; this.order = order; this.state = { ...initial(), order: order?.order_id || '' }; this.emit();
  }
  private current(epoch: number, owner: string): void { if (epoch !== this.epoch || this.client.current()?.profile.user_id !== owner) throw new ApiError('REFUND_CONTEXT_CHANGED'); }
  private pending(owner: string): Pending | null {
    const raw = this.storage.get('refund-pending:' + owner); if (!raw) return null;
    if (raw.length > 16384) throw new Error('原售后记录需要核对，请保留记录。');
    const p = object(JSON.parse(raw)); exact(p, ['profile','owner','path','key','body']); if (p.owner !== owner || typeof p.path !== 'string') throw new ApiError('INVALID_REQUEST');
    validateCommand(p as unknown as Pending); return p as unknown as Pending;
  }
  private async perform(work: (epoch: number, owner: string) => Promise<void>): Promise<void> {
    if (this.state.busy) return; const owner = this.client.current()?.profile.user_id; if (!owner) return;
    const epoch = this.epoch; this.update({ busy: true, error: '', notice: '' });
    try { this.update({ pending: !!this.pending(owner) }); await work(epoch, owner); }
    catch (error) { if (epoch === this.epoch) this.update({ error: errorText(error) }); }
    finally { if (epoch === this.epoch) this.update({ busy: false, pending: !!this.pendingSafely(owner) }); }
  }
  private pendingSafely(owner: string): boolean { try { return !!this.pending(owner); } catch { return true; } }
  async refresh(more = false): Promise<void> {
    const order = this.order; if (!order) return;
    await this.perform(async (epoch, owner) => {
      const cursor = more ? this.state.cursor : null; if (more && !cursor) return;
      const v = await this.client.request('GET', '/orders/' + order.order_id + '/refunds?limit=20' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : ''), pageOf); this.current(epoch, owner);
      if (v.items.some(x => x.order_id !== order.order_id)) throw new ApiError('INVALID_RESPONSE');
      this.update({ items: more ? this.merge(this.state.items, v.items) : v.items, cursor: v.next_cursor });
    });
  }
  private merge(old: RefundApplication[], incoming: RefundApplication[]): RefundApplication[] { const map = new Map(old.map(x => [x.refund_id, x])); incoming.forEach(x => map.set(x.refund_id, x)); return [...map.values()]; }
  private async command(epoch: number, owner: string, path: string, body: Record<string, unknown>): Promise<RefundApplication> {
    const candidate = { profile: 'TSM_REFUND_COMMAND_1' as const, owner, path, key: 'cmd_validation', body: JSON.parse(JSON.stringify(body)) as Record<string, unknown> }; validateCommand(candidate);
    let pending = this.pending(owner);
    if (pending && (pending.path !== path || JSON.stringify(pending.body) !== JSON.stringify(candidate.body))) throw new Error('存在待确认售后操作，请先恢复原操作。');
    if (!pending) {
      const key = await newKey(this.random); this.current(epoch, owner); pending = { ...candidate, key }; this.storage.set('refund-pending:' + owner, JSON.stringify(pending));
    }
    this.current(epoch, owner);
    try {
      const result = await this.client.request('POST', pending.path, refundApplicationOf, pending.body, pending.key); this.current(epoch, owner);
      const resource = pending.path.split('/');
      if ((resource[1] === 'orders' && result.order_id !== resource[2]) || (resource[1] === 'finance' && result.refund_id !== resource[3])) throw new ApiError('INVALID_RESPONSE', 200, true);
      this.storage.remove('refund-pending:' + owner); return result;
    } catch (error) {
      if (epoch === this.epoch && error instanceof ApiError && error.status >= 400 && error.status < 500 && !error.uncertain && ![401,403,429].includes(error.status)) this.storage.remove('refund-pending:' + owner);
      throw error;
    }
  }
  async apply(reason: RefundReason, requested: number): Promise<void> {
    const order = this.order; if (!order || this.client.current()?.profile.user_id !== order.buyer_id) return;
    await this.perform(async (epoch, owner) => { const v = await this.command(epoch, owner, '/orders/' + order.order_id + '/refunds', { expected_order_version: order.version, reason_code: reason, requested_fen: requested, evidence_refs: [] }); this.update({ items: this.merge(this.state.items, [v]), notice: '申请已保存，等待独立审核；尚未提交渠道退款。' }); });
  }
  async reviews(more = false): Promise<void> {
    await this.perform(async (epoch, owner) => { const cursor = more ? this.state.queueCursor : null; if (more && !cursor) return;
      const v = await this.client.request('GET', '/finance/refund-reviews?limit=20' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : ''), pageOf); this.current(epoch, owner); this.update({ queue: more ? this.merge(this.state.queue, v.items) : v.items, queueCursor: v.next_cursor }); });
  }
  async openReview(refund: string): Promise<void> {
    if (!id(refund)) return;
    await this.perform(async (epoch, owner) => { const v = await this.client.request('GET', '/finance/refund-reviews/' + refund, refundReviewOf); this.current(epoch, owner); if (v.refund_id !== refund || [v.buyer_id,v.seller_id].includes(owner)) throw new ApiError('INVALID_RESPONSE'); this.update({ review: v }); });
  }
  async decide(approve: boolean, route = '', maximum = 0): Promise<void> {
    const review = this.state.review; if (!review || review.review_state !== 'PENDING') return;
    await this.perform(async (epoch, owner) => { const body = { expected_review_version: review.review_version, expected_order_version: review.order_version, expected_payment_version: review.payment_version,
      decision: approve ? 'APPROVED' : 'REJECTED', review_reason_code: approve ? 'UNUSED_BALANCE_CONFIRMED' : 'INSUFFICIENT_EVIDENCE', ...(approve ? { refund_route_ref: route, maximum_refundable_fen: maximum } : {}) };
      const v = await this.command(epoch, owner, '/finance/refund-reviews/' + review.refund_id + '/decisions', body); this.update({ queue: this.merge(this.state.queue, [v]), review: null, notice: approve ? '批准及原退款意图已保存，资金结果等待原渠道确认。' : '申请未获批准，没有创建渠道退款。' }); });
  }
  async revoke(): Promise<void> {
    const review = this.state.review; if (!review || review.review_state !== 'APPROVED') return;
    await this.perform(async (epoch, owner) => { const v = await this.command(epoch, owner, '/finance/refund-reviews/' + review.refund_id + '/revocations', { expected_review_version: review.review_version, review_reason_code: 'AUTHORITY_REVOKED' }); this.update({ queue: this.merge(this.state.queue, [v]), review: null, notice: '批准已撤销。已提交或未知的原退款继续核对，金额不会自动释放。' }); });
  }
  async recover(): Promise<void> {
    await this.perform(async (epoch, owner) => { const pending = this.pending(owner); if (!pending) return;
      const v = await this.command(epoch, owner, pending.path, pending.body); this.update({ notice: '原售后操作已核对。请刷新对应申请。', ...(this.order?.order_id === v.order_id ? { items: this.merge(this.state.items,[v]) } : {}), queue: this.merge(this.state.queue,[v]), review: null }); });
  }
}
