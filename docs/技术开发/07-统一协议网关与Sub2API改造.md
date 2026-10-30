# 统一协议转发与Sub2API改造细则

P11｜技术v1.0修订｜需求v3.0｜2026-10-29｜F69—F80；D实施规约，未fork、未构建或验证SDK。

低开销完整方案（最新用户确认）：用户允许新增隐私档可嵌入LLM内核，LM Studio继续保留兼容。采用llama.cpp C API原生worker、ComfyUI私有管道入口、LMA内部JNI；买卖端加密/平台盲转，任务数据加密而模型保留原生加载，不使用VM/RAM盘/常驻第二份模型。输入准备和密文交付不额外占GPU锁，控制与大数据通道分离。内存/性能为待实测验收预算，完整链路、二进制协议、媒体、恢复及PE-01—PE-08见[完整实施方案](附录-低开销隐私执行完整方案.md)。当前尚未实现或通过设备/密码/性能验证。

## 1. 四个边界

| 边界 | 职责 | 内容可见性 |
| --- | --- | --- |
| 模型数据入口 | OpenAI Chat/Responses、Anthropic Messages及已批准媒体子集 | 明文在平台；E2E只在买方/卖方端 |
| 自有控制面 | /v1/p11：seller、合同、付款、job、租赁、资源授权 | 必要业务元数据，不承载E2E正文 |
| A402支付桥 | 预下单、402挑战、Payment-Proof验付、同请求恢复 | 金额/订单/凭据，不把模型内容送支付宝 |
| 密文传输 | 已签上下文、密文帧/对象、反向密文响应 | 平台无内容钥，不解析方言/工具/usage |

对外模型JSON不新增私有必填字段。RouteContract先绑定真实seller、listing_revision、公开model、协议profile、mode/privacy、计费依据、预算/期限；模型key只索引该合同。缺路由、模型歧义、资格失效或模式不匹配，在执行/收费前拒绝，不自动购入或默认明文。

## 2. 标准入口与兼容档位

| 入口 | 首轮设计 | 暂未实现时的明确行为 |
| --- | --- | --- |
| GET /v1/models | 仅返回当前路由允许的真实公开别名 | 不返回其他seller或伪造原厂模型 |
| POST /v1/chat/completions | 文本JSON/SSE；n=1；已冻结长度/采样子集 | 视觉/tools/structured output等逐模型开，不静默丢字段 |
| POST /v1/responses | store=false无状态文本、输出项和语义SSE | previous_response_id、background、内置工具/状态存储未支持即拒绝 |
| POST /v1/messages | 固定anthropic-version，system/content、max_tokens、JSON/SSE | thinking/cache/beta/tools按真实后端另开，不能伪造签名或缓存命中 |
| POST /v1/images/generations、/v1/images/edits | 经证据批准的模型/参数及multipart子集 | 未验证的模型/size/edit字段拒绝；没有Anthropic生图API就不编造 |
| Videos标准create/get/content | 仅目标SDK和引擎均通过时发布 | 控制面job≠标准视频job；尚未验证组合关闭 |

每profile必须给出API/SDK版本、输入/输出方言、支持字段/枚举、原生或转换方式、模型/引擎证据、模式/OS、限制、usage来源和拒绝规则，见[字段映射](附录-协议字段映射与兼容档位.md)。原生直通优先；跨方言仅允许已证实语义等价的子集。兼容一个HTTP入口不代表Claude Code/Codex全部应用功能可用。

## 3. 请求处理算法

