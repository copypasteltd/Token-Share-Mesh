# Repository documentation / 仓库文档

Latest R017: [native input and initial lease](../../docs/技术开发/附录-原生输入加密与首租约原子准入.md), [current evidence](../tests/evidence/R017-验证报告.md), [all 56 tasks/222 open steps](../progress/R017-本轮实现与完整待办.md). 原生正文实际校验、明文域AES-GCM与首租约原子接线已实现；真实领取/输入传输、独立信任、实际内核/支付/E2E仍待。

Historical R016: [original-call preparation](../../docs/技术开发/附录-原调用准备与次数预算原子接线.md), [current evidence](../tests/evidence/R016-验证报告.md), [all 56 tasks/222 open steps](../progress/R016-本轮实现与完整待办.md). 原调用/预算/次数原子准备已接通；完整输入/租约/引擎/支付/E2E仍待。

Historical R011: [supervisor specification](../../docs/技术开发/附录-原生执行监督与后端恢复实施细则.md), [native evidence](../tests/evidence/R011-验证报告.md) and [all 56 tasks/222 steps](../progress/R011-本轮实现与完整待办.md). 原生监督、控制停止联动和双端恢复加载修复已纳入本轮；真实引擎/内容加密/支付仍待。

[English README](../../README.md) · [中文 README](../../README.zh-CN.md)

| Guide / 指南 | Purpose / 用途 |
| --- | --- |
| [System design / 系统方案](SYSTEM_DESIGN.md) | C2C boundaries, request paths, privacy and settlement / 交易边界、请求链、隐私和结算 |
| [Usage / 使用指南](USAGE.md) | Current runnable commands and planned buyer/seller journeys / 可运行命令及买卖双方使用逻辑 |
| [Media / 演示媒体](MEDIA.md) | Actual screenshot/recording provenance and reproduction / 真实截图、录屏来源及复现 |
| [Synchronization / 更新同步](SYNCHRONIZATION.md) | Validate, update bilingual material, commit and push / 核验、双语更新、提交和推送 |
| [Contributing / 贡献约定](../../CONTRIBUTING.md) | Development scope, immutable contracts and evidence / 开发边界、不可变契约及证据 |
| [Changelog](../../CHANGELOG.md) / [更新说明](../../CHANGELOG.zh-CN.md) | Same release entries in both languages / 中英文同编号版本记录 |
| [Frontend source guide / 前端源码](../src/frontend/README.md) | Native build commands, dual-client control flow, authorization and lost-reply recovery / 双端构建、授权及原请求恢复 |
| [R010 native inbox/gate / 原生执行元数据](../../docs/技术开发/附录-原生执行收件箱与资源门闩实施细则.md) | Signed Lease, one-shot submit reservation, physical ownership, recovery and cleanup CAS / 签名租约、一次提交、共资源owner、恢复与清理比较更新 |
| [R009 provider UI / 提供方双端控制](../../docs/技术开发/附录-提供方双端设备管理与原请求恢复.md) | Memory-only pairing, policy, drain/revoke and original POST/PATCH recovery / 配对生命周期、共享条件、停止撤销与原请求恢复 |
| [R008 native control / 原生代理与本地恢复](../../docs/技术开发/附录-原生提供方控制代理与本地恢复.md) | OS key adapters, encrypted journal, process locks and original command recovery / 系统钥库、加密日志、进程锁及原命令恢复 |
| [R007 device control / 设备身份控制](../../docs/技术开发/附录-设备配对身份与控制协议实施约束.md) | One-time pairing, request proofs, heartbeat and policy/revocation boundaries / 配对、签名、心跳和资源保留 |
| [R006 frontend constraints / 双端实施细则](../../docs/技术开发/附录-双端交易页面与授权恢复实施约束.md) | NB-1.0 UI, native nonce vs Web PKCE and bounded current scope / 视觉、授权档位和真实边界 |

`README.en.md.in` and `README.zh-CN.md.in` are the maintained templates. Their
links resolve from the repository root after generation. Run
`python -X utf8 dev/tools/构建仓库文档.py` to refresh both public READMEs together.

两份 `.md.in` 为人工维护源，链接以生成后的仓库根为基准。不要只改单语输出；通过生成器同时刷新两种语言及真实进度统计。

[R012 fixed-route relay / 固定路由协议核](../../docs/技术开发/附录-固定路由协议转发核心实施细则.md) · [source / 源码](../src/gateway/core/README.md)：三种原生文本子集及生产端口/SDK/硬件/E2E剩余门槛。


## R015 confirmed-route control / 已确认路由控制

R015 adds confirmed original-order routes and scoped standard-SDK keys. Three owner-session control operations bring the implemented count to 34/93; routes freeze the original seller, listing revision, model, text protocol, price, budget, call ceiling and expiry. ResolveTx selects exactly one key-bound model/profile and rejects ambiguity, revocation or mismatch without seller failover. Forward-only 007 adds five columns and quarantines legacy routes; API startup verifies every embedded migration record. Only PLAINTEXT/STANDARD and the three native text profiles are implemented. Resolution grants no funds or execution; durable invocation/call accounting, real payments, E2E and route-configuration UI remain pending. See [RC-001–022](../../docs/技术开发/附录-已确认订单路由与标准SDK绑定实施细则.md), [R015 source evidence](../../dev/tests/evidence/R015-验证报告.md) and [all 56 tasks / 222 open steps](../../dev/progress/R015-本轮实现与完整待办.md).
