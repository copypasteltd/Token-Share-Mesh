# Native original-payment host / 原生支付宿主

`CODE_FIRST / SOURCE_ONLY_UNVERIFIED`. This package loads an explicit original
personal-seller registry under a trusted runtime root, independent RSA pins and
secrets, and separate proof/receipt keyrings. It owns one dedicated same-proof
recovery loop and logical expired-row retirement. Construction performs no RPC,
approval, migration, listener startup or funds operation.

The API owns its Host and exposes only the original authenticated create/get
operations. A separate `scheduler/cmd/p11-payment-worker` owns its native loop.
Both must use the same explicit environment and retained original registration;
OAuth configuration is never an implicit payment registration. Missing API
configuration disables collection; partial configuration fails startup.

Close cancels and joins the actual loop, Machine operations and channel calls
before releasing either keyring. The outer app closes the DB afterward. A caller
deadline retains ownership and keys until a later successful Close. Live-row
retirement does not erase WAL/backups or resolve financial obligations.

All source remains unbuilt/unexecuted. SDK/A402/Proof and actual PayLauncher,
unique usable funds/business admission, full qualified refund/payout/fee/
statement/renewal/metering/ledger and actual secret/merchant/device evidence are
still open; this package is not a delivered complete payment system.

完整 PH-001—040、环境变量、注册/钥环字段与使用顺序见
[原生宿主与本人接口附录](../../../../docs/技术开发/附录-MachinePay原生宿主与本人支付接口源码.md)。
全部生产代码、实际依赖接线和测试源码齐备后才统一验证；本次未运行任何服务、渠道或资金操作。

## R026 GF-001—040 原订单资金分配与当前准入 / Original allocation and current admission source

R026新增[GF-001—040](../../../../docs/技术开发/附录-原订单资金唯一分配与当前准入源码.md)：原订单唯一资金分配DATA075/前向017、原验签后独立元任务和原子READY/outbox、当前主体四证/gate/退款/期限及路由/小时租赁/密钥三消费者源码。当前50控制HTTP/98规划/75平台表；14项新增测试定义未编译/运行，017未迁移。当前仅显式零费用档；旧正向付款标记夹具升级、SDK/Proof/A402/实际PayLauncher、完整费用退款到账佣金对账/付费续租/可信计量接受账务和真实资格仍待。全部56任务222子项开放，完整生产代码/实际依赖接线/测试源码后集中验证；本次不是整轮完成。

## R026 CK-001—040 原付款挑战与双端恢复 / Original checkout challenge and recovery source

R026新增[CK-001—040](../../../../docs/技术开发/附录-原订单付款挑战与双端恢复源码.md)：原卖家当前付款选项、原订单唯一付款查找、原402挑战、单Proof头提交/恢复五接口，React/Taro与React/Vite原订单付款页、安全元数据journal和会话代次隔离源码。当前55控制HTTP/103规划/124 schema/75平台表；26项新增测试定义未编译/运行，无SQL变更或迁移。真实获准PayLauncher/小程序付款能力、SDK付款前原买方授权及数据面重试、跨端回调恢复、完整费用退款到账佣金对账/付费续租/可信计量接受账务、旧正向夹具与真实资格仍待。当前端未安装获准PayLauncher，付款唤起按钮保持关闭。历史截图/GIF不能证明本次新页面；按全量代码后集中验证要求暂未重录。全部56任务222子项开放，本次不是整轮完成。
