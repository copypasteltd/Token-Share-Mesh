import { useEffect, useState, useSyncExternalStore } from 'react';
import { type AppModel, type Device, type Platform, type PolicyForm, managedDevice, terminalDevice, policyForm, policyFromForm, stateLabel } from '@tsm/core';

export function ProviderDevices({ model }: { model: AppModel }) {
  const s = useSyncExternalStore(model.subscribe, model.snapshot, model.snapshot);
  const [platform, setPlatform] = useState<Platform>('WINDOWS'), [label, setLabel] = useState(''), [confirmed, setConfirmed] = useState(false);
  const approved = s.seller?.qualification_state === 'APPROVED' && s.session?.profile.roles.includes('SELLER');
  useEffect(() => {
    const hide = () => { if (document.visibilityState === 'hidden') model.clearPairingSecret(); };
    document.addEventListener('visibilitychange', hide); window.addEventListener('pagehide', model.clearPairingSecret);
    return () => { document.removeEventListener('visibilitychange', hide); window.removeEventListener('pagehide', model.clearPairingSecret); model.clearPairingSecret(); };
  }, [model]);
  useEffect(() => {
    if (!s.pairing?.one_time_token) return;
    const timer = setTimeout(model.clearPairingSecret, Math.max(0, Date.parse(s.pairing.expires_at) - Date.now()));
    return () => clearTimeout(timer);
  }, [s.pairing?.pairing_id, s.pairing?.one_time_token, model]);
  const create = async () => { await model.createPairing({ platform, device_label: label.trim(), owner_confirmed: confirmed }); setConfirmed(false); };
  return <section className='provider-workspace' aria-label='本人设备管理'>
    <div className='page-heading'><div><div className='eyebrow'>设备在你手中，条件由你确认</div><h2>本人设备</h2></div><button className='button' disabled={s.busy || !s.session?.profile.roles.includes('SELLER')} onClick={() => void model.loadDevices()}>刷新本人设备 ↻</button></div>
    {s.providerNotice && <p className='notice' role='status'>{s.providerNotice}</p>}
    <div className='two-columns'><section className='white-panel pairing-panel'><h3>连接自己的设备</h3><p>只在你持有的设备上使用配对凭据。登记建立设备身份，共享范围、内核及开售资格仍需分别确认。</p>
      {!approved && <p className='notice'>完成个人卖家资格审核后，才能申请设备配对。</p>}
      <fieldset disabled={!approved || s.busy || s.pending}><legend>设备系统</legend><div className='provider-platforms'>{(['WINDOWS', 'LINUX', 'ANDROID'] as Platform[]).map(p => <label key={p}><input type='radio' name='provider-platform' checked={platform === p} onChange={() => { setPlatform(p); setConfirmed(false); }} />{p === 'ANDROID' ? 'Android 手机' : p === 'WINDOWS' ? 'Windows' : 'Linux'}</label>)}</div>
        <label className='field'>设备名称<input aria-label='配对设备名称' value={label} maxLength={512} placeholder='例如：书房 RTX 4090 / 摩托罗拉 X30' onChange={e => { setLabel(e.target.value); setConfirmed(false); }} /></label>
        <label className='disclosure'><input aria-label='确认本人设备配对' type='checkbox' checked={confirmed} onChange={e => setConfirmed(e.target.checked)} />我持有这台设备，将在本机自主确认配对和运行内容。</label>
        <button className='button primary full' disabled={!confirmed || !label.trim()} onClick={() => void create()}>创建一次性配对申请</button>
      </fieldset>
      {s.pairing && <div className='pairing-receipt' data-pairing-id={s.pairing.pairing_id}><h4>本次配对申请</h4><p className='break'>{s.pairing.pairing_id}</p><p>到期时间：{new Date(s.pairing.expires_at).toLocaleString()}</p>
        {s.pairing.one_time_token ? <><p>凭据仅保留在本页内存。离开、切到后台、隐藏或到期后不能找回。</p>{s.pairingVisible ? <code className='pairing-secret' aria-label='一次性配对凭据'>{s.pairing.one_time_token}</code> : <p>一次性凭据已遮盖</p>}
          <div className='provider-actions'><button className='button' onClick={model.revealPairing}>显示一次性凭据</button><button className='button' onClick={model.clearPairingSecret}>隐藏并清除凭据</button></div></> : <p>凭据已清除或未收到，无法再次显示。请核对原申请，勿猜测登记是否成功。</p>}
        <p className='muted'>在本人设备的提供方程序中通过受控输入完成登记。不要将凭据写进网址、命令参数、日志或截图。</p></div>}
    </section><section className='provider-devices' aria-label='设备列表'>
      {!s.devices.length && <div className='white-panel empty'>暂无已登记的本人设备。完成本机配对后，刷新此处核对型号与身份。</div>}
      {s.devices.map(d => <DeviceCard key={d.device_id + ':' + d.version} device={d} model={model} />)}
      {s.deviceCursor && <button className='button' disabled={s.busy} onClick={() => void model.loadDevices(true)}>加载更多本人设备</button>}
    </section></div>
  </section>;
}
function DeviceCard({ device: d, model }: { device: Device; model: AppModel }) {
  const s = useSyncExternalStore(model.subscribe, model.snapshot, model.snapshot);
  const [editing, setEditing] = useState(false), [form, setForm] = useState<PolicyForm>(() => policyForm(d)), [confirmed, setConfirmed] = useState(false);
  const [action, setAction] = useState<'drain' | 'revoke' | null>(null), [actionConfirmed, setActionConfirmed] = useState(false), [error, setError] = useState('');
  const needsRefresh = s.deviceAck?.id === d.device_id && s.deviceAck.version > d.version;
  const disabled = s.busy || s.pending || terminalDevice(d) || !managedDevice(d) || needsRefresh;
  const change = <K extends keyof PolicyForm>(key: K, value: PolicyForm[K]) => { setForm({ ...form, [key]: value }); setConfirmed(false); setError(''); };
  const save = async () => { try { await model.savePolicy(d, policyFromForm(d, form, confirmed)); setConfirmed(false); } catch (e) { setError(e instanceof Error ? e.message : '请核对共享条件。'); } };
  return <article className='white-panel provider-device' data-device-id={d.device_id}><div className='order-heading'><h3>{d.model_name || '型号待核对'}</h3><span className='status-tag'>{stateLabel(d.state)}</span></div><p>{d.device_label || '设备名称待核对'} · {d.platform}</p>
    <dl className='facts'><dt>设备编号</dt><dd>{d.device_id}</dd><dt>当前版本</dt><dd>{d.version}</dd><dt>接新单状态</dt><dd>{d.resource_policy.accepting_new_jobs ? '平台允许，执行仍需资格核验' : '未接新单'}</dd><dt>在途资源占用</dt><dd>{d.active_slots}</dd><dt>最近控制上报</dt><dd>{d.last_seen_at ? new Date(d.last_seen_at).toLocaleString() : '尚未收到'}</dd><dt>设备身份指纹</dt><dd>{d.key_fingerprint || '尚未核对'}</dd></dl>
    <p className='muted'>登记和控制上报不等于能力通过。平台保存的条件不代表设备已开始执行；停止、断线或撤销也不代表在途任务已清理。</p>
    {!managedDevice(d) && <p className='notice'>历史设备尚未建立当前控制身份，仅供核对；请在本机完成新登记。</p>}
    {needsRefresh && <p className='notice'>已收到新条件的保存回执，请刷新本人设备后继续操作。</p>}
    <div className='provider-actions'><button className='button' disabled={disabled || d.state === 'QUARANTINED'} onClick={() => { setEditing(!editing); setAction(null); }}>设置共享条件</button><button className='button' disabled={disabled} onClick={() => { setAction('drain'); setActionConfirmed(false); setEditing(false); }}>停止接新单</button><button className='button danger' disabled={disabled} onClick={() => { setAction('revoke'); setActionConfirmed(false); setEditing(false); }}>撤销设备身份</button></div>
    {editing && <fieldset className='provider-form' disabled={disabled}><legend>共享条件 · 北京时间</legend><label className='disclosure'><input type='checkbox' checked={form.enabled} onChange={e => change('enabled', e.target.checked)} />申请按以下条件共享</label>
      {form.windows.map((w, i) => <div className='provider-window' key={i}><label className='field'>开始时间<input aria-label={'共享开始时间' + (i + 1)} placeholder='2026-10-30 20:00' value={w.start} onChange={e => change('windows', form.windows.map((a, j) => j === i ? { ...a, start: e.target.value } : a))} /></label><label className='field'>结束时间<input aria-label={'共享结束时间' + (i + 1)} placeholder='2026-10-30 23:00' value={w.end} onChange={e => change('windows', form.windows.map((a, j) => j === i ? { ...a, end: e.target.value } : a))} /></label><button className='text-button' onClick={() => change('windows', form.windows.filter((_, j) => j !== i))}>移除此时段</button></div>)}
      <button className='button small' disabled={form.windows.length >= 100} onClick={() => change('windows', [...form.windows, { start: '', end: '' }])}>添加共享时段</button><p className='muted'>按先后顺序填写且不重叠；单段不超过30天，总跨度不超过90天。</p>
      <label className='field'>预留磁盘（MB）<input aria-label='预留磁盘MB' inputMode='numeric' value={form.freeDiskMB} onChange={e => change('freeDiskMB', e.target.value)} /><small>1 MB = 1,000,000 字节；已有非整MB条件向上显示，保存前请核对。</small></label>
      {d.platform === 'ANDROID' && <><label className='field'>最低电量（%）<input aria-label='最低电量百分比' inputMode='numeric' value={form.battery} onChange={e => change('battery', e.target.value)} /></label><label className='field'>最高温度（℃）<input aria-label='最高温度摄氏度' inputMode='numeric' value={form.temperature} onChange={e => change('temperature', e.target.value)} /></label><label className='disclosure'><input type='checkbox' checked={form.charging} onChange={e => change('charging', e.target.checked)} />仅充电时共享</label></>}
      <label className='field'>停止方式<select aria-label='停止方式' value={form.stop} onChange={e => change('stop', e.target.value as PolicyForm['stop'])}><option value='DRAIN'>先结束已有任务</option><option value='FORCE_WITH_RESPONSIBILITY'>强制停止，承担相关责任</option></select></label>
      <label className='disclosure'><input aria-label='确认共享条件' type='checkbox' checked={confirmed} onChange={e => setConfirmed(e.target.checked)} />我确认上述条件{form.stop === 'FORCE_WITH_RESPONSIBILITY' ? '，并理解强制停止可能产生的违约责任' : ''}；保存后仍等待设备执行与资格核验。</label>{error && <p className='field-error' role='alert'>{error}</p>}<button className='button primary full' disabled={!confirmed} onClick={() => void save()}>保存共享条件</button>
    </fieldset>}
    {action && <div className='provider-confirm'><h4>{action === 'revoke' ? '确认撤销这台设备的身份' : '确认停止这台设备接新单'}</h4><p>{action === 'revoke' ? '撤销后原身份不能恢复，重新接入须在本机生成新身份并重新配对。' : '平台先关闭新任务准入。'}已有任务仍须实际停止与清理，资金与合同责任继续按原订单处理。</p><label className='disclosure'><input aria-label='确认设备操作责任' type='checkbox' checked={actionConfirmed} onChange={e => setActionConfirmed(e.target.checked)} />我已核对型号、设备编号和操作责任。</label><div className='provider-actions'><button className='button primary' disabled={disabled || !actionConfirmed} onClick={() => void model.changeDevice(d, action, actionConfirmed)}>{action === 'revoke' ? '确认撤销设备身份' : '确认停止接新单'}</button><button className='button' onClick={() => setAction(null)}>取消操作</button></div></div>}
  </article>;
}
