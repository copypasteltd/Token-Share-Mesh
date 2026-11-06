# Rental session authority / 小时租赁会话授权

**R026 · SOURCE_ONLY / UNVERIFIED · 未编译、未运行测试。** Platform rental HTTP, encrypted original local approval and native shared-session/invocation gate sources are wired. Actual rental engine execution, watchdogs, Android sessions and platform cleanup release remain closed and unverified. 本包不提供支付资格、实际内核停工证明或已验收会话执行。

`claims.go` / `access.go` use pinned **golang-jwt/jwt v5.3.1** (MIT) for compact JWS with EdDSA/Ed25519. Only the fixed three-field protected header and strict integer-time, original-C2C claims are accepted. Independently provisioned issuer pins and the current `Expected` context are mandatory. `Expected.TokenDigest` must come from the current original grant record and match the entire credential; hashing the caller token into Expected is not independent trust. Unknown, duplicate, case-aliased or null scalar fields and noncanonical base64url are rejected. JWS is signed, not encrypted.

签名绑定原买卖双方、订单/会话、设备/物理资源及代次、terms/hash、批准环境/清单、端点aud、scope和硬截止。支持INVOKE、QUEUE_SELF、CANCEL_SELF、EXPORT_SELF；不授shell、RDP、主机管理员或任意节点安装。当前仅GPU_RENTAL/STANDARD；增强/加密租赁不开自动降级。

`Issued.TakeCredential` returns the Bearer once; JSON and ordinary formatting expose safe metadata/redaction only. Store its SHA-256 digest and safe claims, never the token or its reconstructing signature. Deliver the credential only after the original grant commits. A lost first response requires the explicit original-grant recovery/reissue workflow; replay stores must not retain the credential.

`control.go` signs metadata-only ALLOW/STOP responses bound to the original grant and durable request sequence. A response lasts at most 45 seconds and cannot extend the JWS, confirmed terms or paid hard deadline. It is not a payment renewal. Its stable signature identity is independent of JSON layout.

`window.go`要求先持久化序号再外发，重复确认不延时，恢复后须先存新序号，旧回复不能重启执行。磁盘写入失败/未知、撤权、到期、时钟回拨和STOP均关闭本窗口。Anchor codec只是严格元数据编解码，不代替原加密journal、持久flush、防回滚或物理门闩。node.ProviderRental现有源码将原本机加密批准/实际控制传输连接至共用resources行和串行租内inbox；实际内核适配/独立停工watchdog和防回滚水位仍待。`Window.Check`只证明控制活性；每次进内核仍要核scope、原session owner/代次、批准环境、认证输入和invocation。

`preparation.go` preserves the platform's exact original public Manifest/Spec fields and hashed bytes. Explicit trusted owner configuration supplies the independent physical/environment/model/recipe/connection/issuer approval. Strict decoding cannot manufacture it from a network task. The private Prepared value reserves no more than a 120-second preparation window; metadata recovery restores no expired TTL or ALLOW. The original approval and bytes are encrypted before the native gate reserves a physical row. This is public metadata protection, not customer-content E2E or hardware attestation.

Do not run this package's tests during CODE_FIRST. All remaining production code across the complete 56-task / 222-step scope must be implemented before batch compilation, tests and debugging. Existing tests are source definitions only.

[规范、消息字节顺序与53项测试源码登记 / Protocol and test-source inventory](../../../../../docs/技术开发/附录-小时租赁授权与累计计量源码细则.md) · [Native gate integration / 原生门闩接线](../../execution/README.md) · [Dependency/license lock](dependencies.lock.json) · [Full backlog](../../../../全量代码优先实施清单.md)

## R026 RN-091—110：租赁文本与独立停止源码

RN-091—110追加租赁专用文本接线源码：独立原grant/当前物理门闩、本人TextPolicy与三协议预检，随机AES-256-GCM仅RAM一次输入；不构造T1 job/attempt。独立本机截止循环不等SQL即发原取消，真实同步清理回执及进程/管道join后才可能释放本机槽；未知或超时保留owner，父节点未join则保留OS锁/密钥。输入/输出调用者仍负责本人持有的明文切片。

当前只限GPU_RENTAL/STANDARD文本。没有网络E2E、提供方不可见、TEE或实际性能结论；平台独立清理回执/资金释放、Android租内媒体、公开SDK/模型入口与全56/222其余代码仍待。累计67项租赁测试定义均未编译/运行。本段更新早期源码检查点的“租赁内核/watchdog仍待”状态，不追认历史验收。

Source only, unverified. Typed original rental input and private native pipes reuse encoding, not T1 authority. A separate monotonic deadline loop cancels without SQL; original cleanup facts plus actual joins gate local release. Unknown outcomes retain ownership. Local RAM encryption is not network E2E, provider blindness or a platform funds-release proof. No builds, tests, migration or device execution occurred.

## R026 WS-001—030 原媒体工作区与结果托管源码 / Original media workspace and custody source

R026新增[WS-001—030](../../../../../docs/技术开发/附录-租内媒体工作区与结果托管源码.md)：原租赁媒体工作区/native003元数据预留、原节点目录持有/退役删除/清理专用恢复、认证RAM媒体输入、专用子调用适配器和结果钥托管/读取撤销。23项新测试定义均未编译/运行；公开SDK、完整媒体会话监督/独立停工对账/ready/续租/可信水位/平台释放、完整实际包/低权限/E2E/性能仍待。全56任务/222子项开放，只登记native003源，未迁移/真实执行；完整代码后统一验证，源码检查点不是开发轮完成。
