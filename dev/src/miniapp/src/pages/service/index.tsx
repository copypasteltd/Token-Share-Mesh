import { useState } from 'react';
import { View, Text, Button, Input, CheckboxGroup, Checkbox, Label } from '@tarojs/components';
import Taro from '@tarojs/taro';
import { parseYuan, yuan, estimate, splitIncludedFee, privacyLabel, modeLabels, type Spec } from '@tsm/core';
import { BottomNav, Feedback, LoginGate, PageTitle, PriceBlock, Empty, useApp } from '../../components';
import { model } from '../../runtime';
export default function Service() {
  const s = useApp(), o = s.selected;
  const [budget, setBudget] = useState('5.00'), [minutes, setMinutes] = useState(o?.public_spec.duration_ms ? String(Math.min(30, Math.floor(o.public_spec.duration_ms / 60000))) : '30');
  const [input, setInput] = useState(String(Math.min(o?.public_spec.input_token_limit || 1000, 1000))), [output, setOutput] = useState(String(Math.min(o?.public_spec.output_token_limit || 1000, 1000)));
  const [accepted, setAccepted] = useState(false), [localError, setLocalError] = useState('');
  const change = (set: (v: string) => void, value: string) => { set(value); setAccepted(false); setLocalError(''); model.clearQuote(); };
  const positive = (value: string, max: number) => { if (!/^[1-9]\d*$/.test(value) || !Number.isSafeInteger(Number(value)) || Number(value) > max) throw new Error('请填写服务允许范围内的正整数。'); return Number(value); };
  const makeQuote = async () => {
    if (!o) return;
    try {
      const spec: Spec = o.price.unit === 'AVAILABLE_HOUR' ? { ...o.public_spec, duration_ms: positive(minutes, Math.floor(o.public_spec.duration_ms! / 60000)) * 60000 } :
        { ...o.public_spec, input_token_limit: positive(input, o.public_spec.input_token_limit!), output_token_limit: positive(output, o.public_spec.output_token_limit!) };
      await model.makeQuote(spec, parseYuan(budget));
    } catch (error) { setLocalError(error instanceof Error ? error.message : '请核对填写内容。'); }
  };
  const confirm = async () => { const before = s.order?.order_id; await model.confirmOrder(accepted); const order = model.snapshot().order; if (order && order.order_id !== before) void Taro.navigateTo({ url: '/pages/order/index' }); };
  const quote = s.quote;
  const gross = quote ? quote.contract.seller_price_fen + quote.contract.buyer_fee_fen : null;
  const fee = quote && gross !== null ? splitIncludedFee(gross, quote.contract.price.commission_bps) : null;
  return <View className='nb-page'><Button className='text-button' onClick={() => void Taro.navigateBack().catch(() => Taro.redirectTo({ url: '/pages/market/index' }))}><Text>‹ 返回市场</Text></Button><PageTitle>服务详情</PageTitle><Feedback />
    <LoginGate>{!o ? <Empty>请先在市场选择一项服务。</Empty> : <>
      <View className='ticket'><View className='panel'><Text className='resource-title'>{o.device_displays.map(d => d.model_name).join(' / ')}</Text><Text>{o.public_spec.model_id || o.public_spec.workflow_id || '受控GPU工作区'}</Text><Text className='seller-name'>{o.seller_display_name} · 个人卖家</Text><Text className='caption'>挂牌价版 {o.listing_revision} · {modeLabels[o.policy.service_mode]}</Text></View><View className='detail-prices'><PriceBlock price={o.price} /></View></View>
      <View className='ticket panel'><Text className='section-title'>隐私与使用边界</Text><View className='privacy-panel'><Text>{modeLabels[o.policy.service_mode]}</Text><Text>{privacyLabel(o.policy)}</Text></View>
        {o.policy.service_mode === 'E2E' ? <><Text>原始内容由买方可信程序加密后传输。此页面不收集Prompt，也不代替本地网关。</Text><View className='notice'><Text>本地网关未连接</Text><Text className='caption'>可信程序配对尚未开放，加密调用暂不可用。</Text></View><Button className='nb-button' disabled><Text>连接网关并继续</Text></Button></> : <Text>执行设备需要处理任务内容。GPU租赁通过受控工作区使用，不提供设备管理员权限。</Text>}
      </View>
      <View className='ticket panel'><Text className='section-title'>确认用量与预算</Text>
        {o.price.unit === 'AVAILABLE_HOUR' ? <View className='form-field'><Text className='field-label'>本次时长上限（分钟）</Text><Input className='field-input' type='number' aria-label='时长上限分钟' value={minutes} disabled={s.busy || s.pending} onInput={e => change(setMinutes, e.detail.value)} /><Text className='field-help'>最多 {Math.floor(o.public_spec.duration_ms! / 60000)} 分钟；确认就绪后才开始计费。</Text></View> : <>
          <View className='form-field'><Text className='field-label'>输入token上限</Text><Input className='field-input' type='number' aria-label='输入token上限' value={input} disabled={s.busy || s.pending} onInput={e => change(setInput, e.detail.value)} /></View>
          <View className='form-field'><Text className='field-label'>输出token上限</Text><Input className='field-input' type='number' aria-label='输出token上限' value={output} disabled={s.busy || s.pending} onInput={e => change(setOutput, e.detail.value)} /></View></>}
        <View className='form-field'><Text className='field-label'>本次预算上限（元）</Text><Input className='field-input' type='digit' aria-label='预算上限元' value={budget} disabled={s.busy || s.pending} onInput={e => change(setBudget, e.detail.value)} /><Text className='field-help'>预算是授权上限，尚未扣款或冻结。输入和输出分别计量。</Text></View>
        {localError && <View className='notice error' aria-live='polite'>{localError}</View>}
        <Button className='nb-button primary full-button' disabled={s.busy || s.pending || o.availability !== 'AVAILABLE' || o.policy.service_mode === 'E2E'} onClick={() => void makeQuote()}><Text>{s.busy ? '正在核对…' : '获取正式报价'}</Text></Button>
        <Text className='caption'>报价不占用设备、不开始计费。价格已含约定的平台佣金。</Text>
      </View>
      {quote && <View className='ticket panel'><Text className='section-title'>核对正式合同</Text><View className='facts'>
        <View className='fact'><Text>实际卖家</Text><Text>{quote.contract.seller_id}</Text></View><View className='fact'><Text>价版</Text><Text>{quote.contract.listing_revision}</Text></View>
        <View className='fact'><Text>最高预估费用</Text><Text>{yuan(gross!)}</Text></View><View className='fact'><Text>已包含平台佣金</Text><Text>{yuan(fee!.commission)}</Text></View>
        <View className='fact'><Text>卖家预估服务净额</Text><Text>{yuan(fee!.seller)}</Text></View><View className='fact'><Text>预算上限</Text><Text>{yuan(quote.contract.total_budget_fen)}</Text></View>
        <View className='fact'><Text>报价到期</Text><Text>{new Date(quote.expires_at).toLocaleString()}</Text></View></View><Text className='caption'>卖家预估服务净额未扣渠道成本和税费，实际到账以结算账单为准。</Text><Text className='caption'>合同摘要 {quote.contract.contract_hash}</Text>
        <CheckboxGroup onChange={e => setAccepted(e.detail.value.includes('accept'))}><Label className='disclosure'><Checkbox value='accept' checked={accepted} />我确认该个人卖家、价版、预算及隐私说明（{quote.contract.policy.disclosure_version}）。</Label></CheckboxGroup>
        <Button className='nb-button primary full-button' disabled={!accepted || s.busy || s.pending} onClick={() => void confirm()}><Text>确认合同并创建订单</Text></Button><Text className='caption'>创建后进入等待付款。不会显示为支付成功或开始运行。</Text>
      </View>}
    </>}</LoginGate><BottomNav active='market' /></View>;
}
