import { type Offer, type Page, type Quote, type Spec, type Order, type Session, type SellerProfile, type Listing,
  offerOf, pageOf, quoteOf, orderOf, sellerOf, listingOf, object, type QuoteInput, type OrderInput } from './contracts';
import { ControlClient, ApiError, errorText } from './client';
import { type Storage, type SecureBytes, newKey } from './auth';
import { CommandJournal } from './journal';
import { estimate } from './money';
import { type Device, type Pairing, type PairInput, type PolicyInput, type DeviceAck, deviceOf, pairingOf, pairInputOf, policyInputOf, deviceAckOf, managedDevice, terminalDevice } from './provider';

export interface UiState {
  ready: boolean; session: Session | null; error: string; busy: boolean; loadingMarket: boolean;
  offers: Offer[]; nextCursor: string | null; mode: string; selected: Offer | null; quote: Quote | null;
  order: Order | null; recentIds: string[]; seller: SellerProfile | null; listings: Listing[];
  sellerLoaded: boolean; pending: boolean;
  devices: Device[]; deviceCursor: string | null; pairing: Pairing | null; pairingVisible: boolean;
  providerNotice: string; deviceAck: DeviceAck | null;
}
const initial = (): UiState => ({ ready: false, session: null, error: '', busy: false, loadingMarket: false,
  offers: [], nextCursor: null, mode: '', selected: null, quote: null, order: null, recentIds: [], seller: null,
  listings: [], sellerLoaded: false, pending: false, devices: [], deviceCursor: null, pairing: null, pairingVisible: false, providerNotice: '', deviceAck: null });
export interface AuthPort { login(): Promise<Session | void>; resume(): Promise<Session | null> }
const canonical = (value: unknown): string => JSON.stringify(value, (_key, item) =>
  typeof item === 'object' && item && !Array.isArray(item) ? Object.fromEntries(Object.entries(item).sort(([a], [b]) => a.localeCompare(b))) : item);
