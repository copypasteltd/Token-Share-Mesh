# Machine Pay 原生宿主与本人支付接口源码

2026-11-06｜`CODE_FIRST / SOURCE_ONLY_UNVERIFIED`｜PH-001—040。承接 [PT 事务与原凭据恢复](附录-MachinePay支付事务与原凭据恢复源码.md)。本次补入显式配置读取、两套独立钥环、实际调用持有与关闭、专用恢复进程以及既有契约的本人创建/查询 HTTP 接口源码。没有编译、测试、核验器、迁移、数据库、引擎、模型、手机、浏览器、实际渠道或资金操作。仅保存开发分支源码检查点，整轮开发与全部 56 任务/222 子项仍未完成。

当前为 98 项规划操作、74 张平台表、50 个控制 HTTP 操作，其中原租赁仍为 9 项。本次只实现已存在的 createPaymentIntent/getPayment，不追加 INT/DATA/schema，不改任何已发布 SQL。平台 001—016 双份迁移与端侧 DDL/source.json 原字节冻结；016 尚未执行。

## 1. 编号与范围

| 编号 | 需求 | 当前源码与边界 |
|---|---|---|
| PH-001 | 独立宿主注册 | paymenthost.Open 只读可信原生配置，不自动注册、审核或收款 |
| PH-002 | 原生根目录 | 主机显式传入项目 runtime 内绝对目录；os.Root 限制文件读取范围，不接受 HTTP 路径 |
| PH-003 | 规范相对路径 | 拒绝绝对路径、反斜线、冒号、点段、越界、重复分隔和 NUL；不是任意文件代理 |
| PH-004 | 有界文件 | 配置最多 256 KiB，私钥 16 KiB，公钥/钥环 8 KiB，token 4 KiB；仅可信普通文件 |
| PH-005 | 严格配置 | 严格 JSON、固定 profile、1—128 注册，部分/错配配置启动失败 |
| PH-006 | 环境绑定 | 宿主环境与每个原 Binding 必须等于明确 SANDBOX/PRODUCTION，不继承调用方环境 |
| PH-007 | 独立付款密钥 | 从配置指定的原文件读取；OAuth 登录配置不自动提供付款 app、钥或卖家 |
| PH-008 | RSA 原绑定 | PKCS1/PKCS8 RSA 私钥与 RSA 公钥；实际 NewAlipay 校验强度、双 pin、原授权/应用/服务 |
| PH-009 | 代调用 token | 只有原 ThirdParty Binding 可读取准确 token 文件；自研绑定配置 token 即拒绝 |
| PH-010 | 两套钥环 | 分别要求 PROOF_KEYS_1/RECEIPT_KEYS_1，不允许文件引用相同或任意钥材料重合 |
| PH-011 | 对称钥档位 | 每环 1—8 条显式 32 字节 canonical base64url，无 padding，不生成默认钥 |
| PH-012 | 轮换原引用 | 重启配置可显式保留旧对称钥；RSA/授权变化以新 capability 注册，原恢复仍保留原注册 |
| PH-013 | 初始化归属 | 中途失败先关闭已创建原渠道/钥环；不留下半成品可收费服务 |
| PH-014 | 启动无外调 | 构造只创建原生对象，不发 RPC、开监听、建单或执行迁移 |
| PH-015 | 诊断脱敏 | 宿主 fmt/JSON 脱敏，错误不拼文件路径、PEM、token、凭据或渠道回包 |
| PH-016 | 专用循环 | 同一 Host 最多启动一次 RunRecovery；不加入通用任务消费者 |
| PH-017 | 原 capability | 恢复和到期退役都只选择本宿主注册的原 capability，不抢其他卖家/注册任务 |
| PH-018 | 原凭据重验 | 调用既有 RecoverOne，只有原 verify；不重建 precreate、退款、出款或业务租约 |
| PH-019 | 有界节奏 | 有工作 100ms、空闲 1s、错误 2s 后再处理，取消停止；无无限忙转 |
| PH-020 | 到期原退役 | 每次最多清一条到期当前行密文，保留墓碑；不是 WAL/备份清除或金融释放 |
| PH-021 | 本人创建接口 | POST /orders/{order_id}/payment-intents 使用当前身份、角色、原订单/版本、批准 capability 和幂等 |
| PH-022 | 先提交再预下单 | 原意图提交后，仅 CREATED 调用 Precreate；其 SENT/UNKNOWN 提交后才外调 |
| PH-023 | 原未知重放 | UNKNOWN 不派发第二次 precreate；原键重放返回原付款当前状态，不承诺支付成功 |
| PH-024 | 本人查询接口 | GET /payments/{payment_id} 仅当前原买家/原卖家，只有安全 DTO，无 GET 金融副作用 |
| PH-025 | HTTP 边界 | 沿用精确 Origin/Cookie CSRF、Bearer、限流、严格 JSON、无任意 query 和原 ETag |
| PH-026 | 当前角色权限 | 当前 BUYER 得 payment:create/read，SELLER 只有 read；服务仍逐次核当前角色和归属 |
| PH-027 | 同一数据库 | HTTP 配置要求 payment/会话/密钥/交易使用同一个 DB 对象，拒绝跨库主体混用 |
| PH-028 | 正式 API 接线 | p11-api 两个配置均缺省时关闭付款；部分或无效则拒绝启动；不从登录钥回退 |
| PH-029 | 正式 worker 接线 | p11-payment-worker 原生入口读取相同显式注册/环境、验证已应用迁移，再运行专用循环 |
| PH-030 | 实际退出 | Host 先停止并等循环/SQL退出，再 Machine/实际 RPC/保存退出，再渠道/两个 Vault，最后外部 DB |
| PH-031 | 停止超时 | 未实际 join 则保留钥/原 owner，Close 可重试；不能伪造完成或提前关闭外部 DB |
| PH-032 | 测试源码 | 路径/有界文件/钥环/退出/脱敏、启动缺省、本人 HTTP/重放/未知/CSRF定义，全部未编译/运行 |
| PH-033 | 标准 SDK 付款入口 | 继续接 Payment-Proof 严格解析、本人原资源/单次证明、A402 和安全恢复；不在模型引擎中处理 |
| PH-034 | 小程序 PayLauncher | 继续接实际获准调用方式、原预付参考、前后台/取消恢复；预付号不是 my.tradePay trade_no |
| PH-035 | 可执行资金 | 继续接原资金用途唯一消费、当前主体/退款/设备/硬截止和原订单 READY/租赁/推理准入 |
| PH-036 | 完整获准渠道 | 继续实现有正式依据的退款/到账/佣金/对账单与个人卖家资格、异常工作台；不发明接口 |
| PH-037 | 续租及最终账 | 继续接付费原续租附约、可信 M token/小时水位、接受和追加平衡分录 |
| PH-038 | 秘密实际保证 | 继续核原生 ACL/独立秘密来源、保留期限/旧钥销毁、跨进程崩溃/撤销与外部签名证据；不承诺内存取证不可见 |
| PH-039 | 真实组合 | 真实个人 seller/service/代调用/退款/费用和原生支付宝/设备组合仍未验收 |
| PH-040 | 整轮代码后验证 | 全部 56/222 的生产代码、实际依赖接线与测试源码齐备后统一验证；DEV-55 延后、DEV-32无证明关闭 |

