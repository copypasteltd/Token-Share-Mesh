"""RT source/document authoring only; no compiler/test/validator/DB/runtime."""
from collections import Counter
from copy import deepcopy
import hashlib
import json
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[2]
ANNEX = 'docs/技术开发/附录-原退款持久事务与宿主恢复源码.md'
TOOL = 'dev/tools/整理R026退款事务源码.py'
MIGRATION = '018_original_refund_context.sql'
TESTS = ['dev/src/internal/payments/archive_verify_test.go',
         'dev/src/tests/integration/original_refund_test.go',
         'dev/src/tests/integration/original_refund_host_test.go']
REFS = [ANNEX, TOOL, 'dev/src/internal/payments/archive_verify.go',
        'dev/src/internal/payments/refund_service.go', 'dev/src/internal/payments/refund_authority.go',
        'dev/src/internal/payments/refund_store.go', 'dev/src/internal/payments/refund_recovery.go',
        'dev/src/internal/paymenthost/config.go', 'dev/src/internal/paymenthost/host.go',
        'dev/src/tests/integration/core_test.go', 'scripts/技术开发数据.py',
        'dev/planning/编号登记-v1.0.json',
        'docs/技术开发/database/migrations/'+MIGRATION,
        'dev/src/internal/persistence/migrations/'+MIGRATION,
        'dev/src/internal/persistence/migrations/source-v18.json'] + TESTS
ITEMS = [
    ('086', '独立原退款审批及原实付RSA重验源码', 'RT001—014：当前独立审核/唯一决策/原未消费价款及执行预算阻断/解密后原RSA验签；完整含费与售后冲销待'),
    ('087', '原退款持久意图及累计预留源码', 'RT015—018：原锁序、原scope/intent/route/decision、REFUND/QUERY/job/outbox/幂等和提交前UNKNOWN同事务'),
    ('088', '原退款专用工作任务及恢复源码', 'RT019—024：跨退款3秒/原号10秒/20秒租约/128次认领/双代次/迟到事实及原任务ACK；无未知重提'),
    ('089', '前向018与原生退款宿主接线源码', 'RT025—029：DATA076/077不可变原意图/追加回执/同事务成功和预算守恒，受信任根可选配置/实际join；018未执行'),
    ('090', 'RT测试定义与双语文档全待办同步', 'RT030—040：新增测试源码未编译/运行，103规划/124 schema/77表/55控制HTTP；全部56/222保持开放'),
]
TASKS = {'DEV-01', 'DEV-02', 'DEV-03', 'DEV-13', 'DEV-14', 'DEV-15', 'DEV-16',
         'DEV-51', 'DEV-52', 'DEV-53', 'DEV-54', 'DEV-56'}
STEPS = {'DEV-01.01', 'DEV-02.01', 'DEV-03.02', 'DEV-03.03', 'DEV-13.04',
         'DEV-14.01', 'DEV-14.02', 'DEV-14.03', 'DEV-14.04', 'DEV-15.03', 'DEV-15.04',
         'DEV-16.01', 'DEV-16.02', 'DEV-51.01', 'DEV-51.02', 'DEV-51.03', 'DEV-51.04',
         'DEV-52.01', 'DEV-52.02', 'DEV-52.04', 'DEV-53.01', 'DEV-53.02',
         'DEV-54.01', 'DEV-54.02', 'DEV-56.01'}
DETAIL = ('RT补入原退款服务/独立当前路线和UNUSED_BALANCE_1审批、原实付解密后RSA重验、'
          '未停止/未清理/未核实预算及已消费价款检查、同事务原累计预留/不可变意图/REFUND/QUERY/job/outbox/幂等、'
          '提交前UNKNOWN/原付款3秒水位/原号10秒查询/20秒租约/128次认领/双代次及迟到签名事实、'
          '原生受信任根可选退款配置和专用宿主循环源码。新增018与DATA076/077，仅源码未迁移；'
          '当前103规划/124 schema/77平台表/55控制HTTP。公开退款API/审批工作台/双端售后、'
          '含费/已赚取价款冲销/佣金/续租/到账对账/历史修复、真实原交易资格和CK付款SDK/GF031等完整链路仍待。'
          '全56任务222子项开放；先完整生产代码/实际依赖/测试源码，再集中编译测试。本次不是开发轮完成。')


