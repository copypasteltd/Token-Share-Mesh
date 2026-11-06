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

Local immutable ciphertext custody/retry now has unverified IC source. Still open: platform E2E authorization and blind relay storage, actual network recovery, local SDK listener, actual private worker/Host integration, receipt/usage/business transactions, LOCAL_SEGMENTED_1, Android JNI, media dialects and bounded reorder. Limits are design budgets, not measurements. Enhanced confidential privacy stays disabled without its separate full-path proof. Existing plaintext SDK/provider source remains explicitly plaintext; this directory does not change its mode.

## R026 IC-001—050 原密文托管与不可变补传 / Original cipher custody and retry source

R026新增[IC-001—050](../../../../docs/技术开发/附录-原密文托管与不可变补传源码.md)：原OS根/单写者AEAD密文托管、完整预留/WRITING先于加密生产、READY原清单重放/中断不重加密、原签名对象完整检查/原字节范围补传、无私钥副本公钥用途与撤销、串行过期清理/满双槽仍可取消/实际join、父节点双owner先同时取消、四原生组合API/三元数据CLI源码。26项新测试定义未编译/运行，含4项独立native-tag组合。本地托管不是LOCAL_SEGMENTED_1、私有END/执行/签收/可信usage；平台E2E原授权与无钥持久化/真实网络恢复、SDK/Host私有执行/Android JNI/媒体/全局4MiB背压重排/独立计量与真实资格、未知替换残留显式恢复仍待。115控制规划/150既有schema登记/84表/75控制HTTP及3个SDK入口不变，无SQL/迁移。全56任务222子项保留，先完整生产接线及测试源码再集中验证/重录媒体；本次不是开发轮完成。

R026 adds [IC-001–050](../../../../docs/技术开发/附录-原密文托管与不可变补传源码.md): original OS-root/single-writer AEAD ciphertext custody, full reservation/durable WRITING before encryption, original READY-manifest replay/no re-encryption after interruption, complete signed opaque checks/exact-byte ranges, public-only key custody/revocation, serial expiry/full-slot cancellation/actual join, both parent owners canceled before either join, four native APIs and three metadata CLI operations. 26 new test definitions are uncompiled/unexecuted, including four separate native-tag compositions. This is not LOCAL_SEGMENTED_1, private END, execution, acceptance or trusted usage. Platform E2E grants/blind persistence/actual network recovery, local SDK/private Host/Android JNI/media/global 4 MiB flow and reorder/independent meters/real qualification and explicit unknown-replacement recovery remain open. 115 planned control operations/150 existing registered schemas/84 tables/75 control HTTP and three SDK data-plane registrations stay unchanged; no SQL/migration. All 56 tasks/222 steps remain in the same CODE_FIRST round; complete production wiring/test source precede batch validation and new media. This is not a completed round.

## R026 ER-001—050 账户设备公钥目录 / Account and device public endpoint directory source

R026新增[ER-001—050](../../../../docs/技术开发/附录-账户设备公钥目录与原请求发布源码.md)：ER补入公共公钥/轮换验签隔离、原账户设备绑定与历史key用途占用、当前角色和设备证明、原请求引用重放后查当前、连续无分叉轮换/明确版本撤销、本人有界列表和原C2C订单发现、六HTTP及同DB原生宿主接线、客户端AEAD原意图/原证明发布和五元数据CLI源码。022/DATA085—086仅源；当前121控制规划/158schema/86平台表/81控制HTTP及3个SDK入口。账户登记不是独立信任、内容私钥持有或执行/资金/密文通道授权；旧明文租约不放开。平台E2E grant/无钥持久化/SDK与实际Host私有推理/独立信任及本机配对UI/JNI/媒体、全局4MiB流控/有界重排/LOCAL_SEGMENTED_1/独立计量/原签收与真实资格、过期或丢钥显式恢复/未知文件残留恢复和全部其余56/222生产接线继续待。测试源码未编译运行、022未迁移；同一R026全量代码及测试源码齐备后集中验证，不是开发轮完成。 本次35项新测试定义未执行，其中2项独立native组合；无新截图或录屏。

R026 adds [ER-001–050](../../../../docs/技术开发/附录-账户设备公钥目录与原请求发布源码.md): public-only bundle/rotation verification, current account/device binding, immutable public-key purpose claims, reference-only idempotency with current rereads, non-forking signed rotation, explicit version revocation, bounded own listings, original C2C order discovery, six HTTP registrations/the same native DB host, original AEAD client publication intents and five metadata CLI actions. 022/DATA085–086 are source only: 121 planned operations/158 schemas/86 tables/81 control HTTP and three SDK entries. 35 new test definitions including two independent native compositions are uncompiled/unexecuted; 022 is unapplied and no new media is captured. Account binding proves neither independent endpoint trust nor content-key possession, funds, execution or a cipher-channel grant. Existing plaintext lease guards stay closed to E2E. Platform E2E grants/blind storage, private SDK/Host execution, trusted pairing UI, JNI/media, global 4MiB flow/reorder, LOCAL_SEGMENTED_1, independent meters/receipts/real qualification, explicit expired/lost-key and unknown-file recovery and all other production wiring remain open. All 56 tasks/222 steps stay in the same CODE_FIRST round; this is not a completed round.
