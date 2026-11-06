# ComfyUI 完整环境安装与原节点登记源码

R026 / CODE_FIRST / CE-001—036 / SOURCE_ONLY，2026-11-06。本文覆盖可替换完整环境、有限白名单节点安装和本机版本登记的新增源码。所有编译、测试、数据库迁移、Python/ComfyUI/显卡运行均未开展；完整媒体 worker、租内调用和隐私链路仍须继续开发。56任务/222子项继续全部开放，不增加80项主需求编号。

## 1. 当前实现与完整目标的关系

| 范围 | 本次源码位置 | 当前边界 |
|---|---|---|
| 固定依赖及本人明确选择 | `dev/src/client/comfyenv/catalog.go` | 独立公钥与撤销快照，逐文件完整清单，本人另行提供的目录摘要/许可/节点选择/空间上限 |
| 预构建包安装及完整目录复核 | `archive.go`、`files.go` | 只读本机已下载ZIP，不执行安装命令、不访问网络、不导入节点 |
| 原节点加密登记与版本选择 | `registry.go`、`node/kernels.go`、`node/store.go` | 原OS进程锁、系统密钥派生的独立AEAD、原设备/卖家/安装绑定和原物理维护占位 |
| 本机管理入口 | `cmd/p11-node/comfy.go` | 初始化、查看、安装、复核、删除；全部明确 `execution_enabled=false / qualification=NOT_GRANTED` |
| 真正执行媒体 | 后续完整 ComfyUI worker、媒体适配和 RENTAL_SESSION 接线 | 本次没有启动代码、实际媒体运行、首写加密、平台计量或清理释放的完成结论 |

