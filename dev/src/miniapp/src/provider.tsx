import { useEffect, useState } from 'react';
import { View, Text, Button, Input, Checkbox, CheckboxGroup, Label, Picker } from '@tarojs/components';
import { useDidHide, useUnload } from '@tarojs/taro';
import { type Device, type Platform, type PolicyForm, managedDevice, terminalDevice, policyForm, policyFromForm, stateLabel } from '@tsm/core';
import { useApp } from './components';
import { model } from './runtime';

function Check({ label, checked, onChange, disabled = false }: { label: string; checked: boolean; onChange: (value: boolean) => void; disabled?: boolean }) {
  return <CheckboxGroup onChange={e => onChange(e.detail.value.includes('yes'))}><Label className='disclosure'><Checkbox value='yes' checked={checked} disabled={disabled} /><Text>{label}</Text></Label></CheckboxGroup>;
}
export function ProviderDevices() {
  const s = useApp();
  const [platform, setPlatform] = useState<Platform>('WINDOWS'), [label, setLabel] = useState(''), [confirmed, setConfirmed] = useState(false);
  const approved = s.seller?.qualification_state === 'APPROVED' && s.session?.profile.roles.includes('SELLER');
  useDidHide(model.clearPairingSecret); useUnload(model.clearPairingSecret);
  useEffect(() => {
    const hide = () => { if (typeof document !== 'undefined' && document.visibilityState === 'hidden') model.clearPairingSecret(); };
    if (typeof document !== 'undefined') document.addEventListener('visibilitychange', hide);
    return () => { if (typeof document !== 'undefined') document.removeEventListener('visibilitychange', hide); model.clearPairingSecret(); };
  }, []);
  useEffect(() => {
    if (!s.pairing?.one_time_token) return;
    const timer = setTimeout(model.clearPairingSecret, Math.max(0, Date.parse(s.pairing.expires_at) - Date.now()));
    return () => clearTimeout(timer);
  }, [s.pairing?.pairing_id, s.pairing?.one_time_token]);
  const create = async () => { await model.createPairing({ platform, device_label: label.trim(), owner_confirmed: confirmed }); setConfirmed(false); };
  const disabled = !approved || s.busy || s.pending;
  return <View className='provider-workspace'><View className='ticket panel'><Text className='section-title'>连接自己的设备</Text><Text>在本人设备上自主配对。登记建立设备身份，共享范围、内核及开售资格仍需分别确认。</Text>
    {!approved && <View className='notice'><Text>完成个人卖家资格审核后，才能申请设备配对。</Text></View>}
    <View className='provider-platforms'>{(['WINDOWS', 'LINUX', 'ANDROID'] as Platform[]).map(p => <Button key={p} className={'nb-button ' + (platform === p ? 'selected' : '')} disabled={disabled} aria-pressed={platform === p} onClick={() => { setPlatform(p); setConfirmed(false); }}><Text>{p === 'ANDROID' ? 'Android 手机' : p === 'WINDOWS' ? 'Windows' : 'Linux'}</Text></Button>)}</View>
    <View className='form-field'><Text className='field-label'>设备名称</Text><Input aria-label='配对设备名称' value={label} maxlength={512} disabled={disabled} placeholder='书房 RTX 4090 / 摩托罗拉 X30' onInput={e => { setLabel(e.detail.value); setConfirmed(false); }} /></View>
    <Check label='我持有这台设备，将在本机自主确认配对和运行内容。' checked={confirmed} disabled={disabled} onChange={setConfirmed} />
    <Button className='nb-button primary full-button' disabled={disabled || !confirmed || !label.trim()} onClick={() => void create()}><Text>创建一次性配对申请</Text></Button>
    {s.pairing && <View className='pairing-receipt' data-pairing-id={s.pairing.pairing_id}><Text className='resource-title'>本次配对申请</Text><Text className='break'>{s.pairing.pairing_id}</Text><Text>到期时间：{new Date(s.pairing.expires_at).toLocaleString()}</Text>
      {s.pairing.one_time_token ? <><Text>凭据仅保留在本页内存。离开、切到后台、隐藏或到期后不能找回。</Text>{s.pairingVisible ? <Text selectable className='pairing-secret' aria-label='一次性配对凭据'>{s.pairing.one_time_token}</Text> : <Text>一次性凭据已遮盖</Text>}<View className='provider-actions'><Button className='nb-button' onClick={model.revealPairing}><Text>显示一次性凭据</Text></Button><Button className='nb-button' onClick={model.clearPairingSecret}><Text>隐藏并清除凭据</Text></Button></View></> : <Text>凭据已清除或未收到，无法再次显示。请核对原申请，勿猜测登记是否成功。</Text>}
      <Text className='caption'>在本人设备的提供方程序中通过受控输入完成登记。不要把凭据写进网址、命令参数、日志或截图。</Text></View>}
    </View><View className='ticket panel'><Text className='section-title'>本人设备</Text><Button className='nb-button full-button' disabled={s.busy || !s.session?.profile.roles.includes('SELLER')} onClick={() => void model.loadDevices()}><Text>刷新本人设备 ↻</Text></Button>
    {s.providerNotice && <View className='notice'><Text>{s.providerNotice}</Text></View>}{!s.devices.length && <Text className='empty'>暂无已登记的本人设备。完成本机配对后，刷新核对型号与身份。</Text>}</View>
    {s.devices.map(d => <DeviceCard key={d.device_id + ':' + d.version} device={d} />)}
    {s.deviceCursor && <Button className='nb-button' disabled={s.busy} onClick={() => void model.loadDevices(true)}><Text>加载更多本人设备</Text></Button>}
  </View>;
}
function DeviceCard({ device: d }: { device: Device }) {
  const s = useApp();
  const [editing, setEditing] = useState(false), [form, setForm] = useState<PolicyForm>(() => policyForm(d)), [confirmed, setConfirmed] = useState(false);
  const [action, setAction] = useState<'drain' | 'revoke' | null>(null), [actionConfirmed, setActionConfirmed] = useState(false), [error, setError] = useState('');
  const needsRefresh = s.deviceAck?.id === d.device_id && s.deviceAck.version > d.version;
  const disabled = s.busy || s.pending || terminalDevice(d) || !managedDevice(d) || needsRefresh;
  const change = <K extends keyof PolicyForm>(key: K, value: PolicyForm[K]) => { setForm({ ...form, [key]: value }); setConfirmed(false); setError(''); };
  const save = async () => { try { await model.savePolicy(d, policyFromForm(d, form, confirmed)); setConfirmed(false); } catch (e) { setError(e instanceof Error ? e.message : '请核对共享条件。'); } };
  return <View className='ticket panel provider-device' data-device-id={d.device_id}><View className='row'><Text className='resource-title'>{d.model_name || '型号待核对'}</Text><Text className='status-tag'>{stateLabel(d.state)}</Text></View><Text>{d.device_label || '设备名称待核对'} · {d.platform}</Text>
    <View className='facts'>{[['设备编号', d.device_id], ['当前版本', String(d.version)], ['接新单状态', d.resource_policy.accepting_new_jobs ? '平台允许，执行仍需资格核验' : '未接新单'], ['在途资源占用', String(d.active_slots)], ['最近控制上报', d.last_seen_at ? new Date(d.last_seen_at).toLocaleString() : '尚未收到'], ['设备身份指纹', d.key_fingerprint || '尚未核对']].map(([key, value]) => <View className='fact' key={key}><Text>{key}</Text><Text selectable>{value}</Text></View>)}</View>
    <Text className='caption'>登记和控制上报不等于能力通过。条件保存不代表已执行；停止、断线或撤销不代表已有任务已清理。</Text>
    {!managedDevice(d) && <View className='notice'><Text>历史设备尚未建立当前控制身份，仅供核对；请在本机完成新登记。</Text></View>}
    {needsRefresh && <View className='notice'><Text>已收到新条件的保存回执，请刷新本人设备后继续操作。</Text></View>}
    <View className='provider-actions'><Button className='nb-button' disabled={disabled || d.state === 'QUARANTINED'} onClick={() => { setEditing(!editing); setAction(null); }}><Text>设置共享条件</Text></Button><Button className='nb-button' disabled={disabled} onClick={() => { setAction('drain'); setActionConfirmed(false); setEditing(false); }}><Text>停止接新单</Text></Button><Button className='nb-button danger' disabled={disabled} onClick={() => { setAction('revoke'); setActionConfirmed(false); setEditing(false); }}><Text>撤销设备身份</Text></Button></View>
    {editing && <View className='provider-form'><Text className='section-title'>共享条件 · 北京时间</Text><Check label='申请按以下条件共享' checked={form.enabled} disabled={disabled} onChange={v => change('enabled', v)} />
      {form.windows.map((w, i) => <View className='provider-window' key={i}><View className='form-field'><Text className='field-label'>开始时间</Text><Input aria-label={'共享开始时间' + (i + 1)} disabled={disabled} value={w.start} placeholder='2026-10-30 20:00' onInput={e => change('windows', form.windows.map((a, j) => j === i ? { ...a, start: e.detail.value } : a))} /></View><View className='form-field'><Text className='field-label'>结束时间</Text><Input aria-label={'共享结束时间' + (i + 1)} disabled={disabled} value={w.end} placeholder='2026-10-30 23:00' onInput={e => change('windows', form.windows.map((a, j) => j === i ? { ...a, end: e.detail.value } : a))} /></View><Button className='text-button' disabled={disabled} onClick={() => change('windows', form.windows.filter((_, j) => j !== i))}><Text>移除此时段</Text></Button></View>)}
      <Button className='nb-button' disabled={disabled || form.windows.length >= 100} onClick={() => change('windows', [...form.windows, { start: '', end: '' }])}><Text>添加共享时段</Text></Button><Text className='caption'>按先后顺序填写且不重叠；单段不超过30天，总跨度不超过90天。</Text>
      <View className='form-field'><Text className='field-label'>预留磁盘（MB）</Text><Input aria-label='预留磁盘MB' type='number' value={form.freeDiskMB} disabled={disabled} onInput={e => change('freeDiskMB', e.detail.value)} /><Text className='field-help'>1 MB = 1,000,000 字节；已有非整MB条件向上显示，保存前请核对。</Text></View>
      {d.platform === 'ANDROID' && <><View className='form-field'><Text className='field-label'>最低电量（%）</Text><Input aria-label='最低电量百分比' type='number' value={form.battery} disabled={disabled} onInput={e => change('battery', e.detail.value)} /></View><View className='form-field'><Text className='field-label'>最高温度（℃）</Text><Input aria-label='最高温度摄氏度' type='number' value={form.temperature} disabled={disabled} onInput={e => change('temperature', e.detail.value)} /></View><Check label='仅充电时共享' checked={form.charging} disabled={disabled} onChange={v => change('charging', v)} /></>}
      <View className='form-field'><Text className='field-label'>停止方式</Text><Picker mode='selector' range={['先结束已有任务', '强制停止，承担相关责任']} value={form.stop === 'DRAIN' ? 0 : 1} disabled={disabled} onChange={e => change('stop', Number(e.detail.value) === 1 ? 'FORCE_WITH_RESPONSIBILITY' : 'DRAIN')}><View className='provider-picker'>{form.stop === 'DRAIN' ? '先结束已有任务' : '强制停止，承担相关责任'} ▾</View></Picker></View>
      <Check label={'我确认上述条件' + (form.stop === 'FORCE_WITH_RESPONSIBILITY' ? '，并理解强制停止可能产生的违约责任' : '') + '；保存后仍等待设备执行与资格核验。'} checked={confirmed} disabled={disabled} onChange={setConfirmed} />{error && <Text className='field-error'>{error}</Text>}
      <Button className='nb-button primary full-button' disabled={disabled || !confirmed} onClick={() => void save()}><Text>保存共享条件</Text></Button>
    </View>}
    {action && <View className='provider-confirm'><Text className='resource-title'>{action === 'revoke' ? '确认撤销这台设备的身份' : '确认停止这台设备接新单'}</Text><Text>{action === 'revoke' ? '撤销后原身份不能恢复，重新接入须在本机生成新身份并重新配对。' : '平台先关闭新任务准入。'}已有任务仍须实际停止与清理，资金与合同责任继续按原订单处理。</Text><Check label='我已核对型号、设备编号和操作责任。' checked={actionConfirmed} disabled={disabled} onChange={setActionConfirmed} /><View className='provider-actions'><Button className='nb-button primary' disabled={disabled || !actionConfirmed} onClick={() => void model.changeDevice(d, action, actionConfirmed)}><Text>{action === 'revoke' ? '确认撤销设备身份' : '确认停止接新单'}</Text></Button><Button className='nb-button' onClick={() => setAction(null)}><Text>取消操作</Text></Button></View></View>}
  </View>;
}