def load(name):
    return json.loads((ROOT/name).read_text(encoding='utf-8'))


def write(name, text):
    path = ROOT/name
    prior = path.read_bytes() if path.exists() else b''
    ending = '\r\n' if b'\r\n' in prior else '\n'
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(text.replace('\r\n', '\n').replace('\n', ending).encode('utf-8'))


def dump(name, value):
    write(name, json.dumps(value, ensure_ascii=False, indent=2)+'\n')


def section(name, marker, content):
    path = ROOT/name
    body = path.read_text(encoding='utf-8') if path.exists() else ''
    if marker in body:
        start = body.index(marker)
        end = body.find('\n## ', start+len(marker))
        body = body[:start].rstrip()+('\n\n'+body[end+1:] if end >= 0 else '')
    write(name, body.rstrip()+'\n\n'+marker+'\n\n'+content.strip()+'\n')


def register_source():
    # Forward source registration, not migration/verification. Never rewrite any
    # published 001-017/source metadata, base DDL or registered native SQL.
    source = 'docs/技术开发/database/migrations/'+MIGRATION
    target = 'dev/src/internal/persistence/migrations/'+MIGRATION
    raw = (ROOT/source).read_bytes()
    if (ROOT/target).exists() and (ROOT/target).read_bytes() != raw:
        raise ValueError('Preserve published forward SQL bytes; append the next version')
    if not (ROOT/target).exists():
        (ROOT/target).write_bytes(raw)
    digest = hashlib.sha256(raw).hexdigest()
    metadata = {'source': source, 'source_sha256': digest, 'migration': MIGRATION,
                'migration_sha256': digest,
                'policy': '018 original refund intent/signed query/archive/budget obligations; SOURCE_ONLY, not executed or qualified'}
    name = 'dev/src/internal/persistence/migrations/source-v18.json'
    if not (ROOT/name).exists():
        dump(name, metadata)
    registry = load('dev/planning/编号登记-v1.0.json')
    for table, number in [('refund_machine_contexts', 'DATA-076'), ('refund_machine_receipts', 'DATA-077')]:
        if registry['tables'].get(table, number) != number:
            raise ValueError('Preserve explicit DATA identities')
        registry['tables'][table] = number
    dump('dev/planning/编号登记-v1.0.json', registry)
    name = 'dev/src/tests/integration/core_test.go'
    body = (ROOT/name).read_text(encoding='utf-8')
    write(name, body.replace('tables != 75 {', 'tables != 77 {'))
    # Technical source additions describe the new source-only branch. Existing
    # overall requirements/qualification/fee gates are retained verbatim.
    name = 'scripts/技术开发数据.py'
    body = (ROOT/name).read_text(encoding='utf-8')
    body = body.replace('payment_machine_recoveries order_fundings"',
                        'payment_machine_recoveries order_fundings refund_machine_contexts refund_machine_receipts"')
    body = body.replace('OriginalFundingAdmission CurrentFundingGuard"',
                        'OriginalFundingAdmission CurrentFundingGuard RefundService OriginalRefundRecovery OriginalPaidArchiveVerifier"')
    if "'original_refund_transaction_source':" not in body:
        addition = ("      'original_refund_transaction_source':{'status':'SOURCE_ONLY_UNVERIFIED',\n"
            f"        'annex':'{ANNEX}',\n"
            "        'profile':'P11_ALIPAY_ORIGINAL_REFUND_1','policy':'UNUSED_BALANCE_1',\n"
            "        'tables':{'refund_machine_contexts':'DATA-076','refund_machine_receipts':'DATA-077'},\n"
            "        'forward_source':'018_original_refund_context.sql','migration_executed':False,\n"
            "        'original_paid_archive_rsa_reverification_source_written':True,\n"
            "        'durable_original_refund_and_native_host_source_wired':True,\n"
            "        'public_refund_http_wired':False,'actual_original_refund_qualification':False,\n"
            "        'full_fee_reversal_renewal_payout_reconciliation_complete':False,\n"
            "        'compiled_or_executed':False},\n")
        body = body.replace("      'unproven_capabilities':", addition+"      'unproven_capabilities':", 1)
    write(name, body)
    return metadata


