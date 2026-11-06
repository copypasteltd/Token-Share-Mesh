# Usage / 使用指南

R026继续补齐[小时租赁源码 RN-001—110](../../docs/技术开发/附录-小时租赁授权与累计计量源码细则.md)：原本机批准/加密journal/共享物理门闩/串行inbox之外，新增独立原grant认证、三标准文本协议预检、仅驻内存的随机AES-GCM一次输入、租赁专用llama私有管道适配、不等SQL的本机到期循环与节点有主关闭。不假造T1 job/attempt，不换卖家/设备/模型/隐私档；真实原同步/清理回执及进程/管道join后才可能释放本机槽。参见[租赁执行模块](../src/client/rentalruntime/README.md)。

**SOURCE_ONLY / UNVERIFIED.** All 67 rental test definitions remain unexecuted; no compilation, migration, model/GPU/phone run occurred. This RAM AEAD covers GPU_RENTAL/STANDARD text and provides neither network E2E nor provider blindness. Unknown stop/cleanup retains the original owner; unjoined shutdown retains the node lock/master key. Public SDK/model listeners, owner approval UI/independent readiness, real acquiring/paid renewal, confirmed metering, Android rental media, E2E/LP and platform cleanup/budget release remain open. Counts stay at 48 control/nine rental HTTP handlers, 98 planned operations and 71 platform tables. Published DDL bytes are unchanged. All 56 tasks/222 steps remain open until full-scope code precedes batch validation. Internal use: prepare the original physical hold; explicitly OpenNative during PREPARING; attach independently pinned current control; SealTextInput then Invoke. Discard unused envelopes. Query/Cancel/Output reauthenticate current credentials; close and clear owned outputs. These Go methods do not open a public listener.

## R021 · 原生 C API worker 与受限管道

本轮增加可替换的 llama.cpp 原生 C++ worker 与 Go InputAdapter：独立本机批准包/模型/模板及逐文件摘要；三文本请求严格映射到同一模型模板与真实 tokenizer；受限匿名内容管道、独立取消、两个64KiB输出缓冲及原 owner 一次提交。模型权重可复用，每任务同步、清KV并释放上下文后才发原清理回执；进程退出保持 UNKNOWN。

`本机批准包 → 独占资源准备 → Open暖机 → 可信HostKernel注册 → 原ticket.Start → 授权sink读取 → native同步清理 → 原凭证投影`。当前节点CLI执行仍关闭；对外输出/SSE/ACK、可信usage、真实模型/GPU、独立低权限服务、E2E、LM Studio兼容内核及其他设备路线继续待验。合成 C ABI 动态库测试证明实际进程接线，不证明真实推理或LP等级。

[42条内核约束](../../docs/技术开发/附录-原生LLM受限管道与可替换worker.md) · [本轮证据](../tests/evidence/R021-验证报告.md) · [全部56任务与222子项](../progress/R021-本轮实现与完整待办.md)。原R016/R019/R020时序与归档限制继续保留，最终交付目标ACTIVE。

## R021 · Native C API worker and bounded private pipes

This round adds a replaceable llama.cpp C++ worker and Go InputAdapter. Locally approved pack/model/template hashes control loading; three strict text profiles preserve their semantics before the approved chat template and actual tokenizer. Directed anonymous content pipes, a separate cancellation pipe, two 64-KiB output buffers and original-owner once-only submission bound execution. Model weights are reused; each task synchronizes, clears KV and frees its context before its original cleanup receipt. Process exit remains UNKNOWN.

`Owner-approved pack → exclusive resource preparation → Open/warm-up → trusted HostKernel → original ticket.Start → authorized content sink → native synchronization/clear → original cleanup projection`. Node CLI execution remains disabled. Public output/SSE/ACK, trusted usage, real models/GPUs, a separate low-privilege service, E2E, LM Studio compatibility and other device kernels still need implementation/qualification. Tests with a synthetic C ABI DLL exercise the actual Windows process; they are not real inference or LP acceptance.

[42 kernel constraints](../../docs/技术开发/附录-原生LLM受限管道与可替换worker.md) · [Current evidence](../tests/evidence/R021-验证报告.md) · [All 56 tasks and 222 open steps](../progress/R021-本轮实现与完整待办.md). Historical R016/R019/R020 timing and archive limitations remain; the delivery goal is ACTIVE.

开发机只构建与测试合成ABI：先运行 `python -B -X utf8 dev/tools/构建原生LLM内核.py --fetch --fixture`，再运行 `python -B -X utf8 dev/tools/核验原生LLM内核.py`。这些命令不供应或启动真实模型/GPU。真实包的授权、安装、资源门闩和资格完成前，不开启执行入口。

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

原生宿主若缺少原任务对应的环境、能力或清单内核登记，必须保留原资源占用。当前监督器的登记在创建时冻结，无法在本次运行中自动修复；关停会报告未完成，同时仍对其他已匹配的原任务执行取消。恢复时须重新提供同一原环境的可信适配器并查询原任务，确认停止和清理后才释放；不能换一个相似内核重新提交。R019未知登记修正的完整失败记录和针对性回归见DG附录，慢元数据时序风险继续开放。

If the native host lacks the original environment/capability/manifest binding, retain that resource's original occupied owner. The supervisor freezes its registry at construction, so that run cannot repair the binding automatically. Shutdown reports incomplete cleanup while still cancelling other matching original owners. Recovery must provide the same trusted original adapter and query the original work; release requires confirmed stop and cleanup. Do not resubmit through a similar kernel. The DG appendix retains both failed complete runs and the targeted registry regression; the separate slow-metadata timing risk remains open.

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

## R014 order capacity / 订单容量

Trusted application transactions can compose `budget.ReserveTx`, `CompleteTx`, `LookupTx` and `SnapshotTx`. Authenticate and check actual funding/route/lease eligibility independently before execution. Complete takes an existing original qualification reference, never a client amount or usage candidate. On unknown commit query the original purpose. / 容量不是钱包或已付证明；未知保留、旧数据保守持有，见[OBC细则](../../docs/技术开发/附录-订单预算预留与资格化消耗实施细则.md)。

## R013 native metadata / 元数据与执行截止

Trusted supervisor embeddings may set Options.MetadataTimeout (default 2s, 20ms–5s) independently of CallTimeout. Each metadata step gets its own budget, capped by an earlier shutdown deadline. The CLI exposes no engine or public execution listener. See [NMD-001–013](../../docs/技术开发/附录-原生元数据事务与执行截止修正.md) and [current native evidence](../tests/evidence/R013-验证报告.md).

SQL开始前取消只拒绝该次操作，不重建/清空健康账证；SQL或提交不确定仍关闭。原owner必须查、停、清后才能复用。Activity.Deadline是事务前锚定的固定原生截止，不能用Remaining在返回后重新计时。当前严格回归选--round R013；旧R012失败和受限对照仍为历史记录。

## R012 relay embedding / 转发核使用

Run the native protocol tests from the clone root:

```powershell
python -B -X utf8 dev/tools/原生开发.py test -count=1 ./gateway/core
```

Production embedding supplies an independently resolved immutable Binding, a durable current-route/funding/owner Authorizer and seller-scoped SecretSource to core.New. Forward returns submission/protocol facts; ServeHTTP is available only to that trusted host. The writer must support read/write deadlines and flush. Close the owned core/server after use. No inference command/listener or permissive admission is installed by the current CLI.

先完成路由确认/预算及执行授权，再让可信生产端口授予原调用一次提交权；SDK只用标准model/正文与本站认证，不填卖家或后端私有字段。未知网络结果查原调用，不能再次Admit重跑。当前只有明确合成夹具可运行测试，不是付费服务启动说明。

三档都是同方言受限文本，Responses必须store=false、Messages固定2026-10-21；未支持字段先拒绝。STANDARD平台明文与未来E2E_BASIC卖方解密域分开配置；没有内容加密接线。详见[完整字段、流状态与时限](../../docs/技术开发/附录-固定路由协议转发核心实施细则.md)。

Current follow-up validation is limited: 20 relay cases pass; 114 other cases reuse byte-identical source evidence from the initial published full pass. The latest full rerun failed seven existing short-deadline cases, and the serialized diagnostic failed six. Do not interpret source/build or browser checks as a full-suite pass. / 当前20项转发通过、114项相同源码对照复用；末次全量7失败及串行6失败见[完整记录](../tests/evidence/R012-验证报告.md)，未放宽时限或清理规则。


## R011 supervisor integration / 执行监督接入

Use the trusted Go embedding API only: explicitly initialize the registered node execution ledger, construct node.NewSupervisor with exact environment/capability/manifest adapters, start node.RunSupervised and wait for Ready before Submit. Keep the original OS lock until RunSupervised returns. Pending control commands and DRAINING/REVOKED state latch new admission closed while original-owner query/stop/cleanup remains available. No CLI task input, trust-key installation or engine configuration is enabled.

可信宿主调用顺序为显式登记→execution-init→NewSupervisor→RunSupervised；同一OS锁下等待退出完成再关闭节点。租约到期/回拨先撤权并取消原请求，查询确认停算后清理，再按owner代次释放；Cancel ACK、查询超时、PID消失或退出超时均不释放。当前适配器测试仅合成元数据，生产引擎/低权限/真实文件钥清理仍待。完整参数及错误见[NSV-001—020](../../docs/技术开发/附录-原生执行监督与后端恢复实施细则.md)。

Web/Taro在原配对恢复期间进入共享页，会等待请求空闲后自动尝试一次本人资料读取；读取失败保留显式刷新，不自动重试写请求。NB04视觉、具体型号及小时/M token不变。

## R010 native metadata commands / 原生执行元数据命令

After explicit owner enrollment, run `p11-node execution-init --state <same-private-absolute-directory>` once, then `p11-node execution-status --state <same-private-absolute-directory>`. Both commands run in the foreground and close their database/OS lock before returning. Initialization requires no pending control command and a non-revoked registered identity. Do not initialize a different directory to bypass an ambiguous/lost state. There is no reset, force-free or lease-input command.

本人登记后新增execution-init/status，第一次先加密锚定后建库，第二次打开原库；只输出档位、资源状态/代次、收据数量。缺库、坏库、绑定错误或DDL漂移保持关闭，不重建；旧owner重开先隔离，再由未来真实适配器查/停/清，不自动恢复推理。不能与持有同目录OS锁的run并行调用；当前没有UI绕锁直读库。用户仍自主决定安装、运行/停止程序，本轮无模型或后台服务启动。

SQLite is metadata-only and not encrypted content storage. It accepts no prompt/output/content key. Exact database/WAL/SHM files remain in the private state directory under runtime; never commit or publicly copy them. No real backend execution is enabled. The verification runner uses independent public synthetic signatures and explicit synthetic identities/qualifications; it is not a production lease issuer.

当前真实浏览器联调每端运行init/enroll/execution-init/execution-status/heartbeat/撤权后heartbeat共六个Windows进程；元数据库始终0资源/0任务、execution_enabled=false。Go局部用例另外验证合成任务的并发、异常退出、双截止和owner清理状态机；不能替代真实GPU或内容隐私验收。源码改动后先完成依赖整理，再运行 `核验原生源码.py --round R010`、`原生数据库验证.py --round R010`、`核验前端源码.py --round R010`、`整理原生验证证据.py --round R010`。真实数据库/自建浏览器/子进程全部结束关闭。

See [complete implementation constraints](../../docs/技术开发/附录-原生执行收件箱与资源门闩实施细则.md), [R010 evidence](../tests/evidence/R010-验证报告.md) and [all 56 tasks/222 outstanding steps](../progress/R010-本轮实现与完整待办.md). Historical flows below retain their round labels; explicitly select the intended --round rather than relying on a script default.

## R009 provider page flow / 提供方页面操作

