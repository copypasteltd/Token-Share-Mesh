"""Generate source/docs inventories; never run builds, tests or validators."""
from collections import Counter
from copy import deepcopy
import hashlib
import json
from pathlib import Path
import re
import sys

ROOT = Path(__file__).resolve().parents[2]
ANNEX = 'docs/技术开发/附录-MachinePay支付事务与原凭据恢复源码.md'
TOOL = 'dev/tools/整理R026支付事务源码.py'
MIGRATION = '016_machine_payment_context.sql'
TEST_FILES = ['dev/src/internal/payments/receipt_test.go',
              'dev/src/tests/integration/machine_payment_test.go']
REFS = sorted([ANNEX, TOOL,
    'dev/src/internal/payments/machine.go', 'dev/src/internal/payments/machine_store.go',
    'dev/src/internal/payments/machine_recovery.go', 'dev/src/internal/payments/receipt.go',
    'dev/src/internal/payments/alipay.go', 'dev/src/internal/payments/types.go',
    'dev/src/internal/payments/README.md', 'dev/src/internal/payments/channel-profile.json',
    'dev/src/tests/integration/core_test.go', 'dev/src/internal/persistence/migrate.go',
    'dev/src/internal/persistence/migrations/'+MIGRATION,
    'dev/src/internal/persistence/migrations/source-v16.json',
    'docs/技术开发/database/migrations/'+MIGRATION,
    'scripts/技术开发数据.py', 'scripts/白皮书契约数据.py',
    'dev/planning/编号登记-v1.0.json']+TEST_FILES)
ITEMS = [
    ('061', '原个人卖家支付意图与提交后派发事务源码', 'PT-001—014：原主体/合同、独立审核四证/销售开关、零平台费档、原支付/PAY意图/上下文/outbox同事务，提交确认后一次precreate；未验证真实资格'),
    ('062', '完整签名回包及同事务密文归档源码', 'PT-015—023：原envelope持有/副本关闭、独立Receipt钥环与短期Proof钥分离、原范围追加receipt/payment/channel/outbox及已付单调；未迁移/运行'),
    ('063', '原凭据密文与持久恢复意图事务源码', 'PT-024—026：同一原Proof比较、最长30分钟、密文/QUERY意图/专用job/幂等一次提交及到期墓碑；不清金融义务'),
    ('064', '专用原凭据处理与实际调用持有源码', 'PT-027—032：原capability过滤、原锁序/generation/有界同Proof验付、迟到原事实保存/旧代次拒绝和实际关闭；正式宿主循环仍待'),
    ('065', 'PT编号、前向016和全量测试源码文档', 'PT-033—040：DATA072—074、原渠道参考格式、精确74表测试源、边界/SQL测试定义和双语全56/222待办；全部未编译/执行'),
]
STEPS = {
    'DEV-02': {'DEV-02.01', 'DEV-02.02', 'DEV-02.03', 'DEV-02.04'},
    'DEV-13': {'DEV-13.01', 'DEV-13.02', 'DEV-13.03', 'DEV-13.04'},
    'DEV-15': {'DEV-15.01', 'DEV-15.02', 'DEV-15.03', 'DEV-15.04'},
    'DEV-16': {'DEV-16.01', 'DEV-16.02'},
    'DEV-51': {'DEV-51.01', 'DEV-51.02', 'DEV-51.03'},
    'DEV-52': {'DEV-52.01', 'DEV-52.02', 'DEV-52.04'},
    'DEV-53': {'DEV-53.01', 'DEV-53.04'},
    'DEV-54': {'DEV-54.01', 'DEV-54.02'},
    'DEV-56': {'DEV-56.01', 'DEV-56.04'},
}
DETAIL = ('新增原个人seller支付意图/提交后一次派发、独立审核证据/feature gate、完整原签名回包独立钥环加密、'
          'receipt/payment/channel/outbox同事务、原Proof密文/QUERY/专用job及同凭据恢复处理源码；新增前向016与三表。'
          '全部未编译/迁移/运行。正式API/worker宿主注册/秘密轮换、本人HTTP/SDK/A402/PayLauncher、'
          '原资金唯一消费与当前退款/设备/主体/硬截止下订单READY/租赁推理、完整原退款/到账/佣金/对账单、'
          '付费续租/可信token或小时水位/接受/追加账和真实渠道资格仍待。全部56任务222子项未验收；'
          '完整生产代码、实际依赖接线和测试源码齐备后统一验证；本次仅源码检查点，不是开发轮完成。')


