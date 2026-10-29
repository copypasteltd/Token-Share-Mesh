# P11 v3.0机器可读设计契约

P11｜v3.0｜2026-10-29｜设计草案；没有已部署服务。仅检查结构不能证明SDK、签名、密码、状态、安全或资金正确。

| 文件 | 范围 |
| --- | --- |
| [控制面OpenAPI](openapi-control-v3.0.json) | 自有控制面：权限、请求响应、幂等、归属与错误。地址p11.invalid为保留占位 |
| [密文帧Schema](cipher-frame-v3.0.schema.json) | 认证上下文与密文外层；payload类型/终态在密文内，suite仍待冻结 |
| [事件信封Schema](event-envelope-v3.0.schema.json) | outbox事件、关联、版本和安全载荷；生产者权限另核 |
| [路由合同Schema](route-contract-v3.0.schema.json) | 标准请求之外绑定卖家/模式/预算的已确认路由 |
| [配置目录](config-catalog-v3.0.json) | CFG01—31设计初值、未定参数与默认关闭的证据门槛 |
| [虚构示例](examples-v3.0.json) | schema形状夹具；签名/密文不是真实有效报文，金额不代表价格 |

完整厂商模型/媒体API、OAuth登录、支付宝支付签名/回调和ComfyUI/LM Studio/LMA原生协议未被这份控制面取代；必须冻结实际版本并用官方规范/真实环境验证。未知个人渠道不以私有模拟接口冒充。

## 操作目录