| Step / 步骤 | Owner action / 本人操作 | Actual boundary / 实际边界 |
| --- | --- | --- |
| 1 | Authorize, open sharing, check APPROVED SELLER / 授权后打开共享页，核对已审核卖家 | No UI self-approval; fixtures only in tests / 页面不能自批 |
| 2 | Choose Windows/Linux/Android, name the device and confirm ownership / 选择系统、设备名并主动确认 | Actual device cards show concrete models / 设备显示具体型号 |
| 3 | Create pairing, explicitly reveal the masked credential and use owner-controlled stdin in p11-node / 创建配对、显式显示并在本人程序受控输入 | Five-minute once-only secret stays in page memory; never URL/arguments/logs/screenshots / 五分钟、只首次、默认遮盖 |
| 4 | Refresh own devices and check ID/model/fingerprint/version / 刷新本人设备并核对身份 | PAIRED and UNKNOWN do not grant sale or execution / 登记与控制上报不是可售资格 |
| 5 | Set ordered Beijing-time windows, MB reserve, phone conditions and stopping responsibility / 设置北京时间时段、磁盘MB、手机条件和停止责任 | One slot; 1 MB=1,000,000 bytes, max100 windows, each≤30days and span≤90days / 单槽、有界条件 |
| 6 | Save, then refresh current device version / 保存后刷新当前设备版本 | ACK proves platform persistence, not device enforcement / 保存不等于执行生效 |
| 7 | Confirm drain or irrevocable identity revocation separately / 分别确认停止接单或撤销身份 | New admission closes; existing work needs real stopping/cleanup / 不冒称GPU清理完成 |
| 8 | On an unknown reply reload and choose 核对原请求 / 未知时刷新并核对原请求 | Preserve original method/key/body/If-Match; never overwrite at a newer version automatically / 不换键、不自动覆盖 |

Hiding, backgrounding, leaving, expiry, logout or 401/403 removes the pairing secret and suppresses late secret adoption. A lost first response recovers only the original intent metadata. If needed, explicitly confirm a new intent; existing expiry/three-pending-intent limits still apply. Policy forms reset responsibility consent when edited. Terminal and legacy identities are read-only; quarantine prevents policy editing. This page is not the buyer content-gateway pairing flow.

离开/后台/隐藏/到期/退出或撤权清除秘密，晚到响应不放回。首次回执丢失只能恢复原申请元数据；需要另开时重新主动确认，仍受服务端限制。表单修改后重新确认，旧身份和终态只读，隔离关闭策略编辑。提供方控制不等于买方内容加密网关，不收Prompt。Android页面条件可规划/保存，真实LMA控制程序与真机仍待实现；Linux钥库仍需物理机实测。

Reproduce the actual local control journey after building: `python -B -X utf8 dev/tools/核验原生源码.py --round R009`, then `python -B -X utf8 dev/tools/原生数据库验证.py --round R009` and `python -B -X utf8 dev/tools/核验前端源码.py --round R009`. The temporary database, owned browsers and native command processes are closed. Test seller approval/OAuth/market hardware are explicit synthetic fixtures; Windows CLI identity/enrollment/control signatures and HTTP persistence are real, with execution disabled.

See [provider UI constraints](../../docs/技术开发/附录-提供方双端设备管理与原请求恢复.md), [R009 local evidence](../tests/evidence/R009-验证报告.md) and [all 56 tasks/222 pending steps](../progress/R009-本轮实现与完整待办.md).

## Frontend builds / 双端构建

Native Node22.14.0/npm10.9.2 is pinned by the frontend manifests. Run from the clone root. The wrapper copies tracked sources and keeps dependencies, npm cache and build output under runtime/frontend; install scripts are disabled. No Docker/WSL/emulator is used.

```powershell
python -X utf8 dev/tools/前端开发.py bootstrap
python -X utf8 dev/tools/前端开发.py check
python -X utf8 dev/tools/前端开发.py test
python -X utf8 dev/tools/前端开发.py build
```

Builds produce miniapp-alipay, miniapp-h5 and web under runtime/frontend/builds. Native login requires an explicit P11_FRONTEND_API_BASE compiled in and actual registered Alipay identity; an empty address disables it. Web/H5 use the same-origin control API by default. These outputs are application sources/builds, not a deployment plan. See [frontend details](../src/frontend/README.md).

使用顺序为授权→发现个人设备→选实际卖家/模式→填时长或双向token上限/预算→正式报价→主动勾选披露→建单等待付款。E2E可信程序未配对则关闭继续；页面不收Prompt。原生显式nonce档与Web/H5 S256档分开，参见[完整约束](../../docs/技术开发/附录-双端交易页面与授权恢复实施约束.md)。未知写刷新后核对原请求，保留相同键和参数；本机历史仅订单号，不能当全历史。共享页展示本人资格/挂牌，并支持本人设备配对/策略/停止撤销；完整挂牌编辑、收益结算及付款未交付。

After building the frontends and native binary, `python -X utf8 dev/tools/原生数据库验证.py --round R009` exercises actual compiled Web/Taro H5 with native HTTP/PostgreSQL, synthetic OAuth, qualification and fixture-device heartbeats only. It captures genuine screenshots and recordings and checks one order/event after lost-reply recovery. It closes its browser/database. Run `python -X utf8 dev/tools/核验前端源码.py` to consolidate current evidence. Native Alipay rendering, real authorization, devices and payments remain unverified.

## Existing runnable software / 当前可运行内容

Use a native Windows amd64 development machine, Python 3.13 and Node.js 22+.
Execute the following commands from the clone root. Linux is a target platform;
the current Windows bootstrap is not a Linux installer.

使用 Windows amd64 原生开发环境、Python3.13、Node.js22+，命令从克隆根执行。Linux 为目标平台，当前 Windows 工具入口不是 Linux 安装器。

```powershell
python -X utf8 dev/tools/文档工具.py bootstrap
python -X utf8 dev/tools/文档工具.py build
python -X utf8 dev/tools/文档工具.py check
python -X utf8 dev/tools/预览文档.py --port 8765
```

Visit the printed local URL. Search by `P11-F74`, `端对端` or another keyword;
click a result to reach the original chapter. Escape clears a query. Printing
uses the reader's print view. Stop the preview with Ctrl+C. An occupied port can
be avoided with `--port 0`, which prints the chosen loopback port.

访问打印出的本机地址，搜索编号/关键词，点击结果跳转，Escape 清空；阅读器支持打印视图。Ctrl+C 停止。端口占用时用 `--port 0` 自动选择并打印本机端口。

```powershell
python -X utf8 dev/tools/获取原生工具链.py
python -X utf8 dev/tools/原生开发.py mod download
python -X utf8 dev/tools/原生数据库验证.py --round R006
python -X utf8 dev/tools/核验原生源码.py --round R006
python -X utf8 dev/tools/整理原生验证证据.py --round R006
```

The database command creates its own random-credential, loopback-only PostgreSQL17
instance inside `runtime/db/integration/`. It runs actual migrations and tests and
confirms shutdown. It does not connect to your other databases. For non-ASCII
Windows paths it temporarily aliases this project path and removes only its own
alias after shutdown confirmation. If cleanup fails, recover that specific
instance before removing the retained alias. Never stop every PostgreSQL process
or delete another project's drive mapping.

数据库入口在项目内创建随机凭据、仅 loopback 的 PostgreSQL17 实例，执行真实迁移/测试并确认关闭，不连接其他数据库。中文路径通过临时路径别名处理，停库确认后只撤销自己创建的映射。失败时先恢复、停止明确的本次实例，不能批量停所有 PostgreSQL 或删除其他项目映射。

## Existing database CLI / 已有数据库命令

For a PostgreSQL17 development database you explicitly selected, provide
`P11_DATABASE_URL` through your local environment. Do not commit a connection
string or put a password in an example command. The command names are:

对本人明确选择的 PostgreSQL17 开发库，通过本机环境提供 `P11_DATABASE_URL`，不把连接串提交或把密码写入示例命令。

```powershell
python -X utf8 dev/tools/原生开发.py run ./api/cmd/p11-admin migrate
python -X utf8 dev/tools/原生开发.py run ./api/cmd/p11-admin check-schema
```

`migrate` uses a transaction advisory lock and immutable migration hashes.
`check-schema` checks the presence/count of domain tables and migration metadata;
it is not a full schema-diff, permission or recovery audit. No destructive down
migration or database-reset command is provided. Production role separation and
full restart/upgrade recovery remain in DEV-02.

`migrate` 有同事务互斥和不可变摘要；`check-schema` 只检查领域表数量及迁移元数据，不等于完整差异、权限或恢复审计。没有 down/清库命令；正式权限分离和完整升级、恢复仍在 DEV-02。

## Native identity/control API / 原生身份控制接口

Configure these values explicitly in your local environment. All *_FILE values point to files you own inside the ignored runtime area; no real secret belongs in source or example commands. HMAC files contain at least 32 bytes and must stay stable across restarts. The identity digest key requires a migration before replacement. RSA material uses one PEM key per file, at least 2048 bits. This adapter supports RSA public-key mode, not certificate mode.

在本机环境显式配置下表。*_FILE指向runtime内本人持有的文件，不将真实秘密放入示例命令/源码。HMAC文件至少32字节，重启不能随意重生；更换身份摘要密钥须先做迁移。RSA每文件一把PEM密钥、至少2048位；当前为公钥模式，未实现证书模式。

| Variable / 变量 | Meaning / 含义 |
| --- | --- |
| P11_DATABASE_URL | Explicitly selected migrated development DB / 已选且已迁移的开发库 |
| P11_ALIPAY_APP_ID | Authorized application ID / 本应用ID |
| P11_ALIPAY_SUBJECT_FIELD | Explicit OPEN_ID or USER_ID; no fallback / 明确选字段，不回退 |
| P11_ALIPAY_PRIVATE_KEY_FILE | Application RSA private key path / 应用RSA私钥路径 |
| P11_ALIPAY_PUBLIC_KEY_FILE | Trusted Alipay RSA public key path / 可信支付宝RSA公钥路径 |
| P11_ALIPAY_OAUTH_RETURN_URL | Registered WEB callback; omit only if WEB login stays closed / Web已登记回调，未配则Web登录关闭 |
| P11_AUTH_PEPPER_FILE | Stable session/CSRF HMAC secret / 稳定会话摘要密钥 |
| P11_IDENTITY_PEPPER_FILE | Stable identity/code/browser-binding HMAC secret / 稳定身份、授权码和浏览器绑定摘要密钥 |
| P11_API_KEY_PEPPER_FILE | Stable API-key HMAC secret / 稳定API密钥摘要密钥 |
| P11_CURSOR_KEY_FILE | Stable signed-page cursor secret / 稳定分页签名密钥 |
| P11_BROWSER_ORIGIN | One exact allowed browser origin, no path / 唯一明确允许的浏览器源，无路径 |
| P11_API_ADDR | Optional numeric loopback, default 127.0.0.1:8787 / 可选数字本机地址 |
| P11_INSECURE_LOOPBACK | Set 1 explicitly for HTTP loopback development cookies / 本机HTTP开发显式为1 |

```powershell
python -X utf8 dev/tools/原生开发.py run ./api/cmd/p11-api
```

The command requires migration version 4 and listens only on loopback. It does not migrate, create test identities or execute models/payments. No server deployment, TLS setup or observability work is introduced. For local HTTP development use an exact loopback browser origin and explicit insecure-loopback flag; development cookies have separate names. Default Cookie policy remains Secure/HttpOnly/Strict.

命令要求迁移版本4，只监听loopback，不自动迁移、造测试身份或执行模型/支付。不增加部署/TLS/可观测性工作。本机HTTP使用精确loopback浏览器源和显式开发标志，Cookie名称独立；默认策略仍为Secure/HttpOnly/Strict。