1. 识别路径/方法与Content-Type；头/真实编码字节限额；拒绝歧义长度、重复关键JSON键、非法Unicode及无效数值。模型JSON按发布profile处理，不能简单拒绝所有未来合法字段或默认忽略影响意图字段。
2. 解析Authorization或x-api-key到本站主体/route；验证版本、scope、owner、模式、公开model、实际seller及合同期限。输入中的upstream_url、任意代理头不能改变目标。
3. 纯预检字段、输入对象/配方、输出上限和价格可执行性；不支持能力先400，体积超限413。有限文件抓取也须授权/大小检查；E2E输入获取在买方端完成。
4. 建立调用意图/稳定幂等映射，解析资金路径。缺资金时只发A402挑战/控制面待付，不锁GPU、不向引擎提交。
5. E2E先建立INPUT_PENDING/PREPARED上下文并封装上传；它没有执行权、不占GPU。输入/封装、资金/许可/容量齐后才签发ISSUED执行租约。
6. 原子预算预留＋当前attempt/资源代次准入；持锁事务不能跨网络。BoundRelayCore仅接受验证过的合同和执行授权，选择该seller披露范围内的一个后端。
7. 提交引擎前持久化intent与可恢复backend引用；返回丢失进入QUERY_ORIGINAL，不盲目重算。首响应前核后端状态/方言，随后写标准JSON或SSE。
8. 完成后分别形成协议终态、原始usage候选、模式有效接受及账单；HTTP200/密文到达不是成交证据。finally传播停止意图，查询实际停算，清理确认后才释放资源。

## 4. BoundRelayCore实现端口

```text
IngressAdapter -> ProfileValidator -> ContractResolver -> PaymentGate
              -> AdmissionPort -> BoundRelayCore -> EngineTransport
              <- DialectWriter <- StreamTranslator <- EngineEvent
```

RelayInvocation包含route/contract_hash、seller/device/resource及围栏、authority_kind、profile/version、model_binding、budget/deadline、billing_mode和仅在授权明文域可访问的body_ref。JOB_LEASE带job/attempt/lease/token；RENTAL_SESSION带session/invocation/accessgrant/terms_revision，不能同时携两类执行权。

端口职责：IngressAdapter处理HTTP方言；FieldPolicy产生逐字段PASS/MAP/REJECT决定；ContractResolver只查已确认路由；BackendSelector只能选择当前seller且合格的后端；EngineTransport负责execute/query/cancel；StreamTranslator维护工具/输出项状态；DialectWriter输出原入口方言；UsageEvidencePort提交候选证据；Ledger/价格权威仍在平台交易服务。无接收任意base_url或跨卖家池的参数。

内部结构保留原方言AST与可用原始字节、角色优先级、内容块、工具ID/索引/参数片段、结束原因和usage口径；不要压成唯一字符串。直通只改已批准model/上游认证等字段；转换生成目标AST并记录所用mapping版本，不将内容写入平台审计。未知且可能改变意图的字段，不具备已批准直通条件时在执行前拒绝。

拟代码包：dev/src/gateway/core/{invocation,profiles,fieldpolicy,contracts,backends,stream,usage}、core/dialects/{openai-chat,openai-responses,anthropic-messages}、core/transports/{lmstudio,comfyui,authorized-upstream}。这是本项目计划目录，不宣称上游已有同名可抽取包。

## 5. HTTP头和凭据

| 类别 | 规则 |
| --- | --- |
| 本站模型认证 | OpenAI入口Bearer，Messages入口x-api-key；同请求冲突身份拒绝，不把买方本站key原样送引擎 |
| 方言头 | Content-Type/Accept与冻结anthropic-version；anthropic-beta仅白名单且有证据的值；不支持值显式拒绝 |
| 幂等 | Idempotency-Key为可选传输扩展；key绑定主体/route/操作/请求语义摘要与合同版本，不能取代渠道out_trade_no |
| 付费头 | amtPaymentLink为402响应头；Payment-Proof只交受权支付桥，解析验付后剥离，不送模型/卖家引擎 |
| 逐跳/代理头 | 不盲传Connection及其指名字段、Transfer-Encoding、Host、Cookie、代理认证或任意转发目标头；长度重算 |
| 上游认证 | 在实际执行域按seller绑定的secret_ref取凭据；不由用户JSON覆盖，不把上游key返回买方 |
| 响应 | 重写合法Content-Type、长度和安全请求ID；Set-Cookie、内部地址、provider调试包不直出；标准限流/重试头按已核profile筛选 |

