import { object, safeInteger, text } from './contracts';

export type Platform = 'WINDOWS' | 'LINUX' | 'ANDROID';
export interface PairInput { platform: Platform; device_label: string; owner_confirmed: boolean }
export interface Pairing { pairing_id: string; seller_id: string; expires_at: string; one_time_token?: string }
export interface Window { starts_at: string; ends_at: string }
export interface ResourcePolicy {
  sharing_enabled: boolean; accepting_new_jobs?: boolean; active_slots: number; windows: Window[];
  minimum_free_disk_bytes: number; stop_policy: 'DRAIN' | 'FORCE_WITH_RESPONSIBILITY';
  minimum_battery_percent?: number; maximum_temperature_celsius?: number; charging_required?: boolean;
}
export interface PolicyInput extends Omit<ResourcePolicy, 'accepting_new_jobs'> { expected_version: number }
export interface Device {
  device_id: string; seller_id: string; platform: Platform; resource_kind: string; state: string; version: number;
  model_name: string | null; device_label: string; identity_profile_id: string; key_fingerprint: string;
  client_release_id: string; last_seen_at: string | null; heartbeat_sequence: string | null;
  reported_resource_state: string; active_slots: number; capability_ids: string[]; resource_policy: Partial<ResourcePolicy>;
}
export interface DeviceAck { id: string; version: number; status: 'PROBING' | 'DRAINING'; recorded_at: string }
const id = (v: unknown) => { if (!/^[A-Za-z0-9_-]{1,128}$/.test(text(v, 128))) throw new Error('INVALID_RESPONSE'); return v as string; };
const exact = (v: Record<string, unknown>, keys: string[]) => { if (Object.keys(v).some(k => !keys.includes(k))) throw new Error('INVALID_RESPONSE'); };
const date = (v: unknown) => { if (!Number.isFinite(Date.parse(text(v, 64)))) throw new Error('INVALID_RESPONSE'); return v as string; };
const version = (v: unknown) => { const n = safeInteger(v, 1); if (n >= 2147483647) throw new Error('INVALID_RESPONSE'); return n; };
const boolean = (v: unknown) => { if (typeof v !== 'boolean') throw new Error('INVALID_RESPONSE'); return v; };
export const managedDevice = (d: Device): boolean => d.identity_profile_id === 'ED25519_DEVICE_1';
export const terminalDevice = (d: Device): boolean => ['REVOKED', 'RETIRED'].includes(d.state);