| Step / 步骤 | Miniapp / 小程序 | Web / 浏览器 |
| --- | --- | --- |
| Bind / 绑定 | Explicit MINIAPP/ALIPAY_NATIVE_NONCE_1; server CSPRNG proof, digest only / 显式服务端一次性证明，原S256仍兼容 | WebCrypto S256, temporary per-tab sessionStorage proof and HttpOnly binding Cookie / 短时标签证明与绑定Cookie |
| Authorize / 授权 | my.getAuthCode returns a fresh auth code / 取得新授权码 | Authorized callback checks state; scrub temporary code from address / 回调核state并清临时地址 |
| Exchange / 兑换 | POST /auth/sessions with challenge_id/auth_code/code_verifier / POST兑换 | Same plus web_state, exact Origin and binding Cookie / 另带state、Origin和Cookie |
| Use / 使用 | In-memory once-returned access_token, Bearer / 仅内存持有会话令牌 | HttpOnly Cookie; csrf_token only in memory; Origin and X-CSRF-Token on writes / 只读Cookie及内存CSRF |
| Read / 查询 | GET /auth/session never redisplays token / 不重发令牌 | GET /auth/session restores current CSRF / 可读取本会话CSRF |
| Exit / 退出 | DELETE /auth/session and clear memory / 注销清内存 | DELETE with CSRF then expired Cookie / 带CSRF注销并清Cookie |

R006 implements the full-page Web/H5 callback using a five-minute, single-use verifier temporarily in this tab's sessionStorage; it is consumed and deleted before exchange. Normal startup without callback parameters clears abandoned proof. Session tokens never enter browser storage. Native MINIAPP uses the explicit server-nonce profile because Alipay random-byte support is not assumed. These are platform bindings, not Alipay PKCE parameters; native device/channel validation remains pending.

R006已实现Web/H5整页回调，5分钟一次性证明仅暂存当前标签sessionStorage，返回先删地址参数与本地证明，再核state并POST兑换；无回调普通启动清遗留证明。会话令牌不入浏览器存储。原生MINIAPP显式服务端nonce，不假设支付宝WebCrypto；原生真机及真实渠道仍待核验。详见双端实施细则，这不是内容加密。

All paths above have /v1/p11 prefix. The original identity/trade subset covers 13 operations within the current 23 of 93; seller/consent/listing operations below add ten. The subset includes: auth challenge/session create/read/revoke; profile; key list/create/revoke; services; quote create/read; order create/read. A valid account session is required for purchase; API keys never authorize it. Unknown authorization outcomes require fresh authorization. Signed synthetic OAuth and native HTTP tests passed; real application/channel integration has not.

上述路径前缀均为/v1/p11。当前23/93操作中的原13项身份交易子集涵盖挑战/会话、本人、密钥、市场、报价和订单；买单必须本人会话，API key不能代购。授权UNKNOWN重新发起授权，不重复兑原码。签名合成渠道响应及本机HTTP已验证，真实应用/渠道联调尚未执行。详见[实际源码路由](../src/api/internal/http/routes.json)和[实施约束](../../docs/技术开发/附录-身份登录与HTTP交易实施约束.md)。

## Intended buyer journey / 规划买方流程

| Step / 步骤 | Buyer decision and system behavior / 买方决策及系统动作 |
| --- | --- |
| Discover / 发现 | Select an actual seller and approved capability / 选择实际卖家及获准能力 |
| Confirm / 确认 | Read model/workflow, processors, mode/privacy, total fees, budget, limits and deadline / 查看模型、处理方、模式隐私、总费、预算限额和期限 |
| Contract / 成约 | Accept an immutable quote/order; no silent seller replacement / 确认不可变报价订单，不静默换卖家 |
| Pay / 支付 | Complete authorized channel interaction; verify proof under the original intent / 完成授权交互，按原意图验付 |
| Pair / 配对 | E2E requires buyer-local or explicitly paired trusted-LAN gateway and verified recipient / 加密需本机或已配对可信LAN网关及接收端核验 |
| Invoke / 调用 | Configure the contract route and standard SDK base URL; unsupported profiles reject / 配置合同路由和标准SDK入口，不支持的档位拒绝 |
| Accept / 接受 | Authenticate result, confirm acceptance or submit bounded rejection evidence / 认证结果并接受或提交有界拒绝证据 |
| Resolve / 售后 | Follow order/refund/dispute state without disclosing unnecessary encrypted content / 按订单退款争议状态处理，不额外披露加密正文 |

R004 exposes a limited native control API, including market/quote/order operations.
The full buyer journey, model invocation and payments are still pending.

R004已接入市场/报价/建单的有限原生HTTP子集；此表完整使用流程、模型调用与资金链仍待完成。只有已验证凭据适配器可构造Principal，普通请求不能自报角色。

R003 creates bounded quotes and WAITING_PAYMENT orders only. It does not reserve GPU capacity or channel funds. Hourly rental sessions/renewals, budget transactions and trusted usage remain pending. See [the exact subset and historical price boundaries](../../docs/技术开发/附录-NB价版与交易服务实施约束.md).

R003仅产生有界报价与WAITING_PAYMENT订单，不占GPU或冻结渠道资金。小时会话/续租、预算事务与可信usage仍待完成；[实施子集与旧价格边界](../../docs/技术开发/附录-NB价版与交易服务实施约束.md)逐项说明。

## Intended provider journey / 规划卖方流程

Pair the enrolled device, confirm software/model/node installation, set available
time and resource limits, pass capability/permission checks, set a listing price
and opt in to work. The provider approves what may run; the scheduler selects a
permitted task within the accepted contract. Switching kernels stops new sales,
drains current owners, validates the replacement environment and switches only
under the shared resource lock.

设备配对后确认软件/模型/节点安装，设置时段和资源上限，通过能力/许可检查，自主定价并开接单。卖方批准可运行内容，调度器只在合同内选择。换内核先停新售、排空当前任务、验证新环境，再在共享资源锁下切换。

Providers see approved versions, resource/stage and earnings information, and can
stop new work. Privacy tasks omit prompt/output browsing. Android participation
requires real-device lifecycle, charging, network, temperature and consent checks;
an emulator or a claimed SoC capability is not proof of eligible supply.

卖方可见批准版本、资源、阶段和收益，能停新单；隐私任务不提供正文浏览。Android 必须真机验证生命周期、充电、网络、温度和同意；模拟器或芯片型号不能证明可售供给。

## Evidence / 验证口径

[R001 evidence](../tests/evidence/R001-验证报告.md) covers 10 native foundation cases.
[Implementation progress](../progress/README.md) covers every pending task. No
real OAuth/payment, physical GPU/Android, E2E interoperability or whole-platform
acceptance is implied by a successful document build or CLI cross-compilation.

[R001证据](../tests/evidence/R001-验证报告.md)覆盖10项原生基础场景；[实施进度](../progress/README.md)列全部缺口。文档构建或 CLI 交叉编译不代表真实登录/支付、GPU/手机、加密互通或完整平台验收通过。

[R003 evidence](../tests/evidence/R003-验证报告.md) covers 22 native cases, including pricing, qualification, quote/order concurrency and revoked-role replay. Temporary DB approvals are synthetic fixtures and cannot qualify real supply. Explicitly select the intended --round; preserve these historical results.

[R003证据](../tests/evidence/R003-验证报告.md)覆盖22项原生场景，含计价、资格、报价/订单并发和撤销角色回放。临时库审批是合成夹具，不解锁真实供给。显式指定目标--round，保留这些历史证据。



## R005 seller and future-listing control / 卖家与未来挂牌

R005 introduced the ten seller/listing operations below; R006 retains 23/93 operations and 64 tables, with 45 Go cases plus 13 shared TS cases. Real seller/channel/device review and seller write UI remain pending. All writes below require an account session, current authority and a stable Idempotency-Key; WEB additionally sends the exact Origin and session CSRF token. API keys cannot perform these account mutations.

R005新增以下十项卖家挂牌操作；R006仍23/93操作、64表，45项Go和13项TS用例。真实卖家/渠道/设备审核和卖家写流程页面仍待完成。以下写操作要求当前账户会话及稳定Idempotency-Key；Web另带精确Origin和会话CSRF。API密钥不能代理这些账户操作。

| Step / 步骤 | Request / 请求 | Result / 结果 |
| --- | --- | --- |
| Apply / 申请 | POST /v1/p11/seller/applications with SellerApplicationWrite | SUBMITTED, no SELLER grant / 待审，不授卖家角色 |
| Inspect / 本人查看 | GET /v1/p11/seller/profile | Own pending profile, ETag / 本人待审资料 |
| Consent / 同意或撤回 | POST /v1/p11/me/consents; explicit accepted boolean | Append-only receipt / 追加回执；不自动授权内容披露 |
| Create / 建草稿 | POST /v1/p11/seller/listings with ListingWrite | DRAFT; derived owned models/environment / 服务端绑定本人型号/环境 |
| Inspect / 读取 | GET /v1/p11/seller/listings/{listing_id} | Current definition and strong ETag / 当前定义和强ETag |
| Future edit / 修改未来版 | PATCH same path + If-Match: "current_version" | New revision, state DRAFT / 新修订、回草稿 |
| Submit / 提交 | POST .../{listing_id}/submit with ReasonWrite | SUBMITTED after evidence checks; no publication / 重核证据后待审，不自动开售 |
| Pause / 暂停 | POST .../{listing_id}/pause with ReasonWrite | PAUSED, existing sold contracts retained / 保留已售合同 |
| Exit / 退出 | POST /v1/p11/seller/exit with ReasonWrite | EXITING, new admission off; cleanup still pending / 停新准入，仍需善后 |

Application references are unverified claims. Review must independently establish legal scope, current payee qualification and device rights before SELLER/APPROVED supply becomes usable. This round does not provide a runtime self-approval or fixture-seeding endpoint. A new draft/submission requires independently approved seller regions, owned concrete device models and current capability/environment evidence. No nickname, capacity claim or application reference substitutes for those records.

申请引用仅为待核材料。本轮没有自批或测试种子入口。新增草稿/提交要求独立批准的地区、本人具体型号与能力环境；昵称、容量声明和材料ID都不能替代资格。已批准档案的完整收款/证据投影仍待实现，当前本人Profile的payee_eligibility_state为UNVERIFIED。

Configure a complete business fee policy to enable new listing writes. These are public policy values, not credentials; the following are examples and are not automatic defaults or evidence of channel permission:

新挂牌需完整显式配置费用政策。以下只是示例，不是默认佣金或渠道许可：

```powershell
$env:P11_FEE_POLICY_VERSION = 'seller_fee_v1'
$env:P11_COMMISSION_BPS = '500'
$env:P11_CHANNEL_COST_OWNER = 'SELLER'
```

BPS must be 0–10000, owner SELLER/PLATFORM, and submitted Price values must match this configuration exactly. Missing policy closes new listing writes; incomplete/invalid configuration rejects startup. Clients must obtain the current business policy through the forthcoming configuration/UI flow; this round has no public policy-configuration endpoint. Seller-selected hourly/input/output rates remain separate from platform commission. Existing orders retain their original fee policy.

BPS为0—10000，费用承担方SELLER/PLATFORM；Price须精确匹配。未配置则关闭新增挂牌写入，部分或非法配置拒绝启动。前端读取政策的配置流程尚待实现，当前没有公共费率配置接口；不能自行把示例当真实政策。旧订单仍保原费率。

R005 introduced migration 004; the current R009 API requires explicit migrations 001–005 before startup. Never rewrite 001–004. Migration 004 leaves existing approved_regions empty and model_name nullable, and does not invent historical listing definitions. Approved-region/model backfill and historical definition migration need independent review. The current owner list includes authored definitions only; directly reading an older listing without its definition returns an explicit migration-required failure. New-schema migration tests are not historical-upgrade tests.

R005引入004；当前R009 API启动前须显式迁移至005，不改001—004。旧地区默认空、型号可空，旧定义不补造；回填及历史升级仍须独立审核。本人列表当前只含有新定义的挂牌，直接GET无定义旧挂牌返回需迁移错误。新库迁移通过不等于存量升级已验证。

ETags represent mutable resource versions, not listing revisions. PATCH requires one quoted numeric strong ETag; weak/list/wildcard/duplicate forms and null fields are rejected. PATCH replay returns the original receipt and ETag; GET again for current state. State changes use ReasonWrite.expected_version. All accepted edits preserve prior immutable versions and order snapshots. Display units remain 小时 or M token; availability gaps and insufficient remaining GPU time cannot be sold.