RequestID只关联业务意图，不用用户可控trace头驱动权限。E2E场景支付凭据与外层授权是平台可见元数据，模型认证/目标上游key和正文不进入盲转层；卖方不得把本地解密内容再发回平台明文网关。

## 6. 三种方言的SSE

状态：VALIDATED→WAIT_FUNDS→ADMITTED→UPSTREAM_CONNECTING→HEADERS_SENT→STREAMING→TERMINAL_VALIDATED→CLOSED。失败分BEFORE_HEADERS、AFTER_HEADERS、EXECUTION_UNKNOWN，不能因连接关闭而补发成功终态。

parser按字节→增量UTF-8→SSE行/空行事件→方言JSON四层；TCP chunk不是JSON或token边界。处理CRLF/LF、跨块字符、多行data、注释/keepalive；[DONE]只在其方言合法位置识别。设置事件/头/参数限额：D单事件1MiB、累计单工具参数1MiB、头合计32KiB；与总请求/单流缓冲/预算共同取严限，SKU可下调。

| 方言 | 需要保存的状态 | 结束判断 |
| --- | --- | --- |
| Chat | choices[index]、delta角色/文本、tool_calls[index]的id/name/arguments片段 | 每choice的真实finish_reason；若请求stream_options.include_usage则按后端证据产生usage块；[DONE]不替代缺失的业务结果 |
| Responses | response.id、output_index/item_id/content_index；output item及function_call arguments各自增量 | completed、incomplete、failed/cancelled按实际状态，不把长度截断映射completed；不随意追加Chat的[DONE] |
| Messages | message_start、content_block_start/delta/stop按index关联，message_delta累计状态/usage | message_stop且必要块闭合；ping不是内容；error、redacted/thinking/signature块按已支持能力处理 |

工具参数碎片先按顺序拼接，到合法块结束后才解析JSON；不得把半段JSON触发为客户端工具执行。跨方言tool_use.id/function_call.call_id保持往返一一对应；工具执行仍由调用方承担，转发内核不执行shell/browser工具。thinking/signature_delta只能原生可信直通，不能由普通文本伪造。

