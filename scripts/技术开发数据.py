"""人工维护的技术设计数据；不运行产品，不推进开发或验收状态。"""
from __future__ import annotations

VERSION = "1.0-draft"
DATE = "2026-10-29"
STATUS = "DESIGN_UNIMPLEMENTED"

# name, owner role, application package, source chapter, authoritative tables,
# application services, principal dependencies. Paths are planned, not files.
MODULE_ROWS = [
 (1,"身份授权","身份/安全工程","identity",16,"users role_grants api_keys consents pairing_intents auth_sessions auth_attempts","PrincipalResolver RolePolicy KeyService ConsentService Sessions Alipay",[]),
 (2,"市场卖家","市场后端","market",3,"seller_profiles listings listing_versions listing_devices","SellerOnboarding ListingRevision Discovery",[1,3]),
 (3,"目录证据","资格/许可工程","catalog",11,"evidence capabilities catalog_entries","CapabilityQualification LicenseReview ProfileCatalog",[1]),
 (4,"合同订单路由预算","交易后端","orders",5,"quotes orders route_contracts budget_reservations","QuoteService OrderService RouteContractService BudgetReservation",[1,2,3]),
 (5,"支付退款适配","支付工程","payments",13,"payments refunds channel_requests","PaymentIntent ChannelQuery RefundCommand",[1,4]),
 (6,"调度资源租约","调度工程","dispatch",6,"devices resources resource_locks jobs attempts execution_leases deliveries acceptances","Admission ResourceArbiter AttemptLease Fence",[3,4,5]),
 (7,"桌面执行客户端","Windows/Linux工程","desktop",9,"","AgentLifecycle LocalInbox RuntimeSupervisor KernelRegistry NodeAllowlistInstaller AdapterSupervisor",[6,9]),
 (8,"Android客户端","Android工程","android",10,"","LmaDerivedApp PlatformBridge ShareService InferenceGate MobileAdapter",[6,9]),
 (9,"兼容明文网关内核","网关工程","relay",7,"","ProfileValidator DialectAdapter BoundRelayCore",[4,6]),
 (10,"端侧加密","密码/客户端工程","crypto",8,"recipient_keys encryption_intents envelopes","TrustStore EnvelopeService StreamCrypto LocalAcceptance",[1,4]),
 (11,"平台密文中转","网关/安全工程","cipherrelay",8,"cipher_channels","CipherChannel OpaqueObjectRelay FrameQuota",[1,6,10]),
 (12,"GPU租赁","租赁/调度工程","rental",12,"rental_sessions rental_renewals rental_access_grants availability_intervals","SessionCoordinator WorkspaceGrant AvailabilityMeter",[4,5,6]),
 (13,"文件媒体","存储后端","assets",15,"assets transfer_grants deletion_tasks","AssetRegistry TransferGrant LifecycleCleaner",[1,4]),
 (14,"用量计量","计量工程","metering",13,"usage_evidence meter_statements","UsageVerifier MeterStatement PriceCalculator",[4,6,12]),
 (15,"财务账与结算","财务工程","finance",13,"trade_qualifications ledger_accounts journal_transactions journal_entries settlements settlement_items settlement_batches batch_items","LedgerService SettlementEligibility Reconciler",[4,5,14]),
 (16,"售后与信誉","售后后端","support",14,"reviews disputes appeals evidence_grants","ReviewEligibility DisputeService AppealService",[1,4,13]),
 (17,"治理发布","运营/安全工程","governance",16,"audit_events feature_gates releases","FeatureGate ReleaseDecision Revocation",[1,3]),
 (18,"事件与后台任务","基础后端","outbox",17,"idempotency_records outbox_events inbox_events durable_jobs notifications","OutboxPublisher InboxDedup DurableJob",[1]),
 (19,"小程序与Web","React/Taro前端工程","frontend",14,"","MiniappBuyer MiniappSeller WebDeveloper WebAdmin TypedAPIClient PayLauncher",[1,4]),
 (20,"测试与契约验证","测试工程","validation",20,"","ProtocolFixtures NativeFaultHarness ScopeValidation",[1,17]),
]

