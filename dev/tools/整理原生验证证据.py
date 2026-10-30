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

def main() -> None:
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--round',default='R008',choices=['R003','R004','R005','R006','R007','R008'])
    round_id=parser.parse_args().round
    cases={'R003':R003_CASES,'R004':R004_CASES,'R005':R005_CASES,'R006':R006_CASES,'R007':R007_CASES,'R008':R008_CASES}[round_id]
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
    if round_id in {'R006','R007','R008'}:
        for target in ['web', 'miniapp']:
            key = ('p11.local/compute/api/tests', 'TestFrontendBrowserJourneysUseNativeControlAndOneOriginalOrder/' + target)
            if key not in results or results[key]['Action'] != 'pass': raise ValueError('双端实际浏览器子场景不可跳过')
    # These hashes were collected by the source check after testing. Require
    # them to still match before publishing the consolidated report.
    for name, digest in checks['source_sha256'].items():
        if hashlib.sha256((ROOT / name).read_bytes()).hexdigest() != digest:
            raise ValueError('源码已变化，不能复用旧构建证据')
    report = {
        'round': round_id, 'status': 'PASSED_FOR_CONTROL_SERVICES_ONLY',
        'database_run': summary['run_id'], 'temporary_instance_stopped': True,
        'virtualization_used': False, 'passed_cases': len(cases), 'failed_cases': 0, 'skipped_cases': 0,
        'product_acceptance_status': 'NOT_EXECUTED',
        'tests': [{'id': code, 'package': 'p11.local/compute/' + package, 'test': name, 'description': description, 'result': 'PASSED'} for code, package, name, description in cases],
        'source_evidence': [f'dev/tests/evidence/{round_id}-native-summary.json', f'dev/tests/evidence/{round_id}-native-go-tests.jsonl', f'dev/tests/evidence/{round_id}-native-source-checks.json'],
        'limitations': ['OAuth uses independently signed synthetic responses; no real Alipay channel or seller/device integration', 'No GPU, Android, real payment, crypto interoperability, independent review or whole-product acceptance', 'Linux admin/API commands cross-compiled only, no Linux runtime validation', 'Not a crash or power-loss durability test; no Go race-detector claim', 'Seller/listing approvals and browser device heartbeats are synthetic, no runtime self-approval; provider pairing UI, complete native key stores/daemons and actual device qualification remain pending'],
    }
    if round_id == 'R008':
        report['limitations'][2] = 'Linux admin/API/node commands cross-compiled only; Linux Secret Service protocol fixtures do not validate a real Linux keyring'
        report['limitations'][3] = 'Windows process-termination recovery is exercised; no power-loss durability or Go race-detector claim'
        report['limitations'][-1] = 'Windows DPAPI/control agent is exercised; Linux Secret Service uses protocol fixtures and cross-compilation only. No GPU, engine execution, job inbox, content encryption or native Alipay acceptance.'
    (EVIDENCE / f'{round_id}-验证报告.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    text = f'# {round_id} 原生源码验证报告\n\n仅控制应用服务/原生HTTP身份子集及既有基础通过：{len(cases)}项用例通过，实际用例无失败/跳过；临时原生数据库已停止。完整平台80项主验收仍为 NOT_EXECUTED。\n\n'
    text += '| 用例编号 | 验证内容 | 结果 |\n| --- | --- | --- |\n'
    for code, _, _, description in cases:
        text += f'| {code} | {description} | PASSED |\n'
    text += '\n原生 PostgreSQL 实例：`' + summary['run_id'] + '`；关闭方式：`' + summary['shutdown_mode'] + '`。实例与工具均在项目 runtime，无本地虚拟化。\n\n'
    commands = 'admin/API/node' if round_id == 'R008' else 'admin/API'
    text += f'Go 依赖校验、格式检查、vet及 Windows/Linux amd64 {commands}命令构建均通过；Linux仅交叉编译。依赖版本/许可清单只覆盖当前Go模块，不是全平台SBOM。\n\n'
    text += f'- [Go/数据库实际事件]({round_id}-native-go-tests.jsonl)\n- [实例退出与范围]({round_id}-native-summary.json)\n- [构建、源码摘要与依赖校验]({round_id}-native-source-checks.json)\n- [Go依赖与许可证](../../licenses/go-dependencies.json)\n\n'
    text += '本轮实际覆盖原生HTTP/会话/CSRF，但仅通过签名合成渠道响应；未覆盖真实支付宝授权、GPU、Android、资金渠道、密码互通、独立评审、性能和完整产品主验收；不声称Go race detector或断电恢复通过。测试资格均为显式写入临时数据库的合成夹具，不能作为真实开售证据。\n\n'
    if round_id in {'R006','R007','R008'}:
        text += f'第45项包含实际React Web/Taro H5两个浏览器子场景：连接真正HTTP/临时PostgreSQL，主动丢弃已提交建单回执，刷新后以原键原参数恢复，数据库各仅一单及一条OrderCreated。授权/资格/设备心跳为合成夹具，H5不代表支付宝原生容器。测试包顺序执行，各用例内真实并发保留，业务锁等待和设备45秒在线规则不放宽。另有13项共享TS用例、三目标编译与真实媒体证据，见[当前前端检查]({round_id}-frontend-checks.json)。迁移用例增加真正持锁3.5秒后的双并发准入回归；旧SQL原字节不改。\n\n'
    if round_id == 'R008':
        text += '第61—72项新增原生提供方控制代理：实际Windows DPAPI/ACL、进程强制终止后的OS锁与原命令恢复，以及真实HTTP/临时PG丢登记和心跳回执验证。另以当前编译出的Windows二进制独立运行init、status和未登记heartbeat三个命令，跨进程身份不变且未登记心跳被拒绝，所有命令进程已退出，见[实际CLI记录](R008-native-node-cli.json)。Linux Secret Service只经过线协议夹具与交叉编译，未实测真实Linux钥库；这里的控制状态加密不是客户内容端对端加密。\n\n'
    attempts=sorted(EVIDENCE.glob(round_id+'-native-summary-attempt*.json'))
    for path in attempts:
        old=json.loads(path.read_text(encoding='utf-8'))
        text+=f'- 历次验证记录：[{path.name}]({path.name})；测试退出{old["test_exit_code"]}；临时实例停止={old["temporary_instance_stopped"]}。\n'
    (EVIDENCE / f'{round_id}-验证报告.md').write_text(text.rstrip() + '\n', encoding='utf-8')
    print(json.dumps({'round': round_id, 'passed': len(cases), 'scope': 'CONTROL_SERVICES_ONLY'}, ensure_ascii=False))


if __name__ == '__main__':
    main()
