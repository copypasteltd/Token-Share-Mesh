"""汇集本轮实际 Go/原生数据库/构建证据；不修改产品验收状态。"""
from __future__ import annotations

import hashlib
import argparse
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
EVIDENCE = ROOT / 'dev/tests/evidence'
CASES = [
    ('R001-TEST-001', 'shared/money', 'TestCumulativeBillingDoesNotRoundEveryChunk', '累计有理计价；分块不反复向上舍入'),
    ('R001-TEST-002', 'shared/money', 'TestAmountsRejectOverflowNegativeAndReversal', '金额上限、溢出、负数和回退读数拒绝'),
    ('R001-TEST-003', 'tests/integration', 'TestJournalCommitBalancesAndPreventsLaterMutation', '空/不平账本拒绝；平账可提交；追加修改及普通角色绕过拒绝'),
    ('R001-TEST-004', 'tests/integration', 'TestCompositeForeignKeyRejectsWrongSellerResource', '资源复合外键拒绝其他卖家的设备'),
    ('R001-TEST-005', 'tests/integration', 'TestConsumedQuoteAndOrderTermsCannotBeChanged', '订单卖家不可暗改；已消费报价不可复原'),
    ('R001-TEST-006', 'tests/integration', 'TestMigrationsExecuteAllDomainDDLAndRejectDrift', '60张领域表真实迁移、重复执行与摘要漂移拒绝'),
    ('R001-TEST-007', 'tests/integration', 'TestKeysSecretOnceScopesOwnershipAndRevocation', '密钥只显一次、归属隔离、有限权限、HMAC存储与撤销'),
    ('R001-TEST-008', 'tests/integration', 'TestConcurrentIdempotencyCommitsOneBusinessMutation', '20个并发同意图只提交一次；用户版本变化拒绝旧回放'),
    ('R001-TEST-009', 'tests/integration', 'TestInboxReceiptRollsBackWithSideEffectAndDeduplicates', 'inbox与副作用同事务回滚；重复去重；异载荷拒绝'),
    ('R001-TEST-010', 'tests/integration', 'TestDurableJobsFenceStaleClaimsAndBoundRetries', '认领代次递增、旧ACK拒绝、有限重试和错误码/任务白名单'),
]

R003_CASES = [(f'R003-TEST-{i:03d}', p, n, d) for i, (_, p, n, d) in enumerate(CASES, 1)] + [
    ('R003-TEST-011', 'internal/pricing', 'TestHourlyAndTokenContractsUseExactAggregateRounding', '小时与输入/输出M token精确累计、合并一次进位'),
    ('R003-TEST-012', 'internal/pricing', 'TestUnknownUsageInvalidRatesAndOverflowAreRejected', '未知用量不当零；价率/费用缺失、负数与溢出拒绝'),
    ('R003-TEST-013', 'internal/pricing', 'TestFeeRefundTargetsAndLegacyConversion', '佣金退款目标重算；350分每小时拒绝有损分钟转换'),
    ('R003-TEST-014', 'internal/pricing', 'TestPriceDecoderRejectsUnknownComponentsAndTrailingData', '未支持收费组件、重复键、缺少费率及多余JSON拒绝'),
    ('R003-TEST-015', 'shared/strictjson', 'TestControlJSONRejectsAmbiguityAndBounds', '控制JSON大小、深度、UTF8、未知字段及嵌套重复键边界'),
    ('R003-TEST-016', 'tests/integration', 'TestMarketRequiresScopedApprovalsAndConcreteC2CModels', '具体卖家/型号、安全字段投影、分页及组合资格/撤销/资源忙'),
    ('R003-TEST-017', 'tests/integration', 'TestHourlyAndDualRateQuotesFreezeSellerBudgetAndPolicy', '报价冻结双价/预算/卖家；无默降、API购单和自购；证据TTL'),
    ('R003-TEST-018', 'tests/integration', 'TestOrderConfirmationConcurrentReplayAndOwnerIsolation', '16并发同键一个订单/outbox/同意；归属隔离和角色撤销回放'),
    ('R003-TEST-019', 'tests/integration', 'TestQuoteSingleConsumptionAcrossDifferentOrderKeysAndRollback', '数据库费率漂移拒绝并回滚报价消费；12异键同报价仅一单'),
    ('R003-TEST-020', 'tests/integration', 'TestNewListingRevisionPreservesHistoricalQuoteAndRejectsStalePurchase', '新挂牌价版不覆旧合同；旧确认拒绝；350分小时精确报价'),
    ('R003-TEST-021', 'tests/integration', 'TestOrderRechecksDeviceEvidenceAndBuyerConsentBeforeMutation', '停新单/撤证/错误意图拒绝，不消费原报价'),
    ('R003-TEST-022', 'tests/integration', 'TestHistoricalOrderProjectionRetainsLegacyPriceFields', '历史订单原始价格及扩展完整保留；不静默改写旧报价'),
]


