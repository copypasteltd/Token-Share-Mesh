# Original rental runtime / 原会话执行监督器

R026 · CODE_FIRST · SOURCE_ONLY。当前源码未编译、未运行测试、未迁移、未启动模型/GPU/手机。全部56任务/222子项完整代码齐备后再统一验证。

`node.ProviderRental.OpenNative` 只在已持有原共享物理槽的 PREPARING 阶段显式加载本人批准的 llama.cpp 包。该包、GPU UUID/PCI/设备、环境、模型、模板和本机公开模型映射必须相符。不会另取 JOB/MAINTENANCE 槽，也不制造 T1 job/attempt/lease。模型暖机不是平台 ready、资金资格或买方执行权。

调用次序：

1. 本人通过可信本地配置独立批准原平台清单、物理GPU、包/模型/模板和连接；`PrepareRentalSession` 先加密保存批准，再取得原 RENTAL 槽。重复准备只能恢复原编号。
2. PREPARING 时显式 `OpenNative(ctx, installation, textPolicy, options)`。只加载固定摘要的本机文件；不会下载、选其他设备、CPU回退或自动切换 LM Studio。
3. `AttachControl` 独立查原平台摘要并核固定公钥/JWS；签名持续控制提交到同物理槽后才有 INVOKE。PREPARING 和未收到首次 ALLOW 均不可推理。
4. 每次用 `NativeRental.SealTextInput(ctx, verifier, credential, id, raw)` 验当前完整原授权及标准协议；随后 `Invoke`。未提交的封装用 `DiscardInput` 释放密钥与名额；调用者自行清除仍持有的原 `raw`。
5. 每次查询、取消和取得输出句柄也传当前 verifier/Bearer，分别核 QUEUE_SELF/CANCEL_SELF/INVOKE。输出句柄逐次核同原授权，只有一个读取者；读取出的切片归调用者，使用后必须清除并关闭句柄。
6. 查询/取消/清理沿用原 invocation/backend ID。重复已进入的 ID 只查，不能换 ID 重送或重建丢失输入。子清理结束仍占原租赁父槽，下一子调用保持同卖家/设备/模型/隐私档。
7. 到期、撤权、关闭和买方断开先减权并取消原调用。全部子调用有原停止/清理事实、后续启动撤销、真实进程及管道 join 后，才可能释放本机槽。平台清理回执/预算释放仍待实现。

| 独立机制 | 源码行为与边界 |
|---|---|
| 本机截止循环 | 缓存原共享门闩已接受的单调截止时间；不读SQL、不等页面/控制HTTP，直接走独立取消管道。较晚回复不能复活已经关闭的入口。 |
| 元数据监督 | 默认200ms轮询，核原owner/窗口/scope，处理查询、停止和清理；不是实际GPU停工时延承诺。 |
| 控制交换 | 默认10秒请求一次，最长45秒原签名窗口；失败不制造ALLOW，不延付费硬截止，不代表可收费可用区间。 |
| 一次输入 | 每封装独立随机AES-256-GCM；仅驻内存，一次解封；512KiB正文上限，最多8个待提交封装、1个活动子调用、1024条原调用元数据上限。 |
| 有界输出 | 复用原生双64KiB缓冲，单读取者；完成后默认最多3秒等待读取，随后丢弃/清理，不能无限囤积。 |
| 关闭所有权 | 默认8秒停工清理预算；超时保留原owner。父节点默认等待40秒，仍未join则保留OS锁/主密钥/账本，由原finalizer等待实际结束。 |

这里只支持 GPU_RENTAL/STANDARD 文本；本机加密封装不构成客户到设备的网络 E2E、设备所有者不可见、独立低权限隔离或TEE。Go解析和AES库可能产生受垃圾回收管理的临时副本；源码清除本人持有的切片，不声称所有堆/驱动内存得到可证明擦除。媒体素材、持久加密结果仓、公开模型监听/SDK/SSE/ACK、安装器、实际性能、Android租内执行与平台收单/清理均未因本模块存在而完成。

The runtime is source-only and unverified. It owns one original C2C rental session and serial typed child invocations, never a fabricated T1 job. Local approval and exact package/GPU/model identity precede warm-up; independent stored grant metadata, pinned signatures and the shared physical gate precede inference. A separate monotonic deadline loop cancels through the native control pipe without waiting for SQL. Query/cancel/output access rechecks current credentials and the original child binding. Unknown stop/cleanup or lifecycle join retains ownership; local cleanup cannot release platform funds.

Local RAM AEAD is not network E2E or provider blindness. Limits and timeouts are code defaults, not measured performance guarantees. Synthetic backend/pipe fixtures do not qualify hardware, funds, physical stopped work or privacy. See [RN-091—110](../../../../docs/技术开发/附录-小时租赁授权与累计计量源码细则.md) and the [complete 56-task/222-step scope](../../../全量代码优先实施清单.md).
