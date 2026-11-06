# Machine Pay 支付事务与原凭据恢复源码

2026-11-06｜`CODE_FIRST / SOURCE_ONLY_UNVERIFIED`｜PT-001—040。承接[MP 原支付边界](附录-MachinePay原支付边界与凭证恢复源码.md)，本次新增原生 Go 应用服务、平台前向 016、完整签名回包加密归档与专用原凭据恢复处理的源码和测试定义。没有编译、运行测试、执行迁移、启动数据库、调用实际渠道或资金操作；源码检查点不是开发轮完成。全部 56 任务、222 子项及真实验收保持开放。

当前源码目录为 98 项规划操作、74 张平台表、48 个控制 HTTP 操作；本次没有增加公共路由。新增 DATA-072—074 分别为 `payment_machine_contexts`、`payment_machine_receipts`、`payment_machine_recoveries`。016 在文档与原生迁移目录保存相同字节并登记 source-v16；发布后只允许追加 017+。001—015 双份 SQL 与全部原端侧迁移保持原字节。本次仅写迁移源，不执行迁移。

## 1. 完整编号与当前边界

| 编号 | 需求 | 源码行为与限制 |
|---|---|---|
| PT-001 | 原渠道注册 | Registration 由可信宿主注入实际 Alipay 对象和 SalePolicy，按独立 capability ref 索引；买方不提交 app/key/地址 |
| PT-002 | 原 C2C 主体 | Create 锁本人原订单，核 buyer/seller/合同 hash/版本，拒绝同人交易与换卖家 |
| PT-003 | 固定预付范围 | 付款金额来自原总预算，1—5000 分；不按作品/attempt/任务次数收费，不自动拆单 |
| PT-004 | 产品与费用 | 核原 SKU/mode/privacy/有效价格和原个人直收零平台费档；其他佣金/后扣/分账能力仍待完整渠道接线 |
| PT-005 | 当前销售准入 | 新付款和实际 precreate 前均复核原卖家、确认挂牌版本、设备资格及当前可用性 |
| PT-006 | 四类证据 | 独立代调用授权、个人 seller、退款路线、佣金路线分别核类型、摘要、范围与期限 |
| PT-007 | 独立审核主体 | APPROVED 证据要求当前 PAYMENT_REVIEWER/self 角色、有效账号，且审核者不等于买家或卖家 |
| PT-008 | 销售开关 | 精确 binding digest 的 feature gate 需明确开启并包含全部四份证据；默认没有真实可用注册 |
| PT-009 | 原窗口 | 原创建时间、合同截止与授权销售截止共同限制付款窗口，最多 12 小时，不重新锚定 |
| PT-010 | 原回跳引用 | 只接受原注册的 return_target_id，保存/比较安全引用，不接受任意 URL；实际回跳启动器仍待 |
| PT-011 | 事务先行 | payments、PAY channel request、原上下文、幂等安全响应与 outbox 在同一事务提交 |
| PT-012 | 无持锁外调 | 事务内只有 SQL/本地有界编码加密，RPC 只发生在提交确认后；提交未知不外发 |
| PT-013 | 一次预创建派发 | INTENT→SENT 与 payment UNKNOWN 同事务提交，再调用原 precreate；SENT/UNKNOWN 不再次创建 |
| PT-014 | 丢回包保留原号 | 原号/原绑定保留，缺凭据且无获证查询能力时进入人工恢复，不虚构 query 或重新付款 |
| PT-015 | 完整原回包 | Alipay Observation 持有完整已验签 envelope；保留原节点空白/转义和 sign，不重序列化替代 |
| PT-016 | 独立归档加密 | Receipt Vault 使用另一套 AES-256-GCM 钥环，AAD 含原 scope、金额、类型、交易号、摘要和观察时间 |
| PT-017 | 不共用钥 | NewMachine 拒绝相同 Vault 或两个钥环内任何重合钥材料；短期凭据与财务保留独立 |
| PT-018 | 原回包生命周期 | Observation.Close 撤销所有副本的原字节读取，财务事实元数据不被反转；仍须调用方关闭 |
| PT-019 | 归档与付款同事务 | receipt 密文、原 context 指针、付款状态、渠道安全结果、归档和付款 outbox 在同一事务提交 |
| PT-020 | 归档追加唯一 | 同 payment/kind/evidence hash 去重；禁止删除/改写历史，完整密文不进入 safe_result/普通事件 |
| PT-021 | 付款单调 | SETTLED 不被 UNKNOWN/PENDING 覆盖，交易号仍受原渠道唯一约束；迟到实付只记财务事实 |
| PT-022 | 数据库保护 | 首次 SETTLED 需当前事务的原范围 VERIFIED_PAID receipt；数据库不替代应用层 RSA2/GCM 核验 |
| PT-023 | 时间精度 | 本机观察时间归一为 UTC 微秒，避免 JSON 纳秒与 PostgreSQL 微秒导致归档匹配不一致 |
| PT-024 | 单一原凭据 | SubmitProof 在本人当前角色下登记同一原付款凭据；同键或另一键的不同凭据均不能覆盖活跃原恢复 |
| PT-025 | 凭据密文事务 | 短期密文、QUERY 意图、专用 durable job 和安全重放结果同事务；不依赖可选本地 ProofStore |
| PT-026 | 期限与墓碑 | 恢复最长 30 分钟且不超授权恢复截止；到期只清当前行密文、保留原引用/状态墓碑与金融义务 |
| PT-027 | 专用处理 | VERIFY_MACHINE_PAYMENT 独立于通用队列；只处理当前注册 capability 的原恢复任务 |
| PT-028 | 原任务领取 | 原订单→payment/context→recovery/job 锁序；generation、20 秒领取期限及最多 8 次同凭据验付 |
| PT-029 | 当前恢复授权 | 每次核原注册与当前授权/个人卖家证据；恢复不需要重新出售，拒绝冒用新 capability/key/卖家 |
| PT-030 | 原财务回包优先保存 | 实际 RPC 返回后使用独立有界持久化窗口保存原事实；迟到回包可入账，旧 generation 不能确认新任务 |
| PT-031 | 付款与凭据区分 | 付款已核但此 QUERY 没有同份成功记录时，只终止重复查询，不伪称该份凭据已验付 |
| PT-032 | 实际持有与关闭 | Machine 关闭取消在途并等实际方法返回，不自动关闭外部 Channel/两套 Vault；额度/设备/执行不因此释放 |
| PT-033 | 应用宿主入口 | 待接正式 API/worker 进程的可信注册加载、秘密读取/轮换、长期归档保留与完整关闭顺序 |
| PT-034 | 本人 HTTP 与 SDK | 待接 create/get 控制路由、普通 SDK 原付款前置/单 Payment-Proof、A402 与安全错误映射，不允许普通 SDK 自动扣款 |
| PT-035 | 小程序付款体验 | 待正式 PayLauncher、真实获准渠道调用、回前台读原状态/恢复与本人付款页；prepay ref 不是小程序 trade_no |
| PT-036 | 可执行资金事实 | 待原资格/资金唯一消费、当前退款/设备/主体及硬截止的原事务，才允许订单 READY 与租赁/推理准入 |
| PT-037 | 完整后续渠道 | 待原退款受理/成功/到账、卖家到账、佣金/平台费、对账单与原异常工作台；当前公开 API 只有原两项能力 |
| PT-038 | 全产品与续租 | 待原小时附约付费续租、完整佣金产品、可信 M token/小时区间、接受及追加账；不移除原目标范围 |
| PT-039 | 实际资格与故障证据 | 待原生迁移/跨进程与退出/外部 RSA2 向量、实际个人注册与真实渠道验证；全部未执行 |
| PT-040 | 全量后统一验证 | 全部 56/222 生产代码、实际依赖和测试源码齐备后统一编译/测试/调试；DEV-55 延后，增强档无证明关闭 |

