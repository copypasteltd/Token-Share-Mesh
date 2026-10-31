# 真实原生 LLM 包校验与本机接入

版本：R023。衔接 NK-001—042、RC-001—060；当前私有握手为 `P11_LLAMA_PIPE_3`。本附录定义固定包安装、本机批准、只读检查与独立真实模型验证。源码与验证结果分别登记，最终结果以本轮证据为准。

## 1. 交付及权限边界

| 编号 | 事项 | 强制约束 |
| --- | --- | --- |
| NF-001 | C2C | 设备属于个人卖家。平台不能通过任务指定新程序、模型、节点、库、显卡或启动参数。 |
| NF-002 | 三类入口 | 准备包、检查包、显式测试是不同入口；它们不等于入驻、挂牌、领取任务或生产执行。节点控制入口继续默认关闭内容执行。 |
| NF-003 | 本机环境 | 仅使用原生 Windows 程序、目录、进程、管道及现有驱动；禁止 Docker、WSL、QEMU、虚拟机或模拟器。 |
| NF-004 | 不影响既有任务 | 测试只能启动自身进程，关闭自身句柄；不能停用其他应用、接管 LM Studio、卸载驱动、修改系统显卡策略或终止他人进程。 |
| NF-005 | 原权限 | 可信宿主先取得原物理资源准备权，再暖机、装配匹配 HostKernel，最后仅通过原持久 ticket 一次提交。包摘要或检查结果不授予这些权限。 |
| NF-006 | 模式 | 本轮仅推进 PLAINTEXT / STANDARD 原生文本路径。LP、网络 E2E、硬件增强档继续独立实施和验证，不由“磁盘加密”或“无监听”推导。 |
| NF-007 | 不降档 | 请求或新任务不能改变已批准的 GPU/CPU、内核、卖家、资源或隐私档位。显卡选择失败直接拒绝；CPU 必须有另一份明确批准。 |
| NF-008 | 不扩大资金事实 | 原生 token 计数、检查结果、测试成功、成功写出均不等于可信 usage、买家 ACK、收单、退款或卖家结算。 |

## 2. 固定输入与分块安装

| 编号 | 事项 | 强制约束 |
| --- | --- | --- |
| NF-009 | 上游版本 | llama.cpp v0.8.0 / `8642541b255bfb5dc39cd351a178be09013a55d6`；官方对应二进制为 `b11960`，不得取最新构建替换。 |
| NF-010 | CUDA 包 | 使用固定 Windows amd64 CUDA 12.4 主包与运行库包；两份归档各有官方 SHA-256、精确字节数、资产 ID，见 qualification.lock.json。 |
| NF-011 | 模型 | Qwen2.5-0.5B-Instruct 单文件 Q4_K_M，491400032 B，SHA-256 `74a4da8c9fdbcd15bd1f6d01d621410d31c6fc00986f5eb687824e7b93d7a9db`。固定版本与许可对象见锁文件。它是小模型验证输入，不是默认商业挂牌模型。 |
| NF-012 | 下载镜像 | 选择 hf-mirror 仅改变传输地址；仍要求与 Qwen 官方文件页相同的 SHA-256。不能信任镜像自报摘要，不能自动选择另一模型。 |
| NF-013 | 下载边界 | 显式 --fetch；约 1.2 GB 输入只落忽略的 runtime。缓存需重新校验；未完整下载或摘要不符不可安装、加载或执行。 |
| NF-014 | 解包边界 | 校验整份归档后才按白名单解包；拒绝绝对路径、穿越、ADS、符号链接、同名冲突。每项最多 594121728 B、总展开最多 2 GiB；该单项上限对应已固定的 ggml-cuda.dll。 |
| NF-015 | 安装内存 | 下载分块不超过 1 MiB，解包分块 64 KiB；不得把约 594 MB 的 CUDA DLL 一次读入内存。包验证以 64 KiB 缓冲计算摘要。 |
| NF-016 | 第三方许可 | 保留 Qwen Apache-2.0 原文并核验 Git blob；保留上游附带许可和固定 llama.cpp MIT 文本。NVIDIA 运行库再分发资格仍需在正式打包前单独核验；源码仓库不提交这些 DLL、权重或编译器。 |

## 3. 包文档及批准环境