# Task: number, title, main module, requirements, prior task numbers, PoC/E gates,
# implementation, completion evidence. Each row is intentionally bounded work.
TASK_ROWS = [
 (1, '原生工程与版本锁', 20, '56 58 68 80', [], 'POC-01', '建立单Go模块、原生Go/Node/Android构建入口和锁文件/SBOM；不引入虚拟化脚本。', 'Windows/Linux编译记录、依赖来源/hash、internal导入及无网络文档重建可复现。'),
 (2, '物理模型与原生迁移', 20, '01 10 14 20 31 53', [1], 'POC-01 POC-05', '将DDL草案转换为正式版本迁移；实现TxRunner、锁序、复合FK和账务触发器。', '原生PG17 up/约束/触发器/竞态/兼容回退；AST结果单列，不冒充数据库通过。'),
 (3, '主体scope与密钥', 1, '01 32 50 75', [1, 2], 'POC-09', '实现Principal、会话/CSRF、scope+owner+状态策略、key摘要/撤销和配对令牌。', '跨租户ID替换/角色撤销/原key只显示一次；浏览器Host/Origin与会话攻击拒绝证据。'),
 (4, '同意权限与数据处理边界', 1, '22 32 40 50 52 66', [3], 'E12', '实现版本化同意、限时排障授权、删除申请及受控证据访问审计。', '授权对象/人员/目的/期限逐项核验，撤销即时有效；密文无正文提取入口。'),
 (5, '个人卖家准入及退出', 2, '02 19 55 56', [3, 4], 'E01 E12', '实现自愿卖家申请、资格/收款主体绑定、暂停/退出和在途尾单处理。', '未合格不能收费挂牌；退出保留历史与尾款，不生成平台采购合同。'),
 (6, '设备配对与身份生命周期', 6, '03 05 57 65 66', [3, 5], 'POC-02 POC-03', '实现一次配对、设备凭据、出站心跳、drain/revoke和恢复再登记。', '令牌重复消耗拒绝；设备撤销不影响其他合法设备；恢复不复用旧租约。'),
 (7, '能力许可与内核环境证据', 3, '04 06 55 57 60 67 68', [5, 6], 'POC-02 E03 E04', '实现按OS/内核组件/模型/节点依赖/配方/profile/模式/地区的资格与许可；登记不可变kernel_environment_ref/digest，ComfyUI图/视频/音频/3D分别准入。', '六桌面组合与LMA派生真机分别登记，节点或组件漂移停新售；环境摘要与许可/物理证据可追，CPU回退不标GPU。'),
 (8, '挂牌版本与市场查询', 2, '02 05 07 08 29', [5, 7], 'E13', '实现卖家未来挂牌版、时段/价费、披露设备及按真实资格/容量过滤的市场查询。', '旧价旧设备快照不改，无法切换卖家或购买未通过组合；稳定cursor/本人scope。'),
 (9, '报价预检及费率快照', 4, '07 08 31 32 48 56', [7, 8], 'E01 E13', '实现SKU/模式/隐私/期限/资金预检，冻结设备/能力/kernel_environment集合与价费；消费报价时重核环境，变动需重新确认。', '过期/环境或证据变化/缺seller/未定佣金拒绝；报价明确可见方、各媒体规格和费用承担。'),
 (10, '不可变C2C订单', 4, '09 16 30 31 54', [9], 'E01 E02', '原子消费报价，固定seller/设备能力环境集合/价费/模式/隐私/验收；既售内核不热换，跨卖家仍需关联新单。', '同quote只一order、重放幂等；改active内核或节点不能修改旧合同，无法原环境履约则按约取消/退款。'),
 (11, '标准SDK路由合同', 4, '54 69 70 77 79', [10, 3], 'POC-07', '实现RouteContract与scoped key，标准Base URL/key即可绑定已确认卖家/预算/模式。', '不强制私有JSON路由字段；缺配置/过期/越界拒绝，无自动明文或换seller。'),
 (12, '预算预留与有理费率', 4, '08 18 27 44 61 77', [10], 'E11', '实现并发预算reserve/consume/release、新售小时与输入输出M token有理费率、整单累计取整和硬截止；关闭NB-R1价格契约缺口，旧版只精确适配。', '并发不超预算，非整分钟价不舍入，双价/缓存去重、部分退款/流断/usage未知守恒；预算不冒称渠道冻结。'),
 (13, 'Machine Pay预下单验付与原号查询', 5, '09 17 20 55', [10, 12], 'POC-06 E01 E02', '实现precreate/amtPaymentLink/Payment-Proof/verify及原号绑定；应用/service/实际seller授权快照，50元/12h校验，分离沙箱/正式。', '验签/active/金额/原号一致；缺废弃resource_id仍可正确绑定；UNKNOWN查原交易，不重复履约或扣款。'),
 (14, '退款累计及未知恢复', 5, '17 46 53', [13], 'POC-06 E02', '锁原支付累计生成唯一退款原号；处理部分退款、重复回调和未知查核。', '退款预留含UNKNOWN不超可退，失败/成功证据明确，重复请求不二退。'),
 (15, '资金资格与长任务时序', 5, '09 25 30 42 45 55', [13, 14], 'POC-06 E01 E02 E05', '实现固定有界预付、未知token最终价/租赁预算的产品能力门槛，区分已付款、业务接受和正式成交资格。', '无可退预算/适用个人seller/实际授权证据的组合关闭；不编造verify后settle或自动补扣能力。'),
 (16, '对账与资金异常', 15, '18 19 20 53', [13, 14], 'POC-06 E14', '实现支付/退款/佣金/卖家到账的原号核对、差额队列和双人受控修复。', '渠道事实与镜像账逐日匹配；异常不改删历史，不把GMV当平台收入。'),
 (17, '任务上下文与输入准入', 6, '10 11 33 34 36 54', [10, 12, 15, 7], 'POC-05', '实现INPUT_PENDING/PREPARED和输入/封装、资金/证据/容量短事务准入。', 'PREPARED不执行不占GPU；输入未就绪不能签ISSUED；总deadline不因等待延长。'),
 (18, '资源门闩与双围栏', 6, '05 10 11 41 64', [17, 2], 'POC-05', '实现每物理资源全模式/引擎锁、job token、跨job lock_generation、签名租约与版本化续约。', '并发买卖角色/引擎/自用/租赁争同卡唯一；旧资源代次/旧续约重放拒绝，失联不延硬截止。'),
 (19, '同卖家重派及唯一接受', 6, '14 16 35 36 39', [18], 'POC-05 POC-04', '实现一次同卖家重派、换端重封、当前尝试交付/签收唯一CAS和取消竞争。', '旧token交付拒绝、重传不二次接受；跨卖家重选明确新报价新单。'),
 (20, '取消停止与清理确认', 6, '05 17 22 47 52 59', [18, 19], 'POC-05 POC-10', '实现取消意向→撤租约→真实停止→清理ACK→释放；未知状态资源隔离。', 'TTL/进程崩溃不直接FREE；取消不误杀他人；离线重连补清不复活旧执行。'),
 (21, '桌面RuntimeSupervisor与可替换内核', 7, '03 05 58 65 68 80', [1, 6], 'POC-02 E14', '实现KernelRegistry/RuntimeSupervisor、Windows/Linux普通权限生命周期、不可变版本目录/暂存/验证、排空维护/切换/合格旧版回滚，持久inbox与进程归属。', '四物理组合中文路径/端口/权限、活动单不可切换、失败/撤销回滚与双围栏不回绕；不接管本人既有引擎/模型。'),
 (22, 'ComfyUI完整内核与有限节点安装', 7, '06 12 13 24 59 67 80', [7, 18, 21], 'POC-02 E04', '封装固定Python/ComfyUI/传递依赖/媒体工具和批准图像/视频/音频/3D能力包；实现NodeAllowlistInstaller、本人确认、导入前目录核验及prompt_id恢复。', '批准节点安装/依赖闭包/hash与撤销、Manager旁路拒绝；各媒体实际输出/取消/清理，环境变化重验，未知执行先查原ID。'),
 (23, 'LM Studio CLI可替换内核封装', 7, '59 60 61 67', [7, 18, 21], 'POC-02 E03 E04 E11', '实现lms CLI/llmster/runtime三层版本与官方接口桥、独立配置/进程归属验证、本地API/SSE/模型管理/取消/usage，以及经验证的精确版本替换恢复。', '两OS物理机真实offload/上下文隔离；不解析交互chat当协议，不接管外部daemon；固定版本/恢复机制或许可未获证不售。'),
 (24, '桌面共享门闩与本机入口', 7, '05 43 64 65 75 80', [18, 21], 'POC-09 POC-10', '实现持久inbox/资源代次及环境摘要、本地维护与双引擎/自用/租赁共门闩、loopback受权内核页和可选LAN配对撤销；买家key无安装权限。', '显示版本/节点/模型与更新差异，未知引擎状态不可重跑；有单不能切环境，Host/Origin与管理越权拒绝。'),
 (25, '兼容转发内核与授权上游', 9, '25 26 27 59 71 73 77 80', [11, 15, 18], 'POC-07 E05 E11', '实现BoundRelayCore/FieldPolicy/BackendSelector，固定seller/设备/route，审核上游目标/usage；Payment-Proof不进模型。', '平台明文与卖方解密域使用同语义内核；无全局池/钱包，逐字段策略及同seller重试证据可复核。'),
 (26, '三文本协议与字段档位', 9, '28 54 69 70 79', [25], 'POC-07', '实现三个文本入口的原生直通/无损子集profile，逐字段PASS/MAP/REJECT、工具ID往返和usage口径。', '按FW01—FW18及固定SDK验证JSON/SSE/角色/长度/工具；无静默丢字段、伪造thinking/签名或usage。'),
 (27, 'SSE媒体、音频与资产协议', 9, '13 24 49 69 74 76', [26, 22, 23], 'POC-07 POC-11', '实现有界SSE/取消/原ID恢复，Images/Videos及Audio Speech/Transcriptions批准子集；声明Responses音乐/3D资产结果档位并验证background/retrieve/cancel。', 'FW01—FW22固定SDK字段/JSON/SSE/音频二进制及multipart/资产异步和隐私通过，不伪造官方3D端点或usage，租内不另收费。'),
 (28, 'Sub2API源码裁剪与许可', 9, '71 78 80', [1], 'POC-08 E06', '冻结候选commit/依赖/许可证，记录修改和转发内核边界，完成原生构建/分发义务。', '源码/补丁/SBOM及无钱包/跨seller池/正文遥测审查；未冻结不出产品包。'),
 (29, '端点身份与密钥库', 10, '32 33 36 72 75', [3, 11], 'POC-04 POC-09 E08', '实现独立端点核验/公钥版本、独立签名/加密钥、轮换/撤销及OS秘密存储。', '平台无内容私钥；sealed box匿名性不冒充身份认证，换端须买方核验重封。'),
 (30, '本地加密与内置转发', 10, '33 34 35 72 73 80', [25, 29, 17], 'POC-04 E08', '通过成熟库薄封装实现上下文规范化、sealed key、ordered secretstream及反向响应。', 'GoCGO/JNI互通、FINAL+业务END、篡改/重放/崩溃不复用nonce；seller所有者可见如实披露。'),
 (31, '盲转分块与本地接受', 11, '34 35 39 40 74 76', [19, 27, 30], 'POC-04 POC-09 E08', '实现平台opaque frame/channel、配额/有界重排、已认证本地媒体缓存/Range和签名收据。', '平台不解密/猜token；无回执不自动成交；乱序/断流/缓存清理/重传有证据。'),
 (32, '增强档证明放钥门槛', 10, '37 38 40 48', [29, 30], 'E08 E09', '实现独立证明/放钥政策和全路径覆盖，绑定kernel_environment_digest及实际CLI/daemon/runtime、ComfyUI/节点/编解码或native测量；替换必须重验。', '真实硬件覆盖全路径，节点/内核替换及目录漂移使旧证明不可用；mock/普通进程永不解锁，不能静默降隐私。'),
 (33, '租赁占位及双方就绪', 12, '41 42 43 48', [15, 18, 24], 'POC-10', '实现同卡独占、零费准备、设备/买方就绪、单次起表及受控工作区grant。', '超时/预热失败零费；默认STANDARD且禁止shell/RDP/admin/任意节点。'),
 (34, '可用区间与最终计费', 12, '18 44 46 61', [12, 33], 'POC-10 E11', '实现序号/单调时/资源代次证据、区间去重合并、累计毫秒一次取整和预算停止。', '重叠/重复/UNKNOWN不多计，准备/故障不算可用；与最终statement逐区间可复核。'),
 (35, '续租断线与硬截止', 12, '45 46', [13, 14, 33, 34], 'POC-10 E02', '实现显式续租报价/支付有效后延长，断线宽限、签名本地deadline和到期强停。', '网断/支付未知/续租晚到不超已付截止；不能靠客户端连接恢复自动续费。'),
 (36, '租赁撤权导出与清理', 12, '22 43 46 47 52', [20, 33, 35], 'POC-10', '实现结束撤grant、受控导出、真实停止及租户工作区/缓存清理ACK。', '下一租户无旧结果/上下文，清理失败隔离；先释放再删的竞态被测试拒绝。'),
 (37, '媒体对象生命周期', 13, '13 15 22 34 49 52 76', [4, 10], 'POC-09 E12', '实现单对象授权/明密文隔离/受控取回、图像视频音频3D规格检查与运行删除；3D拒脚本/任意外部纹理，音频真实解码。备份方案延后。', '坏包/超限/解析炸弹和3D外链拒绝；密文不解码，Responses资产描述不等于真实产物通过，未清不可复用。'),
 (38, '可信usage与价格计算', 14, '18 27 44 53 61', [12, 19, 23], 'E11', '实现固定tokenizer/模板/缓存口径、输入输出M token双价、原始数量/有理费率和证据版；当前/最终账单与净额投影有水位。', '本地/手机/上游自报非默认可信；无可信usage关闭新售按量，不估扣/伪造0/暗改固定价；旧合同原版保留。'),
 (39, '成交资格与追加镜像账', 15, '14 18 19 39 53', [14, 19, 34, 38], 'POC-06', '实现接受/最终租时/真实用量成交资格、平衡分录和累计退款差额冲正。', '一源唯一journal，历史entry不可追加修改；部分退款/重复事件金额守恒。'),
 (40, '个人卖家渠道结算', 15, '19 20 53 55', [15, 16, 39], 'POC-06 E01 E02', '按实际seller渠道绑定实现到账/成交资格/争议hold、渠道结算能力及原号查询；直收路径只记录到账不重复平台出款。', '个人收款/平台费资格有正式证据；同源不双出款，UNKNOWN先查；模拟金额不算正式销量。'),
 (41, 'LMA源码二开与内核桥基座', 8, '03 57 62 67 68 80', [1, 6, 7], 'POC-03 E04 E10', '固定LMA源码/依赖许可和原生链，在本项目建立派生Android工程；复用Java服务/WebView/EngineHost/ApiHost，新增Kotlin可互操作PlatformBridge。', '上游commit/本地补丁/许可清单，两款真机安装/升级/ABI/JNI；不改外部仓库，不把原LMA写成已是Compose。'),
 (42, '手机本地推理和执行槽', 8, '59 61 62 64 80', [18, 25, 41], 'POC-03 E10 E11', '在LMA派生工程内接本机native推理和GatewayLite，标准协议转换/环境租约校验、原聊天/API/共享共门闩及真实usage。', '断PC/云仍本机执行，环境一致不重复计算/收益；CPU不标GPU，不标整卡租赁，标准协议能力逐项证实。'),
 (43, '手机生命周期及共享条件', 8, '05 63 64 65', [41, 42], 'POC-03 E10', '改造LMA前台服务共享生命周期、充电/WiFi/热条件、用户停止；内核可执行组件随受签APK更新，模型独立选择下载，更新前drain后先恢复查询补清。', '两真机锁屏/强杀/切网/取消、APK/模型分离与无任意下载代码执行；后台恢复不回绕代次或重算，通知和停止可见。'),
 (44, '手机权限与加密角色', 8, '33 35 66 72 73 75 80', [29, 30, 41, 42], 'POC-03 POC-04 E08 E10', '在LMA派生端实现最小权限、Keystore用途隔离、平台桥/端侧加解密、模型调用与管理权限分离和清数据后新身份。', '不读私人聊天/相册/联系人，Keystore不宣称owner-blind；旧钥不复活执行，派生转发与本机资源/环境一致。'),
 (45, 'Taro小程序买家交易主链', 19, '07 08 09 15 17 31 32 39 48', [10, 13, 19, 37], 'POC-07', '以React/Taro支付宝目标实现市场/seller选择、三模式价费确认、PayLauncher、订单/接受/退款/结果及前后台恢复。', '支付宝真机付款返回查单、弱网/拒绝/重复点击不二付；默认非流式或控制面job轮询，不假设微信chunked能力。'),
 (46, 'Taro卖家与Web设备挂牌收益', 19, '02 04 05 19 29 57 63', [8, 40], 'POC-02 POC-03', '小程序提供卖家准入、设备状态、挂牌未来版/时段及收益；React/Vite Web补复杂配置，原生端负责真实执行。', '真机/物理设备联调，安装不等于可售；无保底/自营采购暗示，停止共享与清理/尾单分别显示。'),
 (47, 'React Web开发者与可信程序配对', 19, '28 43 54 69 72 75 79', [11, 26, 29, 33], 'POC-07 POC-09', 'React/Vite实现三方言SDK配置、RouteContract/预算、撤销key及本机/LAN指引；小程序只做配对摘要和交易控制。', '共享纯TS DTO/金额/状态，分端transport/路由；无内容私钥进入平台网页，不静默降模式或协议。'),
 (48, 'React Web运营财务与售后', 19, '20 21 23 50 55', [4, 40, 49, 52], 'E12', 'React/Vite实现证据审查/开关/争议/渠道查单/批次复核与业务审计；不建设观测仪表盘或告警后台。', '运营不可读非授权内容或出款，财务双人权限后端执行；跨角色/账号缓存隔离，资金原号恢复可核。'),
 (49, '争议申诉与最小披露', 16, '21 39 40 46 50', [4, 14, 19], 'E12', '实现双方证据/责任/决定/申诉，争议hold与限时内容授权。', '不索私钥/全量prompt；未知资金不凭客服按钮改成功，申诉保留旧决定。'),
 (50, '评价信誉与反关联', 16, '07 29 30', [8, 19, 49], 'E13', '实现仅真实成交主体评价、关联/刷单信号和可申诉可解释限制。', '测试/关联/退款交易分开统计；不能购买删差评或把长期包当收益保底。'),
 (51, '事件outbox与持久任务', 18, '11 15 16 17 20 22 52', [2, 3], 'POC-05', '实现事务outbox/inbox、claim_generation、有限重试/死信、通知和清理/查单worker。', '重复20次只一次业务副作用；过期认领旧worker写回拒绝，恢复不重放扣款。'),
 (52, '证据门槛与撤销发布', 17, '23 24 25 37 38 40 48 55 56 67 68 70 79', [7, 15, 28, 51], 'E01 E03 E04 E06 E07 E08 E09 E10 E12', '实现seller/device/SKU/mode/privacy/profile/region证据约束、审批/到期撤销和默认关闭。', '开关无法绕过证据；每组合许可/资金/实机/SDK均齐后才售，失败不降档。'),
 (53, '安全与隐私攻击验证', 20, '01 22 32 37 38 40 50 52 66 75', [4, 20, 30, 37, 44], 'POC-04 POC-09 E12', '实施跨租户/秘密/Origin/SSRF/流篡改/升级包/内容留存攻击场景及独立安全审查。', '原始证据和修复回归可复核；基本E2E与增强威胁模型分别判断。'),
 (54, '协议资源边界与运行故障验证', 20, '10 11 14 16 45 47 63 68 74', [19, 27, 35, 43, 51], 'POC-05 POC-11', '原生故障验证流限额/取消/旧代次/租赁截止，以及内核替换中断、撤销回滚、节点目录漂移、LMA升级恢复；部署/观测延后。', 'FW01—FW22及K01—K20适用真实证据，未知原ID恢复和双围栏/账证不回绕，未停清不复用。'),
 (55, '部署运维与可观测性（本轮延后）', 20, '20 22 23 51 52 58 65 68 78', [], 'POC-12', '依用户当前范围，本轮不规划HTTPS、部署发布、备份运维或可观测性，保留历史需求编号并标DEFERRED。', '后续用户恢复范围后另定实施和证据；本轮不生成相关方案，也不将延后项写成已通过或当前依赖。'),
 (56, '本轮需求与真实组合验收登记', 20, '01 24 25 28 30 37 38 56 68 79 80', [45, 46, 47, 48, 50, 53, 54], 'E01 E02 E03 E04 E05 E06 E07 E08 E09 E10 E11 E12 E13 E15', '保留80项F/T追踪，按CURRENT/PARTIAL/DEFERRED执行当前适用场景，增加小程序/转发/收单证据，不依赖DEV-55。', '真实登记PASS/FAIL/BLOCKED/NOT_EXECUTED，延后不计通过；未测组合不开启，文档通过不替代产品通过。'),
]

