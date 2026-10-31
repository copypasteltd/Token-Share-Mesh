# 开发实施进度

目标：按全部开发细则交付成体系源码。当前源码目标 ACTIVE，源码实施进展与需求/设计工作分别记录。

R025接通独立批准的原生Host会话、原物理GPU与持久任务、加密结果仓和三协议交付；密文资产摘要保持原义，解密正文另以仅驻内存的原租约/清单/字节凭据校验。宿主拥有worker和结果读取句柄并在关闭时排空、撤权及等待退出，未知原owner不释放。当前限PLAINTEXT/STANDARD；生产资金/usage/ACK/SDK、崩溃恢复、完整LM Studio/ComfyUI/LMA、E2E/LP/性能与80/96完整验收仍待，全56任务222子项保持开放。 [本轮完整证据](../tests/evidence/R025-验证报告.md)。

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
| R017 | [实际原生输入加密与首租约原子准入](R017-本轮实现与完整待办.md) | PASSED_FOR_CONTROL_AND_RELAY_SUBSET_ONLY |
| R018 | [原设备签名领取与原租约交付](R018-本轮实现与完整待办.md) | PASSED_FOR_CONTROL_AND_RELAY_SUBSET_ONLY |
| R019 | [原清单与输入受限交付](R019-本轮实现与完整待办.md) | PASSED_FOR_CONTROL_AND_RELAY_SUBSET_ONLY |
| R020 | [持久原生执行宿主与内容适配接线](R020-本轮实现与完整待办.md) | PASSED_FOR_CONTROL_AND_RELAY_SUBSET_ONLY |
| R021 | [原生LLM可替换worker与受限管道](R021-本轮实现与完整待办.md) | PASSED_FOR_CONTROL_AND_RELAY_SUBSET_ONLY |
| R022 | [原生文本结果加密保管与三协议交付](R022-本轮实现与完整待办.md) | PASSED_FOR_CONTROL_AND_RELAY_SUBSET_ONLY |
| R023 | [真实原生LLM包校验、本机检查与CUDA验证](R023-本轮实现与完整待办.md) | PASSED_FOR_CONTROL_AND_RELAY_SUBSET_ONLY |
| R024 | [物理显卡探测、启动绑定与同卡别名防绕过](R024-本轮实现与完整待办.md) | PASSED_FOR_CONTROL_AND_RELAY_SUBSET_ONLY |
| R025 | [原生宿主、输入驻留校验与实际模型结果接线](R025-本轮实现与完整待办.md) | PASSED_FOR_CONTROL_AND_RELAY_SUBSET_ONLY |

任务现状：CONDITIONAL=1；DEFERRED=1；IN_PROGRESS=31；NOT_STARTED=23。

[人工维护的机器进度](implementation-status.json)；[完整开发规范](../开发细则总表.md)。
