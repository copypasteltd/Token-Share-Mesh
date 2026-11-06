# 开发实施进度

目标：按全部开发细则交付成体系源码。当前源码目标 ACTIVE，源码实施进展与需求/设计工作分别记录。

当前为[R026全量代码阶段](R026-全量代码阶段进展.md)，SOURCE_ONLY / 未验证；14项实现登记及全部56任务/222子项保持开放。新增小时租赁原订单事务、前向014、7项HTTP和一次授权响应源码，共28项租赁测试定义未运行。先完成全部生产代码/真实依赖接线/测试源码，再统一编译、测试和调试。下述R025仅为历史验证身份。

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

R026现登记10项源码进展，含小时租赁领域规则、成熟JWS/控制活性和完整确认历史计量；[本轮进展](R026-全量代码阶段进展.md)列出完整56项，[全量代码清单](../全量代码优先实施清单.md)列出222子项。18项租赁测试和8项Android测试只有源码；平台实际会话/资金/Android与物理owner仍待接通，代码阶段未完成，测试阶段未开放。

## R026 CE-001—036 源码检查点

R026新增[CE-001—036](../../docs/技术开发/附录-ComfyUI完整环境安装与原节点登记源码.md)：独立签名/本人许可节点同意、固定完整目录、有限节点依赖、有界ZIP安装/全树复核、原节点AEAD登记、原物理维护共槽切换和五项本机管理入口源码。官方ComfyUI固定v0.44.0/2c75dc1b379a59e0880619d029607af5169a62ef，尚未制作完整实际运行包；31项新测试定义全部未编译/运行。没有ComfyUI媒体执行/首写加密/租内接线或资格/性能结论。RN-110/67租赁测试、全部56任务222子项保持开放，代码齐备后统一验证；无SQL变更/迁移。

## R026 MW-001—044 私有媒体源码继续 / Private media source

R026新增[MW-001—044](../../docs/技术开发/附录-ComfyUI私有媒体执行与本地加密源码.md)：固定原生私有Comfy执行器、GPU UUID/PCI复核、有限配方、PNG/分片MP4/PCM WAV/几何GLB编码、原锚/加密预登记/分块GCM及Go一次原scope凭据/私有帧源码。44项新测试定义未编译/运行；正式原宿主启动器/租内媒体/完整运行包/公开交付/低权限/E2E与性能仍待。CE31/RN67及全部56任务222子项仍未收口，代码齐备后统一验证；本次无SQL变更。

## R026 CH-001—036 原环境与宿主源码继续 / Original environment and host source

R026新增[CH-001—036](../../docs/技术开发/附录-ComfyUI原环境持有与本机进程接线源码.md)：原Comfy环境运行持有、固定原生进程/三管道、PIPE_2准备/一次原授权ACK/单调截止和原ProviderRental准备接线。29项新测试定义未编译/运行；CH-033—036正式租内媒体Backend、认证工作区/恢复清理、公开交付、完整包/低权限/E2E、独立ready/计量/付费续租/平台释放仍待。全56任务/222子项开放，无SQL变更、迁移或真实执行；完整代码后统一验证，源码检查点不是开发轮完成。

## R026 WS-001—030 原媒体工作区与结果托管源码 / Original media workspace and custody source

R026新增[WS-001—030](../../docs/技术开发/附录-租内媒体工作区与结果托管源码.md)：原租赁媒体工作区/native003元数据预留、原节点目录持有/退役删除/清理专用恢复、认证RAM媒体输入、专用子调用适配器和结果钥托管/读取撤销。23项新测试定义均未编译/运行；公开SDK、完整媒体会话监督/独立停工对账/ready/续租/可信水位/平台释放、完整实际包/低权限/E2E/性能仍待。全56任务/222子项开放，只登记native003源，未迁移/真实执行；完整代码后统一验证，源码检查点不是开发轮完成。

## R026 MS-001—036 原媒体会话监督与停止回收源码 / Original media-session supervision source

