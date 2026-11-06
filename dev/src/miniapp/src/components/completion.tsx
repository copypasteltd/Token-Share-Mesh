import { useEffect, useState, useSyncExternalStore } from 'react';
import { View,Text,Input,Button } from '@tarojs/components';
import { type AppModel,type Order,yuan } from '@tsm/core';

export function CompletionPanel({model,order}:{model:AppModel;order:Order}) {
 const flow=model.completion,state=useSyncExternalStore(flow.subscribe,flow.snapshot),[job,setJob]=useState('');
 useEffect(()=>{setJob('');flow.bindOrder(order);return()=>flow.bindOrder(null);},[flow,order.order_id]);
 if(state.order!==order.order_id)return null;const bill=state.bill,j=state.job,buyer=model.client.current()?.profile.user_id===order.buyer_id;
 return <View className='ticket panel'><Text className='section-title'>交付与账单</Text>
  {bill&&<><View className='facts'><View className='fact'><Text>已确认成交额</Text><Text>{yuan(bill.gross_fen)}</Text></View><View className='fact'><Text>已计佣金</Text><Text>{yuan(bill.commission_fen)}</Text></View><View className='fact'><Text>卖家收入 · 渠道费用待核对</Text><Text>{yuan(bill.provisional_seller_fen)}</Text></View><View className='fact'><Text>仍保留预算</Text><Text>{yuan(bill.reserved_fen+bill.unverified_fen)}</Text></View></View><Text className='caption'>截至 {new Date(bill.as_of).toLocaleString()}；账单尚未最终确认，实际到账待渠道核对。</Text></>}
  <View className='form-field'><Input className='field-input' placeholder='填写已有任务号' maxlength={128} value={job} onInput={e=>setJob(e.detail.value)} /></View>
  <Button className='nb-button' disabled={state.busy} onClick={()=>void flow.refresh(job||undefined)}>查询交付与账单</Button>
  {j&&<View><Text>{j.job_id} · {j.state}</Text>{j.results.map(d=><Text className='caption' key={d.delivery_id}>{d.delivery_id} · {d.state} · 用量{d.usage_state==='VERIFIED'?'已核验':'待核验'}</Text>)}
   {buyer&&['INPUT_PENDING','QUEUED','RUNNING','VALIDATING','WAITING_BUYER','UNVERIFIED'].includes(j.state)&&<Button className='nb-button' disabled={state.busy||state.pending} onClick={()=>void flow.cancel()}>请求停止此任务</Button>}
   <Text className='caption'>请在接收并核对结果的本地客户端完成签收。停止请求后的用量和清理仍需确认。</Text></View>}
  {state.pending&&<Button className='nb-button' disabled={state.busy} onClick={()=>void flow.recover()}>恢复原任务操作</Button>}{state.error&&<Text role='alert'>{state.error}</Text>}
 </View>;
}
