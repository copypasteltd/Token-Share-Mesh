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


def main() -> None:
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--round',default='R003',choices=['R003'])
    round_id=parser.parse_args().round
    cases=R003_CASES
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
    if set(results) != expected or any(event['Action'] != 'pass' for event in results.values()):
        raise ValueError(f'本轮{len(cases)}项实际用例必须齐全且无跳过/失败')
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
        'limitations': ['No real OAuth, HTTP session/CSRF or seller/device integration', 'No GPU, Android, real payment, crypto interoperability, independent review or whole-product acceptance', 'Linux CLI cross-compiled only, no Linux runtime validation', 'Not a crash or power-loss durability test; no Go race-detector claim'],
    }
    (EVIDENCE / f'{round_id}-验证报告.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    text = f'# {round_id} 原生源码验证报告\n\n仅控制应用服务及既有基础通过：{len(cases)}项用例通过，实际用例无失败/跳过；临时原生数据库已停止。完整平台80项主验收仍为 NOT_EXECUTED。\n\n'
    text += '| 用例编号 | 验证内容 | 结果 |\n| --- | --- | --- |\n'
    for code, _, _, description in cases:
        text += f'| {code} | {description} | PASSED |\n'
    text += '\n原生 PostgreSQL 实例：`' + summary['run_id'] + '`；关闭方式：`' + summary['shutdown_mode'] + '`。实例与工具均在项目 runtime，无本地虚拟化。\n\n'
    text += 'Go 依赖校验、格式检查、vet及 Windows/Linux amd64 CLI 构建均通过；Linux仅交叉编译。依赖版本/许可清单只覆盖当前Go模块，不是全平台SBOM。\n\n'
    text += f'- [Go/数据库实际事件]({round_id}-native-go-tests.jsonl)\n- [实例退出与范围]({round_id}-native-summary.json)\n- [构建、源码摘要与依赖校验]({round_id}-native-source-checks.json)\n- [Go依赖与许可证](../../licenses/go-dependencies.json)\n\n'
    text += '未覆盖真实登录、HTTP/CSRF、GPU、Android、资金渠道、密码互通、独立评审、性能和完整产品主验收；不声称Go race detector或断电恢复通过。测试资格均为显式写入临时数据库的合成夹具，不能作为真实开售证据。\n\n'
    attempts=sorted(EVIDENCE.glob(round_id+'-native-summary-attempt*.json'))
    for path in attempts:
        old=json.loads(path.read_text(encoding='utf-8'))
        text+=f'- 历次失败记录：[{path.name}]({path.name})；测试退出{old["test_exit_code"]}；临时实例停止={old["temporary_instance_stopped"]}。\n'
    (EVIDENCE / f'{round_id}-验证报告.md').write_text(text, encoding='utf-8')
    print(json.dumps({'round': round_id, 'passed': len(cases), 'scope': 'CONTROL_SERVICES_ONLY'}, ensure_ascii=False))


if __name__ == '__main__':
    main()
