import Taro from '@tarojs/taro';
import { AppModel, ControlClient, beginNativeLogin, finishLogin, type Storage, type SecureBytes, type Transport } from '@tsm/core';
import { browserAuth, browserStorage, browserTransport, browserRandom } from '@tsm/core/browser';

interface NativeAlipay {
  getAuthCode(options: { scopes: string[]; success(result: { authCode?: string }): void; fail(): void }): void;
}
declare const my: NativeAlipay;
const isBrowser = process.env.TARO_ENV === 'h5';
const storage: Storage = isBrowser ? browserStorage('tsm-mini:') : {
  get: key => { try { const v = Taro.getStorageSync('tsm:' + key); return typeof v === 'string' ? v : null; } catch { return null; } },
  set: (key, value) => Taro.setStorageSync('tsm:' + key, value), remove: key => Taro.removeStorageSync('tsm:' + key)
};
let nativeCommandBytes: SecureBytes | null = null;
const nativeRandom: SecureBytes = async length => {
  if (!nativeCommandBytes) throw new Error('请先完成安全授权，再继续操作。');
  return nativeCommandBytes(length);
};
const nativeTransport: Transport = async request => {
  const result = await Taro.request({ url: request.url, method: request.method, header: request.headers, data: request.body, timeout: 15000, dataType: 'text', responseType: 'text' });
  const raw = typeof result.data === 'string' ? result.data : JSON.stringify(result.data);
  if (typeof raw !== 'string' || raw.length > 2_097_152) throw new Error('RESPONSE_TOO_LARGE');
  return { status: result.statusCode, headers: result.header as Record<string, string>, data: JSON.parse(raw) };
};
export const client = new ControlClient(P11_API_BASE, isBrowser ? browserTransport : nativeTransport, isBrowser ? 'WEB' : 'MINIAPP');
const random = isBrowser ? browserRandom : nativeRandom;
const auth = isBrowser ? browserAuth(client, storage) : {
  async resume() { return null; },
  async login() {
    if (!P11_API_BASE) throw new Error('当前服务尚未开放登录。');
    if (typeof my === 'undefined' || typeof my.getAuthCode !== 'function') throw new Error('当前环境不支持支付宝授权。');
    const pending = await beginNativeLogin(client);
    const code = await new Promise<string>((resolve, reject) => my.getAuthCode({ scopes: ['auth_base'],
      success: result => result.authCode ? resolve(result.authCode) : reject(new Error('未取得授权，请重试。')), fail: () => reject(new Error('授权未完成，可稍后重新登录。')) }));
    const session = await finishLogin(client, pending.challenge, pending.verifier, code);
    nativeCommandBytes = pending.commandBytes;
    return session;
  }
};
export const model = new AppModel(client, storage, random, auth);
