# System design / 系统方案

## R021 · 原生 C API worker 与受限管道

本轮增加可替换的 llama.cpp 原生 C++ worker 与 Go InputAdapter：独立本机批准包/模型/模板及逐文件摘要；三文本请求严格映射到同一模型模板与真实 tokenizer；受限匿名内容管道、独立取消、两个64KiB输出缓冲及原 owner 一次提交。模型权重可复用，每任务同步、清KV并释放上下文后才发原清理回执；进程退出保持 UNKNOWN。

`本机批准包 → 独占资源准备 → Open暖机 → 可信HostKernel注册 → 原ticket.Start → 授权sink读取 → native同步清理 → 原凭证投影`。当前节点CLI执行仍关闭；对外输出/SSE/ACK、可信usage、真实模型/GPU、独立低权限服务、E2E、LM Studio兼容内核及其他设备路线继续待验。合成 C ABI 动态库测试证明实际进程接线，不证明真实推理或LP等级。

[42条内核约束](../../docs/技术开发/附录-原生LLM受限管道与可替换worker.md) · [本轮证据](../tests/evidence/R021-验证报告.md) · [全部56任务与222子项](../progress/R021-本轮实现与完整待办.md)。原R016/R019/R020时序与归档限制继续保留，最终交付目标ACTIVE。

## R021 · Native C API worker and bounded private pipes

This round adds a replaceable llama.cpp C++ worker and Go InputAdapter. Locally approved pack/model/template hashes control loading; three strict text profiles preserve their semantics before the approved chat template and actual tokenizer. Directed anonymous content pipes, a separate cancellation pipe, two 64-KiB output buffers and original-owner once-only submission bound execution. Model weights are reused; each task synchronizes, clears KV and frees its context before its original cleanup receipt. Process exit remains UNKNOWN.

`Owner-approved pack → exclusive resource preparation → Open/warm-up → trusted HostKernel → original ticket.Start → authorized content sink → native synchronization/clear → original cleanup projection`. Node CLI execution remains disabled. Public output/SSE/ACK, trusted usage, real models/GPUs, a separate low-privilege service, E2E, LM Studio compatibility and other device kernels still need implementation/qualification. Tests with a synthetic C ABI DLL exercise the actual Windows process; they are not real inference or LP acceptance.

[42 kernel constraints](../../docs/技术开发/附录-原生LLM受限管道与可替换worker.md) · [Current evidence](../tests/evidence/R021-验证报告.md) · [All 56 tasks and 222 open steps](../progress/R021-本轮实现与完整待办.md). Historical R016/R019/R020 timing and archive limitations remain; the delivery goal is ACTIVE.

## R020 原生宿主 / Persistent native host

R020新增持久原生执行宿主：独立issuer公钥与原订单/本机内核批准，原领取命令在网络前加密保存，丢回执和重启始终恢复原编号。清单/输入有界接收，正文仅在内存，通过显式InputAdapter交给原生一次提交监督器；旧receipt不能重送正文。清理状态只投影原执行账本的同lease/metadata凭证。损坏索引不能隐藏原命令，缺库/错绑定拒绝重建。Windows DPAPI/私有ACL复用，Linux仅交叉构建及既有钥环协议夹具。新增host-init/host-status，不自动安装或运行模型，execution_enabled仍false。只覆盖PLAINTEXT/STANDARD三文本profile，元数据加密不等于E2E或设备所有者不可见。真实tokenizer/内核、输出回传/usage/ACK/续期、支付渠道、LP/PE/租赁和完整验收仍待；37/95控制操作及65表/664列/113外键保持，旧SQL001—010与原执行DDL字节不变，新宿主SQLite独立首发。全部56任务/222子项及R016/R019慢元数据未证明根因风险保留，目标ACTIVE。

R020 adds a persistent native execution host with an independently provisioned issuer public pin and original order/kernel approval. The original claim is encrypted and committed before networking; missing replies and restarts retain that exact command. Bounded manifest/input reads produce an in-memory ticket. An explicit InputAdapter hands original bytes to the existing once-only native supervisor; an old receipt cannot submit content again. Cleanup is projected only from the original execution journal with the same lease and metadata hash. Corrupted selectors cannot hide unfinished commands, and missing or mismatched anchored state is never recreated. Windows reuses actual DPAPI/private ACLs; Linux remains cross-built with protocol fixtures. CLI host-init/host-status do not install or run models; execution_enabled remains false. This covers three PLAINTEXT/STANDARD text profiles. Metadata encryption does not provide content E2E or provider blindness. Real tokenizers/kernels, output/usage/ACK/renewal, payment channels, LP/PE/rental and complete acceptance remain open. The 37/95 control-operation and 65-table/664-column/113-FK counts are unchanged. Previously published SQL001–010 and execution DDL bytes are preserved; a separate native host SQLite schema is new. All 56 tasks/222 development steps and unresolved R016/R019 slow-metadata timing risks remain open; the goal stays ACTIVE.