输出使用目标SDK真实事件字段，不靠客户端猜测所有增量都是text。未知事件若官方允许忽略且不影响终态可按profile处理，否则安全中断并保留未完成状态；不得丢失工具/拒绝等关键语义继续报成功。参考[OpenAI流](https://developers.openai.com/api/docs/guides/streaming-responses)、[Anthropic流](https://platform.claude.com/docs/en/build-with-claude/streaming)。

## 7. 背压、超时和取消

首字节期限、有效事件空闲期、绝对总deadline按SKU/profile冻结；keepalive不能延长总期限。每调用有一个有界缓冲预算4MiB，不能每层各分配无限4MiB；并发总内存也有准入上限。写端阻塞暂停读取并向引擎传压，超时取消；不使用无界channel或将长流整体读进内存。

客户端断开→context取消→引擎stop/cancel→持久化状态→查原backend ID→停止/清理确认。ComfyUI全局interrupt只可在确认由该资源独占当前执行且不会杀他人时使用；未知停止状态隔离资源。基础库/进程取消不等于GPU已停。租赁请求取消仅终止该invocation，是否结束session由会话合同/显式stop决定。

已发HTTP头不能改成402/429/500；按方言允许的标准error/failed事件输出或异常关闭，不追加自造成功终块。E2E还需库FINAL＋认证业务END，EOF仅是连接结束。任何流已发内容后不得偷偷重试/拼接第二次答案。

## 8. 幂等与同卖家重试

稳定key同主体/route/操作＋同语义返回同原意图，异参数409；控制面重领结果不新收款。摘要仅在允许访问正文的域计算/保存，E2E平台不收prompt hash。SDK未提供稳定键时两段相同正文可能是两次真实意图，不能仅靠内容去重。文档示例要求付费调用关闭SDK自动重试或固定传输头；不宣称无key能保证网络重试绝不二次购买。

引擎尚未接收且无副作用才允许短重连；提交后未知先QUERY_ORIGINAL。一个订单最多一次已披露且合格的同seller重派，沿原预算/总deadline，新attempt及token，换端需新PREPARED和买方核验重封。跨seller关闭/退未用金额，用户确认新报价/关联新单；不得继承Sub2API默认全局轮换。

## 9. E2E与租赁的特殊路径

E2E由买方本地入口接收标准JSON/HTTP认证，在可信域执行字段校验、素材取回、A402桥接及加密。平台只处理外层上下文、凭证/预算和密文。卖方验证合同/租约/资源代次后解密，调用同语义内核；SSE事件或媒体在密文通道反传，买方认证后还原标准HTTP。平台不能为E2E直接做token计数、内容缓存或明文兼容转换。

基础档seller所有者能见解密后的内核/推理内容；增强档必须把解密、转换、内核、模型/CPU-GPU、前后处理/编码及输出保护都纳入真实证明。普通Go进程/Keystore不形成增强能力。[08密码](08-端到端加密协议与密钥实现设计.md)仍为密码层权威。

租赁中的每次标准调用带内部RENTAL_SESSION上下文，复用session锁/terms_revision，不创建独立按量销售，也不再次A402。到期/撤销拒绝新调用并按硬截止停止已有执行；续租必须是同价同环境的签名附录，不能更改旧合同。

## 10. 计量、错误与媒体

usage分protocol_usage与billable_evidence。协议数字也须真实；不因缺数据填0/猜值。无法满足目标SDK要求的usage字段时，该profile不能宣称完整兼容/按token收费；可在明确的新固定价SKU中提供已验证的输出子集。Chat总token及cache/reasoning包含关系、Messages输入/缓存各口径、Responses用量映射分别核对，不能简单相加重复收费。

400用于字段/方言不支持，401认证无效，403合同/scope不符，404无本人资源，409幂等冲突，413体积，429配额，502后端响应错误，503临时无合格容量，504期限；402只用于真实支付挑战。OpenAI error和Messages error分别按各自结构序列化，内部安全码不能伪装成厂商未定义type。错误返回可理解字段/能力原因，不回显prompt、secret或上游原包。

媒体输入URL由允许执行域受控读取，限制重定向/大小/超时/目标；封禁任意本机/内网探测，但不能错误封禁本人明确配置的受控本地引擎。Images到ComfyUI只走审核recipe，不接受用户任意graph/custom node；prompt_id及文件ID留在内部。同步接口不能回私有202 job冒充兼容，超时只报告失败/查询原执行。标准异步视频另按目标SDK流程验证。

E2E产物不放平台明文URL；买方完整认证缓存后提供base64或本人受权本地URI，响应总字节也需预检。ordered secretstream不支持任意跳块解密，Range只能在完整认证后的本地对象上提供。

## 11. Sub2API改造与完成证据

| 原组件 | 改造要求 | 必须证明 |
| --- | --- | --- |
| 方言解析/stream/合法连接器 | 包装在上述端口，冻结原生直通/转换profile | 固定commit差异、字段/事件与SDK回归 |
| 账号池/调度 | 受ContractResolver和seller边界约束 | 失效/限流/重试均不会跨seller/地区/隐私 |
| 充值/积分/钱包 | 不接入资金权威 | 收款/退款/交易账只用本站C2C链 |
| 内容预览/缓存/遥测 | 关闭正文采集；E2E不回平台 | 仅做泄漏负向验证，不建设观测系统 |
| Redis/Gin/Ent耦合 | POC-08先做依赖切片与原生构建 | 不能称原版已无Redis；Android使用轻量等价适配 |
| 发布/容器脚本 | 不执行；部署本轮不设计 | 无本地Docker/WSL/VM/testcontainers启动 |

LICENSE/NOTICE、锁定源码/补丁和依赖图先评审；能抽取则复用，不能脱离中心依赖时评估边界明确的等价实现，不用选型表宣称已经改好。验收按[映射附录](附录-协议字段映射与兼容档位.md)的正负场景及固定SDK/OS/模式证据执行；当前全部NOT_EXECUTED。

## 12. 供给内核与新增媒体档位

转发核心与推理内核独立：Windows LLM为lms管理的llmster/runtime本地API；图/视频/音频/3D为完整ComfyUI环境＋有限节点，Android为LMA二开轻量桥。新增AUDIO_SPEECH、AUDIO_TRANSCRIPTIONS、RESPONSES_ASSET_RECIPE三种设计档位（当前共11种）；前两者遵循标准音频语义，音乐/3D仅用明确声明的Responses结构化资产结果，不伪造端点。background/retrieve/cancel和产物领取必须冻结SDK验证，尚未开放。FW19—FW22补充音频字段/二进制与multipart、音乐/3D结构化/异步、资产外部引用及环境漂移拒绝；与既有FW01—FW18合计22项，均未执行。

## 14. 加密链路对转发内核的具体约束

BuyerGateway与SellerGateway使用相同协议语义和受限转换规则，平台CipherRelay不导入正文解析/转换/内容缓存模块。卖方服务在核ISSUED、环境与双围栏后解密；BoundRelayCore只接已认证有界请求，不接受买方任意backend/base_url或透传管理操作。输入本地API key/平台验付proof不流入worker。

JSON_COMPAT、BINARY_STREAM_1和LOCAL_SEGMENTED_1分层，由[技术08](08-端到端加密协议与密钥实现设计.md)定义；新增二进制channel协商/Schema未完成不能在现有帧通道偷换。请求HEADERS、响应status/允许头、DATA、USAGE、ERROR、END/CANCEL的内容都在认证密文内；跨DATA的SSE/UTF-8/工具增量保持原语义，不逐token签名。END使用库TAG_FINAL并核标准终态，起流后认证失败不能伪造成功事件。

重连只补传持久原密文；接收可从头认证但不得重复交已输出前缀。SDK HTTP断开如实报错，不再次购买/推理。计算资源清理释放后原结果按原attempt/token和交付scope继续，不能因新resource_generation丢失合法交付，也不能用交付scope提交新模型任务。

## R012源码边界更新

已实现dev/src/gateway/core独立Sub2API类转发库，无复制Sub2API源码/新Go依赖；固定同卖家合同及精确后端，三个原生文本JSON/SSE子集，生产Authorizer/SecretSource必须显式实现。详见[BRC-001—024](附录-固定路由协议转发核心实施细则.md)。尚未挂载模型路由，支付/预算/Lease原子准入、原backend持久恢复、跨方言、工具、媒体及E2E接线仍待；原11市场profile及FW01—FW22不因此转为发布/完整验收通过。


## R015 已确认路由实施边界

已实现三项本人会话路由控制与事务内scoped-key精确解析，控制操作增至34/93；007前向追加5列，原001—006及端侧DDL不改，旧路由隔离。仅PLAINTEXT/STANDARD、三种native文本profile；冻结原订单、同单已登记付款、卖家/版本/模型/价格/费用和上限，不产生执行权或付款。标准请求不要求私有路由JSON，歧义不换卖家。完整字段、错误、幂等/当前状态、启动迁移检查与待接入调用签发器见 [RC-001—022](附录-已确认订单路由与标准SDK绑定实施细则.md)。

## R016原调用准备实施边界

R016新增原调用准备库：当前scoped key/原订单/同单资金事实重新核对后，将INPUT_PENDING、整笔单次预算、次数占用及安全事件在同一事务提交。原键恢复不重复占用；改参数/key冲突；取消/过期/未知保留原记录和占用。请求摘要只以版本化HMAC持久保存。前向008追加六列，001—007和端侧DDL原字节不改；仍34/93操作、65表。仅三个原生文本档位的PLAINTEXT/STANDARD；没有挂载模型入口、签发Lease/Grant或运行模型。输入就绪/资源原子准入、可信usage、真实支付、E2E和路由UI仍待。详见[RI-001—024](附录-原调用准备与次数预算原子接线.md)。RouteInvocationPrepared是元数据事实，不是执行投递；后续TX04仍需输入、资源owner和签名租约。