ETag代表资源version，与revision不同。PATCH不接收弱/列表/星号/重复ETag或null字段；回放是原回执，当前状态须再GET。状态操作带expected_version。历史修订和订单快照不改；小时/M token展示、空档及剩余时长门槛继续适用。

See [full seller/listing constraints](../../docs/技术开发/附录-卖家申请与挂牌版本实施约束.md) and [R005 evidence](../tests/evidence/R005-验证报告.md). The tests use explicitly synthetic approvals; that historical evidence does not establish actual seller review, payment or provider registration. R006 adds real Web/H5 interaction in its separate frontend evidence; native Alipay remains unverified.


## R008 native provider control / 原生提供方控制程序

Build both native targets with `python -X utf8 dev/tools/核验原生源码.py --round R008`; this creates runtime/bin/p11-node.exe and p11-node-linux-amd64. Linux is cross-compiled only. The foreground control agent starts no engine, accepts no customer prompts, opens no listener and reports UNKNOWN until a future qualified probe exists. R009 adds provider pairing/policy UI; full listing editing remains pending.

构建后以本人普通账户明确运行p11-node；它不自动安装/下载内核，不接收Prompt，不开启共享或自启。Windows系统保护有本机测试，Linux真实钥库仍待联调。与完整RuntimeSupervisor/任务inbox/资源门闩的区别见[原生代理细则](../../docs/技术开发/附录-原生提供方控制代理与本地恢复.md)。

PowerShell example, from the clone root / 以下从各自克隆根执行，先确认选定API地址及真实配对资格：

```powershell
New-Item -ItemType Directory -Force runtime/node | Out-Null
$taskState = Join-Path (Get-Location) 'runtime/node/provider-1'
@'
{"control_base":"http://127.0.0.1:8787","model_name":"RTX 4090 24GB","resource_kind":"GPU_DEVICE","os_build":"OWNER_DECLARED_WINDOWS_BUILD","client_release_id":"p11_node_r008"}
'@ | & ./runtime/bin/p11-node.exe init --state $taskState
Get-Content -Raw -LiteralPath runtime/secrets/pair-token.txt | & ./runtime/bin/p11-node.exe enroll --state $taskState
& ./runtime/bin/p11-node.exe status --state $taskState
& ./runtime/bin/p11-node.exe recover --state $taskState
& ./runtime/bin/p11-node.exe status --state $taskState --refresh
& ./runtime/bin/p11-node.exe heartbeat --state $taskState
& ./runtime/bin/p11-node.exe run --state $taskState
```

The pair-token file must be owner-controlled, ignored by Git, and contain the actual once-only credential from the approved seller's R007 pairing request. Never paste it in arguments/environment/screenshots. The example does not create or approve a seller, generate a token or start the API. `recover` retries the exact encrypted pending command; do not repeat `init` or create a new enrollment after an unknown response. `run` remains foreground; Ctrl+C cancels its current request/timer. It is not a stop/cleanup acknowledgement for an engine.

配对文件仅由本人准备于受控runtime/secrets，不提交Git、不放参数或环境变量，不出现在截图。示例不会造卖家资格、配对令牌或启动API。回执未知时保留原目录并执行recover；不要重新初始化、更换私钥或登记键。run只在前台运行，Ctrl+C结束控制请求/定时器；不代表GPU任务停止或锁释放。若状态写入失败、钥库锁定或设备撤权，程序关闭处理并保留原状态。

Linux equivalent, requiring an already running/unlocked user Secret Service with a local unix DBUS_SESSION_BUS_ADDRESS / Linux须先解锁已运行的本人桌面钥库，不会自动启动或退回文件明文：

```sh
mkdir -p runtime/node
task_state="$(pwd)/runtime/node/provider-1"
printf '%s' '{"control_base":"http://127.0.0.1:8787","model_name":"RTX 4090 24GB","resource_kind":"GPU_DEVICE","os_build":"OWNER_DECLARED_LINUX_BUILD","client_release_id":"p11_node_r008"}' | ./runtime/bin/p11-node-linux-amd64 init --state "$task_state"
./runtime/bin/p11-node-linux-amd64 enroll --state "$task_state" < runtime/secrets/pair-token.txt
./runtime/bin/p11-node-linux-amd64 recover --state "$task_state"
./runtime/bin/p11-node-linux-amd64 run --state "$task_state"
```

The state directory must be new for init, absolute/canonical, privately owned and free of symlink/junction/reparse components. Linux requires 0700 directories/0600 files; Windows creates owner/SYSTEM DACLs. Same-directory concurrent writers fail; OS releases the lock after process termination. Missing/corrupt state or key never silently generates a replacement identity. Failed initialization may leave an unpaired directory/keyring entry; explicitly inspect before owner-directed cleanup. The encrypted one-command journal is not the pending SQLite execution inbox.

初始化仅接受新目录；已有身份不能覆盖。目录须绝对规范、本人所属且无链接/重解析组件。状态主钥与加密日志损坏均停止，不回退明文。状态输出只含型号、设备/版本/序号与固定结果，execution_enabled为false；Windows DPAPI和Linux同用户钥库不等于客户内容隔离或严格增强档。真正的内容加密、低权限执行服务、物理机/真机及全产品验收仍待完成。

See [R008 native evidence](../tests/evidence/R008-验证报告.md), [R008 frontend regression](../tests/evidence/R008-frontend-checks.json) and [all 56 tasks/222 remaining steps](../progress/R008-本轮实现与完整待办.md). Current scope remains 31/93 operations and 65 tables, with migrations 001–005 unchanged.

## R007 provider identity/control / 提供方设备身份控制

Historical R007 source: 31/93 operations, 65 tables and 60 actual Go cases. R006 UI remains read-only for providers: pairing/policy buttons are not enabled merely because backend operations now exist. The shared TS/build evidence remains from unchanged frontend source; R007 replayed both actual browser journeys against that HTTP/backend and stored independent media under dev/media/R007. See R008 above for current native agent delivery.

R007当时31/93操作、65表、60项实际Go用例。提供方页面仍只读，不能因后台接口完成就启用尚未接通的按钮。共享TS与三目标编译记录对应未改前端源码；R007针对当时后台重跑双端浏览器，媒体独立保存在dev/media/R007，保留R006原证据。当前原生代理进展见上方R008。

| Step / 步骤 | Input / 输入 | Result / 结果 |
| --- | --- | --- |
| Owner pairing / 主人配对 | POST /devices/pairing-intents; platform WINDOWS/LINUX/ANDROID, device_label, owner_confirmed=true; stable Idempotency-Key | 201 pairing_id/expires_at/seller_id; one_time_token only first reply / 只首次返回秘密 |
| Native registration / 本机登记 | POST /device-enrollments; Bearer pairing token, original Idempotency-Key; EnrollmentWrite | 201 same PAIRED device on original recovery / 原意图恢复同一个设备 |
| Device read / 设备读取 | GET /devices/{device_id}; P11_DEVICE_ED25519_1 proof | Own state/policy/version, strong ETag / 本设备状态、策略和版本 |
| Owner device list / 主人列表 | GET /seller/devices?limit=50; account session | Own devices, signed owner-scoped next_cursor / 本人分页 |
| Heartbeat / 心跳 | POST /devices/{device_id}/heartbeats; device proof and original key | Monotonic sequence, server-time Ack; no qualification / 不授可售资格 |
| Resource policy / 共享策略 | PATCH /devices/{device_id}/resource-policy; owner session, If-Match, expected_version, explicit conditions | Version+1; PROBING/DRAINING with admission off / 关闭接单，等待资格复核 |
| Stop/revoke / 停止撤销 | POST /devices/{device_id}/drain or /revoke; owner session, original key, reason_code/expected_version | Close new admission; retain actual lock owner/generation / 不假装已停算清理 |

Paths above use /v1/p11. WEB owner writes require exact Origin, session Cookie and X-CSRF-Token; MINIAPP owner writes use the native session Bearer. Device/enrollment transports reject Cookie/Origin. Never send a pairing or device proof to the inference/content relay.

上表统一前缀/v1/p11。Web主人写操作带Origin/Cookie/CSRF；原生小程序带会话Bearer。设备/登记拒绝浏览器Cookie/Origin。不要把配对秘密或控制证明送入模型/内容转发。

Enable pairing only by setting P11_DEVICE_PAIRING_PEPPER_FILE to an explicitly prepared, private runtime file containing at least 32 random bytes; no sample secret or generator auto-enables it. The optional setting has no fallback to an auth/API-key secret. An invalid configured file rejects startup; missing configuration closes pairing and enrollment. The current command uses pair_v1; lifecycle/rotation tooling is still pending.

配对须显式设置P11_DEVICE_PAIRING_PEPPER_FILE，指向受保护runtime文件（至少32随机字节）。不提供默认秘密或自动启用。未配置关闭配对/登记，配置非法则拒绝启动；不会借用登录/API密钥。当前命令档位pair_v1，轮换工具仍待完成。

Enrollment fields are pairing_id, device_public_key (canonical raw-base64url Ed25519 32-byte public key), key_profile_id=ED25519_DEVICE_1, os_build, client_release_id, device_fingerprint_hash (SHA256), resource_kind and concrete model_name. Android must say its actual model, such as 摩托罗拉 X30 or 努比亚 Z20; these strings do not prove the hardware or model is qualified. No private key is submitted. Registration creates no resource/capability/release and starts with sharing off.

登记字段完整列于上段及权威EnrollmentWrite；型号不能写通用设备名称。公钥不含私钥；安装指纹与型号自报不等于实机资格。登记不创建可售资源/能力/发布，共享默认关闭。

The native Go library at dev/src/client/protocol/devicecontrol implements bounded signing and HTTP only. Load an installation key through a future native credential-store adapter, construct NewClient, freeze a command with Prepare, then Send. On transport UNKNOWN keep the same prepared input/key and explicitly Send again with a fresh proof. Never repeat the old Authorization header or silently replace the enrollment key. Close clears the library copy and idle connections. Durable local journals and Windows/Linux key storage are not yet delivered; do not store a private key in plaintext to make this example look complete.

原生Go库只含有界签名/HTTP。系统凭据存储适配尚待实现，调用方当前仅可在受控测试内存提供钥。Prepare冻结命令；UNKNOWN后保持原字段/键显式Send，库生成新证明；不重复旧Authorization，也不暗换登记意图。Close清理本库副本/连接。完整本地恢复日志、Windows/Linux密钥存储和daemon尚未交付。

Heartbeat sequence is a canonical decimal string 0–18446744073709551615, strictly increasing. A recovered original heartbeat does not refresh last_seen_at. Device policy changes require fetching the new version; neither IDLE nor a fresh timestamp clears a busy/quarantined lock, authorizes execution, acknowledges cleanup or supplies trusted billable usage. Android policies explicitly include battery, temperature and charging limits; actual enforcement awaits LMA integration.

心跳序号为规范十进制字符串，原命令回放不续活；策略变更先读新版本。IDLE/新时间不清锁、不授权执行、不确认清理、不产可信计费。Android须明确电量/温度/充电条件，真实执行仍待LMA接入。

See [complete control constraints](../../docs/技术开发/附录-设备配对身份与控制协议实施约束.md), [R007 evidence](../tests/evidence/R007-验证报告.md) and [all remaining work](../progress/R007-本轮实现与完整待办.md). No real GPU, phone, channel or funds are enabled by these checks.

见以上完整协议、实际局部证据与全部待办；不因此开启真实GPU、手机、渠道或资金操作。


## R015 confirmed-route control / 已确认路由控制

R015新增已确认订单路由与标准SDK key绑定：明确原订单/同单支付引用，固定个人卖家、商品版本、模型、文本协议、价格、预算、次数上限和有效期。三个本人控制接口使已实现操作增至34/93；ResolveTx仅在key绑定范围内精确解析，同名歧义/失效/协议误配拒绝，不跨卖家兜底。007只追加5列，旧路由隔离；API启动改为核对所有内嵌迁移登记。当前只实现PLAINTEXT/STANDARD及三种native文本profile，路由不是执行或资金授权，调用签发/次数记账、真实付款、E2E和UI路由配置仍待完成。见[RC-001—022](../../docs/技术开发/附录-已确认订单路由与标准SDK绑定实施细则.md)、[R015实际证据](../../dev/tests/evidence/R015-验证报告.md)及[全部56任务/222未收口子项](../../dev/progress/R015-本轮实现与完整待办.md)。

