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


R014_CASES = [(f'R014-TEST-{i:03d}', p, n, d) for i, (_, p, n, d) in enumerate(R013_CASES, 1)] + [
    ('R014-TEST-141', 'tests/integration', 'TestBudgetConcurrentReservationsCannotExceedOriginalOrder', '20并发预留不超过原订单上限'),
    ('R014-TEST-142', 'tests/integration', 'TestBudgetOriginalPurposeReplaysWithoutSecondReservation', '同原purpose并发只预留一次，异金额冲突'),
    ('R014-TEST-143', 'tests/integration', 'TestBudgetQualifiedActualConsumesAndZeroReleasesUnusedCeiling', '可信资格实际额消耗，已确认零费用释放，未用上限返回'),
    ('R014-TEST-144', 'tests/integration', 'TestBudgetUnknownAndUnverifiedUsageNeverReleaseHold', '未知/未验证usage与订单关闭不释放预留'),
    ('R014-TEST-145', 'tests/integration', 'TestBudgetScopeAndOriginalPurposeCannotCrossOrders', '原买家/卖家/合同/用途绑定，跨单拒绝'),
    ('R014-TEST-146', 'tests/integration', 'TestBudgetDeadlineAndOrderStateStopNewCapacity', '待付款/关闭/复核/到期停止新预留'),
    ('R014-TEST-147', 'tests/integration', 'TestBudgetRollbackIncludesCapacityAndCallerMutation', '预算与调用方任务写同事务回滚'),
    ('R014-TEST-148', 'tests/integration', 'TestBudgetConcurrentCompletionIsOneImmutableOutcome', '并发同来源完成只消耗一次，终态不可重开'),
    ('R014-TEST-149', 'tests/integration', 'TestBudgetSourceAmountAndPurposeMustMatchReservation', '资格来源/原用途/实际额与预留上限匹配'),
    ('R014-TEST-150', 'tests/integration', 'TestBudgetSQLGuardsRejectCounterAndHistoryBypasses', '直接SQL不能改计数、删历史、伪零或改资格'),
    ('R014-TEST-151', 'tests/integration', 'TestBudgetRepeatableReadCannotUseStaleCapacity', '重复读旧快照因父行更新中止，不超分配'),
    ('R014-TEST-152', 'tests/integration', 'TestBudgetInvalidAndZeroRequestsStayBounded', '金额边界及零上限不伪造实际费用'),
    ('R014-TEST-153', 'tests/integration', 'TestBudgetDirectSQLConcurrencySharesParentCapacityGuard', '直接SQL并发仍受父订单原子容量约束'),
    ('R014-TEST-154', 'tests/integration', 'TestBudgetFinalRentalMeterReleasesOnlyItsOriginalSession', '只接受原租赁会话的最终计量资格'),
    ('R014-TEST-155', 'tests/integration', 'TestBudgetAcceptanceRequiresAcceptedOriginalDelivery', '接受来源须为原买家验收且关联原任务VALID已接受交付'),
    ('R014-TEST-156', 'tests/integration', 'TestBudgetLegacyMigrationRetainsUnknownCapacityAndRejectsOverbooking', '001—005升级保守保留旧行，历史超额整体回滚006'),
]


R015_CASES = [(f'R015-TEST-{i:03d}', p, 'TestSupervisorShortRealLeaseNeverExecutesPastExpiry' if n == 'TestSupervisorRealDeadlineInterruptsPendingSubmission' else n, '150ms签名短租约：零提交安全拒绝或单次后端截止中止，禁止到期后迟交' if n == 'TestSupervisorRealDeadlineInterruptsPendingSubmission' else d) for i, (_, p, n, d) in enumerate(R014_CASES, 1)] + [
    ('R015-TEST-157', 'tests/integration', 'TestRouteContractFreezesOriginalOrderAndConcurrentReplay', '原订单合同固定与12并发同键只建一份路由/事件'),
    ('R015-TEST-158', 'tests/integration', 'TestRouteContractRejectsCrossOwnerAndChangedConfirmation', '跨买家、卖家、商品版本和策略改写拒绝'),
    ('R015-TEST-159', 'tests/integration', 'TestRouteContractRequiresOriginalSettledEnvironmentAndNoRefund', '同单原付款、已结算、环境和退款门槛'),
    ('R015-TEST-160', 'tests/integration', 'TestRouteContractBoundsDeadlineCapacityAndUnsupportedModes', '预算/次数/有效期边界及未实现模式拒绝'),
    ('R015-TEST-161', 'tests/integration', 'TestRouteContractCurrentRevocationsAndReplayNeverRestoreActive', '当前撤销/关闭投影且旧键不恢复ACTIVE'),
    ('R015-TEST-162', 'tests/integration', 'TestRouteSDKUsesOnlyKeyBoundModelAndExactProtocol', '标准SDK仅精确匹配key绑定模型和原协议'),
    ('R015-TEST-163', 'tests/integration', 'TestRouteSDKAmbiguityIncludesUnavailableSellerAndNeverFailsOver', '同名歧义包含失效路由，禁止换卖家兜底'),
    ('R015-TEST-164', 'tests/integration', 'TestRouteSDKRechecksKeyRoleOrderAndSellerRevocation', '事务内重查key、角色、卖家和原订单'),
    ('R015-TEST-165', 'tests/integration', 'TestRouteSQLBindingsAreImmutableAndCannotCreateBareActiveRows', 'SQL固定路由/订单/支付绑定，空壳ACTIVE拒绝'),
    ('R015-TEST-166', 'tests/integration', 'TestRouteListIsOwnedBoundedAndSharesOriginalBudgetCapacity', '本人有界分页和原订单共享预算保留'),
    ('R015-TEST-167', 'tests/integration', 'TestRouteLegacyUpgradeQuarantinesWithoutInventingConsent', '007升级旧路由隔离，不推测确认/付款'),
    ('R015-TEST-168', 'tests/integration', 'TestReadOnlyStartupMigrationVerificationRejectsMetadataDrift', 'API只读启动迁移校验拒绝缺失/漂移/未知版本'),
    ('R015-TEST-169', 'api/tests', 'TestHTTPRouteContractsUseOriginalOrderScopedKeyAndSafeReplay', '真实HTTP原订单路由、scoped key和安全重放'),
    ('R015-TEST-170', 'api/tests', 'TestHTTPRouteContractsRejectAmbiguousInputCursorAndMissingCSRF', '真实HTTP严格输入、认证/CSRF及游标签名边界'),
]


R016_CASES = [(f'R016-TEST-{i:03d}', p, n, d) for i, (_, p, n, d) in enumerate(R015_CASES, 1)] + [
    ('R016-TEST-171', 'tests/integration', 'TestInvocationPreparationAtomicAcrossThreeNativeProfiles', '三种原生文本方言：原调用/整笔预算/次数/安全事件同事务'),
    ('R016-TEST-172', 'tests/integration', 'TestInvocationConcurrentOriginalRetryCommitsOnePreparation', '12并发原键重试只生成一任务一预留一事件'),
    ('R016-TEST-173', 'tests/integration', 'TestInvocationConcurrentCallsCannotExceedCountOrRestoreCanceledSlot', '独立请求竞争次数上限，取消不恢复已占次数'),
    ('R016-TEST-174', 'tests/integration', 'TestInvocationDistinctCallsShareOriginalOrderCapacity', '不同调用共享原订单容量，不超额'),
    ('R016-TEST-175', 'tests/integration', 'TestInvocationChangedInputOrBoundKeyCannotReplaceOriginal', '改正文/参数/绑定key不能替换原调用'),
    ('R016-TEST-176', 'tests/integration', 'TestInvocationOriginalRecoveryAfterRouteClosureRetainsHold', '原路由/资金/设备失效或取消后只恢复原记录和原占用'),
    ('R016-TEST-177', 'tests/integration', 'TestInvocationCurrentIdentityBlocksNewAndOriginalReplay', '当前key/角色/用户/路由范围撤销均阻止新请求和重放'),
    ('R016-TEST-178', 'tests/integration', 'TestInvocationRejectsInvalidUnfundedOrUncoveredRequests', '无效输入/会话冒用/资金未知/单次上限不足均零副作用'),
    ('R016-TEST-179', 'tests/integration', 'TestInvocationFingerprintRotationAndMissingOldKeyFailClosed', 'HMAC独立密钥复制轮换，缺旧钥或关闭服务拒绝重发'),
    ('R016-TEST-180', 'tests/integration', 'TestInvocationRollbackAndMissingEventCannotCommitPartialPreparation', '业务回滚与延迟约束拒绝缺安全事件的半成品'),
    ('R016-TEST-181', 'tests/integration', 'TestInvocationSQLBindingsCannotMutateDeleteOrRetrofit', '数据库固定原调用绑定，禁止覆盖/删除/降档'),
    ('R016-TEST-182', 'tests/integration', 'TestInvocationDirectSQLCannotBypassCountOrAtomicBudget', '直接SQL也不能越过次数或缺预算/事件提交'),
    ('R016-TEST-183', 'tests/integration', 'TestInvocationDeadlineUsesCurrentFundsEvidenceAndKey', '准备截止取当前资金/资格证据/API key最短值'),
    ('R016-TEST-184', 'tests/integration', 'TestInvocationUpgradePreservesUnboundHistoryWithoutConsent', '008保留历史未绑定任务，不补造同意/预算/调用资格'),
]


