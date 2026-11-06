"""Source/document generation only; no compiler/test/validator/migration/IO."""
from collections import Counter
from copy import deepcopy
import hashlib
import json
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[2]
ANNEX = 'docs/技术开发/附录-原订单资金唯一分配与当前准入源码.md'
TOOL = 'dev/tools/整理R026资金准入源码.py'
MIGRATION = '017_original_order_funding.sql'
TESTS = ['dev/src/tests/integration/original_funding_test.go']
REFS = [ANNEX, TOOL, 'dev/src/internal/fundguard/README.md',
        'dev/src/internal/fundguard/current.go', 'dev/src/internal/payments/funding.go',
        'dev/src/internal/payments/types.go', 'dev/src/internal/payments/machine_store.go',
        'dev/src/internal/payments/machine_recovery.go', 'dev/src/internal/paymenthost/host.go',
        'dev/src/internal/routecontract/service.go', 'dev/src/internal/rental/store.go',
        'dev/src/internal/identity/keys.go', 'dev/src/tests/integration/core_test.go',
        'dev/planning/编号登记-v1.0.json', 'scripts/技术开发数据.py',
        'docs/技术开发/database/migrations/'+MIGRATION,
        'dev/src/internal/persistence/migrations/'+MIGRATION,
        'dev/src/internal/persistence/migrations/source-v17.json'] + TESTS
ITEMS = [
    ('071', '原订单资金唯一分配与前向017源码', 'GF-001—010：原双方/合同/付款/receipt/scope/四证、唯一不可变DATA075/独立期限；未迁移'),
    ('072', '原支付后独立资金准入与原子READY源码', 'GF-011—021：同事务元任务/注册能力过滤/原锁序/写前准入/分配READY事件任务原子提交；不重收款或启动执行'),
    ('073', '路由租赁与密钥当前资金接线源码', 'GF-022—026/028—029：共同CurrentTx/当前主体四证gate退款/期限收窄，三消费者去除旧付款标记准入'),
    ('074', '原退款父锁与历史义务保护源码', 'GF-023/027：先父锁后新快照、退款写入串行和不可删/改原范围/降级成功；不是完整退款渠道'),
    ('075', 'GF编号测试源码和全量文档', 'GF-030—040：付款分离/幂等/撤权/并发/消费者测试源码、旧夹具升级和全56/222；未编译或运行'),
]
TASKS = {'DEV-02', 'DEV-03', 'DEV-11', 'DEV-13', 'DEV-14', 'DEV-15', 'DEV-16',
         'DEV-33', 'DEV-34', 'DEV-35', 'DEV-36', 'DEV-51', 'DEV-52', 'DEV-53', 'DEV-54', 'DEV-56'}
DETAIL = ('GF补入原订单唯一资金分配DATA075/前向017、独立原capability元任务/原子READY及outbox、'
          '原付款后当前主体/四证/gate/退款/期限复核、原父锁新快照和路由/小时租赁/密钥三消费者源码。'
          '当前75表/98规划/50控制HTTP。源码未编译/测试，017未迁移。只支持显式原零费用收款档；'
          '旧直接READY/SETTLED正向夹具升级、标准SDK单Proof/A402/实际PayLauncher、完整原退款到账佣金对账、'
          '原付费续租/可信M token或小时水位/接受与追加账、真实资格和全部组合仍待。'
          '全部56任务222子项未验收；完整生产代码/实际依赖接线/测试源码后集中验证；本次不是整轮完成。')


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


def source_registration():
    # Register a NEW forward source only. No old SQL/source metadata is edited
    # and no database connection or migration runner is imported or invoked.
    source = 'docs/技术开发/database/migrations/'+MIGRATION
    destination = 'dev/src/internal/persistence/migrations/'+MIGRATION
    raw = (ROOT/source).read_bytes()
    if (ROOT/destination).exists() and (ROOT/destination).read_bytes() != raw:
        raise ValueError('Preserve published forward SQL bytes; append a new version')
    if not (ROOT/destination).exists():
        (ROOT/destination).write_bytes(raw)
    digest = hashlib.sha256(raw).hexdigest()
    metadata = {'source': source, 'source_sha256': digest, 'migration': MIGRATION,
                'migration_sha256': digest,
                'policy': '017 original order allocation/current funding guard; SOURCE_ONLY, unexecuted, not a wallet, charge, ledger or execution grant'}
    target = 'dev/src/internal/persistence/migrations/source-v17.json'
    if not (ROOT/target).exists():
        dump(target, metadata)
    registry = load('dev/planning/编号登记-v1.0.json')
    previous = registry['tables'].get('order_fundings')
    if previous is not None and previous != 'DATA-075':
        raise ValueError('Preserve explicit DATA identities')
    registry['tables']['order_fundings'] = 'DATA-075'
    dump('dev/planning/编号登记-v1.0.json', registry)
    core = 'dev/src/tests/integration/core_test.go'
    body = (ROOT/core).read_text(encoding='utf-8')
    write(core, body.replace('tables != 74 {', 'tables != 75 {'))
    return metadata


