import { View, Text, Button } from '@tarojs/components';
import { contractOf, yuan, stateLabel, modeLabels, privacyLabel } from '@tsm/core';
import { BottomNav, Feedback, LoginGate, PageTitle, PriceBlock, Empty, useApp } from '../../components';
import { model } from '../../runtime';
export default function OrderDetail() {
  const s = useApp(), o = s.order;
  let contract: ReturnType<typeof contractOf> | null = null;
  try { if (o) contract = contractOf(o.contract); } catch { /* An historical contract is never silently upgraded. */ }
  return <View className='nb-page'><PageTitle>订单详情</PageTitle><Feedback /><LoginGate>
    {!o ? <Empty>请先查询一笔本人订单。</Empty> : <>
      <View className='ticket'><Text className='status-bar'>{stateLabel(o.state)}</Text><View className='panel'><Text className='order-id'>{o.order_id}</Text><Text className='seller-name'>个人卖家 {o.seller_id}</Text><Text className='caption'>以此订单保存的合同为准，挂牌的新价版不会修改本单。</Text></View>{contract && <View className='detail-prices'><PriceBlock price={contract.price} /></View>}</View>
      {contract ? <View className='ticket panel'><Text className='section-title'>合同与费用</Text><View className='facts'>
        <View className='fact'><Text>模式</Text><Text>{modeLabels[contract.policy.service_mode]}</Text></View><View className='fact'><Text>隐私</Text><Text>{privacyLabel(contract.policy)}</Text></View>
        <View className='fact'><Text>最高预估费用</Text><Text>{yuan(contract.seller_price_fen + contract.buyer_fee_fen)}</Text></View><View className='fact'><Text>预算上限</Text><Text>{yuan(contract.total_budget_fen)}</Text></View>
        <View className='fact'><Text>当前确认用量</Text><Text>暂无已确认用量</Text></View><View className='fact'><Text>最终费用</Text><Text>待确认</Text></View></View><Text className='caption'>合同摘要 {contract.contract_hash}</Text></View> : <Empty>该订单使用历史合同版本，请保留原合同并联系支持核对。</Empty>}
      <View className='ticket panel'><Text className='section-title'>下一步</Text><Text>{o.state === 'WAITING_PAYMENT' ? '订单已创建，尚未付款，尚未开始计时。当前付款入口未开放。' : '当前页面仅展示已返回的订单状态。运行、计量与停止状态等待相应服务确认。'}</Text>
        <View className='form-field'><Button className='nb-button primary full-button' disabled><Text>付款入口暂未开放</Text></Button></View><Button className='nb-button full-button' disabled={s.busy} onClick={() => void model.readOrder(o.order_id)}><Text>刷新订单状态</Text></Button></View>
    </>}
  </LoginGate><BottomNav active='orders' /></View>;
}