R017_CASES = [(f'R017-TEST-{i:03d}', p, n, d) for i, (_, p, n, d) in enumerate(R016_CASES, 1)] + [
    ('R017-TEST-185', 'tests/integration', 'TestExecutionInputsAcrossThreeNativeProfilesAreActuallyEncrypted', '三种原生请求实际校验/加密入库/签发'),
    ('R017-TEST-186', 'tests/integration', 'TestExecutionConcurrentReadyRetainsOneOriginalCiphertext', '八并发就绪仅一份原始密文'),
    ('R017-TEST-187', 'tests/integration', 'TestExecutionInputRejectsMismatchedBodyAndUnsupportedNativeFields', '不信任正文摘要/参数并拒绝不支持字段'),
    ('R017-TEST-188', 'tests/integration', 'TestExecutionInputKeyCopiesRotationAndMissingOldKey', '输入钥复制轮换/缺旧钥关闭'),
    ('R017-TEST-189', 'tests/integration', 'TestExecutionExplicitConfigurationAndCloseFailClosed', '显式密钥配置/用途隔离和关闭'),
    ('R017-TEST-190', 'tests/integration', 'TestExecutionLastPreparedCallUsesItsOwnFullBudgetHold', '末次调用使用自己的预算，不再索取新额度'),
    ('R017-TEST-191', 'tests/integration', 'TestExecutionConcurrentOriginalIssueCommitsOneOwnerLeaseAndDelivery', '八并发签发只有原租约/owner/投递意图'),
    ('R017-TEST-192', 'tests/integration', 'TestExecutionDifferentOrdersContendForOnePhysicalResource', '不同买家订单竞争同一物理资源'),
    ('R017-TEST-193', 'tests/integration', 'TestExecutionPendingAndQueuedCallsNeverAcquirePhysicalOwner', '准备和排队均不提前占设备'),
    ('R017-TEST-194', 'tests/integration', 'TestExecutionRequiresNativeIdentityAndExplicitFreeResource', '需原生身份和明确FREE，过期不释放'),
    ('R017-TEST-195', 'tests/integration', 'TestExecutionCurrentAuthorizationFundsAndQualificationRemainRequired', '当前身份/角色/资金/资格/原路由/输入重核'),
    ('R017-TEST-196', 'tests/integration', 'TestExecutionRecoveryCannotRenewOrReplaceWithdrawnAuthority', '原键恢复不得续租/替换已撤销授权'),
    ('R017-TEST-197', 'tests/integration', 'TestExecutionActualExpiryRetainsOriginalBudgetAndPhysicalOwner', '真实短截止后预算和物理占用保留'),
    ('R017-TEST-198', 'tests/integration', 'TestExecutionPhysicalGenerationPreservesSignedSQLBoundaryWithoutWrap', '平台signed-bigint围栏上限不回绕'),
    ('R017-TEST-199', 'tests/integration', 'TestExecutionReadinessRollbackAndMissingEventLeaveNoInputOrHold', '就绪回滚/缺事件不提交半成品'),
    ('R017-TEST-200', 'tests/integration', 'TestExecutionIssuanceRollbackOrMissingAtomicMemberRetainsQueuedCall', '签发回滚/缺事件/投递/owner/预算拒绝'),
    ('R017-TEST-201', 'tests/integration', 'TestExecutionSQLBindingsCannotReplaceInputLeaseOrInitialOwner', 'SQL不允许重写输入/租约/原owner'),
    ('R017-TEST-202', 'tests/integration', 'TestExecutionCiphertextMustAuthenticateEvenWhenSQLShapeIsReady', '数据库READY形状不代替密文AEAD认证'),
    ('R017-TEST-203', 'tests/integration', 'TestExecutionUpgradeLeavesHistoricalAssetsAndLeasesUnbound', '009保留真实历史资产/租约UNBOUND'),
    ('R017-TEST-204', 'tests/integration', 'TestExecutionIssuedLeaseVerifiesAgainstIndependentPlanAndNativeInbox', '签名租约对独立原计划验证并进入真实SQLite收件箱'),
]


R018_CASES = [(f'R018-TEST-{i:03d}', p, n, d + ('；通用队列不认领或耗尽改写执行投递类型' if n == 'TestDurableJobsFenceStaleClaimsAndBoundRetries' else '')) for i, (_, p, n, d) in enumerate(R017_CASES, 1)] + [
    ('R018-TEST-205', 'tests/integration', 'TestExecutionClaimSignedDeviceGetsExactOriginalLeaseWithoutDecryptor', '原设备领取完全相同的原签名租约，无需内容解密器'),
    ('R018-TEST-206', 'tests/integration', 'TestExecutionClaimEightConcurrentRecoveriesPersistOneBindingAndEvent', '八路并发恢复仅一份原领取绑定/安全事件'),
    ('R018-TEST-207', 'tests/integration', 'TestExecutionClaimOriginalKeyCannotSelectAnotherBodyOrClaim', '原键异正文/另一领取/不同header均不能改领'),
    ('R018-TEST-208', 'tests/integration', 'TestExecutionClaimRejectsSessionOtherDeviceAndMissingExecuteScope', '会话/SDK key/其他设备/缺execute scope拒绝'),
    ('R018-TEST-209', 'tests/integration', 'TestExecutionClaimCurrentAuthorityAlsoAppliesToReplay', '首次与重放均复核撤权、版本、角色、原key、资金/缩短期限、资格及输入'),
    ('R018-TEST-210', 'tests/integration', 'TestExecutionClaimResourceAndBudgetCannotBeBorrowed', '物理owner/代次变化不可借用，原预算保留'),
    ('R018-TEST-211', 'tests/integration', 'TestExecutionClaimVerifierCopiesRetainsHistoryAndCloses', '公钥复制/原issuer保留及缺钥/关闭拒绝'),
    ('R018-TEST-212', 'tests/integration', 'TestExecutionClaimNoCurrentLeaseNeverCreatesAttemptOrFreesResource', '无当前候选不创建尝试、不释放占用'),
    ('R018-TEST-213', 'tests/integration', 'TestExecutionClaimSQLRequiresSafeEventAndRetainsOriginalCommand', 'SQL缺事件回滚，原领取元数据不可重写/删除'),
    ('R018-TEST-214', 'tests/integration', 'TestExecutionClaimExpiryDoesNotReturnOldLeaseOrReleaseHold', '真实到期后不返回旧租约且原预算/资源保留'),
    ('R018-TEST-215', 'tests/integration', 'TestExecutionClaimReturnedBytesStillNeedIndependentPinAndPlan', '返回字节仍需独立公钥与原批准计划验证'),
    ('R018-TEST-216', 'tests/integration', 'TestExecutionClaimWaitsForOriginalOrderBeforeDeviceOrSellerLocks', '实际PG订单/设备锁顺序和原命令advisory等待后重核版本'),
    ('R018-TEST-217', 'tests/integration', 'TestExecutionClaimUsesOwnReservationAfterNewCallCapacityIsExhausted', '新调用容量耗尽仍使用原调用整笔已占预算'),
    ('R018-TEST-218', 'api/tests', 'TestHTTPExecutionClaimNativeTransportReturnsOneOriginalSignedLease', '实际HTTP与原生签名客户端重复领取同原租约'),
    ('R018-TEST-219', 'api/tests', 'TestHTTPExecutionClaimRejectsNonDeviceBadProofAndChangedOriginalBody', 'HTTP设备证明重放/非设备身份/原键异正文拒绝'),
    ('R018-TEST-220', 'api/tests', 'TestHTTPExecutionClaimUnconfiguredServiceFailsClosed', '未显式配置领取服务时503关闭'),
    ('R018-TEST-221', 'client/protocol/devicecontrol', 'TestNativeLeaseClaimFreezesOriginalCommandAndUsesFreshProofs', '原生命令冻结正文，恢复使用新证明和同原键'),
    ('R018-TEST-222', 'api/cmd/p11-api', 'TestLeaseVerifierFileIsExplicitBoundedAndPublicOnly', '公钥文件显式/有界/严格字段和缺环境关闭'),
]