def source_inventory():
    tests = []
    for source in TESTS:
        for name in re.findall(r'^func (Test\w+)\(', (ROOT/source).read_text(encoding='utf-8'), re.M):
            tests.append({'id': f'RT-T{len(tests)+1:03d}', 'name': name, 'source': source, 'status': 'SOURCE_ONLY_NOT_EXECUTED'})
    registry = load('dev/planning/编号登记-v1.0.json')
    api = load('docs/白皮书/contracts/openapi-control-v3.0.json')
    routes = load('dev/src/api/internal/http/routes.json')
    return tests, {'planned_operations': len(registry['operations']), 'schemas': len(api['components']['schemas']),
                   'platform_tables': len(registry['tables']), 'control_http_operations': len(routes['operations'])}


def main():
    migration = register_source()
    tests, counts = source_inventory()
    plan, stage, status = [load(name) for name in ['dev/progress/code-first-plan.json',
        'dev/progress/R026-code-stage.json', 'dev/progress/implementation-status.json']]
    integration = {'status': 'SOURCE_ONLY_CONDITIONAL_UNVERIFIED', 'requirements': 'RT-001—040', 'annex': ANNEX,
        'original_paid_archive_offline_rsa_reverification_source_written': True,
        'independent_current_unused_balance_decision_source_wired': True,
        'same_decision_unique_original_refund_source_written': True,
        'unresolved_work_cleanup_and_budget_original_capacity_source_wired': True,
        'durable_original_refund_context_jobs_outbox_idempotency_source_wired': True,
        'three_second_submit_ten_second_query_persistent_boundaries_source_written': True,
        'dual_generation_late_financial_fact_source_written': True,
        'native_optional_refund_registry_and_worker_host_source_wired': True,
        'public_refund_http_and_aftersale_ui_source_wired': False,
        'full_fee_earned_reversal_commission_renewal_payout_reconciliation_complete': False,
        'definitive_failure_retry_review_resume_and_historical_import_complete': False,
        'actual_original_transaction_personal_seller_refund_qualification': False,
        'legacy_positive_fixtures_rewritten': False, 'approved_paylauncher_sdk_chain_complete': False,
        'compiled_or_executed': False, 'migration_executed': False,
        'new_screenshots_or_recordings_captured': False, 'full_development_round_complete': False,
        'forward_migration_source': migration, 'test_definition_count': len(tests), **counts,
        'remaining': ['RT031：公开退款申请/获准执行/查询与原合同映射，不能把买家申请改成自审批',
            'RT032：独立审核/撤销/双端售后工作台，不以本地配置自批',
            'RT033：含费/已赚取价款冲销/佣金/续租/到账对账及可信计量接受账务',
            'RT034：权威失败原号重试、独立复核恢复、历史原金融记录重验签导入',
            'RT035—037：真实原交易资格、CK PayLauncher/SDK及GF031旧正向夹具和完整端侧链',
            'RT038—040：全部56/222生产源码/实际依赖/测试源码后统一测试、真实组合验证和媒体；DEV55延后、增强无证明关闭']}
    for record in (plan, stage):
        record['original_refund_transaction_source_integration'] = deepcopy(integration)
        for key in ['machine_pay_source_integration', 'machine_payment_transaction_source_integration',
                    'machine_payment_host_source_integration', 'original_order_funding_source_integration',
                    'original_checkout_source_integration', 'original_refund_source_integration']:
            record[key].update({'current_original_refund_transaction_source_annex': ANNEX,
                'current_original_refund_native_host_transaction_source_wired': True,
                'actual_qualified_full_refund_chain_complete': False})
    for task in plan['tasks']:
        if task['id'] in TASKS:
            task['original_refund_transaction_source_limit'] = DETAIL
        for step in task['steps']:
            if step['id'] in STEPS:
                step['implementation_refs'] = sorted(set(step.get('implementation_refs', [])+REFS))
    for task in status['tasks']:
        if task['id'] in TASKS:
            task['original_refund_transaction_source_limit'] = DETAIL
            task['current_source_refs'] = sorted(set(task.get('current_source_refs', [])+REFS))
            task['current_validation_status'] = 'NOT_EXECUTED'
            if task['id'] == 'DEV-14':
                task['remaining_detail'] = DETAIL
    status['activity_source'] = '2026-11-06 CODE_FIRST：RT-001—030 / IMP086—090；'+DETAIL
    for number, title, state in ITEMS:
        entry = {'id': 'IMP-R026-'+number, 'title': title, 'status': state}
        stage['implemented_items'] = [old for old in stage['implemented_items'] if old['id'] != entry['id']]+[entry]
    stage['complete'] = False
    stage['validation_started_after_user_policy'] = False
    stage['source_files'] = sorted(set(stage.get('source_files', [])+REFS))
    stage['tasks'] = deepcopy(status['tasks'])
    stage['platform_original_refund_migration_source'] = migration
    stage['original_refund_transaction_test_definitions'] = {'tests': tests, 'ids': [t['id'] for t in tests], 'status': 'SOURCE_ONLY_NOT_EXECUTED'}
    for name, value in [('dev/progress/code-first-plan.json', plan), ('dev/progress/R026-code-stage.json', stage),
                        ('dev/progress/implementation-status.json', status)]:
        dump(name, value)
    progress(plan, stage, status, tests, counts)
    documentation(tests, counts)
    print(json.dumps({'source_document_generation': 'RT001–040 / IMP086–090', 'unexecuted_test_definitions': len(tests),
        **counts, 'forward_source': MIGRATION, 'compiler_test_validator_migration_runtime': 'NOT_RUN',
        'all_tasks_open': len(plan['tasks']), 'all_steps_open': sum(len(t['steps']) for t in plan['tasks'])}, ensure_ascii=False))


