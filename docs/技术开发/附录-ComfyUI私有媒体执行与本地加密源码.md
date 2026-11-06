# ComfyUI 私有媒体执行与本地加密源码

R026 / CODE_FIRST / MW-001—044 / SOURCE_ONLY，2026-11-06。新增 Python 私有执行器、固定配方、四类编码器和 Go 加密文件/私有帧边界源码，均未编译、导入或运行。它们尚未接入原节点的正式 Comfy 启动器、原 RENTAL_SESSION Backend 或公开模型接口。全部56任务/222子项仍待收口，先完成全部生产接线和测试源码，再统一验证。

本文承接 [CE完整环境安装](附录-ComfyUI完整环境安装与原节点登记源码.md)、[原租赁授权](附录-小时租赁授权与累计计量源码细则.md)、[低开销隐私方案](附录-低开销隐私执行完整方案.md) 和 [E2E协议](08-端到端加密协议与密钥实现设计.md)。不新增第二套交易、计量或资金真相。

## 1. 本次代码边界

| 组件 | 源码 | 已写内容 | 尚未完成的接线 |
|---|---|---|---|
| 独立入口 | `dev/src/client/kernels/comfy-private/worker.py` | 固定包内Python、`-I -B -S`、独立控制句柄、内容stdin和协议stdout | 原节点/原物理维护与租赁占位下的可信启动器、进程/管道join |
| 环境与显卡 | `mesh_comfy/environment.py` | 完整树逐文件复核、固定官方提交、CUDA UUID/PCI复核、Torch仅见一张指定显卡 | 完整运行包制作、候选运行持有、真实多卡/驱动/性能证据 |
| 受控图 | `recipe.py`、`private_nodes.py` | 固定节点/连接/输出，有限有类型参数、私有图像输入和四类输出节点 | 实际模型/节点许可与固定可运行图，原合同模型/配方绑定 |
| 编码 | `encoders.py` | PNG、分片MP4、PCM16 WAV、基础几何GLB直接写加密流 | 四类真实生成模型、节点兼容、完整媒体能力与实际质量证据 |
| 本地密文 | `spool.py`、`workspace.py` | RAM钥、分块GCM、可寻址密文、原工作区锚、首建前加密登记 | 原宿主持久预留整个目录/空间，结果保管与真实清理释放 |
| 原生边界 | `dev/src/client/adapters/comfyworker/` | 严格原scope、一次凭据、有限私有帧、64KiB随机读取与撤权 | 真实进程适配器、租内媒体输入/结果交付、公开API映射 |

私有 Python 进程没有执行权签发、买家认证或支付核验功能。INIT来自原宿主，不能由买方、平台响应或UI随意构造。当前正式客户端执行入口保持关闭。

## 2. 完整约束编号

以下“源码”表示已写入本次源文件，不代表验证通过。