## 2. 配置文件与使用逻辑

以下是未来配置/运行契约，本阶段没有执行。API 与专用 worker 都要求明确 `P11_DATABASE_URL` 与 `P11_PAYMENT_ENVIRONMENT`。付款配置只使用 `P11_MACHINE_PAY_ROOT`（项目 `runtime/payments/host/` 下的绝对目录）与 `P11_MACHINE_PAY_CONFIG`（例如 `registry.json`）。两个变量全空只允许 API 保持付款关闭；worker 本身必须配置完整，否则退出。数据库迁移由既有独立管理员入口处理，worker/API 均只核对已应用迁移，不自动执行 016。

`registry.json` 字段为：

| 字段 | 内容 |
|---|---|
| profile_id | P11_MACHINE_PAY_HOST_1 |
| environment | 与进程明确环境一致 |
| proof_keyring_file / receipt_keyring_file | 两个不同的根内相对文件；分别保存短期 Proof 和财务归档钥环 |
| registrations | 1—128 项，每项是下面的原注册 |
| capability_ref | 本站稳定引用，原上下文恢复仍查准确此项 |
| binding | 完整 payments.Binding 原字段，当前存储沿用 Profile/Revision/Seller/AppID/ServiceID 等大写字段名；不得改为另一商户 |
| policy | SalePolicy 小写字段：goods_name/sku_id/mode/privacy/collection/maximum_window_seconds/refund_evidence_ref/hash/commission_evidence_ref/hash/return_target_id |
| signing_key_file / alipay_key_file | 原应用 RSA 私钥 PEM 与独立固定的支付宝 RSA 公钥 PEM，相对文件路径 |
| app_auth_token_file | 仅原 ThirdParty=true 且有 AppAuthTokenRef 时配置；原值不允许空白或换行 |