def main():
    migration = source_registration()
    plan, stage, status = [load(name) for name in ['dev/progress/code-first-plan.json',
        'dev/progress/R026-code-stage.json', 'dev/progress/implementation-status.json']]
    tests = []
    for source in TESTS:
        # Text inventory only, not a Go parser/compiler or test discovery run.
        for name in re.findall(r'^func (Test\w+)\(', (ROOT/source).read_text(encoding='utf-8'), re.M):
            tests.append({'id': f'GF-T{len(tests)+1:03d}', 'name': name, 'source': source,
                          'status': 'SOURCE_ONLY_NOT_EXECUTED'})
    integration = {
        'status': 'SOURCE_ONLY_UNVERIFIED', 'requirements': 'GF-001—040', 'annex': ANNEX,
        'unique_original_order_payment_allocation_source_wired': True,
        'verified_receipt_metadata_job_ready_outbox_atomic_source_wired': True,
        'current_parties_approval_refund_deadline_guard_source_wired': True,
        'route_rental_scoped_key_consumers_source_wired': True,
        'refund_parent_snapshot_serialization_source_written': True,
        'legacy_direct_settled_ready_positive_fixtures_rewritten': False,
        'standard_sdk_proof_a402_paylauncher_complete': False,
        'full_fee_refund_payout_statement_renewal_usage_acceptance_ledger_complete': False,
        'real_financial_or_device_qualification': False,
        'compiled_or_executed': False, 'migration_executed': False,
        'source_control_operations': 98, 'source_catalog_tables': 75, 'control_http_operations': 50,
        'migration': migration, 'test_definition_count': len(tests),
        'full_development_round_complete': False,
        'remaining': [
            'GF-031：旧直接READY/SETTLED正向路由/执行/租赁夹具改为资格化原源码链，禁止生产旁路和降断言',
            'GF-032—033：标准SDK原Proof/A402和真实PayLauncher/原失败恢复',
            'GF-034：完整获准费用佣金/渠道退款/到账/对账单和异常处理',
            'GF-035—036：原RENTAL_RENEWAL付款/唯一用途、可信token或小时水位/接受/追加账',
            'GF-037—038：真实个人卖家/服务/渠道/设备、秘密ACL和跨进程崩溃/撤销实证',
            'GF-039—040：全56/222生产源码/实际接线/全部测试源码后集中验证；DEV55延后/增强无证明关闭']}
    for record in (plan, stage):
        record['original_order_funding_source_integration'] = deepcopy(integration)
        for key in ['machine_pay_source_integration', 'machine_payment_transaction_source_integration',
                    'machine_payment_host_source_integration', 'rental_source_integration']:
            record[key].update({'current_original_funding_source_annex': ANNEX,
                'original_allocation_and_current_consumer_guard_source_written': True,
                'full_financial_business_admission_and_acceptance_complete': False})
    for task in plan['tasks']:
        if task['id'] in TASKS:
            task['code_status'] = 'IN_PROGRESS'
            for step in task['steps']:
                step['code_status'] = 'IN_PROGRESS'
                step['implementation_refs'] = sorted(set(step.get('implementation_refs', [])+REFS))
    for task in status['tasks']:
        if task['id'] in TASKS:
            task['implementation_status'] = 'IN_PROGRESS'
            task['original_funds_source_limit'] = DETAIL
            task['current_validation_status'] = 'NOT_EXECUTED'
            if task['id'] == 'DEV-15':
                task['remaining_detail'] = DETAIL
    status['activity_source'] = '2026-11-06 CODE_FIRST：GF-001—030 / IMP071—075；'+DETAIL
    for number, title, state in ITEMS:
        entry = {'id': 'IMP-R026-'+number, 'title': title, 'status': state}
        stage['implemented_items'] = [old for old in stage['implemented_items'] if old['id'] != entry['id']]+[entry]
    stage['source_files'] = sorted(set(stage.get('source_files', [])+REFS))
    stage['tasks'] = deepcopy(status['tasks'])
    stage['original_funds_test_definitions'] = {'tests': tests, 'ids': [t['id'] for t in tests],
                                              'status': 'SOURCE_ONLY_NOT_EXECUTED'}
    for name, value in [('dev/progress/code-first-plan.json', plan), ('dev/progress/R026-code-stage.json', stage),
                        ('dev/progress/implementation-status.json', status)]:
        dump(name, value)
    progress(plan, stage, status, tests)
    documentation(tests)
    print(f'Source/doc generation only: IMP071–075 / GF001–040 / {len(tests)} unexecuted tests / 50 HTTP / 98 planned / 75 tables. No compiler/test/validator/migration/runtime; all 56/222 open.')


