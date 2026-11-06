# Native provider control / 原生提供方控制

## R020 宿主库与初始化 / Native host

独立pin/原订单批准和持久claim journal已接到InputAdapter及既有双围栏。CLI仅host-init/host-status，实际引擎仍未提供；先停止监督循环再关闭父Store。Windows DPAPI/ACL原生验证与Linux交叉构建分开登记。

[完整EH约束与准确JSON字段](../../../docs/技术开发/附录-原生执行宿主与内容适配接线.md) · [当前源码证据](../../tests/evidence/R020-验证报告.md)。历史R008—R019命令和边界保留，当前检查使用--round R020。

R011 adds client/supervisor: a running watchdog, frozen exact backend bindings, one-shot submission, original-query recovery, expiry/cancel/cleanup retries and bounded shutdown. node.NewSupervisor/RunSupervised retains the same OS lock and couples control failure/drain to cleanup. The adapters in tests are synthetic metadata components; production engine adapters, process isolation and content handling remain pending.

R011新增执行监督与原生控制联动；仍不开放CLI任务领取/内核/信任配置，不授GPU资格。详见[监督实施细则](../../../docs/技术开发/附录-原生执行监督与后端恢复实施细则.md)，核验当前源码使用--round R011。

R010 adds a native SQLite metadata inbox, signed Lease verification, one physical-resource gate and conservative stop/cleanup recovery. Explicit execution-init/status initialize/read only; no kernel, lease claimant, content decryption or paid execution is enabled. The original OS lock and encrypted node journal anchor the new database once; lost files are never recreated automatically.

R010新增client/execution与protocol/executionlease：原命令去重、双围栏、受管本地/任务/维护共门闩、到期/重开隔离和owner比较清理。SQLite只存限定元数据，prompt/输出/内容钥不进入库；它不是加密内容库。完整RuntimeSupervisor、平台发行/信任钥、真实适配器/停止/清理和租赁父owner仍待。

| New path / 新路径 | Responsibility / 职责 |
| --- | --- |
| execution | Bounded SQLite inbox, generations, one-shot submit reservation and cleanup CAS / 有界收件箱、代次、一次提交与清理比较更新 |
| protocol/executionlease | Strict signed Lease + independent approved plan binding / 严格签名租约及独立计划绑定 |
| node/execution.go | Once-only encrypted anchor and private SQLite lifecycle / 加密一次锚定及私有库生命周期 |

See [R010 details](../../../docs/技术开发/附录-原生执行收件箱与资源门闩实施细则.md) and [all remaining tasks](../../progress/README.md). Compile current sources with 核验原生源码.py --round R010; historical R008 commands below describe the original control baseline.

R008 implements the p11-node control foundation in the shared Go module. Windows current-user DPAPI is locally exercised; Linux Secret Service is implemented but has protocol-fixture/cross-build evidence only. This is not an inference engine, resource scheduler, content encryption gateway or complete provider release.

R008实现原生控制基座：本人显式初始化/登记/前台运行，加密本地状态及原命令恢复。当前心跳只报UNKNOWN，execution_enabled=false，不自授可售资格，不接受任务、下载模型、启动内核或打开监听端口。

| Path / 路径 | Responsibility / 职责 |
| --- | --- |
| cmd/p11-node | Foreground CLI; bounded stdin, safe output and cancellation / 前台命令、受控标准输入、安全状态和取消 |
| node | OS key adapters, AEAD journal, process locks, recovery and heartbeat / 系统钥库、加密日志、进程锁、恢复及心跳 |
| protocol/devicecontrol | Shared API/native DTOs, enrollment transport and request proofs / 服务端共用DTO、登记传输及签名证明 |

Build from the repository root / 从项目根构建：

```text
python -X utf8 dev/tools/核验原生源码.py --round R008
```

Outputs remain under runtime/bin. Explicit commands are init, enroll, recover, status, heartbeat and run, all requiring --state with an absolute private directory. init consumes public JSON on piped stdin; enroll consumes the once-only owner pairing token on piped stdin. No secret arguments/environment or plaintext fallback are supported. Never initialize over existing state after an unknown result.

输出在runtime/bin。所有命令均显式指定本人绝对状态目录；新建目录的父目录须已存在。Windows用本人/SYSTEM ACL和DPAPI；Linux需本人已解锁且运行中的Secret Service。缺钥、坏状态、重复写进程均停止，不静默重建身份。对外只输出不含秘密的型号/设备/版本/序号投影。临时数据、模型及凭据不能进入本源码目录。

See [bilingual usage](../../repository/USAGE.md), [complete recovery specification](../../../docs/技术开发/附录-原生提供方控制代理与本地恢复.md) and [R008 evidence](../../tests/evidence/R008-验证报告.md). Future Windows/Linux engine adapters and LMA-derived Android execution remain separate pending work. No local virtualization is used or required.

## R021 原生LLM源码