| 编号 | 约束及源码做法 | 完成边界 |
|---|---|---|
| MW-001 | 原生Windows/Linux固定Python+Comfy目录；无Docker/WSL/QEMU/模拟器 | 源码，运行包待制作 |
| MW-002 | 官方Comfy固定 `2c75dc1b379a59e0880619d029607af5169a62ef` / v0.44.0 | 沿用CE审阅快照/许可 |
| MW-003 | Python以 `-I -B -S`进入，只加入固定私有包目录，不执行`.pth`/sitecustomize | 入口源码；宿主启动待接 |
| MW-004 | bundle全部文件精确大小/摘要，目录/大小写/软链接/额外文件均拒绝 | 启动前源码复核 |
| MW-005 | 独立签名、许可、本人同意和资源准入由原宿主先行核验 | 本次不产生这些权威 |
| MW-006 | `CUDA_VISIBLE_DEVICES`精确UUID，PCI顺序，驱动UUID/PCI与Torch单卡一致 | 无CPU/其他GPU降档，未实测 |
| MW-007 | Windows驱动只选系统nvcuda；Linux仅列明的原生CUDA驱动位置 | 路径和API源码，兼容待验证 |
| MW-008 | 只读批准模型文件按精确类别/别名/大小/摘要解析，当前仅safetensors | 实际多模型配方待接 |
| MW-009 | 仅逐个加载批准extra/custom模块，拒绝覆盖已有类和未知导出类 | 不运行全部custom/API初始化 |
| MW-010 | 未启动main/server、HTTP、WebSocket或ComfyUI网页 | 仅内部私有管道源码 |
| MW-011 | immutable配方最多128节点，每节点最多64输入，一个指定私有sink | 买方只填参数槽 |
| MW-012 | 最多32个STRING/INTEGER/NUMBER/ENUM/ASSET参数，严格UTF-8、数值和枚举 | 未列字段拒绝 |
| MW-013 | 路径/模型/设备/类名和连接保持原配方，ASSET仅绑定私有输入槽 | 不授权任意买方图或安装 |
| MW-014 | 每调用复制批准图，结束释放买方参数/图、缓存、历史和私有上下文 | 不承诺法证级RAM清除 |
| MW-015 | 图像输入用可寻址密文reader，限PNG/JPEG、单帧、最多1M像素 | WebP需read-all，当前拒绝 |
| MW-016 | PNG单张最多1M像素/2048边，16行量化、无prompt/EXIF元数据 | 编码源码，非生图验收 |
| MW-017 | MP4单次最多240帧/30秒，1—60fps、偶数尺寸、1M像素 | 原视频tensor由真实图产生 |
| MW-018 | PyAV固定libx264/yuv420p/单编码线程，分片MP4，逐帧写GCM | 不调用ffmpeg子进程或换编码 |
| MW-019 | WAV为单batch、1/2声道、8—96kHz、最多120秒PCM16 | 已知nframes避免回写明文头 |
| MW-020 | GLB只含三角几何，最多50万顶点/100万面，分块位置与索引 | 纹理/动画/完整3D能力待扩展 |
| MW-021 | 单个输入/输出最多64MiB，最多4输入+1输出，累计预留明文限128MiB | 原宿主还须预留物理空间 |
| MW-022 | 每个文件新32字节RAM钥+8字节随机nonce前缀，GCM标签16字节 | 原钥丢失保持不可用，不重算 |
| MW-023 | 每记录nonce是前缀+LE32序号，AAD为固定48字节头+9字节记录头 | 绑定原scope/类型/长度/顺序 |
| MW-024 | 数据记录64KiB，只有最后数据块允许较短；一次认证END固定48字节 | 头/尾/截断/追加均检查 |
| MW-025 | 本地密文名由随机数生成，原scope以SHA256绑定session/invocation/本地代次/环境/配方 | 非公共内容摘要 |
| MW-026 | 原宿主先持久预留工作区；worker校验原32字节ownership.anchor | 工作区建立权不来自买方 |
| MW-027 | 随机payload文件创建前，独立GCM封装reservation并fsync | Windows原宿主持久性接线待完成 |
| MW-028 | 无明文媒体临时文件、RAM盘或第二份完整视频；编码直接首写密文 | 真实OS低权限/原生库行为待验 |
| MW-029 | 一个有限随机reader，缓存至多一块；拒绝无界read-all，顺序消费者可重开 | 调用者自有返回缓冲另计 |
| MW-030 | Python关闭逐个回收拥有的reader/AEAD/可变钥引用，保留原密文及登记 | 目录删除只能由原宿主完成 |
| MW-031 | Go receipt只接受独立期望scope/kind，严格JSON、规范base64url/文件名/计数 | receipt不是准入凭证 |
| MW-032 | Go receipt和Frame共享一次消费状态，复制别名也不能重用 | 失败或Close同样撤权 |
| MW-033 | Go在原os.Root内只读已存在文件，复核身份/大小/头/END和每块GCM | 不创建目录或推测丢失钥 |
| MW-034 | Go reader复制别名共锁/共撤权，Close清理缓存和自有钥引用，格式输出脱敏 | 不把摘要/钥传给提供方UI |
| MW-035 | 内容、取消独立管道和写锁；Go/Python分配前检查frame长度 | 控制不等待大正文写完 |
| MW-036 | 原调用id取消墓碑，finish后不能重入；其他id取消不停止当前调用 | 单session串行执行 |
| MW-037 | 会话和子调用双单调截止，watchdog独立于输入流和SQL | 墙钟回拨不延长已收期限 |
| MW-038 | native interrupt绑定取消竞态；Comfy的reset(False)不能清除已取消状态 | 中途失败不伪造清理 |
| MW-039 | CacheType.NONE、无资产历史库、无preview/UI事件和原始异常转发 | 保留阶段/有限常量错误 |
| MW-040 | Python审计拒绝常规socket/子进程/明文文件写，密文写仅受控短临界区允许 | 不是对恶意native代码的沙箱 |
| MW-041 | RESULT只发私有宿主，包含原scope结果钥；随后实际同步/清图缓存/关输入钥才STOPPED | 不是买方ACK/可信usage |
| MW-042 | CLOSED还要求原宿主真实process/pipe join；缺证据保留UNKNOWN和父owner | 子任务不释放RENTAL父占位 |
| MW-043 | 本地格式不是BINARY_STREAM_1、网络E2E、提供方不可见或TEE证明 | 公共隐私标签保持关闭 |
| MW-044 | 测试源码/双语说明/全56和222待办/普通Git同步；冻结DDL字节不变 | 全量代码后统一验证 |