PT-001—032 表示相应生产源码存在，仍未编译、迁移或运行；PT-033—040 保留全部后续接线与验收。父 MP-034/036 由此推进了 SQL/密文归档和原凭据恢复核心，尚不能说整个平台支付已完成。

## 2. 原生应用调用与数据流

可信宿主先加载实际个人卖家原注册、独立应用/支付宝公钥 pin 与秘密，构造 Alipay、短期 Proof Vault 和不同密钥的 Receipt Vault，再构造 `NewMachine(db, proofVault, receiptVault, registrations)`。没有可用真实注册、独立证据和开关时，销售不能准入。本构造器不注册网络监听、不启动 worker、不执行真实资金操作。

1. 本人控制入口以当前 Principal、原订单 ID、原 expected_order_version、明确 channel_capability_ref 和原幂等键调用 CreateIntent。它只冻结意图，返回 CREATED；重放读原付款当前状态。
2. 明确推进同一意图时调用 Precreate。原 PAY 请求先提交 SENT、payment 先记 UNKNOWN，随后才实际外调。成功回包加密入库后变 PENDING；异常保持原号/原义务，不再次发 precreate。
3. 后续 SDK/客户端的原 Payment-Proof 由严格解析器处理，不能送入模型。SubmitProof 把短期密文和原 QUERY/job 一次提交；不同内容不能覆盖活跃原凭据。
4. 正式宿主未来以专用循环调用 RecoverOne。它按自身注册 capability 找到原任务，提交领取后解开同一份密文，调用原 verify；不会创建第二笔付款。
5. 完整成功回包先加密，再与 receipt/context/payment/channel/outbox 同事务落库。即使请求已经取消，已返回的原财务观察仍有独立最多 5 秒保存窗口；失败/提交未知保留原恢复路径，不能宣告渠道未收款。
6. 付款变为 SETTLED 后，原订单仍可处于 WAITING_PAYMENT。原资金用途唯一消费、原业务资格/退款/设备/硬截止仍须 PT-036 接线，不能直接开始推理或租赁。
7. 到期凭据由 RetireExpiredProof 清当前行密文并保留墓碑；财务回包继续使用独立归档钥。正式宿主停止接入、取消并等待所有 Machine 调用返回后，再关闭外部 Alipay、两个 Vault 和 DB。

