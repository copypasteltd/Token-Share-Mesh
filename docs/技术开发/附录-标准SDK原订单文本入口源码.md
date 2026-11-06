# 标准 SDK 原订单文本入口源码

本附录为 R026 全量代码阶段的 SI-001—040 源码增量。所有代码和测试定义均为 **SOURCE_ONLY / 未编译 / 未运行**。同一轮的完整范围继续为 56 个任务、222 个子项；本次保存不是一轮开发完成。开发顺序统一遵守 [开发轮次执行规则](../../dev/开发轮次执行规则.md)。本附录不改既有 C2C 交易、隐私和计费合同。

已写源码把三个非流式原生文本入口接入既有原订单、预算、加密输入、首次租约和结果读取服务。API 宿主尚未实际启动；提供方完整常驻接单和自动上传编排仍待补齐，因此不能据此声称真实 SDK 到设备全链已交付。

| 编号 | 开发项 | 源码约束 |
|---|---|---|
| SI-001 | OpenAI Chat | 仅 `POST /v1/chat/completions`，原生 JSON 文本子集 |
| SI-002 | OpenAI Responses | 仅 `POST /v1/responses`，原生文本子集、`store:false`；不冒充后台任务/工具/完整资产能力 |
| SI-003 | Anthropic Messages | 仅 `POST /v1/messages`，原生文本子集和独立版本头 |
| SI-004 | 公共模型解析 | 同一严格解析器拒重复字段、不合法 Unicode、未知字段和超界；保留原始请求字节与摘要 |
| SI-005 | 请求上限 | JSON 原文最多 256 KiB，不接压缩请求，不支持私有必填 JSON 字段 |
| SI-006 | 稳定重试键 | 必须提供 8—128 字节可见 ASCII 的单一 `Idempotency-Key`，缺失时拒绝；不生成替代购入键 |
| SI-007 | OpenAI 身份 | 单一 Bearer SDK key，经当前 API key 服务认证；不接 `x-api-key` 混用 |
| SI-008 | Messages 身份 | 单一 `x-api-key` 或单一 Bearer，二者同时出现、空值或重复头拒绝 |
| SI-009 | 版本与 beta | `anthropic-version: 2026-10-21`；尚未映射的 beta 头拒绝，不静默转成旧协议 |
| SI-010 | 入口隔离 | 不接 Cookie、Payment-Proof、Origin、Referer；浏览器走已有控制面，推理凭据不能变成账户会话 |
| SI-011 | 请求路径 | 精确 Host 白名单；拒查询参数、绝对目标、转义路径和其他方法；不做重定向 |
| SI-012 | 同库配置 | 输入交付、指纹、原声明、结果和 SDK key 必须属于同一持久库与同一原 verifier |
| SI-013 | 独立签发配置 | 独立可选配置文件，64 字节 Ed25519 私钥必须匹配事先配置的公钥 pin；不从网络回包自动信任 |
| SI-014 | 四类钥用途 | 签名、输入 AEAD、指纹 HMAC、结果 AEAD 分开；配置复用拒绝；只清除各服务自己持有的钥副本 |
| SI-015 | 当前路由 | 只用当前 key 已绑定、买家已明确确认且实际资金准入的路由；歧义不选默认卖家 |
| SI-016 | 原交易 | 固定原订单、实际个人 seller、模式、模型、价费、预算；既有准入服务逐次复核 |
| SI-017 | 原子准入 | 一次 SQL 事务完成原调用、预算预留、加密输入、第一次 attempt/租约和 outbox；事务内无网络或内核调用 |
| SI-018 | 提交未知 | 原事务提交未知返回安全 503；重试必须沿原 key/原正文，不释放预算或新建替代付款 |
| SI-019 | 原任务恢复 | 原 key/正文复用原 job；已运行/已交付不再发新 attempt，不重跑已完成生成 |
| SI-020 | 流式门槛 | 本入口在准入前拒 `stream:true`；不把已完成结果重新分帧冒称实时 token 流 |
| SI-021 | 等待上限 | 最多等待 120 秒，500 ms 轮询；数据库事务不跨等待，不常驻持锁 |
| SI-022 | 当前读取身份 | 每次状态、分块、发送前重查当前用户/BUYER 角色、API key 撤销/期限和当前路由成员关系 |
| SI-023 | 原调用所有权 | 读取必须同时匹配原 buyer/key/job/route/order/seller；同账户另一 key 也不能代取 |
| SI-024 | 只读权隔离 | 不伪造 `USER_SESSION`，不调用会话专用读授权，不把读取变成计算权 |
| SI-025 | 原清单核对 | 完整签名头和执行 manifest 固定原尝试/设备/资源代次/合同/模型/协议/请求上限；不跟随未来环境更新 |
| SI-026 | 完整性验证 | 逐块 AEAD、scope、head、位置、长度和 SHA-256；最后核总长度、UTF-8、总摘要及认证 END |
| SI-027 | 公共结果预算 | 原文最多 1 MiB，最终 JSON 最多 8 MiB；四个并发入口，均为 D 预算、未做性能实测 |
| SI-028 | RAM 输出 | 逐块读取到有界非流式响应，完成验证前不发送成功正文；不写新的明文结果文件 |
| SI-029 | 协议结果 | 使用既有 textoutput 的 Chat/Responses/Messages JSON；原 reported token 是协议候选，不能当可信计量 |
| SI-030 | 停止原因 | 保留 `eog`/`limit`/`stop`，认证的匹配 stop 字符串仅在内容域使用，不进日志/journal/outbox |
| SI-031 | 发送前复核 | 完整编码后、首字节前再核原内容可读性和当前 key；中途撤销/取消则关闭本次成功输出 |
| SI-032 | 超时与断线 | 保持原任务和未知资金/资源状态，客户端沿同一请求恢复；不因 HTTP 断线推断停算/清理/零用量 |
| SI-033 | 错误包 | 按 OpenAI 或 Messages 错误结构返回安全消息；不回显上游/数据库原错、prompt、URL、秘密或内容 |
| SI-034 | 请求和任务标识 | 返回安全 request id、原 job id、重放标记；结果响应显式要求本人后续接受，不自动签收 |
| SI-035 | 有界入口 | 固定四个并发槽、IP 120 次/分钟、key 60 次/分钟、最多 4096 个限流桶；不信任转发头扩容 |
| SI-036 | 同宿主接线 | 在既有原生 p11-api 同地址按路径接线；控制面原 20 秒写期限保留，只有此入口有有界长等待 |
| SI-037 | 关闭所有权 | 拒绝新请求、取消本宿主读/等待、实际 join 全部调用，然后才销毁 SDK 私钥及外层服务 |
| SI-038 | 测试源码 | 编写解析、配置、协议输出、恶意头/正文、并发/退出、原资金链/唯一准入/撤权/取消测试定义，全部未执行 |
| SI-039 | 编号与文档 | 保留 56/222 和历史证据；三个数据面入口单列，不把控制面 115 规划操作、75 路由、84 表改写成已全交付 |
| SI-040 | 完成门槛 | 继续补完整生产代码/实际依赖/测试源码；只在整轮清单齐备后统一编译、测试、调试、回归与重录媒体 |

