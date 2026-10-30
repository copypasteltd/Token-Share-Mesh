import { View, Text, Button } from '@tarojs/components';
import { BottomNav, Feedback, LoginGate, PageTitle, useApp } from '../../components';
import { model } from '../../runtime';
export default function Account() {
  const s = useApp();
  return <View className='nb-page'><PageTitle>我的账号</PageTitle><Feedback /><LoginGate>
    <View className='ticket panel'><Text className='section-title'>账号与权限</Text><View className='facts'><View className='fact'><Text>账号</Text><Text>{s.session?.profile.user_id}</Text></View>
      <View className='fact'><Text>当前身份</Text><Text>{s.session?.profile.roles.map(r => ({ BUYER: '买家', SELLER: '个人卖家' } as Record<string, string>)[r] || '其他授权身份').join(' / ')}</Text></View>
      <View className='fact'><Text>本次登录到期</Text><Text>{s.session && new Date(s.session.expires_at).toLocaleString()}</Text></View></View>
      <Text className='caption'>退出不会取消已有订单。待确认请求会按本人账号保留，重新登录后核对。</Text><View className='form-field'><Button className='nb-button full-button' disabled={s.busy} onClick={() => void model.logout()}><Text>退出登录</Text></Button></View></View>
    </LoginGate><BottomNav active='account' /></View>;
}