| 编号 | 事项 | 强制约束 |
| --- | --- | --- |
| NF-017 | 包格式 | P11_LLAMA_PACK_1，最多 64 KiB；外部独立固定包文件的原字节 SHA-256。拒绝重复键、大小写别名、未知字段、尾随 JSON、错误档位或发行版。 |
| NF-018 | 包内容 | executable、model、template、完整 libraries 列表分别固定路径和 SHA-256；library、backend_registry、backend_core 显式选择主库、注册库、基础库。 |
| NF-019 | 三库关系 | 官方 Windows 对应 llama.dll、ggml.dll、ggml-base.dll；注册器/设备查询在 ggml.dll，设备类型/日志接口在 ggml-base.dll。三者都须在同目录白名单中，不能靠一个合成 DLL 推定真实发行布局。 |
| NF-020 | 相对路径 | 包内路径为受限 ASCII 分段、正斜杠、最多 512 字节；拒绝空段、父路径、盘符、反斜杠、保留设备名、尾点与 ADS。最终文件必须位于明确的本机 runtime 根目录下。 |
| NF-021 | 文件约束 | 拒绝符号路径、非普通文件、未列出的 DLL/SO、同目录大小写重复库、分片命名 GGUF、任意模板脚本。模板仅为已固定的内置名称，例如 chatml。 |
| NF-022 | 本机参数 | OwnerSelection 固定 capability、resource、gpu_device、context_tokens、batch_tokens、threads、gpu_layers。范围沿用 NK；资源和能力必须由原 Host 规则另行比对。 |
| NF-023 | 环境摘要 | SHA-256 对 `P11_LLAMA_ENV_1 + NUL + 包摘要 + NUL + 参数规范 JSON` 计算，绑定 GPU 名、上下文、批量、线程和层数。资源及能力继续单独授权，不因相同环境摘要互通。 |
| NF-024 | 计算不是批准 | PackageEnvironment 只是确定性计算；LoadPackage 还要求外部传入的批准环境完全一致，并验证所有文件。正式宿主须从本机批准规则取得期望值，不能信任任务自带期望值。 |
| NF-025 | 启动重核 | LoadPackage 不启动进程；Open 在实际启动前重新验证所有摘要。摘要扫描和暖机属于安装/准备阶段，不放进每次 token 的热路径。 |
| NF-026 | 升级 | 先停止原 worker、准备独立新包、重新检查和批准，再创建新环境。不能热替换 DLL，不能让旧租约、旧环境或旧结果继承新包身份。工具准备的可变研究目录不作为生产安装器。 |

## 4. 本机握手与提供方透明度

| 编号 | 事项 | 强制约束 |
| --- | --- | --- |
| NF-027 | 当前握手 | PIPE_3 INIT 依次为 profile、commit、主库、注册库、基础库、模型、模板名、设备名，以及 context/batch/threads/gpu_layers。其余正文、控制、STARTED、OUTPUT、DONE 继续沿用 NK/RC 类型和边界。 |
| NF-028 | 旧档位 | PIPE_1/2 均拒绝，不兼容回退。R021/R022 历史源码与证据保留原字节；它们的成功不代替 PIPE_3 当前验证。 |
| NF-029 | 安全启动错误 | INIT_ERROR=134，仅含当前 profile、固定阶段编号及 uint32 原生错误码；阶段为 LOAD_MAIN、LOAD_REGISTRY、LOAD_CORE、RESOLVE_API、LOAD_BACKENDS、SELECT_DEVICE、LOAD_MODEL。不回传文件路径、原生日志或正文。 |
| NF-030 | 错误语义 | 启动错误继续属于未知内核结果类别，不是清理回执。无效阶段/档位/额外字节拒绝；错误不能让任何原任务 owner 释放、重试提交或换设备。 |
| NF-031 | 本机可见 | LocalInfo 返回自身 PID、固定发行/提交、模型/模板/环境摘要和选择的显卡/层数。提供方可以了解装了什么、选了什么和启停状态；不提供 Prompt、生成正文、密钥或管道句柄。 |
| NF-032 | 选择与实测 | selected_gpu_device 只说明批准值；不能冒称物理卡身份或实测全层卸载。模块加载观察也不能独立证明每层都在 GPU 或系统能效达标。 |
| NF-033 | 内容链路 | 公共仍为 OpenAI Chat/Responses 或 Anthropic Messages；正文通过原许可进入原生私有管道，结果经 RC 密文仓交付。PIPE_3 不是第四种公共 API。 |
| NF-034 | 保密边界 | 不开本机明文内容 HTTP 监听；保留有界原生缓冲、独立取消管道、日志关闭、密文结果盘。仍不声称同账户管理员无法分析进程、显存或内存；低权限隔离、主机保护和 LP 防初中级拦截还需后续实装。 |