R004_CASES = [(f'R004-TEST-{i:03d}', p, n, d.replace('60张领域表','62张领域表')) for i, (_, p, n, d) in enumerate(R003_CASES, 1)] + [
    ('R004-TEST-023','shared/strictjson','TestControlJSONRequiresExactDeclaredKeys','精确字段大小写、别名、嵌套键及非对象DTO拒绝'),
    ('R004-TEST-024','internal/identity','TestAlipayVerifiesOriginalBytesAndSignsApplicationRequest','RSA2应用请求与原始响应字节验签；身份字段显式选择'),
    ('R004-TEST-025','internal/identity','TestAlipayRejectsUnsignedTamperedOrAmbiguousIdentityResponses','未签名/篡改/重复/错身份/超界响应拒绝，禁止身份字段回退'),
    ('R004-TEST-026','internal/identity','TestAlipayRejectsIncompleteConfigurationAndNeverFollowsRedirects','缺配置关闭身份；授权兑换不跟随HTTP重定向'),
    ('R004-TEST-027','tests/integration','TestAuthorizationChallengeIsSingleUseAndStoresNoOriginalCredentials','16并发同挑战仅兑换/签发一次，原始码/凭据不落库'),
    ('R004-TEST-028','tests/integration','TestBrowserAuthorizationBindingAndUnknownExchangeFailClosed','浏览器挑战/state绑定及授权UNKNOWN不发会话、不盲重试'),
    ('R004-TEST-029','tests/integration','TestSessionVersionsExpiryAndRevocationApplyBeforeTradeReplay','用户版本/会话到期/撤销在业务回放前再次生效'),
    ('R004-TEST-030','tests/integration','TestRoleRevocationBlocksKeyReplayAndLoginDoesNotRestoreRevokedRoles','撤销角色阻断密钥回放；重登不恢复撤权/封禁账户'),
    ('R004-TEST-031','api/tests','TestHTTPMiniappLoginQuoteOrderAndTenantIsolation','原生HTTP登录/市场/报价/订单同键回放和跨买家隔离'),
    ('R004-TEST-032','api/tests','TestHTTPBrowserCookiesCSRFAndSessionRevocation','Web HttpOnly Strict Cookie、CSRF、跨域、双端凭据混用及注销'),
    ('R004-TEST-033','api/tests','TestHTTPRejectsAmbiguousOversizeAndUnsupportedRequests','真实HTTP拒绝Host/方法/查询/JSON歧义、超限及未实现路由'),
    ('R004-TEST-034','api/tests','TestHTTPKeySecretOnceSignedPaginationAndRateLimits','密钥只显一次、签名游标用户隔离、撤销、预检和授权限流'),
]


R005_CASES = [(f'R005-TEST-{i:03d}', p, n, d.replace('62张领域表','64张领域表')) for i, (_, p, n, d) in enumerate(R004_CASES, 1)] + [
 ('R005-TEST-035','tests/integration','TestSellerApplicationAndConsentAreIdempotentUntrustedSubmissions','16并发申请仅留一份材料/事件、无自动SELLER；同意/撤回追加与数据库不可变'),
 ('R005-TEST-036','tests/integration','TestListingSubmissionNeverAutoPublishesAndRechecksEvidence','草稿/提交不自动开售；撤证阻断且事务回滚；版本化原因留痕'),
 ('R005-TEST-037','tests/integration','TestListingFutureRevisionsPreserveSoldContractsAndRejectStaleETags','新价创建未来修订；已售数据库合同不变；过期ETag拒绝及旧版不可删改'),
 ('R005-TEST-038','tests/integration','TestConcurrentListingEditsCommitOneRevisionAndRevocationBlocksReplay','12异键竞争同版本只提交一个修订；角色撤销阻断旧回放'),
 ('R005-TEST-039','tests/integration','TestListingOwnershipFeeRegionAndEnvironmentCannotBeClientForged','跨卖家设备/证据、佣金篡改、未批准地区、通用型号及环境漂移拒绝'),
 ('R005-TEST-040','tests/integration','TestListingWindowsRejectGapSalesAndBoundRemainingExecution','真实跨排期边界/空档拒售；剩余可用时间限制deadline和GPU时长'),
 ('R005-TEST-041','tests/integration','TestSellerExitStopsNewAdmissionWithoutErasingOrdersOrResourceFences','独立SELLER可退出；停新售但保留订单合同、隔离状态和资源代次'),
 ('R005-TEST-042','api/tests','TestHTTPApplicantCanReadPendingProfileWithoutObtainingSellerAuthority','小程序本人申请/读待审/同意/退出HTTP；申请不授予挂牌权限'),
 ('R005-TEST-043','api/tests','TestHTTPSellerListingETagsTenantPagingAndBrowserCSRF','实际Web Cookie/CSRF挂牌链、强ETag回放/竞争、空补丁值拒绝、租户分页及PATCH预检'),
]


R006_CASES = [(f'R006-TEST-{i:03d}', p, n, d) for i, (_, p, n, d) in enumerate(R005_CASES, 1)] + [
 ('R006-TEST-044','api/tests','TestNativeNonceAuthorizationIsExplicitSingleUseAndNotWebPKCE','显式MINIAPP服务端随机证明：仅存摘要、错证明/重放拒绝；不可冒充Web PKCE'),
 ('R006-TEST-045','api/tests','TestFrontendBrowserJourneysUseNativeControlAndOneOriginalOrder','实际Web/Taro H5浏览器经HTTP/原生数据库：报价、显式同意、丢建单回执后原键恢复各一单/事件；浏览器关闭'),
]