当前只承接 `ORDER` 固定预付的个人直收零平台费源码档，其他合同/费率不自动改写为这个档；不符合即拒绝。金额是原确认预算，不是另设的媒体单价。RENTAL_RENEWAL、平台费与其他获准产品继续保留 PT-038；不能用新订单替代原续租附约。

## 3. 事务、密文与恢复语义

| 情况 | 当前源码结果 | 后续义务 |
|---|---|---|
| 意图提交未知 | 不调用外部渠道 | 用原幂等键/订单找原记录 |
| 已提交 SENT 后进程退出 | 原 payment UNKNOWN | 保留原号，获原凭据后验同一笔；缺获证查询时人工处理 |
| 凭据重复同内容 | 返回原 recovery，不增加 job | 保留原期限，不滚动延期 |
| 凭据改变或跨买家 | 拒绝 | 不替换原密文、交易或卖家 |
| 验付回包丢失/无效 | 原 UNKNOWN、有限原凭据重验 | 不释放预算，不创建另一收费 |
| 回包已核但 DB 保存未知 | 继续保留原恢复记录 | 重验原笔、去重归档；不假称财务失败 |
| 已付款后迟到 UNKNOWN | 已付事实保留 | 不重复付费或提前记退款 |
| job 领取过期而原调用未返回 | 原调用仍由 Machine 持有 | 允许保存迟到原财务事实，旧代次不能确认新 job |
| 原凭据到期/缺钥/授权不明 | job 结束并保留 UNKNOWN | 保留原金融义务，转正式复核；不重新收款 |

数据库触发器核对范围、引用、摘要、状态和同事务写入条件；它不在 PostgreSQL 内验证 RSA2 或 GCM。密码边界仍是固定 Alipay 适配器及私有 Observation/Receipt Vault，DB 写入权限不能开放给用户。合成测试直接插入独立审核记录仅用于未来临时数据库断言，不构成正式审核或真实渠道许可。

RetireExpiredProof 是当前行逻辑清理，不承诺清除旧 MVCC/WAL/备份中的密文。短期钥轮换、旧钥销毁、归档保留与实际秘密来源仍属 PT-033/039。当前未声称数据库已启用磁盘加密，或已通过 Windows 权限、崩溃持久化、性能和渠道验证。

## 4. 源码、契约与统一验证入口