默认 SDK 付款前授权/A402 自动恢复、获准 PayLauncher、实际提供方守护进程、实时 SSE/心跳/取消传播、完整兼容 SDK 版本锁、Tools/音视频/3D 公共数据面、买方本地 E2E 程序及盲转、独立可信计量生产、同卖家重派/换端、持久签收超时/争议、租赁最终小时账单、完整费用冲正/渠道到账/佣金和对账继续待完成。不得把本次非流式明文源码外推为这些能力已实现或合格。

当前对应 DEV-01/03/11/12/17/19/20/26/27/37/38/51/52/53/54/56 的部分源码；各任务原未收口子项保持原编号和状态。001—021 平台双份 SQL 及既有端侧 DDL/source 元数据原字节保留，本次没有新增 SQL、没有迁移。

最小使用顺序是：本人登录→个人卖家/真实设备资格与挂牌→明确原订单和获准付款→确认资金分配→确认原 seller/模型/预算路由→签发只绑定该路由的 SDK key→提供方本人独立批准并运行实际执行宿主→SDK 原生请求→原提供方生成及封存上传→SDK 完整核验返回→买家通过现有控制端明确接受/拒绝→独立可信计量/成交资格和最终账务。现阶段这些环节仍有明确待办，尚无端到端可用性结论。