export class AppModel {
  private state = initial();
  private listeners = new Set<() => void>();
  private epoch = 0;
  private marketRequest = 0;
  private pairingEpoch = 0;
  private journal: CommandJournal;
  constructor(readonly client: ControlClient, private storage: Storage, private random: SecureBytes, private auth: AuthPort) {
    this.journal = new CommandJournal(client, storage, random);
  }
  subscribe = (listener: () => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; };
  snapshot = () => this.state;
  private update(patch: Partial<UiState>) { this.state = { ...this.state, ...patch }; this.listeners.forEach(f => f()); }
  clearError = () => this.update({ error: '' });
  clearPairingSecret = () => {
    this.pairingEpoch++;
    const p = this.state.pairing;
    if (p?.one_time_token || this.state.pairingVisible) this.update({ pairing: p ? { pairing_id: p.pairing_id, seller_id: p.seller_id, expires_at: p.expires_at } : null, pairingVisible: false });
  };
  revealPairing = () => {
    if (!this.state.pairing || Date.parse(this.state.pairing.expires_at) <= Date.now()) { this.clearPairingSecret(); return; }
    this.update({ pairingVisible: true });
  };
  private pending(): boolean { try { return Boolean(this.journal.pending()); } catch { return true; } }
  private entered(session: Session): void {
    this.client.setSession(session);
    let recentIds: string[] = [];
    try { const r = JSON.parse(this.storage.get('orders:' + session.profile.user_id) || '[]'); if (Array.isArray(r)) recentIds = r.filter(x => typeof x === 'string' && /^[A-Za-z0-9_-]{1,128}$/.test(x)).slice(0, 20); } catch { /* Do not trust damaged local history. */ }
    this.update({ session, ready: true, recentIds, error: '', pending: this.pending() });
  }
  private failed(error: unknown): void {
    if (error instanceof ApiError && [401, 403].includes(error.status)) this.clearPairingSecret();
    if (error instanceof ApiError && error.status === 401) {
      this.epoch++; this.marketRequest++; this.client.setSession(null);
      this.state = { ...initial(), ready: true }; // Never retain another session's market/order/seller projection.
    }
    this.update({ error: errorText(error), pending: this.client.current() ? this.pending() : false });
  }
  async start(): Promise<void> {
    const epoch = this.epoch;
    try {
      const session = await this.auth.resume() || await this.client.restore();
      if (epoch !== this.epoch) return;
      this.entered(session); await this.loadMarket();
    } catch (error) {
      if (epoch !== this.epoch) return;
      if (error instanceof ApiError && error.status === 401) this.update({ ready: true });
      else { this.failed(error); this.update({ ready: true }); }
    }
  }
  async login(): Promise<void> {
    if (this.state.busy) return;
    this.update({ busy: true, error: '' }); const epoch = ++this.epoch;
    try { const session = await this.auth.login(); if (epoch !== this.epoch) return; if (session) { this.entered(session); await this.loadMarket(); } }
    catch (error) { if (epoch === this.epoch) this.failed(error); }
    finally { if (epoch === this.epoch) this.update({ busy: false }); }
  }
  async logout(): Promise<void> {
    if (this.state.busy) return;
    this.update({ busy: true, error: '' });
    this.clearPairingSecret();
    try { await this.client.request('DELETE', '/auth/session', object); }
    catch (error) { this.failed(error); this.update({ busy: false }); return; }
    this.epoch++; this.marketRequest++; this.client.setSession(null);
    this.state = { ...initial(), ready: true }; this.update({});
  }
  async loadMarket(mode = this.state.mode, more = false): Promise<void> {
    if (!this.state.session) return;
    const id = ++this.marketRequest, epoch = this.epoch;
    const cursor = more ? this.state.nextCursor : null;
    if (more && !cursor) return;
    this.update({ loadingMarket: true, error: '', mode, ...(!more ? { offers: [], nextCursor: null } : {}) });
    try {
      const params = ['limit=20', ...(mode ? ['mode=' + encodeURIComponent(mode)] : []), ...(cursor ? ['cursor=' + encodeURIComponent(cursor)] : [])].join('&');
      const page = await this.client.request('GET', '/services?' + params, x => pageOf(x, offerOf));
      if (id !== this.marketRequest || epoch !== this.epoch) return;
      const offers = more ? [...this.state.offers, ...page.items] : page.items;
      this.update({ offers: offers.filter((o, i, all) => all.findIndex(a => a.listing_id === o.listing_id) === i), nextCursor: page.next_cursor });
    } catch (error) { if (id === this.marketRequest && epoch === this.epoch) this.failed(error); }
    finally { if (id === this.marketRequest && epoch === this.epoch) this.update({ loadingMarket: false }); }
  }
  select(offer: Offer): boolean {
    if (this.state.busy || this.state.pending) { this.update({ error: '请先确认原请求的结果，再更换服务。' }); return false; }
    this.update({ selected: offer, quote: null, error: '' }); return true;
  }
  clearQuote(): void { if (!this.state.busy && !this.state.pending) this.update({ quote: null, error: '' }); }
  private async command<T>(action: () => Promise<T>, accept: (result: T) => void): Promise<void> {
    if (this.state.busy) return;
    const epoch = this.epoch; this.update({ busy: true, error: '' });
    try { const result = await action(); if (epoch === this.epoch) accept(result); }
    catch (error) { if (epoch === this.epoch) this.failed(error); }
    finally { if (epoch === this.epoch) this.update({ busy: false, pending: this.pending() }); }
  }
  async makeQuote(spec: Spec, budgetFen: number): Promise<void> {
    const offer = this.state.selected;
    if (!offer) return;
    await this.command(async () => {
      if (offer.availability !== 'AVAILABLE') throw new Error('该资源暂不可接单，请刷新后重试。');
      if (offer.policy.service_mode === 'E2E') throw new Error('请先在可信程序完成网关配对；当前入口尚未开放加密调用。');
      if (estimate(offer.price, spec) > budgetFen) throw new Error('预算低于最高预估费用，请调整预算或用量。');
      const input: QuoteInput = { listing_id: offer.listing_id, listing_revision: offer.listing_revision, sku_id: offer.sku_id,
        policy: offer.policy, public_spec: spec, budget_fen: budgetFen, desired_region: offer.policy.region, quantity: 1 };
      return this.journal.run('/quotes', input, raw => {
        const quote = quoteOf(raw), c = quote.contract;
        if (c.seller_id !== offer.seller_id || c.listing_id !== offer.listing_id || c.listing_revision !== offer.listing_revision ||
          canonical(c.policy) !== canonical(input.policy) || canonical(c.public_spec) !== canonical(spec) ||
          canonical(c.price) !== canonical(offer.price) || c.total_budget_fen !== budgetFen || c.buyer_fee_fen !== 0) throw new Error('INVALID_RESPONSE');
        return quote;
      });
    }, quote => this.update({ quote }));
  }
  private remember(order: Order) {
    const ids = [order.order_id, ...this.state.recentIds.filter(x => x !== order.order_id)].slice(0, 20);
    // A receipt remains usable even when local history storage is full/disabled.
    try { this.storage.set('orders:' + order.buyer_id, JSON.stringify(ids)); } catch { /* non-authoritative convenience */ }
    this.update({ order, recentIds: ids, quote: null });
  }
  async confirmOrder(accepted: boolean): Promise<void> {
    const q = this.state.quote;
    if (!q) return;
    await this.command(async () => {
      if (!accepted) throw new Error('请先阅读并确认当前卖家、合同、预算和隐私说明。');
      if (q.state !== 'VALID' || Date.parse(q.expires_at) <= Date.now()) throw new Error('报价已失效，请重新获取报价。');
      if (q.contract.policy.service_mode === 'E2E') throw new Error('网关尚未配对，不能继续创建加密订单。');
      const input: OrderInput = { quote_id: q.quote_id, contract_hash: q.contract.contract_hash, disclosure_version: q.contract.policy.disclosure_version,
        disclosure_accepted_at: new Date().toISOString(), caller_key: await newKey(this.random, 'caller') };
      return this.journal.run('/orders', input, raw => {
        const order = orderOf(raw);
        if (order.buyer_id !== this.state.session?.profile.user_id || order.seller_id !== q.contract.seller_id || canonical(order.contract) !== canonical(q.contract)) throw new Error('INVALID_RESPONSE');
        return order;
      });
    }, order => this.remember(order));
  }
  async recover(): Promise<void> {
    const pairingEpoch = this.pairingEpoch;
    await this.command(async () => {
      const p = this.journal.pending(); if (!p) return null;
      if (p.path === '/quotes') return { quote: await this.journal.run('/quotes', p.body, quoteOf) };
      if (p.path === '/devices/pairing-intents') return { pairing: await this.journal.run(p.path, p.body, raw => this.ownPairing(raw)) };
      if (p.path.endsWith('/resource-policy')) return { ack: await this.journal.run(p.path, p.body, raw => this.policyAck(raw, p.path, p.body as PolicyInput), 'PATCH', p.ifMatch) };
      if (/\/(drain|revoke)$/.test(p.path)) return { device: await this.journal.run(p.path, p.body, raw => this.actionDevice(raw, p.path, p.body as { expected_version: number })) };
      return { order: await this.journal.run('/orders', p.body, raw => {
        const o = orderOf(raw); if (o.buyer_id !== this.state.session?.profile.user_id) throw new Error('INVALID_RESPONSE'); return o;
      }) };
    }, result => {
      if (result?.quote) {
        const quote = result.quote, selected = this.state.offers.find(o => o.listing_id === quote.contract.listing_id && o.listing_revision === quote.contract.listing_revision && o.seller_id === quote.contract.seller_id) || null;
        this.update({ selected, quote: selected ? quote : null, error: selected ? '' : '原报价已恢复。请重新选择原挂牌并核对当前状态。' });
      }
      if (result?.order) this.remember(result.order);
      if (result?.pairing) this.acceptPairing(result.pairing, pairingEpoch);
      if (result?.ack) this.update({ deviceAck: result.ack, providerNotice: '共享条件已由平台保存，设备执行与资格仍待确认。请刷新本人设备。' });
      if (result?.device) this.acceptDevice(result.device);
    });
  }
  async readOrder(id: string): Promise<void> {
    await this.command(async () => {
      if (!/^[A-Za-z0-9_-]{1,128}$/.test(id)) throw new Error('请填写完整订单号。');
      return this.client.request('GET', '/orders/' + encodeURIComponent(id), raw => {
        const order = orderOf(raw); if (order.buyer_id !== this.state.session?.profile.user_id) throw new Error('INVALID_RESPONSE'); return order;
      });
    }, order => this.remember(order));
  }
  async loadSeller(): Promise<void> {
    await this.command(async () => {
      let seller: SellerProfile;
      try { seller = await this.client.request('GET', '/seller/profile', sellerOf); }
      catch (error) { if (error instanceof ApiError && error.status === 404) return { seller: null, listings: [], devices: [], deviceCursor: null, sellerLoaded: true }; throw error; }
      if (seller.seller_id !== this.client.current()?.profile.user_id) throw new Error('INVALID_RESPONSE');
      let listings: Listing[] = [];
      let devices: Device[] = [], deviceCursor: string | null = null;
      if (this.state.session?.profile.roles.includes('SELLER')) {
        const page: Page<Listing> = await this.client.request('GET', '/seller/listings?limit=50', raw => pageOf(raw, listingOf));
        listings = page.items;
        if (['APPROVED', 'EXITING'].includes(seller.qualification_state)) {
          const own = await this.client.request('GET', '/seller/devices?limit=50', raw => pageOf(raw, value => this.ownDevice(value)));
          devices = own.items; deviceCursor = own.next_cursor;
        }
      }
      return { seller, listings, devices, deviceCursor, sellerLoaded: true };
    }, result => this.update(result));
  }
  private ownDevice(raw: unknown, expected?: string): Device {
    const d = deviceOf(raw);
    if (d.seller_id !== this.client.current()?.profile.user_id || expected && d.device_id !== expected) throw new Error('INVALID_RESPONSE');
    return d;
  }
  private ownPairing(raw: unknown): Pairing {
    const p = pairingOf(raw); if (p.seller_id !== this.client.current()?.profile.user_id) throw new Error('INVALID_RESPONSE'); return p;
  }
  private acceptPairing(p: Pairing, epoch: number): void {
    if (epoch !== this.pairingEpoch || Date.parse(p.expires_at) <= Date.now()) p = { pairing_id: p.pairing_id, seller_id: p.seller_id, expires_at: p.expires_at };
    this.update({ pairing: p, pairingVisible: false, providerNotice: p.one_time_token ? '配对申请已创建。请在本人设备上使用一次性凭据完成登记。' : '原配对申请已确认，凭据不再显示。如需另开申请，请重新确认；旧申请到期失效。' });
  }
  async createPairing(input: PairInput): Promise<void> {
    this.clearPairingSecret(); const epoch = this.pairingEpoch;
    await this.command(async () => {
      pairInputOf(input);
      if (!this.state.session?.profile.roles.includes('SELLER') || this.state.seller?.qualification_state !== 'APPROVED') throw new Error('请先完成个人卖家资格审核。');
      return this.journal.run('/devices/pairing-intents', input, raw => this.ownPairing(raw));
    }, pairing => this.acceptPairing(pairing, epoch));
  }
  async loadDevices(more = false): Promise<void> {
    const cursor = more ? this.state.deviceCursor : null; if (more && !cursor) return;
    await this.command(() => this.client.request('GET', '/seller/devices?limit=50' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : ''), raw => pageOf(raw, value => this.ownDevice(value))), page => {
      const list = more ? [...this.state.devices, ...page.items] : page.items;
      this.update({ devices: list.filter((d, i) => list.findIndex(x => x.device_id === d.device_id) === i), deviceCursor: page.next_cursor });
    });
  }
  private policyAck(raw: unknown, path: string, input: PolicyInput): DeviceAck {
    const a = deviceAckOf(raw); if (a.id !== path.split('/')[2] || a.version !== input.expected_version + 1) throw new Error('INVALID_RESPONSE'); return a;
  }
  private requireFreshDevice(device: Device): void {
    if (this.state.deviceAck?.id === device.device_id && this.state.deviceAck.version > device.version || this.state.devices.some(d => d.device_id === device.device_id && d.version > device.version)) throw new Error('请刷新本人设备，再重新核对操作。');
  }
  private actionDevice(raw: unknown, path: string, input: { expected_version: number }): Device {
    const d = this.ownDevice(raw, path.split('/')[2]);
    if (d.version !== input.expected_version + 1 || (path.endsWith('/revoke') ? d.state !== 'REVOKED' : !['DRAINING', 'QUARANTINED'].includes(d.state))) throw new Error('INVALID_RESPONSE');
    return d;
  }
  async savePolicy(device: Device, input: PolicyInput): Promise<void> {
    await this.command(async () => {
      policyInputOf(input);
      this.requireFreshDevice(device);
      if (!managedDevice(device) || terminalDevice(device) || device.state === 'QUARANTINED' || input.expected_version !== device.version || device.seller_id !== this.state.session?.profile.user_id) throw new Error('请刷新设备状态，重新核对共享条件。');
      const path = '/devices/' + device.device_id + '/resource-policy';
      return this.journal.run(path, input, raw => this.policyAck(raw, path, input), 'PATCH', '"' + device.version + '"');
    }, ack => this.update({ deviceAck: ack, providerNotice: '共享条件已由平台保存，设备执行与资格仍待确认。请刷新本人设备。' }));
  }
  private acceptDevice(d: Device): void {
    this.update({ devices: this.state.devices.map(x => x.device_id === d.device_id && x.version <= d.version ? d : x), deviceAck: null,
      providerNotice: d.state === 'REVOKED' ? '设备身份已撤销，已关闭新接单；已有任务仍须实际停止与清理。' : '已关闭新接单；已有任务仍须实际停止与清理。' });
  }
  async changeDevice(device: Device, action: 'drain' | 'revoke', confirmed: boolean): Promise<void> {
    await this.command(async () => {
      this.requireFreshDevice(device);
      if (!confirmed || !managedDevice(device) || terminalDevice(device) || device.seller_id !== this.state.session?.profile.user_id) throw new Error('请先核对设备并确认操作责任。');
      const body = { reason_code: action === 'revoke' ? 'OWNER_REVOKE' : 'OWNER_STOP', expected_version: device.version };
      const path = '/devices/' + device.device_id + '/' + action;
      return this.journal.run(path, body, raw => this.actionDevice(raw, path, body));
    }, result => { this.clearPairingSecret(); this.acceptDevice(result); });
  }
}