## Historical R016 original-call preparation / 历史原调用准备库

R016新增原调用准备库：当前scoped key/原订单/同单资金事实重新核对后，将INPUT_PENDING、整笔单次预算、次数占用及安全事件在同一事务提交。原键恢复不重复占用；改参数/key冲突；取消/过期/未知保留原记录和占用。请求摘要只以版本化HMAC持久保存。前向008追加六列，001—007和端侧DDL原字节不改；仍34/93操作、65表。仅三个原生文本档位的PLAINTEXT/STANDARD；没有挂载模型入口、签发Lease/Grant或运行模型。输入就绪/资源原子准入、可信usage、真实支付、E2E和路由UI仍待。详见[RI-001—024](../../docs/技术开发/附录-原调用准备与次数预算原子接线.md)。

R016 adds original-call preparation: current scoped-key, original order and same-order funding facts are rechecked, then INPUT_PENDING, the full per-call budget hold, one call slot and a safe event commit atomically. Original-key recovery does not reserve twice; changed parameters or keys conflict. Cancellation, expiry and unknown results retain the original intent and hold. Only a versioned HMAC of the transient request digest is persisted. Forward migration 008 adds six columns; 001–007 and native SQLite DDL retain their exact bytes. The scope remains 34/93 operations and 65 tables, with three native text profiles under PLAINTEXT/STANDARD. There is no model listener, Lease/Grant issuer or inference. Input-ready/resource admission, trusted usage, real payments, E2E and route UI remain pending.

Library sequence: create invocation.Service with an explicit database, payment environment, fingerprint version and key copies; call Prepare with the currently authenticated API-key principal, original request key and trusted native preflight fields; retain the original receipt or recover the exact request. Close clears the service-owned key copies. PrepareTx is SQL composition only, with caller-owned commit. Do not use its result as execution permission. No public endpoint or production fingerprint-secret loader is supplied yet.

专项检查：`python -B -X utf8 dev/tools/原生数据库验证.py --round R016 --suite invocation`；完整回归移除`--suite invocation`。两者均使用本项目临时原生数据库并在结束关闭。

## R022 源码使用顺序 / Source integration

本轮把原生结果保管与显卡执行清理分开：一次原提交的输出先写入独立 AES-256-GCM 密文仓，默认 16 × 8 MiB、保留 5 分钟；原计算同步停止并清理后可释放显卡，旧结果仍按原订单/本地围栏读取。密钥只在进程中，单读者、定额预留、到期撤权；丢密钥或未知提交不重跑。管道升级 PIPE_2，补实际输入计数与原停止词序号。

原结果可输出 OpenAI Chat、Responses、Anthropic Messages 的 JSON/SSE，保留 UTF-8、流式用量选项、停止原因和有界写入。默认 JSON 512 KiB、单事件 64 KiB；Responses 最终重复全文需要显式能力预算，超限拒绝成功尾，生产准入尚待接通。写出 Receipt 不是买家 ACK、可信 usage 或付款凭证。

仅 PLAINTEXT/STANDARD 源码子集；本地密文不等于 E2E/LP。CLI 执行仍关闭。17 项新增常规 Go 用例，累计 275 项；另 6 项带标签的实际 Windows worker＋合成 C ABI 用例，包含密文保管到三协议的串联。真实模型/GPU/隔离身份、独立官方 SDK、外网结果通道、资金与完整验收仍待。早期监督器关闭超时和首轮 ABI 测试失败均留档；后续通过不能证明历史时序风险消除。

[RC-001—060](../../docs/技术开发/附录-原生文本结果加密保管与标准协议交付.md) · [R022 证据](../tests/evidence/R022-验证报告.md) · [完整56任务](../progress/R022-本轮实现与完整待办.md)。

`results.Open(new empty runtime directory) → results.New(approved native adapter, store) → trusted HostKernel → ticket.Start once → ticket.Output → textoutput.Render → close reader → Discard/retention → close owned store and native adapter`。这是可信源码嵌入顺序，不是已开放的 CLI/HTTP 推理入口；不得复制入站清单自批环境或权限。

## R023 / Pinned native package and explicit CUDA verification

R023实现固定真实LLM包的本机校验、独立环境摘要、只读检查与提供方安全元数据；PIPE_3分开主库/注册库/基础库，并提供不含正文的固定启动阶段错误。固定官方b11960/CUDA12.4与Qwen0.5B实际验证通过三协议JSON/SSE六组合、模型复用、token上限、取消、硬截止和缺失显卡不降档；只覆盖本机小模型的PLAINTEXT/STANDARD路径，不授予物理资源映射、全层卸载/性能、LP/E2E、SDK、可信usage、ACK或资金资格。CLI内容执行继续关闭，全56任务222子项和80/96完整验收保持开放。

See [NF constraints](../../docs/技术开发/附录-真实原生LLM包校验与本机接入.md), [real result](../tests/evidence/R023-native-real-summary.json), [CLI checks](../tests/evidence/R023-native-kernel-cli.json) and [complete task ledger](../progress/R023-本轮实现与完整待办.md). Package inspection starts no worker, returns execution_enabled=false and qualification=NOT_GRANTED. Inputs remain ignored downloads; old failures retain their exact archives.

## R024 / Physical GPU binding

R024增加Windows NVIDIA整卡UUID/PCI探测及PIPE_4启动复核；显式GPU选择和环境摘要绑定实际身份，同UUID别名不能绕过共享门闩。只读gpu-discover不加载模型、不准入共享，LocalInfo仅含安全元数据。本机观测不构成远程证明或LP/E2E/性能/usage/ACK/资金资格；Linux/AMD/手机实际接入与完整LM Studio隔离封装仍待。全56任务222子项、80/96完整验收保持开放。

See [GI constraints](../../docs/技术开发/附录-物理显卡身份探测与启动绑定.md), [current evidence](../tests/evidence/R024-验证报告.md), and [all task IDs](../progress/R024-本轮实现与完整待办.md). GPU discovery accepts a pinned local package and returns metadata only. Owner approval and the physical Host binding remain independent; no first-device auto-selection. The complete LM Studio CLI/daemon/runtime compatibility path is still required.

## R025 / Original Host and transient input custody

R025接通独立批准的原生Host会话、原物理GPU与持久任务、加密结果仓和三协议交付；密文资产摘要保持原义，解密正文另以仅驻内存的原租约/清单/字节凭据校验。宿主拥有worker和结果读取句柄并在关闭时排空、撤权及等待退出，未知原owner不释放。当前限PLAINTEXT/STANDARD；生产资金/usage/ACK/SDK、崩溃恢复、完整LM Studio/ComfyUI/LMA、E2E/LP/性能与80/96完整验收仍待，全56任务222子项保持开放。

Trusted embedding sequence: restore the original Host and independently provisioned approval; prepare the original command; call Host.OpenNative with local pins and exact UUID/PCI; run NativeSession and wait for Ready; start only the same Host ticket; reconcile original cleanup; read the original result; close the session/Host. The session owns outstanding result readers, revokes them and closes their handles before releasing its store. No CLI or public content listener is added. Manifest ciphertext digest and private in-memory plaintext check are different facts. Historical metadata cannot authorize new work, restart cannot replay a prompt, and unknown warm-up/work retains the original gate. Resident model memory is retained until explicit session close; switching engines/unloading and production scheduling remain separate work.

See [NH constraints](../../docs/技术开发/附录-原生宿主内核接线与输入驻留绑定.md), [current evidence](../tests/evidence/R025-验证报告.md), and [all task IDs](../progress/R025-本轮实现与完整待办.md). Actual CUDA Host tests still use a synthetic platform issuer/order/payment fixture; no real funds or production qualification is implied.


## 2026-11-06：全量代码阶段 / Full-code phase

当前进入CODE_FIRST。先完成全部待办生产代码，再统一编译、测试和调试；[全量清单](../全量代码优先实施清单.md)与[R026进展](../progress/R026-全量代码阶段进展.md)分别记录代码和验收。最新固定LMA v1.0.0多模态内核及[Android派生源码](../src/client/android/README.md)纳入本项目；[技术细则](../../docs/技术开发/附录-LMA多模态派生内核与全量代码阶段.md)明确私有任务、共享槽、Keystore与分块密文、尚未接通的控制/加密/恢复边界。现阶段没有新增源码的编译/手机/模型验收记录，R025证据保留原身份。

Complete all production code before batch compilation, tests and debugging. LMA v1.0.0 image/video source is pinned and the private Android overlay is being implemented; it is not an APK, phone-performance or commercial-eligibility claim. Historical R025 evidence does not validate the new source.


## Android提供方源码使用逻辑 / Provider source operation

当前仅为未验证源码：本人创建公钥→核对并批准本机配置→选择已安装模型/后端和共享条件→开启本机准备→私有控制适配器交付原签名任务→原生引擎执行/密文结果→原回执与明确清理/恢复。UI只展示真实机型和安全状态；平台M1签发/领取/续租、放钥、上传和市场资格仍在全量实现范围内。完整字段、构造过程与失败边界见[Android双语说明](../src/client/android/README.md)。所有代码齐全后才集中构建/测试；不能把历史APK/媒体当成这份源码的新验证。

The source defines explicit owner identity/approval, installed-model selection, sharing preparation, private original-lease execution and recovery. It does not yet provide a complete platform dispatcher or a qualified APK. Read the module README for exact bindings and open integrations. Build and run the included physical-device boundary tests only after every in-scope code item is complete.


## 原任务续签与清理恢复源码 / Execution continuation and cleanup recovery source

平台可显式设置 `P11_LEASE_CONTINUATION_SIGNER_FILE` 指向受限签发配置，profile为 `P11_LEASE_CONTINUATION_SIGNER_1`，含 `key_id` 和canonical base64url无填充的64字节 `private_key`；公钥须匹配原独立verifier，不自动生成或借用内容钥。未设置时只有STOP/NO_CHANGE。源码尚未全量完成，当前不据此启动生产执行。

Existing progress requests can receive a signed CONTINUE for the same original execution; the dedicated signing service rechecks original admission and retains both signed revisions. Android verifies and durably accepts that continuation while its current window remains valid. FAILED cleanup is retained and a separately signed recovery names its original receipt; successful recovery clears only that occupant while its lease remains revoked. No extra operation ID or payment/usage authority is introduced.

当前平台授权仍仅T1文本清单；M1媒体准入、桌面报告/恢复、密文素材/钥与上传的完整接线仍在全量清单。012/013及八项Android边界定义、新增Go用例均未运行；这些源码不代表手机媒体已闭环，也不等于GPU租赁付费续租。

## R026 控制连接源码（尚不能执行租赁任务）

原设备先明确初始化`Store.InitExecution`与`InitRentalJournal`、登记独立核对的物理资源，与Host/控制循环共用原节点OS锁；已初始化后用Open方法，缺失/损坏不重建。本人可信配置/UI独立核准实际安装环境/模型/recipe、原公共manifest/物理摘要/connection/issuer后构造`LocalApproval`，经`VerifyPreparation`与`PrepareRentalSession`先加密保存再占原槽。网络任务不能自动构造批准。`ProviderRental.AttachControl`独立读取平台原grant摘要、核公钥pin/JWS并绑定；`RefreshControl`保存请求序号、签名HTTP控制并提交同一物理门闩。`CheckScope`同时核控制window与原owner，`BeginInvocation`给持久一次提交ticket。真实内核适配、认证输入/素材和独立停止watchdog仍待，不开放执行。完整顺序见[原生接线说明](../src/client/execution/README.md)及RN-071—090。没有新增可执行CLI入口或自动模型下载。

原Bearer只作内存校验，原批准、请求序号及停止标记持久加密；网络重试不改原键，重启不复用ALLOW。租内子调用只清自身，父租赁继续占原槽。Stop/关闭保留原owner/预算；本机原事实清理门槛已有源码，真实适配器证明与平台回执/owner/预算释放仍待。新增端侧schema-002前向迁移源码，旧DDL不改；目前不运行迁移或编译。9项租赁HTTP/53项测试定义仅是源码，不能据此开启交易或执行。完整56/222完成代码后才集中编译/测试。