| 方法 | 相对/v1/p11路径 | 用途 | 操作ID |
| --- | --- | --- | --- |
| GET | `/me` | 读取本人角色与安全权限摘要 | `getSelfProfile` |
| POST | `/me/consents` | 记录版本化明确同意 | `recordConsent` |
| POST | `/me/deletion-requests` | 申请内容删除及跟踪合法保留 | `createDeletionRequest` |
| POST | `/seller/applications` | 提交个人卖家资格 | `createSellerApplication` |
| GET | `/seller/profile` | 读取本人卖家档案 | `getSellerProfile` |
| POST | `/seller/exit` | 停新售并清理在途和合法尾款 | `requestSellerExit` |
| GET | `/seller/listings` | 读取本人挂牌 | `listOwnListings` |
| POST | `/seller/listings` | 创建本人未来挂牌 | `createListing` |
| GET | `/seller/listings/{listing_id}` | 读取本人挂牌详情 | `getOwnListing` |
| PATCH | `/seller/listings/{listing_id}` | 修改未来挂牌修订 | `updateFutureListing` |
| POST | `/seller/listings/{listing_id}/submit` | 提交挂牌审查 | `submitListing` |
| POST | `/seller/listings/{listing_id}/pause` | 停止该挂牌新售 | `pauseListing` |
| GET | `/seller/devices` | 读取本人设备 | `listOwnDevices` |
| POST | `/devices/pairing-intents` | 建立一次性设备配对 | `createPairingIntent` |
| POST | `/device-enrollments` | 消耗一次性配对凭据登记设备 | `enrollDevice` |
| GET | `/devices/{device_id}` | 读取本人或本设备状态 | `getDevice` |
| POST | `/devices/{device_id}/capabilities` | 提交本设备待审能力 | `registerCapability` |
| POST | `/devices/{device_id}/heartbeats` | 记录当前身份心跳 | `recordDeviceHeartbeat` |
| POST | `/devices/{device_id}/drain` | 停止设备新任务准入 | `drainDevice` |
| POST | `/devices/{device_id}/revoke` | 撤销设备执行身份 | `revokeDevice` |
| PATCH | `/devices/{device_id}/resource-policy` | 设置资源/时段及停止策略 | `updateResourcePolicy` |
| GET | `/services` | 查询已合格实际卖家供给 | `listServices` |
| GET | `/catalog` | 查询已审核模型/工作流/协议档位 | `listCatalogEntries` |
| GET | `/notifications` | 读取本人安全事件通知 | `listNotifications` |
| POST | `/quotes` | 生成实际卖家/模式不可变报价 | `createQuote` |
| GET | `/quotes/{quote_id}` | 查询本人报价 | `getQuote` |
| GET | `/orders` | 按本人买卖角色查询订单 | `listOrders` |
| POST | `/orders` | 确认披露并消耗报价建业务单 | `createOrder` |
| GET | `/orders/{order_id}` | 查询本人不可变合同与独立状态 | `getOrder` |
| GET | `/orders/{order_id}/bill` | 查询本人可重算分账账单 | `getOrderBill` |
| POST | `/orders/{order_id}/payment-intents` | 按获准个人渠道创建支付意向 | `createPaymentIntent` |
| GET | `/payments/{payment_id}` | 查询原支付而非重复创建 | `getPayment` |
| POST | `/assets` | 登记有界单对象传输 | `registerAsset` |
| GET | `/assets/{asset_id}` | 读取本人对象元数据和合法授权引用 | `getAsset` |
| POST | `/assets/{asset_id}/complete` | 完成并核验上传manifest | `completeAsset` |
| POST | `/assets/{asset_id}/transfer-grants` | 签发本人单对象有界传输授权 | `createAssetTransferGrant` |
| POST | `/jobs` | 绑定本人订单和输入申请执行 | `createJob` |
| GET | `/jobs/{job_id}` | 查询当前任务状态 | `getJob` |
| POST | `/jobs/{job_id}/cancel` | 幂等取消并与接受状态竞争 | `cancelJob` |
| GET | `/jobs/{job_id}/results` | 读取有领取权益的正式结果 | `listResults` |
| POST | `/encryption-intents` | 绑定买方信任的接收端 | `createEncryptionIntent` |
| POST | `/recipients` | 登记本人网关内容接收公钥 | `registerRecipient` |
| GET | `/recipients/{recipient_id}` | 按合法意向范围读取端点公钥与信任材料 | `getRecipient` |
| POST | `/recipients/{recipient_id}/rotations` | 记录有证据公钥轮换供买方重新验证 | `createRecipientRotation` |
| POST | `/recipients/{recipient_id}/revoke` | 撤销本人接收端并停止新放钥 | `revokeRecipient` |
| POST | `/recipients/{recipient_id}/attestation-challenges` | 生成有界证明挑战 | `createAttestationChallenge` |
| GET | `/recipients/{recipient_id}/evidence` | 读取与挑战绑定的原始证明引用 | `getAttestationEvidence` |
| POST | `/jobs/{job_id}/envelopes` | 当前attempt绑定密钥封装引用 | `registerEnvelope` |
| POST | `/jobs/{job_id}/receipts` | 验证买方接受/拒绝回执 | `recordBuyerReceipt` |
| POST | `/cipher/channels` | 签发限scope密文通道引用 | `createCipherChannel` |
| POST | `/rental-sessions` | 按指定设备/合同申请租赁 | `createRentalSession` |
| GET | `/rental-sessions/{session_id}` | 查询本人受控租赁 | `getRentalSession` |
| POST | `/rental-sessions/{session_id}/ready` | 买方确认就绪和预算后起表 | `confirmRentalReady` |
| POST | `/rental-sessions/{session_id}/device-ready` | 设备提交当前锁代次的准备证据 | `recordRentalDeviceReady` |
| POST | `/rental-sessions/{session_id}/renewals` | 明确确认新预算/期限续租 | `createRentalRenewal` |
| POST | `/rental-sessions/{session_id}/stop` | 停止起表并撤会话工作权 | `stopRentalSession` |
| POST | `/rental-sessions/{session_id}/availability` | 记录待核实可用区间 | `recordRentalAvailability` |
| POST | `/rental-sessions/{session_id}/exports` | 导出本会话合法产物 | `createRentalExport` |
| POST | `/rental-sessions/{session_id}/cleanup` | 设备提交停工撤权清理证据后待核验解锁 | `recordRentalCleanup` |
| POST | `/rental-sessions/{session_id}/access-grants` | 签发不超已确认期限的会话受控权限 | `createRentalAccessGrant` |
| GET | `/rental-sessions/{session_id}/statement` | 查询整会话计时与金额证据 | `getRentalMeterStatement` |
| GET | `/route-contracts` | 读取本人已确认调用路由 | `listRouteContracts` |
| POST | `/route-contracts` | 冻结实际卖家/模式与自动调用授权 | `createRouteContract` |
| GET | `/route-contracts/{route_id}` | 查询路由授权及剩余预算 | `getRouteContract` |
| GET | `/api-keys` | 只读key指纹及scope | `listApiKeys` |
| POST | `/api-keys` | 创建有限授权key且只首次显示secret | `createApiKey` |
| DELETE | `/api-keys/{key_id}` | 撤销本人key | `revokeApiKey` |
| POST | `/orders/{order_id}/refunds` | 申请原单合同内退款 | `createRefund` |
| GET | `/refunds/{refund_id}` | 查询原退款请求状态 | `getRefund` |
| GET | `/seller/settlements` | 查询本人应结/未知/到账 | `listOwnSettlements` |
| GET | `/settlements/{settlement_id}` | 查询本人结算与原渠道状态 | `getSettlement` |
| POST | `/orders/{order_id}/reviews` | 对有资格真实订单评价 | `createReview` |
| POST | `/disputes` | 创建本人订单争议及选择性举证 | `createDispute` |
| GET | `/disputes/{dispute_id}` | 限权读取本人争议进度 | `getDispute` |
| POST | `/disputes/{dispute_id}/appeals` | 按资格申请争议复核 | `createAppeal` |
| POST | `/runner/leases/claim` | 本设备领取当前签名执行租约 | `claimLease` |
| POST | `/runner/attempts/{attempt_id}/progress` | 提交当前attempt的progress证据 | `recordAttemptProgress` |
| POST | `/runner/attempts/{attempt_id}/deliveries` | 提交当前attempt的deliveries证据 | `registerAttemptDelivery` |
| POST | `/runner/attempts/{attempt_id}/usage` | 提交当前attempt的usage证据 | `recordAttemptUsage` |
| POST | `/runner/attempts/{attempt_id}/cleanup` | 提交当前attempt的cleanup证据 | `recordAttemptCleanup` |
| POST | `/admin/evidence` | 提交限权审核证据与决定 | `registerReviewedEvidence` |
| POST | `/admin/releases` | 证据齐全才发布具体组合 | `createCapabilityRelease` |
| POST | `/admin/disputes/{dispute_id}/decisions` | 分权裁定争议并引用退款/责任规则 | `recordDisputeDecision` |
| PATCH | `/admin/feature-gates` | 按scope停新售或引用批准版本开放 | `updateFeatureGate` |
| POST | `/admin/settlement-batches` | 仅从合格账创建结算批次 | `createSettlementBatch` |
| GET | `/admin/settlement-batches/{batch_id}` | 查询原结算批次及未知状态 | `getSettlementBatch` |
| POST | `/admin/settlement-batches/{batch_id}/approve` | 独立人员复核结算批次 | `approveSettlementBatch` |
| POST | `/admin/settlement-batches/{batch_id}/submit` | 提交已复核原批次且未知先查 | `submitSettlementBatch` |
| GET | `/admin/audit-events` | 限权查询无正文审计元数据 | `listAuditEvents` |
| POST | `/auth/challenges` | 创建限时一次性登录绑定 | `createAuthChallenge` |
| POST | `/auth/sessions` | 核验授权码并签发有限会话 | `createUserSession` |
| GET | `/auth/session` | 读取本人会话及浏览器CSRF值 | `getCurrentSession` |
| DELETE | `/auth/session` | 撤销当前会话并提升用户版本 | `revokeUserSession` |

## 实施前必须验证的语义

对象归属、合同/价版不变、当前fencing_token、容量互斥、状态竞争、资金资格/未知先查、证据有效、签名真实性、完整终块和清理后解锁均非JSON Schema能自动证明。禁止把结构合格当作允许付款或放钥。

E2E先生成INPUT_PENDING/PREPARED上下文用于绑定attempt/token，再加密上传和提交封装；此时无执行权且不锁GPU。资金、输入/封装、端点身份和资源准入全部满足后才签ISSUED租约。换端先换上下文并由买方核验重封。

[详细接口事件规则](../10-接口事件与错误契约.md)；[需求逐项规格](../15-需求逐项实施规格.md)；[配置与外部证据](../14-决策记录配置与待验证事项.md)。
