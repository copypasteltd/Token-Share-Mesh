# 技术开发机器附件

P11｜技术v1.0｜需求v3.0｜2026-10-29｜全部为设计/未实现，由scripts生成，不手改生成物。

当前范围为[阶段修订](../附录-本轮范围与端侧选型修订.md)：小程序优先，部署/公网HTTPS/可观测性延后。

| 附件 | 用途与边界 |
| --- | --- |
| [technical-module-catalog-v1.0.json](technical-module-catalog-v1.0.json) | 20模块职责/角色/计划路径/表/依赖 |
| [development-backlog-v1.0.json](development-backlog-v1.0.json) | 56任务的实现/完成证据/前置/F/T；55 DESIGN、DEV-55 DEFERRED |
| [api-implementation-map-v1.0.json](api-implementation-map-v1.0.json) | 89控制操作到模块/事务/scope/请求/响应/权威hash |
| [requirement-technical-map-v1.0.json](requirement-technical-map-v1.0.json) | 80项需求到模块/任务/操作/表/详规/同号验收 |
| [database-catalog-v1.0.json](database-catalog-v1.0.json) | DDL AST的表/列/约束/FK/索引；不证明实际数据库行为 |
| [engine-adapter-v1.0.schema.json](engine-adapter-v1.0.schema.json) | 本地可信域受控命令外壳，非厂商AI数据协议 |
| [engine-adapter-result-v1.0.schema.json](engine-adapter-result-v1.0.schema.json) | 安全命令结果/后端状态；SUCCEEDED不等于成交 |
| [config.sample-v1.0.json](config.sample-v1.0.json) | 项目内路径、空秘密引用、未验证收费/隐私/二进制开关关闭 |
| [compatibility-matrix-v1.0.json](compatibility-matrix-v1.0.json) | 28候选组合，含两OS原生隐私LLM与LM兼容；全部未测试/不可售 |
| [frontend-scope-v1.0.json](frontend-scope-v1.0.json) | Taro小程序/React Web职责、纯TS边界和付款动作 |
| [forwarding-profiles-v1.0.json](forwarding-profiles-v1.0.json) | 11个转发设计档、22项联调场景；非厂商Schema |
| [payment-product-profile-v1.0.json](payment-product-profile-v1.0.json) | A402字段/时序、50元/12h快照及C2C未获证能力 |
| [provider-kernel-policy-v1.0.json](provider-kernel-policy-v1.0.json) | 内核路线/替换/导入前白名单/用户同意及本地防护边界 |
| [kernel-environment-v1.0.schema.json](kernel-environment-v1.0.schema.json) | 组件/精确版本/hash/许可引用组成的不可变环境 |
| [node-allowlist-v1.0.schema.json](node-allowlist-v1.0.schema.json) | 有限节点/传递依赖/权限范围；非OS隔离证明 |
| [private-execution-plan-v1.0.json](private-execution-plan-v1.0.json) | 原生LLM/媒体/Android、管道/加密/资源预算，LP/PE未执行 |
| [e2e-protocol-plan-v1.0.json](e2e-protocol-plan-v1.0.json) | 双端/钥/信任/C与H/AAD/三层profile/终态/恢复与20项待执行子场景；不是wire Schema |
| [build-summary-v1.0.json](build-summary-v1.0.json) | 可重建的文档数量，不表示产品完成比例 |

控制面/事件/路由/旧JSON密文帧权威源仍在[白皮书contracts](../../白皮书/contracts/README.md)；不复制另一份OpenAPI。

EngineAdapter仅用于授权本地执行域。local_protocol_body_ref是本机受控引用，authority_payload_ref引用任务租约/租赁授权；JOB_LEASE与RENTAL_SESSION分开，租内调用不另计任务费。PREPARE需真实执行权，不能混同无执行权的E2E PREPARED。QUERY/CANCEL/CLEANUP可用原上下文查停清理，不赋新执行权；服务另核签名/当前代次/归属/期限，不接shell/任意图/裸路径。

计数器用十进制字符串，数据库job token/资源代次受bigint上界约束，不因网络uint64扩大。Schema外壳不证明签名/算法/二进制/许可/真实设备通过。

BINARY_STREAM_1、JSON_COMPAT和LOCAL_SEGMENTED_1分层。旧JSON channel/Schema不覆盖新增二进制格式；协商/独立Schema/库及向量冻结后才可启用。完整钥生命周期、签名/AAD、END/回执/恢复见[技术08](../08-端到端加密协议与密钥实现设计.md)，执行/媒体/预算见[低开销方案](../附录-低开销隐私执行完整方案.md)。

源为技术章节/SQL、scripts/技术开发数据.py、scripts/供给端内核数据.py和scripts/端对端加密数据.py。构建运行scripts/构建技术开发文档.py，核验运行scripts/核验技术开发文档.py；只写/核设计，不运行产品或真实密码。E2E-01—20、LP-01—14、PE-01—08及80项主验收均NOT_EXECUTED。
