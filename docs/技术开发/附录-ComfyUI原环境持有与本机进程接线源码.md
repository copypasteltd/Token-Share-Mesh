# ComfyUI原环境持有与本机进程接线源码

日期：2026-11-06。阶段：R026 / CODE_FIRST / **SOURCE_ONLY_UNVERIFIED**。本文承接[完整环境安装](附录-ComfyUI完整环境安装与原节点登记源码.md)、[私有媒体执行](附录-ComfyUI私有媒体执行与本地加密源码.md)及[原小时租赁](附录-小时租赁授权与累计计量源码细则.md)。当前私有握手为 `P11_COMFY_PRIVATE_PIPE_2` / `comfy-private-2`，上一检查点的 PIPE_1 只保留历史范围，不自动兼容。

本次编写环境运行持有、原节点启动和三管道进程所有权的生产源码与测试定义；没有编译、运行测试、安装依赖运行包、迁移数据库、启动 Python/模型/GPU/手机或浏览器。源码检查点不是完整开发轮完成。全部56任务/222子项与真实交易/设备/密码/性能验收继续开放。

## 一、边界与源码路径

| 组件 | 源码 | 职责与限制 |
|---|---|---|
| 原准备门闩 | `dev/src/client/execution/rental_preparation.go` | 在同一执行账本的原 RENTAL/PREPARING owner 内登记元数据引用；不产生 INVOKE、ready、计费或释放 |
| 环境持有 | `dev/src/client/comfyenv/use.go` | 精确已选目录、原节点根/密钥寿命、单次进程接管；关闭不能提前释放实际在用目录 |
| 本机选择与 INIT | `dev/src/client/adapters/comfyworker/config.go` | 只接受可信本机配置、有限模型映射/模块、原 GPU 与版本；不接受买方传文件路径 |
| 本机进程 | 同目录 `process.go`、`process_native.go`、`process_windows.go`、`process_linux.go` | 固定解释器/入口、独立内容/控制/结果管道、有界队列、实际 wait/join |
| 原节点接线 | `dev/src/client/node/kernels.go`、`rental_comfy.go` | `ProviderRental.OpenPreparedComfy` 使用同节点已开注册表和执行账本；没有新 HTTP 或推理 CLI |
| 私有执行器 | `dev/src/client/kernels/comfy-private/mesh_comfy/{worker,wire}.py` | 准备期间只能加载；一次 AUTHORIZE 与独立 ACK 后才接 INVOKE |

`comfyenv` 仍不执行 Python、pip、git 或节点安装脚本。实际原生启动由 `comfyworker` 执行，且依赖实际已安装、受签、本人确认的完整包。源码仓库尚未提供可直接使用的完整 Python/Torch/编解码器/节点/模型/配方运行包，禁止以合成测试文件冒充。

## 二、CH 编号约束

