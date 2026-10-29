# MOD-10 端侧加密开发细则

P11｜开发细则 v1.0｜MOD-10｜2026-10-30｜需求 v3.0 / 技术 v1.0。

> 由 dev/planning/开发细则源数据.py 与既有技术契约生成；修改源后重建，不手改本文件。本套为冻结设计拆解：55 任务 DESIGN、DEV-55 DEFERRED；真实源码进展见 dev/progress/README.md，完整产品/设备/支付/密码/性能验收不能据此记为通过。

[返回开发总表](../开发细则总表.md)；[模块边界总表](../docs/01-模块划分与职责边界.md)。

## 1. 职责与边界

| 字段 | 约定 |
| --- | --- |
| 职责 | 管理端侧信任、密钥与加密执行入口 |
| 输入 | 独立核验的接收公钥、合同、标准请求 |
| 输出 | sealed信封、签名流头、认证记录、本地验收 |
| 禁止承担 | 不把内容交平台解密、不将普通服务标硬件增强 |
| 岗位/人员 | 密码/客户端工程；实际负责人/工期未分配 |
| 计划路径 | dev/src/client/crypto-gateway |
| 协作模块 | [MOD-01](MOD-01-身份授权.md)、[MOD-04](MOD-04-合同订单路由预算.md) |
| 应用服务 | TrustStore、EnvelopeService、StreamCrypto、LocalAcceptance |
| 权威表 | recipient_keys、encryption_intents、envelopes |
| 适用约束 | [CON-001](../docs/02-表格化需求约束.md#CON-001)、[CON-002](../docs/02-表格化需求约束.md#CON-002)、[CON-003](../docs/02-表格化需求约束.md#CON-003)、[CON-006](../docs/02-表格化需求约束.md#CON-006)、[CON-007](../docs/02-表格化需求约束.md#CON-007)、[CON-008](../docs/02-表格化需求约束.md#CON-008)、[CON-009](../docs/02-表格化需求约束.md#CON-009)、[CON-010](../docs/02-表格化需求约束.md#CON-010)、[CON-012](../docs/02-表格化需求约束.md#CON-012)、[CON-013](../docs/02-表格化需求约束.md#CON-013)、[CON-018](../docs/02-表格化需求约束.md#CON-018)、[CON-019](../docs/02-表格化需求约束.md#CON-019)、[CON-020](../docs/02-表格化需求约束.md#CON-020)、[CON-021](../docs/02-表格化需求约束.md#CON-021)、[CON-023](../docs/02-表格化需求约束.md#CON-023)、[CON-024](../docs/02-表格化需求约束.md#CON-024)、[CON-025](../docs/02-表格化需求约束.md#CON-025)、[CON-026](../docs/02-表格化需求约束.md#CON-026)、[CON-027](../docs/02-表格化需求约束.md#CON-027)、[CON-028](../docs/02-表格化需求约束.md#CON-028)、[CON-029](../docs/02-表格化需求约束.md#CON-029)、[CON-032](../docs/02-表格化需求约束.md#CON-032)、[CON-033](../docs/02-表格化需求约束.md#CON-033)、[CON-034](../docs/02-表格化需求约束.md#CON-034)、[CON-038](../docs/02-表格化需求约束.md#CON-038)、[CON-040](../docs/02-表格化需求约束.md#CON-040)、[CON-041](../docs/02-表格化需求约束.md#CON-041)、[CON-042](../docs/02-表格化需求约束.md#CON-042)、[CON-043](../docs/02-表格化需求约束.md#CON-043)、[CON-044](../docs/02-表格化需求约束.md#CON-044)、[CON-046](../docs/02-表格化需求约束.md#CON-046) |
| 核心规范 | [08-端到端加密协议与密钥实现设计.md](../../docs/技术开发/08-端到端加密协议与密钥实现设计.md) |

## 2. 控制接口归属

| 索引 | operationId | 方法/路径 | 任务 | scope | 事务/幂等 |
| --- | --- | --- | --- | --- | --- |
| INT-009 | createAttestationChallenge | POST /recipients/{recipient_id}/attestation-challenges | [DEV-32](../开发细则总表.md#DEV-32) | attestation:request | TX-01_OR_MODULE_LOCAL / 必需稳定键 |
| INT-014 | createEncryptionIntent | POST /encryption-intents | [DEV-29](../开发细则总表.md#DEV-29) | cipher:intent | TX-01_OR_MODULE_LOCAL / 必需稳定键 |
| INT-021 | createRecipientRotation | POST /recipients/{recipient_id}/rotations | [DEV-29](../开发细则总表.md#DEV-29) | recipient:manage | TX-01_OR_MODULE_LOCAL / 必需稳定键 |
| INT-034 | getAttestationEvidence | GET /recipients/{recipient_id}/evidence | [DEV-32](../开发细则总表.md#DEV-32) | attestation:read | READ_PROJECTION / 见权威契约 |
| INT-043 | getRecipient | GET /recipients/{recipient_id} | [DEV-29](../开发细则总表.md#DEV-29) | recipient:read | READ_PROJECTION / 见权威契约 |
| INT-077 | registerEnvelope | POST /jobs/{job_id}/envelopes | [DEV-30](../开发细则总表.md#DEV-30) | cipher:envelope | TX-01_OR_MODULE_LOCAL / 必需稳定键 |
| INT-078 | registerRecipient | POST /recipients | [DEV-29](../开发细则总表.md#DEV-29) | recipient:manage | TX-01_OR_MODULE_LOCAL / 必需稳定键 |
| INT-083 | revokeRecipient | POST /recipients/{recipient_id}/revoke | [DEV-29](../开发细则总表.md#DEV-29) | recipient:manage | TX-01_OR_MODULE_LOCAL / 必需稳定键 |

[完整控制面、数据表与需求追踪](../docs/04-接口数据与需求追踪.md)；请求/响应 Schema 和响应码沿用既有 OpenAPI。

## 3. 开发任务与子项

<a id="DEV-29"></a>

### DEV-29 端点身份与密钥库

| 字段 | 细则 |
| --- | --- |
| 模块/岗位 | [MOD-10](MOD-10-端侧加密.md) / 密码/客户端工程；实际负责人未分配 |
| 计划路径 | `dev/src/client/crypto-gateway` |
| 范围/状态 | CURRENT / DESIGN / NOT_EXECUTED |
| 依赖 | [DEV-03](../开发细则总表.md#DEV-03)、[DEV-11](../开发细则总表.md#DEV-11) |
| 需求/主验收 | P11-F32、P11-F33、P11-F36、P11-F72、P11-F75；P11-T32、P11-T33、P11-T36、P11-T72、P11-T75 |
| 实施目标 | 实现独立端点核验/公钥版本、独立签名/加密钥、轮换/撤销及OS秘密存储。实现双向接收钥独立信任、用途隔离、正常/泄漏轮换、撤销未知拒绝及丢钥恢复边界。 |
| 原完成证据 | 平台无内容私钥；sealed box匿名性不冒充身份认证，换端须买方核验重封。E2E-01/02/04/14；不把TOFU当首次证明，不宣称静态接收钥具有完整前向保密。 |
| 门槛 | POC-04、POC-09、E08 |
| 控制操作 | createEncryptionIntent、registerRecipient、getRecipient、createRecipientRotation、revokeRecipient |
| 需求约束 | [CON-001](../docs/02-表格化需求约束.md#CON-001)、[CON-002](../docs/02-表格化需求约束.md#CON-002)、[CON-003](../docs/02-表格化需求约束.md#CON-003)、[CON-006](../docs/02-表格化需求约束.md#CON-006)、[CON-007](../docs/02-表格化需求约束.md#CON-007)、[CON-008](../docs/02-表格化需求约束.md#CON-008)、[CON-009](../docs/02-表格化需求约束.md#CON-009)、[CON-010](../docs/02-表格化需求约束.md#CON-010)、[CON-012](../docs/02-表格化需求约束.md#CON-012)、[CON-013](../docs/02-表格化需求约束.md#CON-013)、[CON-018](../docs/02-表格化需求约束.md#CON-018)、[CON-019](../docs/02-表格化需求约束.md#CON-019)、[CON-020](../docs/02-表格化需求约束.md#CON-020)、[CON-021](../docs/02-表格化需求约束.md#CON-021)、[CON-023](../docs/02-表格化需求约束.md#CON-023)、[CON-024](../docs/02-表格化需求约束.md#CON-024)、[CON-025](../docs/02-表格化需求约束.md#CON-025)、[CON-026](../docs/02-表格化需求约束.md#CON-026)、[CON-027](../docs/02-表格化需求约束.md#CON-027)、[CON-028](../docs/02-表格化需求约束.md#CON-028)、[CON-029](../docs/02-表格化需求约束.md#CON-029)、[CON-032](../docs/02-表格化需求约束.md#CON-032)、[CON-033](../docs/02-表格化需求约束.md#CON-033)、[CON-034](../docs/02-表格化需求约束.md#CON-034)、[CON-038](../docs/02-表格化需求约束.md#CON-038)、[CON-040](../docs/02-表格化需求约束.md#CON-040)、[CON-041](../docs/02-表格化需求约束.md#CON-041)、[CON-042](../docs/02-表格化需求约束.md#CON-042)、[CON-043](../docs/02-表格化需求约束.md#CON-043)、[CON-044](../docs/02-表格化需求约束.md#CON-044)、[CON-046](../docs/02-表格化需求约束.md#CON-046) |
| 统一交付 | 适用 CHK-01—08；不适用项注明理由；证据规则见 DOC-06 |

| 子项编号 | 开发项 | 实现要求与失败边界 | 交付/证据 |
| --- | --- | --- | --- |
| DEV-29.01 | 独立信任 | 双向recipient/签名身份/指纹绑定合同，TOFU不当首次证明 | 平台换钥/买方结果钥替换拒绝 |
| DEV-29.02 | 钥用途存储 | auth/content/identity/receipt/update分开，专用OS秘密库 | 用途越权/私钥不上平台/静态钥保护 |
| DEV-29.03 | 轮换撤销 | 旧身份签新钥或独立重核，撤销未知停新放钥 | 正常/泄漏轮换/离线状态报告 |
| DEV-29.04 | 丢钥恢复 | 重装新身份，买方私钥丢失不解旧结果，披露无完整前向保密 | 恢复流程/旧数据不可恢复提示 |

NB-R1需求增量：NB-R010、NB-R029；[正反向验收及契约缺口](../../docs/需求推进/NB-1.0/06-需求追踪与验收矩阵.md)。源码实施见进度登记，子场景仍未执行。

规范来源：[08-端到端加密协议与密钥实现设计.md](../../docs/技术开发/08-端到端加密协议与密钥实现设计.md)；[21-开发任务分解与需求追踪.md](../../docs/技术开发/21-开发任务分解与需求追踪.md)；[18-测试工程与故障验证.md](../../docs/技术开发/18-测试工程与故障验证.md)；[附录-本轮范围与端侧选型修订.md](../../docs/技术开发/附录-本轮范围与端侧选型修订.md)；[附录-低开销隐私执行完整方案.md](../../docs/技术开发/附录-低开销隐私执行完整方案.md)。

<a id="DEV-30"></a>

### DEV-30 本地加密与内置转发

| 字段 | 细则 |
| --- | --- |
| 模块/岗位 | [MOD-10](MOD-10-端侧加密.md) / 密码/客户端工程；实际负责人未分配 |
| 计划路径 | `dev/src/client/crypto-gateway` |
| 范围/状态 | CURRENT / DESIGN / NOT_EXECUTED |
| 依赖 | [DEV-25](../开发细则总表.md#DEV-25)、[DEV-29](../开发细则总表.md#DEV-29)、[DEV-17](../开发细则总表.md#DEV-17) |
| 需求/主验收 | P11-F33、P11-F34、P11-F35、P11-F72、P11-F73、P11-F80；P11-T33、P11-T34、P11-T35、P11-T72、P11-T73、P11-T80 |
| 实施目标 | 通过成熟库薄封装实现上下文规范化、sealed key、ordered secretstream及反向响应。实现C→sealed→H签名→记录AAD、BINARY_STREAM_1及LOCAL_SEGMENTED_1，Go/C++/JNI同库；先补二进制channel协商/版本Schema。 |
| 原完成证据 | GoCGO/JNI互通、FINAL+业务END、篡改/重放/崩溃不复用nonce；seller所有者可见如实披露。E2E-03—11/14/15；END本身TAG_FINAL，无循环摘要/重用状态，顺序流不伪称seek，失败不重跑。 |
| 门槛 | POC-04、E08 |
| 控制操作 | registerEnvelope |
| 需求约束 | [CON-001](../docs/02-表格化需求约束.md#CON-001)、[CON-002](../docs/02-表格化需求约束.md#CON-002)、[CON-003](../docs/02-表格化需求约束.md#CON-003)、[CON-006](../docs/02-表格化需求约束.md#CON-006)、[CON-007](../docs/02-表格化需求约束.md#CON-007)、[CON-008](../docs/02-表格化需求约束.md#CON-008)、[CON-009](../docs/02-表格化需求约束.md#CON-009)、[CON-010](../docs/02-表格化需求约束.md#CON-010)、[CON-012](../docs/02-表格化需求约束.md#CON-012)、[CON-013](../docs/02-表格化需求约束.md#CON-013)、[CON-018](../docs/02-表格化需求约束.md#CON-018)、[CON-019](../docs/02-表格化需求约束.md#CON-019)、[CON-020](../docs/02-表格化需求约束.md#CON-020)、[CON-021](../docs/02-表格化需求约束.md#CON-021)、[CON-023](../docs/02-表格化需求约束.md#CON-023)、[CON-024](../docs/02-表格化需求约束.md#CON-024)、[CON-025](../docs/02-表格化需求约束.md#CON-025)、[CON-026](../docs/02-表格化需求约束.md#CON-026)、[CON-027](../docs/02-表格化需求约束.md#CON-027)、[CON-028](../docs/02-表格化需求约束.md#CON-028)、[CON-029](../docs/02-表格化需求约束.md#CON-029)、[CON-032](../docs/02-表格化需求约束.md#CON-032)、[CON-033](../docs/02-表格化需求约束.md#CON-033)、[CON-034](../docs/02-表格化需求约束.md#CON-034)、[CON-038](../docs/02-表格化需求约束.md#CON-038)、[CON-040](../docs/02-表格化需求约束.md#CON-040)、[CON-041](../docs/02-表格化需求约束.md#CON-041)、[CON-042](../docs/02-表格化需求约束.md#CON-042)、[CON-043](../docs/02-表格化需求约束.md#CON-043)、[CON-044](../docs/02-表格化需求约束.md#CON-044)、[CON-046](../docs/02-表格化需求约束.md#CON-046) |
| 统一交付 | 适用 CHK-01—08；不适用项注明理由；证据规则见 DOC-06 |

| 子项编号 | 开发项 | 实现要求与失败边界 | 交付/证据 |
| --- | --- | --- | --- |
| DEV-30.01 | 冻结加密档 | 同库CGO/C++/JNI，先补二进制channel协商/Schema/版本和域向量 | 官方/跨端黄金字节向量与独立评审 |
| DEV-30.02 | 信封和AAD | 按C→sealed→H签名→长度/seq AAD，无未知资源字段/自引用 | 逐字段/重复键/Unicode/整数篡改拒绝 |
| DEV-30.03 | 记录与终态 | 有界二进制类型/flags/reserved；END本身TAG_FINAL | 长度溢出/截断/追加/标准终止缺失报告 |
| DEV-30.04 | 本地存储恢复 | 独立LOCAL_SEGMENTED_1不可变缓存；丢push状态新授权epoch，不能盲重跑 | 分段manifest/钥丢失/崩溃状态用例 |

NB-R1需求增量：NB-R009、NB-R010、NB-R028、NB-R040、NB-R044；[正反向验收及契约缺口](../../docs/需求推进/NB-1.0/06-需求追踪与验收矩阵.md)。源码实施见进度登记，子场景仍未执行。

规范来源：[08-端到端加密协议与密钥实现设计.md](../../docs/技术开发/08-端到端加密协议与密钥实现设计.md)；[21-开发任务分解与需求追踪.md](../../docs/技术开发/21-开发任务分解与需求追踪.md)；[18-测试工程与故障验证.md](../../docs/技术开发/18-测试工程与故障验证.md)；[附录-本轮范围与端侧选型修订.md](../../docs/技术开发/附录-本轮范围与端侧选型修订.md)；[附录-低开销隐私执行完整方案.md](../../docs/技术开发/附录-低开销隐私执行完整方案.md)。

<a id="DEV-32"></a>

### DEV-32 增强档证明放钥门槛

| 字段 | 细则 |
| --- | --- |
| 模块/岗位 | [MOD-10](MOD-10-端侧加密.md) / 密码/客户端工程；实际负责人未分配 |
| 计划路径 | `dev/src/client/crypto-gateway` |
| 范围/状态 | CURRENT / DESIGN / NOT_EXECUTED；硬件增强条件分支 |
| 依赖 | [DEV-29](../开发细则总表.md#DEV-29)、[DEV-30](../开发细则总表.md#DEV-30) |
| 需求/主验收 | P11-F37、P11-F38、P11-F40、P11-F48；P11-T37、P11-T38、P11-T40、P11-T48 |
| 实施目标 | 实现独立证明/放钥政策和全路径覆盖，绑定kernel_environment_digest及实际CLI/daemon/runtime、ComfyUI/节点/编解码或native测量；替换必须重验。 |
| 原完成证据 | 真实硬件覆盖全路径，节点/内核替换及目录漂移使旧证明不可用；mock/普通进程永不解锁，不能静默降隐私。 |
| 门槛 | E08、E09 |
| 控制操作 | createAttestationChallenge、getAttestationEvidence |
| 需求约束 | [CON-001](../docs/02-表格化需求约束.md#CON-001)、[CON-002](../docs/02-表格化需求约束.md#CON-002)、[CON-003](../docs/02-表格化需求约束.md#CON-003)、[CON-006](../docs/02-表格化需求约束.md#CON-006)、[CON-007](../docs/02-表格化需求约束.md#CON-007)、[CON-008](../docs/02-表格化需求约束.md#CON-008)、[CON-009](../docs/02-表格化需求约束.md#CON-009)、[CON-010](../docs/02-表格化需求约束.md#CON-010)、[CON-012](../docs/02-表格化需求约束.md#CON-012)、[CON-013](../docs/02-表格化需求约束.md#CON-013)、[CON-018](../docs/02-表格化需求约束.md#CON-018)、[CON-019](../docs/02-表格化需求约束.md#CON-019)、[CON-020](../docs/02-表格化需求约束.md#CON-020)、[CON-021](../docs/02-表格化需求约束.md#CON-021)、[CON-023](../docs/02-表格化需求约束.md#CON-023)、[CON-024](../docs/02-表格化需求约束.md#CON-024)、[CON-025](../docs/02-表格化需求约束.md#CON-025)、[CON-026](../docs/02-表格化需求约束.md#CON-026)、[CON-027](../docs/02-表格化需求约束.md#CON-027)、[CON-028](../docs/02-表格化需求约束.md#CON-028)、[CON-029](../docs/02-表格化需求约束.md#CON-029)、[CON-032](../docs/02-表格化需求约束.md#CON-032)、[CON-033](../docs/02-表格化需求约束.md#CON-033)、[CON-034](../docs/02-表格化需求约束.md#CON-034)、[CON-038](../docs/02-表格化需求约束.md#CON-038)、[CON-040](../docs/02-表格化需求约束.md#CON-040)、[CON-041](../docs/02-表格化需求约束.md#CON-041)、[CON-042](../docs/02-表格化需求约束.md#CON-042)、[CON-043](../docs/02-表格化需求约束.md#CON-043)、[CON-044](../docs/02-表格化需求约束.md#CON-044)、[CON-046](../docs/02-表格化需求约束.md#CON-046) |
| 统一交付 | 适用 CHK-01—08；不适用项注明理由；证据规则见 DOC-06 |

| 子项编号 | 开发项 | 实现要求与失败边界 | 交付/证据 |
| --- | --- | --- | --- |
| DEV-32.01 | 证明政策 | 独立买方策略定义challenge/新鲜度/钥绑定/完整测量 | 验证器政策与真实目标硬件清单 |
| DEV-32.02 | 全路径验证 | CPU/GPU互联/内核/编码/日志/出口都覆盖，不仅保密GPU | 真实attestation与明文逃逸检查 |
| DEV-32.03 | 放钥撤销 | 证明有效才封钥，环境升级/过期/撤销重新验证 | 错钥/伪challenge/调试/升级拒绝 |
| DEV-32.04 | 保持独立关闭 | 普通原生/Keystore/VM/mock不解锁增强，不阻基础隐私实现 | 无合格硬件时关闭记录和局限声明 |

NB-R1需求增量：NB-R009、NB-R048；[正反向验收及契约缺口](../../docs/需求推进/NB-1.0/06-需求追踪与验收矩阵.md)。源码实施见进度登记，子场景仍未执行。

规范来源：[08-端到端加密协议与密钥实现设计.md](../../docs/技术开发/08-端到端加密协议与密钥实现设计.md)；[21-开发任务分解与需求追踪.md](../../docs/技术开发/21-开发任务分解与需求追踪.md)；[18-测试工程与故障验证.md](../../docs/技术开发/18-测试工程与故障验证.md)；[附录-本轮范围与端侧选型修订.md](../../docs/技术开发/附录-本轮范围与端侧选型修订.md)；[附录-供给端内核封装与替换规范.md](../../docs/技术开发/附录-供给端内核封装与替换规范.md)。

## 4. 需求与主验收覆盖

| 需求/主验收 | 功能 | 优先级/范围 | 关联任务 |
| --- | --- | --- | --- |
| P11-F32 / P11-T32 | 可见方披露与同意 | P0 / CURRENT | [DEV-03](../开发细则总表.md#DEV-03)、[DEV-04](../开发细则总表.md#DEV-04)、[DEV-09](../开发细则总表.md#DEV-09)、[DEV-29](../开发细则总表.md#DEV-29)、[DEV-45](../开发细则总表.md#DEV-45)、[DEV-53](../开发细则总表.md#DEV-53) |
| P11-F33 / P11-T33 | 买家加密端与端点身份 | P0 / CURRENT | [DEV-17](../开发细则总表.md#DEV-17)、[DEV-29](../开发细则总表.md#DEV-29)、[DEV-30](../开发细则总表.md#DEV-30)、[DEV-44](../开发细则总表.md#DEV-44) |
| P11-F34 / P11-T34 | 密文对象与加密分块 | P0 / CURRENT | [DEV-17](../开发细则总表.md#DEV-17)、[DEV-30](../开发细则总表.md#DEV-30)、[DEV-31](../开发细则总表.md#DEV-31)、[DEV-37](../开发细则总表.md#DEV-37) |
| P11-F35 / P11-T35 | 加密执行、回传与本地验收 | P0 / CURRENT | [DEV-19](../开发细则总表.md#DEV-19)、[DEV-30](../开发细则总表.md#DEV-30)、[DEV-31](../开发细则总表.md#DEV-31)、[DEV-44](../开发细则总表.md#DEV-44) |
| P11-F36 / P11-T36 | 同卖家加密换端与放钥 | P0 / CURRENT | [DEV-17](../开发细则总表.md#DEV-17)、[DEV-19](../开发细则总表.md#DEV-19)、[DEV-29](../开发细则总表.md#DEV-29) |
| P11-F37 / P11-T37 | 增强档证明与密钥释放 | P1 / CURRENT | [DEV-32](../开发细则总表.md#DEV-32)、[DEV-52](../开发细则总表.md#DEV-52)、[DEV-53](../开发细则总表.md#DEV-53)、[DEV-56](../开发细则总表.md#DEV-56) |
| P11-F38 / P11-T38 | 增强档全路径保密与硬件准入 | P1 / CURRENT | [DEV-32](../开发细则总表.md#DEV-32)、[DEV-52](../开发细则总表.md#DEV-52)、[DEV-53](../开发细则总表.md#DEV-53)、[DEV-56](../开发细则总表.md#DEV-56) |
| P11-F40 / P11-T40 | 加密内容治理与能力限制 | P0 / CURRENT | [DEV-04](../开发细则总表.md#DEV-04)、[DEV-31](../开发细则总表.md#DEV-31)、[DEV-32](../开发细则总表.md#DEV-32)、[DEV-49](../开发细则总表.md#DEV-49)、[DEV-52](../开发细则总表.md#DEV-52)、[DEV-53](../开发细则总表.md#DEV-53) |
| P11-F48 / P11-T48 | 租赁隐私属性与组合限制 | P0 / CURRENT | [DEV-09](../开发细则总表.md#DEV-09)、[DEV-32](../开发细则总表.md#DEV-32)、[DEV-33](../开发细则总表.md#DEV-33)、[DEV-45](../开发细则总表.md#DEV-45)、[DEV-52](../开发细则总表.md#DEV-52) |
| P11-F72 / P11-T72 | 买方本地或LAN加密网关 | P0 / CURRENT | [DEV-29](../开发细则总表.md#DEV-29)、[DEV-30](../开发细则总表.md#DEV-30)、[DEV-44](../开发细则总表.md#DEV-44)、[DEV-47](../开发细则总表.md#DEV-47) |
| P11-F73 / P11-T73 | 卖方解密与内置转发内核 | P0 / CURRENT | [DEV-25](../开发细则总表.md#DEV-25)、[DEV-30](../开发细则总表.md#DEV-30)、[DEV-44](../开发细则总表.md#DEV-44) |
| P11-F75 / P11-T75 | 本机与LAN接入和密钥隔离 | P0 / CURRENT | [DEV-03](../开发细则总表.md#DEV-03)、[DEV-24](../开发细则总表.md#DEV-24)、[DEV-29](../开发细则总表.md#DEV-29)、[DEV-44](../开发细则总表.md#DEV-44)、[DEV-47](../开发细则总表.md#DEV-47)、[DEV-53](../开发细则总表.md#DEV-53) |
| P11-F80 / P11-T80 | 多端原生网关内核与角色 | P0 / CURRENT | [DEV-01](../开发细则总表.md#DEV-01)、[DEV-21](../开发细则总表.md#DEV-21)、[DEV-22](../开发细则总表.md#DEV-22)、[DEV-24](../开发细则总表.md#DEV-24)、[DEV-25](../开发细则总表.md#DEV-25)、[DEV-28](../开发细则总表.md#DEV-28)、[DEV-30](../开发细则总表.md#DEV-30)、[DEV-41](../开发细则总表.md#DEV-41)、[DEV-42](../开发细则总表.md#DEV-42)、[DEV-44](../开发细则总表.md#DEV-44)、[DEV-56](../开发细则总表.md#DEV-56) |

## 5. 完成规则

适用 CHK-01—08，分别提交实现、失败恢复、权限/幂等和真实证据。模块代码完成不意味着对应组合可售；无证据的引擎、许可、隐私档位、支付资格保持关闭。[验收交付规则](../docs/06-验收交付与证据规则.md)。
