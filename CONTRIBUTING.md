# Contributing / 贡献约定

Read [AGENTS.md](AGENTS.md), [development constraints](dev/docs/02-表格化需求约束.md)
and [synchronization workflow](dev/repository/SYNCHRONIZATION.md) first.

先读工作约定、46条编号约束和更新同步流程。后续代码、测试、工作流与新工具在 dev，运行数据在本项目 runtime。

| Rule / 规则 | Required behavior / 要求 |
| --- | --- |
| C2C | Preserve the actual seller and explicit buyer confirmation / 保留实际卖家及买方确认 |
| Privacy / 隐私 | Never silently downgrade; distinguish practical local protection from hardware attestation / 不降档，本地防护和硬件增强分开 |
| Execution / 执行 | Shared physical-resource lock, two fencing domains, actual stop/cleanup / 共物理门闩、双围栏、实际停算清理 |
| Money / 资金 | Integer fen, original operation references, unknown-state queries / 整数分、原业务号、未知查原号 |
| Contracts / 契约 | Edit maintained sources and regenerate attachments / 改人工源并重建附件 |
| Migrations / 迁移 | New forward versions after publication; never rewrite applied bytes / 发布后只增前向版本 |
| Evidence / 证据 | Keep design, local tests, physical-device/channel and full acceptance separate / 设计、局部测试、实机渠道和整体验收分开 |
| Environment / 环境 | Native processes only; no Docker/WSL/QEMU/VM/Android emulator / 仅原生进程，禁本地虚拟化和模拟器 |
| Scope / 范围 | React/Taro miniapp; React/Vite Web; deployment/HTTPS/observability deferred / 保持已定前端选型和延后范围 |
| Browser / 浏览器 | Close only the owned unused pages/processes in finally / finally关闭本次不用的页面与进程 |
| Publication / 发布 | Maintain both READMEs, changelogs and complete per-round pending table, then validate/commit/push / 双语文档、更新说明、完整待办同步核验推送 |

Current native verification is Windows amd64. Linux CLI cross-compilation does
not prove Linux runtime/GPU support. Do not claim a diagram, screenshot or a
schema test proves a live marketplace, encrypted inference or payment eligibility.

当前原生验证为 Windows amd64；Linux 交叉编译不证明 Linux/GPU 运行。图示、截图、Schema核验不能作为真实市场、加密推理或支付资格证明。

Useful commands and reproduction paths are in [Usage](dev/repository/USAGE.md)
and [Media](dev/repository/MEDIA.md). Preserve third-party notices in
[dev/licenses](dev/licenses/go-dependencies.json). A future dependency/kernel
change needs its own provenance and permission checks; current Go notices do not
cover unimplemented LMA/ComfyUI/LM Studio/Sub2API packages.

使用/复现命令见两份指南。保留第三方许可；未来新引擎、上游和节点须独立确认来源和权限，不能由现有 Go 许可外推。
