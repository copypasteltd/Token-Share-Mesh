import { useSyncExternalStore, type PropsWithChildren } from 'react';
import { View, Text, Button, Image } from '@tarojs/components';
import Taro from '@tarojs/taro';
import { type Offer, type Price, modeLabels, privacyLabel, rateText, stateLabel } from '@tsm/core';
import mesh from '@tsm/core/assets/mesh.svg';
import gpu from '@tsm/core/assets/gpu.svg';
import phone from '@tsm/core/assets/phone.svg';
import { model } from './runtime';
export const useApp = () => useSyncExternalStore(model.subscribe, model.snapshot, model.snapshot);
export function Brand({ compact = false }: { compact?: boolean }) {
  return <View className={'brand ' + (compact ? 'compact' : '')}><Image src={mesh} className='brand-mark' mode='aspectFit' /><View><Text className='brand-title'>算力共享</Text>{!compact && <Text className='brand-subtitle'>闲置设备，连接更多可能</Text>}</View></View>;
}
export function BottomNav({ active }: { active: string }) {
  const entries = [['market', '发现', '⌕'], ['orders', '订单', '▤'], ['share', '共享', '◇'], ['account', '我的', '○']];
  return <View className='bottom-nav' aria-label='主导航'>{entries.map(([id, name, icon]) => <Button key={id} className={'nav-item ' + (active === id ? 'selected' : '')} aria-label={name} aria-current={active === id ? 'page' : undefined}
    onClick={() => { if (active !== id) void Taro.redirectTo({ url: '/pages/' + id + '/index' }); }}><Text className='nav-icon'>{icon}</Text><Text>{name}</Text></Button>)}</View>;
}
export function Feedback() {
  const s = useApp();
  return <>{s.error && <View className='notice error' aria-live='polite'><Text>{s.error}</Text><Button className='text-button' onClick={model.clearError}><Text>收起提示</Text></Button></View>}
    {s.pending && <View className='notice'><Text>有一笔请求的结果待确认，请先恢复原请求。</Text><Button className='nb-button' disabled={s.busy} onClick={() => void model.recover()}><Text>核对原请求</Text></Button></View>}</>;
}
export function LoginGate({ children }: PropsWithChildren) {
  const s = useApp();
  if (s.session) return <>{children}</>;
  return <View className='ticket login-ticket'><Text className='section-title'>{s.ready ? '从一台个人设备开始' : '正在核对登录状态'}</Text><Text>登录后查看个人卖家的GPU与手机服务，核对卖家、费用和隐私，再决定是否使用。</Text>
    <Button className='nb-button primary' disabled={!s.ready || s.busy} onClick={() => void model.login()}><Text>{s.busy ? '正在准备授权…' : '支付宝授权登录'}</Text></Button><Text className='caption'>登录不会产生费用，也不会自动开启设备共享。</Text></View>;
}
export function PriceBlock({ price, compact = false }: { price: Price; compact?: boolean }) {
  return <View className={'price-block ' + (price.unit === 'TOKEN_1M' ? 'price-dual ' : '') + (compact ? 'price-compact' : '')}>{price.unit === 'AVAILABLE_HOUR' ? <><Text className='price-number'>{rateText(price.hour_rate!)}</Text><Text className='price-unit'>/ 小时</Text></> : <>
    <View className='dual-price'><Text className='price-label'>输入</Text><Text className='price-number'>{rateText(price.input_rate!)}</Text><Text className='price-unit'>/ M token</Text></View>
    <View className='dual-price'><Text className='price-label'>输出</Text><Text className='price-number'>{rateText(price.output_rate!)}</Text><Text className='price-unit'>/ M token</Text></View></>}</View>;
}
export function ResourceTicket({ offer, onOpen }: { offer: Offer; onOpen: () => void }) {
  const rental = offer.policy.service_mode === 'GPU_RENTAL';
  return <View data-listing-id={offer.listing_id} className={'ticket resource-ticket ' + (rental ? 'gpu-ticket' : 'phone-ticket')}><View className='resource-upper'><View className='resource-info'><Image className='device-art' mode='aspectFit' src={rental ? gpu : phone} aria-label='设备类型示意图，非卖家实拍' />
    <View className='resource-copy'><Text className='resource-title'>{offer.device_displays.map(d => d.model_name).join(' / ')}</Text><Text className='resource-model'>{offer.public_spec.model_id || offer.public_spec.workflow_id || '受控GPU工作区'}</Text></View></View><PriceBlock price={offer.price} compact={!rental} /></View>
    <View className='ticket-footer'><View className='seller'><Text className='avatar'>{offer.seller_display_name.slice(0, 1)}</Text><View><Text className='seller-name'>{offer.seller_display_name}</Text><Text className='caption'>个人卖家</Text></View></View>
      <View className='offer-tags'><Text>{modeLabels[offer.policy.service_mode]}</Text><Text className='caption'>{privacyLabel(offer.policy)}</Text><Text className='caption'>{stateLabel(offer.availability)}</Text></View>
      <Button className='nb-button selected' onClick={onOpen}><Text>查看服务 ›</Text></Button></View></View>;
}
export function PageTitle({ children }: PropsWithChildren) { return <Text className='page-title'>{children}</Text>; }
export function Empty({ children }: PropsWithChildren) { return <View className='ticket empty'><Text>{children}</Text></View>; }
