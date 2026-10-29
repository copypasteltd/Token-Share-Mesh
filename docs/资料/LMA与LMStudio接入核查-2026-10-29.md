# LMA与LM Studio：接入依据与能力核查

P11｜2026-10-29｜只读核对，未安装软件/模型、连接设备或修改外部仓库。LMA固定核对提交`5d9e81d59273f2a656763ea3ee3401b2846a18cc`，避免主分支更新改变依据。

| 依据 | 事实与对需求的影响 |
| --- | --- |
| [LMA README](https://github.com/wikigsroom/LMA/blob/5d9e81d59273f2a656763ea3ee3401b2846a18cc/README.md) | 独立Android模型工作台，与LM Studio无隶属关系；公开v0.2.0、多模态开发版和真机验证状态须分开 |
| [LMA API](https://github.com/wikigsroom/LMA/blob/5d9e81d59273f2a656763ea3ee3401b2846a18cc/docs/API.md) | 既有models/chat/SSE与开发异步任务/产物接口分版；loopback/LAN鉴权不等于TLS公网服务，也不等于平台接入 |
| [架构](https://github.com/wikigsroom/LMA/blob/5d9e81d59273f2a656763ea3ee3401b2846a18cc/docs/ARCHITECTURE.md) | 公开版以前台服务/原生CPU引擎/本地存储工作；不能将其描述为已经GPU/NPU加速或保密计算 |
| [验证边界](https://github.com/wikigsroom/LMA/blob/5d9e81d59273f2a656763ea3ee3401b2846a18cc/docs/VALIDATION.md) | 桌面断言、构建/静态检查与Android真实推理/后台/局域网验收分别记录，未取得真机证据 |
| [构建清单](https://github.com/wikigsroom/LMA/blob/5d9e81d59273f2a656763ea3ee3401b2846a18cc/app/build.gradle) | minSdk28/target35/ARM64，当前源码versionName为0.3.0-dev.21，不据此宣称该开发版已正式发布 |
| [Manifest](https://github.com/wikigsroom/LMA/blob/5d9e81d59273f2a656763ea3ee3401b2846a18cc/app/src/main/AndroidManifest.xml) | 原生服务标记specialUse，含联网/通知等权限且允许cleartext；平台需审查真实使用权限及HTTPS出站链路，不盲目复制全部权限 |
| [ApiHost源码](https://github.com/wikigsroom/LMA/blob/5d9e81d59273f2a656763ea3ee3401b2846a18cc/app/src/main/java/com/lma/app/ApiHost.java) | 网关监听/原生执行槽有明确实现；不把打开接口当作模型就绪；共享适配需共用执行门闩 |
| [LMA MIT许可证](https://github.com/wikigsroom/LMA/blob/5d9e81d59273f2a656763ea3ee3401b2846a18cc/LICENSE) | 代码复用保留适用声明；不覆盖所有依赖/权重许可证，不据此推断LM Studio本体再分发权 |
| [LM Studio开发文档](https://lmstudio.ai/docs/developer)及[原生headless](https://lmstudio.ai/docs/developer/core/headless) | 支持本地API及独立llmster守护进程，可设计Windows/Linux原生适配；文档存在不等于平台已接通 |
| [LM Studio桌面条款](https://lmstudio.ai/app-terms)，版本2026-08-23；本日补核 | 内部用途许可不直接覆盖本平台对外收费服务；具体发行物的服务用途及再分发权限须单独确认，llmster适用许可本次未确认，见[商业风险评估](../评审/需求可行性与商业风险评估-2026-10-29.md) |
| [系统需求](https://lmstudio.ai/docs/app/system-requirements)与[认证](https://lmstudio.ai/docs/developer/core/authentication) | OS/架构/后端/版本和认证需按实际环境匹配；不假定任意GPU兼容、也不默认无鉴权开公网 |
| [Android前台服务超时](https://developer.android.com/develop/background-work/services/fgs/timeout) | 不同类型/系统有不同限制；不能把dataSync/mediaProcessing时限直接当specialUse结论，更不能承诺永久后台 |
| [Android热状态回调](https://developer.android.com/reference/android/os/PowerManager.OnThermalStatusChangedListener) | 系统API可用于受支持版本的热压力反馈；未知/不可读状态不得伪造设备安全读数 |

以上资料用于定义准入和待验收需求。商业运行仍需锁定版本、组件/模型许可、物理机/手机实测、平台交易/采购协议、网络安全和隐私评审；没有任何安装或真机通过结果。

## 本轮路线决定补充

以上保留固定源码事实；最新用户决定Android基于LMA源码二开、Windows以lms/llmster/runtime封装为主，已有桌面实例为显式兼容路径。新增官方CLI/内核替换及ComfyUI有限节点证据见[本轮核查](供给端内核封装核查-2026-10-29.md)，不以路线确定替代许可和真机通过。