[配置字段/原生命令/恢复状态/36条约束](../../docs/技术开发/附录-原生执行宿主与内容适配接线.md) · [本轮完整待办](../progress/R020-本轮实现与完整待办.md)。先结束监督循环再关闭父节点；批准和密钥配置仅放忽略runtime。

R020首次完整回归177过/73失/0跳过：旧预算升级子数据库执行001时触发原statement_timeout，同期检查点sync110.501秒，后续多例耗尽原220秒包级上下文；H5旅程另在237.63秒后终止且无最终收据，保留失败画面。存储底层原因与浏览器终止原因仍未证明。验证现将独立原生测试移到PG启动前，明确等待Taro确认控件可用/勾选及提前保存失败收据；未改变产品源码、旧SQL、既有断言或时间上限。后续通过不能证明时序稳定或抹除首轮失败。

The first R020 complete run passed 177 / failed 73 / skipped 0. Migration 001 in a legacy-upgrade child DB hit the existing statement timeout; a contemporaneous checkpoint reported 110.501s sync, and later cases exhausted the unchanged 220s package context. The H5 journey separately terminated after 237.63s without a final receipt; its failure screenshot is retained. Underlying storage and browser causes remain unproven. Validation now runs independent native tests before starting PG, explicitly observes enabled/checked Taro consent, and journals failure evidence earlier. Product source, published SQL, existing assertions and time bounds are unchanged. Later passing runs do not establish timing stability or erase these failures. [Full failure archive / 完整失败归档](../tests/evidence/R020-full-attempt1-archive.json)。

R020第二次运行只完成原生组：113过/2失/0跳过，两项既有监督器测试在原清理期限内返回ErrShutdown；该失败运行未独立观察最终磁盘owner。随后两个数据库测试程序编译成功，但验证脚本错误地假定test2json是独立exe，准备阶段中断；PG及两个浏览器分组均未启动。修正为锁定Go的标准tool入口，不改Go产品源码、既有断言/SQL/运行期限。失败原始事件、当时脚本及构建摘要保留；后续通过不证明清理时序稳定。

The second R020 run completed only the native segment: 113 passed / 2 failed / 0 skipped. Two existing supervisor cases returned ErrShutdown within the unchanged cleanup bound; the failed run did not independently observe final disk ownership. Both database test executables compiled, but the runner incorrectly expected test2json to be a standalone executable and stopped during preparation. PostgreSQL and both browser segments never started. The runner now uses the pinned Go tool entry; product Go source, assertions, SQL and runtime deadlines are unchanged. Original events, the runner and build digests are retained. A later pass does not establish cleanup timing stability. [Partial failure archive / 部分失败归档](../tests/evidence/R020-full-attempt2-archive.json)。

R020第四次完整运行中的五项旧迁移测试因验证工具未保留go test的包工作目录而失败，fixture未读取到五份真实pre-006 SQL；该测试启动问题已定位，修正工作目录而不改夹具、产品或旧SQL。此问题不同于首轮SQL超时及第二轮监督器清理失败，后两者根因仍未证明。

Five legacy-migration tests in the fourth complete R020 run failed because the precompiled-test runner did not preserve go test package working directories; their fixture could not read the five real pre-006 SQL files. The test-launch cause is identified and corrected by restoring package working directories, without changing fixtures, product source or published SQL. This is separate from the first-run SQL timeout and second-run supervisor cleanup failures, whose underlying causes remain unproven. [Failure archive / 失败归档](../tests/evidence/R020-full-attempt4-archive.json)。

R020第五次完整运行249过/1失/0跳过：原生115项、数据库core及Web通过，H5页面操作本身也通过并关闭浏览器，但测试专用合成设备批量保活UPDATE与市场FOR SHARE事务发生40P01死锁，包级清理报告失败。PG日志保留双方等待与批量语句。将合成保活改为逐设备独立自动提交，保持原5秒节拍、45秒在线规则、全部设备及失败断言，不重试/吞掉错误。该测试夹具修正不证明生产死锁不存在，也不修复此前未证明根因的清理/存储/H5时序风险。

The fifth complete R020 run passed 249 / failed 1 / skipped 0. Native 115 cases, database core and Web passed; H5 page interactions also passed and the browser closed, but the test-only multi-device heartbeat UPDATE deadlocked with the market FOR SHARE transaction (40P01), reported by test cleanup. Both waits and the batch statement are retained. Synthetic heartbeats now use independent per-device autocommit statements, preserving the 5s tick, real 45s freshness, every fixture device and all error assertions, without retries or ignored failures. This fixture correction does not prove production deadlocks absent or resolve earlier unproven cleanup/storage/H5 timing causes. [Failure archive / 失败归档](../tests/evidence/R020-full-attempt5-archive.json)。


