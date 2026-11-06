import React, { useEffect, useSyncExternalStore } from 'react';
import { type AppModel, type Order, paymentStateLabel, recoveryStateLabel, yuan } from '@tsm/core';

export function PaymentPanel({ model, order }: { model: AppModel; order: Order }) {
  const flow = model.checkout, state = useSyncExternalStore(flow.subscribe, flow.snapshot);
  const session = model.client.current();
  useEffect(() => {
    flow.select(order); void flow.refresh();
    return () => flow.select(null);
  }, [flow, order, session]);
  if (state.order !== order.order_id) return null;
  return <section aria-label='原订单付款'><h3>原订单付款</h3>
    <p className='muted'>付款对象为本单个人卖家。创建账单和提交凭据不会自动开始执行或计时。</p>
    {state.payment && <dl className='facts'><dt>付款状态</dt><dd>{paymentStateLabel(state.payment.state)}</dd>
      <dt>原付款号</dt><dd className='break'>{state.payment.payment_id}</dd><dt>原合同金额</dt><dd>{yuan(state.payment.amount_fen)}</dd>
      <dt>渠道环境</dt><dd>{state.payment.environment === 'SANDBOX' ? '沙箱，不是真实收单' : '正式渠道'}</dd></dl>}
    {state.recovery && <p>{recoveryStateLabel(state.recovery.state)}</p>}
    {state.options && <div><p>核对金额 {yuan(state.options.amount_fen)} 后，选择原卖家付款方式：</p>
      {state.options.items.map(option => <button className='button' key={option.channel_capability_ref} disabled={state.busy}
        onClick={() => void flow.prepare(option.channel_capability_ref)}>建立原账单 · {option.goods_name}</button>)}
    </div>}
    {state.originalCreatePending && <button className='button' disabled={state.busy} onClick={() => void flow.prepare()}>恢复原账单请求</button>}
    {state.payment?.state === 'PENDING' && <div><button className='button primary' disabled={state.busy || !state.launchAvailable} onClick={() => void flow.launch()}>核对并发起原付款</button>
      {!state.launchAvailable && <p className='muted'>当前端未提供获准的付款调用能力，或原付款已发起待核对。请保留原付款号。</p>}</div>}
    {state.notice && <p className='notice'>{state.notice}</p>}{state.error && <p role='alert'>{state.error}</p>}
    <button className='button' disabled={state.busy} onClick={() => void flow.refresh()}>{state.busy ? '正在核对…' : '查询原付款与凭据状态 ↻'}</button>
  </section>;
}