官方源码审阅固定至 [ComfyUI v0.44.0](https://github.com/Comfy-Org/ComfyUI/releases/tag/v0.44.0)，提交 `2c75dc1b379a59e0880619d029607af5169a62ef`。具体文件摘要见 `dev/src/client/comfyenv/upstream.lock.json`，GPLv3原文保存在 `dev/licenses/`。这是官方源码审阅快照，**尚不是已经制作完成的Python/Torch/编解码器/节点安装包**，也不能拿来发放设备开售资格。后续更换上游必须生成新的完整环境和独立许可记录，不覆盖原环境。

## 2. 完整编号与需求约束

| 编号 | 开发规则 | 当前源码与拒绝条件 |
|---|---|---|
| CE-001 | 只使用原生 Windows/Linux 环境 | 目录限定 windows/linux、amd64；安装/切换还核对本机OS及架构，不引入Docker/WSL/QEMU |
| CE-002 | 固定官方来源 | 记录官方release、40位提交、审阅文件SHA256和原文许可；固定版本不是实际运行证据 |
| CE-003 | 清单签发与信任独立 | Ed25519签名验证使用本机预置issuer/key ID/pins；不从网络清单接受新公钥 |
| CE-004 | 精确签名域 | `P11_COMFY_ENVIRONMENT_CATALOG_1 + LF + payload原始JSON字节`；签名为规范无填充base64url |
| CE-005 | 精确目录身份 | CatalogDigest为payload原始字节SHA256；字段排版变化也需要对应签名和新的本人批准 |
| CE-006 | 有限信任窗口 | issued/expires为规范UTC RFC3339Nano，拒绝未生效/到期，清单有效期上限366天；独立撤销摘要优先拒绝 |
| CE-007 | 本人批准独立 | Consent必须由可信本地UI/配置明确提供，不能复制清单内容自动同意或接受买方传参作为本人批准 |
| CE-008 | 许可与容量明示 | 逐组件接受、精确节点选择、OS/架构和展开字节上限；漏许可、额外同意、不明组件均拒绝 |
| CE-009 | 完整固定软件环境 | Python、ComfyUI核心、私有桥接为必需角色；所有依赖/工具/节点的全部文件、版本、大小、摘要、许可由清单覆盖 |
| CE-010 | 角色不能互相覆盖 | Python/依赖限python，核心限comfy，桥接限bridge，工具限tools；核心不得偷带custom_nodes |
| CE-011 | 有限节点集合 | 节点包有固定ID/revision/root/class/dependencies；拒绝未知包、类覆盖、重复class和跨包路径 |
| CE-012 | 明确依赖闭包 | 可选Python依赖只有被本人选中节点引用时纳入；所有选中组件的许可文件必须实际包含 |
| CE-013 | 不执行安装脚本 | 不运行pip/git/Manager/setup，节点包拒绝install.py、prestartup_script.py；买方任务不能要求装节点 |
| CE-014 | 模型另行批准 | 模型权重不由本清单下载或写入共享环境；既有明确模型引用/权重摘要/GPU资格仍必须独立核验 |
| CE-015 | 便携且唯一的文件路径 | 拒绝绝对路径、反斜杠、点跳转、ADS、Windows设备名、大小写别名及文件/目录冲突 |
| CE-016 | 解压前先限中央目录 | 单ZIP最多100000条、中央目录最多32MiB、单磁盘；ZIP64只接受有限固定记录，避免先分配无界条目 |
| CE-017 | 原本机包逐字节核验 | 全档大小/SHA、每个文件大小/SHA、目录祖先集合、类型均匹配，拒绝额外文件和符号链接 |
| CE-018 | 有界解压 | 使用64KiB缓冲和限定长度，逐文件Sync后关闭；不将整个运行包读入内存，不执行解压后内容 |
| CE-019 | 先登记后创建 | 随机原stage ID与INSTALLING先写入原加密登记，再创建对应暂存目录；失败不另发身份 |
| CE-020 | 分阶段提交 | 完整展开/私有元数据复核→COMMITTING持久化→移至精确digest目录→再次复核→VERIFIED |
| CE-021 | 失败保留原状态 | 失败候选QUARANTINED并保留原stage/final；不自动回滚、删目录、重新安装或恢复开售 |
| CE-022 | 原节点唯一登记 | KernelID先写入原节点AEAD；派生独立登记钥，绑定installation/device/seller/control origin/KernelID |
| CE-023 | 缺失与未知关闭 | 锚定后丢失/损坏/异根journal不自动创建；Commit未知使本Registry中毒，关闭新操作 |
| CE-024 | 重开不接管目录 | 重启将INSTALLING/COMMITTING隔离；不从磁盘存在推断安装完成，不收养未知环境或恢复执行授权 |
| CE-025 | 导入前再核完整树 | `VerifyInstalled`/切换重复检查未知目录、额外文件、摘要、元数据；漂移隔离，没有版本降级回退 |
| CE-026 | 切换共用物理门闩 | 只接受原执行账本的MAINTENANCE/PREPARING占位；LOCAL/JOB/RENTAL、旧代次、STOPPING及恢复态拒绝 |
| CE-027 | 关键提交保持占位 | 复核后在原账本串行门闩内保存选择，禁止检查后释放再提交；回调不启动进程、不递归进账本 |
| CE-028 | 本机选择不发资格 | Active只记resource/local_generation/maintenance_owner/previous/current；不改价格、资金、双围栏或平台能力 |
| CE-029 | 显式回滚同样批准 | 只有仍有效、完整复核通过的旧Plan可用同一切换流程选回；不自动降级、不回滚身份/资金 |
| CE-030 | 删除先墓碑 | TOMBSTONED先持久化，才删除原stage/digest目录；当前选中环境禁止删除，原DELETED记录计入容量 |
| CE-031 | 删除不越出原根 | 删除前核对解析后的完整路径、固定三级结构、64位原ID和无符号链接；保留其他项目/用户文件 |
| CE-032 | 关闭等待实际IO结束 | 停止接收新操作、取消已进入操作、等待其实际返回后清钥；父节点随后才清理原OS锁、根和主密钥 |
| CE-033 | 本机CLI不含隐式执行 | comfy-init/status/install/verify/delete，独立绝对路径trust/consent文件；无run/listen/auto-enable |
| CE-034 | 对外只返安全元数据 | 错误为固定描述，不回显路径/URL/参数/正文；成功统一返回本机操作profile和NOT_GRANTED |
| CE-035 | 安装成本不进入逐帧路径 | 完整散列在安装/选择/后续导入前进行，不在每token或每帧散列全部环境；真实开销未测量 |
| CE-036 | 源码、验证、验收分开 | 本文测试只编写定义，未运行；不生成截图/GIF或合格证明冒充实际ComfyUI/显卡/隐私验收 |

## 3. 清单、同意与磁盘结构

签名信封包含 `payload / key_id / signature`。payload包括 `profile_id / issuer_id / allowlist_revision / issued_at / expires_at / os / architecture / comfy_commit / python_executable / private_bridge / core_class_types / components / artifacts / node_packs`。组件role限 `PYTHON / COMFY_CORE / PRIVATE_BRIDGE / PYTHON_DEPENDENCY / NODE_PACK / MEDIA_TOOL`，核心的exact_version必须等于comfy_commit。Python/桥接和核心关键文件必须真实列入，许可文件属于其组件。

每个Artifact含固定公共来源地址、档案SHA256/字节数及文件列表。来源地址只作经签名的追溯资料，代码不会访问它；档案通过 `artifact ID → 原节点根下的本机相对ZIP路径` 显式提交。每文件含Path/SHA256/Bytes/Executable。目录总上限32MiB JSON、200000文件、512档案、64节点包、200GiB展开空间；本人同意还可设更低磁盘上限。目录大时需要拆分固定档案。

Consent是独立本地资料：`profile_id=P11_LOCAL_COMFY_ENVIRONMENT_CONSENT_1 / catalog_digest / os / architecture / selected_node_packs / accepted_component_ids / maximum_unpacked_bytes`。选中ID与许可ID先排序，PlanDigest是 `SHA256(CatalogProfile + LF + CatalogDigest + LF + 规范Consent JSON)`。调用者不能变更Plan内部切片；所有读取器返回副本。

原节点目录中保留：

```text
state.enc                         原节点状态，追加可选KernelID，无DDL更改
kernel-registry.enc               独立派生钥AES-256-GCM，随机nonce，绑定原根身份
downloads/<本机原档案>.zip        提供方另行准备的固定包，不自动下载
kernel-bundles/stages/<原stage>/  已先登记的暂存安装
kernel-bundles/environments/<PlanDigest>/
    python/                       固定Python及全部已选择依赖
    comfy/                        固定核心，custom_nodes只含已选择节点
    bridge/                       固定私有桥接文件，不由安装器运行
    tools/                        有明确许可的必要工具，可按目录实际选择
    .mesh/catalog.json            完整原签名信封
    .mesh/consent.json            本人同意的规范副本
```

`.mesh`是非秘密软件元数据，文件本身不宣称密文；其精确字节被Plan身份、签名和原加密登记共同约束。登记不保存prompt、生成结果或Bearer。文件安装同步不构成断电持久性实测结论；重开、导入、资格生产均须再核实际文件。

## 4. 本机使用逻辑与接线约束

1. 原节点必须已登记到具体用户/设备，并保持自己的OS进程锁和私有根。提供方显式初始化一次，不能在任务收到时自动初始化。
2. 从独立可信配置准备签发者/公钥/撤销目录文件，另外准备本人同意文件。两者均为本机显式绝对路径；其内容不能取自买方任务或清单内的“推荐同意”。
3. 提供方准备完整固定ZIP，并将其放在原节点根下。通过stdin提交signed_catalog和local_archives映射；CLI按当前同意、信任和实际OS安装新环境。
4. `comfy-status`只展示候选/选择安全元数据；`comfy-verify`要求重新提供当前独立信任与本人同意，复核全树。重开不恢复任何执行许可。
5. 真正版本切换由可信本机控制层取得同原物理资源的维护占位，再调用 `Store.ActivateComfy`。它复核原环境并保存选择，保持原维护占位；**调用方还要完成原监督与清理协议，不自动标FREE**。
6. 后续完整worker只能在独立本机批准、原平台manifest/资格、原RENTAL_SESSION及当前grant都成立时运行。不得把本机VERIFIED当QUALIFIED，不能借T1/owner_attempt_id代替租赁会话。
7. 卸载仅允许精确原候选；当前选择禁止删除。失败或中断保留原编号，重试删除沿同一墓碑进行，不清历史绕过容量。

接口示意仅作为后续统一编译后的使用说明，本代码阶段不执行：

```text
p11-node comfy-init   --state <原节点绝对路径>
p11-node comfy-status --state <原节点绝对路径>
p11-node comfy-install --state <原节点绝对路径> --trust <本机信任文件绝对路径> --consent <本人同意文件绝对路径>
    stdin: {"signed_catalog": <原签名信封对象>, "local_archives": {<artifact_id>: <原根下ZIP相对路径>}}
p11-node comfy-verify --state <原节点绝对路径> --trust <当前信任文件绝对路径> --consent <当前同意文件绝对路径> --digest <精确PlanDigest>
p11-node comfy-delete --state <原节点绝对路径> --digest <精确PlanDigest>
```

## 5. 仍须完整实现的运行部分

官方 [nodes.py](https://github.com/Comfy-Org/ComfyUI/blob/2c75dc1b379a59e0880619d029607af5169a62ef/nodes.py) 默认初始化可引入完整扩展/API/自定义节点，不能把默认全量初始化直接用作隐私入口。后续桥接必须在完整环境复核后，只导入已批准模块并核对class集合；关闭Manager、公开内容HTTP/WebSocket、原生历史/预览和任意脚本入口。节点白名单是可信代码边界，不是Python沙箱，不保证对恶意管理员保密。

后续还须完成真正的受限内容/控制管道、原GPU选择、单实例模型复用、取消与原同步停工、PNG/MP4/WAV/GLB格式白名单、首次落盘前分块加密、素材权限/完整性、结果恢复、租内串行inbox及原清理回执。不可直接使用会写原始prompt/元数据/媒体文件的Save节点。图像/视频/音频/3D分别有模型、配方、许可、GPU和性能证据，不能因“装好ComfyUI”全部开售。

买方统一协议和SDK契约、平台原session计量/清理/资金释放、可信本人批准UI及完整上游构建工具仍待接齐。租赁按小时，不按图片数、视频数或租内token重复计费。网络E2E、独立低权限、提供方日常不可见和TEE门槛各自独立；本安装器的AEAD仅保护本机登记状态，不构成内容端对端加密。

## 6. 测试与证据

测试定义编号和文件索引在模块README及 `dev/progress/R026-code-stage.json` 的 `comfy_environment_test_definitions`。覆盖真实签名/AEAD/本机文件/ZIP/SQLite边界的**未来测试源码**，使用微小合成文件，不安装或执行实际Python、ComfyUI或模型。没有通过数量、截图、GIF、性能值、支付或设备资格结论。

旧RN-001—110与67项租赁测试定义、98规划操作/48控制HTTP/9租赁/71平台表均保持原状态。平台SQL001—015双份及原客户端DDL/端侧v2及其provenance均原字节保留；此处只增加节点原AEAD可选字段，不改DDL。先全56/222代码和测试源码，后统一编译、测试、调试；DEV-55延后，DEV-32无真实证明关闭。