## R019 原清单与原始输入 / Restricted original delivery

R019接通已领取原租约的执行清单和原始输入受限交付，当前37/95显式控制操作、65表/664列/113外键。INT-094/095先登记权威契约与固定编号；两个GET仅接受原签名设备，不隐式领取。每次读取沿同一原订单优先锁序复核原买方key/路由/同单资金/资格、完整已占预算、原设备版本、输入、物理owner/双围栏和原期限。独立交付服务无签发私钥，输入必须通过原AES-GCM/AAD、独立版本化HMAC和同档位Preflight，按原始字节返回。客户端用独立公钥、原订单/路由/模式及本机内核批准核验清单和租约，有界接收输入；不自动批准内核或提交模型。此为PLAINTEXT/STANDARD可信平台经认证连接交付，密文摘要不能当原文明文签名，不是E2E或卖方不可见。领取/读取均不ACK、不续期、不重派、不完成DELIVER任务、不计费/清锁。001—010及端侧DDL原字节保持；真实宿主持久命令、tokenizer/内核/usage/渠道、E2E/LP/PE/租赁和完整验收仍待。

R019 adds restricted manifest and exact original input delivery for an already claimed lease: 37/95 explicitly registered control operations, 65 tables / 664 columns / 113 foreign keys. INT-094/095 are added to the authoritative contract and permanent registry first. Both GETs require the original signed device and recheck its original buyer key/route/order, same-payment funds, qualification, full existing hold, device version, input, physical owner, both fences and unchanged expiry using the shared order-first lock sequence. The delivery service has no issuer private key. Input requires original AES-GCM/AAD, separately keyed versioned HMAC and same-profile Preflight, then returns the original bytes. The native receiver requires an independent issuer pin and original order/route/mode plus locally approved kernel facts; it neither approves nor starts a kernel. This is a trusted PLAINTEXT/STANDARD authenticated transport hop: a ciphertext digest is not a signed plaintext commitment, E2E or provider blindness. Reads never ACK execution, renew, reassign, complete delivery jobs, bill or release a resource. Published 001–010 and client SQLite bytes remain unchanged. Durable host commands, real tokenizer/kernels/usage/channel, E2E/LP/PE/rental and complete acceptance remain open.

[DG细则](../../docs/技术开发/附录-原设备清单与输入受限交付.md) · [本轮完整待办](../progress/R019-本轮实现与完整待办.md)。配置只在忽略runtime，原生CLI执行仍关闭；buffer交给真实适配器/持久宿主前还有独立门槛。

## R018 原设备签名领取 / Native original-lease claim

R018新增原设备签名领取原租约：35/93个控制操作、65表/664列/113外键。仅原设备ED25519证明可领取；原命令编号与header相同，010将其固定绑定原lease并和安全事件原子提交。每次包括重试都重新核原买方scoped key/route、原订单与同单资金、资格、完整已占预算、加密输入元数据、原owner/双围栏和原期限；不换单、不重签/续租。领取服务只有显式公钥，不解密输入、不确认实际执行或计费；DELIVER任务、预算和资源保持原状。通用元数据队列仅认领/耗尽标记自身白名单类型，不能处理独立的执行投递意图。设备独立公钥/批准计划、输入/manifest交付、宿主持久执行命令、真实内核/usage/渠道及E2E/LP/PE/租赁仍待。001—009及端侧DDL保持原字节，历史UNBOUND_CLAIM。

R018 adds authenticated native delivery of the original signed lease: 35/93 registered control operations and 65 tables / 664 columns / 113 foreign keys. Only the original device proof can claim; its original command ID equals Idempotency-Key and is permanently bound to the same lease by forward migration 010 with one atomic safe event. Every recovery rechecks the original scoped buyer key/route, order and same-payment funds, eligibility, full existing hold, encrypted input metadata, physical owner, both fences and unchanged expiry. The delivery service has explicit public verifiers only: it cannot decrypt, issue, renew, reassign, ACK execution, complete delivery tasks or bill. Generic metadata workers can claim or exhaust only their supported kinds, leaving execution-delivery intents untouched. Independent device pins/approved plans, input/manifest transport, durable host execution commands, real kernels/usage/channel, E2E/LP/PE and rental remain open. Published 001–009 and client SQLite bytes are unchanged; history stays UNBOUND_CLAIM.

