# 开发实施进度

目标：按全部开发细则交付成体系源码。当前源码目标 ACTIVE，源码实施进展与需求/设计工作分别记录。

R016新增原调用准备库：当前scoped key/原订单/同单资金事实重新核对后，将INPUT_PENDING、整笔单次预算、次数占用及安全事件在同一事务提交。原键恢复不重复占用；改参数/key冲突；取消/过期/未知保留原记录和占用。请求摘要只以版本化HMAC持久保存。前向008追加六列，001—007和端侧DDL原字节不改；仍34/93操作、65表。仅三个原生文本档位的PLAINTEXT/STANDARD；没有挂载模型入口、签发Lease/Grant或运行模型。输入就绪/资源原子准入、可信usage、真实支付、E2E和路由UI仍待。新增14项，当前完整184项原生与双端浏览器/Windows命令结果见本轮证据；未改21项TS/三目标构建按精确摘要复核。全56任务/222未收口子项和80/96未执行状态保持，目标ACTIVE。

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
| R012 | [固定路由原生文本转发、独立流终态与有界I/O](R012-本轮实现与完整待办.md) | VERIFIED_CHANGED_RELAY_WITH_UNCHANGED_BASELINE_FULL_RERUN_FAILED |
| R013 | [原生元数据事务与执行截止修正](R013-本轮实现与完整待办.md) | PASSED_FOR_CONTROL_AND_RELAY_SUBSET_ONLY |
| R014 | [订单预算预留与资格化消耗](R014-本轮实现与完整待办.md) | PASSED_FOR_CONTROL_AND_RELAY_SUBSET_ONLY |
| R015 | [已确认原订单路由与标准SDK绑定](R015-本轮实现与完整待办.md) | PASSED_FOR_CONTROL_AND_RELAY_SUBSET_ONLY |
| R016 | [原调用准备、次数与订单预算原子接线](R016-本轮实现与完整待办.md) | PASSED_FOR_CONTROL_AND_RELAY_SUBSET_ONLY |

任务现状：CONDITIONAL=1；DEFERRED=1；IN_PROGRESS=28；NOT_STARTED=26。

[人工维护的机器进度](implementation-status.json)；[完整开发规范](../开发细则总表.md)。
