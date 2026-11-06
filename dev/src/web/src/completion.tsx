import { useEffect, useState, useSyncExternalStore } from 'react';
import { type AppModel, type Order, yuan } from '@tsm/core';

export function CompletionPanel({model,order}:{model:AppModel;order:Order}) {
 const flow=model.completion,state=useSyncExternalStore(flow.subscribe,flow.snapshot),[job,setJob]=useState(''),[review,setReview]=useState(''),owner=model.client.current()?.profile.user_id;
 useEffect(()=>{setJob('');setReview('');flow.bindOrder(order);return()=>flow.bindOrder(null);},[flow,order.order_id,owner]);
 if(state.order!==order.order_id)return null;const bill=state.bill,j=state.job,buyer=model.client.current()?.profile.user_id===order.buyer_id;
 return <section className='white-panel' aria-label='交付与账单'><h3>交付与账单</h3>
  {bill&&<><dl className='facts'><dt>已确认成交额</dt><dd>{yuan(bill.gross_fen)}</dd><dt>已计佣金</dt><dd>{yuan(bill.commission_fen)}</dd><dt>卖家收入 · 渠道费用待核对</dt><dd>{yuan(bill.provisional_seller_fen)}</dd><dt>仍保留预算</dt><dd>{yuan(bill.reserved_fen+bill.unverified_fen)}</dd></dl><p className='muted'>截至 {new Date(bill.as_of).toLocaleString()}；账单尚未最终确认，实际到账待渠道核对。</p></>}
  <label className='field'>已有任务号<input value={job} maxLength={128} onChange={e=>setJob(e.target.value)} /></label>
  <button className='button' disabled={state.busy} onClick={()=>void flow.refresh(job||undefined)}>查询交付与账单</button>
  {j&&<article><p className='break'>{j.job_id} · {j.state}</p>{j.results.map(d=><div key={d.delivery_id}><p>{d.delivery_id} · {d.state} · 用量{d.usage_state==='VERIFIED'?'已核验':'待核验'}</p>{buyer&&d.content?.state==='READY'&&d.attempt_id===j.current_attempt_id&&['WAITING_BUYER','VALID'].includes(d.state)&&<button className='button' disabled={state.busy} onClick={()=>void flow.createReadPacket(d.delivery_id,model.client.base||window.location.origin)}>获取此结果的读取凭据</button>}</div>)}
   {buyer&&['INPUT_PENDING','QUEUED','RUNNING','VALIDATING','WAITING_BUYER','UNVERIFIED'].includes(j.state)&&<button className='button' disabled={state.busy||state.pending} onClick={()=>void flow.cancel()}>请求停止此任务</button>}
   {state.readPacket&&<><label className='field'>本地接收凭据 · 仅可读取此结果<textarea readOnly value={state.readPacket}/></label><p className='muted'>五分钟内有效，退出当前登录后失效。仅交给你信任的本地接收程序。</p><button className='button' onClick={()=>flow.clearReadPacket()}>隐藏凭据</button></>}
   {buyer&&j.state==='WAITING_BUYER'&&<><label className='field'>本地核对记录<textarea value={review} maxLength={4096} placeholder='粘贴接收程序生成的核对记录' onChange={e=>setReview(e.target.value)}/></label><button className='button' disabled={state.busy||state.pending||!review} onClick={()=>void flow.decideFromJSON(review,true)}>确认签收</button><button className='button' disabled={state.busy||state.pending||!review} onClick={()=>void flow.decideFromJSON(review,false)}>拒绝此结果</button></>}
   <p>先在本地接收并核对完整结果，再明确选择签收或拒绝。费用仍须独立计量核验；停止请求后的用量和清理也需确认。</p></article>}
  {state.pending&&<button className='button' disabled={state.busy} onClick={()=>void flow.recover()}>恢复原任务操作</button>}{state.error&&<p role='alert'>{state.error}</p>}
 </section>;
}