`POST /v1/p11/runner/leases/claim` 的随机原claim_key必须先由宿主保存，并与Idempotency-Key完全一致；库只冻结内存命令，不冒称已接通持久宿主队列。收到响应丢失时保留原正文/键，仅更换签名证明。新领取不能抢已绑定lease，过期/撤权不能重发旧凭证。

原生配置公钥文件只含 `profile_id=P11_LEASE_VERIFIERS_1` 与 `keys` 映射（ID→规范base64url 32字节Ed25519公钥），上限8192字节、1—16把。置于忽略runtime内，保持原issuer公钥可用；不从外来lease自动信任任何公钥。

[CL详细约束](../../docs/技术开发/附录-原设备签名领取与原租约交付.md) · [全部待办](../progress/R018-本轮实现与完整待办.md)。专项：`python -B -X utf8 dev/tools/原生数据库验证.py --round R018 --suite claim`，完整回归去掉suite；数据库/浏览器结束关闭，无本地虚拟化。

## R017 Native input and initial lease / 原生输入与首租约

R017接通原生文本输入和首租约内部服务：实际校验正文后以AES-256-GCM加密保存；重新核对原订单、当前scoped key/资格/同单资金和已占整笔预算，再原子取得明确FREE的原生设备资源，签发最多45秒且受原硬截止约束的首租约，连同双围栏、元数据manifest、安全事件和持久投递意图提交。原键恢复不换输入/owner、不续租；到期/撤销/未知保留原预算与资源。该加密仅保护PLAINTEXT模式落盘，不是客户E2E或提供方不可见；尚未挂载模型入口、接通实际领取/内容交付、生产密钥/独立信任、真实内核/usage/支付或LP/PE。009仅追加7列/1外键，001—008及端侧DDL原字节保留；仍34/93操作、65表。平台围栏受signed-bigint上限约束，达到上限关闭。

R017 connects internal native-text input and initial lease admission: the actual body is strictly validated and stored with AES-256-GCM. The original order, current scoped key/qualification, same-order funding and existing full per-call hold are rechecked before an explicitly FREE native-device resource, dual fences, metadata manifest, signed first lease (at most 45 seconds within the original hard deadline), safe event and durable delivery intent commit atomically. Original-key recovery never replaces input/owner or renews authority; expiry, revocation and unknown outcomes retain budget and ownership. This protects PLAINTEXT storage only, not customer E2E or provider blindness. Model listeners, actual claim/content delivery, production secrets/independent trust, real engines/usage/payments and LP/PE remain pending. Forward009 adds seven columns/one FK; 001–008 and native SQLite DDL retain their bytes. The scope remains 34/93 operations and 65 tables. Platform generations stop at the persisted signed-bigint limit.

[EI细则](../../docs/技术开发/附录-原生输入加密与首租约原子准入.md) · [本轮全部待办](../progress/R017-本轮实现与完整待办.md)。

`core.Preflight → Execution.Ready → Execution.Issue` 是内部有界文本流程；未接公开模型端口。`ReadyTx/IssueTx`仅组合SQL，提交确认前不能对外发送或运行。用 `python -B -X utf8 dev/tools/原生数据库验证.py --round R017 --suite execution` 运行专项，移除suite运行全部204项；先编译当前原生程序和前端。

## Historical R016 original-call preparation / 历史原调用准备

Current scoped API key → exact original route → original order write lock → current same-order funding/qualification → original-key recovery or atomic INPUT_PENDING + full per-call hold + call slot + safe outbox. This is preparation metadata; input-ready, physical owner/dual fences and qualified Lease/Grant issuance must precede execution. The count measures accepted preparation intents, including later cancellation/expiry, not successful paid generations.

同原键同参数恢复原job/预算且不重复计数；改参数/key冲突。只保存版本化HMAC，不保存正文或原始正文摘要；缺原指纹钥关闭。001—007和端侧DDL保原字节，008前向追加，仍34/93操作、65表。UI继续NB04、React/Taro优先与React/Vite，价格按小时或输入/输出分别M token。完整[RI-001—024](../../docs/技术开发/附录-原调用准备与次数预算原子接线.md)及[全部56任务/222子项](../progress/R016-本轮实现与完整待办.md)约束后续工作。

## R014 capacity accounting / 订单容量

Order-local counters conserve maximum = reserved + consumed + unverified + remaining. Forward 006 makes parent updates and reservation outcomes atomic, preserves legacy uncertainty, and binds consumption to an existing immutable trade qualification and original purpose. / 不由转发usage、HTTP EOF或取消推导费用/释放，不发送渠道付款、退款或执行授权。见[OBC-001—017](../../docs/技术开发/附录-订单预算预留与资格化消耗实施细则.md)。