def progress(plan, stage, status, tests):
    counts = Counter(t['implementation_status'] for t in status['tasks'])
    lines = ['# R026 全量代码阶段进展', '', '**进行中，SOURCE_ONLY / 未验证。** 全部生产代码、实际依赖接线和测试源码后集中验证。新增071—075仅源码保存点，不是整轮完成；历史记录按当时范围阅读。', '',
             '| 编号 | 已写内容 | 实际状态 |', '|---|---|---|']
    lines += [f"| {i['id']} | {i['title']} | {i['status']} |" for i in stage['implemented_items']]
    lines += ['', f'新增[GF-001—040](../../{ANNEX})与{len(tests)}项未执行测试定义。原RN67/CE31/MW44/CH29/WS23/MS27/MG37/MP49/PT16/PH12也未编译/运行。当前50控制HTTP/98规划/75平台表，新增017/DATA075仅源码、未迁移；001—016和既有端侧DDL原字节保留。', '',
              '旧直接READY/SETTLED正向夹具仍待资格化改写，没有生产测试旁路。', '',
              '当前状态：'+'；'.join(f'{k} {v}' for k, v in sorted(counts.items()))+'；全部222子项未验收。', '',
              '| 编号 | 全部任务 | 当前状态 | 全部仍待收口子项 |', '|---|---|---|---|']
    current = {t['id']: t for t in status['tasks']}
    for task in plan['tasks']:
        t = current[task['id']]
        lines.append(f"| {task['id']} | {task['title']} | {t['implementation_status']} | {'、'.join(t['remaining_step_ids'])} |")
    write('dev/progress/R026-全量代码阶段进展.md', '\n'.join(lines)+'\n')
    lines = ['# 全量代码优先实施清单', '', DETAIL, '', '先全部生产代码/实际依赖接线/测试源码，再集中验证。代码与验收分开，全部222子项未验收。', '',
             '| 子项 | 任务 | 未收口需求 | 代码状态 | 验收 |', '|---|---|---|---|---|']
    for task in plan['tasks']:
        for step in task['steps']:
            lines.append(f"| {step['id']} | {task['title']} | {step['code_requirement']} | {step['code_status']} | {step['acceptance_status']} |")
    write('dev/全量代码优先实施清单.md', '\n'.join(lines)+'\n')


