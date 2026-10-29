# MOD-13 文件媒体开发细则

P11｜开发细则 v1.0｜MOD-13｜2026-10-30｜需求 v3.0 / 技术 v1.0。

> 由 dev/planning/开发细则源数据.py 与既有技术契约生成；修改源后重建，不手改本文件。本套为冻结设计拆解：55 任务 DESIGN、DEV-55 DEFERRED；真实源码进展见 dev/progress/README.md，完整产品/设备/支付/密码/性能验收不能据此记为通过。

[返回开发总表](../开发细则总表.md)；[模块边界总表](../docs/01-模块划分与职责边界.md)。

## 1. 职责与边界

| 字段 | 约定 |
| --- | --- |
| 职责 | 统一媒体、对象权限与运行删除 |
| 输入 | 受权上传/读取请求、对象类型/规格 |
| 输出 | 对象/短时grant/完整性/清理结果 |
| 禁止承担 | 不解码E2E密文、不任意URL/路径或无界展开 |
| 岗位/人员 | 存储后端；实际负责人/工期未分配 |
| 计划路径 | dev/src/internal/assets |
| 协作模块 | [MOD-01](MOD-01-身份授权.md)、[MOD-04](MOD-04-合同订单路由预算.md) |
| 应用服务 | AssetRegistry、TransferGrant、LifecycleCleaner |
| 权威表 | assets、transfer_grants、deletion_tasks |
| 适用约束 | [CON-001](../docs/02-表格化需求约束.md#CON-001)、[CON-003](../docs/02-表格化需求约束.md#CON-003)、[CON-006](../docs/02-表格化需求约束.md#CON-006)、[CON-007](../docs/02-表格化需求约束.md#CON-007)、[CON-008](../docs/02-表格化需求约束.md#CON-008)、[CON-009](../docs/02-表格化需求约束.md#CON-009)、[CON-013](../docs/02-表格化需求约束.md#CON-013)、[CON-014](../docs/02-表格化需求约束.md#CON-014)、[CON-022](../docs/02-表格化需求约束.md#CON-022)、[CON-023](../docs/02-表格化需求约束.md#CON-023)、[CON-025](../docs/02-表格化需求约束.md#CON-025)、[CON-026](../docs/02-表格化需求约束.md#CON-026)、[CON-027](../docs/02-表格化需求约束.md#CON-027)、[CON-030](../docs/02-表格化需求约束.md#CON-030)、[CON-033](../docs/02-表格化需求约束.md#CON-033)、[CON-034](../docs/02-表格化需求约束.md#CON-034)、[CON-037](../docs/02-表格化需求约束.md#CON-037)、[CON-038](../docs/02-表格化需求约束.md#CON-038)、[CON-041](../docs/02-表格化需求约束.md#CON-041)、[CON-042](../docs/02-表格化需求约束.md#CON-042)、[CON-043](../docs/02-表格化需求约束.md#CON-043)、[CON-046](../docs/02-表格化需求约束.md#CON-046) |
| 核心规范 | [15-文件媒体与对象存储.md](../../docs/技术开发/15-文件媒体与对象存储.md) |

## 2. 控制接口归属

| 索引 | operationId | 方法/路径 | 任务 | scope | 事务/幂等 |
| --- | --- | --- | --- | --- | --- |
| INT-004 | completeAsset | POST /assets/{asset_id}/complete | [DEV-37](../开发细则总表.md#DEV-37) | asset:write | TX-01_OR_MODULE_LOCAL / 必需稳定键 |
| INT-008 | createAssetTransferGrant | POST /assets/{asset_id}/transfer-grants | [DEV-37](../开发细则总表.md#DEV-37) | asset:transfer | TX-01_OR_MODULE_LOCAL / 必需稳定键 |
| INT-033 | getAsset | GET /assets/{asset_id} | [DEV-37](../开发细则总表.md#DEV-37) | asset:read | READ_PROJECTION / 见权威契约 |
| INT-060 | listResults | GET /jobs/{job_id}/results | [DEV-37](../开发细则总表.md#DEV-37) | asset:read | READ_PROJECTION / 见权威契约 |
| INT-074 | registerAsset | POST /assets | [DEV-37](../开发细则总表.md#DEV-37) | asset:write | TX-01_OR_MODULE_LOCAL / 必需稳定键 |

[完整控制面、数据表与需求追踪](../docs/04-接口数据与需求追踪.md)；请求/响应 Schema 和响应码沿用既有 OpenAPI。

## 3. 开发任务与子项

<a id="DEV-37"></a>

### DEV-37 媒体对象生命周期

| 字段 | 细则 |
| --- | --- |
| 模块/岗位 | [MOD-13](MOD-13-文件媒体.md) / 存储后端；实际负责人未分配 |
| 计划路径 | `dev/src/internal/assets` |
| 范围/状态 | CURRENT / DESIGN / NOT_EXECUTED |
| 依赖 | [DEV-04](../开发细则总表.md#DEV-04)、[DEV-10](../开发细则总表.md#DEV-10) |
| 需求/主验收 | P11-F13、P11-F15、P11-F22、P11-F34、P11-F49、P11-F52、P11-F76；P11-T13、P11-T15、P11-T22、P11-T34、P11-T49、P11-T52、P11-T76 |
| 实施目标 | 实现单对象授权/明密文隔离/受控取回、图像视频音频3D规格检查与运行删除；3D拒脚本/任意外部纹理，音频真实解码。备份方案延后。增加图/视频/音频/3D流式编码加密、不可变分段缓存与spool配额。 |
| 原完成证据 | 坏包/超限/解析炸弹和3D外链拒绝；密文不解码，Responses资产描述不等于真实产物通过，未清不可复用。不增加全片RAM/帧副本或普通文件中转，格式与标准SDK按合同验收。 |
| 门槛 | POC-09、E12 |
| 控制操作 | registerAsset、getAsset、completeAsset、createAssetTransferGrant、listResults |
| 需求约束 | [CON-001](../docs/02-表格化需求约束.md#CON-001)、[CON-003](../docs/02-表格化需求约束.md#CON-003)、[CON-006](../docs/02-表格化需求约束.md#CON-006)、[CON-007](../docs/02-表格化需求约束.md#CON-007)、[CON-008](../docs/02-表格化需求约束.md#CON-008)、[CON-009](../docs/02-表格化需求约束.md#CON-009)、[CON-013](../docs/02-表格化需求约束.md#CON-013)、[CON-014](../docs/02-表格化需求约束.md#CON-014)、[CON-022](../docs/02-表格化需求约束.md#CON-022)、[CON-023](../docs/02-表格化需求约束.md#CON-023)、[CON-025](../docs/02-表格化需求约束.md#CON-025)、[CON-026](../docs/02-表格化需求约束.md#CON-026)、[CON-027](../docs/02-表格化需求约束.md#CON-027)、[CON-030](../docs/02-表格化需求约束.md#CON-030)、[CON-033](../docs/02-表格化需求约束.md#CON-033)、[CON-034](../docs/02-表格化需求约束.md#CON-034)、[CON-037](../docs/02-表格化需求约束.md#CON-037)、[CON-038](../docs/02-表格化需求约束.md#CON-038)、[CON-041](../docs/02-表格化需求约束.md#CON-041)、[CON-042](../docs/02-表格化需求约束.md#CON-042)、[CON-043](../docs/02-表格化需求约束.md#CON-043)、[CON-046](../docs/02-表格化需求约束.md#CON-046) |
| 统一交付 | 适用 CHK-01—08；不适用项注明理由；证据规则见 DOC-06 |

| 子项编号 | 开发项 | 实现要求与失败边界 | 交付/证据 |
| --- | --- | --- | --- |
| DEV-37.01 | 对象权限 | 登记owner/order/attempt/类型/限额，单对象短时上传/读grant | 跨对象/猜ID/撤权/过期拒绝 |
| DEV-37.02 | 完整性与格式 | 完成核hash/长度/manifest，再按公开或端侧路径验证媒体 | 坏包/截断/解码炸弹/3D外链报告 |
| DEV-37.03 | 隐私流式存储 | 首次写盘前加密、不可变分段/加密manifest、有界spool/LRU | 无普通PNG/帧目录/MP4及磁盘满测试 |
| DEV-37.04 | 删除恢复 | 终态留存到期tombstone再删，离线补清，不复活权益 | 删除重试/授权失效/争议副本范围 |

规范来源：[15-文件媒体与对象存储.md](../../docs/技术开发/15-文件媒体与对象存储.md)；[21-开发任务分解与需求追踪.md](../../docs/技术开发/21-开发任务分解与需求追踪.md)；[18-测试工程与故障验证.md](../../docs/技术开发/18-测试工程与故障验证.md)；[附录-本轮范围与端侧选型修订.md](../../docs/技术开发/附录-本轮范围与端侧选型修订.md)；[附录-供给端内核封装与替换规范.md](../../docs/技术开发/附录-供给端内核封装与替换规范.md)；[附录-低开销隐私执行完整方案.md](../../docs/技术开发/附录-低开销隐私执行完整方案.md)；[08-端到端加密协议与密钥实现设计.md](../../docs/技术开发/08-端到端加密协议与密钥实现设计.md)。

## 4. 需求与主验收覆盖

| 需求/主验收 | 功能 | 优先级/范围 | 关联任务 |
| --- | --- | --- | --- |
| P11-F13 / P11-T13 | 输入传输、结果上传与规范化 | P0 / CURRENT | [DEV-22](../开发细则总表.md#DEV-22)、[DEV-27](../开发细则总表.md#DEV-27)、[DEV-37](../开发细则总表.md#DEV-37) |
| P11-F15 / P11-T15 | 结果领取与通知 | P0 / CURRENT | [DEV-37](../开发细则总表.md#DEV-37)、[DEV-45](../开发细则总表.md#DEV-45)、[DEV-51](../开发细则总表.md#DEV-51) |
| P11-F22 / P11-T22 | 内容保护、清理与设备退出 | P0 / CURRENT | [DEV-04](../开发细则总表.md#DEV-04)、[DEV-20](../开发细则总表.md#DEV-20)、[DEV-36](../开发细则总表.md#DEV-36)、[DEV-37](../开发细则总表.md#DEV-37)、[DEV-51](../开发细则总表.md#DEV-51)、[DEV-53](../开发细则总表.md#DEV-53)、[DEV-55](../开发细则总表.md#DEV-55) |
| P11-F34 / P11-T34 | 密文对象与加密分块 | P0 / CURRENT | [DEV-17](../开发细则总表.md#DEV-17)、[DEV-30](../开发细则总表.md#DEV-30)、[DEV-31](../开发细则总表.md#DEV-31)、[DEV-37](../开发细则总表.md#DEV-37) |
| P11-F49 / P11-T49 | 明文预检与内容验收管线 | P0 / CURRENT | [DEV-27](../开发细则总表.md#DEV-27)、[DEV-37](../开发细则总表.md#DEV-37) |
| P11-F52 / P11-T52 | 分模式留存与删除 | P0 / PARTIAL | [DEV-04](../开发细则总表.md#DEV-04)、[DEV-20](../开发细则总表.md#DEV-20)、[DEV-36](../开发细则总表.md#DEV-36)、[DEV-37](../开发细则总表.md#DEV-37)、[DEV-51](../开发细则总表.md#DEV-51)、[DEV-53](../开发细则总表.md#DEV-53)、[DEV-55](../开发细则总表.md#DEV-55) |
| P11-F76 / P11-T76 | 媒体标准接口与加密附件 | P0 / CURRENT | [DEV-27](../开发细则总表.md#DEV-27)、[DEV-31](../开发细则总表.md#DEV-31)、[DEV-37](../开发细则总表.md#DEV-37) |

## 5. 完成规则

适用 CHK-01—08，分别提交实现、失败恢复、权限/幂等和真实证据。模块代码完成不意味着对应组合可售；无证据的引擎、许可、隐私档位、支付资格保持关闭。[验收交付规则](../docs/06-验收交付与证据规则.md)。
