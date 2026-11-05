# Android / LMA 派生内核

固定上游 LMA `v1.0.0 / code65`，提交 `0ca9445b1196ebd808b7c186ceda6ca79ffe0831`。源码、适配层和集成工具正在全量代码阶段实现，**尚未编译、测试或通过本项目真机验收**。上游测试和发布记录不是本项目验证证据。

LMA作为可替换内核依赖；项目小程序/Web继续采用NB04新粗野主义。派生包标识使用 `com.tokenshare.mesh.provider`，保留原LMA安装、模型和设置。初始化、模型选择与共享开关由手机提供方明确决定；不自动下载模型、不替换CPU/Vulkan后端，不公开客户任务正文或预览。

代码位于 `overlay/app/src/main/java/com/lma/app/`。`MeshMediaRunner` 直接调用上游 `LocalEngines` 与 `LocalEngine.Context`，共享真实 `ExecutionGate`，绕过普通 `JobStore` / `ArtifactStore` / 相册和聊天历史；正常本机任务仍遵循原LMA执行槽。`MeshLease` 验签和独立本机批准，`MeshJournal` 使用Android Keystore AES-GCM保存最高代次和原owner，`MeshResultCipher` 用64KiB缓冲生成逐块认证密文。

先在项目根目录运行 `python -B -X utf8 dev/tools/准备AndroidLMA.py --fetch`，仅准备固定版本源码及适配层，输出位于 `runtime/android/source/`；已有源树有改动时保留并拒绝覆盖。该命令不构建APK、不启动手机/模拟器、不下载模型。所有代码待办完成后才统一编译与测试。

当前公开入口只接受PLAINTEXT/STANDARD；应用私有目录与Android系统文件加密不能作为平台盲转或显卡所有者不可见的证明。加密传输、设备控制接线、完整资产/续租/恢复及Android真机验收继续列入全量待办，不以该适配库冒称完整可售手机。MIT与各内核、模型许可分别保留，见 `LICENSE.upstream.txt`、固定源码中的第三方notice。

# Android / LMA derived kernel

Upstream is pinned to LMA v1.0.0, code65, commit `0ca9445b1196ebd808b7c186ceda6ca79ffe0831`. This is **unverified source in the full-scope code phase**. No APK compilation, phone execution, model quality or commercial eligibility is claimed. Upstream evidence retains its original identity.

The overlay calls the installed upstream image/video engines through their private Context and the actual shared ExecutionGate. Platform jobs bypass user-visible task/artifact/chat histories. A Keystore-wrapped metadata journal retains original ownership and both fences; ciphertext output streams through a 64 KiB buffer. The derived application ID protects an existing LMA installation. Model downloads and CPU/Vulkan selection remain explicit owner choices.

The preparation tool fetches pinned source only and never downloads trained weights or invokes virtualization. Full code completion precedes batch compilation/testing. Control transport, complete encrypted modes, asset handling, lease renewal/recovery and physical-device acceptance remain open in the complete numbered plan. App sandboxing is not TEE or end-to-end secrecy from the device owner.


## 本机提供方链路（已写源码，尚未构建APK）