def documentation(tests):
    section(ANNEX, '## GF 测试源码定义索引', '| 编号 | 定义 | 源码 | 状态 |\n|---|---|---|---|\n'+'\n'.join(
        f"| {t['id']} | {t['name']} | {t['source']} | 未编译/未运行 |" for t in tests))
    zh = (f'R026新增[GF-001—040]({ANNEX})：原订单唯一资金分配DATA075/前向017、原验签后独立元任务和原子READY/outbox、当前主体四证/gate/退款/期限及路由/小时租赁/密钥三消费者源码。'
          f'当前50控制HTTP/98规划/75平台表；{len(tests)}项新增测试定义未编译/运行，017未迁移。当前仅显式零费用档；旧正向付款标记夹具升级、SDK/Proof/A402/实际PayLauncher、完整费用退款到账佣金对账/付费续租/可信计量接受账务和真实资格仍待。'
          '全部56任务222子项开放，完整生产代码/实际依赖接线/测试源码后集中验证；本次不是整轮完成。')
    en = (f'R026 adds [GF-001–040]({ANNEX}): unique original-order allocation DATA075/forward source 017, metadata-only admission after verified payment, atomic READY/outbox, current parties/four approvals/gate/refund/deadline guard and route/rental/scoped-key consumers. '
          f'Current source: 50 control HTTP operations/98 planned operations/75 platform tables; {len(tests)} new test definitions remain uncompiled/unexecuted, and 017 is unapplied. Only the explicit zero-fee profile is wired. Legacy positive flag fixtures, SDK/Proof/A402/PayLauncher, full qualified fees/refunds/payouts/statements/renewal/trusted metering/acceptance/ledger and real qualification remain open. '
          'All 56 tasks/222 steps are unaccepted. Complete all production source, actual dependency wiring and test source before batch validation; this is not a completed round.')
    marker = '## R026 GF-001—040 原订单资金分配与当前准入 / Original allocation and current admission source'
    for name, content in [('dev/repository/README.zh-CN.md.in', zh), ('dev/repository/README.en.md.in', en)]:
        section(name, marker, content)
        body = (ROOT/name).read_text(encoding='utf-8')
        body = re.sub(r'\n<!-- R026-[A-Z-]+-CURRENT -->.*?<!-- /R026-[A-Z-]+-CURRENT -->\n', '\n', body, flags=re.S)
        body = body.replace('以上 PH 说明', '以上 GF 说明').replace('The PH notice above', 'The GF notice above')
        body = re.sub(r'\n{3,}', '\n\n', body)
        title, rest = body.split('\n', 1)
        write(name, title+'\n\n<!-- R026-GF-CURRENT -->\n'+content+'\n<!-- /R026-GF-CURRENT -->\n'+rest)
    for name, content in [('CHANGELOG.zh-CN.md', zh), ('CHANGELOG.md', en)]:
        body = (ROOT/name).read_text(encoding='utf-8')
        body = re.sub(r'(?m)^- R026(?:新增| adds )\[GF-001[—–]040\].*\n', '', body)
        title, rest = body.split('\n', 1)
        write(name, title+'\n\n- '+content+'\n'+rest)
    for name in ['dev/repository/USAGE.md', 'dev/repository/SYSTEM_DESIGN.md']:
        section(name, marker, zh.replace('(docs/', '(../../docs/')+'\n\n'+en.replace('(docs/', '(../../docs/'))
    for name, prefix in [('dev/src/internal/payments/README.md', '../../../../'),
                         ('dev/src/internal/paymenthost/README.md', '../../../../'), ('dev/progress/README.md', '../../')]:
        section(name, marker, zh.replace('(docs/', '('+prefix+'docs/'))
    for name in ['docs/技术开发/00-技术文档导航与实施基线.md', 'docs/技术开发/13-支付退款账务与渠道结算.md',
                 'docs/技术开发/附录-MachinePay支付事务与原凭据恢复源码.md',
                 'docs/技术开发/附录-MachinePay原生宿主与本人支付接口源码.md',
                 'docs/技术开发/附录-本轮范围与端侧选型修订.md']:
        section(name, marker, zh.replace('(docs/技术开发/', '('))
    for name in ['AGENTS.md', 'dev/AGENTS.md']:
        section(name, marker, zh.replace('(docs/', '(../docs/' if name.startswith('dev/') else '(docs/')+
                '\n\n当前唯一增量整理器为'+TOOL+'；禁止重跑旧PH/PT/MP/MG/MS/WS/CH/CE/MW/RN整理器覆盖GF状态。001—017平台双份SQL及既有端侧DDL/source.json发布后原字节冻结，只追加前向018+。017只登记源码、未迁移；当前不编译/测试/核验/运行DB、内核、模型、设备或浏览器。全56/222源代码和测试源码后集中验证；DEV55延后/增强无证明关闭；禁止本地虚拟化，浏览器用后关闭不用页。')
    profile = load('dev/src/internal/payments/channel-profile.json')
    profile.update({'original_order_funding_source_annex': ANNEX,
                    'unique_allocation_and_current_consumer_guard_source_wired': True,
                    'full_financial_business_admission_complete': False,
                    'legacy_positive_fixtures_rewritten': False, 'compiled_or_executed': False})
    dump('dev/src/internal/payments/channel-profile.json', profile)


if __name__ == '__main__':
    main()
