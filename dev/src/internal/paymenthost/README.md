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

## R026 RF-001—040 原退款适配与加密归档 / Original refund adapter and encrypted archive source

R026新增[RF-001—040](../../../../docs/技术开发/附录-原退款协议与渠道恢复源码.md)：独立条件原退款profile/原意图、原生RSA2退款及原号查询、提交前UNKNOWN/3秒提交及10秒查询准备/generation/成功保持、完整原签名回执AES-GCM归档源码。refund_fee按整笔累计额处理，签名提交本身不能确认本次部分退款；原号查询完整归属及明确成功才记金融事实。32项新增测试定义未编译/运行，103规划/124 schema/75平台表/55控制HTTP不变，无SQL变更/迁移。默认Machine Pay仍仅precreate/verify；真实原交易退款资格、持久累计锁/提交水位/任务、退款host/worker/HTTP和完整费用/佣金/到账/对账链继续待补。CK真实PayLauncher与SDK付款前授权/数据面恢复、GF031旧正向夹具及全56任务222子项继续开放。先完整源码/实际接线/测试源码，再集中验证和重录媒体；本次不是开发轮完成。

## R026 RT-001—040 原退款事务与宿主恢复 / Original refund transactions and native host recovery source

R026新增[RT-001—040](../../../../docs/技术开发/附录-原退款持久事务与宿主恢复源码.md)：原退款服务/独立当前未消费价款审批、原实付解密后RSA重验、执行清理/预算/累计守恒、同事务原退款意图/渠道/job/outbox/幂等和提交前UNKNOWN、原号恢复/双代次及原生宿主可选配置源码。新增018及DATA076/077，当前103规划/124 schema/77平台表/55控制HTTP；23项新测试定义未编译/运行，018未迁移。当前未消费价款分支不代替完整含费/已赚取价款冲销/佣金/续租/到账对账；公开退款API、审批工作台/双端售后、独立复核重试、真实原交易资格及CK/GF等仍待。全部56任务222子项开放；先完整生产源码/实际接线/测试源码，再集中验证和重录媒体。本次不是开发轮完成。

## R026 RA-001—040 退款申请与双端审核 / Refund applications and independent review source

R026新增[RA-001—040](../../../../docs/技术开发/附录-退款申请独立审核与双端售后源码.md)：原买家申请/双方查询、当前独立金融审核和原退款同事务批准、拒绝/协调撤销、七项控制HTTP/同原生宿主、Taro/React售后与Web审核台、安全原请求恢复和会话代次源码。019/DATA078仅源码，当前108规划/128 schema/78平台表/62控制HTTP；32项新测试定义未编译/运行，019未迁移。申请不是批准/渠道结果；完整费用冲销/佣金/续租/到账结算对账、独立复核重试/历史导入、完整争议举证/运营生命周期、CK/GF与真实资格仍待。全部56任务222子项开放，先全部生产源码/实际接线/测试源码，再集中验证和重录媒体；本次不是整轮完成。
