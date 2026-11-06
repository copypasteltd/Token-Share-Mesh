# Original native Host / 原订单原生宿主

`Host` keeps independently installed issuer pins and local approval separate from incoming commands. `Prepare` durably records the original claim before network delivery, verifies the original manifest/lease, and keeps the bounded input only in an owned in-memory ticket. Reopening journals recovers metadata; it cannot replay a prompt or invent another execution.

`OpenNative` is an explicit trusted owner action under independent local approval. The PD resident branch permits warm-up before its first claim; warm-up supplies no job authority. It verifies the complete local package/environment and every matching approval, checks the GPU UUID physical gate, and obtains a maintenance generation before model warm-up. Actual PIPE_4 READY must match the original resource, selected UUID/PCI, environment, model and template. Successful warm-up ends only its initialization generation before publishing the session. Unknown warm-up retains that owner across restart. It is not proof that a model was unloaded or an inference job cleaned.

宿主当前只拥有一个驻留原生环境。`NativeSession.Run` 启动监督器，等待 `Ready` 后，只允许同一宿主的 `PreparedExecution` 经 `Start` 一次提交。未关闭会话时不能另装通用后端绕过该绑定。输入、显卡清理及结果交付分别授权：原工作凭证经实际同步停止、清理后才能复用资源；加密结果仍只由原票据读取。

The manifest's `input_ciphertext_digest` always identifies the encrypted asset. The accepted delivery creates a separate `PlaintextBinding` held only in call-context memory. The supervisor transfers it to its owned submission context; the native adapter checks the exact original lease, manifest and body before normalization and before submission. No plaintext fingerprint is added to HTTP, SQLite or logs. This relies on the authenticated platform input path in PLAINTEXT/STANDARD; it is not end-to-end authentication against a signed plaintext digest.

`Close` drains the original supervisor, waits for the owned worker to exit, and revokes the session's result scope before closing the Host journal. A kill, timeout or process exit cannot fabricate an UNKNOWN job's cleanup proof. Resident model memory may remain between completed jobs until this explicit close; no extra model copy or public content listener is created.

当前 CLI 仅提供控制、初始化、状态及只读检查；不接受模型内容或自动批准资格。真实模型的独立标签测试与合成平台订单/签发/支付夹具分别记录，不授予 E2E、LP、可信 usage、最终 ACK、资金或性能资格。实现约束见项目根 `docs/技术开发/附录-原生宿主内核接线与输入驻留绑定.md`。

## R026 rental control / 小时租赁控制源码

`rental_journal.go` shares this node's OS lock/private root/master key, with an independently derived AES-GCM key and explicit random journal anchor. It durably stores metadata watermarks and STOP tombstones, never rental Bearers or content. `rental_control.go` independently resolves the stored grant digest using the original device, binds the locally approved manifest/connection and issuer pins, then sends one original-sequence control request. Replays keep their original absolute expiry; restart restores no ALLOW. Missing/corrupt/uncertain storage closes admission.

Only source is provided: no rental execution CLI, original physical-session ticket, native stop watchdog, Android rental consumer or cleanup release is enabled. This journal adds no GPU lock and does not change the existing client DDL. All 56 tasks/222 steps remain open, and all 42 rental test definitions are unexecuted under CODE_FIRST. See RN-051—070 in the hourly-rental annex.

## R026 CH-001—036 原环境与宿主源码继续 / Original environment and host source

R026新增[CH-001—036](../../../../docs/技术开发/附录-ComfyUI原环境持有与本机进程接线源码.md)：原Comfy环境运行持有、固定原生进程/三管道、PIPE_2准备/一次原授权ACK/单调截止和原ProviderRental准备接线。29项新测试定义未编译/运行；CH-033—036正式租内媒体Backend、认证工作区/恢复清理、公开交付、完整包/低权限/E2E、独立ready/计量/付费续租/平台释放仍待。全56任务/222子项开放，无SQL变更、迁移或真实执行；完整代码后统一验证，源码检查点不是开发轮完成。

## R026 WS-001—030 原媒体工作区与结果托管源码 / Original media workspace and custody source

R026新增[WS-001—030](../../../../docs/技术开发/附录-租内媒体工作区与结果托管源码.md)：原租赁媒体工作区/native003元数据预留、原节点目录持有/退役删除/清理专用恢复、认证RAM媒体输入、专用子调用适配器和结果钥托管/读取撤销。23项新测试定义均未编译/运行；公开SDK、完整媒体会话监督/独立停工对账/ready/续租/可信水位/平台释放、完整实际包/低权限/E2E/性能仍待。全56任务/222子项开放，只登记native003源，未迁移/真实执行；完整代码后统一验证，源码检查点不是开发轮完成。

## R026 MS-001—036 原媒体会话监督与停止回收源码 / Original media-session supervision source

R026新增[MS-001—036](../../../../docs/技术开发/附录-原租赁媒体会话监督与停止回收源码.md)：原节点持有的本机媒体会话监督/独立单调截止、真实Comfy叶子、一次原授权/单份输入及实际join、先停工后回收、两类会话共关闭、原媒体结果保留/撤销和本机会话清理CAS源码。27项新增测试定义均未编译/运行；公开媒体SDK/最终ACK、双方ready/可信小时水位/付费续租/平台资金释放、独立物理停工恢复及完整实际运行包/低权限/E2E/性能仍待。全部56任务222子项开放；本次无SQL变更或迁移。完整生产代码/实际依赖/测试源码齐备后统一验证，源码检查点不是开发轮完成。