## 3. 本机程序及选择权

提供方正式客户端将由原节点宿主启动独立Python进程，加载本人同意的完整Comfy环境、模型和有限节点。提供方决定共享哪张GPU、哪些环境/模型/配方、功率/时间/空间限制以及停止共享；买方从已售合同选定的配方填写有限参数。平台负责同单路由与授权，不能替设备所有者自动安装节点或改变已售模型。

提供方UI只展示设备型号/显卡、环境版本、批准节点与许可、模型能力、阶段/占用/收益。用户可以知道机器运行哪套程序和资源限制；买方prompt、图、素材、产物和钥只流过私有内容边界。独立低权限服务及完整桌面UI仍待接入，因此当前不能宣传“设备所有者无法查看内容”。

当前worker要求指定NVIDIA CUDA整卡，拒绝其他ordinal/CPU回退；它没有改变同物理GPU上LM Studio、原生LLM、ComfyUI和三种模式共用门闩的要求。手机LMA执行路线保持原有固定APK/JNI约束，不复用桌面Python包。

## 4. 内部管道格式

内部消息为 `uint32_le(length) || uint8(kind) || payload`；length包含kind。内容和控制必须由原宿主分别拥有，不能共享一个可能被大正文阻塞的互斥锁。文件句柄/FD不是内容钥，argv只允许控制FD/句柄。

| 方向 | kind | payload/上限 |
|---|---|---|
| 内容输入 | INIT=1 | 精确session/environment，最多32MiB；只能由可信原宿主构建 |
| 内容输入 | INVOKE=2 | 原scope、硬截止、原工作区锚/私有journal钥、参数、资产声明；最多2MiB |
| 资产输入 | BEGIN=3 / DATA=4 / END=5 | 首尾4096字节JSON；DATA每块最多65536字节；原顺序/长度/明文摘要联核 |
| 内容输入 | SHUTDOWN=6 | 原session/local_generation精确匹配，最多4096字节 |
| 独立控制 | CANCEL=201 / STOP_SESSION=202 | 原session/local_generation及可选原invocation，最多4096字节 |
| 私有输出 | READY=101 / ACCEPTED=102 / STATE=103 / RESULT=104 / STOPPED=105 / CLOSED=106 / ERROR=107 | 每个payload最多65536字节，只含有限元数据；RESULT私有含输出钥 |