R007_CASES = [(f'R007-TEST-{i:03d}', p, n, d.replace('64张领域表','65张领域表')) for i, (_, p, n, d) in enumerate(R006_CASES, 1)] + [
    ('R007-TEST-046', 'client/protocol/devicecontrol', 'TestDeviceProofGoldenMessageAndRFC8032KeyVector', 'RFC8032独立金向量与项目控制签名字节格式'),
    ('R007-TEST-047', 'client/protocol/devicecontrol', 'TestDeviceProofBindsEveryRoutingAndCommandField', '正文/方法/Host/路径/查询/幂等键/ETag/媒体类型/身份/时间/nonce逐字段篡改拒绝'),
    ('R007-TEST-048', 'client/protocol/devicecontrol', 'TestDeviceProofRejectsExpiredAmbiguousAndUnboundedInputs', '过期、非规范编码、公钥、控制字符、URI与正文上限拒绝'),
    ('R007-TEST-049', 'client/protocol/devicecontrol', 'TestDeviceNativeClientRecoversFrozenCommandWithFreshProof', '实际HTTP丢写回执标UNKNOWN，不自动重试；原字段/键配新证明显式恢复'),
    ('R007-TEST-050', 'client/protocol/devicecontrol', 'TestDeviceNativeClientRejectsRedirectsAndUnsafeBases', '不跟随重定向泄露证明、拒绝非安全base与关闭后调用'),
    ('R007-TEST-051', 'tests/integration', 'TestPairingSecretOnceAndConcurrentEnrollmentOriginalRecovery', '16并发配对/登记各仅一份、秘密只显一次、原意图恢复、公钥唯一/不可变'),
    ('R007-TEST-052', 'tests/integration', 'TestPairingLimitsExpiryFailedAttemptsAndCurrentSellerAuthority', '待配对上限、过期/错误5次提交关闭、具体型号和当前SELLER/审批校验'),
    ('R007-TEST-053', 'tests/integration', 'TestDeviceProofCurrentIdentityDurableNonceAndTamperRejection', '持久nonce跨服务实例防重放、逐字段篡改、过期/跨设备/停权拒绝'),
    ('R007-TEST-054', 'tests/integration', 'TestDeviceHeartbeatMonotonicConcurrencyAndNoAutomaticQualification', '16并发心跳同键一次、uint64完整范围/溢出、时钟/版本/假尝试拒绝，不自授资格'),
    ('R007-TEST-055', 'tests/integration', 'TestDeviceResourcePolicyVersionsSchedulesAndPhoneConditions', '12异键同ETag仅一次，时段/单槽/手机条件/他人设备隔离；更新不自动ONLINE'),
    ('R007-TEST-056', 'tests/integration', 'TestDeviceDrainRevokePreserveFencesAndInvalidateCredential', '停止保留隔离与在途attempt owner/77代次；撤销证明及旧业务回放立即失效、终态不可恢复'),
    ('R007-TEST-057', 'tests/integration', 'TestDeviceReportedHealthStopsOfferAdmissionWithoutGrantingQualification', '设备热/低电/低盘/忙/排空和45秒超时关闭市场新准入'),
    ('R007-TEST-058', 'api/tests', 'TestHTTPDevicePairingCSRFEnrollmentAndNativeControl', '实际Web CSRF配对、原生配对消费/恢复、签名客户端读/心跳，不混用浏览器凭据'),
    ('R007-TEST-059', 'api/tests', 'TestHTTPDeviceProofTamperingRevocationAndTenantPaging', 'HTTP nonce重放/URI篡改/跨设备跨卖家拒绝，签名游标隔离、停止与撤销'),
    ('R007-TEST-060', 'api/tests', 'TestHTTPDevicePolicyETagAndStrictInputBoundaries', 'HTTP强ETag/原键恢复、签名绑定幂等键、重复头/Origin/重复JSON/超界拒绝'),
]

R008_CASES = [(f'R008-TEST-{i:03d}', p, n, d) for i, (_, p, n, d) in enumerate(R007_CASES, 1)] + [
    ('R008-TEST-061', 'client/node', 'TestNodeEncryptedStateLockAndTamper', '真实本地加密状态重开、独占锁、篡改/缺失拒绝与不重建身份'),
    ('R008-TEST-062', 'client/node', 'TestNodeEnrollmentUnknownSurvivesRestart', '登记丢回执跨重启保持公钥/原正文/原命令，成功后移除配对凭据'),
    ('R008-TEST-063', 'client/node', 'TestNodeHeartbeatUnknownSurvivesRestart', '心跳丢回执跨重启保持原序号/键/正文，恢复签新nonce且不自动报空闲'),
    ('R008-TEST-064', 'client/node', 'TestNodeRejectedVersionRefreshAndCounterHighWater', '已知版本冲突经当前读取恢复，保留高水位后以新序号继续'),
    ('R008-TEST-065', 'client/node', 'TestNodeInvalidAcknowledgementAndAuthorityPreservePending', '错设备回执不清命令，撤销即停出站循环并保留原状态'),
    ('R008-TEST-066', 'client/node', 'TestNodePersistenceFailureAndExhaustionStayClosed', '持久化失败前不发送、不换身份；uint64耗尽关闭不回绕'),
    ('R008-TEST-067', 'client/node', 'TestNodeRunCancellationStopsOutboundControl', '实际出站HTTP取消及时关闭，不隐式后台执行'),
    ('R008-TEST-068', 'client/node', 'TestSecretServiceLookupCreationAndClosedFailure', 'Secret Service线协议夹具：唯一查找/创建、锁定/缺钥/重复/提示拒绝及会话关闭'),
    ('R008-TEST-069', 'client/node', 'TestNativeNodeDPAPIAndACLProtectControlState', '实际Windows DPAPI及本人/SYSTEM ACL，损坏保护钥不重建'),
    ('R008-TEST-070', 'client/node', 'TestNativeNodeProcessDeathRetainsOriginalCommand', '实际原生子进程强制退出后释放OS锁，原身份/待登记命令恢复'),
    ('R008-TEST-071', 'client/cmd/p11-node', 'TestNativeCLIRejectsCredentialArgumentsAndUnboundedInput', '原生命令拒绝凭据参数/回显及超界标准输入'),
    ('R008-TEST-072', 'api/tests', 'TestHTTPNativeProviderOriginalRecoveryAndRevocation', '实际原生代理/DPAPI/HTTP/临时PG：丢登记/心跳回执后原命令恢复，数据库唯一事件及撤销停止'),
]

R009_CASES = [(f'R009-TEST-{i:03d}', p, n, d if i != 45 else '实际Web/Taro H5及Windows控制CLI：原建单/配对/PATCH丢回执恢复，登记/心跳、停止撤销与数据库唯一事件') for i, (_, p, n, d) in enumerate(R008_CASES, 1)]