# Every control operation has exactly one implementing task. No HTTP AI protocol
# endpoints are invented here: those use vendor profiles, not this control map.
OPERATION_GROUPS = {
 3:"getSelfProfile listApiKeys createApiKey revokeApiKey createAuthChallenge createUserSession getCurrentSession revokeUserSession",
 4:"recordConsent createDeletionRequest",
 5:"createSellerApplication getSellerProfile requestSellerExit",
 6:"listOwnDevices createPairingIntent enrollDevice getDevice recordDeviceHeartbeat drainDevice revokeDevice updateResourcePolicy",
 7:"registerCapability listCatalogEntries registerReviewedEvidence",
 8:"listOwnListings createListing getOwnListing updateFutureListing submitListing pauseListing listServices",
 9:"createQuote getQuote",
 10:"listOrders createOrder getOrder",
 11:"listRouteContracts createRouteContract getRouteContract",
 13:"createPaymentIntent getPayment",
 14:"createRefund getRefund",
 17:"createJob getJob",
 18:"claimLease recordAttemptProgress",
 19:"registerAttemptDelivery recordBuyerReceipt",
 20:"cancelJob recordAttemptCleanup",
 29:"createEncryptionIntent registerRecipient getRecipient createRecipientRotation revokeRecipient",
 30:"registerEnvelope",
 31:"createCipherChannel",
 32:"createAttestationChallenge getAttestationEvidence",
 33:"createRentalSession getRentalSession confirmRentalReady recordRentalDeviceReady createRentalAccessGrant",
 34:"recordRentalAvailability getRentalMeterStatement",
 35:"createRentalRenewal stopRentalSession",
 36:"createRentalExport recordRentalCleanup",
 37:"registerAsset getAsset completeAsset createAssetTransferGrant listResults",
 38:"recordAttemptUsage getOrderBill",
 40:"listOwnSettlements getSettlement createSettlementBatch getSettlementBatch approveSettlementBatch submitSettlementBatch",
 49:"createDispute getDispute createAppeal recordDisputeDecision",
 50:"createReview",
 51:"listNotifications",
 52:"createCapabilityRelease updateFeatureGate listAuditEvents",
}