1. `MeshProviderActivity` 为派生应用入口，采用NB04黑边框/粗字/明确操作按钮；机型来自系统实际型号。原LMA工作台保留为内部模型管理入口，不为平台任务创建预览或普通历史。
2. 本人明确创建设备控制身份，仅复制公钥到设备绑定流程；Ed25519私钥种子由独立Android Keystore密钥包装保存。打开服务不自动创建身份或恢复推理，开机接单默认关闭。
3. 本人导入、核对并批准配置，固定原seller/device/resource/contract、签发公钥、本机已安装模型及CPU/Vulkan后端、任务类型/尺寸上限和充电/WiFi/电量/温控/内存条件。任务不能更新该配置。
4. 开启共享准备后，`MeshProviderController` 通过原LmaService、不可导出的本机Binder和同一ExecutionGate接收私有控制适配器交付的原签名任务。UI不接受客户正文或密钥Intent，也不声称设备已取得市场资格。平台M1签发/领取/上传通道仍须接通。
5. 独立进度通道最多缓存一条最新安全进度，以不短于2秒间隔发送原回执；网络等待不阻塞原生stdout读取。清理签名采用与Go一致的原字段顺序，先持久建立原owner再ACK，并先冻结回执正文/原键再请求。HTTP未知时只恢复原报告，不新建执行。
6. 正常停止只删除事先登记的本任务目录，确认进程退出后释放本机槽；失败保留原进程、模型租约、围栏和目录。重启恢复需系统启动计数已前进；没有该信号时拒绝猜测清理。同次进程内只允许明确重试原清理。
7. 长任务端侧仅接受同租约连续revision的真实签名续租，保持全部原合同/设备/模型/清单/双围栏/硬截止；落盘新revision后才延长时间窗。平台签发接线仍待，普通进度200不能续租。同一份已接受续租的重复响应保留原计时起点；确认的清理终态会取代更早的待确认进度，原正文/键保留。模型不会因此再次执行。

契约：[本人批准配置schema](../../../../docs/技术开发/contracts/android-owner-config-v1.0.schema.json)、[M1媒体清单schema](../../../../docs/技术开发/contracts/android-media-manifest-v1.0.schema.json)、[LM-001—051技术约束](../../../../docs/技术开发/附录-LMA多模态派生内核与全量代码阶段.md)。固定版本补丁见 `integration-patches.json`，不直接修改上游仓库。

`overlay/app/src/androidTest/` 已写AIB-001—007真机边界测试源码：身份与配置、双围栏/隔离、连续续租、先登记再创建目录、分块认证和未知报告原号恢复。当前未构建测试APK、未执行这些测试，亦未运行真实图像/视频模型或测得性能。它们不能替代完整设备/模型/交易验收。

## Provider-side operation defined by this unverified source

The derived launcher opens an NB04 owner panel displaying the actual manufacturer/model, safe state and explicit sharing/recovery controls. The original LMA workbench remains an internal model-management screen. Opening the app neither enables sharing nor restores an inference engine; boot-driven sharing is disabled.

The owner creates a local Ed25519 identity, exports only its public key, and separately approves a configuration binding their seller/device/resource/contract, independently checked issuer pin, installed model/engine/backend, limits and phone conditions. Tasks cannot rewrite that approval. The private service Binder rejects other UIDs and does not accept customer payloads through exported Intents.

Platform media calls run through the existing service and execution gate. Progress uses one coalescing metadata slot on a separate worker, keeping network waits out of the native stdout reader. Control requests sign exact bytes with fresh proof nonces; durable report bodies and original idempotency keys survive unknown replies. Cleanup reports match the Go signing format and confer no usage, acceptance or charge authority.

A synchronous pre-create hook records each owned engine directory before any work is written. Cleanup never infers ownership by comparing global directory listings. Failed cleanup retains original process/model/gate references. Restart recovery requires an advanced OS boot count and deletes only the original recorded directories. This is a conservative non-root recovery signal, not hardware attestation. Signed, contiguous renewals must preserve the original identity/contract/model/manifest/fences/hard deadline and persist before the time window changes; the platform renewal issuer is still pending.

Both contract schemas and seven physical-device boundary test definitions are included. No APK, phone/model execution or new test result is claimed. The full platform media dispatcher, key/grant/result lifecycle, other modalities, encrypted modes and commercial acceptance remain in the numbered full-code scope.

Original ownership is persisted before ACK and first submission. Repeated identical signed renewals keep the original monotonic clock. Confirmed cleanup supersedes earlier pending progress without fabricating its ACK or rewriting its original request bytes. These rules and the expanded boundary test definitions remain unverified source.
