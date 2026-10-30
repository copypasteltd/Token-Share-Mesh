import type { Storage, SecureBytes } from './auth';
import { newKey } from './auth';
import { ControlClient, ApiError } from './client';
import { object } from './contracts';
import { providerCommand } from './provider';
export interface PendingCommand { owner: string; path: string; key: string; body: unknown; method?: 'POST' | 'PATCH'; ifMatch?: string }
function validate(p: PendingCommand): void {
  if (['/quotes', '/orders'].includes(p.path)) {
    if ((p.method || 'POST') !== 'POST' || p.ifMatch !== undefined) throw new Error('INVALID_PATH');
    object(p.body); return;
  }
  providerCommand(p.path, p.method || 'POST', p.body, p.ifMatch);
}
/** Only bounded control-plane payloads. No auth tokens, prompts, outputs or channel credentials. */
export class CommandJournal {
  private active = false;
  constructor(private client: ControlClient, private storage: Storage, private random: SecureBytes) {}
  pending(): PendingCommand | null {
    const owner = this.client.current()?.profile.user_id;
    if (!owner) return null;
    const raw = this.storage.get('pending:' + owner);
    if (!raw) return null;
    if (raw.length > 65536) throw new Error('待确认记录损坏，请保留原记录并核对订单');
    const p = object(JSON.parse(raw));
    if (p.owner !== owner || typeof p.path !== 'string' || typeof p.key !== 'string' || !/^cmd_[A-Za-z0-9_-]{1,120}$/.test(p.key) ||
      Object.keys(p).some(k => !['owner', 'path', 'key', 'body', 'method', 'ifMatch'].includes(k))) throw new Error('待确认记录损坏，请保留原记录并核对请求');
    validate(p as unknown as PendingCommand); return p as unknown as PendingCommand;
  }
  async run<T>(path: string, body: unknown, parse: (x: unknown) => T, method: 'POST' | 'PATCH' = 'POST', ifMatch?: string): Promise<T> {
    if (this.active) throw new Error('正在核对原请求，请稍候');
    const owner = this.client.current()?.profile.user_id;
    if (!owner) throw new ApiError('UNAUTHENTICATED', 401);
    this.active = true;
    try {
      const snapshot = JSON.parse(JSON.stringify(body)) as unknown;
      validate({ owner, path, body: snapshot, key: 'cmd_validation', method, ifMatch });
      let p = this.pending();
      if (p && (p.path !== path || (p.method || 'POST') !== method || p.ifMatch !== ifMatch || JSON.stringify(p.body) !== JSON.stringify(snapshot))) throw new Error('存在待确认请求，请先恢复原请求');
      if (!p) { p = { owner, path, body: snapshot, key: await newKey(this.random), ...(method === 'PATCH' ? { method, ifMatch } : {}) }; this.storage.set('pending:' + owner, JSON.stringify(p)); }
      if (this.client.current()?.profile.user_id !== owner) throw new ApiError('UNAUTHENTICATED', 401);
      const result = await this.client.request(method, path, parse, p.body, p.key, false, ifMatch);
      this.storage.remove('pending:' + owner); return result;
    } catch (error) {
      // A definite 4xx response terminates this exact command. Network/5xx/malformed success retain the original key and body.
      if (error instanceof ApiError && error.status >= 400 && error.status < 500 && !error.uncertain && ![401, 403, 429].includes(error.status)) this.storage.remove('pending:' + owner);
      throw error;
    } finally { this.active = false; }
  }
}
