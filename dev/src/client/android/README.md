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