def progress(plan, stage, status, tests, counts):
    totals = Counter(t['implementation_status'] for t in status['tasks'])
    lines = ['# R026 全量代码阶段进展', '',
        '**进行中，SOURCE_ONLY / 未验证。** 先全部生产代码、实际依赖接线和测试源码，再集中验证。新增086—090只是源码保存点，不是整轮完成；旧记录按当时范围阅读。', '',
        '| 编号 | 已写内容 | 实际状态 |', '|---|---|---|']
    lines += [f"| {i['id']} | {i['title']} | {i['status']} |" for i in stage['implemented_items']]
    lines += ['', f'新增[RT-001—040](../../{ANNEX})与{len(tests)}项未执行测试定义。此前RN67/CE31/MW44/CH29/WS23/MS27/MG37/MP49/PT16/PH12/GF14/CK26/RF32亦未编译或运行。', '',
        f"当前{counts['control_http_operations']}控制HTTP/{counts['planned_operations']}规划/{counts['schemas']} schema/{counts['platform_tables']}平台表。新增018仅源码登记，没有运行迁移；001—018及全部原端侧DDL/source.json登记后原字节冻结，后续只追加019+。", '',
        DETAIL, '', '当前状态：'+'；'.join(f'{k} {v}' for k, v in sorted(totals.items()))+'；全部222子项未验收。', '',
        '| 编号 | 全部任务 | 当前状态 | 全部仍待收口子项 |', '|---|---|---|---|']
    current = {t['id']: t for t in status['tasks']}
    for task in plan['tasks']:
        t = current[task['id']]
        lines.append(f"| {task['id']} | {task['title']} | {t['implementation_status']} | {'、'.join(t['remaining_step_ids'])} |")
    write('dev/progress/R026-全量代码阶段进展.md', '\n'.join(lines)+'\n')
    lines = ['# 全量代码优先实施清单', '', DETAIL, '',
        '先全部生产代码/实际依赖接线/测试源码，再集中验证。代码与验收分开，全部222子项未验收。', '',
        '| 子项 | 任务 | 未收口需求 | 代码状态 | 验收 |', '|---|---|---|---|---|']
    for task in plan['tasks']:
        for step in task['steps']:
            lines.append(f"| {step['id']} | {task['title']} | {step['code_requirement']} | {step['code_status']} | {step['acceptance_status']} |")
    write('dev/全量代码优先实施清单.md', '\n'.join(lines)+'\n')