R026新增[MS-001—036](../../docs/技术开发/附录-原租赁媒体会话监督与停止回收源码.md)：原节点持有的本机媒体会话监督/独立单调截止、真实Comfy叶子、一次原授权/单份输入及实际join、先停工后回收、两类会话共关闭、原媒体结果保留/撤销和本机会话清理CAS源码。27项新增测试定义均未编译/运行；公开媒体SDK/最终ACK、双方ready/可信小时水位/付费续租/平台资金释放、独立物理停工恢复及完整实际运行包/低权限/E2E/性能仍待。全部56任务222子项开放；本次无SQL变更或迁移。完整生产代码/实际依赖/测试源码齐备后统一验证，源码检查点不是开发轮完成。

## R026 MG-001—036 原租赁媒体入口与加密缓存源码 / Original-rental media gateway source

R026新增[MG-001—036](../../docs/技术开发/附录-原租赁媒体兼容入口与加密缓存源码.md)：原租赁本机STANDARD媒体入口、Images/明确旧版Videos/WAV speech/Responses资产映射、严格请求/独立scope、AES-GCM暂存/原HMAC幂等/提交前缓存配额、完整认证结果后加密缓存/本人单Range和原节点真实join源码。37项新增测试定义均未编译/运行。公开SDK/网络监听/E2E媒体传输/最终ACK、双方ready/可信小时计量/付费续租/物理停工对账/平台资金释放、完整实际配方/低权限/性能仍待。全部56任务222子项开放；无SQL变更或迁移。完整生产代码/实际依赖/测试源码齐备后统一验证，本次不是开发轮完成。

## R026 MP-001—040 原支付边界与凭证恢复源码 / Original-payment boundaries and recovery source

R026新增[MP-001—040](../../docs/技术开发/附录-MachinePay原支付边界与凭证恢复源码.md)：实际个人seller原支付范围/双公钥pin、原生RSA2 precreate/verify及有界A402、严格原Payment-Proof/AES-GCM/原生密文工件、付款单调/迟到实付/资金资格/退款累计和原范围对账源码。49项新测试定义全部未编译/运行；合成签名夹具已按固定标准库的双结果接口修正惰性初始化。平台支付事务/同事务outbox/唯一消费/已核原签名回包归档/秘密与授权注册撤销、本人HTTP/SDK/PayLauncher、独占宿主及持久原凭据恢复worker、真实退款/到账/佣金/对账单与可信usage/小时水位/账务仍待。尚无真实个人收单资格或渠道验收，不开放收费。全部56任务222子项开放；无SQL变更或迁移。完整生产代码/实际依赖接线/测试源码后统一验证，本次不是开发轮完成。

## R026 PT-001—040 支付事务与原凭据恢复源码 / Original-payment transactions and recovery source

R026新增[PT-001—040](../../docs/技术开发/附录-MachinePay支付事务与原凭据恢复源码.md)：原个人卖家支付/PAY意图/上下文/outbox事务、独立审核四证/销售开关、提交后一次precreate、完整签名envelope与独立归档钥环、同事务receipt/payment/channel/outbox、原Proof密文/QUERY/专用job、原capability过滤/generation恢复与实际调用关闭源码。新增前向016/DATA072—074，当前源码98规划操作/74平台表/48控制HTTP；16项新测试定义未编译/运行，SQL未迁移。正式API/worker宿主/秘密注册轮换、本人HTTP/SDK/A402/PayLauncher、原可执行资金/退款/设备/截止准入、完整退款/到账/佣金/对账单/付费续租/可信计量接受账务与真实资格仍待。全部56任务222子项开放；完整生产代码/实际依赖接线/测试源码后统一验证，本次不是开发轮完成。

## R026 PH-001—040 原生支付宿主与本人接口 / Native payment host and owner HTTP source

