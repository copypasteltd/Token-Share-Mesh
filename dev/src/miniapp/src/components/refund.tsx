import { useEffect, useState, useSyncExternalStore } from 'react';
import { View, Text, Button, Input, Picker } from '@tarojs/components';
import { type AppModel, type Order, type RefundReason, refundReasons, refundReviewLabel, refundFinancialLabel, yuan } from '@tsm/core';

export function RefundPanel({ model, order }: { model: AppModel; order: Order }) {
 const flow = model.aftersale, state = useSyncExternalStore(flow.subscribe, flow.snapshot), session = model.client.current();
 const [amount,setAmount] = useState(''), [reason,setReason] = useState<RefundReason>('NO_LONGER_NEEDED');
 useEffect(() => { setAmount(''); setReason('NO_LONGER_NEEDED'); flow.select(order); void flow.refresh(); return () => flow.select(null); }, [flow,order,session]);
 const reasons = Object.keys(refundReasons) as RefundReason[], fen = /^[1-9]\d{0,3}$/.test(amount) ? Number(amount) : 0;
 if (state.order !== order.order_id) return null;
 return <View className='ticket panel'><Text className='section-title'>原订单售后</Text>
  <Text className='caption'>申请由独立金融审核人核对。审核状态和渠道资金状态分别展示。</Text>
  {session?.profile.user_id === order.buyer_id && <View className='form-field'>
   <Text>申请金额（分，最高 5000 分）</Text><Input type='number' value={amount} maxlength={4} onInput={e => setAmount(e.detail.value)} />
   <Picker mode='selector' range={reasons.map(r => refundReasons[r])} value={reasons.indexOf(reason)} onChange={e => { const next = reasons[Number(e.detail.value)]; if (next) setReason(next); }}><Text>申请原因：{refundReasons[reason]}</Text></Picker>
   <Button className='nb-button full-button' disabled={state.busy || state.pending || fen < 1 || fen > 5000} onClick={() => void flow.apply(reason,fen)}><Text>提交 {fen > 0 ? yuan(fen) : ''} 退款申请</Text></Button>
  </View>}
  {state.items.map(v => <View className='ticket panel' key={v.refund_id}><Text className='order-id'>{v.refund_id}</Text><Text>{refundReviewLabel(v)}</Text><Text>{refundFinancialLabel(v)}</Text><Text>申请 {yuan(v.requested_fen)} · 已确认退回 {yuan(v.confirmed_fen)}</Text>{v.review_required && <Text>原退款需要独立复核，请保留原退款号。</Text>}</View>)}
  {!state.items.length && <Text className='caption'>暂无已返回的售后申请。</Text>}
  {state.notice && <Text>{state.notice}</Text>}{state.error && <Text role='alert'>{state.error}</Text>}
  {state.pending && <Button className='nb-button full-button' disabled={state.busy} onClick={() => void flow.recover()}><Text>恢复原售后操作</Text></Button>}
  <Button className='nb-button full-button' disabled={state.busy} onClick={() => void flow.refresh()}><Text>刷新售后状态</Text></Button>
  {state.cursor && <Button className='nb-button full-button' disabled={state.busy} onClick={() => void flow.refresh(true)}><Text>加载更多申请</Text></Button>}
 </View>;
}