def documentation(tests, counts):
    section(ANNEX, '## RT 测试源码定义索引', '| 编号 | 定义 | 源码 | 状态 |\n|---|---|---|---|\n'+'\n'.join(
        f"| {t['id']} | {t['name']} | {t['source']} | 未编译/未运行 |" for t in tests))
    zh = (f'R026新增[RT-001—040]({ANNEX})：原退款服务/独立当前未消费价款审批、原实付解密后RSA重验、'
        '执行清理/预算/累计守恒、同事务原退款意图/渠道/job/outbox/幂等和提交前UNKNOWN、原号恢复/双代次及原生宿主可选配置源码。'
        f"新增018及DATA076/077，当前{counts['planned_operations']}规划/{counts['schemas']} schema/{counts['platform_tables']}平台表/{counts['control_http_operations']}控制HTTP；{len(tests)}项新测试定义未编译/运行，018未迁移。"
        '当前未消费价款分支不代替完整含费/已赚取价款冲销/佣金/续租/到账对账；公开退款API、审批工作台/双端售后、独立复核重试、真实原交易资格及CK/GF等仍待。'
        '全部56任务222子项开放；先完整生产源码/实际接线/测试源码，再集中验证和重录媒体。本次不是开发轮完成。')
    en = (f'R026 adds [RT-001–040]({ANNEX}): original-refund service/current independent unused-consideration decisions, offline RSA reverification after paid-archive decryption, '
        'work/cleanup/budget/cumulative guards, atomic refund intents/channel requests/jobs/outbox/idempotency, pre-dispatch UNKNOWN, original-number recovery/dual generations and optional native host composition. '
        f"New 018 and DATA076/077 keep counts at {counts['planned_operations']} planned operations/{counts['schemas']} schemas/{counts['platform_tables']} platform tables/{counts['control_http_operations']} control HTTP operations; {len(tests)} new test definitions are uncompiled/unexecuted and 018 is unapplied. "
        'The unused-consideration branch does not complete fees/earned-service reversals/commission/renewal/payout/reconciliation. Public refund HTTP, reviewer/aftersale UI, reviewed retries, actual original-transaction qualification and CK/GF follow-ups remain open. '
        'All 56 tasks/222 steps stay open. Complete production source, actual wiring and test source precede batch validation and fresh media; this is not a completed development round.')
    marker = '## R026 RT-001—040 原退款事务与宿主恢复 / Original refund transactions and native host recovery source'
    for name, content in [('dev/repository/README.zh-CN.md.in', zh), ('dev/repository/README.en.md.in', en)]:
        section(name, marker, content)
        body = (ROOT/name).read_text(encoding='utf-8')
        body = re.sub(r'\n<!-- R026-[A-Z-]+-CURRENT -->.*?<!-- /R026-[A-Z-]+-CURRENT -->\n', '\n', body, flags=re.S)
        body = body.replace('以上 RF 说明', '以上 RT 说明').replace('The RF notice above', 'The RT notice above')
        body = re.sub(r'\n{3,}', '\n\n', body)
        title, rest = body.split('\n', 1)
        write(name, title+'\n\n<!-- R026-RT-CURRENT -->\n'+content+'\n<!-- /R026-RT-CURRENT -->\n'+rest)
    for name, content in [('CHANGELOG.zh-CN.md', zh), ('CHANGELOG.md', en)]:
        body = (ROOT/name).read_text(encoding='utf-8')
        body = re.sub(r'(?m)^- R026(?:新增| adds )\[RT-001[—–]040\].*\n', '', body)
        title, rest = body.split('\n', 1)
        write(name, title+'\n\n- '+content+'\n'+rest)
    for name in ['dev/repository/USAGE.md', 'dev/repository/SYSTEM_DESIGN.md']:
        section(name, marker, zh.replace('(docs/', '(../../docs/')+'\n\n'+en.replace('(docs/', '(../../docs/'))
    for name, prefix in [('dev/src/internal/payments/README.md', '../../../../'),
        ('dev/src/internal/paymenthost/README.md', '../../../../'), ('dev/progress/README.md', '../../')]:
        section(name, marker, zh.replace('(docs/', '('+prefix+'docs/'))
    for name in ['docs/技术开发/00-技术文档导航与实施基线.md', 'docs/技术开发/13-支付退款账务与渠道结算.md',
        'docs/技术开发/附录-MachinePay原生宿主与本人支付接口源码.md',
        'docs/技术开发/附录-原订单资金唯一分配与当前准入源码.md',
        'docs/技术开发/附录-原退款协议与渠道恢复源码.md',
        'docs/技术开发/附录-本轮范围与端侧选型修订.md']:
        section(name, marker, zh.replace('(docs/技术开发/', '('))
    for name in ['AGENTS.md', 'dev/AGENTS.md']:
        section(name, marker, zh.replace('(docs/', '(../docs/' if name.startswith('dev/') else '(docs/')+
            '\n\n当前唯一增量整理器为'+TOOL+'；禁止重跑旧RF/CK/GF/PH/PT/MP/MG/MS/WS/CH/CE/MW/RN整理器覆盖当前RT状态。'
            '001—018平台双份SQL/source.json及原端侧DDL/source.json登记后原字节冻结，只追加019+。'
            '当前不编译/测试/核验/迁移或启动DB、内核、模型、设备、浏览器；全量生产代码/实际依赖/测试源码后集中验证。'
            '无本地虚拟化，DEV55延后，DEV32无真实证明关闭；浏览器用后关闭不用页。')
    profile = load('dev/src/internal/payments/channel-profile.json')
    profile.update({'original_refund_transaction_source_annex': ANNEX,
        'original_refund_platform_transaction_and_native_host_source_wired': True,
        'public_refund_http_source_wired': False, 'original_refund_actual_qualification': False,
        'compiled_or_executed': False})
    dump('dev/src/internal/payments/channel-profile.json', profile)
    profile = load('dev/src/internal/payments/refund-profile.json')
    profile.update({'status': 'SOURCE_ONLY_CONDITIONAL_NATIVE_COMPOSITION_UNVERIFIED',
        'transaction_source_annex': ANNEX, 'platform_durable_refund_transaction_wired': True,
        'payment_worker_and_host_refund_source_wired': True, 'public_refund_http_source_wired': False,
        'source_policy': 'UNUSED_BALANCE_1_INDEPENDENT_CURRENT_REVIEW',
        'forward_migration_source': MIGRATION, 'migration_executed': False,
        'full_fee_earned_reversal_renewal_payout_chain_complete': False,
        'maximum_claims_including_original_submit': 128, 'worker_lease_seconds': 20,
        'compiled_or_executed': False})
    dump('dev/src/internal/payments/refund-profile.json', profile)


if __name__ == '__main__':
    main()