## 5. 只读检查与真实模型验证

| 编号 | 事项 | 强制约束 |
| --- | --- | --- |
| NF-035 | 检查命令 | p11-node kernel-inspect --runtime-root 接收最多 4096 B 标准输入 JSON；包含包路径、独立包摘要、环境摘要和本机选择。失败只输出固定安全信息，不回显参数。 |
| NF-036 | 检查结果 | 只返回批准候选元数据；execution_enabled=false、qualification=NOT_GRANTED。不开动态库、不加载模型、不启动 worker、不注册设备、不改控制状态。 |
| NF-037 | 常规测试 | 常规 Go 回归不自动下载或运行真实模型；包文件与启动错误的边界测试使用受控输入，不能宣称 GPU 验收。 |
| NF-038 | 独立标签 | p11_native_capi_fixture 为合成 C ABI 的实际进程测试；p11_native_real 为固定真实库/权重的 Windows CUDA 测试。分别登记，不能合并数量混淆来源。 |
| NF-039 | 实际内容验证 | 真实测试显式选择 CUDA0 / 999 层请求，检查自身进程中固定目录的 CUDA 模块，验证真实生成、同一模型复用及三协议 JSON/SSE 六组合；不保存生成正文，只保存计数、长度和摘要。 |
| NF-040 | 失败与清理验证 | 另测真实 token 上限拒绝、独立取消、硬截止、原清理证明及不存在设备不退 CPU。只有原 STOPPED/同步清理及原绑定回执用于测试断言；进程退出本身不是释放证明。 |
| NF-041 | 证据原样保留 | 每次 attempt 使用新编号、原始 JSONL 与摘要；失败后的源码/worker/包清单在修改前按原摘要归档。不能覆盖失败、生成替代媒体或把原失败写成成功。 |
| NF-042 | 当前报告 | 常规回归、合成测试、真实模型测试、检查命令、前端实际运行及完整业务验收分别登记。成功结果只适用于实际机器、固定输入及选定路径。 |
| NF-043 | 浏览器与进程 | 验证退出后关闭自有 worker、数据库、浏览器上下文、预览进程和不用的标签页；不终止其他应用或用户服务。 |
| NF-044 | 性能门槛 | 启动/首 token/持续速率/峰值内存/显存/取消延迟及至少 30 次重复实测尚需独立验收。一次小模型通过不能证明业务模型性能、长期可靠性、完整 GPU 调度或资源竞争安全。 |

## 6. 当前未收口范围

| 编号 | 事项 | 强制约束 |
| --- | --- | --- |
| NF-045 | 生产接线 | 真实长驻宿主、受控升级器、生产能力预检、Responses 尾事件预算、授权内容通道和结果 ACK 仍需完成；不能因研究测试开放 CLI 内容执行。 |
| NF-046 | 模型能力 | 本轮仅单文件文本 GGUF。工具调用、多模态、媒体、ComfyUI 完整内核和有限节点、LM Studio 兼容及 LMA 手机端均有各自门槛，不能由本测试推定可用。 |
| NF-047 | 不动范围 | 已发布 22 份 SQL 保持原字节；DEV-55 部署、HTTPS、运维及可观测性延后。NB04、小时/M token 和具体手机型号约束保持。 |
| NF-048 | 进度 | 全部 56 任务、222 子项、80 业务及 96 设计组合继续逐项登记；局部通过不自动收口。历史时序/存储问题和 R020 原媒体缺失记录继续保留。 |

## 7. 源码使用顺序

从项目根目录显式构建及准备研究包：

```powershell
python -B -X utf8 dev/tools/构建原生LLM内核.py --fetch --fixture
python -B -X utf8 dev/tools/准备真实LLM验证包.py --fetch --model-mirror hf-mirror
```

准备工具只下载固定输入、校验及生成包清单。它不会产生市场资格。正式安装应使用独立版本目录，本轮 runtime 研究包用于可复现实测。

可信本机装配程序使用下列顺序；expectedPackageSHA 和 approvedEnvironment 均来自本机批准，不得从调用方任务读取：