[42项原生worker/管道细则](../../../docs/技术开发/附录-原生LLM受限管道与可替换worker.md)；源码在client/adapters/llamaworker与client/kernels/llama-worker。本机批准原物理资源/模型/模板，原提交一次、独立取消、128KiB输出、同步后清理；空上下文和退出保持UNKNOWN，撤销不能被迟到完成回执覆盖。合成C ABI实进程测试不是实际模型/GPU/LP/E2E资格，节点CLI执行保持关闭。

## R026 KE-001—050 端点密钥与签名密文流 / Endpoint keys and signed cipher source

R026新增[KE-001—050](../../../docs/技术开发/附录-端点密钥与签名二进制密文流源码.md)：固定libsodium/JCS依赖、原OS秘密根/单写者AEAD密钥库、分用途钥与八项元数据CLI、独立pin/原轮换回执恢复/撤销及join、签名BINARY_STREAM_1/加密END/仅密文摘要清单/无钥结构检查、原双方用途组合/完整输入屏障/同库预检/独立传输期限源码。31项新测试定义未编译/运行，依赖工具未调用。新增独立未发布envelope Schema，115控制规划/150既有schema登记/84表/75控制HTTP及3个SDK入口不变，无SQL/迁移。平台E2E授权/盲转持久化及原密文补传、本机SDK/实际Host/LOCAL_SEGMENTED_1、Android JNI/媒体/有界重排、原签收/独立计量及真实资格仍待；不能称完整可用或合格E2E。全部56任务222子项继续保留，先完整生产接线/测试源码再集中验证和重录媒体；本次不是开发轮完成。

R026 adds [KE-001–050](../../../docs/技术开发/附录-端点密钥与签名二进制密文流源码.md): pinned libsodium/JCS source, original OS-root/single-writer AEAD key vault, separate key purposes and eight metadata CLI operations, independent pins/original rotation-receipt recovery/revocation/join, signed BINARY_STREAM_1/encrypted END/ciphertext-only manifest/opaque structural inspection, exact-party composition/full INPUT barrier/shared preflight and separate transport deadlines. 31 new test definitions are uncompiled/unexecuted; the dependency tool has not run. An independent unpublished envelope schema is added; 115 planned control operations/150 existing registered schemas/84 tables/75 control HTTP and three SDK data-plane registrations stay unchanged, with no SQL/migration. Platform E2E authorization/blind storage/original retransmission, local SDK/actual Host/LOCAL_SEGMENTED_1, Android JNI/media/bounded reorder, original acceptance/independent meters and real qualification remain open. This is not a usable or qualified complete E2E path. All 56 tasks/222 steps stay open; complete production wiring/test source precede batch validation and new media. This is not a completed development round.

## R026 IC-001—050 原密文托管与不可变补传 / Original cipher custody and retry source

R026新增[IC-001—050](../../../docs/技术开发/附录-原密文托管与不可变补传源码.md)：原OS根/单写者AEAD密文托管、完整预留/WRITING先于加密生产、READY原清单重放/中断不重加密、原签名对象完整检查/原字节范围补传、无私钥副本公钥用途与撤销、串行过期清理/满双槽仍可取消/实际join、父节点双owner先同时取消、四原生组合API/三元数据CLI源码。26项新测试定义未编译/运行，含4项独立native-tag组合。本地托管不是LOCAL_SEGMENTED_1、私有END/执行/签收/可信usage；平台E2E原授权与无钥持久化/真实网络恢复、SDK/Host私有执行/Android JNI/媒体/全局4MiB背压重排/独立计量与真实资格、未知替换残留显式恢复仍待。115控制规划/150既有schema登记/84表/75控制HTTP及3个SDK入口不变，无SQL/迁移。全56任务222子项保留，先完整生产接线及测试源码再集中验证/重录媒体；本次不是开发轮完成。

R026 adds [IC-001–050](../../../docs/技术开发/附录-原密文托管与不可变补传源码.md): original OS-root/single-writer AEAD ciphertext custody, full reservation/durable WRITING before encryption, original READY-manifest replay/no re-encryption after interruption, complete signed opaque checks/exact-byte ranges, public-only key custody/revocation, serial expiry/full-slot cancellation/actual join, both parent owners canceled before either join, four native APIs and three metadata CLI operations. 26 new test definitions are uncompiled/unexecuted, including four separate native-tag compositions. This is not LOCAL_SEGMENTED_1, private END, execution, acceptance or trusted usage. Platform E2E grants/blind persistence/actual network recovery, local SDK/private Host/Android JNI/media/global 4 MiB flow and reorder/independent meters/real qualification and explicit unknown-replacement recovery remain open. 115 planned control operations/150 existing registered schemas/84 tables/75 control HTTP and three SDK data-plane registrations stay unchanged; no SQL/migration. All 56 tasks/222 steps remain in the same CODE_FIRST round; complete production wiring/test source precede batch validation and new media. This is not a completed round.
