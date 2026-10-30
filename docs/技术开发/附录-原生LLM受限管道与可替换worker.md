# 原生 LLM 受限管道与可替换 worker

版本：R021；对应 DEV-21、23、24、26、38、53、54。本附录延续 NB04、React/Taro 小程序、React/Vite Web、C2C 同卖家原订单与共享物理资源门闩约束。这里只增加原生内核源码与内部适配，不增加公开 HTTP 操作、数据库结构或渠道资格。部署和可观测性仍延后。

## 1. 实施边界与权威

| 编号 | 约束 | 当前源码行为与验收边界 |
| --- | --- | --- |
| NK-001 | 内核角色 | `LLAMA_CPP_NATIVE` 承载可嵌入 llama.cpp C API；LM Studio 的 lms/llmster/runtime 兼容路线继续单独实现，不能用本 worker 冒充其版本或资格。 |
| NK-002 | 固定 ABI | 本版冻结 llama.cpp v0.8.0、提交 `8642541b255bfb5dc39cd351a178be09013a55d6`，`engine_release=llama.cpp-v0.8.0-8642541b`。头文件与工具链摘要见 `dev/src/client/kernels/llama-worker/dependencies.lock.json`。 |
| NK-003 | 资格 | 源码编译、合成管道测试、合成 C ABI DLL 的实际进程测试分别记录。它们不签发真实引擎环境、GPU、LP、E2E、硬件增强或支付资格；设备 CLI 执行入口保持关闭。 |
| NK-004 | 决定运行内容 | 提供方本机批准完整内核包、模型、模板、资源、能力、上下文、批大小、线程及 GPU 配置；订单仅引用已批准标识和摘要，不携带可执行路径、shell、下载地址或任意启动参数。 |
| NK-005 | 接入宿主 | 可信装配程序先恢复节点控制状态与原 owner，再将 `llamaworker.Adapter` 作为 `node.HostKernel.Backend` 注册到原宿主。必须通过 `node.RunSupervised` 管理监督循环，先停循环再关闭父节点；直接构造适配器不取得执行授权。 |
| NK-006 | 当前模式 | 本版适配器只接收已验证的 `P11_NATIVE_TEXT_MANIFEST_1`、`PLAINTEXT/STANDARD` 文本任务。这个限制避免把尚未完成的解密或可信执行链宣称为可用隐私等级；本地内容管道设计继续供后续隐私档复用。 |