| 编号 | 需求/实现细则 | 当前状态 |
|---|---|---|
| CH-001 | 准备回调必须属于同执行账本、原 session/resource/local_generation | 已写源码，未验证 |
| CH-002 | 原 manifest/approval/physical hash、PREPARING、未停止及三种时钟共同核验 | 已写源码，未验证 |
| CH-003 | 回调限元数据，不递归 SQL、不启动内核/传内容/转资金 | 已写源码约束，未验证 |
| CH-004 | 重开账本没有活的准备时钟，不恢复准备或 ALLOW | 已写源码，未验证 |
| CH-005 | 只持有 VERIFIED 且在该 resource 已选中的精确环境 | 已写源码，未验证 |
| CH-006 | 历史维护代次不冒充当前租赁代次；当前 owner 由原账本决定 | 已写源码，未验证 |
| CH-007 | 同资源一次运行持有；同一不可变环境可供不同原 GPU 分别持有 | 已写源码，未验证 |
| CH-008 | 启动前全树复核，包含原 catalog/consent；不收养新文件/目录 | 已写源码，未验证 |
| CH-009 | 不可变目录以原节点 `os.Root.OpenRoot` 持有，不改原根/锁身份 | 已写源码，未验证 |
| CH-010 | 使用中拒绝同资源切换与相同目录删除 | 已写源码，未验证 |
| CH-011 | 发现目录漂移或登记提交未知，取消实际使用者；不提前释放引用 | 已写源码，未验证 |
| CH-012 | 未接管引用准备到期可关闭；复制别名共享取消/一次接管状态 | 已写源码，未验证 |
| CH-013 | 接管后只由实际 process/pipe/已进入操作 join 触发目录释放 | 已写源码，未验证 |
| CH-014 | Registry/Store 关闭需等待持有者，再清原根/节点密钥/OS 锁 | 已写源码，未验证 |
| CH-015 | RuntimeFiles/Layout 返回副本，格式化/JSON 默认隐藏私有路径 | 已写源码，未验证 |
| CH-016 | 仅固定 Comfy commit、PIPE_2、包内 Python 和 bridge/worker.py | 已写源码，未验证 |
| CH-017 | 模型集合摘要覆盖 category/alias/bytes/hash，排除机器绝对路径 | 已写源码，未验证 |
| CH-018 | 模型限有限类别、ASCII alias、绝对规范 safetensors 路径 | 已写源码，未验证 |
| CH-019 | extra 模块必须为已批准核心类的显式分组；custom_nodes 只来自本人所选包 | 已写源码，未验证 |
| CH-020 | 固定 NVIDIA UUID、PCI 与原 physical digest，不选 CPU/其他卡降级 | 已写源码，未验证 |
| CH-021 | 固定系统驱动路径；不继承用户代理、PYTHONPATH、服务钥或内容钥环境 | 已写源码，未验证 |
| CH-022 | 原生无窗口/无 shell 启动；三个私有 OS 管道，不开 HTTP/WebSocket 服务 | 已写源码，未验证 |
| CH-023 | 原准备截止与付费硬截止分开；准备加载不允许推理 | 已写源码，未验证 |
| CH-024 | 独立验证后的原 grant 必须匹配当前原账本及新鲜持续 ALLOW | 已写源码，未验证 |
| CH-025 | AUTHORIZE 一次且不能复活过期准备、换 grant 或改变原已付时限 | 已写源码，未验证 |
| CH-026 | 等独立管道 AUTHORIZED ACK 后才允许内容，防跨管道先后竞态 | 已写源码，未验证 |
| CH-027 | 控制写与内容写分别排队；每路一个进入者，无公共写锁 | 已写源码，未验证 |
| CH-028 | 半写/取消内容流关闭原会话，不猜下一帧或重新执行 | 已写源码，未验证 |
| CH-029 | 原账本持续控制截止推动独立单调计时器，原付费上限只锚定一次 | 已写源码，未验证 |
| CH-030 | CLOSE/超时先撤未来启动；实际 OS wait、所有 pump 与进入操作 join 后才 done | 已写源码，未验证 |
| CH-031 | 子进程退出后有界排空原管道，不能在缓冲 CLOSED 到达前误作成功 | 已写源码，未验证 |
| CH-032 | 格式化与错误不回传 prompt/key/path；Frame.Take 后字节归可信宿主负责 clear | 已写源码，未验证 |
| CH-033 | 原租内媒体 inbox、认证输入、创建前工作区预留/锚/密钥与实际恢复清理 | **仍待完整接线** |
| CH-034 | 正式媒体 Backend、OpenAI/Anthropic 媒体入口、资产权限、导出/接受回执 | **仍待完整接线** |
| CH-035 | 完整真实运行包、低权限身份/ACL、GPU/编解码组合、性能与跨端向量 | **仍待代码/依赖及统一验收** |
| CH-036 | 独立 ready 证据生产、小时可信水位/付费续租、平台 owner/预算/资金释放 | **仍待完整接线和验收** |

## 三、实际调用逻辑