`READY.qualification=NOT_GRANTED`。未知kind、重复JSON字段、NaN/Infinity、非法UTF-8/孤立代理项、额外字段和越界长度失败关闭。输出的`ERROR.code`仅有限常量，不附异常栈/原图/正文/本机路径。

这不是公开API。所有买方请求仍必须经OpenAI/Anthropic兼容入口和已确认的原卖家/合同/模式/预算；把公开请求安全映射为批准配方与租内调用的正式适配器尚未完成。本次没有新增可用监听端口，也没有借普通T1 job/attempt伪装租赁。

## 5. 本地密文格式的精确字节

scope只有五项ASCII值，规范JSON按键排序，无额外空格：

```json
{"environment_digest":"64hex","invocation_id":"original_id","local_generation":"19","recipe_digest":"64hex","session_id":"original_id"}
```

本地代次必须是规范非零uint64十进制字符串。文件头48字节：8字节 `P11CS01\0`、8字节随机前缀、32字节原scope摘要。每记录头为 `<IBI`，即4字节序号、1字节类型、4字节明文长度；后跟GCM密文及16字节tag。nonce为前缀+LE32序号，AAD为完整文件头+该记录头。

DATA类型1，明文最多65536字节，满块除末块外必须保持整齐。END类型2，明文是 **`<QQ32s>` = 48字节**，保存数据记录数、明文总字节和明文SHA256；END自身经过GCM认证。完整文件大小为：

```text
48 + plaintext_bytes + data_records * 25 + 73
```

这里73是END的9字节记录头+48字节正文+16字节tag。Go/Python均按这个格式编写；独立fixture测试源码专门避免用同一常量掩盖长度不一致。每文件钥随机且RAM驻留；密文文件与原登记在失钥/崩溃后保持不可用，不以重跑补造同一结果。

这层GCM仅用于本机素材/产物的有限随机访问，不替代网络libsodium secretstream的顺序认证与TAG_FINAL，不将分块GCM改称平台盲转E2E。当前代码没有打通买方网络E2E或严格增强档。

## 6. 执行、取消及真实所有权

原宿主必须先完成当前本人批准、完整环境信任/撤销、原合同/身份/资金/预算、签名grant、双围栏、物理session owner和hard deadline核验；然后持久预留整个随机工作区及足够物理空间，再创建anchor并启动进程。这些权威不能由INIT自身或Python READY产生。

worker每次串行接收原invocation，先绑定单调截止/取消墓碑，再验证参数和原资产。资产在写入时立即GCM封装，逐块验证长度/原明文摘要；输入中断造成流边界不明时关闭整个原进程会话。随后批准图执行、编码到密文，实际CUDA同步后向私有宿主交付结果钥。

清理时先撤销该invocation未来启动权，释放图/缓存/上下文，执行实际CUDA同步，关闭拥有的输入/工作区钥，再发送STOPPED。同步/编码/管道出错不制造清理成功。STOPPED只描述这个内部进程的子调用事实；原宿主还必须核验结果保管、原inbox、原资源围栏与所有实际句柄。结束会话还需等待真实进程/内容管道/控制管道join，才能产生原资源清理证明。kill、超时、取消ACK和CLOSED单独都不能释放UNKNOWN父owner或预算。

输出结果钥在私有RESULT交给原宿主后不再属于worker长期保管；其后原生结果对象的保留期、买方下载权限/完整性、撤权和删除必须另行绑定原合同。显卡释放不等于结果自动删除，结果读取权也不授予新执行权。

## 7. 内存与隐私边界

加密写缓冲和随机解密缓存按64KiB分块；Go端固定一次缓存，Python reader拒绝无界read-all。完整文件清单仅在准备/复核时使用，完成后释放，不在每帧或每token重复hash。视频编码逐帧将必要像素交给libx264，不额外构建整段视频副本；PNG量化按16行，WAV按4096采样块，GLB按4096行。模型与Comfy原生tensor仍有其正常显存/RAM需求，视频模型本身可能一次产生完整输出tensor；本实现没有承诺把模型固有内存变成流式。