# Primary implementation module for each unchanged F01..F80. Supporting modules
# and tasks are derived from concrete task references, not guessed from keywords.
PRIMARY_MODULES = [1,2,7,3,6,3,2,4,5,6,6,7,13,6,19,6,5,14,15,15,
 16,13,17,9,9,6,14,9,16,4,4,1,10,10,10,10,10,10,15,17,
 12,12,12,14,12,12,12,12,9,1,20,13,15,9,17,17,3,7,7,7,
 14,8,8,6,7,8,3,20,9,9,9,10,10,11,10,13,4,9,19,9]


def req_ids(value: str) -> list[str]:
    return [f"P11-F{int(x):02d}" for x in value.split()]


def modules(chapters: dict[int, str]) -> list[dict]:
    result=[]
    for n,name,owner,package,chapter,tables,services,deps in MODULE_ROWS:
        package_path = ("dev/src/miniapp/src" if n==19 else
            "dev/src/client/android" if n==8 else "dev/src/client" if n==7 else
            "dev/src/gateway/core" if n==9 else "dev/src/client/crypto-gateway" if n==10 else
            "dev/src/gateway/cipher-relay" if n==11 else "dev/tests" if n==20 else f"dev/src/internal/{package}")
        result.append({"id":f"MOD-{n:02d}","name":name,"owner_role":owner,
            "planned_package":package_path,"source_document":chapters[chapter],
            "authoritative_tables":tables.split(),"application_services":services.split(),
            "dependencies":[f"MOD-{d:02d}" for d in deps],"status":STATUS,
            "test_status":"NOT_EXECUTED",
            "planned_packages":(["dev/src/miniapp/src","dev/src/web/src","dev/src/frontend/packages"] if n==19 else [package_path])})
    return result