export function pairInputOf(value: unknown): PairInput {
  const p = object(value); exact(p, ['platform', 'device_label', 'owner_confirmed']);
  if (!['WINDOWS', 'LINUX', 'ANDROID'].includes(String(p.platform)) || p.owner_confirmed !== true) throw new Error('请先确认设备由你持有，并在设备上自主完成配对。');
  const label = text(p.device_label, 512);
  if (label !== label.trim() || /[\u0000-\u001f\u007f]/.test(label)) throw new Error('请填写设备名称。');
  return p as unknown as PairInput;
}
export function pairingOf(value: unknown): Pairing {
  const p = object(value); exact(p, ['pairing_id', 'seller_id', 'expires_at', 'one_time_token']);
  id(p.pairing_id); id(p.seller_id); date(p.expires_at);
  if (p.one_time_token !== undefined && !new RegExp('^p11p\\.' + p.pairing_id + '\\.[A-Za-z0-9_-]{43}$').test(text(p.one_time_token, 256))) throw new Error('INVALID_RESPONSE');
  return p as unknown as Pairing;
}
export function resourcePolicyOf(value: unknown): ResourcePolicy {
  const p = object(value); exact(p, ['sharing_enabled', 'accepting_new_jobs', 'active_slots', 'windows', 'minimum_free_disk_bytes', 'stop_policy', 'minimum_battery_percent', 'maximum_temperature_celsius', 'charging_required']);
  boolean(p.sharing_enabled); if (p.accepting_new_jobs !== undefined) boolean(p.accepting_new_jobs);
  safeInteger(p.minimum_free_disk_bytes); if (p.active_slots !== 1 || !['DRAIN', 'FORCE_WITH_RESPONSIBILITY'].includes(String(p.stop_policy)) || !Array.isArray(p.windows) || p.windows.length > 100) throw new Error('INVALID_RESPONSE');
  let end = -Infinity, first = 0;
  p.windows.forEach((raw, i) => {
    const w = object(raw); exact(w, ['starts_at', 'ends_at']);
    const a = Date.parse(date(w.starts_at)), b = Date.parse(date(w.ends_at));
    if (b <= a || b - a > 30 * 86400000 || a < end) throw new Error('INVALID_RESPONSE');
    if (!i) first = a; end = b;
  });
  if (p.sharing_enabled && !p.windows.length || end - first > 90 * 86400000) throw new Error('INVALID_RESPONSE');
  if (p.minimum_battery_percent !== undefined && safeInteger(p.minimum_battery_percent) > 100) throw new Error('INVALID_RESPONSE');
  if (p.maximum_temperature_celsius !== undefined && (typeof p.maximum_temperature_celsius !== 'number' || !Number.isFinite(p.maximum_temperature_celsius) || p.maximum_temperature_celsius < 1 || p.maximum_temperature_celsius > 120)) throw new Error('INVALID_RESPONSE');
  if (p.charging_required !== undefined) boolean(p.charging_required);
  return p as unknown as ResourcePolicy;
}
export function policyInputOf(value: unknown): PolicyInput {
  const p = object(value), { expected_version, ...policy } = p;
  version(expected_version); if ('accepting_new_jobs' in policy) throw new Error('INVALID_RESPONSE'); resourcePolicyOf(policy);
  return p as unknown as PolicyInput;
}
export function deviceOf(value: unknown): Device {
  const d = object(value); id(d.device_id); id(d.seller_id); version(d.version);
  if (!['WINDOWS', 'LINUX', 'ANDROID'].includes(String(d.platform)) || !['GPU_DEVICE', 'ANDROID_DEVICE', 'AUTHORIZED_RELAY'].includes(String(d.resource_kind)) ||
    !['PAIRED', 'PROBING', 'ONLINE', 'OFFLINE', 'DRAINING', 'QUARANTINED', 'REVOKED', 'RETIRED'].includes(String(d.state))) throw new Error('INVALID_RESPONSE');
  for (const k of ['identity_profile_id', 'key_fingerprint', 'client_release_id', 'device_label', 'reported_resource_state']) if (typeof d[k] !== 'string' || (d[k] as string).length > 512) throw new Error('INVALID_RESPONSE');
  if (d.model_name !== null) text(d.model_name, 128);
  if (d.last_seen_at !== null) date(d.last_seen_at);
  if (d.heartbeat_sequence !== null && (typeof d.heartbeat_sequence !== 'string' || !/^(0|[1-9]\d{0,19})$/.test(d.heartbeat_sequence) || BigInt(d.heartbeat_sequence) > 18446744073709551615n)) throw new Error('INVALID_RESPONSE');
  safeInteger(d.active_slots);
  if (!Array.isArray(d.capability_ids) || d.capability_ids.length > 100) throw new Error('INVALID_RESPONSE'); d.capability_ids.forEach(id);
  if (d.identity_profile_id === 'ED25519_DEVICE_1') {
    if (!/^[a-f0-9]{64}$/.test(d.key_fingerprint as string) || !d.model_name || /^(android\s*(设备|设别|device)|gpu|手机|显卡|设备)$/i.test(d.model_name as string)) throw new Error('INVALID_RESPONSE');
    resourcePolicyOf(d.resource_policy);
  } else object(d.resource_policy); // Legacy rows stay visible and read-only; no invented identity/policy.
  return d as unknown as Device;
}
export function deviceAckOf(value: unknown): DeviceAck {
  const a = object(value); id(a.id); version(a.version); date(a.recorded_at);
  if (!['PROBING', 'DRAINING'].includes(String(a.status))) throw new Error('INVALID_RESPONSE');
  return a as unknown as DeviceAck;
}
/** Only allowlisted non-secret owner commands may enter the durable browser journal. */
export function providerCommand(path: string, method: string, body: unknown, ifMatch?: string): void {
  if (path === '/devices/pairing-intents' && method === 'POST' && ifMatch === undefined) { pairInputOf(body); return; }
  if (!/^\/devices\/[A-Za-z0-9_-]{1,128}\/(resource-policy|drain|revoke)$/.test(path)) throw new Error('INVALID_PATH');
  const p = object(body);
  if (path.endsWith('/resource-policy')) {
    policyInputOf(p); if (method !== 'PATCH' || ifMatch !== '"' + p.expected_version + '"') throw new Error('INVALID_PATH');
  } else {
    exact(p, ['reason_code', 'expected_version']); id(p.reason_code); version(p.expected_version);
    if (method !== 'POST' || ifMatch !== undefined) throw new Error('INVALID_PATH');
  }
}