R011 implementation: [native supervisor and backend recovery](../../docs/技术开发/附录-原生执行监督与后端恢复实施细则.md). A bounded watchdog uses the existing SQLite owner/fences, frozen backend bindings and original command IDs. Control drain/revocation closes admission and waits for confirmed stop/cleanup. Synthetic metadata adapters do not prove production engines, process isolation, E2E or GPU performance. Platform remains 31/93 operations and 65 tables.

R011实施为执行监督循环与原生控制联动；原请求恢复期间的提供方资料加载时序已在Web/Taro修正。真实内核、完整KernelRegistry、内容加密/支付及56任务/222子项仍待收口。

The detailed requirements and technical chapters are authoritative; this guide
connects them into a single system. Data-plane behavior below remains planned.
The delivered Go foundation is identified separately in the implementation tracker.

完整需求和技术章节为权威；本文串起完整链路。下述数据面仍为待实现方案，已交付 Go 基础以实施进度和真实测试证据单独登记。

## Actors and boundaries / 角色与边界

| Actor / 角色 | Responsibility / 职责 |
| --- | --- |
| Buyer / 买方 | Confirm seller, capability, privacy, price, payment and acceptance / 确认卖家、能力、隐私、价格、支付和验收 |
| Seller / 卖方 | Voluntary device sharing, installation consent, price and fulfillment / 自愿共享设备、确认安装、自主定价并履约 |
| Platform / 平台 | Marketplace, immutable contracts, scheduling, routing and transaction evidence / 市场、不可变合同、调度、转发和交易证据 |
| Local gateways / 本地网关 | Bind credentials, protect content and preserve supported standard API semantics / 绑定凭据、保护内容并保留已支持的标准接口语义 |
| Native engine / 原生内核 | Execute only approved model/workflow under an issued lease / 仅凭已签发租约执行获准模型或工作流 |
| Payment channel / 支付渠道 | Actual authorized collection, verification and supported refunds/settlement / 实际获准的收单、验付及其支持的退款、结算 |

The platform does not become the compute seller by pooling devices. Every order
retains its seller. Upstream subscription access is eligible only where the
provider's terms and authorization permit this use.

平台不通过设备池变为算力自营方。每单保留实际卖家；上游订阅只有明确许可和授权允许时才能挂牌。

## State and authority / 状态与执行权

```mermaid
flowchart LR
    Quote[Accepted quote / 已确认报价] --> Order[Immutable order / 不可变订单]
    Order --> Prepared[INPUT_PENDING / PREPARED]
    Prepared --> Checks[Input + funds + permission + capacity]
    Checks --> Lease[ISSUED signed execution lease]
    Lease --> Run[Native execution / 原生执行]
    Run --> Stop[Actual stop + cleanup]
    Run --> Delivery[Encrypted/plain result delivery]
    Delivery --> Receipt[Acceptance + usage evidence]
    Receipt --> Ledger[Accounting + eligible settlement]
```

`PREPARED` is not a GPU reservation. A lease includes both job fencing and the
physical resource generation. Releasing a GPU requires actual stop/cleanup;
delivery can continue under its own scope after compute ownership ends. A stale
worker cannot stop or clean a newer owner's work. Rentals retain their session
lock across subtasks and release it only at the session's actual termination.

`PREPARED` 不占 GPU；租约绑定任务围栏及物理资源代次。设备释放以实际停算、清理为准，交付可独立继续；旧 worker 不得停止或清理新所有者。租赁子任务保留父会话资源锁。

## Forwarding and privacy / 转发与隐私

Plaintext: SDK → contract-bound platform gateway → same-seller adapter → engine.
Encrypted: SDK → buyer-local gateway → platform ciphertext relay → seller-local
gateway → private pipe/JNI engine. Responses reverse the path. All media uses an
explicitly approved OpenAI/Anthropic-compatible profile; unsupported tool or event
semantics are rejected.

明文：SDK → 合同绑定平台网关 → 同卖家适配器 → 内核。加密：SDK → 买方本地网关 → 平台盲转 → 卖方本地网关 → 私有管道/JNI 内核；结果反向返回。媒体同样须经批准的兼容档位，不静默丢弃工具或事件语义。

The crypto candidate combines independently verified endpoint identity, separate
Ed25519 signatures, sealed stream keys and authenticated secretstream records.
Key purposes, directions and object/attempt contexts are distinct. Sequential
streaming and locally segmented random reads are different formats. Authentication
failure, replay, truncation or an unknown profile fails closed. No automatic
plaintext downgrade is permitted. See the [protocol authority](../../docs/技术开发/08-端到端加密协议与密钥实现设计.md).

密码候选组合独立身份核验、独立 Ed25519 签名、封装流钥和认证 secretstream；用途、方向、对象/attempt 分离。网络顺序流与本地随机分段分别定义。认证失败、重放、截断或未知档位拒绝，不能自动降为明文。精确版本和跨端验证仍未完成。