官方依据：[固定 llama.h](https://github.com/ggml-org/llama.cpp/blob/8642541b255bfb5dc39cd351a178be09013a55d6/include/llama.h)、[固定 ggml-backend.h](https://github.com/ggml-org/llama.cpp/blob/8642541b255bfb5dc39cd351a178be09013a55d6/ggml/include/ggml-backend.h)、[原生编译器发行](https://github.com/mstorsjo/llvm-mingw/releases/tag/20261020)。本版按实际固定头文件的 `load_mode` 字段实现；旧版 `use_mmap/use_mlock` 字段不能直接套用。头文件注明 abort callback 当前作用于 CPU 执行，所以取消回调不能证明 CUDA 已完成。

## 2. 本机包、启动与所有权

| 编号 | 输入/状态 | 强制规则 |
| --- | --- | --- |
| NK-007 | 本地配置 | `Config` 必须来自独立本机批准，禁止从任务清单反序列化生成。`RuntimeRoot` 及所有路径为绝对路径；文件逐个验 SHA-256、常规文件与路径范围，拒绝符号链接。 |
| NK-008 | 动态库 | `Libraries` 是同一目录完整原生库白名单，1—64 个文件；主 C API 库及后端注册库都须在列表。目录出现额外 `.dll`/`.so` 文件拒绝启动。Windows 加载只使用批准库目录与系统目录，不搜索继承的 PATH。 |
| NK-009 | 模型与模板 | 当前只接收单文件 `.gguf`，拒绝自动分片命名；模板文件为 1—64 字节的已批准内置模板名，摘要对应 recipe。不得接收任意任务 Jinja、模型 URL 或临时脚本。真实权重许可证、引擎发行许可和实际 ABI 对应关系仍需独立核查。 |
| NK-010 | 配置上限 | 上下文 32—131072 token；批大小 1—2048 且不超过上下文；线程 1—64；GPU 层 0—999。请求不能提高这些上限。配置是能力边界，允许上限不等于设备承载能力或性能通过。 |
| NK-011 | 单 GPU 选择 | GPU 层大于 0 必须显式指定本机批准的原生设备名；通过后端 API 查找 GPU，使用单设备列表、`LLAMA_SPLIT_MODE_NONE`、列表内 `main_gpu=0`，拒绝设备不存在后回落。原生设备名到物理资源/UUID 的映射和实际卸载层数仍待真机资格，不允许据此跳过共享门闩。 |
| NK-012 | 暖机 | `Open` 校验包并完成一次模型加载；发生在原生提交短预算之外。可信装配程序必须先取得对应物理资源的独占准备权，不能与其他模式或 worker 同时占用同一显卡。当前 CLI 不触发这一步。 |
| NK-013 | Windows | 定向继承 stdin/stdout 与独立控制句柄，使用显式额外句柄表；不开可见终端；启动后、发送 INIT 前放入本进程持有的 kill-on-close Job Object。只继承必要 SystemRoot，不继承平台密钥、更新密钥、业务凭据或内容密钥。 |
| NK-014 | Linux | 源码使用 stdin/stdout、显式控制 fd 3 与父进程死亡信号；当前只有 Go 交叉编译资格，尚无原生 C++ Linux 运行验收。Linux 原生依赖搜索、权限隔离、GPU 和设备适配必须另验。 |
| NK-015 | 权限等级 | 当前原生进程仍使用调用程序账户。Job Object、匿名管道与文件摘要不替代独立低权限服务账户、服务 SID/ACL、安装授权或 LP 攻击验证。提供方对安装/版本/占用/收益/停止有控制权；生产 UI 不提供 prompt、输出正文、调试终端或解密接口。 |

## 3. 内部二进制协议 P11_LLAMA_PIPE_1

这是本机父子进程的固定字段二进制档位，不是公共 API，也不是网络端 `BINARY_STREAM_1`。此前内部“小 JSON 元数据＋DATA”的通用设计仍可供其他内核使用；本 worker 冻结为下面的类型化字段，减少原生 JSON 解析器与依赖。所有整数为大端；`str` 为 uint32 字节长度＋UTF-8 字节；浮点为 IEEE754 binary64 的大端位表示。帧为 `uint32 frame_length + uint8 type + payload`；长度包括 type、不包括前缀，范围 1—65536。

| 编号 | 类型/方向 | 载荷与顺序 |
| --- | --- | --- |
| NK-016 | INIT=1，父→工作进程 | profile、固定 commit、批准主库绝对路径、注册库绝对路径、模型绝对路径、模板名、设备名，随后 context/batch/threads/gpu_layers 四个 uint32。仅启动时一次，不包含订单正文或密钥。 |
| NK-017 | READY=129，工作进程→父 | profile、固定 commit；加载与 API 解析完成后发送。READY 只表明本机启动过程完成，不是平台资源准入或硬件资格。 |
| NK-018 | BEGIN=2，父→工作进程 | 原本地 backend_id、uint64 硬截止 Unix 毫秒、uint32 正文总字节。总正文 1—524288 字节。接下来必须是 DATA、最后 END；禁止重放同 backend_id。 |
| NK-019 | DATA=3 / END=4 | DATA 为不超过 65535 字节的原始块，累计必须等于 BEGIN 长度；END 必须空载。内部请求正文是固定字段，不能再嵌入本机路径或命令。 |
| NK-020 | 类型化正文 | input_token_limit、max_output_tokens 两个 uint32；temperature、top_p 两个 binary64；uint32 stop_count＋stop 字符串；uint32 message_count＋每项 role/content 字符串。stop≤4 项、每项 1—256 字节，message 1—130 项；文本拒绝 NUL；整体继续受 512KiB 限制。 |
| NK-021 | CANCEL=5，独立控制管道 | 只含原 backend_id，帧载荷≤260 字节。先于 BEGIN 抵达也记录撤销；不能由后续同 ID 启动。取消不排队在正文 DATA 或输出流后面。 |
| NK-022 | STARTED=130 | 原 backend_id；完成输入分词校验与 prompt decode 后发送，仅供本地状态使用，不是计费或平台 ACK。 |
| NK-023 | OUTPUT=131 | 原 backend_id、从 0 连续的 uint32 sequence、1—16384 字节原始 token piece。允许跨 UTF-8 字符边界；未来对外 JSON/SSE 适配必须做合法 UTF-8 拼接，不能直接逐 piece 编码字符串。 |
| NK-024 | DONE=132 | 原 backend_id、uint32 input_count/output_count/reason/clear_marker。marker 必须为 1；reason：1=eog，2=limit，3=stop，4=cancelled，5=rejected，6=kernel_error。成功类输入数必须在批准 input_token_limit 内；全部输出数受批准上限约束。 |
| NK-025 | 协议错误 | 超长、截断、错类型、错 ID/sequence、重复原 ID、无效字段及不完整流失败关闭；进程退出/失联保持 UNKNOWN，不能伪造 DONE、自动重启重提或释放资源。保留类型 133 不承载任意错误正文，本版未将其用作释放凭据。 |

## 4. 标准请求到原生推理

| 编号 | 阶段 | 细则 |
| --- | --- | --- |
| NK-026 | 三文本档位 | 复用网关严格 `Preflight`：Chat Completions、Responses、Anthropic Messages。模型、流式与输出上限必须和冻结清单相同，原始字节长度与 SHA-256 必须相同。工具、图片、外部资产和其他未支持字段提前拒绝。 |
| NK-027 | 内容映射 | Chat 保留 role/content；Responses instructions 作为 system、字符串 input 作为 user，消息数组保留角色；Messages system 放在消息前；仅文本块按原顺序拼接。developer 不静默改为 system，模板不支持时拒绝。 |
| NK-028 | 采样语义 | 未传 temperature/top_p 时此内核档位明确使用 1/1；temperature=0 走 greedy，否则 top_p→temperature→distribution sampler；本档不支持 top_p=0，提前拒绝。stop/stop_sequences 不丢弃，使用有界前缀后缀过滤，完整停止词和其后文字不得先泄露到输出。 |
| NK-029 | 真分词门槛 | 使用批准模板调用 `llama_chat_apply_template`，模板结果≤1MiB；使用同一模型 vocab 的 `llama_tokenize` 实际计数。模板＋special token 后的输入超限，或 input＋最大输出超过 context，必须在创建推理上下文和 decode 前拒绝。字符数/平均 token 比率不能代替。 |
| NK-030 | 推理与内存 | 原生 `llama_decode` 按批准批大小处理输入；模型权重 mmap 并在同一 worker 内复用。每任务单独创建 context/sampler，结束同步后清 KV 并释放 context，不保留跨买家的会话 KV。避免重复载入两份权重；上下文创建成本、真实 GPU 显存和吞吐须另测。 |
| NK-031 | 提交/恢复 | adapter 对原 Work 的 request/resource/local_generation/environment 绑定一次；进程内最多保存 1024 个原 ID 墓碑，满后拒绝新工作，显式轮换前由持久宿主维持一次提交规则。当前不自动重启内核；重启后无法观察的旧 owner 为 UNKNOWN。 |
| NK-032 | 输出预算 | 父进程持续排空管道并把小 piece 合并为最多两个 64KiB 缓冲。授权 sink 用 `Read` 消费并清零已取得切片；缓冲耗尽时显式标记输出不可用并通过独立管道取消，继续排空回执。禁止无界积压或偷偷把丢失输出当成功。 |
| NK-033 | 硬截止与取消 | 本机独立计时器和 worker 截止检查都不能续期；取消先撤销未来启动/采样。CPU abort callback 不是 GPU 立即中断。等待已提交工作 `llama_synchronize` 后，清内存、释放 sampler/context、擦除本任务拥有的正文/token/pending 缓冲，再发 DONE。 |
| NK-034 | 原清理证明 | `Query` 只有收到原 worker 的合法同步清理回执才为 STOPPED。`Cleanup` 再清父进程未消费输出，返回原 request/backend/local_generation/manifest_hash 的三项证明。旧回执、其他 generation、进程退出、取消 ACK 都不能释放新的 owner。Work/manifest即使同时声明另一资源，也必须因不匹配本机批准的Resource而拒绝；先撤销后完成不得恢复成功。 |
| NK-035 | 关闭和故障 | 只终止自己启动且持有句柄的 worker，等待自己的子进程；不扫描杀死系统已有 LM Studio、ComfyUI 或其他用户程序。上下文初始化未返回可同步句柄也必须以 UNKNOWN 结束，禁止合成已清理回执。未获得 native stop/clear 回执即使强制退出，也保留 UNKNOWN/隔离，原宿主继续原号停止恢复。 |
| NK-036 | 日志与留存 | 内容不进入 argv、继承环境、状态 UI 或正文日志；llama/ggml 常规日志使用丢弃回调，stderr 不接交互终端。对自己拥有的字节缓冲做 best effort 清零；不声称 Go GC、分配器、GPU 驱动、崩溃转储或管理员内存取证得到形式化擦除。 |

## 5. 构建、替换和交付门槛

| 编号 | 开发项 | 验收与剩余工作 |
| --- | --- | --- |
| NK-037 | 本机开发构建 | `python -B -X utf8 dev/tools/构建原生LLM内核.py --fetch --fixture` 显式获取摘要锁定的原生编译器/头文件并编译。仅第三方头文件/工具进入忽略的 runtime；不执行第三方安装脚本，不启动模型/GPU/虚拟化。仓库保存 MIT 许可文本，未供应真实动态库或权重。 |
| NK-038 | 管道与 ABI 测试 | 常规 Go 测试验证原 owner、三协议字段、帧上限、阻塞正文取消、输出超限、旧回执与退出 UNKNOWN。`核验原生LLM内核.py` 另以显式 build tag 运行真实 Windows worker＋合成 C API DLL，验证分词前置、stop 过滤、模型只加载一次、native 同步/清理顺序和崩溃隔离；合成 DLL 绝不是模型推理。 |
| NK-039 | 可替换内核 | 上游升级先固定新 commit/头文件/包摘要与许可，重新编译，校核 struct ABI 和导出函数，再做合成及真实运行验收，产出新 environment/engine release，取得本机安装/批准后切换。旧任务始终使用原批准；禁止单独替换 DLL 而沿用旧资格。 |
| NK-040 | 对外闭环 | 原 Host 接口可注册本适配器，但长期运行命令、真实内核环境证据、物理 GPU 映射、真实分词/模型推理、三规范完整结果/SSE、输出接收 ACK、可信 usage、执行续租与渠道时序仍需接线验收。激活前必须把本内核额外输入限制前移到能力预检/准入，并实现拒绝到原停止恢复的明确投影；当前SubmitInput的错误按未知结果保守处理，不可据此开放公共执行。当前 local Result 只是本地计数，不能入成交/账本。 |
| NK-041 | 加密接入 | 后续买家本机/局域网加解密程序在授权端解包，使用同一受限内核边界；平台盲转与本地解密、密钥库和版本冻结测试向量须另完工。匿名管道、宿主静态 AEAD、DPAPI 都不等于已完成 E2E。 |
| NK-042 | 产品验收 | 真机需要覆盖单/多显卡映射、撤销/硬截止与长 GPU kernel、模型替换、进程异常、低权限服务、抓包/普通提权工具、占用/显存/吞吐及 Windows/Linux 边界；手机 LMA 和 ComfyUI 仍按各自白名单完整内核实施。80/96 产品验收和全部56任务/222子项不得用本模块测试清空。 |

源代码入口：`dev/src/client/adapters/llamaworker/`、`dev/src/client/kernels/llama-worker/`。证据入口：`dev/tests/evidence/R021-native-worker-build.json`、`R021-native-capi-fixture-summary.json`、`R021-验证报告.md`。当前是受限原生执行链源码推进，最终交付目标保持 ACTIVE。