R026新增[PH-001—040](../../docs/技术开发/附录-MachinePay原生宿主与本人支付接口源码.md)：原生显式注册/独立秘密/双钥环、有界文件与原scope、专用恢复和到期原退役宿主、实际join后关闭、既有本人支付create/get两个HTTP及正式API/worker入口源码。当前50控制HTTP/98规划/74平台表，无SQL变更；12项新测试定义及既有全部R026源码均未编译/运行。SDK/Proof/A402/实际PayLauncher、原资金唯一消费/当前退款设备截止准入、完整渠道退款到账佣金对账/续租/可信计量接受账务与真实资格仍待。全部56/222开放，完整生产代码/实际接线/测试源码后统一验证；本次不是开发轮完成。

## R026 GF-001—040 原订单资金分配与当前准入 / Original allocation and current admission source

R026新增[GF-001—040](../../docs/技术开发/附录-原订单资金唯一分配与当前准入源码.md)：原订单唯一资金分配DATA075/前向017、原验签后独立元任务和原子READY/outbox、当前主体四证/gate/退款/期限及路由/小时租赁/密钥三消费者源码。当前50控制HTTP/98规划/75平台表；14项新增测试定义未编译/运行，017未迁移。当前仅显式零费用档；旧正向付款标记夹具升级、SDK/Proof/A402/实际PayLauncher、完整费用退款到账佣金对账/付费续租/可信计量接受账务和真实资格仍待。全部56任务222子项开放，完整生产代码/实际依赖接线/测试源码后集中验证；本次不是整轮完成。

## R026 CK-001—040 原付款挑战与双端恢复 / Original checkout challenge and recovery source

R026新增[CK-001—040](../../docs/技术开发/附录-原订单付款挑战与双端恢复源码.md)：原卖家当前付款选项、原订单唯一付款查找、原402挑战、单Proof头提交/恢复五接口，React/Taro与React/Vite原订单付款页、安全元数据journal和会话代次隔离源码。当前55控制HTTP/103规划/124 schema/75平台表；26项新增测试定义未编译/运行，无SQL变更或迁移。真实获准PayLauncher/小程序付款能力、SDK付款前原买方授权及数据面重试、跨端回调恢复、完整费用退款到账佣金对账/付费续租/可信计量接受账务、旧正向夹具与真实资格仍待。当前端未安装获准PayLauncher，付款唤起按钮保持关闭。历史截图/GIF不能证明本次新页面；按全量代码后集中验证要求暂未重录。全部56任务222子项开放，本次不是整轮完成。

## R026 RF-001—040 原退款适配与加密归档 / Original refund adapter and encrypted archive source

R026新增[RF-001—040](../../docs/技术开发/附录-原退款协议与渠道恢复源码.md)：独立条件原退款profile/原意图、原生RSA2退款及原号查询、提交前UNKNOWN/3秒提交及10秒查询准备/generation/成功保持、完整原签名回执AES-GCM归档源码。refund_fee按整笔累计额处理，签名提交本身不能确认本次部分退款；原号查询完整归属及明确成功才记金融事实。32项新增测试定义未编译/运行，103规划/124 schema/75平台表/55控制HTTP不变，无SQL变更/迁移。默认Machine Pay仍仅precreate/verify；真实原交易退款资格、持久累计锁/提交水位/任务、退款host/worker/HTTP和完整费用/佣金/到账/对账链继续待补。CK真实PayLauncher与SDK付款前授权/数据面恢复、GF031旧正向夹具及全56任务222子项继续开放。先完整源码/实际接线/测试源码，再集中验证和重录媒体；本次不是开发轮完成。

## R026 RT-001—040 原退款事务与宿主恢复 / Original refund transactions and native host recovery source

R026新增[RT-001—040](../../docs/技术开发/附录-原退款持久事务与宿主恢复源码.md)：原退款服务/独立当前未消费价款审批、原实付解密后RSA重验、执行清理/预算/累计守恒、同事务原退款意图/渠道/job/outbox/幂等和提交前UNKNOWN、原号恢复/双代次及原生宿主可选配置源码。新增018及DATA076/077，当前103规划/124 schema/77平台表/55控制HTTP；23项新测试定义未编译/运行，018未迁移。当前未消费价款分支不代替完整含费/已赚取价款冲销/佣金/续租/到账对账；公开退款API、审批工作台/双端售后、独立复核重试、真实原交易资格及CK/GF等仍待。全部56任务222子项开放；先完整生产源码/实际接线/测试源码，再集中验证和重录媒体。本次不是开发轮完成。