R019_CASES = [(f'R019-TEST-{i:03d}',p,n,d) for i,(_,p,n,d) in enumerate(R018_CASES,1)] + [
('R019-TEST-223', 'tests/integration', 'TestExecutionDeliveryThreeProfilesReturnExactOriginalBytesWithoutIssuer', '三种文本profile重复交付原字节；签发器关闭不影响独立交付'),
('R019-TEST-224', 'tests/integration', 'TestExecutionDeliveryRequiresExistingOriginalClaimAndDevice', '必须先原设备领取，拒绝会话/key/其他设备/缺scope及外来lease'),
('R019-TEST-225', 'tests/integration', 'TestExecutionDeliveryEveryReadRechecksCurrentOriginalAuthority', '每次manifest/input读取重新检查版本/key/资金/资格/输入/资源及双围栏'),
('R019-TEST-226', 'tests/integration', 'TestExecutionDeliveryMissingWrongOrClosedPurposeKeysFailWithoutBytes', '缺/错/关闭的input或fingerprint目的钥不返回内容；原HMAC核对'),
('R019-TEST-227', 'tests/integration', 'TestExecutionDeliveryOwnsKeyCopiesAndRejectsCrossDomainConfiguration', '交付持有独立key副本，调用方清零不破坏；非法/复用目的配置拒绝'),
('R019-TEST-228', 'tests/integration', 'TestExecutionDeliveryIndependentApprovalPinsAndNativeInputAcceptance', '独立原订单/路由/模式及本机内核批准、独立pin和原清单hash验证'),
('R019-TEST-229', 'tests/integration', 'TestExecutionDeliveryNativeInputRejectsExpiryAmbiguousProtocolAndOversize', '端侧拒绝过期/模糊协议/模型变化/超限及未验证的空接收器'),
('R019-TEST-230', 'api/tests', 'TestHTTPExecutionDeliveryNativeSignedReadsReturnManifestAndExactInput', '实际签名原生HTTP清单/原文往返且不ACK/计费或释放'),
('R019-TEST-231', 'api/tests', 'TestHTTPExecutionDeliveryRejectsUnclaimedSessionProofReplayAndPartialReads', 'HTTP拒绝未领取/会话代领/证明重放/部分读取，保留no-store和原空白'),
('R019-TEST-232', 'api/tests', 'TestHTTPExecutionDeliveryUnconfiguredContentKeysFailClosed', '未显式配置内容服务保持503，不因存在设备身份开放'),
('R019-TEST-233', 'client/protocol/devicecontrol', 'TestNativeDeliveryReadCommandsUseExactOriginAndFreshProof', '原生GET绑定预配置origin/URI及空正文，重复Send使用新nonce'),
('R019-TEST-234', 'api/cmd/p11-api', 'TestInputDeliveryFileRequiresSeparateExplicitPurposeKeysAndClaims', '内容密钥文件需显式claims/环境、原版本独立目的钥；超限/重复/未知字段拒绝'),
('R019-TEST-235', 'client/supervisor', 'TestSupervisorUnavailableRegistryStillCancelsOtherOriginalOwners', '未知内核原owner持续隔离；仍取消其他已匹配原owner、不重提或无证释放'),
]


R020_CASES = [(f'R020-TEST-{i:03d}',p,n,d) for i,(_,p,n,d) in enumerate(R019_CASES,1)] + [
('R020-TEST-236', 'client/node', 'TestHostOriginalInputDispatchOnceAndCleanupProjection', '原始正文一次提交、内存清零、原清理凭证投影及重启保留'),
('R020-TEST-237', 'client/node', 'TestHostUnknownClaimRecoversExactCommandAcrossReopen', '丢失领取回执后重启复用原键原正文并更换设备nonce'),
('R020-TEST-238', 'client/node', 'TestHostIndependentApprovalAndPinRejectBeforeInput', '独立pin/订单/模型/输出上限/流式及物理别名与模式不符先拒绝'),
('R020-TEST-239', 'client/node', 'TestHostReopenKeepsOriginalOwnerQueryOnly', '未知提交回执后重启原收件箱只查询，不再次提交正文'),
('R020-TEST-240', 'client/node', 'TestHostCommandsFreezeParametersAndSerializeCapability', '16并发同能力仅一个原命令、参数冻结且持久化先于网络'),
('R020-TEST-241', 'client/node', 'TestHostMissingCorruptAndCrossNodeStateNeverReinitializes', '缺失/损坏/错绑定/错密文不重新创建已锚定宿主'),
('R020-TEST-242', 'client/node', 'TestHostBoundedResponsesAndExpiredTicketNeverSubmit', '清单和输入接收上限、签名过期边界与已关闭票据拒绝'),
('R020-TEST-243', 'client/node', 'TestHostConcurrentPrepareAndCloseCancelOriginalRead', '并行准备不重复领取、关闭取消原阻塞读取'),
('R020-TEST-244', 'client/node', 'TestHostCloseWipesIdleTicketsAndStatusHasNoContent', '空闲票据关闭清零，安全状态/原生账本扫描无样例正文'),
('R020-TEST-245', 'client/node', 'TestHostCatalogRejectsSubstitutionAndForeignSupervisor', '已批准内核版本替换与非宿主监督器均拒绝'),
('R020-TEST-246', 'client/node', 'TestHostCorruptCommandStopsAdmissionAndCannotProjectCleanup', '未认证阶段损坏隔离，未提交命令不能冒领清理'),
('R020-TEST-247', 'client/node', 'TestHostSelectorsCannotHideUnfinishedCommand', '损坏phase/cap索引不能隐藏原未完成命令去领取新任务'),
('R020-TEST-248', 'client/node', 'TestHostCapacityRetainsOriginalAndRefusesNew', '4096条有界元数据满额保留原记录、拒绝新领取'),
('R020-TEST-249', 'client/node', 'TestHostLegacyMetadataAdapterCannotConsumeOriginalContent', '元数据适配器不隐式消费正文、不写入执行receipt'),
('R020-TEST-250', 'client/node', 'TestHostNativeDPAPIACLAndEncryptedJournalReopen', '实际Windows DPAPI/owner-SYSTEM ACL和加密宿主账本重开'),
]


R021_CASES = [(f'R021-TEST-{i:03d}',p,n,d) for i,(_,p,n,d) in enumerate(R020_CASES,1)] + [
('R021-TEST-251', 'client/adapters/llamaworker', 'TestNativePipeThreeProfilesKeepTextAndRejectUnimplementedSemantics', '三文本规范保留正文/采样/停止语义，额外字段/错模式/错绑定拒绝'),
('R021-TEST-252', 'client/adapters/llamaworker', 'TestNativePipeFramesBoundLengthBeforeAllocation', '内部分帧先验长，超限/截断在分配前拒绝'),
('R021-TEST-253', 'client/adapters/llamaworker', 'TestNativePipeOriginalOwnerOnceAndCleanupReceipt', '一次原提交、原generation清理、输出只交授权内容读取者'),
('R021-TEST-254', 'client/adapters/llamaworker', 'TestNativePipeCancelIndependentOfBlockedContent', '正文管道阻塞时独立原取消仍可达'),
('R021-TEST-255', 'client/adapters/llamaworker', 'TestNativePipeExitKeepsAmbiguousOwner', '内核退出保持UNKNOWN、不可重提和伪造STOPPED'),
('R021-TEST-256', 'client/adapters/llamaworker', 'TestNativePipeSlowOutputCancelsWithinFixedBuffer', '两个64KiB缓冲满后取消，清空输出不冒充成功'),
('R021-TEST-257', 'client/adapters/llamaworker', 'TestNativePipeStaleCallbackCannotReleaseNextOwner', '旧内核回执不得释放新本地generation'),
('R021-TEST-258', 'client/adapters/llamaworker', 'TestNativePackPinsPathsAndCompleteLibraryAllowlist', '本地包/模型/模板hash、路径范围及完整动态库白名单'),
]


R022_CASES = [(f'R022-TEST-{i:03d}',p,n,d) for i,(_,p,n,d) in enumerate(R021_CASES,1)] + [
('R022-TEST-259', 'client/results', 'TestEncryptedResultRetainsOriginalAcrossCleanupAndNextOwner', '原结果密文保管与显卡清理解耦，下一owner不覆盖，读取不重执行'),
('R022-TEST-260', 'client/results', 'TestEncryptedResultRejectsTamperTruncateSwapAndTrailing', 'AEAD篡改、截断、换头、序号、尾随字节拒绝成功END，磁盘无正文/密钥'),
('R022-TEST-261', 'client/results', 'TestEncryptedResultLimitsRetentionAndOneReader', '单读者、总槽位预留、过期撤权与读者关闭后删除，不提前释放容量'),
('R022-TEST-262', 'client/results', 'TestEncryptedResultQuotaFailureCancelsAndDrainsBeforeRelease', '密文容量耗尽取消并持续排空原输出，失败结果不冒充完整回执'),
('R022-TEST-263', 'client/results', 'TestEncryptedResultMissingKeyCannotReplayOrStart', '孤儿/活跃根目录拒绝复用，丢密钥不重构、不重跑、无记录保持UNKNOWN'),
('R022-TEST-264', 'client/results', 'TestEncryptedResultPreEntryAbsenceAndAmbiguousEntry', '仅已完成的入内核前拒绝可证明ABSENT，已进入不明提交保持UNKNOWN'),
('R022-TEST-265', 'client/results', 'TestEncryptedResultCancelledReadCannotForgeDurableEnd', '读取取消/未到STARTED/结果失败不能伪造持久END'),
('R022-TEST-266', 'client/results', 'TestEncryptedResultStoreCloseWithdrawsPendingEntry', '结果仓关闭撤销准备中的准入，不触发本机推理'),
('R022-TEST-267', 'client/protocol/textoutput', 'TestNativeTextOutputThreeProfilesRoundTripStrictRelay', '三协议流式/非流式及三种停止原因，经实际HTTP和既有严格转发器验证'),
('R022-TEST-268', 'client/protocol/textoutput', 'TestNativeTextOutputFailureNeverWritesSuccessTail', '错误/非法UTF8/截断UTF8/计数不符/错误停止词不产生成功终止'),
('R022-TEST-269', 'client/protocol/textoutput', 'TestNativeTextOutputResponsesTerminalOverflowHasNoPartialEnd', 'Responses最终重复全文超事件界限时不发部分完成序列'),
('R022-TEST-270', 'client/protocol/textoutput', 'TestNativeTextOutputSinkFailureAndCancelledContextCloseReader', '写入失败或调用方取消必须关读者，不生成完成凭证'),
('R022-TEST-271', 'client/protocol/textoutput', 'TestNativeTextOutputChatUsageOnlyWhenRequested', 'Chat仅按原include_usage请求追加独立用量块'),
('R022-TEST-272', 'client/protocol/textoutput', 'TestNativeTextOutputHTTPSinkRequiresDeadlineSupport', '拒绝无写期限能力的响应包装和无限写入时间'),
('R022-TEST-273', 'client/execution', 'TestNativeResultBindingSurvivesNextOwnerWithoutGrant', '原inbox结果身份在释放/新owner后仍可校验，但不恢复执行资格'),
('R022-TEST-274', 'client/node', 'TestHostResultCapabilityReadsOriginalAfterComputeCleanup', '真实签名HTTP/双SQLite/监督器：清理原计算后读原结果且不重执行'),
('R022-TEST-275', 'client/adapters/llamaworker', 'TestNativePipeRejectsMismatchedStartAndStopMetadata', 'STARTED与DONE计数/停止序号不符保持UNKNOWN、不能清理放行'),
]

