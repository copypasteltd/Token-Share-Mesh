import { useEffect, useState, useSyncExternalStore } from 'react';
import Taro from '@tarojs/taro';
import { View,Text,Input,Button,Textarea } from '@tarojs/components';
import { type AppModel,type Order,yuan } from '@tsm/core';

export function CompletionPanel({model,order}:{model:AppModel;order:Order}) {
 const flow=model.completion,state=useSyncExternalStore(flow.subscribe,flow.snapshot),[job,setJob]=useState(''),[review,setReview]=useState(''),owner=model.client.current()?.profile.user_id;
 useEffect(()=>{setJob('');setReview('');flow.bindOrder(order);return()=>flow.bindOrder(null);},[flow,order.order_id,owner]);
 if(state.order!==order.order_id)return null;const bill=state.bill,j=state.job,buyer=model.client.current()?.profile.user_id===order.buyer_id;
 return <View className='ticket panel'><Text className='section-title'>交付与账单</Text>
  {bill&&<><View className='facts'><View className='fact'><Text>已确认成交额</Text><Text>{yuan(bill.gross_fen)}</Text></View><View className='fact'><Text>已计佣金</Text><Text>{yuan(bill.commission_fen)}</Text></View><View className='fact'><Text>卖家收入 · 渠道费用待核对</Text><Text>{yuan(bill.provisional_seller_fen)}</Text></View><View className='fact'><Text>仍保留预算</Text><Text>{yuan(bill.reserved_fen+bill.unverified_fen)}</Text></View></View><Text className='caption'>截至 {new Date(bill.as_of).toLocaleString()}；账单尚未最终确认，实际到账待渠道核对。</Text></>}
  <View className='form-field'><Input className='field-input' placeholder='填写已有任务号' maxlength={128} value={job} onInput={e=>setJob(e.detail.value)} /></View>
  <Button className='nb-button' disabled={state.busy} onClick={()=>void flow.refresh(job||undefined)}>查询交付与账单</Button>
  {j&&<View><Text>{j.job_id} · {j.state}</Text>{j.results.map(d=><View key={d.delivery_id}><Text className='caption'>{d.delivery_id} · {d.state} · 用量{d.usage_state==='VERIFIED'?'已核验':'待核验'}</Text>{buyer&&d.content?.state==='READY'&&d.attempt_id===j.current_attempt_id&&['WAITING_BUYER','VALID'].includes(d.state)&&<Button className='nb-button' disabled={state.busy} onClick={()=>void flow.createReadPacket(d.delivery_id)}>获取此结果的读取凭据</Button>}</View>)}
   {buyer&&['INPUT_PENDING','QUEUED','RUNNING','VALIDATING','WAITING_BUYER','UNVERIFIED'].includes(j.state)&&<Button className='nb-button' disabled={state.busy||state.pending} onClick={()=>void flow.cancel()}>请求停止此任务</Button>}
   {state.readPacket&&<View><Text className='caption'>本地接收凭据 · 仅可读取此结果</Text><Textarea className='field-input' disabled value={state.readPacket} maxlength={8192}/><Text className='caption'>五分钟内有效，退出当前登录后失效。仅交给你信任的本地接收程序。</Text><Button className='nb-button' onClick={()=>void Taro.setClipboardData({data:state.readPacket}).catch(()=>Taro.showToast({title:'复制未完成，请重试',icon:'none'}))}>复制读取凭据</Button><Button className='nb-button' onClick={()=>flow.clearReadPacket()}>隐藏凭据</Button></View>}
   {buyer&&j.state==='WAITING_BUYER'&&<View><Text className='caption'>本地核对记录</Text><Textarea className='field-input' value={review} maxlength={4096} placeholder='粘贴接收程序生成的核对记录' onInput={e=>setReview(e.detail.value)}/><Button className='nb-button' disabled={state.busy||state.pending||!review} onClick={()=>void flow.decideFromJSON(review,true)}>确认签收</Button><Button className='nb-button' disabled={state.busy||state.pending||!review} onClick={()=>void flow.decideFromJSON(review,false)}>拒绝此结果</Button></View>}
   <Text className='caption'>先在本地接收并核对完整结果，再明确选择签收或拒绝。费用仍须独立计量核验；停止请求后的用量和清理也需确认。</Text></View>}
  {state.pending&&<Button className='nb-button' disabled={state.busy} onClick={()=>void flow.recover()}>恢复原任务操作</Button>}{state.error&&<Text role='alert'>{state.error}</Text>}
 </View>;
}
