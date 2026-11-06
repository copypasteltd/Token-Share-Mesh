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

API/worker host registration and secret lifecycle, authenticated HTTP/SDK/
PayLauncher, uniquely usable funds and order admission, actual refund/payout/
commission/reconciliation capabilities and paid renewal remain **not wired**.
No actual merchant, channel, funds or SDK qualification has been established;
charging remains closed. See the PT annex for source scope and remaining work.

完整 MP-001—040、使用顺序、当前官方字段、秘密持有与恢复、资金/退款限制及全部后续
接线见[技术附录](../../../../docs/技术开发/附录-MachinePay原支付边界与凭证恢复源码.md)。
代码、实际依赖接线和测试源码全部齐备后，统一开展编译、测试、调试和真实组合验收。

## R026 MP-001—040 原支付边界与凭证恢复源码 / Original-payment boundaries and recovery source

R026新增[MP-001—040](../../../../docs/技术开发/附录-MachinePay原支付边界与凭证恢复源码.md)：实际个人seller原支付范围/双公钥pin、原生RSA2 precreate/verify及有界A402、严格原Payment-Proof/AES-GCM/原生密文工件、付款单调/迟到实付/资金资格/退款累计和原范围对账源码。49项新测试定义全部未编译/运行；合成签名夹具已按固定标准库的双结果接口修正惰性初始化。平台支付事务/同事务outbox/唯一消费/已核原签名回包归档/秘密与授权注册撤销、本人HTTP/SDK/PayLauncher、独占宿主及持久原凭据恢复worker、真实退款/到账/佣金/对账单与可信usage/小时水位/账务仍待。尚无真实个人收单资格或渠道验收，不开放收费。全部56任务222子项开放；无SQL变更或迁移。完整生产代码/实际依赖接线/测试源码后统一验证，本次不是开发轮完成。

## R026 PT-001—040 支付事务与原凭据恢复源码 / Original-payment transactions and recovery source

R026新增[PT-001—040](../../../../docs/技术开发/附录-MachinePay支付事务与原凭据恢复源码.md)：原个人卖家支付/PAY意图/上下文/outbox事务、独立审核四证/销售开关、提交后一次precreate、完整签名envelope与独立归档钥环、同事务receipt/payment/channel/outbox、原Proof密文/QUERY/专用job、原capability过滤/generation恢复与实际调用关闭源码。新增前向016/DATA072—074，当前源码98规划操作/74平台表/48控制HTTP；16项新测试定义未编译/运行，SQL未迁移。正式API/worker宿主/秘密注册轮换、本人HTTP/SDK/A402/PayLauncher、原可执行资金/退款/设备/截止准入、完整退款/到账/佣金/对账单/付费续租/可信计量接受账务与真实资格仍待。全部56任务222子项开放；完整生产代码/实际依赖接线/测试源码后统一验证，本次不是开发轮完成。