def tasks(module_catalog: list[dict], chapters: dict[int, str]) -> list[dict]:
    m={x['id']:x for x in module_catalog}
    result=[]
    for n,title,mod,reqs,deps,gates,work,done in TASK_ROWS:
        privacy_details = {
            7: ('六桌面组合增加两OS的LLAMA_CPP_NATIVE，独立登记本地防护/性能证据。', '原生worker资格不借用LM Studio许可/测试。'),
            18: ('输入准备/结果密文交付不额外持GPU锁，编码仍用GPU则继续持锁；控制通道独立。', '旧attempt回调不能清下一资源owner，租赁子任务不释放会话锁。'),
            21: ('实现独立低权限执行身份与定向继承数据/控制管道，worker可复用。', '不加VM、常驻Chromium或第二份模型，UI无正文/解密/任意文件权限。'),
            22: ('增加不开Web服务的ComfyPrivacyBridge和首次写盘前加密的媒体sink。', '全部媒体支路无普通temp/预览/历史API旁路，私有入口补丁随环境摘要复验。'),
            23: ('新增llama.cpp C API原生C++ worker，通过管道执行隐私文本/流式；原LM Studio三层封装保留兼容。', '两OS分别验证真实GPU/上下文/usage/取消；原生worker无正文TCP端口，不把CPU abort回调当CUDA即时中断。'),
            29: ('实现双向接收钥独立信任、用途隔离、正常/泄漏轮换、撤销未知拒绝及丢钥恢复边界。', 'E2E-01/02/04/14；不把TOFU当首次证明，不宣称静态接收钥具有完整前向保密。'),
            30: ('实现C→sealed→H签名→记录AAD、BINARY_STREAM_1及LOCAL_SEGMENTED_1，Go/C++/JNI同库；先补二进制channel协商/版本Schema。', 'E2E-03—11/14/15；END本身TAG_FINAL，无循环摘要/重用状态，顺序流不伪称seek，失败不重跑。'),
            31: ('实现二进制盲转、4MiB共享配额/8ms聚合、原密文补传、完整认证后Range及签名回执唯一终态。', 'E2E-07—13/15；原合法交付不因释放GPU失效，交付scope无计算权，无回执不自动成功。'),
            37: ('增加图/视频/音频/3D流式编码加密、不可变分段缓存与spool配额。', '不增加全片RAM/帧副本或普通文件中转，格式与标准SDK按合同验收。'),
            44: ('复用LMA内部Java/JNI调用和受控私有存储，不启动额外Go/Node/Python。', '真机新增活动内存/缓冲按方案A/B，个人API/历史/通知/logcat不泄漏。'),
            51: ('增加PE-01—PE-08/LP专项及同模型同机30次A/B的内存/VRAM/性能预算验证。', '所有数字以物理机/真机证据裁定，测试绕过不得出现在正式可启用配置。')
        }
        if n in privacy_details:
            extra_work, extra_done = privacy_details[n]
            work += extra_work
            done += extra_done
        if n == 23:
            title = '原生隐私LLM与LM Studio兼容内核'
        mid=f"MOD-{mod:02d}"
        result.append({"id":f"DEV-{n:02d}","title":title,"module_id":mid,
            "owner_role":m[mid]['owner_role'],"requirement_ids":req_ids(reqs),
            "acceptance_ids":[r.replace('-F','-T') for r in req_ids(reqs)],
            "dependencies":[f"DEV-{d:02d}" for d in deps],"gates":gates.split(),
            "implementation":work,"done_evidence":done,
            "source_documents":list(dict.fromkeys([m[mid]['source_document'],chapters[21],chapters[18],"docs/技术开发/附录-本轮范围与端侧选型修订.md"] + (["docs/技术开发/附录-协议字段映射与兼容档位.md"] if n in (25,26,27,28) else []) + (["docs/技术开发/附录-供给端内核封装与替换规范.md"] if n in (7,9,10,18,20,21,22,23,24,27,32,33,37,41,42,43,44,49,51,54,56) else []))),
            "development_status":"DEFERRED" if n==55 else "DESIGN",
            "current_phase_scope":"DEFERRED" if n==55 else "CURRENT",
            "test_status":"NOT_EXECUTED",
            "actual_owner":None,"estimate":None,"evidence_refs":[]})
        if n in privacy_details:
            result[-1]['source_documents'].append('docs/技术开发/附录-低开销隐私执行完整方案.md')
        if n in (18, 21, 22, 23, 26, 27, 29, 30, 31, 32, 37, 44, 51):
            result[-1]['source_documents'] = list(dict.fromkeys(result[-1]['source_documents'] + [
                'docs/技术开发/08-端到端加密协议与密钥实现设计.md']))
        if n == 23:
            result[-1]['branch_gates'] = {'LLAMA_CPP_NATIVE': ['POC-02', 'E04', 'E11'],
                'LM_STUDIO': ['POC-02', 'E03', 'E04', 'E11']}
        if n in (1,2,3,8,9,10,51,53,56):
            result[-1]['source_documents'].append('docs/技术开发/附录-身份登录与HTTP交易实施约束.md')
    return result