## R026 RA-001—040 退款申请与双端审核 / Refund applications and independent review source

R026新增[RA-001—040](../../docs/技术开发/附录-退款申请独立审核与双端售后源码.md)：原买家申请/双方查询、当前独立金融审核和原退款同事务批准、拒绝/协调撤销、七项控制HTTP/同原生宿主、Taro/React售后与Web审核台、安全原请求恢复和会话代次源码。019/DATA078仅源码，当前108规划/128 schema/78平台表/62控制HTTP；32项新测试定义未编译/运行，019未迁移。申请不是批准/渠道结果；完整费用冲销/佣金/续租/到账结算对账、独立复核重试/历史导入、完整争议举证/运营生命周期、CK/GF与真实资格仍待。全部56任务222子项开放，先全部生产源码/实际接线/测试源码，再集中验证和重录媒体；本次不是整轮完成。

## R026 DA-001—040 交付签收与可信账务 / Original delivery, independent usage and billing source

R026新增[DA-001—040](../../docs/技术开发/附录-交付签收独立计量与镜像账源码.md)：原交付/本地签名报告恢复、当前原买家唯一签收与取消、独立计量核验、整单有理累计费用/佣金差额、同事务成交资格/原预算/追加镜像账，六既有控制HTTP/同原生宿主及Taro/React暂计账单源码。020/DATA079—081仅源码，当前108规划/137 schema/81平台表/68控制HTTP；42项新增/扩展测试定义未编译/运行，020未迁移。实际结果传输/本地签收程序、独立计量生产端/真实资格、同卖家重派/E2E换端/媒体/持久超时/争议、完整费用冲正/续租/到账对账、历史恢复、CK/GF继续待。全部56任务222子项开放，先全部生产源码/实际接线/测试源码，再集中验证和重录媒体；本次不是整轮完成。

## R026 DC-001—045 原结果传输与本地接收 / Original result transport and local receipt source

R026新增[DC-001—045](../../docs/技术开发/附录-原结果分块传输与本地接收源码.md)：PLAINTEXT/STANDARD原结果分块AEAD保存/完整核验、原提供方上传与原状态恢复、当前买家session绑定短时只读授权、原生加密接收和明确导出、React/Taro与Web显式本地核对签收、取消拒绝/过期退役及七HTTP源码。021/DATA082—084仅源码，当前115规划/150 schema/84平台表/75控制HTTP；40项新测试定义未编译/运行，021未迁移。平台能解密此档内容，不是E2E盲转；SDK/提供方守护编排、独立计量生产/真实资格、E2E/媒体/同卖家重派/持久超时、完整费用冲正/续租/到账对账、历史恢复与CK/GF继续待。全部56任务222子项开放，先全部生产源码/实际接线/测试源码，再集中验证和重录媒体；本次不是整轮完成。

## R026 SI-001—040 标准SDK原订单入口 / Original native SDK ingress source

R026新增[SI-001—040](../../docs/技术开发/附录-标准SDK原订单文本入口源码.md)：三协议非流式原生JSON入口、原请求幂等、同原订单/预算/加密输入/首次租约事务、原API key只读结果/完整校验、有界等待/并发和可选原生宿主实际join源码。31项新测试定义全部未编译/运行。115规划控制操作/150 schema/84平台表/75控制HTTP不变，另有3个数据面入口；无SQL或迁移。实时SSE/提供方常驻编排、SDK付款前授权/获准PayLauncher、E2E/媒体/独立计量/真实资格、持久签收超时/争议、完整结算对账及CK/GF仍待。全部56任务222子项保留，按CF-01—05先完整生产源码/实际依赖/测试源码，再集中验证和重录媒体；本次不是整轮完成。
