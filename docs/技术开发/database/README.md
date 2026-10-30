# PostgreSQL物理设计附件

P11｜技术v1.0｜需求v3.0｜设计基线。R001已在项目临时原生PostgreSQL17执行60表初始迁移及选定约束测试，实例已停止；[实际证据](../../../dev/tests/evidence/R001-验证报告.md)。完整业务事务与产品验收仍待实现/执行。

本轮payments增加product_profile_revision、channel_binding_snapshot、prepay_reference及渠道交易号唯一绑定；绑定/金额/原号不可改，预下单号与交易号首次写入后不可替换。只存秘密引用，不存Payment-Proof/会话或授权原值。SQL AST仍不执行PL/pgSQL或数据库约束。

供给内核修订为capabilities增加必需的kernel_environment_ref和kernel_environment_digest，分别引用完整不可变环境载荷及其原始字节摘要。模型/节点/依赖或实际运行组件改变即产生新环境身份。Service与ContractSnapshot的kernel_bindings冻结允许的设备/能力/环境集合；执行Lease和RentalAccess绑定本次环境摘要。环境准入、撤销和提供方确认记录独立保存，不写回已哈希环境载荷。现有合同快照与执行manifest承担相应存储，未新建一套可绕过合同的“最新内核”执行通道；具体服务层检查见[内核规范](../附录-供给端内核封装与替换规范.md)。

本轮以[范围修订](../附录-本轮范围与端侧选型修订.md)为准：小程序React/Taro优先、Web React/Vite；部署/HTTPS/运维和可观测性延后，历史相关模板不属于本轮交付要求。所有产品/真机/渠道实验仍未执行。

- [schema-draft.sql](schema-draft.sql)：表、域、主键/复合外键、唯一/状态/金额约束、不可变合同/账务和延迟平衡触发器。
- [transaction-patterns.sql](transaction-patterns.sql)：带sqlc命名的锁、准入、接受/取消、退款累计、outbox认领、租时计算及清理释放查询片段。
- [002_nb_contract_guards.sql](migrations/002_nb_contract_guards.sql)：R003前向迁移，只增加不可变设备绑定及订单/报价一致性守卫；原001与schema-draft基础字段字节不变。嵌入副本和摘要位于dev/src/internal/persistence/migrations/source-v2.json。
- [表结构目录](表结构目录.md)：由DDL语法树生成字段/主键/外键/索引目录，不能独立修改。
- [设计解释与锁序](../04-数据库物理设计与迁移.md)、[调度事务](../06-调度资源锁与执行租约.md)、[资金账务](../13-支付退款账务与渠道结算.md)。

SQL片段不是可直接顺序执行的完整业务事务。调用方仍需授权、固定锁序、检查影响行数、生成稳定ID、查询证据与外部状态，并在同一事务写必要的唯一接受/账/outbox。特别是退款累计、计量区间不重叠、证据引用有效性与真实停止不能靠CHECK自动成立。

evidence_ids/asset_ids等数组须服务层逐项验证引用和权限；需要强参照的支付/订单/设备等已采用外键。recipient公钥轮换新增版本，不覆盖原材料；撤销状态另记。平台永不保存内容私钥。报价/挂牌版本/订单合同不可变；账务和审计追加，纠错生成新的冲正/修复记录。

文档核验只用项目内pglast解析DDL和查询的PostgreSQL语法树，不连接数据库、不执行PL/pgSQL体。R001另有原生测试证据，范围仅覆盖初始迁移、账本/合同选定约束、密钥服务和幂等/队列场景，不证明所有触发器路径、应用数据库权限、索引性能和整体业务并发正确。初始迁移位于dev/src/internal/persistence/migrations，发布后只增forward migration；后续变更继续在项目专用**原生PostgreSQL17**验证，禁止本地虚拟化。

此文件不创建数据库角色或给普通应用DDL/超级用户权限。正式迁移需分离owner/migrator、api、scheduler、gateway、finance、audit权限；租户过滤与RLS采用经过连接池上下文测试的策略。账务不可变触发器不能防数据库owner恶意操作，仍须权限分离/备份/外部对账。

## R004 前向身份表

[003_auth_sessions.sql](migrations/003_auth_sessions.sql)追加auth_sessions/auth_attempts，当前领域表62；基础60表和已发布001/002保持原字节。摘要见dev/src/internal/persistence/migrations/source-v3.json。结构目录合并基础与前向建表并保留来源；实际迁移与本机HTTP测试见[R004证据](../../../dev/tests/evidence/R004-验证报告.md)，静态目录构建仍不执行SQL。

## R005前向申请与挂牌记录

[004_seller_listing_records.sql](migrations/004_seller_listing_records.sql)新增seller_applications/listing_definitions及6个前向字段，当前64表/613列；申请、定义、同意追加不可变。001—003保原字节；source-v4.json记录精确副本。新表和ALTER新增列合并到结构目录，静态构建不执行SQL；[实际局部证据](../../../dev/tests/evidence/R005-验证报告.md)与[限制](../附录-卖家申请与挂牌版本实施约束.md)分开。旧批准地区默认空、型号/挂牌定义不推测补造，历史迁移仍须独立验证。

## R014订单容量前向006

[006_order_budget_capacity.sql](migrations/006_order_budget_capacity.sql)为orders与budget_reservations追加8列和原子守恒/终态/资格来源约束，仍65表。001—005及端侧SQLite DDL保持原字节，source-v6.json记录嵌入副本。原预算任意状态均全额保留为未核实占用；历史总额超订单上限则整笔升级失败，不推测零费或释放。预算SQL事务primitive不能替代当前身份、渠道资金、路由授权或执行owner；完整依据见[OBC细则](../附录-订单预算预留与资格化消耗实施细则.md)。

## R016原调用前向008

[008_route_invocation_preparation.sql](migrations/008_route_invocation_preparation.sql)追加jobs六列、三外键、两个索引，冻结原调用和整笔预算绑定；延迟约束拒绝缺预算/安全事件提交。历史任务保留UNBOUND_JOB，不补造同意。001—007及端侧DDL原字节保持，source-v8.json绑定嵌入副本，仍65表。完整[RI-001—024](../附录-原调用准备与次数预算原子接线.md)及[当前证据](../../../dev/tests/evidence/R016-验证报告.md)说明元数据准备与执行授权的区别。

## R017 前向009

新增assets三列、jobs一列、execution_leases三列，共7列、1外键、2索引。输入/首租约/安全事件/持久投递意图的结构与原子性守卫在临时原生PG执行验证；001—008和端侧DDL不修改。旧asset/lease保持UNBOUND、新列为空，禁止回填成已同意执行。数据库不验证密码、当前用户或真实收单/设备资格。详见[EI细则](../附录-原生输入加密与首租约原子准入.md)。

## R018 前向010

六个领取字段、一外键、一唯一索引和结构/原子守卫追加到原execution_leases；仍65表，累计664列/113外键。原设备/原键永久绑定原lease，原命令恢复仍复核当前权威；不把领取当执行ACK。001—009及端侧SQLite原字节保留。详见[CL细则](../附录-原设备签名领取与原租约交付.md)和[原字节证明](../../../dev/tests/evidence/R018-migration-immutability.json)。

## R019 无新迁移

本轮复用原领取绑定和原AEAD/HMAC，不新增表/列或迁移。65表/664列/113外键保持，SQL001—010和端侧DDL共21个文件对R018原字节核验，见[证明](../../../dev/tests/evidence/R019-migration-immutability.json)。
