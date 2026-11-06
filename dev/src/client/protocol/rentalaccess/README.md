# Rental session authority / 小时租赁会话授权

**R026 · SOURCE_ONLY / UNVERIFIED · 未编译、未运行测试。** Platform rental HTTP and native encrypted-control journal sources are now wired; Android/native physical execution and cleanup remain closed and unverified. 本包不提供支付资格、GPU占用票据或已验收会话执行。

`claims.go` / `access.go` use pinned **golang-jwt/jwt v5.3.1** (MIT) for compact JWS with EdDSA/Ed25519. Only the fixed three-field protected header and strict integer-time, original-C2C claims are accepted. Independently provisioned issuer pins and the current `Expected` context are mandatory. `Expected.TokenDigest` must come from the current original grant record and match the entire credential; hashing the caller token into Expected is not independent trust. Unknown, duplicate, case-aliased or null scalar fields and noncanonical base64url are rejected. JWS is signed, not encrypted.

签名绑定原买卖双方、订单/会话、设备/物理资源及代次、terms/hash、批准环境/清单、端点aud、scope和硬截止。支持INVOKE、QUEUE_SELF、CANCEL_SELF、EXPORT_SELF；不授shell、RDP、主机管理员或任意节点安装。当前仅GPU_RENTAL/STANDARD；增强/加密租赁不开自动降级。

`Issued.TakeCredential` returns the Bearer once; JSON and ordinary formatting expose safe metadata/redaction only. Store its SHA-256 digest and safe claims, never the token or its reconstructing signature. Deliver the credential only after the original grant commits. A lost first response requires the explicit original-grant recovery/reissue workflow; replay stores must not retain the credential.

`control.go` signs metadata-only ALLOW/STOP responses bound to the original grant and durable request sequence. A response lasts at most 45 seconds and cannot extend the JWS, confirmed terms or paid hard deadline. It is not a payment renewal. Its stable signature identity is independent of JSON layout.

`window.go`要求先持久化序号再外发，重复确认不延时，恢复后须先存新序号，旧回复不能重启执行。磁盘写入失败/未知、撤权、到期、时钟回拨和STOP均关闭本窗口。Anchor codec只是严格元数据编解码，不代替原加密journal、持久flush、防回滚或物理门闩。本轮新增node加密journal/flush与实际控制传输源码；独立防回滚水位和实际物理执行仍待。`Window.Check`只证明控制活性；每次进内核仍要核scope、原session owner/代次、批准环境和串行invocation inbox。

Do not run this package's tests during CODE_FIRST. All remaining production code across the complete 56-task / 222-step scope must be implemented before batch compilation, tests and debugging. Existing tests are source definitions only.

[规范、消息字节顺序与42项测试源码登记 / Protocol and test-source inventory](../../../../../docs/技术开发/附录-小时租赁授权与累计计量源码细则.md) · [Dependency/license lock](dependencies.lock.json) · [Full backlog](../../../../全量代码优先实施清单.md)