1. 提供方独立安装并确认完整签名环境与有限节点，通过原 MAINTENANCE 门闩选定该环境。安装/选定本身不开共享，不产生实际运行资格。
2. 同买家、订单、卖家、设备与资源的原 `ProviderRental` 接受独立本机批准，原执行账本预留 `RENTAL/PREPARING`。无新 job、attempt 或 T1。
3. `OpenPreparedComfy` 调用原节点 `OpenComfyUse`。全树复核后，引用在原准备事务中登记；同资源第二次打开失败。
4. 本机可信 `RuntimeSelection` 与原 manifest 比较，构建有界 INIT；原进程完成信任校验并加载环境，返回精确 READY。该帧始终是 `qualification=NOT_GRANTED`，不能直接写平台 ready、开售或起表。
5. 原独立控制链取得并验证当前租赁 JWS、当前 grant 元数据，原账本绑定 grant 并提交新鲜签名控制 ALLOW。提供给 `Process.Authorize` 的是已验证对象，不是从来访 Bearer 计算出的摘要。
6. `Authorize` 再向引用绑定的原账本核验，发送 AUTHORIZE；收到同 session/local_generation/grant_digest 的一次 ACK 后允许包内内容写入口。此入口仍是 package-private，未接入认证输入/工作区前，公开调用无法直写原始 INVOKE。
7. 取消与 session 停止经独立控制管道发送。原父会话继续持有 GPU，子调用 STOPPED 不释放租赁父 owner。正式媒体 Backend 必须另核验原租内 inbox、实际停算/钥/目录清理与平台原回执。
8. 实际 `cmd.Wait`、管道协程、已进入宿主操作全部结束才关闭 `done`。引用据此释放目录；关闭超时只返回未知，不能自行完成资源/资金/结果 ACK。

## 四、原环境引用寿命

`Use` 是不可伪造的共享状态引用。复制只增加别名，不另建数据库或物理门闩。`Claim(joined)` 仅一次；可信宿主必须保证 `joined` 只在实际退出后关闭，此接口本身不是对恶意宿主的远程证明。

未接管引用：Close/准备到期取消并释放目录句柄。已接管引用：Close 只取消上下文，实际进程所有者停止并 join 后才释放。Registry.Close 先将状态置为 closing、防新入口、取消短操作与运行引用，等全部操作退出后清钥。节点之后才清 execution/journal/root/OS 锁。

目录漂移进入 QUARANTINED；登记提交失败进入 poisoned，两者均取消引用。引用仍保留到 actual join，不因失败先删目录。原账本 owner 的处置另走原租赁恢复协议，不能由 Registry 释放。

RuntimeFiles 包含本人选择后的精确文件及 `.mesh/catalog.json`、`.mesh/consent.json` 的精确大小/摘要；不包含未选包、可变模型路径或调用正文。Layout 导出副本并对 fmt/JSON 隐藏私有路径。验证目录不等于已完成 OS 低权限隔离；所有者高级篡改/原生注入仍不在当前已验能力内。

Layout 只在准备阶段导出；Claim 后引用丢弃自己的完整文件/目录清单，只保留原环境摘要、原根和原授权关系。INIT 实际写入管道后立即清除并撤下宿主副本，不等待模型加载期间继续占用大块初始化内存。Python 同样在准备后放开全文件清单；这只是有界实现措施，实际峰值内存/延迟尚未测量。

## 五、固定原生配置