两套钥环各为 `profile_id`、`active_key_id`、`keys`。profile 分别是 `P11_MACHINE_PAY_PROOF_KEYS_1`、`P11_MACHINE_PAY_RECEIPT_KEYS_1`；keys 是显式 key ID→32 字节无 padding base64url 的映射。不得将真实 PEM、token 或钥写进 Git、产品页面、普通事件或这里的示例。没有默认生产配置、随机启动钥或自动审批。配置文件读取和启动不是商户获准证据，销售仍须 PT 原独立审核四证和开关通过。

每次改 RSA pin、个人卖家授权、服务、环境、商品/费用政策时，必须给新的 capability/ref 配置相应批准记录，并为仍未解决的旧付款保留旧注册及其原验证钥/政策直到原恢复义务处理完。不能直接复用旧 capability 换绑。对称钥轮换也必须明确保留当前未退役密文所需旧钥；删除旧钥不会使金融义务消失。热更新、ACL实证和保留策略仍是 PH-038，当前实现显式重启加载。

未来创建接口先在原本人请求中保存意图，再预下单，成功返回 201/PENDING 和安全预付参考。外调未知返回 503；用原键、原输入重放，读取同一 UNKNOWN 付款，不再创建。201/UNKNOWN 表示原意图存在，仍不是验付成功。GET 只有本地查询，不外调渠道、不延长期限。当前没有把 Payment-Proof 增加为控制面 JSON 字段或另造不在标准协议中的付款方式；因此 SDK/小程序完成验付链仍在 PH-033/034。

专用 worker 只处理已经持久提交的原 QUERY/job 并清到期当前行密文；它不能自动生成 Proof、重复收费或取代正式退款/结算。每个配置可启动多个独立进程实例，由既有 DB 原锁/领取代次控制同一 job；一个 Host 实例只启动一个循环。实际渠道默认 8 秒上限、原事实保存最多额外 5 秒，超时与未知保留原义务。它们不是数据库 WAL/崩溃/物理性能验证结果。

## 3. 源码和后续统一验证

源码位于 `dev/src/internal/paymenthost/`、`dev/src/api/cmd/p11-api/payment.go`、`dev/src/api/internal/http/payment.go` 和 `dev/src/scheduler/cmd/p11-payment-worker/`。注册表明确增加原 create/get 两项，现有 98 项规划/120 schema/74 表不变。当前没有新增编译依赖或付款 UI，没有声明真实收款/退款/增强隐私资格。

新增 12 项测试定义覆盖宿主配置和真实原生 SQL+内存合成 RSA2 HTTP 边界。它们与 PT 的 16 项、MP 的 49 项及其他源码定义都未编译/运行；合成审批仅供未来测试，不等于真实准入。本次没有启动任何运行环境或更改其他项目。

## PH 测试源码定义索引

| 编号 | 定义 | 源码 | 状态 |
|---|---|---|---|
| PH-T001 | TestPaymentHostPathsCannotEscapeExplicitRoot | dev/src/internal/paymenthost/config_test.go | 未编译/未运行 |
| PH-T002 | TestPaymentHostReadsOnlyBoundedRegularOriginalFiles | dev/src/internal/paymenthost/config_test.go | 未编译/未运行 |
| PH-T003 | TestPaymentHostKeyringRejectsDuplicateOrWrongProfile | dev/src/internal/paymenthost/config_test.go | 未编译/未运行 |
| PH-T004 | TestPaymentHostAcceptsExplicitOldAndNewKeyEntries | dev/src/internal/paymenthost/config_test.go | 未编译/未运行 |
| PH-T005 | TestPaymentHostCloseDoesNotPretendWorkerJoined | dev/src/internal/paymenthost/config_test.go | 未编译/未运行 |
| PH-T006 | TestPaymentHostDiagnosticsDoNotRevealRegistry | dev/src/internal/paymenthost/config_test.go | 未编译/未运行 |
| PH-T007 | TestOptionalPaymentHostRequiresCompleteConfiguration | dev/src/api/cmd/p11-api/payment_test.go | 未编译/未运行 |
| PH-T008 | TestPaymentHostCannotReuseOAuthConfigurationImplicitly | dev/src/api/cmd/p11-api/payment_test.go | 未编译/未运行 |
| PH-T009 | TestHTTPMachinePayCreatesAndReadsOnlyOriginalPayment | dev/src/api/tests/payment_http_test.go | 未编译/未运行 |
| PH-T010 | TestHTTPMachinePayUnknownDispatchReplaysWithoutFinancialRetry | dev/src/api/tests/payment_http_test.go | 未编译/未运行 |
| PH-T011 | TestHTTPMachinePayRejectsUnregisteredBuyerFieldsAndBrowserCSRF | dev/src/api/tests/payment_http_test.go | 未编译/未运行 |
| PH-T012 | TestHTTPMachinePayRemainsUnavailableWithoutExplicitHost | dev/src/api/tests/payment_http_test.go | 未编译/未运行 |