R010_CASES = [(f'R010-TEST-{i:03d}', p, n, d) for i, (_, p, n, d) in enumerate(R009_CASES, 1)] + [
    ('R010-TEST-073', 'client/protocol/executionlease', 'TestExecutionLeaseIndependentSignatureVector', '独立固定字节签名向量、全uint64和验后数据不可变'),
    ('R010-TEST-074', 'client/protocol/executionlease', 'TestExecutionLeaseRejectsAmbiguityTamperAndWrongPlan', '逐项计划绑定、重复/别名/未知/空值字段和签名篡改拒绝'),
    ('R010-TEST-075', 'client/protocol/executionlease', 'TestExecutionLeaseFreshnessAndZeroTrustFailClosed', '无信任钥、零值、未生效及到期租约关闭'),
    ('R010-TEST-076', 'client/execution', 'TestNativeInboxOriginalCommandAndSingleSubmit', '原命令去重、异载荷拒绝、一次提交与原backend映射'),
    ('R010-TEST-077', 'client/execution', 'TestNativePhysicalGateUnifiesEnginesAndMaintenance', '同物理摘要不建别名；本地/任务/维护同门闩；不造租赁权'),
    ('R010-TEST-078', 'client/execution', 'TestNativeDualFencesPreserveFullUint64HighWater', '任务/资源双高水位、跨任务拒旧与本地代次耗尽不回绕'),
    ('R010-TEST-079', 'client/execution', 'TestNativeRenewalRequiresNewSignedImmutableRevision', '完整新签名递增revision；环境/截止不暗改，旧版和迟到续约拒绝'),
    ('R010-TEST-080', 'client/execution', 'TestNativeDeadlineAndClockRollbackNeverFreeResource', '绝对/单调双截止和回拨隔离，清理可继续但不自动FREE'),
    ('R010-TEST-081', 'client/execution', 'TestNativeReopenQuarantinesWithoutResubmit', '重开一律撤销内存执行权；保留原backend，先查停清'),
    ('R010-TEST-082', 'client/execution', 'TestNativeStopCleanupEvidenceAndOldOwnerCAS', '未知停算/部分清理拒绝；旧owner清理不能释放下一代'),
    ('R010-TEST-083', 'client/execution', 'TestNativeInboxSQLFailureRollsBackAndPoisonsGate', '事务中途真实SQLite故障原子回滚，门闩中毒关闭'),
    ('R010-TEST-084', 'client/execution', 'TestNativeStoreRejectsMissingCorruptionBindingAndSchemaDrift', 'SQLite缺失/损坏/错误锚定/DDL及版本漂移拒绝，不重建'),
    ('R010-TEST-085', 'client/execution', 'TestNativeConcurrentReceiveAndSubmitReserveOnlyOnce', '20并发只接纳一个owner；20并发只保留一次提交权'),
    ('R010-TEST-086', 'client/execution', 'TestNativeAbruptProcessExitRetainsAmbiguousSubmission', '实际子进程异常退出保留WAL中的SUBMITTING，恢复只查不重提'),
    ('R010-TEST-087', 'client/execution', 'TestNativeLateBackendQueryDuringStopNeverRestoresExecution', '停止期间迟到原backend可用于取消，不恢复执行或替换映射'),
    ('R010-TEST-088', 'client/execution', 'TestNativeMetadataCapacityAndSQLiteBounds', '4096收据容量、SQLite缓存/映射/页数上限和安全投影'),
    ('R010-TEST-089', 'client/node', 'TestNodeExecutionAnchorSingleInitAndSharedProcessLock', '节点加密锚定与一次初始化，共OS锁，重开本地owner隔离'),
    ('R010-TEST-090', 'client/node', 'TestNodeExecutionMissingDatabaseNeverResetsIdentity', '丢执行库不重建、不换原控制身份或执行锚定'),
    ('R010-TEST-091', 'client/node', 'TestNodeExecutionInterruptedInitAndBindingFailClosed', '先锚后库中断、既有无锚文件、错误绑定均关闭'),
    ('R010-TEST-092', 'client/node', 'TestNativeExecutionSQLiteInheritsPrivateWindowsACL', '实际Windows DPAPI/本人SYSTEM ACL覆盖SQLite/WAL/SHM及重开'),
]

R011_CASES = [(f'R011-TEST-{i:03d}', p, n, d) for i, (_, p, n, d) in enumerate(R010_CASES, 1)] + [
    ('R011-TEST-093', 'client/supervisor', 'TestSupervisorRequiresWatchdogAndFrozenRegistry', '监督循环未运行拒绝提交、注册绑定复制且单次运行'),
    ('R011-TEST-094', 'client/supervisor', 'TestSupervisorOriginalCommandSubmittedOnlyOnce', '原命令重复12次只提交一次，退出先停算清理'),
    ('R011-TEST-095', 'client/supervisor', 'TestSupervisorExpiryCancelsAndCleansWithoutRenewal', '租约到期自动取消/查询/清理，不由轮询续租'),
    ('R011-TEST-096', 'client/supervisor', 'TestSupervisorAmbiguousSubmitQueriesOriginalAndStops', '提交丢回执查询原backend后停止，不重新提交'),
    ('R011-TEST-097', 'client/supervisor', 'TestSupervisorStopInterruptsInFlightSubmit', '显式停止打断进行中的提交并继续清理'),
    ('R011-TEST-098', 'client/supervisor', 'TestSupervisorCallerCancellationDoesNotAbandonCleanup', '调用方取消不放弃原owner恢复清理'),
    ('R011-TEST-099', 'client/supervisor', 'TestSupervisorCancelAcknowledgementNeverProvesStopped', '取消ACK不冒充已停，关闭超时保留占用'),
    ('R011-TEST-100', 'client/supervisor', 'TestSupervisorQueryFailureStillCancelsAndRetainsOwner', '查询失败仍先取消，缺停止证据保留占用'),
    ('R011-TEST-101', 'client/supervisor', 'TestSupervisorWrongOwnerAndIncompleteCleanupRetainResource', '跨owner回执及不完整清理都不释放'),
    ('R011-TEST-102', 'client/supervisor', 'TestSupervisorReopenQueriesWithoutSubmissionOrAuthority', 'SQLite重开只查停原任务，不重授执行或提交'),
    ('R011-TEST-103', 'client/supervisor', 'TestSupervisorUnknownRegistryQuarantinesWithoutFallback', '环境未注册隔离，禁止换内核兜底'),
    ('R011-TEST-104', 'client/supervisor', 'TestSupervisorDatabaseFailureClosesAdmissionAndCancelsOwnedWork', '数据库故障关闭准入并尽力取消原任务'),
    ('R011-TEST-105', 'client/supervisor', 'TestSupervisorCleanupRetryDoesNotResubmitOrRepeatStop', 'CLEANING重试不重提或反复取消后端'),
    ('R011-TEST-106', 'client/supervisor', 'TestSupervisorQueryTimeoutDoesNotBlockExpiryLoop', '受阻查询不阻塞截止检查及取消'),
    ('R011-TEST-107', 'client/supervisor', 'TestSupervisorSignedRenewalKeepsOriginalBackend', '完整签名续约保持原backend，不重启任务'),
    ('R011-TEST-108', 'client/supervisor', 'TestSupervisorRealDeadlineInterruptsPendingSubmission', '真实时钟截止打断未完成提交，不等待更长调用超时'),
    ('R011-TEST-109', 'client/supervisor', 'TestSupervisorKnownBackendAbsentCannotFreeResource', '已知backend消失不作为STOPPED证据'),
    ('R011-TEST-110', 'client/supervisor', 'TestSupervisorSlowResourceDoesNotBlockAnotherExpiry', '一资源查询受阻不阻塞另一资源到期清理'),
    ('R011-TEST-111', 'client/supervisor', 'TestSupervisorClockRollbackStopsWithoutFreeByTime', '时钟回拨触发实际取消/查询/清理流程'),
    ('R011-TEST-112', 'client/node', 'TestNodeSupervisorRequiresExplicitLedgerAndSameOwnership', '显式初始化执行库，监督器不可越过原节点OS锁'),
    ('R011-TEST-113', 'client/node', 'TestNodeControlDrainStopsSupervisedOriginalOwner', '实际原生HTTP控制读到DRAINING/拒绝心跳，关闭监督器并等待原owner清理'),
]