The source-level join is explicit owner approval → encrypted durable preparation → original shared physical row → independently read grant metadata and pinned JWS → signed control committed to that row → serial once-only invocation. Stop/expiry retains ownership. Trusted actual engine cleanup observations, platform acknowledgement and release are not implemented by a JSON boolean. No CLI enables rental execution; no tests or database migration have run for this code.

## R026 ComfyUI source checkpoint / 源码检查点

R026新增[CE-001—036](../../docs/技术开发/附录-ComfyUI完整环境安装与原节点登记源码.md)：独立签名/本人许可节点同意、固定完整目录、有限节点依赖、有界ZIP安装/全树复核、原节点AEAD登记、原物理维护共槽切换和五项本机管理入口源码。官方ComfyUI固定v0.44.0/2c75dc1b379a59e0880619d029607af5169a62ef，尚未制作完整实际运行包；31项新测试定义全部未编译/运行。没有ComfyUI媒体执行/首写加密/租内接线或资格/性能结论。RN-110/67租赁测试、全部56任务222子项保持开放，代码齐备后统一验证；无SQL变更/迁移。

R026 adds [CE-001–036](../../docs/技术开发/附录-ComfyUI完整环境安装与原节点登记源码.md): independently pinned/signed complete catalogs and local license/node consent, bounded immutable ZIP installation and whole-tree checks, the original node AEAD registry, shared physical maintenance selection and five metadata-only local CLI operations. Official source is pinned to ComfyUI v0.44.0/2c75dc1b379a59e0880619d029607af5169a62ef; no complete actual runtime archive ships yet. All 31 new test definitions remain uncompiled/unexecuted. Media worker/first-write encryption/rental integration, actual qualification and performance remain open. RN-110/67 rental tests and every 56/222 task/step remain open until full-scope code precedes batch validation. No DDL change/migration.

## R026 MW-001—044 私有媒体源码继续 / Private media source

R026新增[MW-001—044](../../docs/技术开发/附录-ComfyUI私有媒体执行与本地加密源码.md)：固定原生私有Comfy执行器、GPU UUID/PCI复核、有限配方、PNG/分片MP4/PCM WAV/几何GLB编码、原锚/加密预登记/分块GCM及Go一次原scope凭据/私有帧源码。44项新测试定义未编译/运行；正式原宿主启动器/租内媒体/完整运行包/公开交付/低权限/E2E与性能仍待。CE31/RN67及全部56任务222子项仍未收口，代码齐备后统一验证；本次无SQL变更。

R026 adds [MW-001–044](../../docs/技术开发/附录-ComfyUI私有媒体执行与本地加密源码.md): a fixed native private Comfy worker, exact CUDA UUID/PCI, finite recipes, PNG/fragmented MP4/PCM WAV/geometry GLB encoders, original workspace anchors, encrypted pre-create reservations, chunked GCM and a Go single-use original-scope receipt/private-frame boundary. All 44 new test definitions remain uncompiled/unexecuted. The actual host launcher, rental-media Backend, complete runtime archives, public delivery, OS low-privilege/E2E and performance remain open. CE31/RN67 and all 56 tasks/222 steps remain open until full-scope code precedes batch validation. No SQL change.

## R026 CH-001—036 原环境与宿主源码继续 / Original environment and host source

R026新增[CH-001—036](../../docs/技术开发/附录-ComfyUI原环境持有与本机进程接线源码.md)：原Comfy环境运行持有、固定原生进程/三管道、PIPE_2准备/一次原授权ACK/单调截止和原ProviderRental准备接线。29项新测试定义未编译/运行；CH-033—036正式租内媒体Backend、认证工作区/恢复清理、公开交付、完整包/低权限/E2E、独立ready/计量/付费续租/平台释放仍待。全56任务/222子项开放，无SQL变更、迁移或真实执行；完整代码后统一验证，源码检查点不是开发轮完成。

R026 adds [CH-001–036](../../docs/技术开发/附录-ComfyUI原环境持有与本机进程接线源码.md): original Comfy environment lifetime references, fixed native process/three pipes, PIPE_2 preparation/one-shot original grant ACK/monotonic deadlines and original ProviderRental preparation wiring. All 29 new test definitions are uncompiled/unexecuted. CH-033–036 rental-media Backend, authenticated workspace/recovery cleanup, public delivery, complete runtime archives/OS low privilege/E2E, independent readiness/metering/paid renewal/platform release remain open. All 56 tasks/222 steps remain open; no SQL change, migration or engine run. Full-scope source precedes batch validation; a source checkpoint is not a completed development round.

## R026 WS-001—030 原媒体工作区与结果托管源码 / Original media workspace and custody source

R026新增[WS-001—030](../../docs/技术开发/附录-租内媒体工作区与结果托管源码.md)：原租赁媒体工作区/native003元数据预留、原节点目录持有/退役删除/清理专用恢复、认证RAM媒体输入、专用子调用适配器和结果钥托管/读取撤销。23项新测试定义均未编译/运行；公开SDK、完整媒体会话监督/独立停工对账/ready/续租/可信水位/平台释放、完整实际包/低权限/E2E/性能仍待。全56任务/222子项开放，只登记native003源，未迁移/真实执行；完整代码后统一验证，源码检查点不是开发轮完成。

R026 adds [WS-001–030](../../docs/技术开发/附录-租内媒体工作区与结果托管源码.md): original rental media workspace reservations/native migration 003 source, node-owned directory lifetimes/retirement/cleanup-only recovery, authenticated RAM media input, typed child adapter and result-key custody/revocation. All 23 new test definitions are uncompiled/unexecuted. Public SDK/media sessions, independent stopped-work reconciliation/readiness/paid renewal/hour meters/platform release, complete runtime archives/OS low privilege/E2E/performance remain open. All 56 tasks/222 steps remain unaccepted. Native 003 is registered as source only; no migration or runtime was executed. Complete all source before batch validation; this source checkpoint is not a completed development round.

## R026 MS-001—036 原媒体会话监督与停止回收源码 / Original media-session supervision source

R026新增[MS-001—036](../../docs/技术开发/附录-原租赁媒体会话监督与停止回收源码.md)：原节点持有的本机媒体会话监督/独立单调截止、真实Comfy叶子、一次原授权/单份输入及实际join、先停工后回收、两类会话共关闭、原媒体结果保留/撤销和本机会话清理CAS源码。27项新增测试定义均未编译/运行；公开媒体SDK/最终ACK、双方ready/可信小时水位/付费续租/平台资金释放、独立物理停工恢复及完整实际运行包/低权限/E2E/性能仍待。全部56任务222子项开放；本次无SQL变更或迁移。完整生产代码/实际依赖/测试源码齐备后统一验证，源码检查点不是开发轮完成。

R026 adds [MS-001–036](../../docs/技术开发/附录-原租赁媒体会话监督与停止回收源码.md): node-owned original media-session supervision, independent monotonic withdrawal, the actual typed Comfy leaf, once-only original authorization/one pending input, actual joins, stop-before-cleanup, combined text/media node closure, result retention/revocation and local-session cleanup CAS source. All 27 new test definitions are uncompiled/unexecuted. Public media SDK/final ACK, independent readiness/hour meters/paid renewal/platform fund release, physical crash reconciliation, complete runtime archives/OS low privilege/E2E/performance remain open. All 56 tasks/222 steps remain unaccepted. No SQL changes or migrations. Complete all production source, dependency wiring and test source before batch validation. This source checkpoint is not a completed development round.

## R026 MG-001—036 原租赁媒体入口与加密缓存源码 / Original-rental media gateway source

R026新增[MG-001—036](../../docs/技术开发/附录-原租赁媒体兼容入口与加密缓存源码.md)：原租赁本机STANDARD媒体入口、Images/明确旧版Videos/WAV speech/Responses资产映射、严格请求/独立scope、AES-GCM暂存/原HMAC幂等/提交前缓存配额、完整认证结果后加密缓存/本人单Range和原节点真实join源码。37项新增测试定义均未编译/运行。公开SDK/网络监听/E2E媒体传输/最终ACK、双方ready/可信小时计量/付费续租/物理停工对账/平台资金释放、完整实际配方/低权限/性能仍待。全部56任务222子项开放；无SQL变更或迁移。完整生产代码/实际依赖/测试源码齐备后统一验证，本次不是开发轮完成。

R026 adds [MG-001–036](../../docs/技术开发/附录-原租赁媒体兼容入口与加密缓存源码.md): an original-rental local STANDARD media handler, approved Images/explicit legacy Videos/WAV speech/Responses assets, strict requests and independent scopes, AES-GCM staging, original-id HMAC replay, result capacity reserved before submission, complete authenticated results before encrypted caching/ranges, and actual node-owned HTTP/callback/cache joins. All 37 new test definitions remain uncompiled/unexecuted. Public SDK/listeners/E2E media transport/final ACK, independent readiness/hour meters/paid renewal/physical reconciliation/platform fund release, complete real recipes/low privilege/performance remain open. All 56 tasks/222 steps remain unaccepted. No SQL changes or migrations. Complete all production source, dependency wiring and test source before batch validation. This is a source checkpoint, not a completed development round.

## R026 MP-001—040 原支付边界与凭证恢复源码 / Original-payment boundaries and recovery source

R026新增[MP-001—040](../../docs/技术开发/附录-MachinePay原支付边界与凭证恢复源码.md)：实际个人seller原支付范围/双公钥pin、原生RSA2 precreate/verify及有界A402、严格原Payment-Proof/AES-GCM/原生密文工件、付款单调/迟到实付/资金资格/退款累计和原范围对账源码。49项新测试定义全部未编译/运行；合成签名夹具已按固定标准库的双结果接口修正惰性初始化。平台支付事务/同事务outbox/唯一消费/已核原签名回包归档/秘密与授权注册撤销、本人HTTP/SDK/PayLauncher、独占宿主及持久原凭据恢复worker、真实退款/到账/佣金/对账单与可信usage/小时水位/账务仍待。尚无真实个人收单资格或渠道验收，不开放收费。全部56任务222子项开放；无SQL变更或迁移。完整生产代码/实际依赖接线/测试源码后统一验证，本次不是开发轮完成。

R026 adds [MP-001–040](../../docs/技术开发/附录-MachinePay原支付边界与凭证恢复源码.md): original personal-seller payment bindings and two key pins, a native RSA2 precreate/verify adapter, bounded A402, strict original Payment-Proof, AES-GCM/native ciphertext artifacts, monotonic and late-paid facts, funding guards, refund capacity and original-scope reconciliation. All 49 new test definitions remain uncompiled/unexecuted; synthetic-key lazy initialization now uses the pinned standard library two-result interface. Platform transactions/outbox/unique consumption/signed receipt archive/secret and authority registry, authenticated HTTP/SDK/PayLauncher, exclusive host and durable recovery workers, actual refund/payout/commission/statements and trusted usage/hour meters/ledger remain unwired. No real personal-seller/channel qualification or charging is enabled. All 56 tasks/222 steps remain open. No SQL changes/migrations. Complete all production source, actual dependency wiring and test source before batch validation. This is a source checkpoint, not a completed development round.

## R026 PT-001—040 支付事务与原凭据恢复源码 / Original-payment transactions and recovery source

R026新增[PT-001—040](../../docs/技术开发/附录-MachinePay支付事务与原凭据恢复源码.md)：原个人卖家支付/PAY意图/上下文/outbox事务、独立审核四证/销售开关、提交后一次precreate、完整签名envelope与独立归档钥环、同事务receipt/payment/channel/outbox、原Proof密文/QUERY/专用job、原capability过滤/generation恢复与实际调用关闭源码。新增前向016/DATA072—074，当前源码98规划操作/74平台表/48控制HTTP；16项新测试定义未编译/运行，SQL未迁移。正式API/worker宿主/秘密注册轮换、本人HTTP/SDK/A402/PayLauncher、原可执行资金/退款/设备/截止准入、完整退款/到账/佣金/对账单/付费续租/可信计量接受账务与真实资格仍待。全部56任务222子项开放；完整生产代码/实际依赖接线/测试源码后统一验证，本次不是开发轮完成。

