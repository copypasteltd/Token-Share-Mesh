import React, { useEffect, useState, useSyncExternalStore } from 'react';
import { type AppModel, type Order, type RefundReason, refundReasons, refundReviewLabel, refundFinancialLabel, yuan } from '@tsm/core';

export function RefundPanel({ model, order }: { model: AppModel; order: Order }) {
 const flow = model.aftersale, state = useSyncExternalStore(flow.subscribe,flow.snapshot), session = model.client.current();
 const [amount,setAmount] = useState(''), [reason,setReason] = useState<RefundReason>('NO_LONGER_NEEDED');
 useEffect(() => { setAmount(''); setReason('NO_LONGER_NEEDED'); flow.select(order); void flow.refresh(); return () => flow.select(null); },[flow,order,session]);
 const fen = /^[1-9]\d{0,3}$/.test(amount) ? Number(amount) : 0;
 if (state.order !== order.order_id) return null;
 return <section aria-label='原订单售后'><h3>原订单售后</h3><p>申请由独立金融审核人核对。审核状态和渠道资金状态分别展示。</p>
  {session?.profile.user_id === order.buyer_id && <div><label className='field'>申请金额（分，最高 5000 分）<input inputMode='numeric' value={amount} maxLength={4} onChange={e => setAmount(e.target.value)} /></label>
   <label className='field'>申请原因<select value={reason} onChange={e => setReason(e.target.value as RefundReason)}>{(Object.keys(refundReasons) as RefundReason[]).map(r => <option key={r} value={r}>{refundReasons[r]}</option>)}</select></label>
   <button className='button' disabled={state.busy || state.pending || fen < 1 || fen > 5000} onClick={() => void flow.apply(reason,fen)}>提交 {fen > 0 ? yuan(fen) : ''} 退款申请</button></div>}
  {state.items.map(v => <article key={v.refund_id}><h4 className='break'>{v.refund_id}</h4><p>{refundReviewLabel(v)}</p><p>{refundFinancialLabel(v)}</p><p>申请 {yuan(v.requested_fen)} · 已确认退回 {yuan(v.confirmed_fen)}</p>{v.review_required && <p>原退款需要独立复核，请保留原退款号。</p>}</article>)}
  {!state.items.length && <p>暂无已返回的售后申请。</p>}{state.notice && <p role='status'>{state.notice}</p>}{state.error && <p role='alert'>{state.error}</p>}
  {state.pending && <button className='button' disabled={state.busy} onClick={() => void flow.recover()}>恢复原售后操作</button>}
  <button className='button' disabled={state.busy} onClick={() => void flow.refresh()}>刷新售后状态</button>{state.cursor && <button className='button' disabled={state.busy} onClick={() => void flow.refresh(true)}>加载更多申请</button>}
 </section>;
}
export function RefundReviewDesk({ model }: { model: AppModel }) {
 const flow = model.aftersale, state = useSyncExternalStore(flow.subscribe,flow.snapshot), session = model.client.current();
 const [route,setRoute] = useState(''), [maximum,setMaximum] = useState(''), [confirmed,setConfirmed] = useState(false);
 useEffect(() => { setRoute(''); setMaximum(''); setConfirmed(false); },[state.review,session]);
 if (!session?.profile.permission_scope_ids.includes('perm_refund_review')) return null;
 const review = state.review, max = /^[1-9]\d{0,3}$/.test(maximum) ? Number(maximum) : 0;
 return <section className='white-panel' aria-label='独立退款审核'><h2>独立退款审核</h2><p>仅显示原合同与资金元数据，不提供客户生成内容。买卖双方不能审核自己的订单。</p>
  <button className='button' disabled={state.busy} onClick={() => void flow.reviews()}>查询审核队列</button>
  {state.queue.map(v => <article key={v.refund_id}><p className='break'>{v.refund_id} · {yuan(v.requested_fen)}</p><p>{refundReviewLabel(v)} · {refundFinancialLabel(v)}</p><button className='button' disabled={state.busy} onClick={() => void flow.openReview(v.refund_id)}>核对申请</button></article>)}
  {state.queueCursor && <button className='button' disabled={state.busy} onClick={() => void flow.reviews(true)}>加载更多</button>}
  {review && <article><h3>核对原申请</h3><dl className='facts'><dt>原订单</dt><dd className='break'>{review.order_id}</dd><dt>买家 / 卖家</dt><dd>{review.buyer_id} / {review.seller_id}</dd><dt>实收 / 申请</dt><dd>{yuan(review.collected_fen)} / {yuan(review.requested_fen)}</dd><dt>已占退款额度</dt><dd>{yuan(review.held_refund_fen)}</dd><dt>未消费累计上限</dt><dd>{yuan(review.maximum_refundable_fen)}</dd></dl>
   {review.review_state === 'PENDING' && <><label className='field'>原退款路线<select value={route} onChange={e => { setRoute(e.target.value); setConfirmed(false); }}><option value=''>请选择已登记原路线</option>{review.refund_route_refs.map(r => <option value={r} key={r}>{r}</option>)}</select></label>
    <label className='field'>批准累计退款上限（分）<input inputMode='numeric' maxLength={4} value={maximum} onChange={e => { setMaximum(e.target.value); setConfirmed(false); }} /></label>
    <label><input type='checkbox' checked={confirmed} onChange={e => setConfirmed(e.target.checked)} />我已核对原交易、未消费价款、执行清理及本次申请</label>
    <div><button className='button primary' disabled={state.busy || state.pending || !confirmed || !route || max < review.held_refund_fen + review.requested_fen || max > review.maximum_refundable_fen} onClick={() => void flow.decide(true,route,max)}>批准并保存原退款意图</button>
    <button className='button' disabled={state.busy || state.pending} onClick={() => void flow.decide(false)}>证据不足，拒绝本次申请</button></div></>}
   {review.review_state === 'APPROVED' && <button className='button' disabled={state.busy || state.pending} onClick={() => void flow.revoke()}>撤销批准，保留已产生的退款义务</button>}
  </article>}
  {state.notice && <p role='status'>{state.notice}</p>}{state.error && <p role='alert'>{state.error}</p>}{state.pending && <button className='button' disabled={state.busy} onClick={() => void flow.recover()}>恢复原审核操作</button>}
 </section>;
}
