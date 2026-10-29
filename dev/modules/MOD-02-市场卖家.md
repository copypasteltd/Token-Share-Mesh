# MOD-02 市场卖家开发细则

P11｜开发细则 v1.0｜MOD-02｜2026-10-30｜需求 v3.0 / 技术 v1.0。

> 由 dev/planning/开发细则源数据.py 与既有技术契约生成；修改源后重建，不手改本文件。本套为冻结设计拆解：55 任务 DESIGN、DEV-55 DEFERRED；真实源码进展见 dev/progress/README.md，完整产品/设备/支付/密码/性能验收不能据此记为通过。

[返回开发总表](../开发细则总表.md)；[模块边界总表](../docs/01-模块划分与职责边界.md)。

## 1. 职责与边界

| 字段 | 约定 |
| --- | --- |
| 职责 | 承载个人卖家与公开交易市场 |
| 输入 | 卖家资格、设备能力、挂牌价/时段 |
| 输出 | 卖家状态、挂牌版本、可售目录投影 |
| 禁止承担 | 不采购供给、不静默改卖家价格 |
| 岗位/人员 | 市场后端；实际负责人/工期未分配 |
| 计划路径 | dev/src/internal/market |
| 协作模块 | [MOD-01](MOD-01-身份授权.md)、[MOD-03](MOD-03-目录证据.md) |
| 应用服务 | SellerOnboarding、ListingRevision、Discovery |
| 权威表 | seller_profiles、listings、listing_versions、listing_devices |
| 适用约束 | [CON-001](../docs/02-表格化需求约束.md#CON-001)、[CON-002](../docs/02-表格化需求约束.md#CON-002)、[CON-006](../docs/02-表格化需求约束.md#CON-006)、[CON-007](../docs/02-表格化需求约束.md#CON-007)、[CON-008](../docs/02-表格化需求约束.md#CON-008)、[CON-009](../docs/02-表格化需求约束.md#CON-009)、[CON-013](../docs/02-表格化需求约束.md#CON-013)、[CON-017](../docs/02-表格化需求约束.md#CON-017)、[CON-038](../docs/02-表格化需求约束.md#CON-038)、[CON-041](../docs/02-表格化需求约束.md#CON-041)、[CON-043](../docs/02-表格化需求约束.md#CON-043)、[CON-047](../docs/02-表格化需求约束.md#CON-047) |
| 核心规范 | [03-领域模块与应用服务设计.md](../../docs/技术开发/03-领域模块与应用服务设计.md) |

## 2. 控制接口归属

| 索引 | operationId | 方法/路径 | 任务 | scope | 事务/幂等 |
| --- | --- | --- | --- | --- | --- |
| INT-016 | createListing | POST /seller/listings | [DEV-08](../开发细则总表.md#DEV-08) | listing:write | TX-01_OR_MODULE_LOCAL / 必需稳定键 |
| INT-029 | createSellerApplication | POST /seller/applications | [DEV-05](../开发细则总表.md#DEV-05) | seller:apply | TX-01_OR_MODULE_LOCAL / 必需稳定键 |
| INT-040 | getOwnListing | GET /seller/listings/{listing_id} | [DEV-08](../开发细则总表.md#DEV-08) | seller:read | READ_PROJECTION / 见权威契约 |
| INT-049 | getSellerProfile | GET /seller/profile | [DEV-05](../开发细则总表.md#DEV-05) | seller:read | READ_PROJECTION / 见权威契约 |
| INT-058 | listOwnListings | GET /seller/listings | [DEV-08](../开发细则总表.md#DEV-08) | seller:read | READ_PROJECTION / 见权威契约 |
| INT-062 | listServices | GET /services | [DEV-08](../开发细则总表.md#DEV-08) | market:read | READ_PROJECTION / 见权威契约 |
| INT-063 | pauseListing | POST /seller/listings/{listing_id}/pause | [DEV-08](../开发细则总表.md#DEV-08) | listing:write | TX-01_OR_MODULE_LOCAL / 必需稳定键 |
| INT-080 | requestSellerExit | POST /seller/exit | [DEV-05](../开发细则总表.md#DEV-05) | seller:manage | TX-01_OR_MODULE_LOCAL / 必需稳定键 |
| INT-085 | submitListing | POST /seller/listings/{listing_id}/submit | [DEV-08](../开发细则总表.md#DEV-08) | listing:write | TX-01_OR_MODULE_LOCAL / 必需稳定键 |
| INT-088 | updateFutureListing | PATCH /seller/listings/{listing_id} | [DEV-08](../开发细则总表.md#DEV-08) | listing:write | TX-01_OR_MODULE_LOCAL / 必需稳定键 |

[完整控制面、数据表与需求追踪](../docs/04-接口数据与需求追踪.md)；请求/响应 Schema 和响应码沿用既有 OpenAPI。

## 3. 开发任务与子项

<a id="DEV-05"></a>

### DEV-05 个人卖家准入及退出

| 字段 | 细则 |
| --- | --- |
| 模块/岗位 | [MOD-02](MOD-02-市场卖家.md) / 市场后端；实际负责人未分配 |
| 计划路径 | `dev/src/internal/market` |
| 范围/状态 | CURRENT / DESIGN / NOT_EXECUTED |
| 依赖 | [DEV-03](../开发细则总表.md#DEV-03)、[DEV-04](../开发细则总表.md#DEV-04) |
| 需求/主验收 | P11-F02、P11-F19、P11-F55、P11-F56；P11-T02、P11-T19、P11-T55、P11-T56 |
| 实施目标 | 实现自愿卖家申请、资格/收款主体绑定、暂停/退出和在途尾单处理。 |
| 原完成证据 | 未合格不能收费挂牌；退出保留历史与尾款，不生成平台采购合同。 |
| 门槛 | E01、E12 |
| 控制操作 | createSellerApplication、getSellerProfile、requestSellerExit |
| 需求约束 | [CON-001](../docs/02-表格化需求约束.md#CON-001)、[CON-002](../docs/02-表格化需求约束.md#CON-002)、[CON-006](../docs/02-表格化需求约束.md#CON-006)、[CON-007](../docs/02-表格化需求约束.md#CON-007)、[CON-008](../docs/02-表格化需求约束.md#CON-008)、[CON-009](../docs/02-表格化需求约束.md#CON-009)、[CON-013](../docs/02-表格化需求约束.md#CON-013)、[CON-017](../docs/02-表格化需求约束.md#CON-017)、[CON-038](../docs/02-表格化需求约束.md#CON-038)、[CON-041](../docs/02-表格化需求约束.md#CON-041)、[CON-043](../docs/02-表格化需求约束.md#CON-043)、[CON-047](../docs/02-表格化需求约束.md#CON-047) |
| 统一交付 | 适用 CHK-01—08；不适用项注明理由；证据规则见 DOC-06 |

| 子项编号 | 开发项 | 实现要求与失败边界 | 交付/证据 |
| --- | --- | --- | --- |
| DEV-05.01 | 申请准入 | 登记自愿个人卖家、主体/地区/许可/收款条件，不建采购库存 | 准入字段、资格状态与失败说明 |
| DEV-05.02 | 渠道绑定 | 收款主体与实际seller绑定，资格未明确不得真实收费 | 卖家渠道绑定及资格引用 |
| DEV-05.03 | 暂停退出 | 停新售/排空/撤销分开，在途订单/争议/尾款继续受控处理 | 暂停/退出状态机及尾单清单 |
| DEV-05.04 | 权限隔离 | 仅本人/受权审核者变更准入，恢复重新核证据 | 越权变更/撤销/重新准入用例 |

规范来源：[03-领域模块与应用服务设计.md](../../docs/技术开发/03-领域模块与应用服务设计.md)；[21-开发任务分解与需求追踪.md](../../docs/技术开发/21-开发任务分解与需求追踪.md)；[18-测试工程与故障验证.md](../../docs/技术开发/18-测试工程与故障验证.md)；[附录-本轮范围与端侧选型修订.md](../../docs/技术开发/附录-本轮范围与端侧选型修订.md)。

<a id="DEV-08"></a>

### DEV-08 挂牌版本与市场查询

| 字段 | 细则 |
| --- | --- |
| 模块/岗位 | [MOD-02](MOD-02-市场卖家.md) / 市场后端；实际负责人未分配 |
| 计划路径 | `dev/src/internal/market` |
| 范围/状态 | CURRENT / DESIGN / NOT_EXECUTED |
| 依赖 | [DEV-05](../开发细则总表.md#DEV-05)、[DEV-07](../开发细则总表.md#DEV-07) |
| 需求/主验收 | P11-F02、P11-F05、P11-F07、P11-F08、P11-F29；P11-T02、P11-T05、P11-T07、P11-T08、P11-T29 |
| 实施目标 | 实现卖家未来挂牌版、时段/价费、披露设备及按真实资格/容量过滤的市场查询。 |
| 原完成证据 | 旧价旧设备快照不改，无法切换卖家或购买未通过组合；稳定cursor/本人scope。 |
| 门槛 | E13 |
| 控制操作 | listOwnListings、createListing、getOwnListing、updateFutureListing、submitListing、pauseListing、listServices |
| 需求约束 | [CON-001](../docs/02-表格化需求约束.md#CON-001)、[CON-002](../docs/02-表格化需求约束.md#CON-002)、[CON-006](../docs/02-表格化需求约束.md#CON-006)、[CON-007](../docs/02-表格化需求约束.md#CON-007)、[CON-008](../docs/02-表格化需求约束.md#CON-008)、[CON-009](../docs/02-表格化需求约束.md#CON-009)、[CON-013](../docs/02-表格化需求约束.md#CON-013)、[CON-017](../docs/02-表格化需求约束.md#CON-017)、[CON-038](../docs/02-表格化需求约束.md#CON-038)、[CON-041](../docs/02-表格化需求约束.md#CON-041)、[CON-043](../docs/02-表格化需求约束.md#CON-043)、[CON-047](../docs/02-表格化需求约束.md#CON-047) |
| 统一交付 | 适用 CHK-01—08；不适用项注明理由；证据规则见 DOC-06 |

| 子项编号 | 开发项 | 实现要求与失败边界 | 交付/证据 |
| --- | --- | --- | --- |
| DEV-08.01 | 挂牌版本 | 卖家编辑未来价费/时段/设备集合，既售快照不变 | 挂牌及版本差异记录 |
| DEV-08.02 | 可售投影 | 只展示资格/容量允许组合，手机CPU不标GPU | 市场筛选与错误标签负向用例 |
| DEV-08.03 | 检索分页 | 过滤模式/能力/地区，游标绑定查询范围，逐对象授权 | 稳定分页与跨卖家读取检查 |
| DEV-08.04 | 暂停关联 | 卖家/设备/证据停用传播至新报价，不改历史单 | 停售传播与旧合同读取记录 |

NB-R1需求增量：NB-R001、NB-R002、NB-R003、NB-R005、NB-R006、NB-R021、NB-R025；[正反向验收及契约缺口](../../docs/需求推进/NB-1.0/06-需求追踪与验收矩阵.md)。源码实施见进度登记，子场景仍未执行。

规范来源：[03-领域模块与应用服务设计.md](../../docs/技术开发/03-领域模块与应用服务设计.md)；[21-开发任务分解与需求追踪.md](../../docs/技术开发/21-开发任务分解与需求追踪.md)；[18-测试工程与故障验证.md](../../docs/技术开发/18-测试工程与故障验证.md)；[附录-本轮范围与端侧选型修订.md](../../docs/技术开发/附录-本轮范围与端侧选型修订.md)。

## 4. 需求与主验收覆盖

| 需求/主验收 | 功能 | 优先级/范围 | 关联任务 |
| --- | --- | --- | --- |
| P11-F02 / P11-T02 | 个人卖家自助准入、挂牌与合同 | P0 / CURRENT | [DEV-05](../开发细则总表.md#DEV-05)、[DEV-08](../开发细则总表.md#DEV-08)、[DEV-46](../开发细则总表.md#DEV-46) |
| P11-F05 / P11-T05 | 资源、时段和停止共享 | P0 / CURRENT | [DEV-06](../开发细则总表.md#DEV-06)、[DEV-08](../开发细则总表.md#DEV-08)、[DEV-18](../开发细则总表.md#DEV-18)、[DEV-20](../开发细则总表.md#DEV-20)、[DEV-21](../开发细则总表.md#DEV-21)、[DEV-24](../开发细则总表.md#DEV-24)、[DEV-43](../开发细则总表.md#DEV-43)、[DEV-46](../开发细则总表.md#DEV-46) |
| P11-F07 / P11-T07 | C2C能力市场、卖家选择与预检 | P0 / CURRENT | [DEV-08](../开发细则总表.md#DEV-08)、[DEV-09](../开发细则总表.md#DEV-09)、[DEV-45](../开发细则总表.md#DEV-45)、[DEV-50](../开发细则总表.md#DEV-50) |
| P11-F08 / P11-T08 | 卖家报价、平台费用与预算快照 | P0 / CURRENT | [DEV-08](../开发细则总表.md#DEV-08)、[DEV-09](../开发细则总表.md#DEV-09)、[DEV-12](../开发细则总表.md#DEV-12)、[DEV-45](../开发细则总表.md#DEV-45) |
| P11-F19 / P11-T19 | 卖家成交收益与结算状态 | P0 / CURRENT | [DEV-05](../开发细则总表.md#DEV-05)、[DEV-16](../开发细则总表.md#DEV-16)、[DEV-39](../开发细则总表.md#DEV-39)、[DEV-40](../开发细则总表.md#DEV-40)、[DEV-46](../开发细则总表.md#DEV-46) |
| P11-F29 / P11-T29 | C2C基础信誉、评价与反串通 | P0 / CURRENT | [DEV-08](../开发细则总表.md#DEV-08)、[DEV-46](../开发细则总表.md#DEV-46)、[DEV-50](../开发细则总表.md#DEV-50) |
| P11-F55 / P11-T55 | 按卖家能力与资金证据发布 | P0 / CURRENT | [DEV-05](../开发细则总表.md#DEV-05)、[DEV-07](../开发细则总表.md#DEV-07)、[DEV-13](../开发细则总表.md#DEV-13)、[DEV-15](../开发细则总表.md#DEV-15)、[DEV-40](../开发细则总表.md#DEV-40)、[DEV-48](../开发细则总表.md#DEV-48)、[DEV-52](../开发细则总表.md#DEV-52) |
| P11-F56 / P11-T56 | C2C基线、旧假设与待决事项 | P0 / CURRENT | [DEV-01](../开发细则总表.md#DEV-01)、[DEV-05](../开发细则总表.md#DEV-05)、[DEV-09](../开发细则总表.md#DEV-09)、[DEV-52](../开发细则总表.md#DEV-52)、[DEV-56](../开发细则总表.md#DEV-56) |

## 5. 完成规则

适用 CHK-01—08，分别提交实现、失败恢复、权限/幂等和真实证据。模块代码完成不意味着对应组合可售；无证据的引擎、许可、隐私档位、支付资格保持关闭。[验收交付规则](../docs/06-验收交付与证据规则.md)。