## R026 MG-001—036 原租赁媒体入口与加密缓存源码 / Original-rental media gateway source

R026新增[MG-001—036](../../../../docs/技术开发/附录-原租赁媒体兼容入口与加密缓存源码.md)：原租赁本机STANDARD媒体入口、Images/明确旧版Videos/WAV speech/Responses资产映射、严格请求/独立scope、AES-GCM暂存/原HMAC幂等/提交前缓存配额、完整认证结果后加密缓存/本人单Range和原节点真实join源码。37项新增测试定义均未编译/运行。公开SDK/网络监听/E2E媒体传输/最终ACK、双方ready/可信小时计量/付费续租/物理停工对账/平台资金释放、完整实际配方/低权限/性能仍待。全部56任务222子项开放；无SQL变更或迁移。完整生产代码/实际依赖/测试源码齐备后统一验证，本次不是开发轮完成。

## R026 PD-001—045 原生提供方常驻编排 / Original native provider orchestration source

R026新增[PD-001—045](../../../../docs/技术开发/附录-原生提供方常驻接单与签名续期源码.md)：原生host-run常驻接单、独立本地批准/GPU维护预热、冻结内核和逐原manifest绑定、单控制owner/实际本地资源心跳、原进度回执/签名续期、原RECEIVE与当前摘要分离、原结果上传/清理及READY和原清理ACK后本地缓存退役、关闭实际join源码。31项新测试定义全部未编译/运行，1项历史定义改名。115规划控制操作/150 schema/84平台表/75控制HTTP及3个SDK数据面入口不变，无SQL或迁移。当前仅PLAINTEXT/STANDARD原生文本；其他内核守护编排/实时SSE/E2E/媒体/独立计量与真实资格、SDK付款前授权/获准PayLauncher、原空claim版本恢复/旧进程托管/容量轮换/崩溃密文回收、同卖家重派/持久超时/争议、完整结算对账和CK/GF仍待。全56任务222子项保留；CF01—05要求全量生产接线及测试源码后集中验证/重录媒体；本次不是开发轮完成。

R026 adds [PD-001–045](../../../../docs/技术开发/附录-原生提供方常驻接单与签名续期源码.md): explicit native host-run orchestration, independently approved local GPU warm-up, frozen resident kernels and per-original-manifest binding, one control owner with actual local-gate heartbeat, durable original progress/signed continuation, immutable RECEIVE versus current lease hashes, original output/cleanup recovery, local cache retirement after remote READY and cleanup ACK, and joined shutdown source. 31 new test definitions are uncompiled/unexecuted; one historical definition is renamed. The 115 planned control operations/150 schemas/84 tables/75 control HTTP and three SDK data-plane registrations stay unchanged; no SQL/migration. Only PLAINTEXT/STANDARD native text is wired. Other engine daemons/live SSE/E2E/media/independent meters/real qualification, SDK pre-payment authorization/approved PayLauncher, empty-claim version recovery/old-process custody/capacity rotation/crash-ciphertext cleanup, same-seller retry/durable deadlines/disputes, full settlement/reconciliation and CK/GF remain open. All 56 tasks/222 steps stay in this round. Complete production wiring and test source precede batch validation and new media under CF01–05; this is not a completed development round.

## R026 KE-001—050 端点密钥与签名密文流 / Endpoint keys and signed cipher source

R026新增[KE-001—050](../../../../docs/技术开发/附录-端点密钥与签名二进制密文流源码.md)：固定libsodium/JCS依赖、原OS秘密根/单写者AEAD密钥库、分用途钥与八项元数据CLI、独立pin/原轮换回执恢复/撤销及join、签名BINARY_STREAM_1/加密END/仅密文摘要清单/无钥结构检查、原双方用途组合/完整输入屏障/同库预检/独立传输期限源码。31项新测试定义未编译/运行，依赖工具未调用。新增独立未发布envelope Schema，115控制规划/150既有schema登记/84表/75控制HTTP及3个SDK入口不变，无SQL/迁移。平台E2E授权/盲转持久化及原密文补传、本机SDK/实际Host/LOCAL_SEGMENTED_1、Android JNI/媒体/有界重排、原签收/独立计量及真实资格仍待；不能称完整可用或合格E2E。全部56任务222子项继续保留，先完整生产接线/测试源码再集中验证和重录媒体；本次不是开发轮完成。

R026 adds [KE-001–050](../../../../docs/技术开发/附录-端点密钥与签名二进制密文流源码.md): pinned libsodium/JCS source, original OS-root/single-writer AEAD key vault, separate key purposes and eight metadata CLI operations, independent pins/original rotation-receipt recovery/revocation/join, signed BINARY_STREAM_1/encrypted END/ciphertext-only manifest/opaque structural inspection, exact-party composition/full INPUT barrier/shared preflight and separate transport deadlines. 31 new test definitions are uncompiled/unexecuted; the dependency tool has not run. An independent unpublished envelope schema is added; 115 planned control operations/150 existing registered schemas/84 tables/75 control HTTP and three SDK data-plane registrations stay unchanged, with no SQL/migration. Platform E2E authorization/blind storage/original retransmission, local SDK/actual Host/LOCAL_SEGMENTED_1, Android JNI/media/bounded reorder, original acceptance/independent meters and real qualification remain open. This is not a usable or qualified complete E2E path. All 56 tasks/222 steps stay open; complete production wiring/test source precede batch validation and new media. This is not a completed development round.
