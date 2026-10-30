# MOD-06 调度资源租约开发细则

P11｜开发细则 v1.0｜MOD-06｜2026-10-30｜需求 v3.0 / 技术 v1.0。

> 由 dev/planning/开发细则源数据.py 与既有技术契约生成；修改源后重建，不手改本文件。本套为冻结设计拆解：55 任务 DESIGN、DEV-55 DEFERRED；真实源码进展见 dev/progress/README.md，完整产品/设备/支付/密码/性能验收不能据此记为通过。

[返回开发总表](../开发细则总表.md)；[模块边界总表](../docs/01-模块划分与职责边界.md)。

## 1. 职责与边界

| 字段 | 约定 |
| --- | --- |
| 职责 | 协调任务准入、资源占有及恢复 |
| 输入 | 合同/资金/输入/证据、设备状态 |
| 输出 | PREPARED上下文、签名租约、唯一接受、清理授权 |
| 禁止承担 | 不在准备阶段推理，不凭TTL把未知GPU当空闲 |
| 岗位/人员 | 调度工程；实际负责人/工期未分配 |
| 计划路径 | dev/src/internal/dispatch |
| 协作模块 | [MOD-03](MOD-03-目录证据.md)、[MOD-04](MOD-04-合同订单路由预算.md)、[MOD-05](MOD-05-支付退款适配.md) |
| 应用服务 | Admission、ResourceArbiter、AttemptLease、Fence |
| 权威表 | devices、device_request_nonces、resources、resource_locks、jobs、attempts、execution_leases、deliveries、acceptances |
| 适用约束 | [CON-001](../docs/02-表格化需求约束.md#CON-001)、[CON-002](../docs/02-表格化需求约束.md#CON-002)、[CON-006](../docs/02-表格化需求约束.md#CON-006)、[CON-007](../docs/02-表格化需求约束.md#CON-007)、[CON-008](../docs/02-表格化需求约束.md#CON-008)、[CON-009](../docs/02-表格化需求约束.md#CON-009)、[CON-013](../docs/02-表格化需求约束.md#CON-013)、[CON-016](../docs/02-表格化需求约束.md#CON-016)、[CON-018](../docs/02-表格化需求约束.md#CON-018)、[CON-019](../docs/02-表格化需求约束.md#CON-019)、[CON-020](../docs/02-表格化需求约束.md#CON-020)、[CON-021](../docs/02-表格化需求约束.md#CON-021)、[CON-022](../docs/02-表格化需求约束.md#CON-022)、[CON-032](../docs/02-表格化需求约束.md#CON-032)、[CON-033](../docs/02-表格化需求约束.md#CON-033)、[CON-036](../docs/02-表格化需求约束.md#CON-036)、[CON-038](../docs/02-表格化需求约束.md#CON-038)、[CON-041](../docs/02-表格化需求约束.md#CON-041)、[CON-042](../docs/02-表格化需求约束.md#CON-042)、[CON-043](../docs/02-表格化需求约束.md#CON-043)、[CON-044](../docs/02-表格化需求约束.md#CON-044)、[CON-046](../docs/02-表格化需求约束.md#CON-046) |
| 核心规范 | [06-调度资源锁与执行租约.md](../../docs/技术开发/06-调度资源锁与执行租约.md) |

## 2. 控制接口归属

| 索引 | operationId | 方法/路径 | 任务 | scope | 事务/幂等 |
| --- | --- | --- | --- | --- | --- |
| INT-002 | cancelJob | POST /jobs/{job_id}/cancel | [DEV-20](../开发细则总表.md#DEV-20) | job:cancel | TX-06 / 必需稳定键 |
| INT-003 | claimLease | POST /runner/leases/claim | [DEV-18](../开发细则总表.md#DEV-18) | device:execute | TX-04 / 必需稳定键 |
| INT-015 | createJob | POST /jobs | [DEV-17](../开发细则总表.md#DEV-17) | job:create | TX-04 / 必需稳定键 |
| INT-018 | createPairingIntent | POST /devices/pairing-intents | [DEV-06](../开发细则总表.md#DEV-06) | device:pair | TX-01_OR_MODULE_LOCAL / 必需稳定键 |
| INT-031 | drainDevice | POST /devices/{device_id}/drain | [DEV-06](../开发细则总表.md#DEV-06) | device:manage | TX-01_OR_MODULE_LOCAL / 必需稳定键 |
| INT-032 | enrollDevice | POST /device-enrollments | [DEV-06](../开发细则总表.md#DEV-06) | pairing:consume | TX-01_OR_MODULE_LOCAL / 必需稳定键 |
| INT-035 | getDevice | GET /devices/{device_id} | [DEV-06](../开发细则总表.md#DEV-06) | device:read | READ_PROJECTION / 见权威契约 |
| INT-037 | getJob | GET /jobs/{job_id} | [DEV-17](../开发细则总表.md#DEV-17) | job:read | READ_PROJECTION / 见权威契约 |
| INT-057 | listOwnDevices | GET /seller/devices | [DEV-06](../开发细则总表.md#DEV-06) | seller:read | READ_PROJECTION / 见权威契约 |
| INT-064 | recordAttemptCleanup | POST /runner/attempts/{attempt_id}/cleanup | [DEV-20](../开发细则总表.md#DEV-20) | device:execute | TX-10 / 必需稳定键 |
| INT-065 | recordAttemptProgress | POST /runner/attempts/{attempt_id}/progress | [DEV-18](../开发细则总表.md#DEV-18) | device:execute | TX-04 / 必需稳定键 |
| INT-067 | recordBuyerReceipt | POST /jobs/{job_id}/receipts | [DEV-19](../开发细则总表.md#DEV-19) | cipher:receipt | TX-06 / 必需稳定键 |
| INT-069 | recordDeviceHeartbeat | POST /devices/{device_id}/heartbeats | [DEV-06](../开发细则总表.md#DEV-06) | device:heartbeat | TX-01_OR_MODULE_LOCAL / 必需稳定键 |
| INT-075 | registerAttemptDelivery | POST /runner/attempts/{attempt_id}/deliveries | [DEV-19](../开发细则总表.md#DEV-19) | device:execute | TX-06 / 必需稳定键 |
| INT-082 | revokeDevice | POST /devices/{device_id}/revoke | [DEV-06](../开发细则总表.md#DEV-06) | device:manage | TX-01_OR_MODULE_LOCAL / 必需稳定键 |
| INT-089 | updateResourcePolicy | PATCH /devices/{device_id}/resource-policy | [DEV-06](../开发细则总表.md#DEV-06) | device:manage | TX-01_OR_MODULE_LOCAL / 必需稳定键 |
| INT-094 | getExecutionManifest | GET /runner/leases/{lease_id}/manifest | [DEV-18](../开发细则总表.md#DEV-18) | device:execute | READ_PROJECTION / 见权威契约 |
| INT-095 | getExecutionInput | GET /runner/leases/{lease_id}/input | [DEV-18](../开发细则总表.md#DEV-18) | device:execute | READ_PROJECTION / 见权威契约 |

[完整控制面、数据表与需求追踪](../docs/04-接口数据与需求追踪.md)；请求/响应 Schema 和响应码沿用既有 OpenAPI。

## 3. 开发任务与子项

<a id="DEV-06"></a>

### DEV-06 设备配对与身份生命周期

| 字段 | 细则 |
| --- | --- |
| 模块/岗位 | [MOD-06](MOD-06-调度资源租约.md) / 调度工程；实际负责人未分配 |
| 计划路径 | `dev/src/internal/dispatch` |
| 范围/状态 | CURRENT / DESIGN / NOT_EXECUTED |
| 依赖 | [DEV-03](../开发细则总表.md#DEV-03)、[DEV-05](../开发细则总表.md#DEV-05) |
| 需求/主验收 | P11-F03、P11-F05、P11-F57、P11-F65、P11-F66；P11-T03、P11-T05、P11-T57、P11-T65、P11-T66 |
| 实施目标 | 实现一次配对、设备凭据、出站心跳、drain/revoke和恢复再登记。 |
| 原完成证据 | 令牌重复消耗拒绝；设备撤销不影响其他合法设备；恢复不复用旧租约。 |
| 门槛 | POC-02、POC-03 |
| 控制操作 | listOwnDevices、createPairingIntent、enrollDevice、getDevice、recordDeviceHeartbeat、drainDevice、revokeDevice、updateResourcePolicy |
| 需求约束 | [CON-001](../docs/02-表格化需求约束.md#CON-001)、[CON-002](../docs/02-表格化需求约束.md#CON-002)、[CON-006](../docs/02-表格化需求约束.md#CON-006)、[CON-007](../docs/02-表格化需求约束.md#CON-007)、[CON-008](../docs/02-表格化需求约束.md#CON-008)、[CON-009](../docs/02-表格化需求约束.md#CON-009)、[CON-013](../docs/02-表格化需求约束.md#CON-013)、[CON-016](../docs/02-表格化需求约束.md#CON-016)、[CON-018](../docs/02-表格化需求约束.md#CON-018)、[CON-019](../docs/02-表格化需求约束.md#CON-019)、[CON-020](../docs/02-表格化需求约束.md#CON-020)、[CON-021](../docs/02-表格化需求约束.md#CON-021)、[CON-022](../docs/02-表格化需求约束.md#CON-022)、[CON-032](../docs/02-表格化需求约束.md#CON-032)、[CON-033](../docs/02-表格化需求约束.md#CON-033)、[CON-036](../docs/02-表格化需求约束.md#CON-036)、[CON-038](../docs/02-表格化需求约束.md#CON-038)、[CON-041](../docs/02-表格化需求约束.md#CON-041)、[CON-042](../docs/02-表格化需求约束.md#CON-042)、[CON-043](../docs/02-表格化需求约束.md#CON-043)、[CON-044](../docs/02-表格化需求约束.md#CON-044)、[CON-046](../docs/02-表格化需求约束.md#CON-046) |
| 统一交付 | 适用 CHK-01—08；不适用项注明理由；证据规则见 DOC-06 |

| 子项编号 | 开发项 | 实现要求与失败边界 | 交付/证据 |
| --- | --- | --- | --- |
| DEV-06.01 | 设备配对 | 短时一次性token绑定本人seller/设备，重复或过期拒绝 | 配对结果和重用负向记录 |
| DEV-06.02 | 设备身份 | 设备认证与内容接收钥分开，重装/转让新身份 | 身份撤销/恢复及钥用途对照 |
| DEV-06.03 | 在线生命周期 | 出站连接、心跳、共享时段与drain状态，不以在线代表空闲/可售 | 设备状态投影与失联处理 |
| DEV-06.04 | 恢复先核权 | 重连核最高围栏/旧后端/清理待办后再允许新售 | 失联/重启/撤证恢复报告 |

NB-R1需求增量：NB-R001、NB-R019、NB-R020、NB-R022；[正反向验收及契约缺口](../../docs/需求推进/NB-1.0/06-需求追踪与验收矩阵.md)。源码实施见进度登记，子场景仍未执行。

规范来源：[06-调度资源锁与执行租约.md](../../docs/技术开发/06-调度资源锁与执行租约.md)；[21-开发任务分解与需求追踪.md](../../docs/技术开发/21-开发任务分解与需求追踪.md)；[18-测试工程与故障验证.md](../../docs/技术开发/18-测试工程与故障验证.md)；[附录-本轮范围与端侧选型修订.md](../../docs/技术开发/附录-本轮范围与端侧选型修订.md)；[附录-原生提供方控制代理与本地恢复.md](../../docs/技术开发/附录-原生提供方控制代理与本地恢复.md)；[附录-提供方双端设备管理与原请求恢复.md](../../docs/技术开发/附录-提供方双端设备管理与原请求恢复.md)。

<a id="DEV-17"></a>

### DEV-17 任务上下文与输入准入

| 字段 | 细则 |
| --- | --- |
| 模块/岗位 | [MOD-06](MOD-06-调度资源租约.md) / 调度工程；实际负责人未分配 |
| 计划路径 | `dev/src/internal/dispatch` |
| 范围/状态 | CURRENT / DESIGN / NOT_EXECUTED |
| 依赖 | [DEV-10](../开发细则总表.md#DEV-10)、[DEV-12](../开发细则总表.md#DEV-12)、[DEV-15](../开发细则总表.md#DEV-15)、[DEV-07](../开发细则总表.md#DEV-07) |
| 需求/主验收 | P11-F10、P11-F11、P11-F33、P11-F34、P11-F36、P11-F54；P11-T10、P11-T11、P11-T33、P11-T34、P11-T36、P11-T54 |
| 实施目标 | 实现INPUT_PENDING/PREPARED和输入/封装、资金/证据/容量短事务准入。 |
| 原完成证据 | PREPARED不执行不占GPU；输入未就绪不能签ISSUED；总deadline不因等待延长。 |
| 门槛 | POC-05 |
| 控制操作 | createJob、getJob |
| 需求约束 | [CON-001](../docs/02-表格化需求约束.md#CON-001)、[CON-002](../docs/02-表格化需求约束.md#CON-002)、[CON-006](../docs/02-表格化需求约束.md#CON-006)、[CON-007](../docs/02-表格化需求约束.md#CON-007)、[CON-008](../docs/02-表格化需求约束.md#CON-008)、[CON-009](../docs/02-表格化需求约束.md#CON-009)、[CON-013](../docs/02-表格化需求约束.md#CON-013)、[CON-016](../docs/02-表格化需求约束.md#CON-016)、[CON-018](../docs/02-表格化需求约束.md#CON-018)、[CON-019](../docs/02-表格化需求约束.md#CON-019)、[CON-020](../docs/02-表格化需求约束.md#CON-020)、[CON-021](../docs/02-表格化需求约束.md#CON-021)、[CON-022](../docs/02-表格化需求约束.md#CON-022)、[CON-032](../docs/02-表格化需求约束.md#CON-032)、[CON-033](../docs/02-表格化需求约束.md#CON-033)、[CON-036](../docs/02-表格化需求约束.md#CON-036)、[CON-038](../docs/02-表格化需求约束.md#CON-038)、[CON-041](../docs/02-表格化需求约束.md#CON-041)、[CON-042](../docs/02-表格化需求约束.md#CON-042)、[CON-043](../docs/02-表格化需求约束.md#CON-043)、[CON-044](../docs/02-表格化需求约束.md#CON-044)、[CON-046](../docs/02-表格化需求约束.md#CON-046) |
| 统一交付 | 适用 CHK-01—08；不适用项注明理由；证据规则见 DOC-06 |

| 子项编号 | 开发项 | 实现要求与失败边界 | 交付/证据 |
| --- | --- | --- | --- |
| DEV-17.01 | 准备上下文 | E2E建INPUT_PENDING/PREPARED、attempt/token/recipient/截止 | 不占GPU/无执行授权记录 |
| DEV-17.02 | 输入准入 | 核资产归属/完成、封装签名/引用/限额，平台不验私密内容 | 坏引用/旧token/超限拒绝 |
| DEV-17.03 | 综合复核 | 资金/资格/环境/地区/预算/期限/容量都重核 | 并发证据变更/到期测试 |
| DEV-17.04 | 事务提交 | 短事务准备准入/outbox，外部引擎/支付不持锁调用 | 中断点/重复准入/等待恢复 |

规范来源：[06-调度资源锁与执行租约.md](../../docs/技术开发/06-调度资源锁与执行租约.md)；[21-开发任务分解与需求追踪.md](../../docs/技术开发/21-开发任务分解与需求追踪.md)；[18-测试工程与故障验证.md](../../docs/技术开发/18-测试工程与故障验证.md)；[附录-本轮范围与端侧选型修订.md](../../docs/技术开发/附录-本轮范围与端侧选型修订.md)。

<a id="DEV-18"></a>

### DEV-18 资源门闩与双围栏

| 字段 | 细则 |
| --- | --- |
| 模块/岗位 | [MOD-06](MOD-06-调度资源租约.md) / 调度工程；实际负责人未分配 |
| 计划路径 | `dev/src/internal/dispatch` |
| 范围/状态 | CURRENT / DESIGN / NOT_EXECUTED |
| 依赖 | [DEV-17](../开发细则总表.md#DEV-17)、[DEV-02](../开发细则总表.md#DEV-02) |
| 需求/主验收 | P11-F05、P11-F10、P11-F11、P11-F41、P11-F64；P11-T05、P11-T10、P11-T11、P11-T41、P11-T64 |
| 实施目标 | 实现每物理资源全模式/引擎锁、job token、跨job lock_generation、签名租约与版本化续约。输入准备/结果密文交付不额外持GPU锁，编码仍用GPU则继续持锁；控制通道独立。 |
| 原完成证据 | 并发买卖角色/引擎/自用/租赁争同卡唯一；旧资源代次/旧续约重放拒绝，失联不延硬截止。旧attempt回调不能清下一资源owner，租赁子任务不释放会话锁。 |
| 门槛 | POC-05 |
| 控制操作 | claimLease、getExecutionManifest、getExecutionInput、recordAttemptProgress |
| 需求约束 | [CON-001](../docs/02-表格化需求约束.md#CON-001)、[CON-002](../docs/02-表格化需求约束.md#CON-002)、[CON-006](../docs/02-表格化需求约束.md#CON-006)、[CON-007](../docs/02-表格化需求约束.md#CON-007)、[CON-008](../docs/02-表格化需求约束.md#CON-008)、[CON-009](../docs/02-表格化需求约束.md#CON-009)、[CON-013](../docs/02-表格化需求约束.md#CON-013)、[CON-016](../docs/02-表格化需求约束.md#CON-016)、[CON-018](../docs/02-表格化需求约束.md#CON-018)、[CON-019](../docs/02-表格化需求约束.md#CON-019)、[CON-020](../docs/02-表格化需求约束.md#CON-020)、[CON-021](../docs/02-表格化需求约束.md#CON-021)、[CON-022](../docs/02-表格化需求约束.md#CON-022)、[CON-032](../docs/02-表格化需求约束.md#CON-032)、[CON-033](../docs/02-表格化需求约束.md#CON-033)、[CON-036](../docs/02-表格化需求约束.md#CON-036)、[CON-038](../docs/02-表格化需求约束.md#CON-038)、[CON-041](../docs/02-表格化需求约束.md#CON-041)、[CON-042](../docs/02-表格化需求约束.md#CON-042)、[CON-043](../docs/02-表格化需求约束.md#CON-043)、[CON-044](../docs/02-表格化需求约束.md#CON-044)、[CON-046](../docs/02-表格化需求约束.md#CON-046) |
| 统一交付 | 适用 CHK-01—08；不适用项注明理由；证据规则见 DOC-06 |

| 子项编号 | 开发项 | 实现要求与失败边界 | 交付/证据 |
| --- | --- | --- | --- |
| DEV-18.01 | 物理资源标识 | 一卡一resource，所有引擎/自用/租赁/维护共门闩 | 多入口争抢和真实占用识别 |
| DEV-18.02 | 双围栏签名 | job token和跨job资源代次签入租约，核owner/环境/硬截止 | 旧代次/错环境/回绕拒绝 |
| DEV-18.03 | 续约恢复 | 有效新revision才续权，持久最高值，重启先查后端 | 迟到续约/时钟回拨/崩溃报告 |
| DEV-18.04 | 计算交付分离 | 输入准备/原密文补传不占卡，编码用GPU仍持锁 | 新owner不受旧cleanup影响、旧交付可签收 |

NB-R1需求增量：NB-R013、NB-R022、NB-R046；[正反向验收及契约缺口](../../docs/需求推进/NB-1.0/06-需求追踪与验收矩阵.md)。源码实施见进度登记，子场景仍未执行。

规范来源：[06-调度资源锁与执行租约.md](../../docs/技术开发/06-调度资源锁与执行租约.md)；[21-开发任务分解与需求追踪.md](../../docs/技术开发/21-开发任务分解与需求追踪.md)；[18-测试工程与故障验证.md](../../docs/技术开发/18-测试工程与故障验证.md)；[附录-本轮范围与端侧选型修订.md](../../docs/技术开发/附录-本轮范围与端侧选型修订.md)；[附录-供给端内核封装与替换规范.md](../../docs/技术开发/附录-供给端内核封装与替换规范.md)；[附录-低开销隐私执行完整方案.md](../../docs/技术开发/附录-低开销隐私执行完整方案.md)；[08-端到端加密协议与密钥实现设计.md](../../docs/技术开发/08-端到端加密协议与密钥实现设计.md)。

<a id="DEV-19"></a>

### DEV-19 同卖家重派及唯一接受

| 字段 | 细则 |
| --- | --- |
| 模块/岗位 | [MOD-06](MOD-06-调度资源租约.md) / 调度工程；实际负责人未分配 |
| 计划路径 | `dev/src/internal/dispatch` |
| 范围/状态 | CURRENT / DESIGN / NOT_EXECUTED |
| 依赖 | [DEV-18](../开发细则总表.md#DEV-18) |
| 需求/主验收 | P11-F14、P11-F16、P11-F35、P11-F36、P11-F39；P11-T14、P11-T16、P11-T35、P11-T36、P11-T39 |
| 实施目标 | 实现一次同卖家重派、换端重封、当前尝试交付/签收唯一CAS和取消竞争。 |
| 原完成证据 | 旧token交付拒绝、重传不二次接受；跨卖家重选明确新报价新单。 |
| 门槛 | POC-05、POC-04 |
| 控制操作 | recordBuyerReceipt、registerAttemptDelivery |
| 需求约束 | [CON-001](../docs/02-表格化需求约束.md#CON-001)、[CON-002](../docs/02-表格化需求约束.md#CON-002)、[CON-006](../docs/02-表格化需求约束.md#CON-006)、[CON-007](../docs/02-表格化需求约束.md#CON-007)、[CON-008](../docs/02-表格化需求约束.md#CON-008)、[CON-009](../docs/02-表格化需求约束.md#CON-009)、[CON-013](../docs/02-表格化需求约束.md#CON-013)、[CON-016](../docs/02-表格化需求约束.md#CON-016)、[CON-018](../docs/02-表格化需求约束.md#CON-018)、[CON-019](../docs/02-表格化需求约束.md#CON-019)、[CON-020](../docs/02-表格化需求约束.md#CON-020)、[CON-021](../docs/02-表格化需求约束.md#CON-021)、[CON-022](../docs/02-表格化需求约束.md#CON-022)、[CON-032](../docs/02-表格化需求约束.md#CON-032)、[CON-033](../docs/02-表格化需求约束.md#CON-033)、[CON-036](../docs/02-表格化需求约束.md#CON-036)、[CON-038](../docs/02-表格化需求约束.md#CON-038)、[CON-041](../docs/02-表格化需求约束.md#CON-041)、[CON-042](../docs/02-表格化需求约束.md#CON-042)、[CON-043](../docs/02-表格化需求约束.md#CON-043)、[CON-044](../docs/02-表格化需求约束.md#CON-044)、[CON-046](../docs/02-表格化需求约束.md#CON-046) |
| 统一交付 | 适用 CHK-01—08；不适用项注明理由；证据规则见 DOC-06 |

| 子项编号 | 开发项 | 实现要求与失败边界 | 交付/证据 |
| --- | --- | --- | --- |
| DEV-19.01 | 受限重派 | 同seller已披露合格端最多一次，保持预算/总期限 | 同卖家单端/跨卖家/超次拒绝 |
| DEV-19.02 | 加密换端 | 旧执行上下文围栏，新端独立核验并买方重新封装 | 离线放钥超时/错接收端拒绝 |
| DEV-19.03 | 唯一接受 | 交付版/清单/签名和接受拒绝取消超时CAS竞争 | 并发唯一acceptance与财务去重 |
| DEV-19.04 | 流和未知 | 新生成不拼原SSE，执行未知先query，晚结果不改正式终态 | 旧结果/中断流/重复投递报告 |

NB-R1需求增量：NB-R039、NB-R040；[正反向验收及契约缺口](../../docs/需求推进/NB-1.0/06-需求追踪与验收矩阵.md)。源码实施见进度登记，子场景仍未执行。

规范来源：[06-调度资源锁与执行租约.md](../../docs/技术开发/06-调度资源锁与执行租约.md)；[21-开发任务分解与需求追踪.md](../../docs/技术开发/21-开发任务分解与需求追踪.md)；[18-测试工程与故障验证.md](../../docs/技术开发/18-测试工程与故障验证.md)；[附录-本轮范围与端侧选型修订.md](../../docs/技术开发/附录-本轮范围与端侧选型修订.md)。

<a id="DEV-20"></a>

### DEV-20 取消停止与清理确认

| 字段 | 细则 |
| --- | --- |
| 模块/岗位 | [MOD-06](MOD-06-调度资源租约.md) / 调度工程；实际负责人未分配 |
| 计划路径 | `dev/src/internal/dispatch` |
| 范围/状态 | CURRENT / DESIGN / NOT_EXECUTED |
| 依赖 | [DEV-18](../开发细则总表.md#DEV-18)、[DEV-19](../开发细则总表.md#DEV-19) |
| 需求/主验收 | P11-F05、P11-F17、P11-F22、P11-F47、P11-F52、P11-F59；P11-T05、P11-T17、P11-T22、P11-T47、P11-T52、P11-T59 |
| 实施目标 | 实现取消意向→撤租约→真实停止→清理ACK→释放；未知状态资源隔离。 |
| 原完成证据 | TTL/进程崩溃不直接FREE；取消不误杀他人；离线重连补清不复活旧执行。 |
| 门槛 | POC-05、POC-10 |
| 控制操作 | cancelJob、recordAttemptCleanup |
| 需求约束 | [CON-001](../docs/02-表格化需求约束.md#CON-001)、[CON-002](../docs/02-表格化需求约束.md#CON-002)、[CON-006](../docs/02-表格化需求约束.md#CON-006)、[CON-007](../docs/02-表格化需求约束.md#CON-007)、[CON-008](../docs/02-表格化需求约束.md#CON-008)、[CON-009](../docs/02-表格化需求约束.md#CON-009)、[CON-013](../docs/02-表格化需求约束.md#CON-013)、[CON-016](../docs/02-表格化需求约束.md#CON-016)、[CON-018](../docs/02-表格化需求约束.md#CON-018)、[CON-019](../docs/02-表格化需求约束.md#CON-019)、[CON-020](../docs/02-表格化需求约束.md#CON-020)、[CON-021](../docs/02-表格化需求约束.md#CON-021)、[CON-022](../docs/02-表格化需求约束.md#CON-022)、[CON-032](../docs/02-表格化需求约束.md#CON-032)、[CON-033](../docs/02-表格化需求约束.md#CON-033)、[CON-036](../docs/02-表格化需求约束.md#CON-036)、[CON-038](../docs/02-表格化需求约束.md#CON-038)、[CON-041](../docs/02-表格化需求约束.md#CON-041)、[CON-042](../docs/02-表格化需求约束.md#CON-042)、[CON-043](../docs/02-表格化需求约束.md#CON-043)、[CON-044](../docs/02-表格化需求约束.md#CON-044)、[CON-046](../docs/02-表格化需求约束.md#CON-046) |
| 统一交付 | 适用 CHK-01—08；不适用项注明理由；证据规则见 DOC-06 |

| 子项编号 | 开发项 | 实现要求与失败边界 | 交付/证据 |
| --- | --- | --- | --- |
| DEV-20.01 | 撤执行权 | 取消/到期先拒新调用，签名命令对应原owner/attempt | 取消鉴权与重复命令记录 |
| DEV-20.02 | 真实停算 | 触发owned worker检查点/受控停止，不能把HTTP断开当停算 | GPU完成确认及不能误杀个人进程 |
| DEV-20.03 | 清理ACK | 任务KV/张量/文件/钥按范围清理并签确认 | 下一任务隔离与清理失败场景 |
| DEV-20.04 | 隔离恢复 | 未停/未知资源隔离，重连先查停清再恢复FREE | TTL到期不提前复用证据 |

NB-R1需求增量：NB-R016、NB-R017、NB-R046；[正反向验收及契约缺口](../../docs/需求推进/NB-1.0/06-需求追踪与验收矩阵.md)。源码实施见进度登记，子场景仍未执行。

规范来源：[06-调度资源锁与执行租约.md](../../docs/技术开发/06-调度资源锁与执行租约.md)；[21-开发任务分解与需求追踪.md](../../docs/技术开发/21-开发任务分解与需求追踪.md)；[18-测试工程与故障验证.md](../../docs/技术开发/18-测试工程与故障验证.md)；[附录-本轮范围与端侧选型修订.md](../../docs/技术开发/附录-本轮范围与端侧选型修订.md)；[附录-供给端内核封装与替换规范.md](../../docs/技术开发/附录-供给端内核封装与替换规范.md)。

## 4. 需求与主验收覆盖

| 需求/主验收 | 功能 | 优先级/范围 | 关联任务 |
| --- | --- | --- | --- |
| P11-F03 / P11-T03 | 客户端安装与设备注册 | P0 / CURRENT | [DEV-06](../开发细则总表.md#DEV-06)、[DEV-21](../开发细则总表.md#DEV-21)、[DEV-41](../开发细则总表.md#DEV-41) |
| P11-F05 / P11-T05 | 资源、时段和停止共享 | P0 / CURRENT | [DEV-06](../开发细则总表.md#DEV-06)、[DEV-08](../开发细则总表.md#DEV-08)、[DEV-18](../开发细则总表.md#DEV-18)、[DEV-20](../开发细则总表.md#DEV-20)、[DEV-21](../开发细则总表.md#DEV-21)、[DEV-24](../开发细则总表.md#DEV-24)、[DEV-43](../开发细则总表.md#DEV-43)、[DEV-46](../开发细则总表.md#DEV-46) |
| P11-F10 / P11-T10 | 按卖家归属匹配与容量控制 | P0 / CURRENT | [DEV-02](../开发细则总表.md#DEV-02)、[DEV-17](../开发细则总表.md#DEV-17)、[DEV-18](../开发细则总表.md#DEV-18)、[DEV-54](../开发细则总表.md#DEV-54) |
| P11-F11 / P11-T11 | 领取租约与心跳 | P0 / CURRENT | [DEV-17](../开发细则总表.md#DEV-17)、[DEV-18](../开发细则总表.md#DEV-18)、[DEV-51](../开发细则总表.md#DEV-51)、[DEV-54](../开发细则总表.md#DEV-54) |
| P11-F14 / P11-T14 | 产物验收与单次有效交付 | P0 / CURRENT | [DEV-02](../开发细则总表.md#DEV-02)、[DEV-19](../开发细则总表.md#DEV-19)、[DEV-39](../开发细则总表.md#DEV-39)、[DEV-54](../开发细则总表.md#DEV-54) |
| P11-F16 / P11-T16 | 同卖家重派、跨卖家重选与超时 | P0 / CURRENT | [DEV-10](../开发细则总表.md#DEV-10)、[DEV-19](../开发细则总表.md#DEV-19)、[DEV-51](../开发细则总表.md#DEV-51)、[DEV-54](../开发细则总表.md#DEV-54) |
| P11-F17 / P11-T17 | 取消与退款 | P0 / CURRENT | [DEV-13](../开发细则总表.md#DEV-13)、[DEV-14](../开发细则总表.md#DEV-14)、[DEV-20](../开发细则总表.md#DEV-20)、[DEV-45](../开发细则总表.md#DEV-45)、[DEV-51](../开发细则总表.md#DEV-51) |
| P11-F22 / P11-T22 | 内容保护、清理与设备退出 | P0 / CURRENT | [DEV-04](../开发细则总表.md#DEV-04)、[DEV-20](../开发细则总表.md#DEV-20)、[DEV-36](../开发细则总表.md#DEV-36)、[DEV-37](../开发细则总表.md#DEV-37)、[DEV-51](../开发细则总表.md#DEV-51)、[DEV-53](../开发细则总表.md#DEV-53)、[DEV-55](../开发细则总表.md#DEV-55) |
| P11-F26 / P11-T26 | 订阅窗口、保留量与容量 | P1-AUTH / CURRENT | [DEV-25](../开发细则总表.md#DEV-25) |
| P11-F33 / P11-T33 | 买家加密端与端点身份 | P0 / CURRENT | [DEV-17](../开发细则总表.md#DEV-17)、[DEV-29](../开发细则总表.md#DEV-29)、[DEV-30](../开发细则总表.md#DEV-30)、[DEV-44](../开发细则总表.md#DEV-44) |
| P11-F34 / P11-T34 | 密文对象与加密分块 | P0 / CURRENT | [DEV-17](../开发细则总表.md#DEV-17)、[DEV-30](../开发细则总表.md#DEV-30)、[DEV-31](../开发细则总表.md#DEV-31)、[DEV-37](../开发细则总表.md#DEV-37) |
| P11-F35 / P11-T35 | 加密执行、回传与本地验收 | P0 / CURRENT | [DEV-19](../开发细则总表.md#DEV-19)、[DEV-30](../开发细则总表.md#DEV-30)、[DEV-31](../开发细则总表.md#DEV-31)、[DEV-44](../开发细则总表.md#DEV-44) |
| P11-F36 / P11-T36 | 同卖家加密换端与放钥 | P0 / CURRENT | [DEV-17](../开发细则总表.md#DEV-17)、[DEV-19](../开发细则总表.md#DEV-19)、[DEV-29](../开发细则总表.md#DEV-29) |
| P11-F39 / P11-T39 | 加密接受与C2C风险分担 | P0 / CURRENT | [DEV-19](../开发细则总表.md#DEV-19)、[DEV-31](../开发细则总表.md#DEV-31)、[DEV-39](../开发细则总表.md#DEV-39)、[DEV-45](../开发细则总表.md#DEV-45)、[DEV-49](../开发细则总表.md#DEV-49) |
| P11-F41 / P11-T41 | 租赁供给、环境与独占锁 | P0 / CURRENT | [DEV-18](../开发细则总表.md#DEV-18)、[DEV-33](../开发细则总表.md#DEV-33) |
| P11-F47 / P11-T47 | 结束撤权、清理与再出租 | P0 / CURRENT | [DEV-20](../开发细则总表.md#DEV-20)、[DEV-36](../开发细则总表.md#DEV-36)、[DEV-54](../开发细则总表.md#DEV-54) |
| P11-F52 / P11-T52 | 分模式留存与删除 | P0 / PARTIAL | [DEV-04](../开发细则总表.md#DEV-04)、[DEV-20](../开发细则总表.md#DEV-20)、[DEV-36](../开发细则总表.md#DEV-36)、[DEV-37](../开发细则总表.md#DEV-37)、[DEV-51](../开发细则总表.md#DEV-51)、[DEV-53](../开发细则总表.md#DEV-53)、[DEV-55](../开发细则总表.md#DEV-55) |
| P11-F54 / P11-T54 | 模式化接口、SDK与错误 | P0 / CURRENT | [DEV-10](../开发细则总表.md#DEV-10)、[DEV-11](../开发细则总表.md#DEV-11)、[DEV-17](../开发细则总表.md#DEV-17)、[DEV-26](../开发细则总表.md#DEV-26)、[DEV-47](../开发细则总表.md#DEV-47) |
| P11-F57 / P11-T57 | 多端与运行时能力登记 | P0 / CURRENT | [DEV-06](../开发细则总表.md#DEV-06)、[DEV-07](../开发细则总表.md#DEV-07)、[DEV-41](../开发细则总表.md#DEV-41)、[DEV-46](../开发细则总表.md#DEV-46) |
| P11-F59 / P11-T59 | 统一引擎适配协议 | P0 / CURRENT | [DEV-20](../开发细则总表.md#DEV-20)、[DEV-22](../开发细则总表.md#DEV-22)、[DEV-23](../开发细则总表.md#DEV-23)、[DEV-25](../开发细则总表.md#DEV-25)、[DEV-42](../开发细则总表.md#DEV-42) |
| P11-F64 / P11-T64 | 自用与共享统一资源槽 | P0 / CURRENT | [DEV-18](../开发细则总表.md#DEV-18)、[DEV-24](../开发细则总表.md#DEV-24)、[DEV-42](../开发细则总表.md#DEV-42)、[DEV-43](../开发细则总表.md#DEV-43) |
| P11-F65 / P11-T65 | 多端出站连接与凭据 | P0 / PARTIAL | [DEV-06](../开发细则总表.md#DEV-06)、[DEV-21](../开发细则总表.md#DEV-21)、[DEV-24](../开发细则总表.md#DEV-24)、[DEV-43](../开发细则总表.md#DEV-43)、[DEV-55](../开发细则总表.md#DEV-55) |
| P11-F66 / P11-T66 | 移动隐私、清数据与身份恢复 | P0 / CURRENT | [DEV-04](../开发细则总表.md#DEV-04)、[DEV-06](../开发细则总表.md#DEV-06)、[DEV-44](../开发细则总表.md#DEV-44)、[DEV-53](../开发细则总表.md#DEV-53) |

## 5. 完成规则

适用 CHK-01—08，分别提交实现、失败恢复、权限/幂等和真实证据。模块代码完成不意味着对应组合可售；无证据的引擎、许可、隐私档位、支付资格保持关闭。[验收交付规则](../docs/06-验收交付与证据规则.md)。
