# Original native Host / 原订单原生宿主

`Host` keeps independently installed issuer pins and local approval separate from incoming commands. `Prepare` durably records the original claim before network delivery, verifies the original manifest/lease, and keeps the bounded input only in an owned in-memory ticket. Reopening journals recovers metadata; it cannot replay a prompt or invent another execution.

`OpenNative` is an explicit trusted owner action after preparation. It verifies the complete local package/environment and every matching approval, checks the GPU UUID physical gate, and obtains a maintenance generation before model warm-up. Actual PIPE_4 READY must match the original resource, selected UUID/PCI, environment, model and template. Successful warm-up ends only its initialization generation before publishing the session. Unknown warm-up retains that owner across restart. It is not proof that a model was unloaded or an inference job cleaned.

宿主当前只拥有一个驻留原生环境。`NativeSession.Run` 启动监督器，等待 `Ready` 后，只允许同一宿主的 `PreparedExecution` 经 `Start` 一次提交。未关闭会话时不能另装通用后端绕过该绑定。输入、显卡清理及结果交付分别授权：原工作凭证经实际同步停止、清理后才能复用资源；加密结果仍只由原票据读取。

The manifest's `input_ciphertext_digest` always identifies the encrypted asset. The accepted delivery creates a separate `PlaintextBinding` held only in call-context memory. The supervisor transfers it to its owned submission context; the native adapter checks the exact original lease, manifest and body before normalization and before submission. No plaintext fingerprint is added to HTTP, SQLite or logs. This relies on the authenticated platform input path in PLAINTEXT/STANDARD; it is not end-to-end authentication against a signed plaintext digest.

`Close` drains the original supervisor, waits for the owned worker to exit, and revokes the session's result scope before closing the Host journal. A kill, timeout or process exit cannot fabricate an UNKNOWN job's cleanup proof. Resident model memory may remain between completed jobs until this explicit close; no extra model copy or public content listener is created.

当前 CLI 仅提供控制、初始化、状态及只读检查；不接受模型内容或自动批准资格。真实模型的独立标签测试与合成平台订单/签发/支付夹具分别记录，不授予 E2E、LP、可信 usage、最终 ACK、资金或性能资格。实现约束见项目根 `docs/技术开发/附录-原生宿主内核接线与输入驻留绑定.md`。