```go
selection := llamaworker.OwnerSelection{
    Capability: originalCapability, Resource: originalResource,
    GPUDevice: "CUDA0", GPULayers: 999,
    ContextTokens: 2048, BatchTokens: 128, Threads: 2,
}
config, err := llamaworker.LoadPackage(ctx, runtimeRoot,
    llamaworker.FilePin{Path: packagePath, SHA256: expectedPackageSHA},
    selection, approvedEnvironment)
// 检查失败直接返回。包校验成功后，仍须取得原资源准备权。
adapter, err := llamaworker.Open(preparationContext, config)
// 将匹配批准的 adapter 装入原 HostKernel，包裹 RC 结果仓，再使用原 ticket。
// LocalInfo 只用于本机说明；它不能代替资源/执行/清理证明。
```

检查命令的标准输入结构如下；这些占位值必须由本机候选方案及独立批准替换。检查不启动推理：

```json
{
  "package": "E:/.../runtime/.../package.json",
  "package_sha256": "<independently approved exact SHA-256>",
  "environment": "<approved PackageEnvironment digest>",
  "selection": {
    "capability": "<original capability>",
    "resource": "<original physical resource>",
    "gpu_device": "CUDA0",
    "context_tokens": 2048,
    "batch_tokens": 128,
    "threads": 2,
    "gpu_layers": 999
  }
}
```

真实模型验证使用独立 attempt 编号。已存在的编号会拒绝覆盖；下一次测试改为新编号，修改源码前先归档原记录：

```powershell
python -B -X utf8 dev/tools/核验真实LLM内核.py --attempt 1
python -B -X utf8 dev/tools/归档真实LLM验证.py --attempt 1
```

首轮验证失败不会变成可用资格。最终通过情况见 [R023 验证报告](../../dev/tests/evidence/R023-验证报告.md)；完整业务验收与局部测试保持分别记录。

## 8. 官方来源

- [llama.cpp v0.8.0](https://github.com/ggml-org/llama.cpp/releases/tag/v0.8.0) 与 [对应 b11960 构建](https://github.com/ggml-org/llama.cpp/releases/tag/b11960)。
- [固定 llama.h](https://github.com/ggml-org/llama.cpp/blob/8642541b255bfb5dc39cd351a178be09013a55d6/include/llama.h) 与 [ggml-backend.h](https://github.com/ggml-org/llama.cpp/blob/8642541b255bfb5dc39cd351a178be09013a55d6/ggml/include/ggml-backend.h)。
- [Qwen 官方 GGUF 文件页及 SHA-256](https://huggingface.co/Qwen/Qwen2.5-0.5B-Instruct-GGUF/blob/main/qwen2.5-0.5b-instruct-q4_k_m.gguf)；下载固定修订与摘要写入锁文件，main 链接仅作为查证来源。

## R024 当前物理显卡约束

当前私有握手为PIPE_4，环境域为P11_LLAMA_ENV_2；旧版握手仅作历史证据。以[GI-001—032](附录-物理显卡身份探测与启动绑定.md)为当前启动身份约束。R024增加Windows NVIDIA整卡UUID/PCI探测及PIPE_4启动复核；显式GPU选择和环境摘要绑定实际身份，同UUID别名不能绕过共享门闩。只读gpu-discover不加载模型、不准入共享，LocalInfo仅含安全元数据。本机观测不构成远程证明或LP/E2E/性能/usage/ACK/资金资格；Linux/AMD/手机实际接入与完整LM Studio隔离封装仍待。全56任务222子项、80/96完整验收保持开放。

## R025 当前宿主与正文交接约束

以[NH-001—040](附录-原生宿主内核接线与输入驻留绑定.md)为当前装配、输入与结果句柄生命周期约束。R025接通独立批准的原生Host会话、原物理GPU与持久任务、加密结果仓和三协议交付；密文资产摘要保持原义，解密正文另以仅驻内存的原租约/清单/字节凭据校验。宿主拥有worker和结果读取句柄并在关闭时排空、撤权及等待退出，未知原owner不释放。当前限PLAINTEXT/STANDARD；生产资金/usage/ACK/SDK、崩溃恢复、完整LM Studio/ComfyUI/LMA、E2E/LP/性能与80/96完整验收仍待，全56任务222子项保持开放。
