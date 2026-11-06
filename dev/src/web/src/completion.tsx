import { useEffect, useState, useSyncExternalStore } from 'react';
import { type AppModel, type Order, yuan } from '@tsm/core';

export function CompletionPanel({model,order}:{model:AppModel;order:Order}) {
 const flow=model.completion,state=useSyncExternalStore(flow.subscribe,flow.snapshot),[job,setJob]=useState('');
 useEffect(()=>{setJob('');flow.bindOrder(order);return()=>flow.bindOrder(null);},[flow,order.order_id]);
 if(state.order!==order.order_id)return null;const bill=state.bill,j=state.job,buyer=model.client.current()?.profile.user_id===order.buyer_id;
 return <section className='white-panel' aria-label='交付与账单'><h3>交付与账单</h3>
  {bill&&<><dl className='facts'><dt>已确认成交额</dt><dd>{yuan(bill.gross_fen)}</dd><dt>已计佣金</dt><dd>{yuan(bill.commission_fen)}</dd><dt>卖家收入 · 渠道费用待核对</dt><dd>{yuan(bill.provisional_seller_fen)}</dd><dt>仍保留预算</dt><dd>{yuan(bill.reserved_fen+bill.unverified_fen)}</dd></dl><p className='muted'>截至 {new Date(bill.as_of).toLocaleString()}；账单尚未最终确认，实际到账待渠道核对。</p></>}
  <label className='field'>已有任务号<input value={job} maxLength={128} onChange={e=>setJob(e.target.value)} /></label>
  <button className='button' disabled={state.busy} onClick={()=>void flow.refresh(job||undefined)}>查询交付与账单</button>
  {j&&<article><p className='break'>{j.job_id} · {j.state}</p>{j.results.map(d=><p key={d.delivery_id}>{d.delivery_id} · {d.state} · 用量{d.usage_state==='VERIFIED'?'已核验':'待核验'}</p>)}
   {buyer&&['INPUT_PENDING','QUEUED','RUNNING','VALIDATING','WAITING_BUYER','UNVERIFIED'].includes(j.state)&&<button className='button' disabled={state.busy||state.pending} onClick={()=>void flow.cancel()}>请求停止此任务</button>}
   <p>请在接收并核对结果的本地客户端完成签收。停止请求后的用量和清理仍需确认。</p></article>}
  {state.pending&&<button className='button' disabled={state.busy} onClick={()=>void flow.recover()}>恢复原任务操作</button>}{state.error&&<p role='alert'>{state.error}</p>}
 </section>;
}