## Provider execution / 提供方执行

Native GPU calls remain native. Private LLM workers embed llama.cpp; the LM Studio
compatibility branch retains `lms`, llmster and runtime as replaceable layers.
ComfyUI runs a complete approved environment with a private bridge and finite node
allowlist. Android is a real LMA fork using internal JNI. Shared content is kept
out of ordinary UI/history/log/loopback interfaces; task files are encrypted,
while model files retain normal native loading. No VM/RAM disk/model duplicate is
introduced by the design.

GPU 调用保持原生；隐私 LLM 嵌入 llama.cpp，LM Studio 三层兼容保留。ComfyUI 完整环境、私有桥和有限白名单；Android 真实 LMA 二开走内部 JNI。任务正文不开放给普通 UI/历史/日志/loopback API；任务文件加密，模型正常原生加载，不引入 VM、RAM 盘或重复常驻模型。

## Payment and acceptance / 支付与成交

Payment proof is not execution permission and not result acceptance. Verify the
original payment reference and its binding before funding qualification; issue
execution rights only after all other checks. Unknown financial outcomes query
the original operation. Trusted consumption, result acceptance, journal entries,
refund obligations and seller settlement remain separate. Do not invent an
unverified channel settlement endpoint or assume C2C seller onboarding is approved.

支付凭据不等于执行权或结果验收。先按原号核验支付及绑定，再形成资金资格；其余检查通过后才授权执行。资金未知查原操作。可信用量、接受、记账、退款义务和卖家结算分开，不虚构未证实的渠道结算接口或假定个人准入已获准。

## Authority map / 权威入口

| Topic / 主题 | Source / 来源 |
| --- | --- |
| Requirements / 需求 | [Whitepaper navigation](../../docs/白皮书/00-阅读导航与需求基线.md) |
| Gateway / 网关 | [Protocol forwarding](../../docs/技术开发/07-统一协议网关与Sub2API改造.md) |
| Provider / 提供方 | [Kernel packaging](../../docs/技术开发/附录-供给端内核封装与替换规范.md) |
| Local protection / 本地保护 | [Low-overhead design](../../docs/技术开发/附录-低开销隐私执行完整方案.md) |
| Payments / 支付 | [Collection and settlement](../../docs/技术开发/13-支付退款账务与渠道结算.md) |
| Development / 开发 | [56 tasks / 222 steps](../开发细则总表.md) |
| Actual evidence / 实际证据 | [Implementation progress](../progress/README.md) |


## Current identity implementation / 当前身份实施

R004 separates verified Alipay identity exchange, single-use challenge state, current user/role/session checks and a 13-route native HTTP adapter. Authorization codes and channel tokens are not stored; session/API-key digests use HMAC. WEB uses HttpOnly/Strict Cookie plus Origin/CSRF; MINIAPP uses memory-held Bearer sessions. The transports are not interchangeable. Trade mutations recheck credentials and roles before replay. Registration is BUYER only, never KYC or seller approval.

R004将支付宝验签适配、一次性挑战、当前用户/角色/会话及13操作原生HTTP分层。渠道码/令牌不落库，凭据摘要用HMAC。Web只读Cookie加Origin/CSRF，小程序内存Bearer，禁止混用；交易回放前重核身份/角色。初始仅BUYER，不能冒称KYC或卖家资格。

The embedded route manifest is checked against 93 canonical operations. Forward 003 adds two identity tables to the immutable 60-table base (62 current). All 34 local cases passed with signed synthetic provider responses; real Alipay authorization, UI, payment and provider execution remain pending. See [identity/HTTP authority](../../docs/技术开发/附录-身份登录与HTTP交易实施约束.md).

嵌入路由对照93操作权威契约；003前向新增2身份表，基础60表原字节保留，当前62表。34项局部测试使用签名合成响应通过，真实授权、UI、支付及提供方执行仍待完成，不能将有限注册当全平台可用。


## R005 seller submissions and future revisions / 卖家申请与未来修订

Account-owned seller applications retain submitted claims without granting SELLER or copying requested regions into approved regions. New listing definitions resolve owned model/capability rows, validate the explicit platform fee policy, create immutable revisions and use strong resource ETags for concurrent changes. Submission rechecks exact scope approvals and remains SUBMITTED; controlled review/publication is still required. Exiting atomically disables new admission while retaining sold contracts, money records and resource fences.