def load(name):
    return json.loads((ROOT/name).read_text(encoding='utf-8'))


def write(name, value):
    path = ROOT/name
    prior = path.read_bytes() if path.exists() else b''
    ending = '\r\n' if b'\r\n' in prior else '\n'
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(value.replace('\r\n', '\n').replace('\n', ending).encode('utf-8'))


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


def source_contracts():
    # Copy/register the new forward source; NEVER execute a migration or rewrite
    # an already-published older SQL file. This is document/source generation.
    source = 'docs/技术开发/database/migrations/'+MIGRATION
    destination = 'dev/src/internal/persistence/migrations/'+MIGRATION
    raw = (ROOT/source).read_bytes()
    if (ROOT/destination).exists() and (ROOT/destination).read_bytes() != raw:
        raise ValueError('Preserve frozen migration bytes; use a new forward migration')
    if not (ROOT/destination).exists():
        (ROOT/destination).write_bytes(raw)
    digest = hashlib.sha256(raw).hexdigest()
    metadata = {'source': source, 'source_sha256': digest, 'migration': MIGRATION,
                'migration_sha256': digest,
                'policy': '016 original Machine Pay context/receipts/recovery; SOURCE_ONLY, unexecuted, no execution or channel qualification'}
    target = 'dev/src/internal/persistence/migrations/source-v16.json'
    if not (ROOT/target).exists():
        dump(target, metadata)
    registry = load('dev/planning/编号登记-v1.0.json')
    for name, number in [('payment_machine_contexts', 'DATA-072'),
                         ('payment_machine_receipts', 'DATA-073'),
                         ('payment_machine_recoveries', 'DATA-074')]:
        if name in registry['tables'] and registry['tables'][name] != number:
            raise ValueError('Preserve existing DATA identities')
        registry['tables'][name] = number
    dump('dev/planning/编号登记-v1.0.json', registry)
    # Regenerate just the two changed authoritative Payment properties; retain
    # all later protocol/rental schemas and routes added to the source catalog.
    sys.path.insert(0, str(ROOT/'scripts'))
    from 白皮书契约数据 import make_schemas
    api = load('docs/白皮书/contracts/openapi-control-v3.0.json')
    payment = make_schemas()['Payment']['properties']
    for field in ['channel_reference', 'channel_action_ref']:
        api['components']['schemas']['Payment']['properties'][field] = deepcopy(payment[field])
    dump('docs/白皮书/contracts/openapi-control-v3.0.json', api)
    return metadata


