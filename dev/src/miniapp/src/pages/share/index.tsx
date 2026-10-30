import { useEffect, useRef } from 'react';
import { View, Text, Button } from '@tarojs/components';
import { stateLabel } from '@tsm/core';
import { BottomNav, Feedback, LoginGate, PageTitle, PriceBlock, Empty, useApp } from '../../components';
import { model } from '../../runtime';
import { ProviderDevices } from '../../provider';
export default function Share() {
  const s = useApp();
  const attempted = useRef<string | null>(null);
  useEffect(() => {
    const owner = s.session?.profile.user_id;
    if (!owner) { attempted.current = null; return; }
    if (!s.busy && !s.sellerLoaded && attempted.current !== owner) {
      attempted.current = owner; void model.loadSeller();
    }
  }, [s.busy, s.sellerLoaded, s.session?.profile.user_id]);
  return <View className='nb-page'><PageTitle>我的共享</PageTitle><Feedback /><LoginGate>
    <View className='ticket panel'><Text className='section-title'>{s.seller?.display_name || '让闲置设备自主接单'}</Text><Text>设备由你持有并自主决定共享条件。平台提供撮合服务，不承诺保底收入。</Text><View className='facts'>
      <View className='fact'><Text>卖家资格</Text><Text>{s.seller ? stateLabel(s.seller.qualification_state) : '尚未取得'}</Text></View><View className='fact'><Text>收款资格</Text><Text>{s.seller ? stateLabel(s.seller.payee_eligibility_state) : '待核验'}</Text></View>
      <View className='fact'><Text>已结算收入</Text><Text>暂无已确认账单</Text></View></View><Button className='nb-button' disabled={s.busy} onClick={() => void model.loadSeller()}><Text>刷新本人共享状态</Text></Button></View>
    {!s.listings.length && <Empty>暂无可展示的本人挂牌。完成个人卖家准入及设备配对后，才能设置价格并申请上架。</Empty>}
    {s.listings.map(listing => <View className='ticket' key={listing.listing_id}><View className='panel'><Text className='resource-title'>{listing.definition.title}</Text><Text>{stateLabel(listing.state)} · 修订 {listing.revision}</Text><Text className='caption'>本人设备编号：{listing.definition.device_ids.join(' / ')}</Text></View><View className='detail-prices'><PriceBlock price={listing.definition.price} /></View></View>)}
    <ProviderDevices />
    </LoginGate><BottomNav active='share' /></View>;
}