默认图像上限1M像素，视频240帧/30秒，WAV120秒，几何GLB无纹理/动画。这些是当前有限profile，扩展必须有新批准环境/配方和明确预算，不静默缩图、缩时长、换节点、换模型或改计价单位。提供方时长仍按小时，LLM输入/输出分别M token；媒体在原STANDARD租赁期内执行，不按文件数量另收费。

Python审计钩子只减少批准代码误开网络、子进程、缓存/明文写文件等出口；它不能隔离恶意native扩展，也不是OS低权限服务或保密硬件。Python字符串、Torch/native编码器和AES内部钥调度无法承诺法证级RAM擦除。当前目标是减少普通UI/日志/本地端口/文件读取出口，尚无LP、E2E、严格增强或性能验收结论。

## 8. 统一验证前仍需补齐

| 待办 | 必须补齐的实际生产代码/依赖 |
|---|---|
| 原环境运行持有 | 从已验证原候选导出可信完整文件/模型/配方清单；执行期间阻止删除/切换；本人批准和撤销独立复核 |
| 完整可运行包 | 固定Python/Torch/CUDA/Pillow/PyAV/cryptography/numpy/节点/工具及许可证，实际包签发/分发；不是安装器微小fixture |
| 正式原生适配器 | 原OS锁/主密钥/低权限身份、最小环境、私有句柄权限、独立读写、进程/线程/管道join和UNKNOWN保留 |
| 原RENTAL_SESSION接线 | 本人媒体策略、一次认证输入、原租内inbox/双围栏/watchdog、无假T1、停止/恢复与平台原清理回执 |
| 四类真实配方 | 经许可模型、可运行节点组合、输入资产类型及限额、3D Mesh适配和能力逐项资格 |
| 协议/对象交付 | OpenAI/Anthropic媒体入口、资产权限、原结果保管/流式交付/撤权/最终ACK和可信计量 |
| 隐私及真实验收 | 本机独立低权限、E2E双端、攻击测试、真实Windows/Linux GPU、手机和全部组合性能/业务证据 |

未来批次包含本次Go/Python密码格式、边界、真实编解码器（合成CPU素材）测试源码；它们不替代实际生成模型或GPU/手机资格。当前不执行任何测试、编译器、数据库迁移、APK/浏览器/模型/GPU，不重写冻结SQL。文档索引见组件README；剩余全部222子项见 `dev/全量代码优先实施清单.md`。

## 9. 固定官方来源

- [ComfyUI v0.44.0](https://github.com/Comfy-Org/ComfyUI/releases/tag/v0.44.0) 与 [固定提交](https://github.com/Comfy-Org/ComfyUI/tree/2c75dc1b379a59e0880619d029607af5169a62ef)，对应CE源快照与原文GPL许可。
- [NVIDIA CUDA Driver device API](https://docs.nvidia.com/cuda/cuda-driver-api/cuda_driver_api/group__CUDA__DEVICE.html)，用于UUID/PCI只读身份复核；源码调用不构成实机兼容证明。

ComfyUI及第三方依赖保留各自许可；本项目新增桥接代码不改变上游许可证，也不把未来完整Comfy运行包描述为仅适用仓库顶层许可。全部正式分发包仍需逐文件源/许可清单。

## R026 CH-001—036 原环境与宿主源码继续 / Original environment and host source

R026新增[CH-001—036](附录-ComfyUI原环境持有与本机进程接线源码.md)：原Comfy环境运行持有、固定原生进程/三管道、PIPE_2准备/一次原授权ACK/单调截止和原ProviderRental准备接线。29项新测试定义未编译/运行；CH-033—036正式租内媒体Backend、认证工作区/恢复清理、公开交付、完整包/低权限/E2E、独立ready/计量/付费续租/平台释放仍待。全56任务/222子项开放，无SQL变更、迁移或真实执行；完整代码后统一验证，源码检查点不是开发轮完成。