def main():
    migration = source_contracts()
    plan, stage, status = [load(name) for name in [
        'dev/progress/code-first-plan.json', 'dev/progress/R026-code-stage.json',
        'dev/progress/implementation-status.json']]
    tests = []
    for source in TEST_FILES:
        # Text inventory only; no Go parser, compiler or test runner.
        for name in re.findall(r'^func (Test\w+)\(', (ROOT/source).read_text(encoding='utf-8'), re.M):
            tests.append({'id': f'PT-T{len(tests)+1:03d}', 'name': name,
                          'source': source, 'status': 'SOURCE_ONLY_NOT_EXECUTED'})
    integration = {
        'status': 'SOURCE_ONLY_UNVERIFIED', 'requirements': 'PT-001—040', 'annex': ANNEX,
        'original_order_payment_pay_intent_outbox_transaction_source_written': True,
        'independent_reviewer_evidence_and_sale_gate_source_written': True,
        'commit_before_once_precreate_source_written': True,
        'original_signed_envelope_and_separate_receipt_keys_source_written': True,
        'same_transaction_append_receipt_payment_channel_outbox_source_written': True,
        'encrypted_original_proof_query_job_transaction_source_written': True,
        'dedicated_capability_filtered_generation_recovery_source_written': True,
        'original_live_row_proof_retirement_source_written': True,
        'actual_operation_ownership_and_close_source_written': True,
        'api_and_worker_host_registry_secret_wiring_complete': False,
        'public_http_sdk_a402_or_paylauncher_wired': False,
        'unique_usable_funds_order_ready_or_execution_wired': False,
        'full_refund_payout_commission_statement_capability_wired': False,
        'paid_renewal_trusted_usage_acceptance_final_ledger_wired': False,
        'real_personal_seller_or_channel_qualified': False,
        'compiled_or_executed': False, 'migration_executed': False,
        'source_catalog_tables': 74, 'source_control_operations': 98,
        'control_http_operations': 48, 'new_data_ids': ['DATA-072', 'DATA-073', 'DATA-074'],
        'migration_source': migration, 'test_definition_count': len(tests),
        'full_development_round_complete': False,
        'remaining': [
            'PT-033：正式API/worker宿主、独立注册/秘密加载轮换撤销、钥与归档保留/实际关闭',
            'PT-034：本人create/get HTTP、原SDK付款前置/单Proof/A402与安全错误映射',
            'PT-035：实际获准PayLauncher、小程序原付款/回前台原查单和恢复',
            'PT-036：原资金唯一消费、当前主体/合同/退款/设备/截止下订单READY及原租赁/推理准入',
            'PT-037：获证实际退款/到账/佣金分账/对账单和原异常工作台',
            'PT-038：原付费续租、完整佣金产品、可信M token/小时水位/接受与追加账',
            'PT-039：实际迁移/跨进程退出/外部RSA2/个人卖家注册和真实渠道组合',
            'PT-040：全部56/222生产源码/实际依赖/测试源码后统一验证；DEV-55延后/DEV-32无证明关闭',
        ],
    }
    for record in (plan, stage):
        record['machine_payment_transaction_source_integration'] = deepcopy(integration)
        record['machine_pay_source_integration'].update({
            'platform_transaction_recovery_source_annex': ANNEX,
            'platform_transaction_and_signed_receipt_source_written': True,
            'durable_original_proof_recovery_source_written': True,
            'api_worker_host_integration_complete': False,
        })
    for task in plan['tasks']:
        if task['id'] in STEPS:
            task['code_status'] = 'IN_PROGRESS'
            for step in task['steps']:
                if step['id'] in STEPS[task['id']]:
                    step['code_status'] = 'IN_PROGRESS'
                    step['implementation_refs'] = sorted(set(step.get('implementation_refs', [])+REFS))
    for task in status['tasks']:
        if task['id'] in STEPS:
            task['implementation_status'] = 'IN_PROGRESS'
            task['machine_transaction_source_limit'] = DETAIL
            task['current_validation_status'] = 'NOT_EXECUTED'
            if task['id'] in {'DEV-13', 'DEV-15'}:
                task['remaining_detail'] = DETAIL
    status['activity_source'] = '2026-11-06 CODE_FIRST：PT-001—032 / IMP061—065新增源码；'+DETAIL
    for number, title, state in ITEMS:
        item = {'id': 'IMP-R026-'+number, 'title': title, 'status': state}
        stage['implemented_items'] = [old for old in stage['implemented_items'] if old['id'] != item['id']]+[item]
    stage['source_files'] = sorted(set(stage.get('source_files', [])+REFS))
    stage['tasks'] = deepcopy(status['tasks'])
    stage['platform_machine_payment_migration_source'] = migration
    stage['machine_transaction_test_definitions'] = {
        'tests': tests, 'ids': [t['id'] for t in tests], 'status': 'SOURCE_ONLY_NOT_EXECUTED'}
    for name, record in [('dev/progress/code-first-plan.json', plan),
                         ('dev/progress/R026-code-stage.json', stage),
                         ('dev/progress/implementation-status.json', status)]:
        dump(name, record)
    write_progress(plan, stage, status, tests)
    write_documentation(tests)
    print(f'Document/source generation only: IMP061–065 / PT001–040 / {len(tests)} test definitions / forward016 / 74 source tables. All 56/222 open; no compiler/test/validator/migration/runtime invoked.')


