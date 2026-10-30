import { useMemo, useState } from 'react';
import { View, Input, Button, Text } from '@tarojs/components';
import Taro from '@tarojs/taro';
import { modeLabels, type Mode } from '@tsm/core';
import { Brand, BottomNav, Feedback, LoginGate, ResourceTicket, Empty, useApp } from '../../components';
import { model } from '../../runtime';
export default function Market() {
  const s = useApp(), [search, setSearch] = useState(''), [purpose, setPurpose] = useState('全部');
  const offers = useMemo(() => s.offers.filter(o => {
    const text = [o.seller_display_name, o.public_spec.model_id, o.public_spec.workflow_id, ...o.device_displays.map(d => d.model_name)].join(' ').toLowerCase();
    return text.includes(search.trim().toLowerCase()) && (purpose === '全部' || (purpose === '对话' ? o.price.unit === 'TOKEN_1M' : o.policy.service_mode === 'GPU_RENTAL'));
  }), [s.offers, search, purpose]);
  return <View className='nb-page'><Brand />{process.env.TARO_ENV === 'h5' && <Text className='mini-preview-note'>小程序H5预览 · 原生授权与真机能力另行核验</Text>}<Feedback />
    <LoginGate><Input className='search-input' aria-label='搜索已加载资源' placeholder='搜索型号、模型或卖家' value={search} onInput={e => setSearch(e.detail.value)} />
      <View className='mode-row'>{(Object.keys(modeLabels) as Mode[]).map(mode => <Button key={mode} className={'mode-button ' + (s.mode === mode ? 'selected' : '')} disabled={s.loadingMarket}
        onClick={() => void model.loadMarket(s.mode === mode ? '' : mode)}><Text>{modeLabels[mode]}</Text></Button>)}</View>
      <View className='purpose-row'>{['全部', '对话', '图像', '视频'].map(value => <Button key={value} className={'purpose-button ' + (purpose === value ? 'selected' : '')} onClick={() => setPurpose(value)}><Text>{value}</Text></Button>)}</View>
      {purpose === '图像' || purpose === '视频' ? <Text className='caption'>媒体通过受控GPU租赁使用；具体工作流以服务合同为准。</Text> : null}
      {s.loadingMarket && <Empty>正在核对个人卖家的挂牌…</Empty>}
      {!s.loadingMarket && offers.length === 0 && <Empty>当前没有符合条件的服务。可以清除筛选，或稍后刷新。</Empty>}
      {offers.map(offer => <ResourceTicket key={offer.listing_id} offer={offer} onOpen={() => { if (model.select(offer)) void Taro.navigateTo({ url: '/pages/service/index' }); }} />)}
      <View className='row'><Button className='text-button' disabled={s.loadingMarket} onClick={() => { setSearch(''); setPurpose('全部'); void model.loadMarket(''); }}><Text>清除筛选并刷新</Text></Button>
        {s.nextCursor && <Button className='nb-button' disabled={s.loadingMarket} onClick={() => void model.loadMarket(s.mode, true)}><Text>加载更多</Text></Button>}</View>
      <Text className='caption'>搜索范围为当前已加载资源。每笔订单均由选定的个人卖家履约。</Text>
    </LoginGate><BottomNav active='market' /></View>;
}
