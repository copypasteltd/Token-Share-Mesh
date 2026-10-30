import { sha256 } from '@noble/hashes/sha256';
import { hmac } from '@noble/hashes/hmac';
import { ApiError, ControlClient } from './client';
import { object, sessionOf, type Challenge, type Session } from './contracts';
export type SecureBytes = (length: number) => Promise<Uint8Array>;
export interface Storage { get(key: string): string | null; set(key: string, value: string): void; remove(key: string): void }
const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_';
// These protocol inputs are ASCII; native Alipay need not provide TextEncoder.
function ascii(value: string): Uint8Array {
  const bytes = new Uint8Array(value.length);
  for (let i = 0; i < value.length; i++) { const code = value.charCodeAt(i); if (code > 127) throw new Error('INVALID_PROTOCOL_TEXT'); bytes[i] = code; }
  return bytes;
}
export function base64url(bytes: Uint8Array): string {
  let output = '', bits = 0, value = 0;
  for (const b of bytes) { value = (value << 8) | b; bits += 8; while (bits >= 6) { bits -= 6; output += alphabet[(value >>> bits) & 63]; } }
  if (bits) output += alphabet[(value << (6 - bits)) & 63];
  return output;
}
export async function newKey(random: SecureBytes, prefix = 'cmd'): Promise<string> {
  const bytes = await random(32); if (bytes.length !== 32) throw new Error('无法取得安全随机数，已停止授权');
  return prefix + '_' + base64url(bytes);
}
export async function beginLogin(client: ControlClient, random: SecureBytes): Promise<{ challenge: Challenge; verifier: string }> {
  const raw = await random(32); if (raw.length !== 32) throw new Error('无法取得安全随机数，已停止授权');
  const verifier = base64url(raw), codeChallenge = base64url(sha256(ascii(verifier)));
  const challenge = await client.request('POST', '/auth/challenges', value => {
    const c = object(value);
    if (typeof c.challenge_id !== 'string' || c.client_type !== client.kind || typeof c.expires_at !== 'string' || !Number.isFinite(Date.parse(c.expires_at))) throw new Error('INVALID_RESPONSE');
    return c as unknown as Challenge;
  }, { client_type: client.kind, code_challenge: codeChallenge }, undefined, true);
  return { challenge, verifier };
}
export async function beginNativeLogin(client: ControlClient): Promise<{ challenge: Challenge; verifier: string; commandBytes: SecureBytes }> {
  if (client.kind !== 'MINIAPP') throw new ApiError('FORBIDDEN', 403);
  const challenge = await client.request('POST', '/auth/challenges', value => {
    const c = object(value);
    if (c.client_type !== 'MINIAPP' || c.proof_profile !== 'ALIPAY_NATIVE_NONCE_1' || typeof c.challenge_id !== 'string' ||
      typeof c.expires_at !== 'string' || !Number.isFinite(Date.parse(c.expires_at)) || typeof c.native_code_verifier !== 'string' ||
      !/^[A-Za-z0-9_-]{43}$/.test(c.native_code_verifier) || c.authorization_url) throw new Error('INVALID_RESPONSE');
    return c as unknown as Challenge;
  }, { client_type: 'MINIAPP', proof_profile: 'ALIPAY_NATIVE_NONCE_1' }, undefined, true);
  // Only control-command IDs use this per-login PRF. Never use it for content/session keys.
  // The 256-bit server proof is fresh, memory-only, and the login exchange consumes it once.
  const seed = sha256(ascii('P11_COMMAND_ID_SEED_1\u0000' + challenge.native_code_verifier));
  let counter = 0n;
  const commandBytes: SecureBytes = async length => {
    if (length !== 32 || counter >= 0xffffffffffffffffn) throw new Error('请重新登录后继续');
    counter++;
    return hmac(sha256, seed, ascii('P11_COMMAND_ID_1\u0000' + counter.toString()));
  };
  return { challenge, verifier: challenge.native_code_verifier!, commandBytes };
}
export async function finishLogin(client: ControlClient, challenge: Challenge, verifier: string, code: string, state?: string): Promise<Session> {
  if (Date.parse(challenge.expires_at) <= Date.now() || client.kind === 'WEB' && state !== challenge.challenge_id) throw new ApiError('UNAUTHENTICATED', 401);
  const s = await client.request('POST', '/auth/sessions', sessionOf, {
    challenge_id: challenge.challenge_id, auth_code: code, code_verifier: verifier, ...(client.kind === 'WEB' ? { web_state: state } : {})
  }, undefined, true);
  client.setSession(s); return s;
}
