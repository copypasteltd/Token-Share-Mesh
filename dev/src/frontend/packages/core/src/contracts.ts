/** Wire subset of the existing Go control handlers. This is not a second API or ledger. */
export type Mode = 'PLAINTEXT' | 'E2E' | 'GPU_RENTAL';
export type Privacy = 'STANDARD' | 'PLATFORM_BLIND';
export interface Rate { numerator_fen: number; denominator_units: number }
export interface Price {
  price_contract_version: 'NB_PRICE_1'; currency: 'CNY'; unit: 'AVAILABLE_HOUR' | 'TOKEN_1M';
  rounding_profile: 'ORDER_CEIL_FEN_1'; hour_rate?: Rate; input_rate?: Rate; output_rate?: Rate;
  commission_bps: number; channel_cost_owner: 'SELLER' | 'PLATFORM'; fee_policy_version: string;
}
export interface Policy {
  service_mode: Mode; privacy_profile: Privacy; region: string; protocol_profile_id: string;
  acceptance_policy_id: string; disclosure_version: string;
}
export interface Spec { model_id?: string; workflow_id?: string; workflow_revision?: number; duration_ms?: number; input_token_limit?: number; output_token_limit?: number }
export interface Binding { device_id: string; capability_id: string; kernel_environment_ref: string; kernel_environment_digest: string }
export interface Evidence { evidence_id: string; digest: string; valid_until: string; scope_ref: string }
export interface Offer {
  listing_id: string; listing_revision: number; seller_id: string; seller_display_name: string;
  sku_id: string; policy: Policy; public_spec: Spec; price: Price; kernel_bindings: Binding[];
  device_displays: { device_id: string; model_name: string }[]; deadline_sec: number;
  availability: 'AVAILABLE' | 'BUSY' | 'UNAVAILABLE'; evidence_refs: Evidence[];
}
export interface Page<T> { items: T[]; next_cursor: string | null }
export interface Profile { user_id: string; roles: string[]; permission_scope_ids: string[]; identity_state: string; version: number }
export interface Session { session_id: string; client_type: 'WEB' | 'MINIAPP'; expires_at: string; profile: Profile; csrf_token?: string; access_token?: string }
export interface Challenge { challenge_id: string; client_type: 'WEB' | 'MINIAPP'; expires_at: string; authorization_url?: string; proof_profile?: 'ALIPAY_NATIVE_NONCE_1'; native_code_verifier?: string }
export interface QuoteInput { listing_id: string; listing_revision: number; sku_id: string; policy: Policy; public_spec: Spec; budget_fen: number; desired_region: string; quantity: 1 }
export interface Contract {
  seller_id: string; listing_id: string; listing_revision: number; device_ids: string[]; sku_id: string;
  policy: Policy; public_spec: Spec; price: Price; kernel_bindings: Binding[]; total_budget_fen: number;
  seller_price_fen: number; buyer_fee_fen: number; deadline_sec: number; same_seller_retries: number;
  evidence_refs: Evidence[]; contract_hash: string; contract_schema_version: string;
}
export interface Quote { quote_id: string; state: string; version: number; contract: Contract; created_at: string; expires_at: string }
export interface OrderInput { quote_id: string; contract_hash: string; disclosure_version: string; disclosure_accepted_at: string; caller_key: string }
export interface Order { order_id: string; buyer_id: string; seller_id: string; version: number; state: string; contract: unknown; created_at: string; updated_at: string }
export interface SellerProfile { seller_id: string; display_name: string; qualification_state: string; public_identity_notice: string; approved_regions: string[]; payee_eligibility_state: string; version: number; evidence_refs: Evidence[] }
export interface ListingDefinition { title: string; sku_id: string; device_ids: string[]; policy: Policy; public_spec: Spec; price: Price; windows: { starts_at: string; ends_at: string }[]; capacity: number; evidence_refs: string[] }
export interface Listing { listing_id: string; seller_id: string; revision: number; state: string; definition: ListingDefinition; updated_at: string }