def write_progress(plan, stage, status, tests):
    counts = Counter(t['implementation_status'] for t in status['tasks'])
    lines = ['# R026 全量代码阶段进展', '',
        '**进行中，SOURCE_ONLY / 未验证。** 全部生产代码、实际依赖接线和测试源码齐备后统一验证。新增061—065为源码检查点，不是开发轮完成；历史内容按原阶段范围阅读。', '',
        '| 编号 | 本阶段已写内容 | 实际状态 |', '|---|---|---|']
    lines += [f"| {i['id']} | {i['title']} | {i['status']} |" for i in stage['implemented_items']]
    lines += ['', f'新增[PT-001—040](../../{ANNEX})及{len(tests)}项测试源码。原RN67/CE31/MW44/CH29/WS23/MS27/MG37/MP49亦未编译/运行；本次只新增前向016源，未迁移。源码目录98规划操作/74平台表/48控制HTTP。', '',
        '当前状态：'+'；'.join(f'{k} {v}' for k, v in sorted(counts.items()))+'；全部222子项未验收。', '',
        '| 编号 | 任务 | 当前状态 | 全部仍待收口子项 |', '|---|---|---|---|']
    current = {t['id']: t for t in status['tasks']}
    for task in plan['tasks']:
        t = current[task['id']]
        lines.append(f"| {task['id']} | {task['title']} | {t['implementation_status']} | {'、'.join(t['remaining_step_ids'])} |")
    write('dev/progress/R026-全量代码阶段进展.md', '\n'.join(lines)+'\n')
    lines = ['# 全量代码优先实施清单', '', DETAIL, '',
        '先完成全部生产代码、实际依赖接线和测试源码，再批量验证。代码与验收状态分开；全部222子项未验收。', '',
        '| 子项 | 任务 | 未收口需求 | 代码状态 | 验收 |', '|---|---|---|---|---|']
    for task in plan['tasks']:
        for step in task['steps']:
            lines.append(f"| {step['id']} | {task['title']} | {step['code_requirement']} | {step['code_status']} | {step['acceptance_status']} |")
    write('dev/全量代码优先实施清单.md', '\n'.join(lines)+'\n')


