# 开发实施进度

目标：按全部开发细则交付成体系源码。当前源码目标 ACTIVE，源码实施进展与需求/设计工作分别记录。

R012新增固定卖家/合同/路由的独立Sub2API类转发核：三种原生文本JSON/SSE、精确字段、独立终态/文本一致性、显式准入与取钥端口、无重试和有界取消/截止/背压。新增21项Go局部用例（20项转发与1项监督竞态），累计134项；后端与授权为明确合成夹具，不发布付费推理入口，不替真实SDK/内核停算/计费/E2E或性能验收。未变21项TS/三目标构建只按源码/输出/日志摘要复用，当前双端浏览器与Windows命令重跑。平台31/93操作、65表、001—005字节保持，全部56任务/222未收口子项与80/96未执行、DEV-55延后保留。

| 轮次 | 内容与完整待办 | 验证状态 |
| --- | --- | --- |
| R001 | [原生后端基础、数据库与授权核心](R001-本轮实现与完整待办.md) | PASSED_FOR_FOUNDATION_ONLY |
| R003 | [NB价格合同、市场资格与报价建单应用服务](R003-本轮实现与完整待办.md) | PASSED_FOR_CONTROL_SERVICES_ONLY |
| R004 | [身份授权适配、一次性会话与13操作原生HTTP子集](R004-本轮实现与完整待办.md) | PASSED_FOR_CONTROL_SERVICES_ONLY |
| R005 | [卖家申请、同意与未来挂牌版本及23操作控制子集](R005-本轮实现与完整待办.md) | PASSED_FOR_CONTROL_SERVICES_ONLY |
| R006 | [NB双端交易控制页面、原生授权证明与原请求恢复](R006-本轮实现与完整待办.md) | PASSED_FOR_CONTROL_SERVICES_ONLY |
| R007 | [提供方配对登记、设备签名与共享控制，保留执行与资源安全边界](R007-本轮实现与完整待办.md) | PASSED_FOR_CONTROL_SERVICES_ONLY |
| R008 | [原生提供方控制代理、系统钥适配与本地原命令恢复，保留完整执行边界](R008-本轮实现与完整待办.md) | PASSED_FOR_CONTROL_SERVICES_ONLY |
| R009 | [双端提供方设备管理、一次性配对及原POST/PATCH控制恢复](R009-本轮实现与完整待办.md) | PASSED_FOR_CONTROL_SERVICES_ONLY |
| R010 | [原生签名租约、SQLite执行元数据收件箱与共资源门闩](R010-本轮实现与完整待办.md) | PASSED_FOR_CONTROL_SERVICES_ONLY |
| R011 | [原生执行监督、原后端恢复与双端恢复加载修复](R011-本轮实现与完整待办.md) | PASSED_FOR_CONTROL_SERVICES_ONLY |
| R012 | [固定路由原生文本转发、独立流终态与有界I/O](R012-本轮实现与完整待办.md) | PASSED_FOR_CONTROL_AND_RELAY_SUBSET_ONLY |

任务现状：CONDITIONAL=1；DEFERRED=1；IN_PROGRESS=25；NOT_STARTED=29。

[人工维护的机器进度](implementation-status.json)；[完整开发规范](../开发细则总表.md)。