R012_CASES = [(f'R012-TEST-{i:03d}', p, n, d) for i, (_, p, n, d) in enumerate(R011_CASES, 1)] + [
    ('R012-TEST-114', 'gateway/core', 'TestRelayNativeJSONThreeDialectsAndFixedSeller', '三种文本JSON原生直通、固定模型别名及协议usage候选'),
    ('R012-TEST-115', 'gateway/core', 'TestRelayRejectsRequestAmbiguityBeforeAdmission', '重复键/大小写/Unicode/长度/未知字段在准入前拒绝'),
    ('R012-TEST-116', 'gateway/core', 'TestRelayProfilesRejectToolsStateBetaAndMixedCredentials', '工具/状态/beta与冲突身份拒绝，Responses显式store=false'),
    ('R012-TEST-117', 'gateway/core', 'TestRelayStripsBuyerSecretsProxyAndBackendHeaders', '买方凭据代理头不进后端，响应Cookie/调试地址不出域'),
    ('R012-TEST-118', 'gateway/core', 'TestRelayAdmissionDenialAndGrantMismatchNeverSubmit', '准入拒绝及错误卖家/合同/围栏/期限不联系后端'),
    ('R012-TEST-119', 'gateway/core', 'TestRelayFrozenBindingAndPrivacyDomains', '冻结固定路由、HTTP环回显式配置，平台明文拒绝E2E域'),
    ('R012-TEST-120', 'gateway/core', 'TestRelayRedirectAndErrorNeverRetryOrLeak', '307/401/429/500均不轮换重试，不泄露上游正文'),
    ('R012-TEST-121', 'gateway/core', 'TestRelayUnknownSubmissionIsAttemptedOnce', '原POST提交丢响应仅一次，结果保留未知'),
    ('R012-TEST-122', 'gateway/core', 'TestRelayNativeStreamsAcrossByteBoundaries', '三方言实际HTTP逐字节UTF8/CRLF及原生流终态'),
    ('R012-TEST-123', 'gateway/core', 'TestRelayChatRequiresFinishAndRequestedUsageBeforeDone', 'Chat真实finish/按需usage之后才能传DONE'),
    ('R012-TEST-124', 'gateway/core', 'TestRelayMessagesRejectsUnclosedBlocksThinkingAndErrors', 'Messages内容块闭合，thinking/错误/缺终态拒绝'),
    ('R012-TEST-125', 'gateway/core', 'TestRelayResponsesMaintainsItemsSequenceAndTextIntegrity', 'Responses项索引/序号/文本摘要/完成事件一致性'),
    ('R012-TEST-126', 'gateway/core', 'TestRelayUsageAbsentInvalidAndIncompleteRemainHonest', 'usage缺失不补零、错误累计/包含关系拒绝、截断保留'),
    ('R012-TEST-127', 'gateway/core', 'TestRelayBoundsRequestEventResponseAndMalformedSSE', '请求/事件/响应字节上限、坏UTF8/重复键/未闭合SSE拒绝'),
    ('R012-TEST-128', 'gateway/core', 'TestRelaySSECommentsMultilineCRAndEOFFraming', 'SSE注释/多行data/LF-CRLF-CR及EOF不补事件'),
    ('R012-TEST-129', 'gateway/core', 'TestRelayCancellationRevocationAndHardDeadlineReachUpstream', '调用取消/授权撤销/硬截止关闭实际上游HTTP'),
    ('R012-TEST-130', 'gateway/core', 'TestRelayKeepaliveCannotRenewIdleOrAbsoluteDeadline', '注释不续事件空闲期，合法ping不续绝对截止'),
    ('R012-TEST-131', 'gateway/core', 'TestRelayConcurrencyAndDurableAdmissionPortOwnReplay', '有界并发与原键准入端口；重复不得绕过门槛'),
    ('R012-TEST-132', 'gateway/core', 'TestRelayDownstreamFailureStopsReadingAndUsesWriteDeadline', '下游断写及受阻写有界截止，不继续读取/补成功'),
    ('R012-TEST-133', 'gateway/core', 'TestRelayActualHTTPIngressAndOwnedServerClosure', '真实HTTP入口完成与慢请求正文读截止，关闭自建服务'),
    ('R012-TEST-134', 'client/supervisor', 'TestSupervisorStaleSweepAfterCleanupDoesNotFailOrTouchNextOwner', '清理后旧快照不误判退出失败，资源已释放或下一owner不受影响'),
]


