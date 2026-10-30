# 已确认订单路由与标准 SDK 绑定实施细则

版本：R015 / CONFIRMED_ROUTE_1。对应 DEV-03、DEV-09、DEV-10、DEV-11、DEV-12、DEV-25、DEV-53。NB04 页面风格、React/Taro 小程序和 React/Vite Web 选型保持。本文说明本轮实际控制与解析源码，完整付费调用仍待实现。

## 编号约束

| 编号 | 约束 | 当前实现与验收边界 |
| --- | --- | --- |
| RC-001 | 标准 C2C 归属 | 路由固定原买家、原订单、原卖家和商品版本；无跨卖家资源池、自动购买或失败换卖家。 |
| RC-002 | 显式原订单 | `confirmed_order_id` 必填，必须是本人已确认合同的原订单，不根据最新商品生成新订单。 |
| RC-003 | 购买授权引用 | `purchase_authorization_ref` 是同单原始 `payments.id`；不是任意字符串、OAuth 授权、客户端支付成功布尔值或渠道原始流水。 |
| RC-004 | 支付适用范围 | 仅消费服务端登记的 ALIPAY / ORDER / SETTLED 事实，purpose_ref 必须等于原订单；金额覆盖原订单上限，渠道引用、产品修订及绑定快照齐全。AUTHORIZED 暂不支持。 |
| RC-005 | 环境隔离 | 服务必须明确配置 SANDBOX 或 PRODUCTION 并与支付记录完全一致；未配置关闭创建/解析，不选默认环境。 |
| RC-006 | 有效状态 | 创建仅接受 READY/FULFILLING、未到期、有足够单次容量的原订单。待付款、关闭、复核、付款未知或待退款均不能产生可用路由。 |
| RC-007 | 固定协议 | 仅支持下表三种 native 文本 profile / rev1；旧 openai_nb1、任意别名和未实现 profile 不自动映射。 |
| RC-008 | 隐私边界 | 当前仅 PLAINTEXT + STANDARD；E2E、增强机密和 GPU_RENTAL 路由拒绝。加密收件人字段不能用于启用未完成能力。 |
| RC-009 | 固定内容 | 固定原模型、设备/能力/内核环境绑定、价格及费用规则。卖家发布未来新版本不改写或暂停已确认的原价版；新购买仍只接受当前版本。当前商品资格和冻结合同不相符时关闭解析。客户端不能提交 endpoint、secret_ref 或私有路由字段。 |
| RC-010 | 原订单预算 | total_budget_fen 必须等于原订单上限；单次上限 1…total，最大次数 1…2147483647，金额不超过安全整数分。不是钱包余额或渠道冻结。 |
| RC-011 | 共享容量 | 一个订单只允许一份已确认路由。读取原订单剩余容量，不复制资金；创建、读取、解析不调用 ReserveTx，不建任务或租约。 |
| RC-012 | 调用次数 | R015配置阶段没有调用记录；R016起used_calls统计同路由已提交ROUTE_PREPARE_1原准备意图，受原max_call_count限制。同键恢复不重复占用，取消/过期不返还；不是成功推理/计费次数。完整执行签发仍待RI后续准入。 |
| RC-013 | 有效期 | 截止不能晚于订单、当前资格、商品窗口或已知资金截止。UTC 时间归一至 PostgreSQL 微秒精度；SDK key 不能晚于路由截止。 |
| RC-014 | 本人控制 | 创建、列表、详情均限当前 BUYER 本人账户会话。ROUTE_INVOKE key 只能参与标准调用解析，不能创建路由、购买、读本人控制页或扩大授权。 |
| RC-015 | 幂等 | 用户/操作范围内同键同输入返回同一路由，异输入 409；新键不能为同单建立第二路由。同次 SQL 事务提交路由和一条安全 outbox 事件。 |
| RC-016 | 当前投影 | 重放重新读取当前状态；过期投影 EXPIRED，资格/资金/容量/订单不再满足时投影 SUSPENDED、可用额度 0。该投影不代表退款、计费终态或 GPU 已释放。 |
| RC-017 | 标准 model 选择 | ResolveTx 只在已验证 key 的 route_ids 中精确匹配模型和请求方言；零项 404，多项 409。同名歧义包括已失效项，不能悄悄选另一个卖家。 |
| RC-018 | 事务内重查 | ResolveTx 重查用户版本、key 撤销/截止/scope/route 集、BUYER 角色、路由、原订单/支付和当前商品资格。返回固定合同配置，不返回 relay Grant。 |
| RC-019 | 数据不可变 | 007 保护路由绑定和被路由引用的订单/支付固定字段；只允许指定的关闭状态变更及版本递增，禁止删除和终态重激活。正常预算计数与订单/支付生命周期继续独立运行。 |
| RC-020 | 历史升级 | 001—006 和端侧 SQLite DDL 不改。007 把旧 ACTIVE 路由置 SUSPENDED 并标为 LEGACY_UNVERIFIED，不补造付款、合同、模型或确认时间。旧 key 不能据此取得可用路由。 |
| RC-021 | 启动检查 | API 使用 VerifyMigrations 只读核对全部内嵌迁移名称/摘要和登记数量，替换旧的固定第 5 版判断；缺失、改名、摘要不符或未知登记拒绝启动。核对的是可信迁移登记，不是任意管理员改动后的物理 DDL 取证。 |
| RC-022 | 真实证据范围 | 本轮数据库/HTTP/解析测试使用明确合成的资格、设备在线和 SETTLED 记录，无外部支付、模型、显卡或手机调用。完整平台 80 项主验收及 NB 96 场景仍未执行。 |

