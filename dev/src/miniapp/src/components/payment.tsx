import { useEffect, useSyncExternalStore } from 'react';
import { View, Text, Button } from '@tarojs/components';
import { type AppModel, type Order, paymentStateLabel, recoveryStateLabel, yuan } from '@tsm/core';

export function PaymentPanel({ model, order }: { model: AppModel; order: Order }) {
  const flow = model.checkout, state = useSyncExternalStore(flow.subscribe, flow.snapshot);
  const session = model.client.current();
  useEffect(() => {
    flow.select(order); void flow.refresh();
    return () => flow.select(null);
  }, [flow, order, session]);
  if (state.order !== order.order_id) return null;
  return <View className='ticket panel'><Text className='section-title'>原订单付款</Text>
    <Text className='caption'>付款对象为本单个人卖家。创建账单和提交凭据不会自动开始执行或计时。</Text>
    {state.payment && <View className='facts'>
      <View className='fact'><Text>付款状态</Text><Text>{paymentStateLabel(state.payment.state)}</Text></View>
      <View className='fact'><Text>原付款号</Text><Text className='order-id'>{state.payment.payment_id}</Text></View>
      <View className='fact'><Text>原合同金额</Text><Text>{yuan(state.payment.amount_fen)}</Text></View>
      <View className='fact'><Text>渠道环境</Text><Text>{state.payment.environment === 'SANDBOX' ? '沙箱，不是真实收单' : '正式渠道'}</Text></View>
    </View>}
    {state.recovery && <Text>{recoveryStateLabel(state.recovery.state)}</Text>}
    {state.options && <View className='form-field'><Text>核对金额 {yuan(state.options.amount_fen)} 后，选择原卖家付款方式：</Text>
      {state.options.items.map(option => <Button className='nb-button full-button' key={option.channel_capability_ref} disabled={state.busy}
        onClick={() => void flow.prepare(option.channel_capability_ref)}><Text>建立原账单 · {option.goods_name}</Text></Button>)}
    </View>}
    {state.originalCreatePending && <Button className='nb-button full-button' disabled={state.busy} onClick={() => void flow.prepare()}><Text>恢复原账单请求</Text></Button>}
    {state.payment?.state === 'PENDING' && <View className='form-field'>
      <Button className='nb-button primary full-button' disabled={state.busy || !state.launchAvailable} onClick={() => void flow.launch()}><Text>核对并发起原付款</Text></Button>
      {!state.launchAvailable && <Text className='caption'>当前端未提供获准的付款调用能力，或原付款已发起待核对。请保留原付款号。</Text>}
    </View>}
    {state.notice && <Text className='caption'>{state.notice}</Text>}
    {state.error && <Text role='alert'>{state.error}</Text>}
    <Button className='nb-button full-button' disabled={state.busy} onClick={() => void flow.refresh()}><Text>{state.busy ? '正在核对…' : '查询原付款与凭据状态'}</Text></Button>
  </View>;
}