R013_CASES = [(f'R013-TEST-{i:03d}', p, n, d) for i, (_, p, n, d) in enumerate(R012_CASES, 1)] + [
    ('R013-TEST-135', 'client/execution', 'TestNativeCancelledMetadataDoesNotPoisonOriginalOwner', 'SQL开始前取消不毒化健康元数据库或释放原owner'),
    ('R013-TEST-136', 'client/execution', 'TestNativeCancelledMetadataWaitReturnsBeforeOwnerTransaction', '取消排队可在原事务结束前返回，原事务与资源保留'),
    ('R013-TEST-137', 'client/execution', 'TestNativeMetadataLatencyCannotExtendExecutionAuthority', '事务延迟不能续执行权，原owner保留待清理'),
    ('R013-TEST-138', 'client/supervisor', 'TestSupervisorAdapterDeadlineCannotExceedSignedExpiry', '真实原生后端context截止不晚于签名expiry'),
    ('R013-TEST-139', 'client/supervisor', 'TestSupervisorMetadataStepsHaveIndependentBudgets', '各元数据步骤独立I/O预算，后端调用上限不放宽'),
    ('R013-TEST-140', 'client/supervisor', 'TestSupervisorShutdownObservesCleanupBeforeNextPoll', '清理完成后唤醒退出重读，轮询间隔不制造假超时'),
]


