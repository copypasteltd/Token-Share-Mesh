# Machine Pay 原支付边界与凭证恢复源码

2026-11-06｜`CODE_FIRST / SOURCE_ONLY_UNVERIFIED`｜MP-001—040。本次实现渠道边界、原支付领域约束和凭证密文恢复仓的生产源码及测试定义，尚未接成平台支付链。没有外发支付、签约、退款、结算或其他资金操作；没有编译、运行测试、数据库或核验器。全部 56 任务、222 子项及真实验收保持开放。

本附录承接 [支付退款技术](13-支付退款账务与渠道结算.md)、[小时租赁](附录-小时租赁授权与累计计量源码细则.md)与[原媒体入口](附录-原租赁媒体兼容入口与加密缓存源码.md)。原 MP 阶段为 98 项设计操作、71 表、48 个控制 HTTP 操作，无迁移变更。后续 [PT 支付事务源码](附录-MachinePay支付事务与原凭据恢复源码.md)追加前向 016 与 DATA-072—074，当前源码平台表数为 74；001—015 双份 SQL 和全部既有端侧迁移原字节保留。

## 1. 当前官方依据与 C2C 边界

2026-11-06 只读核对[赛事页](https://aipay.alipay.com/community/events/payathon-2026)、[Machine Pay 指南](https://aipay.alipay.com/docs/ai-receive/MACHINE_PAY.html)、[预下单 API](https://aipay.alipay.com/docs/ai-receive/api-list/alipay-aipay-agent-payment-precreate.html)与[验付 API](https://aipay.alipay.com/docs/ai-receive/api-list/alipay-aipay-agent-payment-verify.html)。两项 API 页面仍标记 2026-10-28 更新；本源码冻结的边界如下：

- 原请求号、已注册 service、商品名、固定金额、付款截止与 CNY 均来自原合同和可信卖家配置。固定预付限 1—5000 分，付款窗口最长 12 小时。
- precreate 得到 amt_pre_pay_id 后返回 402/amtPaymentLink；随后解析原 Payment-Proof 并调用 verify。普通模型 SDK 不因此自动付款。
- verify 必须验签并核 active、金额、交易号与原请求号；resource_id 已废弃，不作为资源归属。
- 当前公开依据不能证明退款、关单、事后扣款或分账接口。此适配器只声明 PRECREATE 和 VERIFY_SAME_PROOF；未知资金沿原意图恢复，不制造第二笔付款。

平台是用户向用户售卖闲置资源的 C2C 撮合方。Binding 必须指向实际个人卖家、应用、注册服务与原授权主体；不能把所有服务挂到平台自营商户。资格/授权引用与 hash 是可信配置输入，不是已取得资格的证明，也不是客户端提供的字段。真实注册、代调用、退款路线、平台费及收款资格仍需正式证据后才能开放。

## 2. 完整编号与源码状态

| 编号 | 需求约束 | 当前源码行为与边界 |
|---|---|---|
| MP-001 | 原卖家配置 | Binding 固定 seller/app/service/授权主体、资格引用、环境和修订；默认没有可用真实配置 |
| MP-002 | 原支付范围 | Scope 绑定 payment/order/buyer/seller/purpose/ref/contract hash/binding hash/out_trade_no |
| MP-003 | 固定付款窗口 | CreatedAt 锚定原窗口，pay_before 不超过 12h、合同截止和销售授权截止；重试不重锚 |
| MP-004 | 整数金额 | 分转固定两位元字符串；拒绝零、超 50 元、浮点精度、指数和隐式取整 |
| MP-005 | 渠道原号 | 独立随机生成 32 字符原号，不截断本站订单 ID，不为 UNKNOWN 换原号 |
| MP-006 | 安全商品元数据 | goods_name 限 32 字符，来自批准服务；不得放 prompt/文件名或买方私人正文 |
| MP-007 | 密钥 pin | 应用签名公钥与支付宝公钥 DER SHA256 必须匹配独立 Binding；RSA2、2048—4096 位 |
| MP-008 | 环境隔离 | SANDBOX/PRODUCTION 固定不同 HTTPS 渠道端点；不接受请求内任意地址或自动降级 |
| MP-009 | 第三方代调用 | app_auth_token 只来自独立授权引用；自研应用禁止带此 token，客户端不能指定 |
| MP-010 | 请求签名 | 对未 URL 编码的规范参数按序签名，再表单编码；不把编码后文本拿来签 |
| MP-011 | 有界外调 | 8 秒总上限、有限响应头/64 KiB 响应、禁重定向/压缩；无自动支付重试 |
| MP-012 | 响应原字节 | 验证准确 method 响应节点的原始字节，重复键/歧义/错签名均 UNKNOWN；error_response 不作为成功节点 |
| MP-013 | 预下单非付款 | PRECREATED 只产生 PENDING 与预付参考号，不生成 SETTLED、执行或成交事实 |
| MP-014 | 凭证输入范围 | trade_no/payment_proof/client_session 只取原严格解析的 Payment-Proof；不透传引擎 |
| MP-015 | 验付完整匹配 | active=true、原交易号、整数金额和 out_trade_no 必须一致；有签名也不能放宽 |
| MP-016 | 已废弃字段 | 不强制 resource_id，也不让它覆盖原合同/原设备映射 |
| MP-017 | 严格凭据解析 | 单一头、16 KiB 编码上限、4 KiB 规范 JSON、重复/未知/别名/null 歧义拒绝 |
| MP-018 | 原 client_session | 最多 1024 字符，按原值传递；缺失时不自造，渠道要求时仍由真实客户端补齐 |
| MP-019 | 凭据持有与诊断 | Proof 共享状态撤销，副本共关闭；配置/渠道/Proof/Vault 等诊断脱敏 |
| MP-020 | 原范围密文 | AES-256-GCM 把完整 Scope、key ID、期限和 profile 纳入 AAD；跨单/卖家/用途不能打开 |
| MP-021 | 密钥轮换 | 显式保留旧钥才能读取旧密文；缺钥、损坏和关闭均拒绝，不偷偷重签或换付款 |
| MP-022 | 恢复期限 | 密文恢复最长 30 分钟，过期拒绝；它不是支付有效期、执行授权或租赁续期 |
| MP-023 | 实际文件仓 | 只在传入项目 runtime 的 payments/proofs 下保存密文；使用原生 os.Root 约束路径 |
| MP-024 | 写入不确定 | 独占新文件、写/Sync/Close 后返回；失败保留原引用和占用供恢复，不宣布持久成功 |
| MP-025 | 仓容量 | 单实例至多 4096 项、每项最多 8 KiB；既有未知文件也占容量，不擅自清除；跨进程独占仍待宿主接线 |
| MP-026 | 退役与结束 | 只删除原 scope 的准确引用；先由上层登记 tombstone；关闭等待实际文件操作，不销毁外部持有钥环 |
| MP-027 | 付款单调事实 | 已核 SETTLED 不被迟到 PENDING/UNKNOWN 覆盖；迟到实付仍记财务事实，不复活合同 |
| MP-028 | 资金资格边界 | 原主体/合同/设备当前有效、原金额和未决退款均核；函数结果不是 GPU 租约/放钥或接受证据 |
| MP-029 | 退款累计 | SUCCEEDED/REQUESTED/PROCESSING/UNKNOWN 都占可退额度；坏数据/重复/超额拒绝，不钳成可退余额 |
| MP-030 | 退款原号重放 | 同 ID/原号/金额返回原记录，UNKNOWN 不另造退款；未收款关闭不能叫退款成功 |
| MP-031 | 原范围对账 | 比较原 scope/金额/交易事实；不符或未知阻止新准入，不改原历史、不自动出款 |
| MP-032 | A402 元数据 | 检查原 PENDING 快照构建 Challenge，上层仍须先持久提交；amtPaymentLink 是预付号，无自动付款、小程序 trade_no 或 200 假成功 |
| MP-033 | 边界测试源码 | 原签名、金额/原号/环境、凭据/密文/文件仓、迟到付款、退款累计和实际退出等定义；全未执行 |
| MP-034 | 平台支付事务 | PT 已补原 DB/PAY 意图、幂等/outbox、完整原回包加密与提交后外调源码；正式宿主、秘密注册撤销和原资金唯一消费仍待 |
| MP-035 | 应用支付入口 | 待接本人控制 HTTP、原标准 SDK 支付前置和小程序 PayLauncher/回前台查单；普通 SDK 不能自动付款 |
| MP-036 | 持久未知恢复 | PT 已补同 Proof 的 DB 密文/QUERY/job 与专用处理源码；正式 worker 宿主、实际崩溃/撤销和全链恢复仍待，无虚构 query API |
| MP-037 | 实际后续渠道能力 | 待获证并实现具体退款/到账/分账/结算与对账单接口；当前领域预留不是外调能力 |
| MP-038 | 用量与账务镜像 | 待真实接受/可信 token 或小时计量、原资格、退款差额和追加镜像账；不是钱包/预授权后扣 |
| MP-039 | 真实资格与联调 | 待实际个人 seller/service/授权/退款/佣金材料、RSA2 外部向量及正式/沙箱端到端证据；未执行 |
| MP-040 | 全量后统一验证 | 全部 56/222 生产代码、实际依赖接线及测试源码齐备后统一编译/测试/调试；DEV-55 延后，增强档无证明关闭 |

MP-001—033 表示原阶段源码已写；后续 PT 推进了 MP-034/036 的事务与恢复核心，全部生产接线及 MP-035/037—040 仍未完成。全部编号均未验收。原生文件仓不等于 payments/channel_requests 事务；PT 的新服务也尚未接成正式 CLI/API/worker，不自动开始收款。

## 3. 配置与使用顺序

可信宿主从独立卖家注册配置取得 Binding，包含 Profile/Revision、Provider/Environment、Seller/AppID/ServiceID/AuthoritySubject、CredentialRef、授权/资格引用与 hash、应用签名钥/支付宝公钥 pin、第三方授权引用，以及三个有效期。真实应用签名私钥、支付宝公钥和 app_auth_token 从受限秘密来源注入 `NewAlipay`，不能来自买方正文或路由头；构造时克隆密钥并核 pin。

`ValidFrom—ValidUntil` 控制新销售/付款创建；`ResolveUntil` 是另外获准的原财务恢复期限，不得早于销售截止或晚于其后 30 天。恢复窗口只能查询原财务事实，不能售新单、延合同、继续推理或扩大原授权；卖家实际授权撤销时，注册配置和实际秘密访问仍必须一并撤销。该注册/撤销服务仍属于 MP-034/036。

后续平台服务必须按下列顺序接线，当前未完成的步骤不得跳过：

1. 验证当前买家/实际 seller/原合同及渠道 SKU 资格；在同一数据库事务登记固定 Intent、唯一原号、channel request 和 outbox，提交成功后才允许外调。
2. `Alipay.Precreate` 只使用原 Intent；原回包验签后记录 Observation，再持久归并 PENDING。此后才 `NewChallenge/WriteChallenge` 返回 402。
3. 同一原意图再次请求时，`ProofFromRequest` 读取单一原 Payment-Proof；原金额/用途/主体不能变化，也不新建支付。
4. PT 的 Machine.SubmitProof 使用 Proof Vault 加密短期凭据，在同一个 DB 事务保存密文、QUERY 意图、原 job 和安全重放结果。原可选 ProofStore 属于另一种本地工件实现，并未被 Machine 用作事务仓；若未来宿主明确采用它，文件与 DB 的准确引用/期限/用途仍需单独恢复，不能当成新的金融操作。
5. `Alipay.Verify` 完成原字节验签、active、金额/交易号/原号核对后，只有原 Observation 才能归并 SETTLED；同事务保存受限渠道证据、唯一消费与业务事件。
6. `CanFund` 还需上层在原事务读到的当前主体/设备/合同、退款占用和执行窗口；不能把一个布尔返回当成正式执行租约或唯一资金消费证明。
7. 对 UNKNOWN 的恢复 worker 只能领取原任务，用原引用恢复同一凭据并重验同一原意图。凭据失效、缺钥、来源未知或渠道人身资格不明时保留义务并查询/补证，不制造第二笔收费。

`Observation` 的内部构造字段不开放给 JSON，`EvidenceHash` 标识已核响应节点，不能单独证明平台付款入账。后续 PT 已补入完整原签名 envelope 的内存持有/关闭、独立 Receipt Vault 和 receipt/payment/channel/outbox 同事务加密归档源码；原签名与原节点字节均保留。正式应用宿主、原资金唯一消费、数据库迁移/恢复与真实渠道证据仍未完成，见 PT-033—040。

## 4. 凭据仓与清理细则

`Proof` 保存规范化的短期凭证字节，关闭会撤销共享状态。`Vault` 使用显式 key ID 和外部钥环；Blob 是完整 scope/期限/nonce/ciphertext 的 JSON。密文可交文件仓，原 payment_proof、client_session、app_auth_token 和私钥不得进入 `safe_result`、普通日志、URL 或模型转发。

仓目录固定为调用方批准项目 runtime 下的 `payments/proofs/`。引用包含完整原 scope 摘要与随机数，仅接受准确格式；不把请求参数当相对路径。os.Root 阻止路径逃出根目录；文件创建使用 O_EXCL 和 0600，目录 0700。Windows 实际账户 ACL 尚未验证，文件模式不能当成 Windows 权限隔离证明；保密依靠密文与独立钥管理，而非隐藏目录名。

容量由当前 ProofStore 实例管理。正式宿主必须先取得跨进程独占权，且不允许绕过仓接口写文件，才能把单实例限制作为实际总配额；这个宿主持有与重启协调仍在 MP-034/036。当前源码没有声称通过多个实例或多个进程共同维护配额，也不会扫描删除不认识的既有工件。

`Put` 在文件写入、Sync、Close 或上下文不确定时可返回非空原 ref + ErrUnknown，上层应保留这个工件并登记恢复/退役，不能宣称写入成功。该操作不是 DB+文件的分布式事务；文件 Sync 不构成所有系统上的目录元数据崩溃持久保证。重启丢工件、缺钥或解密失败都保持财务 UNKNOWN，不自动重新收费。失联工件扫描、钥轮换保留、最小权限和期限清理任务仍待平台接线。

Retire 仅清除准确原密文工件；上层须先写 tombstone。成功删除、到期和关闭均不是退款成功或资金释放。ProofStore.Close 等待进入的原生文件操作后关闭根句柄，但不关闭外部 Vault；必须由宿主先结束使用者，再处理钥生命周期。Go 解析/签名过程含临时字符串和密码库内部对象，清理拥有的字节及引用不等于承诺内存取证不可见。

## 5. 金融状态与退款／对账细则

| 事实 | 允许行为 | 不得推导 |
|---|---|---|
| precreate 成功 | 保存原 prepay reference，返回 402 | 已付款、已准备 GPU、可售或成交 |
| verify active 且完整匹配 | 记录原已付款观察，待原事务提交 | 买方接受、可信 usage、租赁开始或已到账卖家收益 |
| 验签错/响应丢失/业务未知 | 保留原号、原凭据引用与恢复任务 | FAILED 后自由重扣、自动拆单或换 seller |
| 过期后收到真实已付款 | 财务记录实付并进入原退款/补偿处理 | 原合同复活、hard deadline 延长或新增执行 |
| 退款 UNKNOWN | 继续占原已收金额的可退容量 | 退款失败、可重建另一退款或可重新结算 |
| 卖家直收 | 记录独立已付事实与业务资格 | 平台钱包余额、同笔再次平台出款或自动佣金分账 |

HeldRefunds/ReserveRefund 需要调用方在同一个可串行化事务锁定原 payment 和全部原退款记录；纯函数不替代数据库锁。退款授权上限须来自独立责任/售后证据，不能用买方自填金额。已成功、在途、UNKNOWN 都占用，已知 FAILED 才不占；重复 ID/原号、跨单、坏状态或累计超额整体关闭，不能用截断/取零修复。

Reconcile 只比较同一个 scope、金额和渠道事实，返回明确不符/仍未知/迟到已付等分类。历史追加、对账单获取、人工复核/修复分权、卖家到账与平台费差额仍未完成；本函数不会改账、重扣、重退或向卖家出款。

## 6. 源码与统一验证门槛

| 源码 | 内容 |
|---|---|
| `dev/src/internal/payments/types.go` | 原绑定/Scope/Intent、整数金额、Observation、付款单调归并和资金资格约束 |
| `alipay.go` | 固定两项真实方法、签名/原字节验签、pin、环境、有界 I/O 和真实结束等待 |
| `proof.go` / `a402.go` | 严格单一凭据头、秘密持有、原 PENDING 挑战与安全响应 |
| `vault.go` / `proofstore.go` | 原 scope AES-GCM 恢复、轮换/期限、原生密文工件及关闭 |
| `refunds.go` | 原退款累计/重放和对账差异分类，尚无实际渠道退款能力 |
| `payments_test.go` / `proofstore_test.go` | 合成签名、内存 transport 和原生文件边界定义，均未编译/运行 |

原生依赖仍为已有固定 Go 工具链与标准库，没有安装新的支付宝 Skill/第三方支付 SDK，也没有下载或运行官方安装脚本。测试定义中的临时目录须由后续统一验证入口配置到本项目 runtime。正式 RSA2 向量、真实应用/个人 seller/channel 注册、金额/截止、退款与卖家到账、A402 SDK/小程序和资金→执行→接受→结算的全链证据仍须另行完成；不得用合成签名代替渠道资格。

源码阅读已将合成密钥夹具的惰性初始化改为标准库 `sync.OnceValues` 的两个结果：一份密钥对容器和一个错误值。只阅读固定工具链的接口声明，没有执行密钥生成、编译或测试；这一修正不构成测试通过。

## MP 测试源码定义索引

| 编号 | 测试定义 | 源码 | 状态 |
|---|---|---|---|
| MP-T001 | TestMachinePayIntegerYuanRejectsRoundingAndOverLimit | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T002 | TestMachinePayOriginalNumberIsNotTruncatedOrderID | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T003 | TestMachinePayBindingRequiresActualSellerAuthorityAndEvidence | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T004 | TestMachinePayPrecreateChecksOriginalContractBeforeExternalCall | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T005 | TestMachinePayPrecreateProducesPendingNotSettled | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T006 | TestMachinePaySignedFormBindsServiceCurrencyAndOriginalNumber | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T007 | TestMachinePayVerifyRequiresSignedActiveAmountAndOriginalNumber | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T008 | TestMachinePayVerifyDoesNotRequireDeprecatedResourceID | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T009 | TestMachinePayResponseSignatureUsesOriginalNodeBytes | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T010 | TestMachinePayValidSignatureWrongAmountCannotSettle | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T011 | TestMachinePayCrossOrderSignedProofCannotSettle | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T012 | TestMachinePayInactiveProofCannotSettle | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T013 | TestMachinePayDuplicateResponseKeysCannotSettle | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T014 | TestMachinePayForeignSignatureCannotSettle | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T015 | TestMachinePayTransportFailureIsUnknownWithoutInternalRetry | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T016 | TestMachinePaySignedBusinessFailureRetainsUnknownAndSafeError | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T017 | TestMachinePaySignedErrorEnvelopeCannotActAsSuccessfulPrecreate | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T018 | TestMachinePayRedirectNeverForwardsSignedCredentials | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T019 | TestMachinePayOverLimitResponseIsUnknown | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T020 | TestMachinePayChannelKeyCannotDepartFromApprovedBinding | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T021 | TestMachinePayA402UsesPrepayReferenceAndCannotChargeAutomatically | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T022 | TestMachinePayUnknownCannotBecomeFreshPaymentChallenge | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T023 | TestMachinePayProofHeaderRejectsDuplicateCaseAliases | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T024 | TestMachinePayPublicCapabilitiesDoNotInventRefundOrSettlement | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T025 | TestMachinePayCanonicalFormSignsRawValuesBeforeURLEncoding | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T026 | TestMachinePayProofParserRejectsAmbiguityAndOversizedSecrets | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T027 | TestMachinePayOptionalClientSessionIsNotManufactured | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T028 | TestMachinePayClientSessionIsPreservedWithoutDecodingOrReplacement | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T029 | TestMachinePayClosedProofCannotBeReusedThroughCopy | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T030 | TestMachinePayLatePaidFactDoesNotResurrectExpiredContract | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T031 | TestMachinePayResolutionHorizonDoesNotAuthorizeNewSale | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T032 | TestMachinePayUnknownOrPendingCannotOverwriteSettled | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T033 | TestMachinePayCrossSellerObservationCannotMutateOriginalFunds | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T034 | TestMachinePayFundingRequiresCurrentPartiesAndResolvedRefunds | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T035 | TestMachinePayUnknownRefundRetainsCapacity | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T036 | TestMachinePayRefundReplayReturnsSameUnknownOriginal | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T037 | TestMachinePayUnpaidClosureCannotBecomeRefundedMoney | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T038 | TestMachinePayDamagedRefundRowsNeverClampToFreeCapacity | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T039 | TestMachinePayReconciliationMismatchBlocksAndPreservesOriginal | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T040 | TestMachinePayProofVaultBindsOriginalOrderAndSeller | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T041 | TestMachinePayProofVaultRejectsTamperAndExpiredRecovery | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T042 | TestMachinePayProofVaultRotationRetainsOnlyExplicitOldKey | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T043 | TestMachinePaySecretsAreRedactedFromDiagnosticFormatting | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T044 | TestMachinePayCloseTimeoutRetainsOwnerUntilActualResponseReadReturns | dev/src/internal/payments/payments_test.go | 未编译/未运行 |
| MP-T045 | TestMachinePayProofStoreWritesCiphertextAndCanReopenOriginal | dev/src/internal/payments/proofstore_test.go | 未编译/未运行 |
| MP-T046 | TestMachinePayProofStoreRefCannotCrossOrderOrTraverseRoot | dev/src/internal/payments/proofstore_test.go | 未编译/未运行 |
| MP-T047 | TestMachinePayProofStoreTamperKeepsFinancialOutcomeUnresolved | dev/src/internal/payments/proofstore_test.go | 未编译/未运行 |
| MP-T048 | TestMachinePayProofStoreRetirementIsOriginalAndIdempotent | dev/src/internal/payments/proofstore_test.go | 未编译/未运行 |
| MP-T049 | TestMachinePayProofStoreCloseDoesNotDestroyExternalKeyring | dev/src/internal/payments/proofstore_test.go | 未编译/未运行 |

## R026 PT-001—040 支付事务与原凭据恢复源码 / Original-payment transactions and recovery source

R026新增[PT-001—040](附录-MachinePay支付事务与原凭据恢复源码.md)：原个人卖家支付/PAY意图/上下文/outbox事务、独立审核四证/销售开关、提交后一次precreate、完整签名envelope与独立归档钥环、同事务receipt/payment/channel/outbox、原Proof密文/QUERY/专用job、原capability过滤/generation恢复与实际调用关闭源码。新增前向016/DATA072—074，当前源码98规划操作/74平台表/48控制HTTP；16项新测试定义未编译/运行，SQL未迁移。正式API/worker宿主/秘密注册轮换、本人HTTP/SDK/A402/PayLauncher、原可执行资金/退款/设备/截止准入、完整退款/到账/佣金/对账单/付费续租/可信计量接受账务与真实资格仍待。全部56任务222子项开放；完整生产代码/实际依赖接线/测试源码后统一验证，本次不是开发轮完成。

## R026 PH-001—040 原生支付宿主与本人接口 / Native payment host and owner HTTP source

R026新增[PH-001—040](附录-MachinePay原生宿主与本人支付接口源码.md)：原生显式注册/独立秘密/双钥环、有界文件与原scope、专用恢复和到期原退役宿主、实际join后关闭、既有本人支付create/get两个HTTP及正式API/worker入口源码。当前50控制HTTP/98规划/74平台表，无SQL变更；12项新测试定义及既有全部R026源码均未编译/运行。SDK/Proof/A402/实际PayLauncher、原资金唯一消费/当前退款设备截止准入、完整渠道退款到账佣金对账/续租/可信计量接受账务与真实资格仍待。全部56/222开放，完整生产代码/实际接线/测试源码后统一验证；本次不是开发轮完成。
