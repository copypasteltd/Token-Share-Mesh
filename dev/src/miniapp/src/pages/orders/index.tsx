import { useState } from 'react';
import { View, Text, Input, Button } from '@tarojs/components';
import Taro from '@tarojs/taro';
import { BottomNav, Feedback, LoginGate, PageTitle, useApp, Empty } from '../../components';
import { model } from '../../runtime';
export default function Orders() {
  const s = useApp(), [id, setId] = useState('');
  const open = async (value: string) => { await model.readOrder(value); if (model.snapshot().order?.order_id === value && !model.snapshot().error) void Taro.navigateTo({ url: '/pages/order/index' }); };
  return <View className='nb-page'><PageTitle>我的订单</PageTitle><Feedback /><LoginGate>
    <View className='ticket panel'><Text className='section-title'>按订单号查询</Text><Text className='caption'>只返回当前账号有权查看的订单。</Text><View className='form-field'><Input className='field-input' aria-label='订单号' placeholder='填写完整订单号' maxlength={128} value={id} onInput={e => setId(e.detail.value)} /></View><Button className='nb-button primary' disabled={s.busy || !id} onClick={() => void open(id)}><Text>查询订单</Text></Button></View>
    <Text className='section-title'>本机最近记录</Text><Text className='caption'>这里仅记录此设备上查看过的订单，不代表全部历史订单。</Text>
    {!s.recentIds.length && <Empty>暂无本机订单记录。可从市场查看服务，或填写已有订单号查询。</Empty>}
    {s.recentIds.map(value => <Button key={value} className='nb-button history-item' disabled={s.busy} onClick={() => void open(value)}><Text>{value} ›</Text></Button>)}
    </LoginGate><BottomNav active='orders' /></View>;
}