def write_documentation(tests):
    section(ANNEX, '## PT 测试源码定义索引',
        '| 编号 | 测试定义 | 源码 | 状态 |\n|---|---|---|---|\n'+'\n'.join(
            f"| {t['id']} | {t['name']} | {t['source']} | 未编译/未运行 |" for t in tests))
    note = (f'R026新增[PT-001—040]({ANNEX})：原个人卖家支付/PAY意图/上下文/outbox事务、'
            '独立审核四证/销售开关、提交后一次precreate、完整签名envelope与独立归档钥环、'
            '同事务receipt/payment/channel/outbox、原Proof密文/QUERY/专用job、原capability过滤/'
            'generation恢复与实际调用关闭源码。新增前向016/DATA072—074，当前源码98规划操作/'
            f'74平台表/48控制HTTP；{len(tests)}项新测试定义未编译/运行，SQL未迁移。'
            '正式API/worker宿主/秘密注册轮换、本人HTTP/SDK/A402/PayLauncher、原可执行资金/'
            '退款/设备/截止准入、完整退款/到账/佣金/对账单/付费续租/可信计量接受账务与真实资格仍待。'
            '全部56任务222子项开放；完整生产代码/实际依赖接线/测试源码后统一验证，本次不是开发轮完成。')
    english = (f'R026 adds [PT-001–040]({ANNEX}): original personal-seller payment/PAY-intent/context/'
               'outbox transactions, independent reviewer evidence and sale gates, commit-before-once '
               'precreate, complete signed envelopes and separate archive keys, atomic receipt/payment/'
               'channel/outbox updates, encrypted original-proof QUERY/jobs, capability-filtered generation '
               'recovery and actual operation joins. Forward016/DATA072–074 yields 98 planned operations/'
               f'74 platform tables/48 control HTTP operations. All {len(tests)} new test definitions '
               'remain uncompiled/unexecuted; no migration ran. API/worker hosts and secret registry/rotation, '
               'HTTP/SDK/A402/PayLauncher, uniquely usable funds/current refund-device-deadline admission, '
               'full refunds/payouts/fees/statements/paid renewal/trusted metering/acceptance/ledger and '
               'real qualification remain open. All 56 tasks/222 steps remain unaccepted. Complete all '
               'production source, actual dependency wiring and test source before batch validation. '
               'This is a source checkpoint, not a completed development round.')
    marker = '## R026 PT-001—040 支付事务与原凭据恢复源码 / Original-payment transactions and recovery source'
    for name, content in [('dev/repository/README.zh-CN.md.in', note), ('dev/repository/README.en.md.in', english)]:
        section(name, marker, content)
        body = (ROOT/name).read_text(encoding='utf-8')
        body = re.sub(r'\n<!-- R026-[A-Z-]+-CURRENT -->.*?<!-- /R026-[A-Z-]+-CURRENT -->\n', '\n', body, flags=re.S)
        body = re.sub(r'\n<!-- R026-SOURCE-HISTORY -->.*?<!-- /R026-SOURCE-HISTORY -->\n', '\n', body, flags=re.S)
        body = re.sub(r'\n{3,}', '\n\n', body)
        title, rest = body.split('\n', 1)
        history = ('以下保留此前源码检查点的范围说明和当时统计；当前状态以以上 PT 说明和全量进度表为准。'
                   if name.endswith('zh-CN.md.in') else
                   'The following source snapshots retain their earlier scope and counts. '
                   'The PT notice above and the full progress registry describe the current state.')
        write(name, title+'\n\n<!-- R026-PT-CURRENT -->\n'+content+'\n<!-- /R026-PT-CURRENT -->\n\n'
              '<!-- R026-SOURCE-HISTORY -->\n'+history+'\n<!-- /R026-SOURCE-HISTORY -->\n'+rest)
    for name in ['dev/repository/USAGE.md', 'dev/repository/SYSTEM_DESIGN.md']:
        section(name, marker, note.replace('(docs/', '(../../docs/')+'\n\n'+english.replace('(docs/', '(../../docs/'))
    for name, content in [('CHANGELOG.zh-CN.md', note), ('CHANGELOG.md', english)]:
        body = (ROOT/name).read_text(encoding='utf-8')
        body = re.sub(r'(?m)^- R026(?:新增| adds )\[PT-001[—–]040\].*\n', '', body)
        if content not in body:
            title, rest = body.split('\n', 1)
            write(name, title+'\n\n- '+content+'\n'+rest)
    for name in ['AGENTS.md', 'dev/AGENTS.md']:
        section(name, marker, note.replace('(docs/', '(../docs/' if name.startswith('dev/') else '(docs/')+
            '\n\n当前唯一增量整理器为'+TOOL+'；禁止重跑旧MP/MG/MS/WS/CH/CE/MW/RN整理器覆盖新PT状态。'
            '001—016平台双份SQL及全部已登记原端侧DDL/source.json发布后原字节冻结，后续平台只加017+。'
            '016仅源码，未迁移。当前不编译、测试、核验或运行数据库/内核/模型/设备/浏览器；全量代码和测试源码后统一验证。'
            '禁止本地虚拟化，DEV-55延后、DEV-32无证明关闭；浏览器使用后关闭不用页面。')
    for name, prefix in [('dev/src/internal/payments/README.md', '../../../../'),
                         ('dev/progress/README.md', '../../')]:
        section(name, marker, note.replace('(docs/', '('+prefix+'docs/'))
    for name in [
        'docs/技术开发/13-支付退款账务与渠道结算.md',
        'docs/技术开发/附录-MachinePay原支付边界与凭证恢复源码.md',
        'docs/技术开发/附录-本轮范围与端侧选型修订.md',
        'docs/技术开发/附录-小时租赁授权与累计计量源码细则.md',
        'docs/技术开发/00-技术文档导航与实施基线.md',
        'docs/协议/统一API与本地加密网关需求.md']:
        prefix = '../技术开发/' if name.startswith('docs/协议/') else ''
        section(name, marker, note.replace('(docs/技术开发/', '('+prefix))
    profile = load('dev/src/internal/payments/channel-profile.json')
    profile.update({'machine_transaction_source_annex': ANNEX,
        'platform_transaction_and_signed_envelope_source_written': True,
        'proof_receipt_keyrings': 'SEPARATE_KEY_MATERIAL_REQUIRED',
        'durable_proof_store': 'TRANSACTIONAL_ENCRYPTED_PLATFORM_ROW',
        'optional_native_file_proof_store': 'NOT_USED_BY_MACHINE_SERVICE',
        'public_payment_handlers_wired': False,
        'real_channel_or_merchant_qualified': False, 'compiled_or_executed': False})
    dump('dev/src/internal/payments/channel-profile.json', profile)


if __name__ == '__main__':
    main()