R023_CASES = [(f'R023-TEST-{i:03d}',p,n,d) for i,(_,p,n,d) in enumerate(R022_CASES,1)] + [
('R023-TEST-276', 'client/adapters/llamaworker', 'TestNativePackageLoadsOnlyCompletePinnedLocalSelection', '完整本机包校验、独立CPU选择与禁止GPU批准静默降档'),
('R023-TEST-277', 'client/adapters/llamaworker', 'TestNativePackageRejectsAmbiguousAndForeignDocuments', '重复/别名/未知/尾随字段及旧管道/其他发行版拒绝'),
('R023-TEST-278', 'client/adapters/llamaworker', 'TestNativePackageRejectsPathsAndUnlistedOrChangedLibraries', '跨目录/设备名/ADS路径及额外库、包/模型/模板篡改拒绝'),
('R023-TEST-279', 'client/adapters/llamaworker', 'TestNativePackageEnvironmentBindsEveryOwnerExecutionParameter', '包与GPU/上下文/批量/线程/层数绑定独立环境摘要'),
('R023-TEST-280', 'client/adapters/llamaworker', 'TestNativePackageBoundsCancellationAndIndependentEnvironmentPin', '包文档大小、取消与独立批准环境摘要门槛'),
('R023-TEST-281', 'client/adapters/llamaworker', 'TestNativeStartupReportsOnlyBoundedStagesAndNeverGrantsWork', '固定启动阶段/错误码、旧档位/非法元数据拒绝且不授予执行权'),
]

R024_CASES = [(f'R024-TEST-{i:03d}',p,n,d) for i,(_,p,n,d) in enumerate(R023_CASES,1)] + [
('R024-TEST-282', 'shared/gpuid', 'TestPhysicalGPUCanonicalDriverForms', '物理UUID及PCI规范化、MIG/无效身份拒绝'),
('R024-TEST-283', 'shared/gpuid', 'TestPhysicalGPUDigestSurvivesOrdinalAndSlotChange', '同显卡编号/插槽改变保持同物理摘要'),
('R024-TEST-284', 'shared/gpuid', 'TestPhysicalGPUInventoryRejectsAliasesAndUnboundedMetadata', '重复身份、设备名/显存及库存规模上限'),
('R024-TEST-285', 'client/adapters/llamaworker', 'TestNativeGPUInventoryUsesBoundedUnambiguousFrames', '有界库存帧、旧握手/尾随/截断/别名拒绝'),
('R024-TEST-286', 'client/adapters/llamaworker', 'TestNativeGPUDiscoveryRejectsUnapprovedAndCancelledPackages', '无独立包针及取消探测不启动工作'),
('R024-TEST-287', 'client/adapters/llamaworker', 'TestNativeReadyRejectsForgedPhysicalSelection', '伪造UUID/PCI/逻辑编号及旧READY不能授予工作'),
('R024-TEST-288', 'client/execution', 'TestNativePhysicalUUIDAliasesCannotCreateParallelGates', '实际UUID摘要不能别名注册绕过共享门闩'),
]

