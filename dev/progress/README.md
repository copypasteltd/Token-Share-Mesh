# 开发实施进度

目标：按全部开发细则交付成体系源码。当前源码目标 ACTIVE，源码实施进展与需求/设计工作分别记录。

R007已实现一次性配对/登记恢复、设备逐请求签名与持久nonce、单调心跳、共享策略/停止撤销和可复用原生控制库；31/93操作、65表。60项Go局部用例及既有13项TS/三目标构建证据、双端实际浏览器回归通过，临时数据库/自建浏览器关闭。完整原生执行端/系统密钥存储/探测/资格/内核/资金仍待完成；停止撤销不释放在途owner/代次，配对与心跳不自授可售。全部56任务/222未收口子项、六项NB缺口和80/96未执行状态保留。

| 轮次 | 内容与完整待办 | 验证状态 |
| --- | --- | --- |
| R001 | [原生后端基础、数据库与授权核心](R001-本轮实现与完整待办.md) | PASSED_FOR_FOUNDATION_ONLY |
| R003 | [NB价格合同、市场资格与报价建单应用服务](R003-本轮实现与完整待办.md) | PASSED_FOR_CONTROL_SERVICES_ONLY |
| R004 | [身份授权适配、一次性会话与13操作原生HTTP子集](R004-本轮实现与完整待办.md) | PASSED_FOR_CONTROL_SERVICES_ONLY |
| R005 | [卖家申请、同意与未来挂牌版本及23操作控制子集](R005-本轮实现与完整待办.md) | PASSED_FOR_CONTROL_SERVICES_ONLY |
| R006 | [NB双端交易控制页面、原生授权证明与原请求恢复](R006-本轮实现与完整待办.md) | PASSED_FOR_CONTROL_SERVICES_ONLY |
| R007 | [提供方配对登记、设备签名与共享控制，保留执行与资源安全边界](R007-本轮实现与完整待办.md) | PASSED_FOR_CONTROL_SERVICES_ONLY |

任务现状：CONDITIONAL=1；DEFERRED=1；IN_PROGRESS=18；NOT_STARTED=36。

[人工维护的机器进度](implementation-status.json)；[完整开发规范](../开发细则总表.md)。