export const modeLabels: Record<Mode, string> = { PLAINTEXT: '明文', E2E: '端对端加密', GPU_RENTAL: '显卡租赁' };
export function privacyLabel(policy: Policy): string {
  if (policy.service_mode === 'E2E' && policy.privacy_profile === 'PLATFORM_BLIND') return '平台不可见；执行设备仍需处理明文';
  return policy.service_mode === 'PLAINTEXT' ? '平台网关与履约卖家可处理明文' : '标准隐私；执行设备需处理内容';
}
export const stateLabels: Record<string, string> = {
  WAITING_PAYMENT: '等待付款', VALID: '报价有效', EXPIRED: '报价过期', CONSUMED: '报价已使用',
  DRAFT: '草稿', SUBMITTED: '待审核', REVIEWING: '审核中', APPROVED: '资格已通过', PUBLISHED: '已上架',
  PAUSED: '已暂停新售', EXITING: '退出处理中', CLOSED: '已关闭', UNVERIFIED: '待核验',
  AVAILABLE: '可申请报价', BUSY: '暂不可接单', UNAVAILABLE: '暂不可接单', ACTIVE: '有效',
};
export const stateLabel = (value: string) => stateLabels[value] || '待核对';

export function object(value: unknown): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error('INVALID_RESPONSE');
  return value as Record<string, unknown>;
}
export function safeInteger(value: unknown, minimum = 0): number {
  if (typeof value !== 'number' || !Number.isSafeInteger(value) || value < minimum) throw new Error('INVALID_RESPONSE');
  return value;
}
function text(value: unknown, max = 256): string {
  if (typeof value !== 'string' || !value.trim() || value.length > max || /[\u0000-\u001f]/.test(value)) throw new Error('INVALID_RESPONSE');
  return value;
}
function strings(value: unknown): string[] {
  if (!Array.isArray(value) || value.length > 100 || value.some(v => typeof v !== 'string' || v.length > 256)) throw new Error('INVALID_RESPONSE');
  return value;
}
export function priceOf(value: unknown): Price {
  const p = object(value);
  const allowed = ['price_contract_version', 'currency', 'unit', 'rounding_profile', 'hour_rate', 'input_rate', 'output_rate', 'commission_bps', 'channel_cost_owner', 'fee_policy_version'];
  if (Object.keys(p).some(k => !allowed.includes(k))) throw new Error('INVALID_RESPONSE');
  if (p.price_contract_version !== 'NB_PRICE_1' || p.currency !== 'CNY' || p.rounding_profile !== 'ORDER_CEIL_FEN_1' ||
    !['SELLER', 'PLATFORM'].includes(String(p.channel_cost_owner)) || safeInteger(p.commission_bps) > 10000) throw new Error('INVALID_RESPONSE');
  text(p.fee_policy_version);
  const rate = (v: unknown) => { const r = object(v); safeInteger(r.numerator_fen); safeInteger(r.denominator_units, 1); };
  if (p.unit === 'AVAILABLE_HOUR') { rate(p.hour_rate); if (p.input_rate !== undefined || p.output_rate !== undefined) throw new Error('INVALID_RESPONSE'); }
  else if (p.unit === 'TOKEN_1M') { rate(p.input_rate); rate(p.output_rate); if (p.hour_rate !== undefined) throw new Error('INVALID_RESPONSE'); }
  else throw new Error('INVALID_RESPONSE');
  return p as unknown as Price;
}
export function policyOf(value: unknown): Policy {
  const p = object(value);
  if (!['PLAINTEXT', 'E2E', 'GPU_RENTAL'].includes(String(p.service_mode)) ||
    (p.service_mode === 'E2E' ? p.privacy_profile !== 'PLATFORM_BLIND' : p.privacy_profile !== 'STANDARD')) throw new Error('INVALID_RESPONSE');
  for (const k of ['region', 'protocol_profile_id', 'acceptance_policy_id', 'disclosure_version']) text(p[k]);
  return p as unknown as Policy;
}
export function specOf(value: unknown): Spec {
  const s = object(value);
  if (Object.keys(s).some(k => !['model_id', 'workflow_id', 'workflow_revision', 'duration_ms', 'input_token_limit', 'output_token_limit'].includes(k))) throw new Error('INVALID_RESPONSE');
  for (const k of ['duration_ms', 'input_token_limit', 'output_token_limit', 'workflow_revision']) if (s[k] !== undefined) safeInteger(s[k]);
  for (const k of ['model_id', 'workflow_id']) if (s[k] !== undefined) text(s[k]);
  return s as Spec;
}
export function offerOf(value: unknown): Offer {
  const o = object(value), price = priceOf(o.price), policy = policyOf(o.policy), spec = specOf(o.public_spec);
  ['listing_id', 'seller_id', 'seller_display_name', 'sku_id'].forEach(k => text(o[k]));
  safeInteger(o.listing_revision, 1); safeInteger(o.deadline_sec, 1);
  if (!['AVAILABLE', 'BUSY', 'UNAVAILABLE'].includes(String(o.availability)) || !Array.isArray(o.device_displays) || !o.device_displays.length || o.device_displays.length > 50) throw new Error('INVALID_RESPONSE');
  for (const d of o.device_displays) { const x = object(d); text(x.device_id); if (/^Android\s*设备$/i.test(text(x.model_name))) throw new Error('INVALID_RESPONSE'); }
  if ((policy.service_mode === 'GPU_RENTAL') !== (price.unit === 'AVAILABLE_HOUR')) throw new Error('INVALID_RESPONSE');
  if (price.unit === 'AVAILABLE_HOUR') safeInteger(spec.duration_ms, 1);
  else { safeInteger(spec.input_token_limit, 1); safeInteger(spec.output_token_limit, 1); text(spec.model_id); }
  if (!Array.isArray(o.kernel_bindings) || !Array.isArray(o.evidence_refs)) throw new Error('INVALID_RESPONSE');
  return o as unknown as Offer;
}
export function pageOf<T>(value: unknown, parse: (x: unknown) => T): Page<T> {
  const p = object(value);
  if (!Array.isArray(p.items) || p.items.length > 100 || p.next_cursor !== null && (typeof p.next_cursor !== 'string' || p.next_cursor.length > 512)) throw new Error('INVALID_RESPONSE');
  return { items: p.items.map(parse), next_cursor: p.next_cursor as string | null };
}
export function sessionOf(value: unknown): Session {
  const s = object(value), p = object(s.profile);
  text(s.session_id); text(s.expires_at); text(p.user_id); strings(p.roles); strings(p.permission_scope_ids); safeInteger(p.version, 1); text(p.identity_state);
  if (!['WEB', 'MINIAPP'].includes(String(s.client_type)) || !Number.isFinite(Date.parse(String(s.expires_at)))) throw new Error('INVALID_RESPONSE');
  if (s.csrf_token !== undefined) text(s.csrf_token); if (s.access_token !== undefined) text(s.access_token);
  return s as unknown as Session;
}
export function contractOf(value: unknown): Contract {
  const c = object(value); priceOf(c.price); policyOf(c.policy); specOf(c.public_spec);
  for (const k of ['seller_id', 'listing_id', 'sku_id', 'contract_hash', 'contract_schema_version']) text(c[k]);
  for (const k of ['listing_revision', 'total_budget_fen', 'seller_price_fen', 'buyer_fee_fen', 'deadline_sec', 'same_seller_retries']) safeInteger(c[k]);
  strings(c.device_ids); if (!Array.isArray(c.kernel_bindings) || !Array.isArray(c.evidence_refs)) throw new Error('INVALID_RESPONSE');
  return c as unknown as Contract;
}
export function quoteOf(value: unknown): Quote {
  const q = object(value); text(q.quote_id); text(q.state); safeInteger(q.version, 1); contractOf(q.contract);
  if (!Number.isFinite(Date.parse(text(q.expires_at))) || !Number.isFinite(Date.parse(text(q.created_at)))) throw new Error('INVALID_RESPONSE');
  return q as unknown as Quote;
}
export function orderOf(value: unknown): Order {
  const o = object(value); ['order_id', 'buyer_id', 'seller_id', 'state', 'created_at', 'updated_at'].forEach(k => text(o[k])); safeInteger(o.version, 1); object(o.contract);
  // Historical contracts are kept opaque. Never silently reinterpret an old price version.
  return o as unknown as Order;
}
export function sellerOf(value: unknown): SellerProfile {
  const p = object(value); ['seller_id', 'display_name', 'qualification_state', 'public_identity_notice', 'payee_eligibility_state'].forEach(k => text(p[k], 2048));
  strings(p.approved_regions); safeInteger(p.version, 1); if (!Array.isArray(p.evidence_refs)) throw new Error('INVALID_RESPONSE');
  return p as unknown as SellerProfile;
}
export function listingOf(value: unknown): Listing {
  const p = object(value), d = object(p.definition); ['listing_id', 'seller_id', 'state', 'updated_at'].forEach(k => text(p[k]));
  safeInteger(p.revision, 1); text(d.title); priceOf(d.price); policyOf(d.policy); specOf(d.public_spec); strings(d.device_ids);
  return p as unknown as Listing;
}