本人申请仅留待审材料，不授SELLER或批准地区；挂牌从本人设备/能力推导具体型号与环境，核对显式平台费用，新增不可变修订，以强ETag处理并发。提交重核组合证据后仍待审，受控审核发布未完成。退出原子停止新准入并保留已售合同、资金和资源围栏。详见[实施细则](../../docs/技术开发/附录-卖家申请与挂牌版本实施约束.md)。R006阶段为64表、23/93操作，45项Go与13项TS局部验证；已有实际Web/Taro H5交易控制页，支付宝原生仅编译，真实渠道/设备、完整前端与产品仍未验收。


## R007 provider control / 提供方身份控制

The current catalog has 65 tables and 31/93 control operations. An approved account owner creates an explicit short-lived pairing credential; the native installation registers a unique Ed25519 public key and a concrete self-reported model. The credential is disclosed only once, registration starts PAIRED with sharing off, and original enrollment recovery returns the same identity. No capability or permission is inferred from a model name.

当前65表、31/93操作。本人合格卖家明确配对，本机登记独立公钥与具体自报型号；秘密只显一次，原登记意图恢复同设备。默认PAIRED/不开共享，型号不等于许可或可售资格。

P11_DEVICE_ED25519_1 proofs bind exact routing/body and command headers, ±60-second timestamps and persistent nonces. Device heartbeats carry strictly increasing uint64 decimal sequences and current policy versions; server receipt time governs liveness. Neither IDLE nor replayed Ack renews authority or clears resource locks. Account-owned policy updates, drain and revoke preserve existing attempt owners/generations, including quarantine. Device identity keys are separate from E2E content keys.

签名绑定原路由/正文/幂等键/ETag/媒体类型，nonce持久防重放。心跳严格递增且绑定当前版本，服务端时钟决定在线；IDLE/原Ack回放不续权、不清锁。本人策略/停止/撤销保留在途owner/代次与隔离；控制钥与E2E内容钥分离。

At R007 the native package was a bounded signing/HTTP transport. R008 below adds a persistent native control agent and OS key adapters; hardware qualification, execution inbox/stop/cleanup and LMA/engine integration remain pending. R009 implements the owner control UI; full execution remains independent. See [R007 control specification](../../docs/技术开发/附录-设备配对身份与控制协议实施约束.md) and [historical local evidence](../tests/evidence/R007-验证报告.md); real channel/device/product acceptance remains unexecuted.

## R008 native control agent / 原生控制代理

p11-node is an explicit foreground, outbound-only control process. Current-user DPAPI on Windows or an unlocked Linux Secret Service protects a random state master. The bounded AES-GCM journal retains the installation seed, exact pending enrollment/heartbeat and counter high water; an OS writer lock spans network operations. A write reaches the network only after its original intent is durably saved. Restart/unknown replies preserve the original identity/body/key and sign fresh nonces. Windows DPAPI, ACLs and actual process termination/recovery are locally exercised; Linux keyring behavior remains protocol-fixture/cross-compilation evidence only.

本人决定初始化、登记和前台运行，status提供不含秘密的型号/设备/版本/序号投影。明确原心跳拒绝后先查当前身份/版本/序号，才允许下一心跳；未知结果不换命令。当前只报UNKNOWN，不启动内核、不接受任务、不报空闲、不清锁或产可信usage。同用户存储保护不是客户内容防护，独立低权限执行、SQLite inbox、统一资源门闩及内容网关仍待完成。详见[完整本地恢复规范](../../docs/技术开发/附录-原生提供方控制代理与本地恢复.md)。

原生Go包只交付签名/HTTP库；系统钥库、设备资格、本机执行/停止清理与LMA/内核仍待完成。R009接入提供方配对/策略/停止撤销，完整执行另行验证。真实渠道/设备与完整产品验收不因局部控制测试改变。

## R009 owner control UI / 本人设备控制界面

React/Taro and React/Vite share owner-bound DTOs and a strict non-secret command allowlist. Pairing secrets stay in page memory and are removed on lifecycle/authority changes. Original POST/PATCH method, key, body and strong If-Match survive lost replies. Policy ACK and current device state are separate projections; stale versions require a fresh read. Drain/revoke never release an execution fence or assert cleanup. The actual local Windows CLI journey is evidence of control identity only; GPU/Android, content E2E, payments and full acceptance remain pending.

设计承接NB04共享页，详见[完整双端细则](../../docs/技术开发/附录-提供方双端设备管理与原请求恢复.md)；不新增API/数据表/迁移，31/93操作、65表、001—005原字节保持。

## R010 native metadata gate / 原生执行元数据门闩

The existing Lease DTO is verified against independent approved order/manifest/kernel state and a separately trusted platform public key. The native gate persists inbox/receipt, uint64 job/resource high waters, local owner generation and original backend mapping in one bounded SQLite store. The same node OS lock protects both the encrypted control anchor and database lifecycle. Managed local work, maintenance and jobs share the physical resource; signed rental ownership remains pending.