`P11_NATIVE_TEXT_SDK_FILE` 引用项目 `runtime/` 中独立受限 JSON：`profile_id` 为 `P11_NATIVE_TEXT_SDK_1`，`input_version` 引用已配置输入钥，`signing_key_id` 引用已配置公钥 pin，`signing_private_key` 为 canonical base64url 的 64 字节 Ed25519 私钥。还需已有 `P11_LEASE_VERIFIERS_FILE`、`P11_INPUT_DELIVERY_KEYS_FILE`、原结果钥环和 API key 配置；缺项失败关闭。不要将配置、真实 key 或数据放到仓库。

后续统一测试阶段的原生请求示意：OpenAI base URL 为 `http://127.0.0.1:<已有API端口>/v1`，Chat/Responses 沿对应 SDK 方法调用并携带稳定 `Idempotency-Key`；Messages 沿 `/v1/messages`、匹配版本和单一 SDK 凭据调用。三者使用买家确认的公共模型名、有限最大输出和 `stream:false`。示意不表示已经启动服务或通过 SDK 联调，也不涉及部署规划。

Anthropic 版本头依据其[官方版本说明](https://platform.claude.com/docs/en/api/versioning?version=2026-10-21)，本项目已有冻结文本档沿用 `2026-10-21`；其他历史版本需独立映射和 SDK 合同测试后才开放。此次读取的是文档检索，未启动本机浏览器。

## SI 测试源码定义索引

| 编号 | 定义 | 源码 | 状态 |
|---|---|---|---|
| SI-T001 | TestInspectUsesExactNativeBodyWithoutPrivateFields | dev/src/gateway/core/preflight_test.go | 未编译/未运行 |
| SI-T002 | TestInspectRejectsAmbiguousModelsAndNonNativeFields | dev/src/gateway/core/preflight_test.go | 未编译/未运行 |
| SI-T003 | TestSDKRequiresIndependentlyPinnedSigner | dev/src/internal/invocation/sdk_test.go | 未编译/未运行 |
| SI-T004 | TestSDKRequiresSameOriginalCompletionAuthority | dev/src/internal/invocation/sdk_test.go | 未编译/未运行 |
| SI-T005 | TestSDKRequiresFourSeparateKeyPurposes | dev/src/internal/invocation/sdk_test.go | 未编译/未运行 |
| SI-T006 | TestSDKCloseDoesNotEraseSharedDeliveryOrContent | dev/src/internal/invocation/sdk_test.go | 未编译/未运行 |
| SI-T007 | TestNativeSDKThreeProfilesRenderOriginalWithoutAccepting | dev/src/gateway/platform/sdkhttp/server_test.go | 未编译/未运行 |
| SI-T008 | TestNativeSDKRejectsMalformedHeadersBeforeAdmission | dev/src/gateway/platform/sdkhttp/server_test.go | 未编译/未运行 |
| SI-T009 | TestNativeSDKStreamToolsAndOversizeNeverReserve | dev/src/gateway/platform/sdkhttp/server_test.go | 未编译/未运行 |
| SI-T010 | TestNativeSDKBadChunkOrRevocationCannotSendSuccessfulBody | dev/src/gateway/platform/sdkhttp/server_test.go | 未编译/未运行 |
| SI-T011 | TestNativeSDKAggregateMismatchCannotEmitSuccess | dev/src/gateway/platform/sdkhttp/server_test.go | 未编译/未运行 |
| SI-T012 | TestNativeSDKAnthropicVersionAndMixedBlankHeaderFailBeforeAdmission | dev/src/gateway/platform/sdkhttp/server_test.go | 未编译/未运行 |
| SI-T013 | TestNativeSDKCloseCancelsAndJoinsOwnedAdmission | dev/src/gateway/platform/sdkhttp/server_test.go | 未编译/未运行 |
| SI-T014 | TestNativeSDKFourCallsBoundConcurrency | dev/src/gateway/platform/sdkhttp/server_test.go | 未编译/未运行 |
| SI-T015 | TestNativeSDKPendingTimeoutDoesNotReissueOrReturnEmptySuccess | dev/src/gateway/platform/sdkhttp/server_test.go | 未编译/未运行 |
| SI-T016 | TestNativeSDKAuthenticationFailureHasNoAdmission | dev/src/gateway/platform/sdkhttp/server_test.go | 未编译/未运行 |
| SI-T017 | TestNativeSDKResultLimitFailsBeforeAnyPublicContent | dev/src/gateway/platform/sdkhttp/server_test.go | 未编译/未运行 |
| SI-T018 | TestNativeSDKRateBucketsRemainBoundedWithoutSecretKeys | dev/src/gateway/platform/sdkhttp/server_test.go | 未编译/未运行 |
| SI-T019 | TestSDKHostIsExplicitlyOptIn | dev/src/api/cmd/p11-api/sdk_test.go | 未编译/未运行 |
| SI-T020 | TestSDKHostRequiresPinnedCompleteBoundedConfig | dev/src/api/cmd/p11-api/sdk_test.go | 未编译/未运行 |
| SI-T021 | TestSDKHostRejectsDifferentIdentityStore | dev/src/api/cmd/p11-api/sdk_test.go | 未编译/未运行 |
| SI-T022 | TestSDKAdmissionAtomicallyWiresThreeOriginalNativeProfiles | dev/src/tests/integration/sdk_native_text_test.go | 未编译/未运行 |
| SI-T023 | TestSDKConcurrentSameRequestHasOneAdmissionAndOneLease | dev/src/tests/integration/sdk_native_text_test.go | 未编译/未运行 |
| SI-T024 | TestSDKChangedBodyCannotReplaceOriginalCall | dev/src/tests/integration/sdk_native_text_test.go | 未编译/未运行 |
| SI-T025 | TestSDKResultUsesOriginalKeyWithoutSessionReadGrant | dev/src/tests/integration/sdk_native_text_test.go | 未编译/未运行 |
| SI-T026 | TestSDKPendingUploadDoesNotBecomeResultReady | dev/src/tests/integration/sdk_native_text_test.go | 未编译/未运行 |
| SI-T027 | TestSDKOtherKeyEvenOnSameRouteCannotReadOriginalCall | dev/src/tests/integration/sdk_native_text_test.go | 未编译/未运行 |
| SI-T028 | TestSDKSessionCannotImpersonateOriginalAPIKey | dev/src/tests/integration/sdk_native_text_test.go | 未编译/未运行 |
| SI-T029 | TestSDKRevokedKeyFailsEveryChunkAndFinalRecheck | dev/src/tests/integration/sdk_native_text_test.go | 未编译/未运行 |
| SI-T030 | TestSDKWrongHeadAndChunkIndexCannotSelectAnotherContent | dev/src/tests/integration/sdk_native_text_test.go | 未编译/未运行 |
| SI-T031 | TestSDKCancellationRetiresReadWithoutReleasingCompute | dev/src/tests/integration/sdk_native_text_test.go | 未编译/未运行 |
