# Usage / 使用指南

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
