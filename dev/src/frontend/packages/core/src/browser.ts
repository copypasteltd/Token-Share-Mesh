import { ControlClient, ApiError, type Transport } from './client';
import { beginLogin, finishLogin, type Storage, type SecureBytes } from './auth';
import type { Challenge, Session } from './contracts';
export const browserRandom: SecureBytes = async length => {
  if (!globalThis.crypto?.getRandomValues) throw new Error('当前环境不支持安全授权，请使用受支持的浏览器');
  return globalThis.crypto.getRandomValues(new Uint8Array(length));
};
export function browserStorage(namespace: string): Storage {
  return { get: key => sessionStorage.getItem(namespace + key), set: (key, value) => sessionStorage.setItem(namespace + key, value), remove: key => sessionStorage.removeItem(namespace + key) };
}
export const browserTransport: Transport = async request => {
  const controller = new AbortController(), timeout = setTimeout(() => controller.abort(), 15000);
  try {
    const r = await fetch(request.url, { method: request.method, headers: request.headers, body: request.body, credentials: 'include', cache: 'no-store', redirect: 'error', signal: controller.signal });
    const reader = r.body?.getReader();
    if (!reader) throw new Error('EMPTY_RESPONSE');
    const chunks: Uint8Array[] = []; let size = 0;
    try {
      for (;;) {
        const part = await reader.read(); if (part.done) break;
        size += part.value.byteLength;
        if (size > 2_097_152) { await reader.cancel(); throw new Error('RESPONSE_TOO_LARGE'); }
        chunks.push(part.value);
      }
    } finally { reader.releaseLock(); }
    const bytes = new Uint8Array(size); let offset = 0;
    for (const chunk of chunks) { bytes.set(chunk, offset); offset += chunk.byteLength; }
    const body = new TextDecoder('utf-8', { fatal: true }).decode(bytes);
    return { status: r.status, headers: Object.fromEntries(r.headers), data: JSON.parse(body) };
  } finally { clearTimeout(timeout); }
};
export function browserAuth(client: ControlClient, storage: Storage) {
  return {
    async login(): Promise<void> {
      const pending = await beginLogin(client, browserRandom);
      const url = new URL(pending.challenge.authorization_url || '', location.origin);
      const official = url.protocol === 'https:' && url.hostname === 'openauth.alipay.com' && url.port === '';
      const localFixture = url.origin === location.origin && url.protocol === 'http:' && ['127.0.0.1', 'localhost'].includes(url.hostname);
      if (!official && !localFixture) throw new Error('授权地址不可用');
      if (url.username || url.password) throw new Error('授权地址不可用');
      // PKCE verifier is temporary, single-use and never placed in a URL or logs.
      storage.set('login', JSON.stringify(pending)); location.assign(url.href);
    },
    async resume(): Promise<Session | null> {
      const url = new URL(location.href), code = url.searchParams.get('auth_code'), state = url.searchParams.get('state');
      if (!code && !state) { storage.remove('login'); return null; }
      url.searchParams.delete('auth_code'); url.searchParams.delete('state'); history.replaceState(null, '', url.pathname + url.search + url.hash);
      const raw = storage.get('login'); storage.remove('login');
      if (!raw || !code || !state) throw new ApiError('UNAUTHENTICATED', 401);
      const p = JSON.parse(raw) as { challenge: Challenge; verifier: string };
      return finishLogin(client, p.challenge, p.verifier, code, state);
    }
  };
}