重开、双截止或回拨关闭执行并保留owner；重复提交只查原backend。签名新修订才可续短截止，当前backend确认停算和完整清理经owner比较更新后才FREE。SQLite不存正文/内容钥且不是加密内容库；状态机不代替实际停止/删除，也不开启完整执行。具体流程/编号/固定签名域与剩余门槛见[原生执行细则](../../docs/技术开发/附录-原生执行收件箱与资源门闩实施细则.md)。

## R012 bound text relay / 固定路由文本转发核

A core owns one immutable seller, route, contract, profile, model mapping and upstream endpoint. Exact JSON/text preflight precedes the explicit durable admission port; upstream secrets are separately resolved by seller/reference. Native dialect state machines preserve actual terminals and bounded usage candidates. No pool, redirect, environment proxy, POST replay or cross-mode fallback is inherited.

平台STANDARD明文域与未来卖方E2E_BASIC解密域由可信配置区分，CipherRelay不使用正文核。库仅取消HTTP并返回Outcome，原执行停止/清理、可信计费和入站租约由其他生产模块接入；未挂载可售入口。详见[完整BRC细则](../../docs/技术开发/附录-固定路由协议转发核心实施细则.md)。

Exact-field follow-up: request/response field names use whole-name equality, including delimiter-bearing negative cases. All 20 current relay cases pass, while the latest full rerun failed seven existing short-deadline cases. The 114 unchanged-package cases use only immutable, identical-source baseline evidence; six serialized diagnostic failures are retained too. / 当前补充验证受限，不能宣称全量通过，见[逐项证据和失败](../tests/evidence/R012-验证报告.md)。

## R013 metadata budgets / 元数据预算与固定截止

A cancellable native metadata gate distinguishes pre-SQL cancellation from ambiguous SQL/commit failures. Receive/Renew anchor a fixed monotonic deadline before transaction work; Activity returns that deadline and recalculates remaining authority after commit. Each metadata step has independent bounded I/O, while adapters retain their original call/lease limits.

MetadataTimeout默认2秒、20毫秒—5秒，受退出总截止约束；已知相同映射和已持久停止阶段不重复写。停止、清理及owner比较规则保持，详见[NMD约束](../../docs/技术开发/附录-原生元数据事务与执行截止修正.md)。


## R015 confirmed-route control / 已确认路由控制

R015新增已确认订单路由与标准SDK key绑定：明确原订单/同单支付引用，固定个人卖家、商品版本、模型、文本协议、价格、预算、次数上限和有效期。三个本人控制接口使已实现操作增至34/93；ResolveTx仅在key绑定范围内精确解析，同名歧义/失效/协议误配拒绝，不跨卖家兜底。007只追加5列，旧路由隔离；API启动改为核对所有内嵌迁移登记。当前只实现PLAINTEXT/STANDARD及三种native文本profile，路由不是执行或资金授权，调用签发/次数记账、真实付款、E2E和UI路由配置仍待完成。见[RC-001—022](../../docs/技术开发/附录-已确认订单路由与标准SDK绑定实施细则.md)、[R015实际证据](../../dev/tests/evidence/R015-验证报告.md)及[全部56任务/222未收口子项](../../dev/progress/R015-本轮实现与完整待办.md)。

## R022 original result scope

Original native output now moves into a separate AES-256-GCM result store before compute cleanup. Defaults reserve 16 × 8 MiB for five minutes. Synchronized original compute can release its GPU while its exact order/local-generation result remains readable. Process-only keys, one reader, reserved quotas and expiration bound custody; key loss or ambiguous submission never reruns inference. PIPE_2 adds actual input counts and the matched original stop index.

An authorized original result renders OpenAI Chat, Responses or Anthropic Messages as JSON/SSE with UTF-8, usage options, stop semantics and bounded writes. Defaults are 512 KiB JSON and 64 KiB per event. Responses repeats final text and needs explicit capability budgeting before public activation; overflow cannot emit a false success tail. A local write Receipt is not buyer ACK, trusted usage or payment evidence.

This is a PLAINTEXT/STANDARD source subset, not E2E/LP. Node CLI execution stays disabled. Seventeen new regular Go cases bring the manifest to 275; six separately tagged actual Windows worker/synthetic C ABI cases include encrypted custody and all three protocol outputs. Real models/GPUs, a separate execution identity, independent official SDK certification, public result transport, funds and full acceptance remain pending. The early supervisor shutdown timeout and first tagged failure are preserved; later passing runs do not resolve historical timing risks.

[RC-001—060](../../docs/技术开发/附录-原生文本结果加密保管与标准协议交付.md) · [R022 evidence](../tests/evidence/R022-验证报告.md) · [All 56 tasks](../progress/R022-本轮实现与完整待办.md).
