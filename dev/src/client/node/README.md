# Original native Host / 原订单原生宿主

`Host` keeps independently installed issuer pins and local approval separate from incoming commands. `Prepare` durably records the original claim before network delivery, verifies the original manifest/lease, and keeps the bounded input only in an owned in-memory ticket. Reopening journals recovers metadata; it cannot replay a prompt or invent another execution.

`OpenNative` is an explicit trusted owner action after preparation. It verifies the complete local package/environment and every matching approval, checks the GPU UUID physical gate, and obtains a maintenance generation before model warm-up. Actual PIPE_4 READY must match the original resource, selected UUID/PCI, environment, model and template. Successful warm-up ends only its initialization generation before publishing the session. Unknown warm-up retains that owner across restart. It is not proof that a model was unloaded or an inference job cleaned.

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
