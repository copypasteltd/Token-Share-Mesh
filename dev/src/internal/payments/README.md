# Original C2C Machine Pay boundaries / 原支付边界

`CODE_FIRST / SOURCE_ONLY_UNVERIFIED`. This package contains an actual RSA2
precreate/verify adapter, strict original Payment-Proof handling, AES-GCM proof
recovery, a native ciphertext-only artifact store, bounded A402 responses and
payment/refund/reconciliation domain rules. All new tests remain source only.

Seller/app/service/authority/environment and both public-key fingerprints are
independently configured. Precreation is PENDING, never paid. Verification
requires the signed original node, active=true, exact original trade/amount and
out_trade_no. Unknown operations retain the original number and obligations.
There is no automatic retry, implicit refund/capture/settlement API, wallet,
platform merchant substitution or engine/GPU execution grant.

The native Machine source now includes DB intent/outbox transactions, complete
signed-envelope encryption and same-transaction receipt/payment persistence,
encrypted original-proof QUERY/jobs, and a dedicated recovery processor. Proof
and receipt keyrings must use different key material. Forward016 and these test
definitions have not been compiled, migrated or executed. The optional native
file artifact store is not the Machine service's transactional proof store.

PH now adds source wiring for an explicit rooted host registry, independent
secrets/keyrings, API create/get handlers and a dedicated original-proof worker.
None has been built or run. SDK/PayLauncher, uniquely usable funds and order
admission, full refund/payout/commission/reconciliation capabilities, paid
renewal and actual secret/qualification evidence remain **not complete**.
No actual merchant, channel, funds or SDK qualification has been established;
charging remains closed. See the PH annex for current scope and remaining work.

完整 MP-001—040、使用顺序、当前官方字段、秘密持有与恢复、资金/退款限制及全部后续
接线见[技术附录](../../../../docs/技术开发/附录-MachinePay原支付边界与凭证恢复源码.md)。
代码、实际依赖接线和测试源码全部齐备后，统一开展编译、测试、调试和真实组合验收。

## R026 MP-001—040 原支付边界与凭证恢复源码 / Original-payment boundaries and recovery source

R026新增[MP-001—040](../../../../docs/技术开发/附录-MachinePay原支付边界与凭证恢复源码.md)：实际个人seller原支付范围/双公钥pin、原生RSA2 precreate/verify及有界A402、严格原Payment-Proof/AES-GCM/原生密文工件、付款单调/迟到实付/资金资格/退款累计和原范围对账源码。49项新测试定义全部未编译/运行；合成签名夹具已按固定标准库的双结果接口修正惰性初始化。平台支付事务/同事务outbox/唯一消费/已核原签名回包归档/秘密与授权注册撤销、本人HTTP/SDK/PayLauncher、独占宿主及持久原凭据恢复worker、真实退款/到账/佣金/对账单与可信usage/小时水位/账务仍待。尚无真实个人收单资格或渠道验收，不开放收费。全部56任务222子项开放；无SQL变更或迁移。完整生产代码/实际依赖接线/测试源码后统一验证，本次不是开发轮完成。

## R026 PT-001—040 支付事务与原凭据恢复源码 / Original-payment transactions and recovery source

R026新增[PT-001—040](../../../../docs/技术开发/附录-MachinePay支付事务与原凭据恢复源码.md)：原个人卖家支付/PAY意图/上下文/outbox事务、独立审核四证/销售开关、提交后一次precreate、完整签名envelope与独立归档钥环、同事务receipt/payment/channel/outbox、原Proof密文/QUERY/专用job、原capability过滤/generation恢复与实际调用关闭源码。新增前向016/DATA072—074，当前源码98规划操作/74平台表/48控制HTTP；16项新测试定义未编译/运行，SQL未迁移。正式API/worker宿主/秘密注册轮换、本人HTTP/SDK/A402/PayLauncher、原可执行资金/退款/设备/截止准入、完整退款/到账/佣金/对账单/付费续租/可信计量接受账务与真实资格仍待。全部56任务222子项开放；完整生产代码/实际依赖接线/测试源码后统一验证，本次不是开发轮完成。

## R026 PH-001—040 原生支付宿主与本人接口 / Native payment host and owner HTTP source

R026新增[PH-001—040](../../../../docs/技术开发/附录-MachinePay原生宿主与本人支付接口源码.md)：原生显式注册/独立秘密/双钥环、有界文件与原scope、专用恢复和到期原退役宿主、实际join后关闭、既有本人支付create/get两个HTTP及正式API/worker入口源码。当前50控制HTTP/98规划/74平台表，无SQL变更；12项新测试定义及既有全部R026源码均未编译/运行。SDK/Proof/A402/实际PayLauncher、原资金唯一消费/当前退款设备截止准入、完整渠道退款到账佣金对账/续租/可信计量接受账务与真实资格仍待。全部56/222开放，完整生产代码/实际接线/测试源码后统一验证；本次不是开发轮完成。

## R026 GF-001—040 原订单资金分配与当前准入 / Original allocation and current admission source

R026新增[GF-001—040](../../../../docs/技术开发/附录-原订单资金唯一分配与当前准入源码.md)：原订单唯一资金分配DATA075/前向017、原验签后独立元任务和原子READY/outbox、当前主体四证/gate/退款/期限及路由/小时租赁/密钥三消费者源码。当前50控制HTTP/98规划/75平台表；14项新增测试定义未编译/运行，017未迁移。当前仅显式零费用档；旧正向付款标记夹具升级、SDK/Proof/A402/实际PayLauncher、完整费用退款到账佣金对账/付费续租/可信计量接受账务和真实资格仍待。全部56任务222子项开放，完整生产代码/实际依赖接线/测试源码后集中验证；本次不是整轮完成。

## R026 CK-001—040 原付款挑战与双端恢复 / Original checkout challenge and recovery source

R026新增[CK-001—040](../../../../docs/技术开发/附录-原订单付款挑战与双端恢复源码.md)：原卖家当前付款选项、原订单唯一付款查找、原402挑战、单Proof头提交/恢复五接口，React/Taro与React/Vite原订单付款页、安全元数据journal和会话代次隔离源码。当前55控制HTTP/103规划/124 schema/75平台表；26项新增测试定义未编译/运行，无SQL变更或迁移。真实获准PayLauncher/小程序付款能力、SDK付款前原买方授权及数据面重试、跨端回调恢复、完整费用退款到账佣金对账/付费续租/可信计量接受账务、旧正向夹具与真实资格仍待。当前端未安装获准PayLauncher，付款唤起按钮保持关闭。历史截图/GIF不能证明本次新页面；按全量代码后集中验证要求暂未重录。全部56任务222子项开放，本次不是整轮完成。