export interface PolicyForm {
  enabled: boolean; windows: { start: string; end: string }[]; freeDiskMB: string;
  battery: string; temperature: string; charging: boolean; stop: 'DRAIN' | 'FORCE_WITH_RESPONSIBILITY';
}
export function chinaTime(iso: string): string { return new Date(Date.parse(iso) + 8 * 3600000).toISOString().slice(0, 16).replace('T', ' '); }
export function policyForm(d: Device): PolicyForm {
  if (!managedDevice(d)) return { enabled: false, windows: [], freeDiskMB: '0', battery: '30', temperature: '45', charging: true, stop: 'DRAIN' };
  return { enabled: d.resource_policy.sharing_enabled === true, windows: (d.resource_policy.windows || []).map(w => ({ start: chinaTime(w.starts_at), end: chinaTime(w.ends_at) })),
    freeDiskMB: String(Math.ceil((d.resource_policy.minimum_free_disk_bytes || 0) / 1000000)), battery: String(d.resource_policy.minimum_battery_percent ?? 30),
    temperature: String(d.resource_policy.maximum_temperature_celsius ?? 45), charging: d.resource_policy.charging_required ?? true, stop: d.resource_policy.stop_policy || 'DRAIN' };
}
export function policyFromForm(d: Device, f: PolicyForm, confirmed: boolean): PolicyInput {
  if (!confirmed) throw new Error('请先确认共享条件与停止责任。');
  const integer = (v: string, max: number) => { if (!/^(0|[1-9]\d*)$/.test(v) || !Number.isSafeInteger(Number(v)) || Number(v) > max) throw new Error('请填写范围内的整数。'); return Number(v); };
  const timestamp = (v: string) => {
    if (!/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}$/.test(v)) throw new Error('时间格式为年-月-日 时:分，采用北京时间。');
    const t = new Date(v.replace(' ', 'T') + ':00+08:00');
    if (!Number.isFinite(t.getTime()) || chinaTime(t.toISOString()) !== v) throw new Error('请填写有效的日期和时间。');
    return t.toISOString();
  };
  const p: PolicyInput = { expected_version: d.version, sharing_enabled: f.enabled, active_slots: 1, minimum_free_disk_bytes: integer(f.freeDiskMB, Math.floor(Number.MAX_SAFE_INTEGER / 1000000)) * 1000000,
    windows: f.windows.map(w => ({ starts_at: timestamp(w.start), ends_at: timestamp(w.end) })), stop_policy: f.stop };
  if (d.platform === 'ANDROID') {
    p.minimum_battery_percent = integer(f.battery, 100); p.maximum_temperature_celsius = integer(f.temperature, 120); p.charging_required = f.charging;
  }
  try { policyInputOf(p); } catch { throw new Error('请检查共享时段：启用时至少一段，按时间顺序且不能重叠，单段最多30天、整体跨度最多90天；手机温度须为1至120。'); }
  if (p.windows.some(w => Date.parse(w.ends_at) <= Date.now())) throw new Error('共享时段的结束时间须晚于当前时间。');
  return p;
}