def operation_tx(op: str, method: str) -> str:
    if method=='get': return "READ_PROJECTION"
    if op=="createAuthChallenge": return "AUTH_CHALLENGE_SINGLE_USE"
    if op=="createUserSession": return "TX_CLAIM → EXTERNAL_SIGNED_OAUTH → TX_SESSION"
    if op=="revokeUserSession": return "TX_REVOKE_AND_USER_VERSION"
    if op in ('createQuote','createOrder','createRouteContract'): return 'TX-02'
    if op=='createPaymentIntent': return 'TX-03'
    if op in ('createJob','claimLease','recordAttemptProgress'): return 'TX-04'
    if op in ('registerAttemptDelivery','recordBuyerReceipt','cancelJob'): return 'TX-06'
    if op in ('createRentalSession','confirmRentalReady','recordRentalDeviceReady','createRentalAccessGrant'): return 'TX-08'
    if op=='recordRentalAvailability': return 'TX-09'
    if op in ('stopRentalSession','recordRentalCleanup','recordAttemptCleanup'): return 'TX-10'
    if op=='createRefund': return 'TX-11'
    if 'SettlementBatch' in op: return 'TX-12'
    return 'TX-01_OR_MODULE_LOCAL'


def engine_schema() -> dict:
    idtype={"type":"string","pattern":"^[A-Za-z][A-Za-z0-9_-]{2,127}$"}
    counter={"type":"string","pattern":"^(0|[1-9][0-9]{0,19})$"}
    # Application also bounds counter <= 9223372036854775807.
    schema={"$schema":"https://json-schema.org/draft/2020-12/schema",
      "$id":"https://p11.invalid/design/engine-adapter-v1.0.schema.json",
      "title":"P11 local engine command draft; no product implementation",
      "type":"object","additionalProperties":False,
      "properties":{
       "schema_version":{"const":"1.0-draft"},"command_id":idtype,
       "operation":{"enum":["DESCRIBE","PROBE","PREPARE","EXECUTE","QUERY","CANCEL","COLLECT","CLEANUP","HEALTH"]},
       "execution_domain":{"const":"AUTHORIZED_LOCAL_ENDPOINT"},
       "context":{"type":"object","additionalProperties":False,"properties":{
         "authority_kind":{"enum":["JOB_LEASE","RENTAL_SESSION"]},
         **{k:idtype for k in ('order_id','seller_id','device_id','resource_id','job_id','attempt_id','lease_id')},
         **{k:idtype for k in ('session_id','invocation_id','access_grant_id')},
         "terms_revision":counter,
         "fencing_token":counter,"resource_lock_generation":counter,
         "contract_hash":{"type":"string","pattern":"^[a-f0-9]{64}$"},
         "deadline":{"type":"string","format":"date-time"}},
         "required":["authority_kind","order_id","seller_id","device_id","resource_id","resource_lock_generation","contract_hash","deadline"],
         "oneOf":[
           {"properties":{"authority_kind":{"const":"JOB_LEASE"}},
            "required":["job_id","attempt_id","lease_id","fencing_token"],
            "not":{"anyOf":[{"required":[x]} for x in ('session_id','invocation_id','access_grant_id','terms_revision')]}},
           {"properties":{"authority_kind":{"const":"RENTAL_SESSION"}},
            "required":["session_id","invocation_id","access_grant_id","terms_revision"],
            "not":{"anyOf":[{"required":[x]} for x in ('job_id','attempt_id','lease_id','fencing_token')]}}]},
       "authority_payload_ref":idtype,
       "engine_kind":{"enum":["COMFYUI","LM_STUDIO","LLAMA_CPP_NATIVE","LMA_LOCAL","AUTHORIZED_UPSTREAM"]},
       "protocol_profile_id":idtype,"recipe_digest":{"type":"string","pattern":"^[a-f0-9]{64}$"},
       "kernel_environment_digest":{"type":"string","pattern":"^[a-f0-9]{64}$"},
       "input_asset_refs":{"type":"array","items":idtype,"maxItems":50,"uniqueItems":True},
       "local_protocol_body_ref":idtype,
       "backend_reference":{"type":"string","minLength":1,"maxLength":256},
       "idempotency_key":{"type":"string","minLength":8,"maxLength":128}},
      "required":["schema_version","command_id","operation","execution_domain","engine_kind","idempotency_key"],
      "allOf":[{"if":{"properties":{"operation":{"enum":["PREPARE","EXECUTE","QUERY","CANCEL","COLLECT","CLEANUP"]}}},
        "then":{"required":["context","authority_payload_ref","kernel_environment_digest"]}},
        {"if":{"properties":{"operation":{"const":"EXECUTE"}}},
         "then":{"required":["protocol_profile_id","recipe_digest","input_asset_refs","local_protocol_body_ref"]}}],
      "$comment":"Local boundary only; platform never receives protocol bodies/keys. JOB_LEASE validates current signed lease; RENTAL_SESSION validates signed current session/access grant, same resource owner and terms revision, no per-invocation sale. PREPARE needs actual execution authority, distinct from no-execution E2E attempt PREPARED. Validate signatures/generation/ownership/profile in application; schema cannot prove them."}
    return schema