| 项 | 选择方式 |
|---|---|
| ComfyUI | v0.44.0 / `2c75dc1b379a59e0880619d029607af5169a62ef`，不临时选 latest |
| Python | catalog 内 `Executable=true` 的精确路径；`-I -B -S`，不调用现有用户 Python |
| 入口 | 同包 `bridge/worker.py`；固定根为不可变环境根，不依赖用户工作目录 |
| 模型 | 1—64 个 category/alias；精确 safetensors 路径、大小与 SHA256，Python 在导入执行模块前检查 |
| 配方 | catalog 内 bridge/ 下精确文件，1字节—2MiB、摘要等于原 manifest.recipe_digest |
| GPU | `gpuid.Identity` 的 UUID/PCI/形状与原 physical digest；mask 为 UUID，不用任意 CUDA ordinal |
| 驱动 | Windows 系统 System32/nvcuda.dll；Linux 两个固定系统 libcuda.so.1 路径之一 |
| extra | 本机配置只能分组已批准核心类，模块文件必须在固定包；不增加任意 class |
| custom | 仅 Plan.Consent 所选 NodePack；类覆盖/未声明导出由 Python 拒绝 |
| 环境变量 | 系统最小项、精确 CUDA mask/PCI 顺序及 HF/Transformers 离线项；不继承代理/凭据/内容钥 |
| Windows 寿命 | 无窗口原生进程与 Kill-on-close Job；不是虚拟机、低权限或保密证明 |
| Linux 寿命 | 原生进程、独立控制 fd3、Pdeathsig；不使用 WSL/容器 |

模型集合摘要字节定义：将 `{category,alias,bytes,sha256}` 按 category、alias 的 ASCII 顺序排序；以 Go `encoding/json.Marshal` 输出该固定字段顺序结构数组，前缀 `P11_COMFY_MODEL_SET_1` 后一个 NUL，再对拼接字节做 SHA256。类别为固定 ASCII，alias 限 `[A-Za-z0-9_.][A-Za-z0-9_.+-]{0,191}` 且不能为 `.`/`..`，摘要为小写 hex，bytes 为正整数。绝对路径不参与。跨端字节向量仍未执行，不把本函数当已评审密码协议。

## 六、PIPE_2 帧与两阶段授权

帧保持四字节小端长度（包含一字节 kind），随后 kind/payload。INIT≤32MiB、INVOKE≤2MiB、ASSET_DATA≤64KiB、控制≤4096字节、worker 响应 payload≤64KiB；分配前限长。内容写和控制写各有独立队列/进入名额，结果队列容量1，阻塞部分不会无限复制媒体正文。

| 方向/kind | 字段或行为 |
|---|---|
| INIT / 1 | session 的原 ID/resource/local_generation/env/recipe、初始准备硬截止；environment 为完整精确运行清单。只允许加载 |
| READY / 101 | 新 profile/release/commit、原 session/env/recipe，qualification 必为 NOT_GRANTED |
| AUTHORIZE / 203 | session_id、local_generation、grant_digest、hard_deadline_unix_ms；只走控制管道 |
| STATE / 103 | phase=AUTHORIZED、session_id、local_generation、grant_digest；只能对应正在进行的原授权，拒绝重放/意外应答 |
| INVOKE / 2 | 仅 ACK 后；宿主仍须原认证内容/租内 inbox/工作区权；当前没有公开原始写入口 |
| CANCEL / 201 | 原 session/local_generation/invocation 的永久取消墓碑；不解除父占位 |
| STOP_SESSION / 202 | 原 session/local_generation，撤未来启动，退出实际执行器 |
| STOPPED / 105 | 私有子调用事实；不是平台清理、结果接受或小时计费凭证 |
| CLOSED / 106 | 精确 session、已停/撤未来启动和 requires_original_host_process_and_pipe_join=true；仍需实际 OS wait/join |

AUTHORIZE 只允许一次；错误/过期准备、重复授权或未知控制帧均关闭原 session。付费续租不能通过重复 AUTHORIZE 延长；正式续租/换 grant 仍需独立后续实现。原模型集、配方、买家/卖家/订单/设备/资源均不得在同进程中静默改变。

Python 将授权的付费时限一次转换为单调上限，所有子调用 cutoff 不得超过此值。Go 同样只锚定一次付费上限，再以原账本签名控制的真实 Deadline 为短时截止；200ms 元数据轮询不会按“本轮收到时间”延租，另有独立计时器，不依赖控制查询返回才停工。发生过期不能用新时间值复活进程。这里没有已测停止延迟/性能结论。