R026 adds [PT-001–040](../../docs/技术开发/附录-MachinePay支付事务与原凭据恢复源码.md): original personal-seller payment/PAY-intent/context/outbox transactions, independent reviewer evidence and sale gates, commit-before-once precreate, complete signed envelopes and separate archive keys, atomic receipt/payment/channel/outbox updates, encrypted original-proof QUERY/jobs, capability-filtered generation recovery and actual operation joins. Forward016/DATA072–074 yields 98 planned operations/74 platform tables/48 control HTTP operations. All 16 new test definitions remain uncompiled/unexecuted; no migration ran. API/worker hosts and secret registry/rotation, HTTP/SDK/A402/PayLauncher, uniquely usable funds/current refund-device-deadline admission, full refunds/payouts/fees/statements/paid renewal/trusted metering/acceptance/ledger and real qualification remain open. All 56 tasks/222 steps remain unaccepted. Complete all production source, actual dependency wiring and test source before batch validation. This is a source checkpoint, not a completed development round.

## R026 PH-001—040 原生支付宿主与本人接口 / Native payment host and owner HTTP source

R026新增[PH-001—040](../../docs/技术开发/附录-MachinePay原生宿主与本人支付接口源码.md)：原生显式注册/独立秘密/双钥环、有界文件与原scope、专用恢复和到期原退役宿主、实际join后关闭、既有本人支付create/get两个HTTP及正式API/worker入口源码。当前50控制HTTP/98规划/74平台表，无SQL变更；12项新测试定义及既有全部R026源码均未编译/运行。SDK/Proof/A402/实际PayLauncher、原资金唯一消费/当前退款设备截止准入、完整渠道退款到账佣金对账/续租/可信计量接受账务与真实资格仍待。全部56/222开放，完整生产代码/实际接线/测试源码后统一验证；本次不是开发轮完成。

R026 adds [PH-001–040](../../docs/技术开发/附录-MachinePay原生宿主与本人支付接口源码.md): explicit native registration, independent secrets and proof/receipt keyrings, bounded rooted files, dedicated original recovery/expiry, actual joins before key release, two existing authenticated payment create/get HTTP operations and API/worker entrypoints. Current source: 50 control HTTP operations/98 planned operations/74 platform tables; no SQL changes. All 12 new test definitions and earlier R026 source remain uncompiled/unexecuted. SDK/Proof/A402/PayLauncher, unique funds/current refund-device-deadline admission, full qualified refunds/payouts/fees/statements/renewal/trusted metering/acceptance/ledger and real qualification remain open. All 56 tasks/222 steps are unaccepted. Complete all production source, actual wiring and test source before batch validation; this is not a completed round.

## R026 GF-001—040 原订单资金分配与当前准入 / Original allocation and current admission source

R026新增[GF-001—040](../../docs/技术开发/附录-原订单资金唯一分配与当前准入源码.md)：原订单唯一资金分配DATA075/前向017、原验签后独立元任务和原子READY/outbox、当前主体四证/gate/退款/期限及路由/小时租赁/密钥三消费者源码。当前50控制HTTP/98规划/75平台表；14项新增测试定义未编译/运行，017未迁移。当前仅显式零费用档；旧正向付款标记夹具升级、SDK/Proof/A402/实际PayLauncher、完整费用退款到账佣金对账/付费续租/可信计量接受账务和真实资格仍待。全部56任务222子项开放，完整生产代码/实际依赖接线/测试源码后集中验证；本次不是整轮完成。

R026 adds [GF-001–040](../../docs/技术开发/附录-原订单资金唯一分配与当前准入源码.md): unique original-order allocation DATA075/forward source 017, metadata-only admission after verified payment, atomic READY/outbox, current parties/four approvals/gate/refund/deadline guard and route/rental/scoped-key consumers. Current source: 50 control HTTP operations/98 planned operations/75 platform tables; 14 new test definitions remain uncompiled/unexecuted, and 017 is unapplied. Only the explicit zero-fee profile is wired. Legacy positive flag fixtures, SDK/Proof/A402/PayLauncher, full qualified fees/refunds/payouts/statements/renewal/trusted metering/acceptance/ledger and real qualification remain open. All 56 tasks/222 steps are unaccepted. Complete all production source, actual dependency wiring and test source before batch validation; this is not a completed round.

## R026 CK-001—040 原付款挑战与双端恢复 / Original checkout challenge and recovery source

R026新增[CK-001—040](../../docs/技术开发/附录-原订单付款挑战与双端恢复源码.md)：原卖家当前付款选项、原订单唯一付款查找、原402挑战、单Proof头提交/恢复五接口，React/Taro与React/Vite原订单付款页、安全元数据journal和会话代次隔离源码。当前55控制HTTP/103规划/124 schema/75平台表；26项新增测试定义未编译/运行，无SQL变更或迁移。真实获准PayLauncher/小程序付款能力、SDK付款前原买方授权及数据面重试、跨端回调恢复、完整费用退款到账佣金对账/付费续租/可信计量接受账务、旧正向夹具与真实资格仍待。当前端未安装获准PayLauncher，付款唤起按钮保持关闭。历史截图/GIF不能证明本次新页面；按全量代码后集中验证要求暂未重录。全部56任务222子项开放，本次不是整轮完成。

R026 adds [CK-001–040](../../docs/技术开发/附录-原订单付款挑战与双端恢复源码.md): original-seller payment options, unique original-order payment lookup, original 402 challenge, single-header proof submission/recovery, React/Taro and React/Vite original checkout panels, a metadata-only recovery journal and session-generation guards. Current source: 55 control HTTP operations/103 planned operations/124 schemas/75 platform tables; 26 new test definitions are uncompiled/unexecuted. No SQL changes or migrations. The approved actual PayLauncher/miniapp integration, prepayment original-buyer SDK authority, SDK data-plane recovery, cross-client wallet recovery, full fees/refunds/payouts/commission/statements/renewal/trusted metering/acceptance/ledger, legacy positive fixtures and real qualification remain open. No approved PayLauncher is installed: payment launch stays disabled. Existing screenshots/GIFs predate these source changes; fresh captures wait for full-source batch validation. All 56 tasks/222 steps remain open; this is not a completed round.

## R026 RF-001—040 原退款适配与加密归档 / Original refund adapter and encrypted archive source

R026新增[RF-001—040](../../docs/技术开发/附录-原退款协议与渠道恢复源码.md)：独立条件原退款profile/原意图、原生RSA2退款及原号查询、提交前UNKNOWN/3秒提交及10秒查询准备/generation/成功保持、完整原签名回执AES-GCM归档源码。refund_fee按整笔累计额处理，签名提交本身不能确认本次部分退款；原号查询完整归属及明确成功才记金融事实。32项新增测试定义未编译/运行，103规划/124 schema/75平台表/55控制HTTP不变，无SQL变更/迁移。默认Machine Pay仍仅precreate/verify；真实原交易退款资格、持久累计锁/提交水位/任务、退款host/worker/HTTP和完整费用/佣金/到账/对账链继续待补。CK真实PayLauncher与SDK付款前授权/数据面恢复、GF031旧正向夹具及全56任务222子项继续开放。先完整源码/实际接线/测试源码，再集中验证和重录媒体；本次不是开发轮完成。

R026 adds [RF-001–040](../../docs/技术开发/附录-原退款协议与渠道恢复源码.md): a separate conditional original-refund profile/intent, native RSA2 submit/query, pre-dispatch UNKNOWN preparation, three-second submit and ten-second query boundaries, generations, monotonic success and encrypted complete signed refund archives. Submit refund_fee is the original trade cumulative total; only an exact signed original-refund query can confirm this partial refund. 32 new test definitions are uncompiled/unexecuted. Counts stay at 103 planned operations/124 schemas/75 platform tables/55 control HTTP operations; no SQL changes or migrations. Default Machine Pay still exposes only precreate/verify. Actual original-transaction refund qualification, durable cumulative locks/dispatch watermarks/jobs, refund host/worker/HTTP and full fees/commission/payout/reconciliation remain open. The CK approved PayLauncher/prepayment SDK authority/data-plane recovery, GF031 legacy positive fixtures and all 56 tasks/222 steps remain open. Complete production source, actual dependency wiring and test source precede batch validation and fresh media capture; this is not a completed development round.

## R026 RT-001—040 原退款事务与宿主恢复 / Original refund transactions and native host recovery source

R026新增[RT-001—040](../../docs/技术开发/附录-原退款持久事务与宿主恢复源码.md)：原退款服务/独立当前未消费价款审批、原实付解密后RSA重验、执行清理/预算/累计守恒、同事务原退款意图/渠道/job/outbox/幂等和提交前UNKNOWN、原号恢复/双代次及原生宿主可选配置源码。新增018及DATA076/077，当前103规划/124 schema/77平台表/55控制HTTP；23项新测试定义未编译/运行，018未迁移。当前未消费价款分支不代替完整含费/已赚取价款冲销/佣金/续租/到账对账；公开退款API、审批工作台/双端售后、独立复核重试、真实原交易资格及CK/GF等仍待。全部56任务222子项开放；先完整生产源码/实际接线/测试源码，再集中验证和重录媒体。本次不是开发轮完成。

R026 adds [RT-001–040](../../docs/技术开发/附录-原退款持久事务与宿主恢复源码.md): original-refund service/current independent unused-consideration decisions, offline RSA reverification after paid-archive decryption, work/cleanup/budget/cumulative guards, atomic refund intents/channel requests/jobs/outbox/idempotency, pre-dispatch UNKNOWN, original-number recovery/dual generations and optional native host composition. New 018 and DATA076/077 keep counts at 103 planned operations/124 schemas/77 platform tables/55 control HTTP operations; 23 new test definitions are uncompiled/unexecuted and 018 is unapplied. The unused-consideration branch does not complete fees/earned-service reversals/commission/renewal/payout/reconciliation. Public refund HTTP, reviewer/aftersale UI, reviewed retries, actual original-transaction qualification and CK/GF follow-ups remain open. All 56 tasks/222 steps stay open. Complete production source, actual wiring and test source precede batch validation and fresh media; this is not a completed development round.

## R026 RA-001—040 退款申请与双端审核 / Refund applications and independent review source

R026新增[RA-001—040](../../docs/技术开发/附录-退款申请独立审核与双端售后源码.md)：原买家申请/双方查询、当前独立金融审核和原退款同事务批准、拒绝/协调撤销、七项控制HTTP/同原生宿主、Taro/React售后与Web审核台、安全原请求恢复和会话代次源码。019/DATA078仅源码，当前108规划/128 schema/78平台表/62控制HTTP；32项新测试定义未编译/运行，019未迁移。申请不是批准/渠道结果；完整费用冲销/佣金/续租/到账结算对账、独立复核重试/历史导入、完整争议举证/运营生命周期、CK/GF与真实资格仍待。全部56任务222子项开放，先全部生产源码/实际接线/测试源码，再集中验证和重录媒体；本次不是整轮完成。

R026 adds [RA-001–040](../../docs/技术开发/附录-退款申请独立审核与双端售后源码.md): original buyer applications/participant reads, current independent financial review atomically composed with the original refund, rejection/coordinated revocation, seven control HTTP operations/same native host, Taro/React aftersale views/Web review desk and safe original-command recovery/session epochs. 019/DATA078 are source only; 108 planned operations/128 schemas/78 platform tables/62 control HTTP operations. 32 new test definitions remain uncompiled/unexecuted; 019 is unapplied. An application is neither approval nor a channel result. Full fee/earned-service reversals, commission/renewal/payout/reconciliation, reviewed retries/historical imports, dispute disclosure/reviewer lifecycle, CK/GF and actual qualification remain open. All 56 tasks/222 steps remain open. Complete production source, actual wiring and test source precede batch validation and fresh media; this is not a completed round.