`dev/src/internal/payments/machine.go` 提供应用方法与实际调用持有；`machine_store.go` 处理当前主体、原合同、独立审核/feature gate 及读取；`machine_recovery.go` 实现同事务归档与专用恢复；`receipt.go` 保存完整签名 envelope 并进行独立钥环加密。新 016 只在两个权威目录保存相同源字节。通用 outbox 队列没有增加本专用 kind，不能领取它。

控制面 Payment 的 channel_reference/channel_action_ref 改为有界渠道参考字符串：32 位数字的 Alipay trade_no 不能误套本站字母开头 entity_id；此修订不增加操作或 schema。channel_action_ref 仍是预付参考，不接受 URL、不证明已付、不等于原生小程序支付参数。

新增测试源码覆盖完整原回包、跨 scope/篡改/关闭/旧钥、两套钥环隔离、时间精度、原意图事务、提交后才 RPC、未知不重建、原凭据密文/重放、专用恢复/不同 capability 隔离、已付不启动订单、无签名归档不能改已付和跨买家访问。既有迁移测试的精确平台表数更新为 74，保留原迁移漂移拒绝和全部失败断言。所有定义未编译/运行；本次不运行任何核验器。真实资金、退款、SDK/小程序、端侧、增强档与全量组合验收保持未通过。

## PT 测试源码定义索引

| 编号 | 测试定义 | 源码 | 状态 |
|---|---|---|---|
| PT-T001 | TestMachinePayArchivePreservesCompleteOriginalSignedEnvelope | dev/src/internal/payments/receipt_test.go | 未编译/未运行 |
| PT-T002 | TestMachinePayReceiptCannotCrossOriginalBuyerSellerOrPurpose | dev/src/internal/payments/receipt_test.go | 未编译/未运行 |
| PT-T003 | TestMachinePayReceiptTamperCannotBecomeFinancialEvidence | dev/src/internal/payments/receipt_test.go | 未编译/未运行 |
| PT-T004 | TestMachinePayReceiptRequiresActualVerifiedEnvelopeNotConstructedSnapshot | dev/src/internal/payments/receipt_test.go | 未编译/未运行 |
| PT-T005 | TestMachinePayClosingObservationRevokesAllCopiesButNotPaidFact | dev/src/internal/payments/receipt_test.go | 未编译/未运行 |
| PT-T006 | TestMachinePayArchivedReceiptRequiresExplicitRetainedOriginalKey | dev/src/internal/payments/receipt_test.go | 未编译/未运行 |
| PT-T007 | TestMachinePayObservedTimeMatchesDatabaseMicrosecondPrecision | dev/src/internal/payments/receipt_test.go | 未编译/未运行 |
| PT-T008 | TestMachinePayShortProofAndLongReceiptKeysCannotOverlap | dev/src/internal/payments/receipt_test.go | 未编译/未运行 |
| PT-T009 | TestMachinePaymentIntentCommitsOriginalScopeWithoutChannelIO | dev/src/tests/integration/machine_payment_test.go | 未编译/未运行 |
| PT-T010 | TestMachinePaymentUnknownPrecreateNeverRepeatsFinancialCreate | dev/src/tests/integration/machine_payment_test.go | 未编译/未运行 |
| PT-T011 | TestMachinePaymentProofCiphertextAndOriginalReplayShareOneDurableJob | dev/src/tests/integration/machine_payment_test.go | 未编译/未运行 |
| PT-T012 | TestMachinePaymentRecoveryArchivesVerifiedPaidWithoutStartingOrder | dev/src/tests/integration/machine_payment_test.go | 未编译/未运行 |
| PT-T013 | TestMachinePaymentCannotSettleWithoutSameTransactionSignedReceipt | dev/src/tests/integration/machine_payment_test.go | 未编译/未运行 |
| PT-T014 | TestMachinePaymentRevokedRefundEvidenceBlocksBeforeCreatingIntent | dev/src/tests/integration/machine_payment_test.go | 未编译/未运行 |
| PT-T015 | TestMachinePaymentOtherBuyerCannotReadOrSubmitOriginalProof | dev/src/tests/integration/machine_payment_test.go | 未编译/未运行 |
| PT-T016 | TestMachinePaymentWorkerCannotClaimAnotherRegisteredCapability | dev/src/tests/integration/machine_payment_test.go | 未编译/未运行 |