## R026 GF-001—040 原订单资金分配与当前准入 / Original allocation and current admission source

R026新增[GF-001—040](附录-原订单资金唯一分配与当前准入源码.md)：原订单唯一资金分配DATA075/前向017、原验签后独立元任务和原子READY/outbox、当前主体四证/gate/退款/期限及路由/小时租赁/密钥三消费者源码。当前50控制HTTP/98规划/75平台表；14项新增测试定义未编译/运行，017未迁移。当前仅显式零费用档；旧正向付款标记夹具升级、SDK/Proof/A402/实际PayLauncher、完整费用退款到账佣金对账/付费续租/可信计量接受账务和真实资格仍待。全部56任务222子项开放，完整生产代码/实际依赖接线/测试源码后集中验证；本次不是整轮完成。

## R026 CK-001—040 原付款挑战与双端恢复 / Original checkout challenge and recovery source

R026新增[CK-001—040](附录-原订单付款挑战与双端恢复源码.md)：原卖家当前付款选项、原订单唯一付款查找、原402挑战、单Proof头提交/恢复五接口，React/Taro与React/Vite原订单付款页、安全元数据journal和会话代次隔离源码。当前55控制HTTP/103规划/124 schema/75平台表；26项新增测试定义未编译/运行，无SQL变更或迁移。真实获准PayLauncher/小程序付款能力、SDK付款前原买方授权及数据面重试、跨端回调恢复、完整费用退款到账佣金对账/付费续租/可信计量接受账务、旧正向夹具与真实资格仍待。当前端未安装获准PayLauncher，付款唤起按钮保持关闭。历史截图/GIF不能证明本次新页面；按全量代码后集中验证要求暂未重录。全部56任务222子项开放，本次不是整轮完成。

## R026 RF-001—040 原退款适配与加密归档 / Original refund adapter and encrypted archive source

R026新增[RF-001—040](附录-原退款协议与渠道恢复源码.md)：独立条件原退款profile/原意图、原生RSA2退款及原号查询、提交前UNKNOWN/3秒提交及10秒查询准备/generation/成功保持、完整原签名回执AES-GCM归档源码。refund_fee按整笔累计额处理，签名提交本身不能确认本次部分退款；原号查询完整归属及明确成功才记金融事实。32项新增测试定义未编译/运行，103规划/124 schema/75平台表/55控制HTTP不变，无SQL变更/迁移。默认Machine Pay仍仅precreate/verify；真实原交易退款资格、持久累计锁/提交水位/任务、退款host/worker/HTTP和完整费用/佣金/到账/对账链继续待补。CK真实PayLauncher与SDK付款前授权/数据面恢复、GF031旧正向夹具及全56任务222子项继续开放。先完整源码/实际接线/测试源码，再集中验证和重录媒体；本次不是开发轮完成。

## R026 RT-001—040 原退款事务与宿主恢复 / Original refund transactions and native host recovery source

R026新增[RT-001—040](附录-原退款持久事务与宿主恢复源码.md)：原退款服务/独立当前未消费价款审批、原实付解密后RSA重验、执行清理/预算/累计守恒、同事务原退款意图/渠道/job/outbox/幂等和提交前UNKNOWN、原号恢复/双代次及原生宿主可选配置源码。新增018及DATA076/077，当前103规划/124 schema/77平台表/55控制HTTP；23项新测试定义未编译/运行，018未迁移。当前未消费价款分支不代替完整含费/已赚取价款冲销/佣金/续租/到账对账；公开退款API、审批工作台/双端售后、独立复核重试、真实原交易资格及CK/GF等仍待。全部56任务222子项开放；先完整生产源码/实际接线/测试源码，再集中验证和重录媒体。本次不是开发轮完成。

## R026 RA-001—040 退款申请与双端审核 / Refund applications and independent review source

R026新增[RA-001—040](附录-退款申请独立审核与双端售后源码.md)：原买家申请/双方查询、当前独立金融审核和原退款同事务批准、拒绝/协调撤销、七项控制HTTP/同原生宿主、Taro/React售后与Web审核台、安全原请求恢复和会话代次源码。019/DATA078仅源码，当前108规划/128 schema/78平台表/62控制HTTP；32项新测试定义未编译/运行，019未迁移。申请不是批准/渠道结果；完整费用冲销/佣金/续租/到账结算对账、独立复核重试/历史导入、完整争议举证/运营生命周期、CK/GF与真实资格仍待。全部56任务222子项开放，先全部生产源码/实际接线/测试源码，再集中验证和重录媒体；本次不是整轮完成。