## R026 DA-001—040 交付签收与可信账务 / Original delivery, independent usage and billing source

R026新增[DA-001—040](../../docs/技术开发/附录-交付签收独立计量与镜像账源码.md)：原交付/本地签名报告恢复、当前原买家唯一签收与取消、独立计量核验、整单有理累计费用/佣金差额、同事务成交资格/原预算/追加镜像账，六既有控制HTTP/同原生宿主及Taro/React暂计账单源码。020/DATA079—081仅源码，当前108规划/137 schema/81平台表/68控制HTTP；42项新增/扩展测试定义未编译/运行，020未迁移。实际结果传输/本地签收程序、独立计量生产端/真实资格、同卖家重派/E2E换端/媒体/持久超时/争议、完整费用冲正/续租/到账对账、历史恢复、CK/GF继续待。全部56任务222子项开放，先全部生产源码/实际接线/测试源码，再集中验证和重录媒体；本次不是整轮完成。

R026 adds [DA-001–040](../../docs/技术开发/附录-交付签收独立计量与镜像账源码.md): original delivery/local signed-report recovery, unique current-buyer acceptance/cancellation, independent metering verification, cumulative rational price/commission deltas, atomic qualification/original budget/append-only mirrors, six existing control operations/same native host and Taro/React provisional bills. 020/DATA079–081 are source only; 108 planned operations/137 schemas/81 platform tables/68 control HTTP operations. 42 new/extended test definitions remain uncompiled/unexecuted; 020 is unapplied. Actual result transport/local buyer receipt, the independent meter producer/real qualification, same-seller retry/E2E rewrap/media/durable expiry/disputes, full fee reversals/renewal/payout/reconciliation, reviewed history recovery and CK/GF remain open. All 56 tasks/222 steps remain open. Complete production source, actual wiring and test source precede batch validation and fresh media; this is not a completed round.

## R026 DC-001—045 原结果传输与本地接收 / Original result transport and local receipt source

R026新增[DC-001—045](../../docs/技术开发/附录-原结果分块传输与本地接收源码.md)：PLAINTEXT/STANDARD原结果分块AEAD保存/完整核验、原提供方上传与原状态恢复、当前买家session绑定短时只读授权、原生加密接收和明确导出、React/Taro与Web显式本地核对签收、取消拒绝/过期退役及七HTTP源码。021/DATA082—084仅源码，当前115规划/150 schema/84平台表/75控制HTTP；40项新测试定义未编译/运行，021未迁移。平台能解密此档内容，不是E2E盲转；SDK/提供方守护编排、独立计量生产/真实资格、E2E/媒体/同卖家重派/持久超时、完整费用冲正/续租/到账对账、历史恢复与CK/GF继续待。全部56任务222子项开放，先全部生产源码/实际接线/测试源码，再集中验证和重录媒体；本次不是整轮完成。

R026 adds [DC-001–045](../../docs/技术开发/附录-原结果分块传输与本地接收源码.md): original PLAINTEXT/STANDARD chunked AEAD storage/full verification, provider upload/original status recovery, current-session scoped read-only delegation, native encrypted receipt/explicit export, React/Taro and Web local review, cancellation/rejection/expiry retirement and seven HTTP operations. 021/DATA082–084 are source only; 115 planned operations/150 schemas/84 tables/75 control HTTP operations. 40 new test definitions are uncompiled/unexecuted; 021 is unapplied. The platform can decrypt this profile: it is not E2E. SDK/provider-daemon wiring, the independent meter producer/real qualification, E2E/media/same-seller retry/durable expiry, full fee reversals/renewals/payout/reconciliation, reviewed history and CK/GF remain open. All 56 tasks/222 steps remain open. Complete source/actual wiring/test source precede batch validation and fresh media; this is not a completed round.

## R026 SI-001—040 标准SDK原订单入口 / Original native SDK ingress source

R026新增[SI-001—040](../../docs/技术开发/附录-标准SDK原订单文本入口源码.md)：三协议非流式原生JSON入口、原请求幂等、同原订单/预算/加密输入/首次租约事务、原API key只读结果/完整校验、有界等待/并发和可选原生宿主实际join源码。31项新测试定义全部未编译/运行。115规划控制操作/150 schema/84平台表/75控制HTTP不变，另有3个数据面入口；无SQL或迁移。实时SSE/提供方常驻编排、SDK付款前授权/获准PayLauncher、E2E/媒体/独立计量/真实资格、持久签收超时/争议、完整结算对账及CK/GF仍待。全部56任务222子项保留，按CF-01—05先完整生产源码/实际依赖/测试源码，再集中验证和重录媒体；本次不是整轮完成。

R026 adds [SI-001–040](../../docs/技术开发/附录-标准SDK原订单文本入口源码.md): three non-streaming native text SDK endpoints, original request idempotency, atomic original-order/budget/encrypted-input/first-lease admission, same-key read-only fully verified results, bounded waiting/concurrency and optional native host with actual caller join. 31 new test definitions are uncompiled/unexecuted. The 115 planned control operations/150 schemas/84 tables/75 control HTTP registrations stay unchanged; three data-plane endpoints are separate. No SQL changes or migrations. Live SSE/provider-daemon orchestration, SDK pre-payment authorization/approved PayLauncher, E2E/media/independent meters/real qualification, durable buyer deadlines/disputes, complete settlement/reconciliation and CK/GF remain open. All 56 tasks/222 steps stay in the same round. CF-01–05 require complete source/actual wiring/test source before batch validation and new media; this is not a completed round.

## R026 PD-001—045 原生提供方常驻编排 / Original native provider orchestration source

R026新增[PD-001—045](../../docs/技术开发/附录-原生提供方常驻接单与签名续期源码.md)：原生host-run常驻接单、独立本地批准/GPU维护预热、冻结内核和逐原manifest绑定、单控制owner/实际本地资源心跳、原进度回执/签名续期、原RECEIVE与当前摘要分离、原结果上传/清理及READY和原清理ACK后本地缓存退役、关闭实际join源码。31项新测试定义全部未编译/运行，1项历史定义改名。115规划控制操作/150 schema/84平台表/75控制HTTP及3个SDK数据面入口不变，无SQL或迁移。当前仅PLAINTEXT/STANDARD原生文本；其他内核守护编排/实时SSE/E2E/媒体/独立计量与真实资格、SDK付款前授权/获准PayLauncher、原空claim版本恢复/旧进程托管/容量轮换/崩溃密文回收、同卖家重派/持久超时/争议、完整结算对账和CK/GF仍待。全56任务222子项保留；CF01—05要求全量生产接线及测试源码后集中验证/重录媒体；本次不是开发轮完成。

R026 adds [PD-001–045](../../docs/技术开发/附录-原生提供方常驻接单与签名续期源码.md): explicit native host-run orchestration, independently approved local GPU warm-up, frozen resident kernels and per-original-manifest binding, one control owner with actual local-gate heartbeat, durable original progress/signed continuation, immutable RECEIVE versus current lease hashes, original output/cleanup recovery, local cache retirement after remote READY and cleanup ACK, and joined shutdown source. 31 new test definitions are uncompiled/unexecuted; one historical definition is renamed. The 115 planned control operations/150 schemas/84 tables/75 control HTTP and three SDK data-plane registrations stay unchanged; no SQL/migration. Only PLAINTEXT/STANDARD native text is wired. Other engine daemons/live SSE/E2E/media/independent meters/real qualification, SDK pre-payment authorization/approved PayLauncher, empty-claim version recovery/old-process custody/capacity rotation/crash-ciphertext cleanup, same-seller retry/durable deadlines/disputes, full settlement/reconciliation and CK/GF remain open. All 56 tasks/222 steps stay in this round. Complete production wiring and test source precede batch validation and new media under CF01–05; this is not a completed development round.

## R026 KE-001—050 端点密钥与签名密文流 / Endpoint keys and signed cipher source

R026新增[KE-001—050](../../docs/技术开发/附录-端点密钥与签名二进制密文流源码.md)：固定libsodium/JCS依赖、原OS秘密根/单写者AEAD密钥库、分用途钥与八项元数据CLI、独立pin/原轮换回执恢复/撤销及join、签名BINARY_STREAM_1/加密END/仅密文摘要清单/无钥结构检查、原双方用途组合/完整输入屏障/同库预检/独立传输期限源码。31项新测试定义未编译/运行，依赖工具未调用。新增独立未发布envelope Schema，115控制规划/150既有schema登记/84表/75控制HTTP及3个SDK入口不变，无SQL/迁移。平台E2E授权/盲转持久化及原密文补传、本机SDK/实际Host/LOCAL_SEGMENTED_1、Android JNI/媒体/有界重排、原签收/独立计量及真实资格仍待；不能称完整可用或合格E2E。全部56任务222子项继续保留，先完整生产接线/测试源码再集中验证和重录媒体；本次不是开发轮完成。

R026 adds [KE-001–050](../../docs/技术开发/附录-端点密钥与签名二进制密文流源码.md): pinned libsodium/JCS source, original OS-root/single-writer AEAD key vault, separate key purposes and eight metadata CLI operations, independent pins/original rotation-receipt recovery/revocation/join, signed BINARY_STREAM_1/encrypted END/ciphertext-only manifest/opaque structural inspection, exact-party composition/full INPUT barrier/shared preflight and separate transport deadlines. 31 new test definitions are uncompiled/unexecuted; the dependency tool has not run. An independent unpublished envelope schema is added; 115 planned control operations/150 existing registered schemas/84 tables/75 control HTTP and three SDK data-plane registrations stay unchanged, with no SQL/migration. Platform E2E authorization/blind storage/original retransmission, local SDK/actual Host/LOCAL_SEGMENTED_1, Android JNI/media/bounded reorder, original acceptance/independent meters and real qualification remain open. This is not a usable or qualified complete E2E path. All 56 tasks/222 steps stay open; complete production wiring/test source precede batch validation and new media. This is not a completed development round.

## R026 IC-001—050 原密文托管与不可变补传 / Original cipher custody and retry source

R026新增[IC-001—050](../../docs/技术开发/附录-原密文托管与不可变补传源码.md)：原OS根/单写者AEAD密文托管、完整预留/WRITING先于加密生产、READY原清单重放/中断不重加密、原签名对象完整检查/原字节范围补传、无私钥副本公钥用途与撤销、串行过期清理/满双槽仍可取消/实际join、父节点双owner先同时取消、四原生组合API/三元数据CLI源码。26项新测试定义未编译/运行，含4项独立native-tag组合。本地托管不是LOCAL_SEGMENTED_1、私有END/执行/签收/可信usage；平台E2E原授权与无钥持久化/真实网络恢复、SDK/Host私有执行/Android JNI/媒体/全局4MiB背压重排/独立计量与真实资格、未知替换残留显式恢复仍待。115控制规划/150既有schema登记/84表/75控制HTTP及3个SDK入口不变，无SQL/迁移。全56任务222子项保留，先完整生产接线及测试源码再集中验证/重录媒体；本次不是开发轮完成。

R026 adds [IC-001–050](../../docs/技术开发/附录-原密文托管与不可变补传源码.md): original OS-root/single-writer AEAD ciphertext custody, full reservation/durable WRITING before encryption, original READY-manifest replay/no re-encryption after interruption, complete signed opaque checks/exact-byte ranges, public-only key custody/revocation, serial expiry/full-slot cancellation/actual join, both parent owners canceled before either join, four native APIs and three metadata CLI operations. 26 new test definitions are uncompiled/unexecuted, including four separate native-tag compositions. This is not LOCAL_SEGMENTED_1, private END, execution, acceptance or trusted usage. Platform E2E grants/blind persistence/actual network recovery, local SDK/private Host/Android JNI/media/global 4 MiB flow and reorder/independent meters/real qualification and explicit unknown-replacement recovery remain open. 115 planned control operations/150 existing registered schemas/84 tables/75 control HTTP and three SDK data-plane registrations stay unchanged; no SQL/migration. All 56 tasks/222 steps remain in the same CODE_FIRST round; complete production wiring/test source precede batch validation and new media. This is not a completed round.