def main() -> None:
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--round',default='R013',choices=['R003','R004','R005','R006','R007','R008','R009','R010','R011','R012','R013'])
    round_id=parser.parse_args().round
    cases={'R003':R003_CASES,'R004':R004_CASES,'R005':R005_CASES,'R006':R006_CASES,'R007':R007_CASES,'R008':R008_CASES,'R009':R009_CASES,'R010':R010_CASES,'R011':R011_CASES,'R012':R012_CASES,'R013':R013_CASES}[round_id]
    summary = json.loads((EVIDENCE / f'{round_id}-native-summary.json').read_text(encoding='utf-8'))
    checks = json.loads((EVIDENCE / f'{round_id}-native-source-checks.json').read_text(encoding='utf-8'))
    if summary['test_exit_code'] or not summary['temporary_instance_stopped'] or summary['runner_error'] or summary['cleanup_error'] or checks['status'] != 'PASSED_WITH_LIMITED_SCOPE':
        raise ValueError('必须先取得实际测试、构建和关闭实例的通过证据')
    results = {}
    events = EVIDENCE / f'{round_id}-native-go-tests.jsonl'
    for line in events.read_text(encoding='utf-8').splitlines():
        event = json.loads(line)
        if event.get('Test') and event['Action'] in {'pass', 'fail', 'skip'}:
            results[(event['Package'], event['Test'])] = event
    expected = {('p11.local/compute/' + package, name) for _, package, name, _ in cases}
    top = {k: v for k, v in results.items() if '/' not in k[1]}
    if set(top) != expected or any(event['Action'] != 'pass' for event in results.values()):
        raise ValueError(f'本轮{len(cases)}项实际用例必须齐全且无跳过/失败')
    if round_id in {'R006','R007','R008','R009','R010','R011','R012','R013'}:
        for target in ['web', 'miniapp']:
            key = ('p11.local/compute/api/tests', 'TestFrontendBrowserJourneysUseNativeControlAndOneOriginalOrder/' + target)
            if key not in results or results[key]['Action'] != 'pass': raise ValueError('双端实际浏览器子场景不可跳过')
    # These hashes were collected by the source check after testing. Require
    # them to still match before publishing the consolidated report.
    for name, digest in checks['source_sha256'].items():
        if hashlib.sha256((ROOT / name).read_bytes()).hexdigest() != digest:
            raise ValueError('源码已变化，不能复用旧构建证据')
    report = {
        'round': round_id, 'status': 'PASSED_FOR_CONTROL_AND_RELAY_SUBSET_ONLY' if round_id in {'R012','R013'} else 'PASSED_FOR_CONTROL_SERVICES_ONLY',
        'database_run': summary['run_id'], 'temporary_instance_stopped': True,
        'virtualization_used': False, 'passed_cases': len(cases), 'failed_cases': 0, 'skipped_cases': 0,
        'product_acceptance_status': 'NOT_EXECUTED',
        'tests': [{'id': code, 'package': 'p11.local/compute/' + package, 'test': name, 'description': description, 'result': 'PASSED'} for code, package, name, description in cases],
        'source_evidence': [f'dev/tests/evidence/{round_id}-native-summary.json', f'dev/tests/evidence/{round_id}-native-go-tests.jsonl', f'dev/tests/evidence/{round_id}-native-source-checks.json'],
        'limitations': ['OAuth uses independently signed synthetic responses; no real Alipay channel or seller/device integration', 'No GPU, Android, real payment, crypto interoperability, independent review or whole-product acceptance', 'Linux admin/API commands cross-compiled only, no Linux runtime validation', 'Not a crash or power-loss durability test; no Go race-detector claim', 'Seller/listing approvals and browser device heartbeats are synthetic, no runtime self-approval; provider pairing UI, complete native key stores/daemons and actual device qualification remain pending'],
    }
    if round_id in {'R008','R009','R010','R011','R012','R013'}:
        report['limitations'][2] = 'Linux admin/API/node commands cross-compiled only; Linux Secret Service protocol fixtures do not validate a real Linux keyring'
        report['limitations'][3] = 'Windows process-termination recovery is exercised; no power-loss durability or Go race-detector claim'
        report['limitations'][-1] = 'Windows DPAPI/control agent is exercised; Linux Secret Service uses protocol fixtures and cross-compilation only. No GPU, engine execution, job inbox, content encryption or native Alipay acceptance.'
    if round_id == 'R010':
        report['limitations'][-1] = 'Native metadata inbox, one-shot submission reservations, dual fences and cleanup CAS use explicit synthetic lease/backend observations. No platform issuer/trust lifecycle, timer-driven engine stopping, real GPU/engine/rental, content encryption or native Alipay acceptance.'
        report['limitations'].append('Unchanged frontend unit/build records are reused only after exact source/output/public-log hash checks; both current browser/native-binary journeys are rerun independently.')
        report['development_attempts'] = ['dev/tests/evidence/R010-execution-unit-attempt1.jsonl', 'dev/tests/evidence/R010-native-source-checks-attempt1.json']
    if round_id == 'R011':
        report['limitations'][3] = 'Includes owned native process-exit and SQLite WAL recovery tests; no actual engine crash, power-loss durability or Go race-detector acceptance.'
        report['limitations'][-1] = 'Real native watchdog/SQLite/control coupling uses synthetic metadata-only adapters. Production llama.cpp/LM Studio/ComfyUI/LMA adapters, low-privilege process isolation, physical GPU stop/key/file cleanup, platform lease issuer/trust, rental, E2E and real payments remain pending.'
        report['limitations'].append('Adapter observations/cancellation/cleanup are synthetic and must not qualify hardware. Adapter implementations must honor bounded contexts and authoritative original-key query/cancel semantics; no malicious-adapter containment or timing/performance acceptance is claimed.')
        report['limitations'].append('Current 21 shared TS cases and all three frontend builds are rerun after the provider-loading fix; the held-recovery navigation regression runs on both browser targets. H5 is not native Alipay acceptance.')
        report['development_attempts'] = ['dev/tests/evidence/R011-native-summary-attempt1.json', 'dev/tests/evidence/R011-native-go-tests-attempt1.jsonl', 'dev/tests/evidence/R011-web-browser-attempt1.json', 'dev/tests/evidence/R011-development-attempts.json']
    if round_id in {'R012','R013'}:
        report['limitations'][-1] = 'Native forwarding uses only loopback synthetic HTTP backends and explicit admission/secret fixtures. No production route/funding/lease gate, SDK compatibility certification, real engine stop, content E2E, paid model or billable usage evidence.'
        report['limitations'].append('Three native text profiles only; no cross-dialect conversion, tools, media or Sub2API code extraction. Prior frontend TS/build receipts are reused after exact source/output/log hash checks; both current browser journeys rerun with the current native binary.')
        report['development_attempts'] = ['dev/tests/evidence/R012-development-attempts.json','dev/tests/evidence/R012-native-go-tests-attempt1.jsonl','dev/tests/evidence/R012-native-summary-attempt1.json','dev/tests/evidence/R012-stale-sweep-before-fix.jsonl','dev/tests/evidence/R012-targeted-after-fix.jsonl','dev/tests/evidence/R012-field-whitelist-before-fix.jsonl','dev/tests/evidence/R012-field-whitelist-after-fix.jsonl']
    if round_id == 'R013':
        report['development_attempts'] = ['dev/tests/evidence/R013-deadlines-before-fix.jsonl','dev/tests/evidence/R013-deadlines-after-fix-attempt1.jsonl','dev/tests/evidence/R013-deadlines-after-fix-attempt2.jsonl','dev/tests/evidence/R013-deadlines-after-fix-attempt3.jsonl','dev/tests/evidence/R013-deadlines-after-fix-attempt4.jsonl','dev/tests/evidence/R013-deadlines-after-fix.jsonl','dev/tests/evidence/R013-completed-owner-wakeup-before-fix.jsonl','dev/tests/evidence/R013-native-go-tests-attempt1.jsonl','dev/tests/evidence/R013-native-summary-attempt1.json','dev/tests/evidence/R013-development-attempts.json']
        report['limitations'].append('Metadata I/O has a separate per-operation 2s default (20ms–5s), bounded by shutdown deadlines. Backend call/lease/cleanup gates are not relaxed; no hard real-time or latency benchmark acceptance.')
    (EVIDENCE / f'{round_id}-验证报告.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    text = f'# {round_id} 原生源码验证报告\n\n仅已列明的控制应用、端侧元数据、监督与协议转发源码子集通过：{len(cases)}项用例通过，实际用例无失败/跳过；临时原生数据库已停止。完整平台80项主验收仍为 NOT_EXECUTED。\n\n'
    text += '| 用例编号 | 验证内容 | 结果 |\n| --- | --- | --- |\n'
    for code, _, _, description in cases:
        text += f'| {code} | {description} | PASSED |\n'
    text += '\n原生 PostgreSQL 实例：`' + summary['run_id'] + '`；关闭方式：`' + summary['shutdown_mode'] + '`。实例与工具均在项目 runtime，无本地虚拟化。\n\n'
    commands = 'admin/API/node' if round_id in {'R008','R009','R010','R011','R012','R013'} else 'admin/API'
    text += f'Go 依赖校验、格式检查、vet及 Windows/Linux amd64 {commands}命令构建均通过；Linux仅交叉编译。依赖版本/许可清单只覆盖当前Go模块，不是全平台SBOM。\n\n'
    text += f'- [Go/数据库实际事件]({round_id}-native-go-tests.jsonl)\n- [实例退出与范围]({round_id}-native-summary.json)\n- [构建、源码摘要与依赖校验]({round_id}-native-source-checks.json)\n- [Go依赖与许可证](../../licenses/go-dependencies.json)\n\n'
    text += '本轮实际覆盖原生HTTP/会话/CSRF，但仅通过签名合成渠道响应；未覆盖真实支付宝授权、GPU、Android、资金渠道、密码互通、独立评审、性能和完整产品主验收；不声称Go race detector或断电恢复通过。测试资格均为显式写入临时数据库的合成夹具，不能作为真实开售证据。\n\n'
    if round_id in {'R006','R007','R008','R009','R010','R011','R012','R013'}:
        text += f'第45项包含实际React Web/Taro H5两个浏览器子场景：连接真正HTTP/临时PostgreSQL，主动丢弃已提交建单回执，刷新后以原键原参数恢复，数据库各仅一单及一条OrderCreated。授权/资格/设备心跳为合成夹具，H5不代表支付宝原生容器。测试包顺序执行，各用例内真实并发保留，业务锁等待和设备45秒在线规则不放宽。另有{21 if round_id in {'R009','R010','R011','R012','R013'} else 13}项共享TS用例、三目标编译与真实媒体证据，见[当前前端检查]({round_id}-frontend-checks.json)。迁移用例增加真正持锁3.5秒后的双并发准入回归；旧SQL原字节不改。\n\n'
    if round_id == 'R008':
        text += '第61—72项新增原生提供方控制代理：实际Windows DPAPI/ACL、进程强制终止后的OS锁与原命令恢复，以及真实HTTP/临时PG丢登记和心跳回执验证。另以当前编译出的Windows二进制独立运行init、status和未登记heartbeat三个命令，跨进程身份不变且未登记心跳被拒绝，所有命令进程已退出，见[实际CLI记录](R008-native-node-cli.json)。Linux Secret Service只经过线协议夹具与交叉编译，未实测真实Linux钥库；这里的控制状态加密不是客户内容端对端加密。\n\n'
    if round_id == 'R009':
        text += '第45项另实际覆盖提供方控制：Web和Taro H5各丢弃首次配对及策略提交回执，刷新后恢复原键/方法/正文/If-Match；显式另开配对，通过当前Windows p11-node二进制标准输入登记，心跳只报UNKNOWN。共享条件保存不自授资格，停止/撤销各仅一事件，撤销后的原生心跳拒绝。每端init/enroll/heartbeat/撤权后heartbeat四个进程均退出，见[Web记录](R009-web-browser.json)及[H5记录](R009-miniapp-browser.json)。两次失败记录均保留：首轮H5脚本过早读取配对响应，后一次Web市场读取与合成心跳并发发生RepeatableRead冲突返回503；分别等待完整响应、通过现有页面显式刷新只读市场后重跑，不自动重试写请求或放宽业务事务。Linux钥库、真机、内核、内容加密和资金仍未验收。\n\n'
    if round_id == 'R010':
        text += '第73—92项新增严格签名Lease、独立计划绑定、SQLite原命令/一次提交/双围栏、双截止/回拨/重开隔离及停止清理owner比较更新；包含真实SQLite异常进程退出和Windows DPAPI/WAL/SHM权限，backend状态仍为合成观察。第45项双端各运行当前Windows二进制init/enroll/execution-init/execution-status/heartbeat/撤销后heartbeat六个进程，元数据库保持0任务/0资源且未启用执行；所有进程/浏览器退出。完整平台签发/信任生命周期、真实引擎停止/删除、租赁父会话、内容E2E及硬件/资金未通过。\n\n'
        text += '开发期失败记录保留：[Windows URI首次失败事件](R010-execution-unit-attempt1.jsonl)与[依赖整理未结束时过早核验的缺sum记录](R010-native-source-checks-attempt1.json)。修正本地盘URI，等待固定依赖整理完成再核验；回拨重复隔离影响清理的问题由同名回归覆盖。最终本轮报告仅汇集最终92项全部通过的实际事件，不重写历史失败。\n\n'
    if round_id == 'R011':
        text += '第93—113项实际运行监督循环、SQLite原命令及原生控制联动，覆盖到期/回拨、调用取消、未知提交、原backend查询、停止/清理重试、不同资源并行和数据库失败关闭。后端为明确合成元数据适配器，没有运行真实模型/GPU或删除真实任务文件/密钥；不得将其作为LP/PE/E2E或完整RuntimeSupervisor验收。前端与原生命令证据沿本轮重跑；所有临时实例/浏览器结束关闭。完整开发仍保留全部56项与222子项。首次失败为恢复中挂载页面错过本人资料加载，事件/已停止实例/关闭浏览器及截图见[开发失败留档](R011-development-attempts.json)，修复后通过冻结恢复回执的双端回归验证，不抹除失败记录。\n\n'
    if round_id == 'R012':
        text += '新增第114—133项为实际原生HTTP与合成后端的转发用例：固定卖家/合同、三种文本方言、严格输入、流终态、取消/截止/受阻写入与无重试。准入/凭据均为显式夹具，不发布模型入口，不证明GPU停算、计费、SDK认证或E2E。前端21项TS及三目标构建只经摘要复核复用，双端浏览器与当前Windows节点命令重新运行。第134项确定性复现并修复清理完成后旧快照ErrFence误判退出失败，未放宽清理证据/CAS或时限。既有115/126项中的请求/响应负向用例补充含分隔符伪字段，白名单逐完整名称相等比较，不使用子串近似。开发夹具超时、原完整失败和修正见[R012失败留档](R012-development-attempts.json)。\n\n'
    if round_id == 'R013':
        text += 'R013新增135—140项：取消元数据排队不破坏健康账证、事务耗时不延租、实际backend截止不晚于原签名、独立元数据I/O预算与分步骤预算。初始五项与新增完成唤醒负向复现全部失败并保留，修复后逐项重跑；原150毫秒租约与后端调用上限未放宽。元数据预算新设默认2秒，与后端预算分离，并受退出绝对截止约束；这不是PE/LP或硬实时验收。\n\n'
    attempts=sorted(EVIDENCE.glob(round_id+'-native-summary-attempt*.json'))
    for path in attempts:
        old=json.loads(path.read_text(encoding='utf-8'))
        text+=f'- 历次验证记录：[{path.name}]({path.name})；测试退出{old["test_exit_code"]}；临时实例停止={old["temporary_instance_stopped"]}。\n'
    (EVIDENCE / f'{round_id}-验证报告.md').write_text(text.rstrip() + '\n', encoding='utf-8')
    print(json.dumps({'round': round_id, 'passed': len(cases), 'scope': 'CONTROL_AND_RELAY_SUBSET_ONLY' if round_id in {'R012','R013'} else 'CONTROL_SERVICES_ONLY'}, ensure_ascii=False))


if __name__ == '__main__':
    main()