## 协议与接口映射

| 冻结 profile | 标准模型请求 | 实际状态 |
| --- | --- | --- |
| P11_CHAT_TEXT_NATIVE_1 / 1 | OpenAI POST /v1/chat/completions | 控制/解析与既有内核可组合；未挂公开付费入口 |
| P11_RESPONSES_TEXT_NATIVE_1 / 1 | OpenAI POST /v1/responses | 同上；无跨方言转换 |
| P11_MESSAGES_TEXT_NATIVE_1 / 1 | Anthropic POST /v1/messages | 同上；请求版本仍由既有内核严格验证 |

| 控制 operationId | HTTP 路径（前缀 /v1/p11） | 会话 scope | 事务/输出 |
| --- | --- | --- | --- |
| createRouteContract | POST /route-contracts | route:create | Idempotency-Key 必须；SQL 固定合同 + outbox；201 |
| listRouteContracts | GET /route-contracts | route:read | 本人、limit 1…100、按 ID 游标、签名游标绑定本人及操作；200 |
| getRouteContract | GET /route-contracts/{route_id} | route:read | 归属检查及当前安全投影；200 |

写入字段：confirmed_order_id、seller_id、listing_id、listing_revision、policy、budget_per_call_fen、total_budget_fen、max_call_count、expires_at、purchase_authorization_ref。E2E 两个收件人字段仅保留未来设计，当前拒绝。未知/重复 JSON 字段、null、未知查询项、重复参数、未签名游标均由既有控制适配层拒绝；Cookie 写入继续要求精确 Origin 和会话绑定 CSRF。API 不接收客户端成功支付状态。

返回固定 definition、原订单 contract_hash、order_contract、public_model、protocol_profile_revision、当前 state/remaining_budget_fen/used_calls。order_contract 是已存在的安全合同快照，包括分别按输入/输出 M token 计价的规则、费用、环境引用及证据摘要；不返回渠道流水、支付绑定快照、上游地址/凭据、prompt、输出或内容密钥。LEGACY_UNVERIFIED 旧行保留供独立恢复，不伪装为可用标准路由，当前已确认路由 API 不投影这些旧行。

## 使用逻辑

