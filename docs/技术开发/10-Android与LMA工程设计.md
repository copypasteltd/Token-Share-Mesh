# Android与LMA工程设计

P11｜技术v1.0｜需求v3.0｜2026-10-29｜F62—F68/F73/F80。参考[锁定LMA核查](../资料/LMA与LMStudio接入核查-2026-10-29.md)，无修改外部仓库、APK构建或真机验证。

低开销完整方案（最新用户确认）：用户允许新增隐私档可嵌入LLM内核，LM Studio继续保留兼容。采用llama.cpp C API原生worker、ComfyUI私有管道入口、LMA内部JNI；买卖端加密/平台盲转，任务数据加密而模型保留原生加载，不使用VM/RAM盘/常驻第二份模型。输入准备和密文交付不额外占GPU锁，控制与大数据通道分离。内存/性能为待实测验收预算，完整链路、二进制协议、媒体、恢复及PE-01—PE-08见[完整实施方案](附录-低开销隐私执行完整方案.md)。当前尚未实现或通过设备/密码/性能验证。

本地隐私目标修订（2026-10-29）：本次只要求提高日常读取/普通工具/简单改包成本。LMA派生端的平台任务走内部引擎/JNI或受限IPC，不经过个人聊天/WebView记录，不开放给原本地/LAN API；内容文件应用内加密，Keystore保护静态钥，通知/logcat/备份/诊断不留正文。提供方保留资源/收益/停止界面，普通手机不改标硬件增强。边界与待执行LP验收见[本地防护规范](附录-本地内容隐私防护与分级验收.md)。

## 1. 工程组成

app：自愿共享/收益/诊断UI；identity：设备配对与凭据；share-service：明确前台生命周期；transport：出站HTTPS/WSS；gateway-lite：协议档位/解密和本地转发；engine-adapter：LMA运行时桥；resource-gate：所有自用/本地API/共享的统一门闩；local-store：任务状态/清理待办；crypto-jni：受审原生库封装。

既有LMA Java服务/EngineHost/ApiHost及WebView基座复用；新增模块候选Kotlin/Java互操作、Coroutine、Room/SQLite，新增原生页面可评估Compose，ARM64、minSdk28参考锁定LMA构建。target35是该参考提交事实，项目目标SDK/AGP/Kotlin/JDK/NDK和分发政策必须在POC-03锁定，不把旧target当永远合规。APK和大模型分离，按用户选择显示下载体积/网络/许可/哈希。

## 2. LMA接入边界

确定在本项目dev/src/client/android内基于LMA源码建立独立派生工程，并改造原生服务、模型/任务及引擎接口；参考提交`5d9e81d59273f2a656763ea3ee3401b2846a18cc`、v0.2公开版与0.3开发线分别记录。任何复用都保留代码及依赖许可，MIT代码不能替代权重/底层引擎许可。

桥接统一describe/probe/prepare/execute/query/cancel/collect/cleanup，实际模型在手机CPU/GPU/NPU路径按实测披露。网络断开PC/云后本地推理证据必须成立；不能用手机转发PC后标Android算力。旧模型/多模态代码存在不证明标准协议或设备能力通过。

## 3. 生命周期

用户点击共享→核权限/时段/充电/Wi-Fi/温度/可用内存→显示前台通知与停止按钮→配对出站→可接新任务。休眠/锁屏/来电/撤权/热/低电/切网触发停止新准入与在途策略；WorkManager只做可延迟清理/恢复，不拿它承诺持续推理。

前台服务类型必须与真实用途一致，检查实际OS/target的启动及超时限制；不能为了保活冒充dataSync，也不把某类型六小时限制套所有类型。[Android官方约束](https://developer.android.com/develop/background-work/services/fgs/timeout)。若服务类型/分发规则不允许持续共享，缩短明确窗口或不发布该能力，不root、不绕杀进程策略。

## 4. 调度与停止

single inference gate覆盖聊天、自用API、平台任务、模型切换。JNI执行不能被Coroutine取消假装停止；cancel向真实引擎发当前任务取消并等待状态/停止证据。引擎只能全局interrupt时，只有专用实例且唯一门闩确认当前任务才用；无法保证不误伤个人任务则不开放相关共享组合。

内存不足/温度未知如实显示，停止新准入；缓存/模型卸载清理在无活动占用时进行。默认充电和非计费网络，流量/功耗显示样本与可信度，不承诺赚多少或恒定速度。手机不提供本版整卡租赁/默认增强。

## 5. 本地安全与恢复

平台凭据、HF下载token、内容钥、身份签名用途分离；Android Keystore可保护静态钥引用，JNI短时内容钥不进普通备份/日志。业务共享目录与相册/聊天/自用模型隔离，不把通讯录/短信/全盘权限当共享前提。

后台被杀重启→先查本地inbox与平台当前token/期限→恢复查询或清理→再接新单。清数据/重装撤旧身份并走本人恢复，不复制历史收益；账在平台原seller，不在APP本地钱包。卸载不保证恶意副本擦除，基础档仍信任设备所有者。

## 6. 真机验证

至少两款不同系统/内存机型：本地文本、冷启动/持续热负载、SDK JSON/SSE、基本加解密JNI、锁屏/Doze/厂商杀进程、拔电/低电、Wi-Fi到流量、权限撤销、升级/重装、停止清理/用量与C2C资金。每项存OS/机型/运行时/模型/协议/模式版；无设备标未执行，禁止模拟器替代。

## 7. 源码二开、内核桥与替换

派生工程记录上游commit/本地补丁/依赖许可证，保留原声明；不修改外部LMA仓库。新增PlatformBridge/GatewayLite/LeaseGuard/LocalInbox/CryptoBridge；模型推理复用本机EngineHost/native受控路径，原本地API和共享调用进入同一InferenceGate。Responses/Messages由轻量转发按已验证档位映射，不宣称上游LMA已全部原生支持。

native可执行组件随受签APK固定，升级核ABI、桥接协议与数据库兼容；不以桌面插件替换机制下载任意so/dex到可写目录执行。模型/数据配方仍分离下载，用户选择组件体积/许可。更新前drain/实际停止；更新后先恢复最高代次/inbox/原任务/待清理，再校新环境摘要及真实能力，不复制收益或复活旧租约。K17—K20见[内核附录](附录-供给端内核封装与替换规范.md)。

## 8. 端对端加密在LMA中的实现边界

在LMA派生工程的既有Java服务/内部JNI增加GatewayLite、同库sealed/secretstream薄封装及独立签名；不另装Go/Node/Python，不新增第二份推理进程/模型。手机是实际执行端，不由PC/云代算。静态native接收/签名材料用Keystore的受保护wrap key等适用机制保存，不虚构所有算法均由Keystore硬件直接执行；流/分段临时钥不入WebView、logcat/备份/普通偏好库。

同一PREPARED/ISSUED与双围栏、签名流头/AAD/END/回执规则见[技术08](08-端到端加密协议与密钥实现设计.md)。标准协议映射在内部，不把平台共享正文回送个人聊天/ApiHost。进程被杀先恢复最高代次、backend ID与密文，钥丢失只重授权或失败，不盲重跑。结果已完整密封给买方可原字节补传，不需要保留正文钥；个人自用与共享共执行门闩。

LOCAL_SEGMENTED_1最多2段解密LRU、总内容缓冲8MiB，密文放应用内部存储，内存/温控A/B按真实手机。APK更新后重验环境/协议/ABI与LP/E2E，不将Keystore/应用私有目录标为ATTESTED_CONFIDENTIAL，所有相关验收NOT_EXECUTED。