def engine_result_schema() -> dict:
    idtype={"type":"string","pattern":"^[A-Za-z][A-Za-z0-9_-]{2,127}$"}
    return {"$schema":"https://json-schema.org/draft/2020-12/schema",
      "$id":"https://p11.invalid/design/engine-adapter-result-v1.0.schema.json",
      "title":"P11 local adapter safe result draft","type":"object","additionalProperties":False,
      "properties":{"schema_version":{"const":"1.0-draft"},"command_id":idtype,
       "status":{"enum":["SUCCEEDED","PENDING","REJECTED","FAILED","UNKNOWN"]},
       "execution_state":{"enum":["NOT_STARTED","RUNNING","STOPPING","STOPPED","CLEANED","UNKNOWN"]},
       "safe_code":{"type":"string","pattern":"^[A-Z][A-Z0-9_]{2,63}$"},
       "backend_reference":{"type":"string","maxLength":256},
       "retryability":{"enum":["QUERY_ORIGINAL","RETRY_SAME_COMMAND","NOT_RETRYABLE"]},
       "evidence_refs":{"type":"array","items":idtype,"maxItems":50},
       "local_output_refs":{"type":"array","items":idtype,"maxItems":50},
       "capability_manifest_ref":idtype,"usage_evidence_ref":idtype},
      "required":["schema_version","command_id","status","execution_state","safe_code","retryability","evidence_refs"],
      "allOf":[{"if":{"properties":{"status":{"const":"UNKNOWN"}}},
                "then":{"properties":{"retryability":{"const":"QUERY_ORIGINAL"}}}}],
      "$comment":"SUCCEEDED means this command succeeded, not automatically a completed inference, buyer acceptance, refund or payout. Query original backend reference after uncertainty; no shell/path/secret/raw body in safe result."}


