# Endpoint keys and signed cipher streams / 端点密钥与签名密文流

**SOURCE_ONLY_UNVERIFIED. Not built or executed.** R026 remains one CODE_FIRST round covering all 56 tasks/222 steps. This library does not yet form an end-to-end platform, SDK, provider-worker or Android path. Complete production wiring and test source precede batch validation under [CF-01—05](../../../开发轮次执行规则.md).

**源码阶段，未编译、未运行。** 这里实现原安装密钥库、独立信任、轮换/撤销和二进制密文库；目前只有密钥元数据命令进入 `p11-node`。完整协议与待办见[KE 技术附录](../../../../docs/技术开发/附录-端点密钥与签名二进制密文流源码.md)。不能据此宣称平台 E2E、完整本机防读取或手机 JNI 已可用。

## Ownership and cryptography / 所有权与密码用途

`endpoint` reuses the enclosing native node's original OS-protected master, private directory and single-writer lock. An original random anchor is persisted before its AES-256-GCM journal is created. Missing/corrupt anchored state fails closed; it never replaces lost keys. Content receiving, identity signing and buyer receipt signing use independent keys. Public bundle self-signatures do not establish account ownership or initial trust.

`endpoint` 复用原 OS 秘密根/单写者锁。首次对端信任必须有独立 `OUT_OF_BAND` 身份指纹核验；平台页面、自签和 TOFU 不自动获信。旧身份签新 bundle，轮换回执丢失时恢复原 transition；旧钥只能显式用于原任务，撤销取消相关在途用途。关闭在 owned callbacks 实际退出后才清钥和释放父层锁。

`sodium` pins libsodium **1.0.22** and its source/archive hashes in [dependencies.lock.json](dependencies.lock.json). The native build requires CGO and the `p11_sodium` tag on native Windows/Linux, with a statically linked pinned library. Missing/mismatched native builds return `ErrUnavailable`; there is no substitute cipher or plaintext fallback. JCS uses byte-preserved upstream source under `dev/src/thirdparty/jsoncanonicalizer/` with both license notice and full Apache-2.0 text. Version pinning is not independent release-signature, interoperability or privacy qualification.

## Composition API / 组合接口

`Binding` must come from the independently confirmed original order/contract: exact buyer/seller, endpoints and key versions, kernel/policy, text profile, job/attempt/fence and transfer deadline. `SealObject` checks private INPUT with the same `gateway/core.Preflight` before writing ciphertext. `ConsumeObject` authenticates the entire INPUT through FINAL/manifest/EOF and repeats preflight before calling any consumer. OUTPUT can deliver authenticated prefixes; final END is delivered only after complete verification. Both APIs apply the original parent cancellation, key revocation and transport deadline.

组合接口不打开 HTTP 内容入口，不启动模型，也不授予 GPU/付款/签收权。INPUT/CONTROL 没有未知资源代次和未来输出版本；OUTPUT 绑定原 delivery/revision。传输期限可比计算硬截止晚，但最多硬截止后 30 分钟，且不能延长真实计算；业务更短期限继续适用。消费者必须在可信进程内并响应 context，不能作为卖家明文查看或导出 API。

`cipherstream` is the separately versioned `BINARY_STREAM_1`, not an extension implicitly covered by the old JSON channel. Header signatures bind C and sealed material; every record authenticates header hash, length and sequence; encrypted END itself uses TAG_FINAL. Public manifests contain ciphertext hashes only. `InspectOpaque` establishes structural/signature consistency, never private END or AEAD validity. Partial output is neither buyer acceptance nor trusted metering.

## Metadata CLI / 本机密钥管理

After a future qualified native build, the original state-directory owner can use `p11-node crypto-init|crypto-create|crypto-public|crypto-pin|crypto-rotate|crypto-pin-rotate|crypto-revoke|crypto-peer-revoke --state <original-private-absolute-directory>`. Input is strict JSON on stdin, at most 16 KiB; output is public metadata or fixed status. Never pass secrets as arguments. This stage has **not** run those commands.

初始化/查询输入 `{}`；create 输入 owner_id/endpoint_id/role/expires_at；pin 输入 bundle 和独立核验声明；rotate 输入原 bundle_hash/原 expires_at；pin-rotate 输入完整原 transition；revoke 输入原 bundle_hash。SELLER 创建须匹配原登记主体/设备；BUYER 的本机 owner 声明还需未来账号/合同控制面联核。元数据命令须在可变控制循环前使用原单一所有者，不并发另开写者。没有通用 decrypt、私钥导出或签名 oracle。

## Dependency materialization and remaining work / 依赖与待完成项

[准备原生密码依赖.py](../../../tools/准备原生密码依赖.py) contains bounded, digest-pinned fetch and native-static build source under this project's runtime. It has not been invoked. `build` is gated by the complete-scope validation admission; do not bypass that gate, install system-wide tools, or use Docker/WSL/VM. Native CPU/GPU/model/phone work remains deferred until the unified validation phase.

Still open: platform E2E authorization and blind relay storage, immutable ciphertext recovery, local SDK listener, actual private worker/Host integration, receipt/usage/business transactions, LOCAL_SEGMENTED_1, Android JNI, media dialects and bounded reorder. Limits are design budgets, not measurements. Enhanced confidential privacy stays disabled without its separate full-path proof. Existing plaintext SDK/provider source remains explicitly plaintext; this directory does not change its mode.