## 七、退出与未知处理

部分内容写失败/被取消，协议边界已不可信：先撤入口、请求终止实际 peer，再关闭各管道，等实际写返回，不继续解析下一次调用。控制和内容没有公共互斥锁，所以内容等待不能阻止独立取消进入控制管道。

Close 先给独立控制写250ms预算，最多等待5秒后请求 kill；调用方自己的等待超时返回错误，不关闭 done。实际子进程退出可能早于宿主读到缓冲 CLOSED；退出后关闭新入口，并给原结果管道250ms排空窗口。排空失败/缺 CLOSED/异常退出均保留错误；计时器不构造成功 join。完成仍以 `cmd.Wait`、5个 pump 和已进入操作实际退出为准。

Frame 中 RESULT 私有凭据可含内容钥，只能由可信宿主消费，不能给提供方 UI、日志、DB 或公开响应。Frame.Take 转移后的字节由调用者及时 clear；库不承诺清除所有 string/native/RAM 副本。实际产物/工作区清理、结果读取寿命和物理 owner 释放分别授权，不从 kill 或 CLOSED 推导。

## 八、下一段完整代码与验收门槛

仍需补 CH-033—036：正式原租内媒体 Backend、认证输入和工作区创建前登记/原锚/配额/密钥托管、崩溃恢复与原事实清理、OpenAI/Anthropic 媒体规范与结果/资产权限、独立 ready 证据、可信计量/付费续租和原平台释放。不能把普通文本 Backend 塞入伪媒体 T1，也不能按作品另收费用。

完整运行包与实际 Python/Torch/codec/node/model/recipe 锁定仍待；原生低权限服务/ACL、双端 E2E/盲转、SDK/密码向量、GPU/手机和渠道验收仍待。禁止以当前源代码/API存在开放交易。新增测试只是待运行定义，统一编号与文件见 dev/progress 的 CH 清单；既有 RN67、CE31、MW44 为前一源码检查点定义数量，均未执行。

DEV-55 部署/HTTPS/可观测性仍延后，DEV-32 无实际增强证明保持关闭。全部冻结平台001—015双份SQL与原端侧DDL不改；本次无新迁移。全部代码与测试源码齐备后才进行集中编译、测试、调试和组合验收；不启动任何本地虚拟化环境。

## CH 测试源码定义索引