def main() -> None:
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--round',default='R024',choices=['R003','R004','R005','R006','R007','R008','R009','R010','R011','R012','R013','R014','R015','R016','R017','R018','R019','R020','R021','R022','R023','R024'])
    round_id=parser.parse_args().round
    cases={'R003':R003_CASES,'R004':R004_CASES,'R005':R005_CASES,'R006':R006_CASES,'R007':R007_CASES,'R008':R008_CASES,'R009':R009_CASES,'R010':R010_CASES,'R011':R011_CASES,'R012':R012_CASES,'R013':R013_CASES,'R014':R014_CASES,'R015':R015_CASES,'R016':R016_CASES,'R017':R017_CASES,'R018':R018_CASES,'R019':R019_CASES,'R020':R020_CASES,'R021':R021_CASES,'R022':R022_CASES,'R023':R023_CASES,'R024':R024_CASES}[round_id]
    summary = json.loads((EVIDENCE / f'{round_id}-native-summary.json').read_text(encoding='utf-8'))
    checks = json.loads((EVIDENCE / f'{round_id}-native-source-checks.json').read_text(encoding='utf-8'))
    if round_id in {'R014','R015','R016','R017','R018','R019','R020','R021','R022','R023','R024'} and summary.get('suite') != 'full':
        raise ValueError(f'专项验证不能替代完整{len(cases)}项实际回归')
    if round_id in {'R014','R015','R016','R017','R018','R019','R020','R021','R022','R023','R024'} and ([x['id'] for x in summary.get('test_segments', [])] != (['native','core','web','miniapp'] if round_id in {'R020','R021','R022','R023','R024'} else ['core','web','miniapp']) or
                              any(x['exit_code'] for x in summary['test_segments']) or
                              any(x.get('database_isolation') != 'FRESH_NATIVE_CHILD' or x.get('temporary_database_removed') is not True
                                  for x in summary['test_segments'][2 if round_id in {'R020','R021','R022','R023','R024'} else 1:])):
        raise ValueError('全部登记的原生/核心/Web/H5独立分组必须完整实际通过')
    if round_id in {'R020','R021','R022','R023','R024'} and (not summary.get('native_before_postgres') or summary['test_segments'][0]['database_isolation']!='NO_DATABASE_SERVER'):
        raise ValueError('R020 requires all independent native cases before starting the temporary platform DB')
    if round_id in {'R020','R021','R022','R023','R024'}:
        if summary.get('test_event_converter') != ['go','tool','test2json']:
            raise ValueError('Pinned Go test event converter is required')
        if set(summary.get('verification_sources', {})) != {'dev/tools/原生数据库验证.py','dev/tools/frontend/browser.mjs'}:
            raise ValueError('Verification source inventory is incomplete')
        for name, digest in summary.get('verification_sources', {}).items():
            if hashlib.sha256((ROOT/name).read_bytes()).hexdigest() != digest:
                raise ValueError('Verification program changed after execution')
        for segment in summary['test_segments'][1:]:
            wanted = ['dev/src/api/tests','dev/src/tests/integration'] if segment['id']=='core' else ['dev/src/api/tests']
            if segment.get('working_directories') != wanted:
                raise ValueError('Native test working directories must match go test package behavior')
        binaries = summary.get('compiled_db_tests', {})
        if set(binaries) != {'./api/tests', './tests/integration'}:
            raise ValueError('Precompiled database test inventory is incomplete')
        for package, artifact in binaries.items():
            path = (ROOT / artifact['path']).resolve()
            if not path.is_relative_to((ROOT/'runtime/db/integration'/summary['run_id']).resolve()):
                raise ValueError('Precompiled test path is outside the owned run')
            command = artifact.get('command', [])
            if len(command) != 6 or command[:4] != ['go', 'test', '-c', '-o'] or command[-1] != package or Path(command[4]).resolve() != path:
                raise ValueError('Precompiled test command changed')
            if not path.is_file() or hashlib.sha256(path.read_bytes()).hexdigest() != artifact['sha256']:
                raise ValueError('Precompiled test binary digest changed')
    if summary['test_exit_code'] or not summary['temporary_instance_stopped'] or summary['runner_error'] or summary['cleanup_error'] or checks['status'] != 'PASSED_WITH_LIMITED_SCOPE':
        raise ValueError('必须先取得实际测试、构建和关闭实例的通过证据')
    results = {}
    events = EVIDENCE / f'{round_id}-native-go-tests.jsonl'
    for line in events.read_text(encoding='utf-8').splitlines():
        event = json.loads(line)
        if event['Action'] == 'fail' or event.get('Test') and event['Action'] == 'skip':
            raise ValueError('原始完整事件存在失败/用例跳过，不能被后续同名父项覆盖')
        if event.get('Test') and event['Action'] in {'pass', 'fail', 'skip'}:
            results[(event['Package'], event['Test'])] = event
    expected = {('p11.local/compute/' + package, name) for _, package, name, _ in cases}
    top = {k: v for k, v in results.items() if '/' not in k[1]}
    if set(top) != expected or any(event['Action'] != 'pass' for event in results.values()):
        raise ValueError(f'本轮{len(cases)}项实际用例必须齐全且无跳过/失败')
    if round_id in {'R006','R007','R008','R009','R010','R011','R012','R013','R014','R015','R016','R017','R018','R019','R020','R021','R022','R023','R024'}:
        for target in ['web', 'miniapp']:
            key = ('p11.local/compute/api/tests', 'TestFrontendBrowserJourneysUseNativeControlAndOneOriginalOrder/' + target)
            if key not in results or results[key]['Action'] != 'pass': raise ValueError('双端实际浏览器子场景不可跳过')
    # Source checks and tests run as separate operations. Require the recorded
    # source hashes to still match before publishing the consolidated report.
    for name, digest in checks['source_sha256'].items():
        if hashlib.sha256((ROOT / name).read_bytes()).hexdigest() != digest:
            raise ValueError('源码已变化，不能复用旧构建证据')
    report = {
        'round': round_id, 'status': 'PASSED_FOR_CONTROL_AND_RELAY_SUBSET_ONLY' if round_id in {'R012','R013','R014','R015','R016','R017','R018','R019','R020','R021','R022','R023','R024'} else 'PASSED_FOR_CONTROL_SERVICES_ONLY',
        'database_run': summary['run_id'], 'temporary_instance_stopped': True,
        'virtualization_used': False, 'passed_cases': len(cases), 'failed_cases': 0, 'skipped_cases': 0,
        'product_acceptance_status': 'NOT_EXECUTED',
        'tests': [{'id': code, 'package': 'p11.local/compute/' + package, 'test': name, 'description': description, 'result': 'PASSED'} for code, package, name, description in cases],
        'source_evidence': [f'dev/tests/evidence/{round_id}-native-summary.json', f'dev/tests/evidence/{round_id}-native-go-tests.jsonl', f'dev/tests/evidence/{round_id}-native-source-checks.json'],
        'limitations': ['OAuth uses independently signed synthetic responses; no real Alipay channel or seller/device integration', 'No GPU, Android, real payment, crypto interoperability, independent review or whole-product acceptance', 'Linux admin/API commands cross-compiled only, no Linux runtime validation', 'Not a crash or power-loss durability test; no Go race-detector claim', 'Seller/listing approvals and browser device heartbeats are synthetic, no runtime self-approval; provider pairing UI, complete native key stores/daemons and actual device qualification remain pending'],
    }
    if round_id in {'R008','R009','R010','R011','R012','R013','R014','R015','R016','R017','R018','R019','R020','R021','R022','R023','R024'}:
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
    if round_id in {'R012','R013','R014','R015','R016','R017','R018','R019','R020','R021','R022','R023','R024'}:
        report['limitations'][-1] = 'Native forwarding uses only loopback synthetic HTTP backends and explicit admission/secret fixtures. No production route/funding/lease gate, SDK compatibility certification, real engine stop, content E2E, paid model or billable usage evidence.'
        report['limitations'].append('Three native text profiles only; no cross-dialect conversion, tools, media or Sub2API code extraction. Prior frontend TS/build receipts are reused after exact source/output/log hash checks; both current browser journeys rerun with the current native binary.')
        report['development_attempts'] = ['dev/tests/evidence/R012-development-attempts.json','dev/tests/evidence/R012-native-go-tests-attempt1.jsonl','dev/tests/evidence/R012-native-summary-attempt1.json','dev/tests/evidence/R012-stale-sweep-before-fix.jsonl','dev/tests/evidence/R012-targeted-after-fix.jsonl','dev/tests/evidence/R012-field-whitelist-before-fix.jsonl','dev/tests/evidence/R012-field-whitelist-after-fix.jsonl']
    if round_id == 'R013':
        report['development_attempts'] = ['dev/tests/evidence/R013-deadlines-before-fix.jsonl','dev/tests/evidence/R013-deadlines-after-fix-attempt1.jsonl','dev/tests/evidence/R013-deadlines-after-fix-attempt2.jsonl','dev/tests/evidence/R013-deadlines-after-fix-attempt3.jsonl','dev/tests/evidence/R013-deadlines-after-fix-attempt4.jsonl','dev/tests/evidence/R013-deadlines-after-fix.jsonl','dev/tests/evidence/R013-completed-owner-wakeup-before-fix.jsonl','dev/tests/evidence/R013-native-go-tests-attempt1.jsonl','dev/tests/evidence/R013-native-summary-attempt1.json','dev/tests/evidence/R013-development-attempts.json']
        report['limitations'].append('Metadata I/O has a separate per-operation 2s default (20ms–5s), bounded by shutdown deadlines. Backend call/lease/cleanup gates are not relaxed; no hard real-time or latency benchmark acceptance.')
    text = f'# {round_id} 原生源码验证报告\n\n仅已列明的控制应用、端侧元数据、监督与协议转发源码子集通过：{len(cases)}项用例通过，实际用例无失败/跳过；临时原生数据库已停止。完整平台80项主验收仍为 NOT_EXECUTED。\n\n'
    text += '| 用例编号 | 验证内容 | 结果 |\n| --- | --- | --- |\n'
    for code, _, _, description in cases:
        text += f'| {code} | {description} | PASSED |\n'
    text += '\n原生 PostgreSQL 实例：`' + summary['run_id'] + '`；关闭方式：`' + summary['shutdown_mode'] + '`。实例与工具均在项目 runtime，无本地虚拟化。\n\n'
    if round_id in {'R014','R015','R016','R017','R018','R019','R020','R021','R022','R023','R024'}:
        report['test_segments'] = summary['test_segments']
        report['case_inventory'] = summary['case_inventory']
        report['limitations'].append('Order capacity is not a wallet, payment authorization, route/lease admission or GPU release. Qualifications and READY/usage/rental facts are explicitly synthetic; production qualified-source generation and integration remain pending. Legacy budget rows remain fully held, including old RELEASED/CONSUMED states.')
        report['development_attempts'] = [f'dev/tests/evidence/{round_id}-development-attempts.json',f'dev/tests/evidence/{round_id}-migration-immutability.json']
    if round_id == 'R015':
        report['limitations'].append('Confirmed original-order route configuration and exact scoped-key model resolution are implemented; durable invocation/call accounting, atomic budget/funds/lease admission, route UI and real channel confirmation remain pending. All SETTLED/READY and seller/device qualifications are explicit synthetic fixtures.')
    if round_id == 'R016':
        report['limitations'].append('Original routed invocation preparation now commits INPUT_PENDING, a full per-call order-capacity hold, a durable call slot and safe outbox atomically. It does not authorize execution: input-ready, original resource ownership/dual fences, qualified funds/lease issuance, trusted usage and production relay Grant/SecretSource remain pending. SETTLED/READY, qualifications and adapter observations are synthetic.')
        report['limitations'].append('Fingerprint HMAC keys are copied and versioned by the preparation library; production secret loading and full key lifecycle remain pending. Recovery requires the current original scoped API key and retained fingerprint version. Accepted preparation count includes canceled/expired intents and is not billable successful inference count.')
        report['limitations'].append('First complete R016 run passed183/failed1: unchanged TestSupervisorSignedRenewalKeepsOriginalBackend returned ErrShutdown under its original 400ms cleanup deadline. Resources remained retained. Three unchanged isolated repeats passed, but the cause is unproven and timing stability remains open under DEV-53; a later complete pass does not erase that failure.')
        report['development_attempts'] += ['dev/tests/evidence/R016-full-attempt1-archive.json','dev/tests/evidence/R016-supervisor-renewal-diagnostic.json','dev/tests/evidence/R016-supervisor-renewal-diagnostic.jsonl']
    if round_id == 'R019':
        report['limitations'].append('The second R019 complete run also passed 233 / failed 1: the slow-metadata case passed, while the existing unknown-registry case could not read its expected occupied snapshot after shutdown. Exact logs, source evidence and 18 media files are separately archived. A subsequent correction preserves already quarantined/stopping/cleaning owners without repeated writes when a frozen kernel binding is absent, visits every resource before reporting incomplete shutdown, and retains bounded cancellation for other matching original owners. A mixed-resource regression was added without relaxing existing assertions or bounds. This does not establish the cause of the R016 or first R019 slow-metadata failure; DEV-53 remains open.')
        report['development_attempts'] += ['dev/tests/evidence/R019-full-attempt2-archive.json','dev/tests/evidence/R019-registry-diagnostic.json','dev/tests/evidence/R019-registry-diagnostic.jsonl']
        report['limitations'].append('The first R019 complete run passed 233 / failed 1: the existing TestSupervisorMetadataStepsHaveIndependentBudgets returned ErrShutdown under injected slow metadata. That failure did not authorize release; final disk ownership was not independently observed in that failed run. Exact original logs and 18 media files are archived. The standalone diagnostic emitted no test events before its outer build/process timeout, so it has no passing result. The second complete native/browser attempt repeated unchanged source, assertions and runtime bounds; the subsequent frozen-registry correction is documented separately. A later pass does not establish the cause or timing stability; DEV-53 remains open.')
        report['development_attempts'] += ['dev/tests/evidence/R019-full-attempt1-archive.json','dev/tests/evidence/R019-metadata-budgets-diagnostic.json','dev/tests/evidence/R019-native-source-checks-attempt1.json']
        report['development_attempts'] += ['dev/tests/evidence/R016-full-attempt1-archive.json','dev/tests/evidence/R016-supervisor-renewal-diagnostic.json','dev/tests/evidence/R018-development-attempts.json']
        report['limitations'] = [x for x in report['limitations'] if not x.startswith('Native forwarding uses only loopback synthetic HTTP backends')]
        report['limitations'].extend(['Authenticated original-device manifest and exact bounded input reads are connected in the trusted PLAINTEXT/STANDARD domain. A separate service holds explicit input/fingerprint keys, no issuer private key. Original AEAD/AAD and HMAC are rechecked; the receiver needs independent issuer pins and original-order/route/mode/local-kernel approval. Ciphertext hashes are not signed plaintext commitments; plaintext origin/TLS and server checks remain trusted. This does not provide E2E or provider blindness.', 'No read ACKs, renews, reassigns, completes delivery jobs, submits an engine, bills or releases the physical resource/hold. Production durable host commands and trust/pin lifecycle, content adapter/supervisor integration, tokenizer, real kernels/usage/channel, E2E/LP/PE/rental and full acceptance remain pending. SQL001–010 and client SQLite bytes are unchanged.', 'The R016 first-full-run ErrShutdown under the unchanged 400ms cleanup deadline has no proven root cause. Later passes do not establish timing stability; DEV-53 stays open. Initial R019 compile failures and the explicitly limited pre-hardening subset are retained separately.'])
    if round_id == 'R018':
        report['development_attempts'] += ['dev/tests/evidence/R016-full-attempt1-archive.json','dev/tests/evidence/R016-supervisor-renewal-diagnostic.json']
        report['limitations'].append('First R018 full run: 221 passed / 1 failed. Generic metadata Queue.Claim selected an unsupported DELIVER_EXECUTION_LEASE task created by earlier API fixtures. Both claiming and exhausted-job updates now filter supported kinds; stronger assertions preserve unsupported task state/tries. Failed logs and media remain archived; no task is deleted or marked complete to obtain a pass.')
        report['limitations'].append('Verifier-only authenticated native lease delivery; immutable original claim and safe event, exact original scoped key/order/funds/qualification/hold/input/owner rechecks. Claim never ACKs an attempt, completes delivery tasks, decrypts input, submits an engine, renews, reassigns or bills. Production independent pins/kernel plans, actual device input/manifest transport, native CLI durable execution commands, real kernels/usage/channel, E2E/LP/PE/rental and full acceptance remain open. R016 cleanup timing risk remains unresolved.')
    if round_id == 'R017':
        report['limitations'] = [x for x in report['limitations'] if not x.startswith('Native forwarding uses only loopback synthetic HTTP backends')]
        report['limitations'].append('Forwarding Core still uses explicit synthetic Grant/secret adapters; issued platform leases are not connected to a production model listener or device transport.')
        report['limitations'].append('Original bounded native input is actually validated and AES-GCM encrypted in the PLAINTEXT storage domain. Atomic first issuance binds the original hold, current eligibility, explicit FREE native resource, dual fences, signed lease, metadata manifest and delivery intent. This is an internal SQL/local-crypto library, not a mounted SDK listener, relay Grant or actual device delivery/engine execution.')
        report['limitations'].extend(['Production issuer/input/fingerprint key provisioning, independently approved device pins/kernel plan, authenticated claim/download transport, lease renewal/revocation transport, tokenizer enforcement, trusted usage/payment, E2E/LP/PE and rental remain pending. No customer prompt is sent to a GPU or phone.', 'Platform PostgreSQL counters remain signed bigint (max 9223372036854775807); native wire/SQLite preserve uint64 strings, but this issuer closes at its narrower persisted upper bound. No published SQL byte or timeout was relaxed.', 'R016 timing failure remains unresolved: its first full run returned ErrShutdown under the unchanged 400ms cleanup deadline; later passes do not prove stability. DEV-53 remains open.', 'This round retains its failed development attempts and raw evidence; a current full pass does not erase historical failures.'])
        report['development_attempts'] += ['dev/tests/evidence/R016-full-attempt1-archive.json','dev/tests/evidence/R016-supervisor-renewal-diagnostic.json']
    if round_id in {'R020','R021','R022','R023','R024'}:
        report['compiled_db_tests'] = summary['compiled_db_tests']
        report['verification_sources'] = summary['verification_sources']
        report['test_event_converter'] = summary['test_event_converter']
        report['limitations'][-1] = 'Native control/execution/host journals, once-only metadata fencing and in-memory content handoff are exercised with synthetic lease/backend observations. The real model/tokenizer/output lifecycle and hardware qualifications remain pending.'
        report['development_attempts'] = ['dev/tests/evidence/R020-development-attempts.json','dev/tests/evidence/R020-host-attempt1-tests.jsonl','dev/tests/evidence/R020-host-attempt2-tests.jsonl','dev/tests/evidence/R020-host-development-tests.jsonl','dev/tests/evidence/R020-full-attempt2-archive.json','dev/tests/evidence/R020-full-attempt3-archive.json','dev/tests/evidence/R020-full-attempt4-archive.json','dev/tests/evidence/R020-full-attempt5-archive.json','dev/tests/evidence/R020-historical-media-recovery.json','dev/tests/evidence/R020-supervisor-cleanup-diagnostic.json','dev/tests/evidence/R020-supervisor-cleanup-diagnostic.jsonl']
        report['limitations'].extend(['Native execution host metadata uses a separate OS-anchored AEAD SQLite journal, independently provisioned issuer public pin and original order/kernel rules. The original claim is persisted before networking. Input is bounded and in memory, handed only to an explicitly trusted InputAdapter inside the existing once-only native supervisor path. No real engine, tokenizer, content output, usage, execution ACK, renewal or billing is supplied by this slice.', 'PLAINTEXT/STANDARD only. Metadata encryption is not content E2E or protection against the device owner; LP/PE/enhanced privacy remain unqualified. The host CLI exposes init/status only, heartbeat execution_enabled stays false. Backends in host tests are synthetic; Windows DPAPI/ACL/SQLite and HTTP/device-proof operations are real. Linux is cross-built, not a physical runtime result.', 'Published SQL001–010 and original execution SQLite DDL remain byte-identical to R019; the separate host SQLite schema is first introduced here. R016/R019 slow-metadata cleanup root causes and real hardware/channel/end-to-end acceptance remain unresolved; all 56 tasks/222 child steps stay open.'])
    if round_id in {'R020','R021','R022','R023','R024'}:
        report['development_attempts'] += ['dev/tests/evidence/R020-full-attempt1-archive.json','dev/tests/evidence/R020-budget-native-summary.json','dev/tests/evidence/R020-budget-native-go-tests.jsonl']
        report['limitations'].append('The first R020 full run failed: legacy child-DB migration 001 timed out under unchanged statement bounds, a contemporaneous checkpoint had long synchronization, and later integration cases exhausted their unchanged 220s context. The H5 browser also terminated without its final receipt; a failure screenshot and exact runner/browser sources are retained. Underlying storage and browser causes are not proven. Independent native tests now finish before PG starts, and consent readiness/failure journaling are explicit; product source, SQL, assertions and time bounds are unchanged. Later passes do not establish timing stability.')
        report['limitations'].append('A subsequent unchanged two-case diagnostic passed one / failed one; this is not complete verification. The second R020 run was partial: native segment 113 passed / 2 failed, with unchanged supervisor shutdown bounds returning ErrShutdown. Final on-disk ownership was not independently observed in that failed run. Database tests compiled, but a standalone test2json path assumption aborted preparation; PG/browser segments never started. Exact evidence and scripts remain archived. The harness now uses the verified pinned go tool entry; this correction does not fix or establish the cause of supervisor timing failures.')
        report['limitations'].append('The fourth complete R020 run passed 245 / failed 5 / skipped 0. Native 115 cases and both actual browser journeys passed. Five migration-fixture cases could not read their real pre-006 scripts because the precompiled test runner used the source root instead of go test package working directories. Exact failed events and runner/browser sources are archived; historical media retention is partial and explicitly inventoried in the archive manifests. Only the runner working directory is corrected; product source, SQL, fixtures and time bounds are unchanged. This establishes that path-error cause, not the earlier timing-failure causes.')
        report['limitations'].append('The fifth complete R020 run passed 249 / failed 1 / skipped 0: native/core and both actual page journeys passed, but synthetic batch-device heartbeat cleanup reported PostgreSQL 40P01. The retained server log shows the batch UPDATE and market share-lock transaction waiting on one another. Only this test fixture is changed to independent per-device autocommit heartbeats; the 5s tick, every fixture device, real 45s online rule and all error assertions remain. Current source/vet/build checks are rerun after that Go test-source change. This does not prove production deadlocks absent or resolve earlier unproven timing causes.')
        report['limitations'].append('Historical attempt 4/5 media archiving omitted nested directories before subsequent captures overwrote files. Exact original SHA-256 recovery retained 8/18 and 9/18 assets, including all eight historical MP4/GIF files from original frames; 19 original PNGs remain unavailable and are explicitly listed. No replacements are labelled original. The final current 18 frontend assets and reader media are complete; historical missing media are not current passing evidence.')
    if round_id == 'R021':
        capi_path=EVIDENCE/'R021-native-capi-fixture-summary.json'
        capi=json.loads(capi_path.read_text(encoding='utf-8'))
        native_build=json.loads((EVIDENCE/'R021-native-worker-build.json').read_text(encoding='utf-8'))
        if not capi['passed'] or capi['returncode'] or len(capi['cases'])!=5 or set(capi['cases'].values())!={'pass'} or not capi['owned_worker_exit_waited_by_adapter'] or not native_build['passed']:
            raise ValueError('Current native worker build and actual synthetic C ABI process cases required')
        for name,digest in capi['sources'].items():
            if hashlib.sha256((ROOT/name).read_bytes()).hexdigest()!=digest:raise ValueError('Native worker source/build changed after tagged tests: '+name)
        if hashlib.sha256((ROOT/capi['raw']).read_bytes()).hexdigest()!=capi['raw_sha256'] or checks.get('native_adapter_linux_compile_only') is not True:
            raise ValueError('Native C ABI raw events or Linux compile-only evidence missing')
        report['native_capi_subset']={'cases':capi['cases'],'source':'dev/tests/evidence/R021-native-capi-fixture-summary.json','scope':capi['scope'],'separate_from_regular_cases':True}
        report['development_attempts'] += ['dev/tests/evidence/R021-development-attempts.json','dev/tests/evidence/R021-full-attempt1-archive.json','dev/tests/evidence/R021-full-attempt2-archive.json','dev/tests/evidence/R021-full-attempt2-failed-browser-assets.json','dev/tests/evidence/R021-full-attempt3-archive.json','dev/tests/evidence/R021-full-attempt3-extra-diagnostics.json','dev/tests/evidence/R021-native-timing-replay.json']
        report['limitations'].append('R021 adds a fixed-header native C API worker and Go InputAdapter with approved physical-resource binding, explicit template/tokenizer calls, bounded output, independent cancellation and original synchronized cleanup. Null native-context initialization remains UNKNOWN, and parent revocation cannot be undone by a later completion receipt. Actual Windows process tests use a synthetic ABI DLL only, not real llama libraries/weights, GPU offload, LP, E2E, trusted usage, final ACK or public protocol output. The process still uses the caller account; Linux is Go compile-only. Node execution remains disabled. All 56 tasks/222 steps and historical limitations remain open.')
        report['limitations'].append('R021 attempt2 failed only at the Web CDP response body read; the fixture now captures the real upstream body before fulfillment. Attempt3 passed Web but recorded unconfirmed native control-drain cleanup, a two-second watchdog finish timeout, expired shared 220-second integration context, and a 12-second H5 narrow logout locator timeout. Both unchanged native cases passed three isolated repetitions each; the underlying timing/latency causes remain unproven. All original failures/media and exact recovery receipts remain linked; no production/test deadline or assertion was relaxed.')
    if round_id == 'R022':
        capi_path = ROOT/'dev/tests/evidence/R022-native-capi-fixture-summary.json'
        capi = json.loads(capi_path.read_text(encoding='utf-8'))
        if not capi['passed'] or len(capi['cases']) != 6 or set(capi['cases'].values()) != {'pass'}:
            raise ValueError('R022 separate actual native ABI cases are incomplete')
        for name,digest in capi['sources'].items():
            if hashlib.sha256((ROOT/name).read_bytes()).hexdigest()!=digest:raise ValueError('R022 native source/build changed after tagged tests: '+name)
        if hashlib.sha256((ROOT/capi['raw']).read_bytes()).hexdigest()!=capi['raw_sha256'] or checks.get('native_adapter_linux_compile_only') is not True:
            raise ValueError('R022 native raw or Linux compile-only evidence missing')
        report['native_capi_fixture_evidence'] = str(capi_path.relative_to(ROOT).as_posix())
        report['development_attempts'] = ['dev/tests/evidence/R022-development-attempts.json','dev/tests/evidence/R022-capi-attempt1-archive.json','dev/tests/evidence/R022-capi-attempt2-archive.json','dev/tests/evidence/R022-full-attempt1-archive.json','dev/tests/evidence/R022-native-timing-replay.json','dev/tests/evidence/R021-验证报告.json']
        report['limitations'].append('R022 adds local AES-GCM original-result custody, separate compute/result lifetimes and Chat/Responses/Messages JSON/SSE rendering. Default result quota is 16 x 8MiB with five-minute retention; process-only keys cannot survive restart. This remains PLAINTEXT/STANDARD source, not network E2E/LP, real model/GPU, independent official SDK, trusted usage, final ACK or payment acceptance. Responses final-event budgeting and public result transport remain pending; node execution stays disabled. Early supervisor shutdown timeout and first tagged SSE raw-substring assertion failure remain archived. All historical timing/media limitations and 56 tasks/222 steps stay open.')
    if round_id == 'R024':
        for suffix, count in [('native-capi-fixture-summary.json',6),('native-real-summary.json',3)]:
            current_path = ROOT/('dev/tests/evidence/R024-'+suffix)
            current = json.loads(current_path.read_text(encoding='utf-8'))
            if not current['passed'] or current['returncode'] != 0 or len(current['cases']) != count or set(current['cases'].values()) != {'pass'}:
                raise ValueError('R024 separate native test evidence incomplete: '+suffix)
            for name,digest in current['sources'].items():
                if hashlib.sha256((ROOT/name).read_bytes()).hexdigest()!=digest:raise ValueError('R024 native test source changed: '+name)
            if hashlib.sha256((ROOT/current['raw']).read_bytes()).hexdigest()!=current['raw_sha256']:raise ValueError('R024 native raw changed')
        cli = json.loads((ROOT/'dev/tests/evidence/R024-native-kernel-cli.json').read_text(encoding='utf-8'))
        if not cli['passed'] or len(cli['cases'])!=4 or cli['inference_executed'] or cli['execution_enabled'] or cli['proposal_is_approval']:
            raise ValueError('R024 local inspection boundary incomplete')
        report['native_capi_fixture_evidence'] = 'dev/tests/evidence/R024-native-capi-fixture-summary.json'
        report['native_real_model_evidence'] = 'dev/tests/evidence/R024-native-real-summary.json'
        report['native_kernel_inspection_evidence'] = 'dev/tests/evidence/R024-native-kernel-cli.json'
        gpu = json.loads((ROOT/'dev/tests/evidence/R024-native-gpu-cli.json').read_text(encoding='utf-8'))
        if not gpu['passed'] or len(gpu['cases'])!=4 or not all(c['passed'] for c in gpu['cases'].values()) or gpu['model_loaded'] or gpu['execution_enabled']:
            raise ValueError('R024 local GPU discovery boundary incomplete')
        for name,digest in gpu['sources'].items():
            if hashlib.sha256((ROOT/name).read_bytes()).hexdigest()!=digest:raise ValueError('R024 GPU source changed: '+name)
        report['native_gpu_discovery_evidence'] = 'dev/tests/evidence/R024-native-gpu-cli.json'

        report['development_attempts'] = ['dev/tests/evidence/R024-development-attempts.json', 'dev/tests/evidence/R024-native-real-attempt1-archive.json', 'dev/tests/evidence/R022-验证报告.json']
        report['limitations'].append('R024 adds seven regular physical-identity boundary cases (288 total), Windows metadata-only discovery, PIPE_4 selected UUID/PCI binding before model loading and a stable UUID shared-gate digest. Six synthetic C ABI, three actual CUDA top-level cases and eight actual CLI cases are separate scopes. Real tests cover six JSON/SSE combinations, reuse, token rejection, cancel/deadline, metadata discovery, wrong UUID/PCI and missing-device rejection. Local identity is not remote attestation, measured all-layer offload, performance, LP/E2E, SDK, trusted usage, ACK or funds qualification. The first stale-package rejection retains its exact source/worker archive. Production Host/marketplace binding and full LM Studio isolation remain pending, as do all 56/222 tasks and 80/96 full acceptance. Historical timing/storage concerns and R020 missing original media remain open.')
    if round_id in {'R015','R016','R017','R018','R019','R020','R021','R022','R023','R024'}:
        lease_paths = [event.get('Output', '').strip() for event in (json.loads(line) for line in events.read_text(encoding='utf-8').splitlines()) if event.get('Test') == 'TestSupervisorShortRealLeaseNeverExecutesPastExpiry' and event.get('Action') == 'output' and any(marker in event.get('Output', '') for marker in ['ZERO_SUBMIT_SAFE_REJECTION', 'ONE_SUBMIT_BOUNDED_BY_SIGNED_EXPIRY'])]
        if len(lease_paths) != 1:
            raise ValueError('短签名租约必须有实际零提交/截止中止分支证据')
        report['short_real_lease_observed_path'] = lease_paths[0]
    commands = 'admin/API/node' if round_id in {'R008','R009','R010','R011','R012','R013','R014','R015','R016','R017','R018','R019','R020','R021','R022','R023','R024'} else 'admin/API'
    text += f'Go 依赖校验、格式检查、vet及 Windows/Linux amd64 {commands}命令构建均通过；Linux仅交叉编译。依赖版本/许可清单只覆盖当前Go模块，不是全平台SBOM。\n\n'
    text += f'- [Go/数据库实际事件]({round_id}-native-go-tests.jsonl)\n- [实例退出与范围]({round_id}-native-summary.json)\n- [构建、源码摘要与依赖校验]({round_id}-native-source-checks.json)\n- [Go依赖与许可证](../../licenses/go-dependencies.json)\n\n'
    text += '本轮实际覆盖原生HTTP/会话/CSRF，但仅通过签名合成渠道响应；未覆盖真实支付宝授权、GPU、Android、资金渠道、密码互通、独立评审、性能和完整产品主验收；不声称Go race detector或断电恢复通过。测试资格均为显式写入临时数据库的合成夹具，不能作为真实开售证据。\n\n'
    if round_id in {'R006','R007','R008','R009','R010','R011','R012','R013','R014','R015','R016','R017','R018','R019','R020','R021','R022','R023','R024'}:
        text += f'第45项包含实际React Web/Taro H5两个浏览器子场景：连接真正HTTP/临时PostgreSQL，主动丢弃已提交建单回执，刷新后以原键原参数恢复，数据库各仅一单及一条OrderCreated。授权/资格/设备心跳为合成夹具，H5不代表支付宝原生容器。测试包顺序执行，各用例内真实并发保留，业务锁等待和设备45秒在线规则不放宽。另有{21 if round_id in {'R009','R010','R011','R012','R013','R014','R015','R016','R017','R018','R019','R020','R021','R022','R023','R024'} else 13}项共享TS用例、三目标编译与真实媒体证据，见[当前前端检查]({round_id}-frontend-checks.json)。迁移用例增加真正持锁3.5秒后的双并发准入回归；旧SQL原字节不改。\n\n'
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
    if round_id == 'R015':
        text += 'R015新增157—170项：原订单/同单支付路由、三个本人HTTP接口、scoped key精确模型/方言解析、失效/歧义无换卖家、共享容量、007历史隔离和只读启动迁移校验。配置不产生执行权或资金证明；调用签发/次数记账、实际支付/推理及E2E仍待。完整170项采用核心/Web/H5三个原生独立进程，双端各用全新独立原生数据库并在结束删除，全部原始事件无失败/用例跳过才可汇总。专项夹具失败、首轮完整失败及短租约诊断均留档，未降低业务/页面/租期/清理时限。\n\n'
        text += '150ms短签名租约本次实际分支：`' + report['short_real_lease_observed_path'] + '`。零提交拒绝不冒称执行过后端；另一独立4秒用例检查实际适配器context截止。两次完整失败、三次专项夹具失败及独立诊断均见[R015开发留档](R015-development-attempts.json)；本次完整通过不证明已排除全部时序稳定性风险。\n\n'
    if round_id == 'R016':
        text += 'R016新增171—184项：三个原生文本档位原调用准备、12并发同键恢复、独立请求次数/容量竞争、当前撤权、原路由失效恢复、HMAC轮换、回滚及缺事件/预算拒绝、最短截止和008历史升级。INPUT_PENDING、完整单次预算、次数与安全事件同事务，不签发Grant/Lease，不含输入/attempt/执行token，不运行模型。001—007及端侧DDL原字节保留，历史任务不补造调用同意。实际资格/SETTLED/READY及后端仍为显式合成夹具；完整输入/资金/资源/租约准入、真实usage/引擎/渠道/E2E/LP/PE继续待办。\n\n'
        text += '完整184项按核心/Web/H5三个原生进程顺序执行，双端各用全新独立原生数据库并在结束删除；前端21项TS及三目标构建仅按源码/输出/日志精确摘要复核，当前双端浏览器和Windows原生命令另跑。150ms短租约本次实际分支：`' + report['short_real_lease_observed_path'] + '`，不冒称另一分支或真实推理；原时限、SQL等待、清理门槛不放宽。专项/完整运行与历次情况见[R016开发记录](R016-development-attempts.json)。\n\n'
        text += '首轮完整183通过/1失败：既有续租测试在原400ms退出清理上限内未确认完成，返回ErrShutdown并保留资源。未改源码/时限的独立三次复跑通过，但未证明根因；当前全量通过不能等同稳定性问题已排除。原始事件、媒体和路径映射见[首轮留档](R016-full-attempt1-archive.json)，DEV-53继续保留时序稳定性待办。另有提前生成进度因缺证据被拒绝的开发记录，未绕过生成器门槛。\n\n'
    if round_id == 'R014':
        text += 'R014新增141—156项：订单容量预留、已资格化实际费用/零额释放、同purpose幂等、未知保留、并发/回滚/旧快照及原生升级。006仅前向追加8列与约束，001—005和端侧DDL原字节保留；历史任何状态预算均全额标为未核实占用，超出原订单上限则整笔迁移失败。预算不是渠道冻结/付款/钱包/执行权，可信资格生产器、资金/路由准入和真实硬件仍待接入。\n\n'
        text += '完整回归按核心、Web、H5三个独立Go进程顺序执行，双端各用独立迁移并结束删除的原生临时数据库，仍覆盖全部156个唯一顶层用例及双端子场景；浏览器父用例按所选子场景分别执行。各分组实际参数与退出码见原生实例摘要。此隔离修正旧API测试进程共用220秒上下文、录制后误伤后续夹具的问题，未放宽页面、SQL、执行租期、后端、清理或240秒包级时限。原始事件中任一次失败/用例跳过都拒绝汇总，不能由后续同名父项覆盖。152通过/4失败及其后155通过/页面夹具污染失败的开发记录、之前的编译/监督失败和夹具失败均保留。\n\n'
    attempts=sorted(EVIDENCE.glob(round_id+'-native-summary-attempt*.json'))
    for path in attempts:
        old=json.loads(path.read_text(encoding='utf-8'))
        text+=f'- 历次验证记录：[{path.name}]({path.name})；测试退出{old["test_exit_code"]}；临时实例停止={old["temporary_instance_stopped"]}。\n'
    if round_id == 'R017':
        text += 'R017新增185—204项：真实原生文本输入校验、AES-GCM加密落盘、原调用和已占整笔预算重核、明确FREE的原生设备资源、双围栏及45秒以内首租约/安全事件/投递意图同事务。独立原计划验签与真实SQLite入箱已覆盖；仅元数据集成，不运行推理。当前完整204项及双端浏览器均须实际通过，历史失败见开发记录；R016清理时序风险继续开放。仍34/93操作、65表；生产密钥/信任、实际领取和输入传输、SDK Grant/listener、真实usage/支付、E2E/LP/PE/小时租赁未收口。\n\n'
    if round_id == 'R019':
        text += 'R019：37/95显式操作；65表/664列/113外键。原设备受限获取已领取原manifest与原始文本输入；平台明文域AES-GCM/AAD及原HMAC复核、客户端独立公钥和原订单/内核批准。无签发钥交付，不等于E2E、实际执行或计费。SQL001—010/端侧DDL原字节保留；R016时序风险和全56/222子项仍开放。\n\n'
    if round_id == 'R018':
        text += 'R018：35/93操作、65表/664列/113外键；010仅追加六列和领取约束，001—009及端侧DDL原字节保留。实际签名设备通过原订单/资源/租期重新核验领取原lease，原命令固定，领取不等于执行ACK/推理/计费。输入/manifest交付、生产独立信任、可信usage/真实渠道和完整验收仍待；R016时序风险继续开放。\n\n'
    if round_id == 'R020':
        text += 'R020：持久原生命令、独立pin/订单内核批准、受限内存输入及一次提交监督器接线；宿主清理只投影原执行凭证。Windows DPAPI/ACL实际测试、真实HTTP和原生SQLite，模型后端为合成夹具，CLI执行仍关闭。完整源码回归按以上逐项记录核验；无E2E、GPU推理或计费，全部56/222及既有时序风险开放。\n\n'
    if round_id == 'R021':
        text += 'R021另有5项带构建标签的实际Windows原生worker＋合成C ABI测试（含退出与空上下文子场景），独立于上述258项登记。NK-001—042、完整包与原物理资源绑定、一次提交、分词/采样/停止、独立取消、128KiB输出和同步后清理已有源码；真实模型/GPU/LP/E2E、长期CLI、完整结果/SSE/ACK、可信usage与支付仍待。全部56/222和历史时序/归档限制继续开放。\n\n'
    if round_id == 'R022':
        text += 'R022新增17项，常规275项与带标签实际Windows worker＋合成C ABI的6项分开登记。RC-001—060和PIPE_2实现原结果加密保管、清理解耦及三文本JSON/SSE；不等于真实模型/GPU/LP/E2E、官方SDK、可信usage/ACK/支付验收。早期ErrShutdown与首轮ABI测试失败均留档，历史时序风险未证明消除。全56任务/222子项和80/96未执行、DEV-55延后保留。\n\n'
    if round_id == 'R024':
        text += 'R023新增6项常规边界，当前281项常规Go与6项合成ABI、3项真实Windows CUDA/Qwen0.5B顶层用例分开登记；只读检查实际4项。PIPE_3修正真实三库布局，固定包与本机环境摘要已实施，真实模型三协议JSON/SSE六组合、取消/硬截止及缺失GPU拒绝实际通过。四次失败原源码/worker均已归档。仍不授予生产执行、物理卡映射、全层卸载或性能、LP/E2E、官方SDK、可信usage、ACK或资金资格；全56/222与80/96完整验收继续开放。详见NF-001—048和本轮各独立证据。\n\n'
    (EVIDENCE / f'{round_id}-验证报告.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    (EVIDENCE / f'{round_id}-验证报告.md').write_text(text.rstrip() + '\n', encoding='utf-8')
    print(json.dumps({'round': round_id, 'passed': len(cases), 'scope': 'CONTROL_AND_RELAY_SUBSET_ONLY' if round_id in {'R012','R013','R014','R015','R016','R017','R018','R019','R020','R021','R022','R023','R024'} else 'CONTROL_SERVICES_ONLY'}, ensure_ascii=False))


if __name__ == '__main__':
    main()
