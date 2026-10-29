# MOD-12 GPU租赁开发细则

P11｜开发细则 v1.0｜MOD-12｜2026-10-30｜需求 v3.0 / 技术 v1.0。

> 由 dev/planning/开发细则源数据.py 与既有技术契约生成；修改源后重建，不手改本文件。本套为冻结设计拆解：55 任务 DESIGN、DEV-55 DEFERRED；真实源码进展见 dev/progress/README.md，完整产品/设备/支付/密码/性能验收不能据此记为通过。

[返回开发总表](../开发细则总表.md)；[模块边界总表](../docs/01-模块划分与职责边界.md)。

## 1. 职责与边界

| 字段 | 约定 |
| --- | --- |
| 职责 | 实现受控整卡租赁与有效时间计费 |
| 输入 | 已付合同、双方ready、同卡资源权 |
| 输出 | 会话/grant/可用区间/续租/停止清理 |
| 禁止承担 | 不给主机管理权，不按子任务另售或自动续费 |
| 岗位/人员 | 租赁/调度工程；实际负责人/工期未分配 |
| 计划路径 | dev/src/internal/rental |
| 协作模块 | [MOD-04](MOD-04-合同订单路由预算.md)、[MOD-05](MOD-05-支付退款适配.md)、[MOD-06](MOD-06-调度资源租约.md) |
| 应用服务 | SessionCoordinator、WorkspaceGrant、AvailabilityMeter |
| 权威表 | rental_sessions、rental_renewals、rental_access_grants、availability_intervals |
| 适用约束 | [CON-001](../docs/02-表格化需求约束.md#CON-001)、[CON-002](../docs/02-表格化需求约束.md#CON-002)、[CON-006](../docs/02-表格化需求约束.md#CON-006)、[CON-007](../docs/02-表格化需求约束.md#CON-007)、[CON-008](../docs/02-表格化需求约束.md#CON-008)、[CON-009](../docs/02-表格化需求约束.md#CON-009)、[CON-013](../docs/02-表格化需求约束.md#CON-013)、[CON-015](../docs/02-表格化需求约束.md#CON-015)、[CON-016](../docs/02-表格化需求约束.md#CON-016)、[CON-019](../docs/02-表格化需求约束.md#CON-019)、[CON-021](../docs/02-表格化需求约束.md#CON-021)、[CON-022](../docs/02-表格化需求约束.md#CON-022)、[CON-032](../docs/02-表格化需求约束.md#CON-032)、[CON-035](../docs/02-表格化需求约束.md#CON-035)、[CON-036](../docs/02-表格化需求约束.md#CON-036)、[CON-038](../docs/02-表格化需求约束.md#CON-038)、[CON-040](../docs/02-表格化需求约束.md#CON-040)、[CON-041](../docs/02-表格化需求约束.md#CON-041)、[CON-042](../docs/02-表格化需求约束.md#CON-042)、[CON-043](../docs/02-表格化需求约束.md#CON-043)、[CON-046](../docs/02-表格化需求约束.md#CON-046)、[CON-047](../docs/02-表格化需求约束.md#CON-047) |
| 核心规范 | [12-显卡租赁与可用时间计量.md](../../docs/技术开发/12-显卡租赁与可用时间计量.md) |

## 2. 控制接口归属

| 索引 | operationId | 方法/路径 | 任务 | scope | 事务/幂等 |
| --- | --- | --- | --- | --- | --- |
| INT-005 | confirmRentalReady | POST /rental-sessions/{session_id}/ready | [DEV-33](../开发细则总表.md#DEV-33) | rental:ready | TX-08 / 必需稳定键 |
| INT-023 | createRentalAccessGrant | POST /rental-sessions/{session_id}/access-grants | [DEV-33](../开发细则总表.md#DEV-33) | rental:access | TX-08 / 必需稳定键 |
| INT-024 | createRentalExport | POST /rental-sessions/{session_id}/exports | [DEV-36](../开发细则总表.md#DEV-36) | rental:export | TX-01_OR_MODULE_LOCAL / 必需稳定键 |
| INT-025 | createRentalRenewal | POST /rental-sessions/{session_id}/renewals | [DEV-35](../开发细则总表.md#DEV-35) | rental:renew | TX-01_OR_MODULE_LOCAL / 必需稳定键 |
| INT-026 | createRentalSession | POST /rental-sessions | [DEV-33](../开发细则总表.md#DEV-33) | rental:create | TX-08 / 必需稳定键 |
| INT-045 | getRentalMeterStatement | GET /rental-sessions/{session_id}/statement | [DEV-34](../开发细则总表.md#DEV-34) | rental:read | READ_PROJECTION / 见权威契约 |
| INT-046 | getRentalSession | GET /rental-sessions/{session_id} | [DEV-33](../开发细则总表.md#DEV-33) | rental:read | READ_PROJECTION / 见权威契约 |
| INT-071 | recordRentalAvailability | POST /rental-sessions/{session_id}/availability | [DEV-34](../开发细则总表.md#DEV-34) | device:rental | TX-09 / 必需稳定键 |
| INT-072 | recordRentalCleanup | POST /rental-sessions/{session_id}/cleanup | [DEV-36](../开发细则总表.md#DEV-36) | device:rental | TX-10 / 必需稳定键 |
| INT-073 | recordRentalDeviceReady | POST /rental-sessions/{session_id}/device-ready | [DEV-33](../开发细则总表.md#DEV-33) | device:rental | TX-08 / 必需稳定键 |
| INT-084 | stopRentalSession | POST /rental-sessions/{session_id}/stop | [DEV-35](../开发细则总表.md#DEV-35) | rental:stop | TX-10 / 必需稳定键 |

[完整控制面、数据表与需求追踪](../docs/04-接口数据与需求追踪.md)；请求/响应 Schema 和响应码沿用既有 OpenAPI。

## 3. 开发任务与子项

<a id="DEV-33"></a>

### DEV-33 租赁占位及双方就绪

| 字段 | 细则 |
| --- | --- |
| 模块/岗位 | [MOD-12](MOD-12-GPU租赁.md) / 租赁/调度工程；实际负责人未分配 |
| 计划路径 | `dev/src/internal/rental` |
| 范围/状态 | CURRENT / DESIGN / NOT_EXECUTED |
| 依赖 | [DEV-15](../开发细则总表.md#DEV-15)、[DEV-18](../开发细则总表.md#DEV-18)、[DEV-24](../开发细则总表.md#DEV-24) |
| 需求/主验收 | P11-F41、P11-F42、P11-F43、P11-F48；P11-T41、P11-T42、P11-T43、P11-T48 |
| 实施目标 | 实现同卡独占、零费准备、设备/买方就绪、单次起表及受控工作区grant。 |
| 原完成证据 | 超时/预热失败零费；默认STANDARD且禁止shell/RDP/admin/任意节点。 |
| 门槛 | POC-10 |
| 控制操作 | createRentalSession、getRentalSession、confirmRentalReady、recordRentalDeviceReady、createRentalAccessGrant |
| 需求约束 | [CON-001](../docs/02-表格化需求约束.md#CON-001)、[CON-002](../docs/02-表格化需求约束.md#CON-002)、[CON-006](../docs/02-表格化需求约束.md#CON-006)、[CON-007](../docs/02-表格化需求约束.md#CON-007)、[CON-008](../docs/02-表格化需求约束.md#CON-008)、[CON-009](../docs/02-表格化需求约束.md#CON-009)、[CON-013](../docs/02-表格化需求约束.md#CON-013)、[CON-015](../docs/02-表格化需求约束.md#CON-015)、[CON-016](../docs/02-表格化需求约束.md#CON-016)、[CON-019](../docs/02-表格化需求约束.md#CON-019)、[CON-021](../docs/02-表格化需求约束.md#CON-021)、[CON-022](../docs/02-表格化需求约束.md#CON-022)、[CON-032](../docs/02-表格化需求约束.md#CON-032)、[CON-035](../docs/02-表格化需求约束.md#CON-035)、[CON-036](../docs/02-表格化需求约束.md#CON-036)、[CON-038](../docs/02-表格化需求约束.md#CON-038)、[CON-040](../docs/02-表格化需求约束.md#CON-040)、[CON-041](../docs/02-表格化需求约束.md#CON-041)、[CON-042](../docs/02-表格化需求约束.md#CON-042)、[CON-043](../docs/02-表格化需求约束.md#CON-043)、[CON-046](../docs/02-表格化需求约束.md#CON-046)、[CON-047](../docs/02-表格化需求约束.md#CON-047) |
| 统一交付 | 适用 CHK-01—08；不适用项注明理由；证据规则见 DOC-06 |

| 子项编号 | 开发项 | 实现要求与失败边界 | 交付/证据 |
| --- | --- | --- | --- |
| DEV-33.01 | 租赁占位 | 已付合格合同原子取同物理卡，会话权限不等主机管理员 | 争抢/自用/维护冲突用例 |
| DEV-33.02 | 双方准备 | device/buyer ready核同环境/截止，准备失败零费 | ready重复/超时/预热失败记录 |
| DEV-33.03 | 单次起表 | 双端ready+资金有效后唯一started_at与工作区grant | 并发ready不重起表/重复收费 |
| DEV-33.04 | 受控工作区 | 仅批准模型/配方/参数，租内调用不另售，不给shell/RDP | 越权节点/代码/管理接口拒绝 |

NB-R1需求增量：NB-R013、NB-R018、NB-R029、NB-R046；[正反向验收及契约缺口](../../docs/需求推进/NB-1.0/06-需求追踪与验收矩阵.md)。源码实施见进度登记，子场景仍未执行。

规范来源：[12-显卡租赁与可用时间计量.md](../../docs/技术开发/12-显卡租赁与可用时间计量.md)；[21-开发任务分解与需求追踪.md](../../docs/技术开发/21-开发任务分解与需求追踪.md)；[18-测试工程与故障验证.md](../../docs/技术开发/18-测试工程与故障验证.md)；[附录-本轮范围与端侧选型修订.md](../../docs/技术开发/附录-本轮范围与端侧选型修订.md)；[附录-供给端内核封装与替换规范.md](../../docs/技术开发/附录-供给端内核封装与替换规范.md)。

<a id="DEV-34"></a>

### DEV-34 可用区间与最终计费

| 字段 | 细则 |
| --- | --- |
| 模块/岗位 | [MOD-12](MOD-12-GPU租赁.md) / 租赁/调度工程；实际负责人未分配 |
| 计划路径 | `dev/src/internal/rental` |
| 范围/状态 | CURRENT / DESIGN / NOT_EXECUTED |
| 依赖 | [DEV-12](../开发细则总表.md#DEV-12)、[DEV-33](../开发细则总表.md#DEV-33) |
| 需求/主验收 | P11-F18、P11-F44、P11-F46、P11-F61；P11-T18、P11-T44、P11-T46、P11-T61 |
| 实施目标 | 实现序号/单调时/资源代次证据、区间去重合并、累计毫秒一次取整和预算停止。 |
| 原完成证据 | 重叠/重复/UNKNOWN不多计，准备/故障不算可用；与最终statement逐区间可复核。 |
| 门槛 | POC-10、E11 |
| 控制操作 | recordRentalAvailability、getRentalMeterStatement |
| 需求约束 | [CON-001](../docs/02-表格化需求约束.md#CON-001)、[CON-002](../docs/02-表格化需求约束.md#CON-002)、[CON-006](../docs/02-表格化需求约束.md#CON-006)、[CON-007](../docs/02-表格化需求约束.md#CON-007)、[CON-008](../docs/02-表格化需求约束.md#CON-008)、[CON-009](../docs/02-表格化需求约束.md#CON-009)、[CON-013](../docs/02-表格化需求约束.md#CON-013)、[CON-015](../docs/02-表格化需求约束.md#CON-015)、[CON-016](../docs/02-表格化需求约束.md#CON-016)、[CON-019](../docs/02-表格化需求约束.md#CON-019)、[CON-021](../docs/02-表格化需求约束.md#CON-021)、[CON-022](../docs/02-表格化需求约束.md#CON-022)、[CON-032](../docs/02-表格化需求约束.md#CON-032)、[CON-035](../docs/02-表格化需求约束.md#CON-035)、[CON-036](../docs/02-表格化需求约束.md#CON-036)、[CON-038](../docs/02-表格化需求约束.md#CON-038)、[CON-040](../docs/02-表格化需求约束.md#CON-040)、[CON-041](../docs/02-表格化需求约束.md#CON-041)、[CON-042](../docs/02-表格化需求约束.md#CON-042)、[CON-043](../docs/02-表格化需求约束.md#CON-043)、[CON-046](../docs/02-表格化需求约束.md#CON-046)、[CON-047](../docs/02-表格化需求约束.md#CON-047) |
| 统一交付 | 适用 CHK-01—08；不适用项注明理由；证据规则见 DOC-06 |

| 子项编号 | 开发项 | 实现要求与失败边界 | 交付/证据 |
| --- | --- | --- | --- |
| DEV-34.01 | 区间证据 | 用单调耗时/序号/资源代次表达真实可用区间 | 重放/时钟回拨/断线证据 |
| DEV-34.02 | 去重合并 | 剔重叠/重复/未知，准备和故障不算可用 | 区间合并及缺失ack负向用例 |
| DEV-34.03 | 一次取整 | 累计毫秒按约有理费率最终取整，不逐片多收 | 边界时长/费率/预算守恒 |
| DEV-34.04 | 清单核对 | 区间→meter statement→账单可复核，卖方自报不默认可信 | 最终租时清单与双方确认范围 |

NB-R1需求增量：NB-R014、NB-R015、NB-R024、NB-R032、NB-R033、NB-R041；[正反向验收及契约缺口](../../docs/需求推进/NB-1.0/06-需求追踪与验收矩阵.md)。源码实施见进度登记，子场景仍未执行。

规范来源：[12-显卡租赁与可用时间计量.md](../../docs/技术开发/12-显卡租赁与可用时间计量.md)；[21-开发任务分解与需求追踪.md](../../docs/技术开发/21-开发任务分解与需求追踪.md)；[18-测试工程与故障验证.md](../../docs/技术开发/18-测试工程与故障验证.md)；[附录-本轮范围与端侧选型修订.md](../../docs/技术开发/附录-本轮范围与端侧选型修订.md)。

<a id="DEV-35"></a>

### DEV-35 续租断线与硬截止

| 字段 | 细则 |
| --- | --- |
| 模块/岗位 | [MOD-12](MOD-12-GPU租赁.md) / 租赁/调度工程；实际负责人未分配 |
| 计划路径 | `dev/src/internal/rental` |
| 范围/状态 | CURRENT / DESIGN / NOT_EXECUTED |
| 依赖 | [DEV-13](../开发细则总表.md#DEV-13)、[DEV-14](../开发细则总表.md#DEV-14)、[DEV-33](../开发细则总表.md#DEV-33)、[DEV-34](../开发细则总表.md#DEV-34) |
| 需求/主验收 | P11-F45、P11-F46；P11-T45、P11-T46 |
| 实施目标 | 实现显式续租报价/支付有效后延长，断线宽限、签名本地deadline和到期强停。 |
| 原完成证据 | 网断/支付未知/续租晚到不超已付截止；不能靠客户端连接恢复自动续费。 |
| 门槛 | POC-10、E02 |
| 控制操作 | createRentalRenewal、stopRentalSession |
| 需求约束 | [CON-001](../docs/02-表格化需求约束.md#CON-001)、[CON-002](../docs/02-表格化需求约束.md#CON-002)、[CON-006](../docs/02-表格化需求约束.md#CON-006)、[CON-007](../docs/02-表格化需求约束.md#CON-007)、[CON-008](../docs/02-表格化需求约束.md#CON-008)、[CON-009](../docs/02-表格化需求约束.md#CON-009)、[CON-013](../docs/02-表格化需求约束.md#CON-013)、[CON-015](../docs/02-表格化需求约束.md#CON-015)、[CON-016](../docs/02-表格化需求约束.md#CON-016)、[CON-019](../docs/02-表格化需求约束.md#CON-019)、[CON-021](../docs/02-表格化需求约束.md#CON-021)、[CON-022](../docs/02-表格化需求约束.md#CON-022)、[CON-032](../docs/02-表格化需求约束.md#CON-032)、[CON-035](../docs/02-表格化需求约束.md#CON-035)、[CON-036](../docs/02-表格化需求约束.md#CON-036)、[CON-038](../docs/02-表格化需求约束.md#CON-038)、[CON-040](../docs/02-表格化需求约束.md#CON-040)、[CON-041](../docs/02-表格化需求约束.md#CON-041)、[CON-042](../docs/02-表格化需求约束.md#CON-042)、[CON-043](../docs/02-表格化需求约束.md#CON-043)、[CON-046](../docs/02-表格化需求约束.md#CON-046)、[CON-047](../docs/02-表格化需求约束.md#CON-047) |
| 统一交付 | 适用 CHK-01—08；不适用项注明理由；证据规则见 DOC-06 |

| 子项编号 | 开发项 | 实现要求与失败边界 | 交付/证据 |
| --- | --- | --- | --- |
| DEV-35.01 | 显式续租 | 新报价/付款有效后才延，迟到付款不复活已结束会话 | 续租成功/拒付/UNKNOWN/迟到报告 |
| DEV-35.02 | 断线策略 | 合同固定宽限/继续范围，不能靠重连无限续费 | 断线/休眠/重连计时验证 |
| DEV-35.03 | 本地硬截止 | 签名deadline本地执行，未有效续权必停 | 网断/时钟回拨/续期旧revision拒绝 |
| DEV-35.04 | 停止后恢复 | 超时先停清理、后核新单，旧grant不再有效 | 到期抢占与旧入口撤权证据 |

NB-R1需求增量：NB-R014、NB-R016、NB-R036；[正反向验收及契约缺口](../../docs/需求推进/NB-1.0/06-需求追踪与验收矩阵.md)。源码实施见进度登记，子场景仍未执行。

规范来源：[12-显卡租赁与可用时间计量.md](../../docs/技术开发/12-显卡租赁与可用时间计量.md)；[21-开发任务分解与需求追踪.md](../../docs/技术开发/21-开发任务分解与需求追踪.md)；[18-测试工程与故障验证.md](../../docs/技术开发/18-测试工程与故障验证.md)；[附录-本轮范围与端侧选型修订.md](../../docs/技术开发/附录-本轮范围与端侧选型修订.md)。

<a id="DEV-36"></a>

### DEV-36 租赁撤权导出与清理

| 字段 | 细则 |
| --- | --- |
| 模块/岗位 | [MOD-12](MOD-12-GPU租赁.md) / 租赁/调度工程；实际负责人未分配 |
| 计划路径 | `dev/src/internal/rental` |
| 范围/状态 | CURRENT / DESIGN / NOT_EXECUTED |
| 依赖 | [DEV-20](../开发细则总表.md#DEV-20)、[DEV-33](../开发细则总表.md#DEV-33)、[DEV-35](../开发细则总表.md#DEV-35) |
| 需求/主验收 | P11-F22、P11-F43、P11-F46、P11-F47、P11-F52；P11-T22、P11-T43、P11-T46、P11-T47、P11-T52 |
| 实施目标 | 实现结束撤grant、受控导出、真实停止及租户工作区/缓存清理ACK。 |
| 原完成证据 | 下一租户无旧结果/上下文，清理失败隔离；先释放再删的竞态被测试拒绝。 |
| 门槛 | POC-10 |
| 控制操作 | createRentalExport、recordRentalCleanup |
| 需求约束 | [CON-001](../docs/02-表格化需求约束.md#CON-001)、[CON-002](../docs/02-表格化需求约束.md#CON-002)、[CON-006](../docs/02-表格化需求约束.md#CON-006)、[CON-007](../docs/02-表格化需求约束.md#CON-007)、[CON-008](../docs/02-表格化需求约束.md#CON-008)、[CON-009](../docs/02-表格化需求约束.md#CON-009)、[CON-013](../docs/02-表格化需求约束.md#CON-013)、[CON-015](../docs/02-表格化需求约束.md#CON-015)、[CON-016](../docs/02-表格化需求约束.md#CON-016)、[CON-019](../docs/02-表格化需求约束.md#CON-019)、[CON-021](../docs/02-表格化需求约束.md#CON-021)、[CON-022](../docs/02-表格化需求约束.md#CON-022)、[CON-032](../docs/02-表格化需求约束.md#CON-032)、[CON-035](../docs/02-表格化需求约束.md#CON-035)、[CON-036](../docs/02-表格化需求约束.md#CON-036)、[CON-038](../docs/02-表格化需求约束.md#CON-038)、[CON-040](../docs/02-表格化需求约束.md#CON-040)、[CON-041](../docs/02-表格化需求约束.md#CON-041)、[CON-042](../docs/02-表格化需求约束.md#CON-042)、[CON-043](../docs/02-表格化需求约束.md#CON-043)、[CON-046](../docs/02-表格化需求约束.md#CON-046)、[CON-047](../docs/02-表格化需求约束.md#CON-047) |
| 统一交付 | 适用 CHK-01—08；不适用项注明理由；证据规则见 DOC-06 |

| 子项编号 | 开发项 | 实现要求与失败边界 | 交付/证据 |
| --- | --- | --- | --- |
| DEV-36.01 | 结束撤权 | 结束/退款/到期撤工作区grant与新调用权 | 旧token/长连接/并发结束拒绝 |
| DEV-36.02 | 受控导出 | 只导本人获准结果，期限/对象scope明确，不导系统文件 | 越权路径/导出到期测试 |
| DEV-36.03 | 实际清理 | 后端停算、KV/文件/临时钥清完ACK，不放任任务继续 | 下一租户隔离与资源未停不FREE |
| DEV-36.04 | 离线补清 | 失联标隔离/待清，重启先查停清再售 | 离线到期/强杀/恢复记录 |

NB-R1需求增量：NB-R017；[正反向验收及契约缺口](../../docs/需求推进/NB-1.0/06-需求追踪与验收矩阵.md)。源码实施见进度登记，子场景仍未执行。

规范来源：[12-显卡租赁与可用时间计量.md](../../docs/技术开发/12-显卡租赁与可用时间计量.md)；[21-开发任务分解与需求追踪.md](../../docs/技术开发/21-开发任务分解与需求追踪.md)；[18-测试工程与故障验证.md](../../docs/技术开发/18-测试工程与故障验证.md)；[附录-本轮范围与端侧选型修订.md](../../docs/技术开发/附录-本轮范围与端侧选型修订.md)。

## 4. 需求与主验收覆盖

| 需求/主验收 | 功能 | 优先级/范围 | 关联任务 |
| --- | --- | --- | --- |
| P11-F18 / P11-T18 | 计量、成交额及费用账单 | P0 / CURRENT | [DEV-12](../开发细则总表.md#DEV-12)、[DEV-16](../开发细则总表.md#DEV-16)、[DEV-34](../开发细则总表.md#DEV-34)、[DEV-38](../开发细则总表.md#DEV-38)、[DEV-39](../开发细则总表.md#DEV-39) |
| P11-F22 / P11-T22 | 内容保护、清理与设备退出 | P0 / CURRENT | [DEV-04](../开发细则总表.md#DEV-04)、[DEV-20](../开发细则总表.md#DEV-20)、[DEV-36](../开发细则总表.md#DEV-36)、[DEV-37](../开发细则总表.md#DEV-37)、[DEV-51](../开发细则总表.md#DEV-51)、[DEV-53](../开发细则总表.md#DEV-53)、[DEV-55](../开发细则总表.md#DEV-55) |
| P11-F41 / P11-T41 | 租赁供给、环境与独占锁 | P0 / CURRENT | [DEV-18](../开发细则总表.md#DEV-18)、[DEV-33](../开发细则总表.md#DEV-33) |
| P11-F42 / P11-T42 | 租赁准备与就绪起表 | P0 / CURRENT | [DEV-15](../开发细则总表.md#DEV-15)、[DEV-33](../开发细则总表.md#DEV-33) |
| P11-F43 / P11-T43 | 租赁工作区与受控操作 | P0 / CURRENT | [DEV-24](../开发细则总表.md#DEV-24)、[DEV-33](../开发细则总表.md#DEV-33)、[DEV-36](../开发细则总表.md#DEV-36)、[DEV-47](../开发细则总表.md#DEV-47) |
| P11-F44 / P11-T44 | 可用租时计量、取整与预算 | P0 / CURRENT | [DEV-12](../开发细则总表.md#DEV-12)、[DEV-34](../开发细则总表.md#DEV-34)、[DEV-38](../开发细则总表.md#DEV-38) |
| P11-F45 / P11-T45 | 租期、断线、续租和强制到期 | P0 / CURRENT | [DEV-15](../开发细则总表.md#DEV-15)、[DEV-35](../开发细则总表.md#DEV-35)、[DEV-54](../开发细则总表.md#DEV-54) |
| P11-F46 / P11-T46 | 租赁故障、早退和退款 | P0 / CURRENT | [DEV-14](../开发细则总表.md#DEV-14)、[DEV-34](../开发细则总表.md#DEV-34)、[DEV-35](../开发细则总表.md#DEV-35)、[DEV-36](../开发细则总表.md#DEV-36)、[DEV-49](../开发细则总表.md#DEV-49) |
| P11-F47 / P11-T47 | 结束撤权、清理与再出租 | P0 / CURRENT | [DEV-20](../开发细则总表.md#DEV-20)、[DEV-36](../开发细则总表.md#DEV-36)、[DEV-54](../开发细则总表.md#DEV-54) |
| P11-F48 / P11-T48 | 租赁隐私属性与组合限制 | P0 / CURRENT | [DEV-09](../开发细则总表.md#DEV-09)、[DEV-32](../开发细则总表.md#DEV-32)、[DEV-33](../开发细则总表.md#DEV-33)、[DEV-45](../开发细则总表.md#DEV-45)、[DEV-52](../开发细则总表.md#DEV-52) |
| P11-F52 / P11-T52 | 分模式留存与删除 | P0 / PARTIAL | [DEV-04](../开发细则总表.md#DEV-04)、[DEV-20](../开发细则总表.md#DEV-20)、[DEV-36](../开发细则总表.md#DEV-36)、[DEV-37](../开发细则总表.md#DEV-37)、[DEV-51](../开发细则总表.md#DEV-51)、[DEV-53](../开发细则总表.md#DEV-53)、[DEV-55](../开发细则总表.md#DEV-55) |
| P11-F61 / P11-T61 | 本地LLM与手机计价证据 | P0 / CURRENT | [DEV-12](../开发细则总表.md#DEV-12)、[DEV-23](../开发细则总表.md#DEV-23)、[DEV-34](../开发细则总表.md#DEV-34)、[DEV-38](../开发细则总表.md#DEV-38)、[DEV-42](../开发细则总表.md#DEV-42) |

## 5. 完成规则

适用 CHK-01—08，分别提交实现、失败恢复、权限/幂等和真实证据。模块代码完成不意味着对应组合可售；无证据的引擎、许可、隐私档位、支付资格保持关闭。[验收交付规则](../docs/06-验收交付与证据规则.md)。