def design_supplements() -> dict[str, dict]:
    """Current-scope design manifests; neither vendor API schemas nor runtime evidence."""
    common={'version':VERSION,'date':DATE,'status':STATUS,'test_status':'NOT_EXECUTED'}
    frontend={**common,'primary_surface':'ALIPAY_MINIAPP',
      'miniapp':{'framework':'React + Taro','react_compatibility_line':'18',
        'taro_major_candidate':'4.x','compiler_candidate':'webpack5','frozen_versions':None,
        'planned_package':'dev/src/miniapp','default_ai_transport':'NON_STREAMING_JSON_OR_CONTROL_JOB_POLLING',
        'sse_validated':False,'e2e_role':'CONTROL_AND_PAIRING_SUMMARY'},
      'web':{'framework':'React + Vite','planned_package':'dev/src/web',
        'roles':['DEVELOPER','COMPLEX_BUYER_TASKS','SELLER_CONFIGURATION','OPERATIONS','FINANCE'],
        'sse_design':True,'frozen_versions':None},
      'shared':{'planned_package':'dev/src/frontend/packages','language':'TypeScript',
        'packages':['domain','api-types','api-client','ui-tokens'],
        'forbidden_runtime_dependencies':['window','document','react-dom','Taro','Node networking/filesystem']},
      'payment_actions':['HOSTED_PURCHASE','NATIVE_TRADE','A402_APPROVAL'],
      'deferred':['HTTPS_AND_CERTIFICATES','DEPLOYMENT_AND_OPERATIONS','OBSERVABILITY'],
      'deferred_tasks':['DEV-55'],'deferred_pocs':['POC-12'],
      'scope_document':'docs/技术开发/附录-本轮范围与端侧选型修订.md',
      'source_document':'docs/技术开发/14-前端路由状态与接口集成.md'}
    rows=[
      ('CHAT_NATIVE','OPENAI_CHAT','OPENAI_CHAT','NATIVE_SUBSET','POST /v1/chat/completions',['model','messages'],'QUALIFIED_NATIVE_LLM_LM_STUDIO_OR_AUTHORIZED_UPSTREAM'),
      ('RESPONSES_NATIVE','OPENAI_RESPONSES','OPENAI_RESPONSES','NATIVE_SUBSET','POST /v1/responses',['model','input'],'QUALIFIED_NATIVE_RESPONSES'),
      ('MESSAGES_NATIVE','ANTHROPIC_MESSAGES','ANTHROPIC_MESSAGES','NATIVE_SUBSET','POST /v1/messages',['model','messages','max_tokens'],'QUALIFIED_NATIVE_MESSAGES'),
      ('RESPONSES_TO_CHAT','OPENAI_RESPONSES','OPENAI_CHAT','LOSSLESS_TEXT_SUBSET','POST /v1/responses',['model','input'],'QUALIFIED_CHAT_BACKEND'),
      ('MESSAGES_TO_CHAT','ANTHROPIC_MESSAGES','OPENAI_CHAT','LOSSLESS_TEXT_SUBSET','POST /v1/messages',['model','messages','max_tokens'],'QUALIFIED_CHAT_BACKEND'),
      ('IMAGES_GENERATE','OPENAI_IMAGES','COMFYUI_RECIPE','RECIPE_MAPPING','POST /v1/images/generations',['model','prompt'],'COMFYUI'),
      ('IMAGES_EDIT','OPENAI_IMAGES','COMFYUI_RECIPE','RECIPE_MAPPING','POST /v1/images/edits',['model','image'],'COMFYUI'),
      ('VIDEOS_ASYNC','OPENAI_VIDEOS','COMFYUI_RECIPE','SDK_VERIFIED_ASYNC_SUBSET','CREATE_GET_CONTENT_PER_FROZEN_SDK',[],'QUALIFIED_VIDEO_RECIPE'),
      ('AUDIO_SPEECH','OPENAI_AUDIO_SPEECH','COMFYUI_RECIPE','APPROVED_AUDIO_SUBSET','POST /v1/audio/speech',['model','input','voice'],'QUALIFIED_AUDIO_RECIPE'),
      ('AUDIO_TRANSCRIPTIONS','OPENAI_AUDIO_TRANSCRIPTIONS','COMFYUI_RECIPE','APPROVED_AUDIO_SUBSET','POST /v1/audio/transcriptions',['model','file'],'QUALIFIED_ASR_RECIPE'),
      ('RESPONSES_ASSET_RECIPE','OPENAI_RESPONSES','COMFYUI_RECIPE','DECLARED_STRUCTURED_ASSET_RESULT','POST /v1/responses; background/retrieve/cancel only after SDK validation',['model','input'],'QUALIFIED_MUSIC_OR_3D_RECIPE'),
    ]
    profiles=[]
    for ident,source,target,kind,endpoint,required,engine in rows:
        profiles.append({'id':ident,'source_dialect':source,'target_dialect':target,
          'conversion_kind':kind,'endpoint_design':endpoint,'minimum_design_fields':required,
          'backend_candidate':engine,'field_decisions':['PASS','MAP','REJECT'],
          'seller_binding':'EXACT_CONFIRMED_SELLER','sdk_versions':None,'api_version':None,
          'model_binding':None,'usage_evidence':None,'evidence_refs':[],
          'sale_enabled':False,'test_status':'NOT_EXECUTED',
          'source_document':'docs/技术开发/附录-协议字段映射与兼容档位.md'})
    forwarding={**common,'profiles':profiles,'data_plane_private_json_fields_required':False,
      'idempotency_header':'Idempotency-Key','idempotency_header_required':False,
      'sdk_without_stable_key_note':'No exactly-once purchase guarantee; disable automatic paid retries or supply a stable intent header.',
      'payment_headers':{'challenge':'amtPaymentLink','proof':'Payment-Proof','proof_forwarded_to_engine':False},
      'retry_policy':{'cross_seller':False,'same_seller_max_reassignments':1,'retry_after_content_sent':False},
      'limits_design':{'request_json_bytes':2097152,'headers_bytes':32768,'payment_proof_header_bytes':16384,
        'sse_event_bytes':1048576,'tool_arguments_bytes':1048576,'stream_buffer_bytes':4194304},
      'e2e_platform_content_access':False,'rental_per_invocation_payment':False,
      'conformance_cases':[f'FW{i:02d}' for i in range(1,23)],
      'source_document':'docs/技术开发/07-统一协议网关与Sub2API改造.md'}
    payment={**common,'design_product':'ALIPAY_MACHINE_PAY_A402','runtime_enabled':False,
      'qualification_status':'UNVERIFIED_C2C_SELLER_AND_SERVICE','sandbox_is_real_payment':False,
      'public_api_snapshot':'2026-10-28','create_method':'alipay.aipay.agent.payment.precreate',
      'verify_method':'alipay.aipay.agent.payment.verify',
      'sequence':['FIX_IMMUTABLE_ORDER','PRECREATE','HTTP_402_CHALLENGE','USER_PAYMENT','VERIFY_PROOF','UNIQUE_FULFILLMENT'],
      'public_api_constraints':{'currency':'CNY','currency_required':True,'max_amount_fen':5000,
        'max_pay_before_seconds_from_now':43200,'out_trade_no_max_chars':32,'service_id_required':True},
      'project_constraints':{'minimum_amount_fen':1,'local_order_service_seller_binding_required':True,
        'cross_seller_proof_reuse':False,'automatic_split_to_bypass_limit':False},
      'proof_validation':{'response_signature_required':True,'active_must_be_true':True,
        'amount_match_required':True,'out_trade_no_match_required':True,'trade_no_unique_fulfillment':True,
        'resource_id_required':False,'resource_id_api_status':'DEPRECATED_OPTIONAL'},
      'unproven_capabilities':['PERSONAL_COMPUTE_SELLER_ADMISSION','C2C_COMMISSION_SPLIT','REFUND_CONTRACT',
        'USAGE_POSTPAY','REFUNDABLE_BUDGET_HOLD','VERIFY_THEN_SETTLE_API','MINIAPP_NATIVE_PAYMENT_HANDOFF'],
      'alternative_nowpay':{'enabled':False,'funds_flow_official':'BUYER_TO_CREATOR_DIRECT',
        'quota_reversal_supported_in_current_guide':False,'gpu_minute_rental_equivalence':False},
      'sources':[
        'https://aipay.alipay.com/community/events/payathon-2026',
        'https://aipay.alipay.com/docs/ai-receive/MACHINE_PAY.html',
        'https://aipay.alipay.com/docs/ai-receive/api-list/alipay-aipay-agent-payment-precreate.html',
        'https://aipay.alipay.com/docs/ai-receive/api-list/alipay-aipay-agent-payment-verify.html',
        'https://aipay.alipay.com/docs/vibe-pay/nowpay/platform/platfrom-guide.html'],
      'source_document':'docs/技术开发/13-支付退款账务与渠道结算.md'}
    return {'frontend-scope-v1.0.json':frontend,'forwarding-profiles-v1.0.json':forwarding,
      'payment-product-profile-v1.0.json':payment}
