import React, { useEffect, useState, useSyncExternalStore } from 'react';
import { createRoot } from 'react-dom/client';
import { AppModel, ControlClient, modeLabels, privacyLabel, stateLabel, rateText, yuan, parseYuan, estimate, splitIncludedFee,
  contractOf, type Offer, type Price, type Spec, type Mode } from '@tsm/core';
import { browserAuth, browserStorage, browserTransport, browserRandom } from '@tsm/core/browser';
import mesh from '@tsm/core/assets/mesh.svg';
import gpu from '@tsm/core/assets/gpu.svg';
import phone from '@tsm/core/assets/phone.svg';
import '@tsm/core/tokens.css';
import './styles.css';
import { ProviderDevices } from './provider';

const storage = browserStorage('tsm-web:');
const client = new ControlClient(P11_API_BASE, browserTransport, 'WEB');
const model = new AppModel(client, storage, browserRandom, browserAuth(client, storage));
const useApp = () => useSyncExternalStore(model.subscribe, model.snapshot, model.snapshot);
type Tab = 'market' | 'orders' | 'share' | 'developer' | 'guide' | 'account';
const tabs: [Tab, string][] = [['market', '发现算力'], ['orders', '我的订单'], ['share', '我的共享'], ['developer', '开发者'], ['guide', '使用说明'], ['account', '我的账号']];
function PriceLines({ price }: { price: Price }) {
  return <div className='price-lines'>{price.unit === 'AVAILABLE_HOUR' ? <span><strong>{rateText(price.hour_rate!)}</strong><small>/ 小时</small></span> : <>
    <span><small>输入</small><strong>{rateText(price.input_rate!)}</strong><small>/ M token</small></span>
    <span><small>输出</small><strong>{rateText(price.output_rate!)}</strong><small>/ M token</small></span></>}</div>;
}
function Feedback() {
  const s = useApp();
  return <>{s.error && <div className='notice error' role='alert'><span>{s.error}</span><button className='text-button' onClick={model.clearError}>收起提示</button></div>}
    {s.pending && <div className='notice' role='status'><span>有一笔请求结果待确认，请先核对原请求。</span><button className='button' disabled={s.busy} onClick={() => void model.recover()}>核对原请求</button></div>}</>;
}
function App() {
  const s = useApp(), [tab, setTab] = useState<Tab>('market');
  useEffect(() => { void model.start(); }, []);
  useEffect(() => { if (s.order?.order_id) setTab('orders'); }, [s.order?.order_id]);
  return <><a className='skip-link' href='#main'>跳到主要内容</a><header className='topbar'><a className='brand' href='#market' onClick={e => { e.preventDefault(); setTab('market'); }}><img src={mesh} alt='' /><span>算力共享<small>TOKEN SHARE MESH</small></span></a>
    <nav aria-label='主导航'>{tabs.map(([key, label]) => <button key={key} className={tab === key ? 'nav selected' : 'nav'} aria-current={tab === key ? 'page' : undefined} onClick={() => setTab(key)}>{label}</button>)}</nav>
    <div className='account-control'>{s.session ? <button className='text-button' onClick={() => setTab('account')}>已登录 · 我的账号 ↗</button> : <button className='button small' disabled={!s.ready || s.busy} onClick={() => void model.login()}>支付宝登录 ↗</button>}</div></header>
    <main id='main' className='main'><Feedback />{!s.ready && <div className='notice' role='status'>正在核对登录状态…</div>}
      {tab === 'guide' ? <Guide /> : !s.session ? <Welcome /> : tab === 'market' ? <Market /> : tab === 'orders' ? <Orders /> : tab === 'share' ? <Share /> : tab === 'developer' ? <Developer /> : <Account />}
    </main><footer className='site-footer'><span>用户自发共享的个人设备 · 由选定卖家履约</span><span>按需确认，每一单都清楚。</span></footer></>;
}
function Welcome() {
  const s = useApp();
  return <div className='welcome'><div className='eyebrow'>个人闲置设备 / C2C 算力市场</div><h1>连接你的下一份<br /><em>算力可能。</em></h1><p>从一块GPU到一部手机，查看真实卖家的型号、价格与隐私边界，再决定如何使用。</p><button className='button primary large' disabled={!s.ready || s.busy} onClick={() => void model.login()}>{s.busy ? '正在准备授权…' : '支付宝授权登录 →'}</button><p className='muted'>登录不产生费用，也不会自动开启设备共享。</p><div className='welcome-art'><img src={gpu} alt='GPU类型示意图，非卖家设备实拍' /><span>你的闲置<br />别人的可能</span></div></div>;
}
function Market() {
  const s = useApp(), [search, setSearch] = useState(''), [purpose, setPurpose] = useState('全部');
  useEffect(() => { if (!model.snapshot().selected && s.offers.length && !s.pending) model.select(s.offers[0]); }, [s.offers, s.pending]);
  const filtered = s.offers.filter(o => [o.seller_display_name, o.public_spec.model_id, o.public_spec.workflow_id, ...o.device_displays.map(d => d.model_name)].join(' ').toLowerCase().includes(search.toLowerCase().trim()) &&
    (purpose === '全部' || (purpose === '对话' ? o.price.unit === 'TOKEN_1M' : o.policy.service_mode === 'GPU_RENTAL')));
  return <><div className='page-heading'><div><div className='eyebrow'>个人设备，按需使用</div><h1>找到你的下一台算力<span className='heading-dot'>↗</span></h1></div><p>看清卖家、模式与费用。<br />确认之前，尚未产生费用。</p></div>
    <div className='market-layout'><section className='resource-column' aria-label='个人卖家资源'><div className='search-row'><label className='search-label'><span aria-hidden='true'>⌕</span><input value={search} onChange={e => setSearch(e.target.value)} placeholder='搜索型号、模型或卖家' aria-label='搜索已加载资源' /></label><button className='button' disabled={s.loadingMarket} onClick={() => void model.loadMarket()}>刷新资源 ↻</button></div>
      <div className='mode-grid' aria-label='服务模式'>{(Object.keys(modeLabels) as Mode[]).map(mode => <button key={mode} className={'mode-tile ' + (s.mode === mode ? 'selected' : '')} aria-pressed={s.mode === mode} disabled={s.loadingMarket} onClick={() => void model.loadMarket(s.mode === mode ? '' : mode)}><span className='mode-icon' aria-hidden='true'>{mode === 'PLAINTEXT' ? '▤' : mode === 'E2E' ? '◈' : '▦'}</span><span>{modeLabels[mode]}</span></button>)}</div>
      <div className='purpose-tabs' aria-label='用途'>{['全部', '对话', '图像', '视频'].map(value => <button key={value} className={purpose === value ? 'selected' : ''} aria-pressed={purpose === value} onClick={() => setPurpose(value)}>{value}</button>)}</div>
      {(purpose === '图像' || purpose === '视频') && <p className='muted'>媒体通过受控GPU租赁使用；具体工作流以服务合同为准。</p>}
      <div className='resource-board'><div className='board-title'><h2>个人设备挂牌</h2><span>{s.loadingMarket ? '正在核对…' : '当前已加载 ' + filtered.length + ' 项'}</span></div>
        <div className='resource-table-wrap'><table className='resource-table'><thead><tr><th>设备 / 服务</th><th>个人卖家 / 模式</th><th>约定单价</th><th>操作</th></tr></thead><tbody>
          {filtered.map(o => <tr key={o.listing_id} data-listing-id={o.listing_id} className={s.selected?.listing_id === o.listing_id ? 'active-row' : ''}>
            <td><div className='resource-cell'><img src={o.price.unit === 'AVAILABLE_HOUR' ? gpu : phone} alt='设备类型示意图' /><div><strong>{o.device_displays.map(d => d.model_name).join(' / ')}</strong><small>{o.public_spec.model_id || o.public_spec.workflow_id || '受控GPU工作区'}</small></div></div></td>
            <td><strong>{o.seller_display_name}</strong><small>{modeLabels[o.policy.service_mode]} · {o.policy.privacy_profile === 'PLATFORM_BLIND' ? '平台不可见' : '标准隐私'}</small><small>{stateLabel(o.availability)}</small></td>
            <td><PriceLines price={o.price} /></td><td><button className='choose-button' disabled={s.busy || s.pending} aria-pressed={s.selected?.listing_id === o.listing_id} onClick={() => model.select(o)}>{s.selected?.listing_id === o.listing_id ? '已选择' : '查看服务'}<span aria-hidden='true'> ↗</span></button></td>
          </tr>)}
        </tbody></table></div>
        {!s.loadingMarket && !filtered.length && <div className='empty'>当前没有符合条件的服务。可清除筛选，或稍后刷新。</div>}
        <div className='board-bottom'><button className='text-button' onClick={() => { setSearch(''); setPurpose('全部'); void model.loadMarket(''); }}>清除筛选</button><small>搜索范围为当前已加载资源。</small>{s.nextCursor && <button className='button small' disabled={s.loadingMarket} onClick={() => void model.loadMarket(s.mode, true)}>加载更多</button>}</div>
      </div><div className='plain-note'><span className='note-symbol' aria-hidden='true'>!</span><p>每笔订单绑定一个实际个人卖家。资源暂不可用时，不会替你更换卖家或降低隐私模式。</p></div>
    </section><ContractPanel key={s.selected?.listing_id || 'empty'} /></div></>;
}
function ContractPanel() {
  const s = useApp(), o = s.selected;
  const [minutes, setMinutes] = useState(o?.public_spec.duration_ms ? String(Math.min(30, Math.floor(o.public_spec.duration_ms / 60000))) : '30');
  const [budget, setBudget] = useState('5.00'), [input, setInput] = useState(String(Math.min(o?.public_spec.input_token_limit || 1000, 1000))), [output, setOutput] = useState(String(Math.min(o?.public_spec.output_token_limit || 1000, 1000)));
  const [accepted, setAccepted] = useState(false), [formError, setFormError] = useState('');
  const change = (set: (value: string) => void, value: string) => { set(value); setAccepted(false); setFormError(''); model.clearQuote(); };
  if (!o) return <aside className='contract-ticket'><div className='contract-header'><span>本次使用条件</span><span>↗</span></div><div className='contract-body empty'>选择一项资源后，在这里核对卖家、用量与费用。</div></aside>;
  const positive = (value: string, max: number) => { if (!/^[1-9]\d*$/.test(value) || !Number.isSafeInteger(Number(value)) || Number(value) > max) throw new Error('请填写服务允许范围内的正整数。'); return Number(value); };
  const spec = (): Spec => o.price.unit === 'AVAILABLE_HOUR' ? { ...o.public_spec, duration_ms: positive(minutes, Math.floor(o.public_spec.duration_ms! / 60000)) * 60000 } :
    { ...o.public_spec, input_token_limit: positive(input, o.public_spec.input_token_limit!), output_token_limit: positive(output, o.public_spec.output_token_limit!) };
  let preview: number | null = null;
  try { preview = estimate(o.price, spec()); } catch { /* The field validation explains incomplete input at submission. */ }
  const quote = s.quote, amount = quote ? quote.contract.seller_price_fen + quote.contract.buyer_fee_fen : preview;
  const fee = amount !== null ? splitIncludedFee(amount, o.price.commission_bps) : null;
  const createQuote = async () => { try { await model.makeQuote(spec(), parseYuan(budget)); } catch (error) { setFormError(error instanceof Error ? error.message : '请核对填写内容。'); } };
  return <aside className='contract-ticket' aria-label='当前服务合同'><div className='contract-header'><span>本次使用条件</span><span aria-hidden='true'>↗</span></div><div className='contract-body'>
    <div className='contract-resource'><img src={o.price.unit === 'AVAILABLE_HOUR' ? gpu : phone} alt='设备类型示意图，非卖家实拍' /><h2>{o.device_displays.map(d => d.model_name).join(' / ')}</h2><p>{o.seller_display_name} <span className='seller-stamp'>个人卖家</span></p></div>
    <div className='contract-mode'><span>{modeLabels[o.policy.service_mode]}</span><span>价版 {o.listing_revision}</span></div><p className='privacy-note'>{privacyLabel(o.policy)}</p><PriceLines price={o.price} />
    <div className='contract-fields'>{o.price.unit === 'AVAILABLE_HOUR' ? <label>使用时长上限（分钟）<input aria-label='时长上限分钟' inputMode='numeric' value={minutes} disabled={s.busy || s.pending} onChange={e => change(setMinutes, e.target.value)} /><small>最多 {Math.floor(o.public_spec.duration_ms! / 60000)} 分钟；等待就绪后才计时。</small></label> : <div className='token-fields'><label>输入token上限<input aria-label='输入token上限' inputMode='numeric' value={input} disabled={s.busy || s.pending} onChange={e => change(setInput, e.target.value)} /></label><label>输出token上限<input aria-label='输出token上限' inputMode='numeric' value={output} disabled={s.busy || s.pending} onChange={e => change(setOutput, e.target.value)} /></label></div>}
      <label>本次预算上限（元）<input aria-label='预算上限元' inputMode='decimal' value={budget} disabled={s.busy || s.pending} onChange={e => change(setBudget, e.target.value)} /><small>预算为授权上限，尚未扣款或冻结。</small></label></div>
    <div className='fee-receipt'><div><span>{quote ? '正式报价 · 最高预估' : '费用预览 · 尚未报价'}</span><strong>{amount === null ? '待填写' : yuan(amount)}</strong></div><div><span>含平台佣金</span><span>{fee ? yuan(fee.commission) : '待确认'}</span></div><div><span>卖家预估服务净额</span><span>{fee ? yuan(fee.seller) : '待确认'}</span></div><small>佣金已含买家单价，不重复加收。最终费用按已确认用量核算；卖家净额未扣渠道成本和税费，实际到账以结算账单为准。</small></div>
    {formError && <p className='field-error' role='alert'>{formError}</p>}
    {o.policy.service_mode === 'E2E' ? <div className='gateway-block'><strong>本地网关未连接</strong><p>加密内容必须由买方可信程序处理。配对尚未开放，此页面不收集Prompt。</p><button className='button primary full' disabled>连接网关并继续</button></div> : <button className='button primary full' disabled={s.busy || s.pending || o.availability !== 'AVAILABLE'} onClick={() => void createQuote()}>{s.busy ? '正在核对…' : quote ? '重新核对报价' : '获取正式报价 →'}</button>}
    {quote && <div className='confirmed-contract'><h3>核对正式合同</h3><dl><dt>实际卖家</dt><dd>{quote.contract.seller_id}</dd><dt>报价编号</dt><dd>{quote.quote_id}</dd><dt>到期时间</dt><dd>{new Date(quote.expires_at).toLocaleString()}</dd><dt>合同摘要</dt><dd>{quote.contract.contract_hash}</dd></dl><label className='disclosure'><input type='checkbox' checked={accepted} onChange={e => setAccepted(e.target.checked)} />我确认此卖家、价版、预算及隐私说明（{quote.contract.policy.disclosure_version}）。</label><button className='button primary full' disabled={!accepted || s.busy || s.pending} onClick={() => void model.confirmOrder(accepted)}>确认合同并创建订单</button></div>}
    <p className='muted contract-footnote'>报价不占用设备。当前付款入口尚未开放。</p>
  </div></aside>;
}
function Orders() {
  const s = useApp(), [id, setId] = useState(''); const o = s.order;
  let contract: ReturnType<typeof contractOf> | null = null;
  try { if (o) contract = contractOf(o.contract); } catch { /* Keep old contracts intact. */ }
  return <><div className='page-heading'><div><div className='eyebrow'>每一笔，都有原始合同</div><h1>我的订单</h1></div></div><div className='two-columns'><section className='white-panel'><h2>查询本人订单</h2><label className='field'>完整订单号<input aria-label='订单号' value={id} maxLength={128} onChange={e => setId(e.target.value)} /></label><button className='button primary' disabled={s.busy || !id} onClick={() => void model.readOrder(id)}>查询订单 →</button><h3>本机最近记录</h3><p className='muted'>仅保存当前账号在此设备上查看过的订单号，不代表全部历史订单。</p>{s.recentIds.length ? <ul className='history'>{s.recentIds.map(value => <li key={value}><button className='text-button' disabled={s.busy} onClick={() => { setId(value); void model.readOrder(value); }}>{value} ↗</button></li>)}</ul> : <p className='empty'>暂无本机记录。</p>}</section>
    <section className='white-panel' aria-label='订单详情' data-order-id={o?.order_id}>{o ? <><div className='order-heading'><h2>订单详情</h2><span className='status-tag'>{stateLabel(o.state)}</span></div><p className='break'>{o.order_id}</p><dl className='facts'><dt>实际个人卖家</dt><dd>{o.seller_id}</dd><dt>当前确认用量</dt><dd>暂无已确认用量</dd><dt>最终费用</dt><dd>待确认</dd></dl>
      {contract ? <><PriceLines price={contract.price} /><dl className='facts'><dt>最高预估费用</dt><dd>{yuan(contract.seller_price_fen + contract.buyer_fee_fen)}</dd><dt>预算上限</dt><dd>{yuan(contract.total_budget_fen)}</dd><dt>隐私边界</dt><dd>{privacyLabel(contract.policy)}</dd><dt>合同摘要</dt><dd>{contract.contract_hash}</dd></dl></> : <p>该订单为历史合同版本，请保留原合同并联系支持核对。</p>}
      <p className='notice'>{o.state === 'WAITING_PAYMENT' ? '尚未付款，尚未开始计时。当前付款入口未开放。' : '仅展示服务端已确认状态；运行、计量和停止结果等待确认。'}</p><button className='button' disabled={s.busy} onClick={() => void model.readOrder(o.order_id)}>刷新订单状态 ↻</button></> : <div className='empty'>选择或查询一笔本人订单后，查看合同和当前状态。</div>}</section></div></>;
}
function Share() {
  const s = useApp(); useEffect(() => { if (!s.sellerLoaded) void model.loadSeller(); }, []);
  return <><div className='page-heading'><div><div className='eyebrow'>由你决定，如何共享</div><h1>我的共享</h1></div><button className='button' disabled={s.busy} onClick={() => void model.loadSeller()}>刷新本人状态 ↻</button></div>
    <div className='two-columns'><section className='white-panel'><h2>{s.seller?.display_name || '个人卖家资格'}</h2><p>你自主挂出自己的设备，决定价格和共享条件。平台不采购设备，也不承诺保底收入。</p><dl className='facts'><dt>卖家资格</dt><dd>{s.seller ? stateLabel(s.seller.qualification_state) : '尚未取得'}</dd><dt>收款资格</dt><dd>{s.seller ? stateLabel(s.seller.payee_eligibility_state) : '待核验'}</dd><dt>已结算收入</dt><dd>暂无已确认账单</dd></dl></section>
      <section><h2>本人挂牌</h2>{s.listings.length ? s.listings.map(l => <article key={l.listing_id} className='white-panel listing'><div className='order-heading'><h3>{l.definition.title}</h3><span className='status-tag'>{stateLabel(l.state)}</span></div><p>修订 {l.revision} · {modeLabels[l.definition.policy.service_mode]}</p><PriceLines price={l.definition.price} /><p className='muted break'>本人设备编号：{l.definition.device_ids.join(' / ')}</p></article>) : <div className='white-panel empty'>暂无可展示的本人挂牌。完成资格及设备配对后，才能设置价格并申请上架。</div>}</section></div><ProviderDevices model={model} /></>;
}
function Developer() { return <section className='white-panel readable'><div className='eyebrow'>内容留在可信程序</div><h1>开发者接入</h1><p>文本与媒体调用将使用OpenAI或Anthropic兼容入口。每个路由需要明确绑定个人卖家、模式、预算和协议档位。</p><h2>端对端加密</h2><p>在自己的电脑或可信局域网程序中配置端点和密钥。小程序与Web只负责交易控制，不在此输入Prompt或上传解密密钥。</p><p className='notice'>本地程序配对、调用路由及推理接口尚未开放。此处不会生成可调用的示例密钥或把明文当作加密发送。</p><button className='button' disabled>可信程序配对尚未开放</button></section>; }
function Guide() { return <section className='white-panel readable'><div className='eyebrow'>先了解，再确认</div><h1>如何使用算力共享</h1><ol className='guide-list'><li><h2>选择一个个人卖家的服务</h2><p>查看具体设备型号、可用状态、模式及约定单价。GPU按小时；LLM输入和输出分别按M token报价。</p></li><li><h2>核对用量、预算与正式合同</h2><p>预算为授权上限。正式报价绑定卖家及价版；平台佣金已包含，不在末步重复加收。</p></li><li><h2>确认后创建订单</h2><p>当前订单只到等待付款，付款与设备执行尚未开放。网络结果未知时，核对原请求，不另建一笔。</p></li></ol><h2>三种模式的内容边界</h2><p>明文模式由转发链路处理内容。基础端对端加密对平台隐藏内容，执行设备仍需处理明文。普通GPU租赁采用标准隐私。</p></section>; }
function Account() { const s = useApp(); return <section className='white-panel readable'><h1>我的账号</h1><dl className='facts'><dt>账号</dt><dd>{s.session?.profile.user_id}</dd><dt>当前身份</dt><dd>{s.session?.profile.roles.map(r => ({ BUYER: '买家', SELLER: '个人卖家' } as Record<string, string>)[r] || '其他授权身份').join(' / ')}</dd><dt>本次登录到期</dt><dd>{s.session && new Date(s.session.expires_at).toLocaleString()}</dd></dl><p>退出不会取消已有订单。待确认请求按本人账号保留，重新登录后继续核对。</p><button className='button primary' disabled={s.busy} onClick={() => void model.logout()}>退出登录</button></section>; }
createRoot(document.getElementById('root')!).render(<App />);
