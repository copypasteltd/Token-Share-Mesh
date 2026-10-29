# dev 开发路径与细则索引

P11｜2026-10-30｜用户指定开发路径；需求 v3.0 与技术 v1.0 的业务语义保持有效。

后续开发根目录为本项目 `dev/`。代码、测试、审核配方、新工具分别写入 `dev/src/`、`dev/tests/`、`dev/workflows/`、`dev/tools/`。既有需求/技术设计、SQL、OpenAPI 与生成器继续保存在 `docs/`、`scripts/`；模型和全部真实运行数据继续使用项目 `runtime/`。旧根级空 src/tests/workflows 不作为后续开发入口。

| 内容 | 权威入口 | 编号/范围 |
| --- | --- | --- |
| 完整开发细则 | [开发细则总表](../../dev/开发细则总表.md) | 原有 DEV-01—56，新增 222 个任务子项 |
| 编号和技术基线 | [开发基线](../../dev/docs/00-开发基线与编号规范.md) | DOC、MOD、DEV、CON、INT、DATA、MS、CHK、PTH |
| 模块职责 | [模块划分](../../dev/docs/01-模块划分与职责边界.md) | 20 模块，职责/输入输出/禁止越界 |
| 需求硬约束 | [表格化约束](../../dev/docs/02-表格化需求约束.md) | CON-001—047，CON-047承接NB-R1，失败处理/证据/来源 |
| 接口/数据/需求追踪 | [追踪总表](../../dev/docs/04-接口数据与需求追踪.md) | 89 控制操作、60 表、80 功能及同号主验收 |
| 依赖与阶段 | [阶段清单](../../dev/docs/05-开发依赖与阶段清单.md) | 依赖图和阶段组织，不虚构工期 |
| 验收交付 | [证据规则](../../dev/docs/06-验收交付与证据规则.md) | 代码完成、真实产品验证、可售资格分别记录 |
| 源与路径 | [规范来源](../../dev/docs/07-路径迁移与规范来源.md) | 原始规范、生成映射与新开发路径的职责 |
| 机器开发清单 | [development-plan-v1.0.json](../../dev/contracts/development-plan-v1.0.json) | 与 Markdown 同源生成 |
| 文档结构核验 | [核验报告](../../dev/评审/开发细则核验-v1.0.md) | 仅文档覆盖/编号/依赖/链接，不是产品测试 |

人工细则源为 `dev/planning/开发细则源数据.py`；生成器结合技术模块目录、开发任务、需求追踪、接口映射与 SQL 表目录派生细则。接口和数据库没有在 dev 另起第二份权威定义。修改原始规范后，先重建白皮书/技术附件，再重建开发细则；禁止手改生成物。

```powershell
python -X utf8 scripts/构建项目白皮书.py
python -X utf8 scripts/构建技术开发文档.py
python -X utf8 dev/tools/构建开发细则.py
python -X utf8 scripts/核验需求文档.py
python -X utf8 scripts/核验项目白皮书.py
python -X utf8 scripts/核验技术开发文档.py
python -X utf8 dev/tools/核验开发细则.py
```

这些命令只生成/核验文档。当前 55 个开发任务 DESIGN，DEV-55 DEFERRED；所有真实设备、密码互通、性能、支付及主验收仍 NOT_EXECUTED。新二进制档位、模型/节点/内核及个人卖家渠道仍须分别取得证据。严禁为此启动本地 Docker/WSL/虚拟机/模拟器；部署、HTTPS、运维及可观测性继续延后。