| 编号 | 测试定义 | 源码 | 状态 |
|---|---|---|---|
| CH-T001 | TestRentalPreparationCallbackUsesOriginalOwnerWithoutInvokeOrRelease | dev/src/client/execution/rental_preparation_test.go | 未编译/未运行 |
| CH-T002 | TestRentalPreparationRejectsForeignApprovalAndExpiredClockBeforeCallback | dev/src/client/execution/rental_preparation_test.go | 未编译/未运行 |
| CH-T003 | TestRentalPreparationReopenNeverRestoresPreparationClock | dev/src/client/execution/rental_preparation_test.go | 未编译/未运行 |
| CH-T004 | TestRentalPreparationCancelledQueueNeverCallsRegistration | dev/src/client/execution/rental_preparation_test.go | 未编译/未运行 |
| CH-T005 | TestRentalExpectedScopeCannotBorrowReplacementGrantAndKeepsOriginalDeadline | dev/src/client/execution/rental_preparation_test.go | 未编译/未运行 |
| CH-T006 | TestComfyRuntimeUseRequiresSelectionAndOriginalPreparationGate | dev/src/client/comfyenv/use_test.go | 未编译/未运行 |
| CH-T007 | TestComfyRuntimeUseCopiesShareSingleClaimAndRejectConcurrentResource | dev/src/client/comfyenv/use_test.go | 未编译/未运行 |
| CH-T008 | TestComfyRuntimeUseCloseWaitsForActualClaimJoinBeforeKeyRelease | dev/src/client/comfyenv/use_test.go | 未编译/未运行 |
| CH-T009 | TestComfyRuntimeDriftCancelsExistingClaimWithoutEarlyDirectoryRelease | dev/src/client/comfyenv/use_test.go | 未编译/未运行 |
| CH-T010 | TestComfyRuntimeFileInventoryAndLayoutAreCopiesAndRedacted | dev/src/client/comfyenv/use_test.go | 未编译/未运行 |
| CH-T011 | TestComfyNativeControlWritesWhileOriginalContentPipeIsBlocked | dev/src/client/adapters/comfyworker/process_test.go | 未编译/未运行 |
| CH-T012 | TestComfyCancelledPartialContentPoisonsSessionAndJoinsEnteredWriter | dev/src/client/adapters/comfyworker/process_test.go | 未编译/未运行 |
| CH-T013 | TestComfyExpiredWatchdogCannotBeRevivedOrReanchored | dev/src/client/adapters/comfyworker/process_test.go | 未编译/未运行 |
| CH-T014 | TestComfyCancelledNextDoesNotConsumeQueuedPrivateResult | dev/src/client/adapters/comfyworker/process_test.go | 未编译/未运行 |
| CH-T015 | TestComfyOriginalHostFormattingDoesNotExposeProcessSelection | dev/src/client/adapters/comfyworker/process_test.go | 未编译/未运行 |
| CH-T016 | TestComfyNativeEnvironmentDoesNotInheritParentSecretsOrAlternativeGPU | dev/src/client/adapters/comfyworker/process_test.go | 未编译/未运行 |
| CH-T017 | TestComfyModelSetDigestIsPathIndependentSortedAndContentBound | dev/src/client/adapters/comfyworker/process_test.go | 未编译/未运行 |
| CH-T018 | TestComfyModelSetRejectsAmbiguousAliasesHashesAndUnsupportedCategories | dev/src/client/adapters/comfyworker/process_test.go | 未编译/未运行 |
| CH-T019 | TestComfyReadyRequiresNewProtocolExactOriginalScopeAndNoQualification | dev/src/client/adapters/comfyworker/process_test.go | 未编译/未运行 |
| CH-T020 | TestComfyForeignPrivateAuthorizationAckPoisonsOriginalSession | dev/src/client/adapters/comfyworker/process_test.go | 未编译/未运行 |
| CH-T021 | TestComfyDuplicateOrUnsolicitedAuthorizationAckCannotEnableContent | dev/src/client/adapters/comfyworker/process_test.go | 未编译/未运行 |
| CH-T022 | TestComfyExitedChildDrainsBoundedFramesUntilRealClosedBeforeJoin | dev/src/client/adapters/comfyworker/process_test.go | 未编译/未运行 |
| CH-T023 | test_preparation_can_load_but_cannot_invoke_before_original_authorization | dev/src/client/kernels/comfy-private/tests/test_control.py | 未编译/未运行 |
| CH-T024 | test_expired_preparation_cannot_be_revived_by_a_paid_hard_deadline | dev/src/client/kernels/comfy-private/tests/test_control.py | 未编译/未运行 |
| CH-T025 | test_duplicate_authorization_never_extends_or_replaces_original_paid_bound | dev/src/client/kernels/comfy-private/tests/test_control.py | 未编译/未运行 |
| CH-T026 | test_authorization_ack_precedes_content_permission_on_the_other_pipe | dev/src/client/kernels/comfy-private/tests/test_control.py | 未编译/未运行 |
| CH-T027 | test_child_deadline_cannot_exceed_original_paid_bound | dev/src/client/kernels/comfy-private/tests/test_control.py | 未编译/未运行 |
| CH-T028 | test_model_pins_are_checked_without_importing_inference_packages | dev/src/client/kernels/comfy-private/tests/test_model_inventory.py | 未编译/未运行 |
| CH-T029 | test_damaged_models_and_path_aliases_never_gain_runtime_mapping | dev/src/client/kernels/comfy-private/tests/test_model_inventory.py | 未编译/未运行 |