1. 买家通过小程序或 Web 明确选择个人卖家与版本，取得服务端报价，确认原合同后创建订单；当前普通页面真实流程仍到 WAITING_PAYMENT。
2. 未来支付适配器先完成渠道验签/查询和同单金额、产品、环境绑定，才可登记有效 SETTLED 和 READY 状态。本轮不新增该适配器，也不提供把订单直接改为 READY 的用户接口。
3. 本人控制会话提交原订单、原支付引用和更严格的调用上限，创建唯一固定路由。收到不确定回执时只以原键原参数恢复；不得换键制造第二订单/路由。
4. 本人创建仅绑定该 route_id、ROUTE_INVOKE scope、较短截止的 API key。秘密仅显示一次；标准请求中使用冻结 public_model，不额外要求 seller_id/order_id 等私有 JSON 字段。多个相同模型分别配置独立 key，不能依赖自动择优。
5. 当前可信宿主可在 SQL 事务中调用 `routecontract.Service.ResolveTx` 得到固定安全配置。它不含后端地址或 secret，不能直接当作执行许可交给内核。
6. 后续调用签发器必须同时校验标准请求、当前支付/退款及容量、单次和累计上限、次数、原任务/租赁 owner 与截止；写持久 invocation intent，调用订单预算预留并取得执行权后才允许一次后端副作用。网络请求必须在 SQL 提交之后，未知结果只查询原调用。
7. 协议终态、客户端断线或 route EXPIRED 不产生计费资格、不释放在途预算或显卡。真实 usage/验收/最终租赁计量和清理 ACK 按各自模块办理。

调用签发器还须落实统一锁顺序：先核对当前身份/凭据及角色，再对已确定的原订单取得写锁，然后组合路由、付款、预算与执行归属操作。不能让多个调用先共同持有订单共享锁，再同时升级为预算写锁。R015 的单独 ResolveTx 只做配置读取；不把这里的共享锁、余额快照或 max_call_count 当作并发调用签发已经完成。

开发服务可显式设置 `P11_PAYMENT_ENVIRONMENT=SANDBOX`；不得用该配置或合成 fixture 作为真实支付通过证明。PRODUCTION 仍须真实渠道资格和独立证据。路由模块没有读取调用正文、内容钥或上游凭据的接口；E2E 的本地加解密与提供方隐私内核仍按既有独立方案推进。

## 源码与迁移

| 路径 | 职责 |
| --- | --- |
| dev/src/internal/routecontract/service.go | 固定定义、原单/支付/资格核对、当前投影、本人分页、精确 scoped-key 解析 |
| dev/src/api/internal/http/route_contracts.go | 三个实际控制 operation、严格正文/查询、本人签名游标与幂等回执 |
| dev/src/internal/market/catalog.go | LoadTx只允许当前版本新购买，LoadConfirmedTx保留已售版本并重查当前资格；调用方必须先验证原订单归属 |
| dev/src/internal/identity/keys.go | 排除旧路由和失效原订单/付款，key 截止不晚于路由 |
| dev/src/internal/persistence/verify.go | 与内嵌 SQL 对照的只读启动检查 |
| docs/技术开发/database/migrations/007_confirmed_route_contracts.sql | 5 个路由列、同单支付外键、单订单唯一路由及不可变绑定 |
| dev/src/internal/persistence/migrations/source-v7.json | 原 SQL 与内嵌副本摘要；不覆盖旧迁移 |
| dev/src/tests/integration/route_contract_test.go | 并发、归属、环境、未知资金、资格撤销、歧义、共享容量和 SQL 防绕过 |
| dev/src/tests/integration/route_upgrade_test.go | 原生 001—006→007 升级和只读启动检查 |
| dev/src/api/tests/route_contract_http_test.go | 真实 HTTP 会话、CSRF、严格输入、scoped key 和安全重放 |

本轮新增 14 项顶层原生回归；当前完整用例数量、成功/失败及临时实例清理以 [R015 验证报告](../../dev/tests/evidence/R015-验证报告.md) 为准。实际付款与 qualified-source producer、原子调用签发/次数记账、模型公开监听、本地密文中继和 UI 路由配置仍在 [全部 56 项待办](../../dev/progress/R015-本轮实现与完整待办.md) 中，不能从本控制子集推导完整交付。

短签名租约回归保留150ms截止和已提交场景800ms返回上限；慢速元数据可能在进入后端前耗尽租约，此时必须为零次提交的安全拒绝。进入后端的场景则验证唯一Submit、适配器context截止不晚于签名截止，过期后无迟交/重交。此测试不把元数据耗时当成新增执行时间；另有4秒签名截止的适配器上下文回归及独立元数据延迟回归验证实际调用路径。见R015完整原始事件中的实际分支标记，不将零提交路径说成已执行推理。
