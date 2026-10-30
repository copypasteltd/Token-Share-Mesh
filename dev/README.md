# 算力共享平台开发目录

P11｜开发细则 v1.0｜DEV-ROOT｜2026-10-30｜需求 v3.0 / 技术 v1.0。

> 由 dev/planning/开发细则源数据.py 与既有技术契约生成；修改源后重建，不手改本文件。本套为冻结设计拆解：55 任务 DESIGN、DEV-55 DEFERRED；真实源码进展见 dev/progress/README.md，完整产品/设备/支付/密码/性能验收不能据此记为通过。

## 当前源码阶段：R024

R024增加Windows NVIDIA整卡UUID/PCI探测及PIPE_4启动复核；显式GPU选择和环境摘要绑定实际身份，同UUID别名不能绕过共享门闩。只读gpu-discover不加载模型、不准入共享，LocalInfo仅含安全元数据。本机观测不构成远程证明或LP/E2E/性能/usage/ACK/资金资格；Linux/AMD/手机实际接入与完整LM Studio隔离封装仍待。全56任务222子项、80/96完整验收保持开放。

[全部56任务与每轮证据](progress/README.md)；[本机使用与恢复](repository/USAGE.md)。

开发路径：克隆根目录下的 `dev/`。后续开发均在本目录组织；所有命令以当前克隆的项目根目录为工作目录。

| 编号 | 文档 | 内容 |
| --- | --- | --- |
| DOC-00 | [开发基线与编号规范](docs/00-开发基线与编号规范.md) | 范围/选型/编号 |
| DOC-01 | [模块划分与职责边界](docs/01-模块划分与职责边界.md) | 20 模块职责及边界 |
| DOC-02 | [表格化需求约束](docs/02-表格化需求约束.md) | 47 约束、失败处理及证据 |
| DOC-03 | [开发细则总表](开发细则总表.md) | 56 任务、222 子项、依赖与交付 |
| DOC-04 | [接口数据与需求追踪](docs/04-接口数据与需求追踪.md) | 93 控制操作、65 表、80 F/T |
| DOC-05 | [开发依赖与阶段清单](docs/05-开发依赖与阶段清单.md) | 依赖图、阶段和条件分支 |
| DOC-06 | [验收交付与证据规则](docs/06-验收交付与证据规则.md) | 验收条件与真实证据等级 |
| DOC-07 | [路径迁移与规范来源](docs/07-路径迁移与规范来源.md) | 开发路径、规范源和重建顺序 |

[源码使用说明](源码使用说明.md)；[机器开发清单](contracts/development-plan-v1.0.json)；[结构核验报告](评审/开发细则核验-v1.0.md)；[开发工作约定](AGENTS.md)。

## 路径

| 编号 | 路径 | 用途 |
| --- | --- | --- |
| PTH-001 | `dev/` | 开发根目录；新代码、测试、工作流及开发工具统一入口 |
| PTH-002 | `dev/src/` | 产品工程；单 Go 模块，真实源码与验证进展见 dev/progress |
| PTH-003 | `dev/src/api/` | 控制面入口和 HTTP 适配，不承载共享领域真相 |
| PTH-004 | `dev/src/internal/` | 平台应用服务、领域规则及持久化端口 |
| PTH-005 | `dev/src/scheduler/` | 原生 worker 组合入口，复用共享应用服务 |
| PTH-006 | `dev/src/gateway/` | 明文平台入口、共用转发内核、平台盲转 |
| PTH-007 | `dev/src/client/` | Windows/Linux 原生供给端、Android LMA、买卖加密网关 |
| PTH-008 | `dev/src/miniapp/` | React + Taro 支付宝小程序 |
| PTH-009 | `dev/src/web/` | React + Vite Web |
| PTH-010 | `dev/src/frontend/packages/` | 纯 TypeScript 领域、DTO、验证和端口共享 |
| PTH-011 | `dev/src/shared/` | Go 共享类型、端口、金额、时钟、ID，不放独立业务账本 |
| PTH-012 | `dev/tests/` | 跨端测试入口、证据和脱敏夹具；Go 测试按单模块规则随 dev/src 包存放 |
| PTH-013 | `dev/workflows/` | 受审配方、参数 Schema、模型/节点/版本清单 |
| PTH-014 | `dev/tools/` | 新原生开发工具和细则构建/核验 |
| PTH-015 | `dev/planning/` | 人工维护约束与任务子项源数据 |
| PTH-016 | `dev/docs/` | 生成的编号、约束、依赖、追踪、交付规则 |
| PTH-017 | `dev/modules/` | 生成的 20 模块实施细则 |
| PTH-018 | `dev/contracts/` | 生成的机器开发清单，不替代原 OpenAPI/SQL |
| PTH-019 | `dev/评审/` | 开发清单结构核验；不是产品测试结果 |
| PTH-020 | `docs/` | 既有需求、技术设计、SQL、OpenAPI 及来源，保持权威 |
| PTH-021 | `scripts/` | 既有白皮书/技术附件构建和核验，不新增产品服务 |
| PTH-022 | `runtime/` | 项目共享模型、引擎、密文工作区、数据库、缓存、临时与工具依赖 |

## 使用

先读 DOC-00/01/02，再从 DOC-03 选任务进入对应模块细则，按 DOC-05 依赖实现，最后按 DOC-06 交付。修改人工源/权威契约后先重建相应附件，再重建细则并核验；不得直接编辑生成文档。

```powershell
python -X utf8 dev/tools/构建开发细则.py
python -X utf8 dev/tools/核验开发细则.py
```

[源码实施与每轮完整待办](progress/README.md)；[NB-R1需求推进](../docs/需求推进/NB-1.0/README.md)。源码阶段由进度登记维护；主验收及真实设备/渠道资格仍需独立证据。不启动本地虚拟化或模拟器，部署/HTTPS/可观测性延后。历史 root/src、tests、workflows 不再接收新开发内容；运行数据保持在项目 runtime。
