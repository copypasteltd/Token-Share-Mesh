import { type Session, sessionOf } from './contracts';
export interface Request { method: 'GET' | 'POST' | 'PATCH' | 'DELETE'; url: string; headers: Record<string, string>; body?: string }
export interface Response { status: number; headers: Record<string, string>; data: unknown }
export type Transport = (request: Request) => Promise<Response>;
export class ApiError extends Error {
  constructor(public code: string, public status = 0, public uncertain = false, public requestId = '') { super(code); }
}
export const errorText = (error: unknown): string => {
  if (!(error instanceof ApiError)) return error instanceof Error && !/^[A-Z_]+$/.test(error.message) ? error.message : '返回信息暂不能核验，请刷新重试。';
  if (error.uncertain) return '结果尚未确认。请保留当前参数，核对原请求，不要另发一笔。';
  return ({ UNAUTHENTICATED: '登录已失效，请重新授权。', FORBIDDEN: '当前账号没有此操作权限。', NOT_FOUND: '内容已失效或不属于当前账号，请刷新。',
    CONFLICT: '状态或价版已变化，请刷新并重新确认。', RATE_LIMITED: '操作较频繁，请稍后重试。', UNAVAILABLE: '服务暂不可用，请稍后重试。',
    INVALID_REQUEST: '请检查填写内容后重试。', INVALID_RESPONSE: '返回信息暂不能核验，已停止后续操作。' } as Record<string, string>)[error.code] || '操作未完成，请核对状态后重试。';
};
export class ControlClient {
  private session: Session | null = null;
  private sessionListeners = new Set<() => void>();
  readonly base: string;
  constructor(base: string, private transport: Transport, readonly kind: 'WEB' | 'MINIAPP') {
    if (base && !/^https:\/\/[^?#]+$/.test(base) && !/^http:\/\/(127\.0\.0\.1|localhost)(:\d+)?$/.test(base)) throw new Error('仅支持已配置的安全服务地址');
    if (base.endsWith('/')) base = base.slice(0, -1);
    this.base = base;
  }
  setSession(session: Session | null): void {
    if (session && (session.client_type !== this.kind || this.kind === 'WEB' && session.access_token)) throw new ApiError('INVALID_RESPONSE');
    const changed = this.session !== session;
    this.session = session;
    if (changed) this.sessionListeners.forEach(listener => listener());
  }
  current(): Session | null { return this.session; }
  onSessionChange(listener: () => void): () => void {
    this.sessionListeners.add(listener); return () => { this.sessionListeners.delete(listener); };
  }
  private async exchange(method: Request['method'], path: string, body?: unknown, key?: string, anonymous = false, ifMatch?: string, proof?: string): Promise<Response> {
    if (!path.startsWith('/') || path.includes('://') || path.includes('#')) throw new Error('INVALID_PATH');
    const headers: Record<string, string> = { Accept: 'application/json' };
    if (body !== undefined) headers['Content-Type'] = 'application/json';
    if (key) headers['Idempotency-Key'] = key;
    if (proof !== undefined) {
      if (method !== 'POST' || !/^\/payments\/[A-Za-z0-9_-]{1,128}\/proofs$/.test(path) || proof.length < 4 || proof.length > 16384 ||
        proof.length % 4 !== 0 || !/^[A-Za-z0-9+/]+={0,2}$/.test(proof)) throw new ApiError('INVALID_REQUEST');
      headers['Payment-Proof'] = proof;
    }
    if (ifMatch !== undefined) {
      if (!/^"[1-9]\d{0,9}"$/.test(ifMatch) || Number(ifMatch.slice(1, -1)) >= 2147483647) throw new Error('INVALID_ETAG');
      headers['If-Match'] = ifMatch;
    }
    if (!anonymous) {
      if (!this.session) throw new ApiError('UNAUTHENTICATED', 401);
      if (this.kind === 'MINIAPP') {
        if (!this.session.access_token) throw new ApiError('UNAUTHENTICATED', 401);
        headers.Authorization = 'Bearer ' + this.session.access_token;
      } else if (method !== 'GET') {
        if (!this.session.csrf_token) throw new ApiError('UNAUTHENTICATED', 401);
        headers['X-CSRF-Token'] = this.session.csrf_token;
      }
    }
    const mutates = method !== 'GET';
    let response: Response;
    try { response = await this.transport({ method, url: this.base + '/v1/p11' + path, headers, ...(body === undefined ? {} : { body: JSON.stringify(body) }) }); }
    catch { throw new ApiError('TRANSPORT_UNKNOWN', 0, mutates); }
    finally { delete headers['Payment-Proof']; }
    return response;
  }
  private rejected(response: Response, mutates: boolean): ApiError {
    const data = response.data as { error?: { code?: string; request_id?: string } };
    return new ApiError(typeof data?.error?.code === 'string' ? data.error.code : 'UNAVAILABLE', response.status, mutates && response.status >= 500, data?.error?.request_id || '');
  }
  async request<T>(method: Request['method'], path: string, parse: (x: unknown) => T, body?: unknown, key?: string, anonymous = false, ifMatch?: string): Promise<T> {
    const response = await this.exchange(method, path, body, key, anonymous, ifMatch), mutates = method !== 'GET';
    if (response.status < 200 || response.status >= 300) throw this.rejected(response, mutates);
    try { return parse(response.data); } catch { throw new ApiError('INVALID_RESPONSE', response.status, mutates); }
  }
  /** Only this exact original metadata GET recognizes a 402 as a challenge.
   * No arbitrary paid SDK body, automatic purchase/retry or URL is accepted. */
  async paymentChallenge<T>(payment: string, parse: (data: unknown, headers: Record<string, string>) => T): Promise<T> {
    if (!/^[A-Za-z0-9_-]{1,128}$/.test(payment)) throw new ApiError('INVALID_REQUEST');
    const response = await this.exchange('GET', '/payments/' + payment + '/challenge');
    if (response.status !== 402) {
      if (response.status >= 200 && response.status < 300) throw new ApiError('INVALID_RESPONSE', response.status);
      throw this.rejected(response, false);
    }
    try { return parse(response.data, response.headers); } catch { throw new ApiError('INVALID_RESPONSE', 402); }
  }
  /** Credential headers never pass through CommandJournal or JSON bodies. */
  async submitPaymentProof<T>(payment: string, proof: string, key: string, parse: (data: unknown) => T): Promise<T> {
    if (!/^[A-Za-z0-9_-]{1,128}$/.test(payment) || !/^cmd_[A-Za-z0-9_-]{1,120}$/.test(key)) throw new ApiError('INVALID_REQUEST');
    const response = await this.exchange('POST', '/payments/' + payment + '/proofs', {}, key, false, undefined, proof);
    if (response.status !== 202) {
      if (response.status >= 200 && response.status < 300) throw new ApiError('INVALID_RESPONSE', response.status, true);
      throw this.rejected(response, true);
    }
    try { return parse(response.data); } catch { throw new ApiError('INVALID_RESPONSE', response.status, true); }
  }
  async restore(): Promise<Session> {
    // WEB HttpOnly cookies are read by the server, never JavaScript. Native tokens remain memory-only.
    const value = await this.request('GET', '/auth/session', sessionOf, undefined, undefined, this.kind === 'WEB');
    const merged = this.kind === 'MINIAPP' ? { ...value, access_token: this.session?.access_token } : value;
    this.setSession(merged); return merged;
  }
}
