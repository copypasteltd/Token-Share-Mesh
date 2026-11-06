"""Write payment source/document inventories. Never build, test or validate."""
from collections import Counter
from copy import deepcopy
import json
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[2]
ANNEX = 'docs/技术开发/附录-MachinePay原支付边界与凭证恢复源码.md'
TOOL = 'dev/tools/整理R026支付边界源码.py'
TEST_FILES = ['dev/src/internal/payments/payments_test.go',
              'dev/src/internal/payments/proofstore_test.go']
REFS = sorted([ANNEX, TOOL] + [p.relative_to(ROOT).as_posix()
              for p in (ROOT/'dev/src/internal/payments').iterdir() if p.is_file()])
ITEMS = [
    ('056', '原个人卖家支付范围及独立配置源码', 'MP-001—007：实际seller/app/service/原合同/用途/原号、独立授权资格与双公钥pin、整数分和原始付款窗口；真实资格未获证'),
    ('057', '原生RSA2渠道及有界A402源码', 'MP-008—019/032：固定precreate/verify原字节验签、环境隔离、短期原凭据、有界I/O与真实join；尚无平台HTTP/小程序付款接线'),
    ('058', '原支付凭据加密及原生恢复工件源码', 'MP-020—026：原scope AES-GCM/显式旧钥/30分钟恢复、密文文件写入与不确定引用、原工件退役；不替代DB事务或独占进程锁'),
    ('059', '付款事实与退款对账边界源码', 'MP-027—031：付款单调/迟到实付、当前资金资格、退款UNKNOWN占额/原号重放、原范围差异分类；实际资金事务及渠道后续能力待接'),
    ('060', 'MP编号、边界测试源码与双语全量待办', 'MP-033—040：测试源定义、使用顺序/明确未接线项、全部56任务222子项和双语说明；未编译/运行，完整代码后统一验证'),
]
STEPS = {
    'DEV-11': {'DEV-11.02'},
    'DEV-13': {'DEV-13.01', 'DEV-13.02', 'DEV-13.04'},
    'DEV-14': {'DEV-14.01', 'DEV-14.02', 'DEV-14.03'},
    'DEV-15': {'DEV-15.01', 'DEV-15.02', 'DEV-15.03', 'DEV-15.04'},
    'DEV-16': {'DEV-16.01', 'DEV-16.02'},
    'DEV-53': {'DEV-53.01', 'DEV-53.04'},
    'DEV-54': {'DEV-54.01', 'DEV-54.02'},
    'DEV-56': {'DEV-56.01', 'DEV-56.04'},
}
DETAIL = ('新增实际个人seller原支付范围、RSA2 precreate/verify及A402、原Payment-Proof/'
          'AES-GCM/原生密文工件、付款单调/资金资格/退款容量及原范围对账源码。'
          '全部未编译或运行。平台payments/channel_requests事务、同事务outbox/唯一消费/'
          '已核原签名回包归档/秘密与授权注册撤销、本人HTTP/SDK/小程序PayLauncher、'
          '单宿主持有/原凭据持久恢复worker、真实退款/卖家到账/佣金/对账单、'
          '可信usage/小时计量和追加账、真实个人收单资格与渠道组合仍待。'
          '全部56任务222子项保持未验收；完整生产代码、实际依赖接线和测试源码后统一验证；'
          '本次是源码检查点，不是开发轮完成。')


def load(name):
    return json.loads((ROOT/name).read_text(encoding='utf-8'))


def write(name, value):
    path = ROOT/name
    original = path.read_bytes() if path.exists() else b''
    ending = '\r\n' if b'\r\n' in original else '\n'
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


def main():
    plan, stage, status = [load(name) for name in [
        'dev/progress/code-first-plan.json', 'dev/progress/R026-code-stage.json',
        'dev/progress/implementation-status.json']]
    tests = []
    for source in TEST_FILES:
        # Plain text inventory only: no Go parser, compiler, harness or validator.
        for name in re.findall(r'^func (Test\w+)\(', (ROOT/source).read_text(encoding='utf-8'), re.M):
            tests.append({'id': f'MP-T{len(tests)+1:03d}', 'name': name,
                          'source': source, 'status': 'SOURCE_ONLY_NOT_EXECUTED'})
    integration = {
        'status': 'SOURCE_ONLY_UNVERIFIED', 'requirements': 'MP-001—040', 'annex': ANNEX,
        'native_rsa2_precreate_verify_source_written': True,
        'independent_original_c2c_binding_and_key_pins_source_written': True,
        'a402_and_strict_original_proof_source_written': True,
        'proof_vault_and_native_ciphertext_artifact_source_written': True,
        'payment_monotonic_funding_refund_reconciliation_domain_source_written': True,
        'platform_payment_and_channel_request_transactions_wired': False,
        'same_transaction_outbox_and_unique_payment_consumption_wired': False,
        'verified_original_signed_receipt_archive_wired': False,
        'independent_authority_secret_registry_and_revocation_wired': False,
        'proof_store_exclusive_process_ownership_wired': False,
        'authenticated_payment_http_sdk_paylauncher_wired': False,
        'durable_original_proof_recovery_worker_wired': False,
        'actual_refund_payout_commission_statement_capability_wired': False,
        'trusted_usage_hours_and_final_ledger_wired': False,
        'real_personal_seller_or_channel_qualified': False,
        'compiled_or_executed': False, 'sql_changed_or_migrated': False,
        'full_development_round_complete': False,
        'test_definition_count': len(tests),
        'remaining': [
            'MP-034：原DB支付/渠道意图事务、独占宿主/秘密与授权注册撤销、同事务outbox/唯一消费和已核原签名回包受限归档',
            'MP-035：本人付款HTTP/原SDK支付前置/小程序PayLauncher及回前台原查单',
            'MP-036：原凭据引用/同一原号受限恢复worker、任务/撤销/崩溃恢复；不虚构独立query API',
            'MP-037：正式获证的原退款/到账/分账结算/对账单接口和最小披露财务界面',
            'MP-038：真实接受/可信token及小时水位、退款累计差额/唯一追加镜像账',
            'MP-039：真实个人seller/app/service/代调用/退款与佣金证据、RSA2外部向量和真实渠道组合',
            'MP-040：全部56/222生产代码、实际依赖及测试源码后统一编译测试调试验收；DEV-55延后/DEV-32无证明关闭',
        ],
    }
    for record in (plan, stage):
        # Preserve the published RN/CE/MW/CH/WS/MS/MG facts and their open limits.
        record['machine_pay_source_integration'] = deepcopy(integration)
    for task in plan['tasks']:
        if task['id'] not in STEPS:
            continue
        task['code_status'] = 'IN_PROGRESS'
        for step in task['steps']:
            if step['id'] in STEPS[task['id']]:
                step['code_status'] = 'IN_PROGRESS'
                step['implementation_refs'] = sorted(set(step.get('implementation_refs', [])+REFS))
    for task in status['tasks']:
        if task['id'] not in STEPS:
            continue
        task['implementation_status'] = 'IN_PROGRESS'
        task['machine_pay_source_limit'] = DETAIL
        task['current_validation_status'] = 'NOT_EXECUTED'
        if task['id'] in {'DEV-13', 'DEV-14', 'DEV-15', 'DEV-16'}:
            task['remaining_detail'] = DETAIL
    status['activity_source'] = '2026-11-06 CODE_FIRST：MP-001—033 / IMP056—060新增源码；'+DETAIL
    for number, title, state in ITEMS:
        item = {'id': 'IMP-R026-'+number, 'title': title, 'status': state}
        stage['implemented_items'] = [old for old in stage['implemented_items'] if old['id'] != item['id']]+[item]
    stage['source_files'] = sorted(set(stage.get('source_files', [])+REFS))
    stage['tasks'] = deepcopy(status['tasks'])
    stage['machine_pay_test_definitions'] = {
        'tests': tests, 'ids': [t['id'] for t in tests], 'status': 'SOURCE_ONLY_NOT_EXECUTED'}
    for name, record in [('dev/progress/code-first-plan.json', plan),
                         ('dev/progress/R026-code-stage.json', stage),
                         ('dev/progress/implementation-status.json', status)]:
        dump(name, record)
    write_progress(plan, stage, status, tests)
    write_documentation(tests)
    print(f'Document inventory only: IMP056–060 / MP001–040 / {len(tests)} test definitions; all 56/222 open. No compiler/test/validator/migration/runtime invoked.')


def write_progress(plan, stage, status, tests):
    counts = Counter(t['implementation_status'] for t in status['tasks'])
    lines = ['# R026 全量代码阶段进展', '',
        '**进行中，SOURCE_ONLY / 未验证。** 全部生产代码、实际依赖接线和测试源码齐备后统一验证。新增056—060只是源码检查点，不是开发轮完成；历史内容按原范围阅读。', '',
        '| 编号 | 本阶段已写内容 | 实际状态 |', '|---|---|---|']
    lines += [f"| {i['id']} | {i['title']} | {i['status']} |" for i in stage['implemented_items']]
    lines += ['', f'新增[MP-001—040](../../{ANNEX})及{len(tests)}项测试源码定义。原RN67/CE31/MW44/CH29/WS23/MS27/MG37亦未编译/运行；本次无SQL变更或迁移。', '',
        '当前状态：'+'；'.join(f'{k} {v}' for k, v in sorted(counts.items()))+'；全部222子项未验收。', '',
        '| 编号 | 任务 | 当前状态 | 全部仍待收口子项 |', '|---|---|---|---|']
    current = {t['id']: t for t in status['tasks']}
    for task in plan['tasks']:
        t = current[task['id']]
        lines.append(f"| {task['id']} | {task['title']} | {t['implementation_status']} | {'、'.join(t['remaining_step_ids'])} |")
    write('dev/progress/R026-全量代码阶段进展.md', '\n'.join(lines)+'\n')
    lines = ['# 全量代码优先实施清单', '', DETAIL, '',
        '先完成全部生产代码、实际依赖接线和测试源码，再批量验证。代码状态与验收分开；全部222子项未验收。', '',
        '| 子项 | 任务 | 未收口需求 | 代码状态 | 验收 |', '|---|---|---|---|---|']
    for task in plan['tasks']:
        for step in task['steps']:
            lines.append(f"| {step['id']} | {task['title']} | {step['code_requirement']} | {step['code_status']} | {step['acceptance_status']} |")
    write('dev/全量代码优先实施清单.md', '\n'.join(lines)+'\n')


def write_documentation(tests):
    section(ANNEX, '## MP 测试源码定义索引',
        '| 编号 | 测试定义 | 源码 | 状态 |\n|---|---|---|---|\n'+'\n'.join(
            f"| {t['id']} | {t['name']} | {t['source']} | 未编译/未运行 |" for t in tests))
    note = (f'R026新增[MP-001—040]({ANNEX})：实际个人seller原支付范围/双公钥pin、'
            '原生RSA2 precreate/verify及有界A402、严格原Payment-Proof/AES-GCM/原生密文工件、'
            '付款单调/迟到实付/资金资格/退款累计和原范围对账源码。'
            f'{len(tests)}项新测试定义全部未编译/运行。平台支付事务/同事务outbox/唯一消费/'
            '已核原签名回包归档/秘密与授权注册撤销、本人HTTP/SDK/PayLauncher、'
            '独占宿主及持久原凭据恢复worker、真实退款/到账/佣金/对账单与可信usage/小时水位/账务仍待。'
            '尚无真实个人收单资格或渠道验收，不开放收费。全部56任务222子项开放；无SQL变更或迁移。'
            '完整生产代码/实际依赖接线/测试源码后统一验证，本次不是开发轮完成。')
    english = (f'R026 adds [MP-001–040]({ANNEX}): original personal-seller payment bindings and two key pins, '
               'a native RSA2 precreate/verify adapter, bounded A402, strict original Payment-Proof, '
               'AES-GCM/native ciphertext artifacts, monotonic and late-paid facts, funding guards, '
               f'refund capacity and original-scope reconciliation. All {len(tests)} new test definitions '
               'remain uncompiled/unexecuted. Platform transactions/outbox/unique consumption/signed receipt '
               'archive/secret and authority registry, authenticated HTTP/SDK/PayLauncher, exclusive host '
               'and durable recovery workers, actual refund/payout/commission/statements and trusted '
               'usage/hour meters/ledger remain unwired. No real personal-seller/channel qualification '
               'or charging is enabled. All 56 tasks/222 steps remain open. No SQL changes/migrations. '
               'Complete all production source, actual dependency wiring and test source before batch '
               'validation. This is a source checkpoint, not a completed development round.')
    marker = '## R026 MP-001—040 原支付边界与凭证恢复源码 / Original-payment boundaries and recovery source'
    for name, content in [('dev/repository/README.zh-CN.md.in', note), ('dev/repository/README.en.md.in', english)]:
        section(name, marker, content)
        body = (ROOT/name).read_text(encoding='utf-8')
        body = re.sub(r'\n<!-- R026-[A-Z-]+-CURRENT -->.*?<!-- /R026-[A-Z-]+-CURRENT -->\n', '\n', body, flags=re.S)
        title, rest = body.split('\n', 1)
        write(name, title+'\n\n<!-- R026-MP-CURRENT -->\n'+content+'\n<!-- /R026-MP-CURRENT -->\n'+rest)
    for name in ['dev/repository/USAGE.md', 'dev/repository/SYSTEM_DESIGN.md']:
        section(name, marker, note.replace('(docs/', '(../../docs/')+'\n\n'+english.replace('(docs/', '(../../docs/'))
    for name, content in [('CHANGELOG.zh-CN.md', note), ('CHANGELOG.md', english)]:
        body = (ROOT/name).read_text(encoding='utf-8')
        body = re.sub(r'(?m)^- R026(?:新增| adds )\[MP-001[—–]040\].*\n', '', body)
        if content not in body:
            title, rest = body.split('\n', 1)
            write(name, title+'\n\n- '+content+'\n'+rest)
    for name in ['AGENTS.md', 'dev/AGENTS.md']:
        section(name, marker, note.replace('(docs/', '(../docs/' if name.startswith('dev/') else '(docs/')+
            '\n\n当前唯一增量整理器为'+TOOL+'；不要重跑旧MG/MS/WS/CH/CE/MW/RN整理器覆盖本次MP源码元数据。'
            '本次没有SQL变更。平台001—015双份SQL及schema.sql/host-schema.sql/native002/native003/source.json原字节保留。'
            '当前不编译、测试、核验、迁移或运行数据库/内核/模型/设备/浏览器；全量生产代码、实际接线与测试源码后统一验证。'
            '无本地虚拟化，DEV-55延后、DEV-32无证明关闭；浏览器使用后关闭不用页面。')
    for name, prefix in [('dev/src/internal/payments/README.md', '../../../../'),
                         ('dev/progress/README.md', '../../')]:
        section(name, marker, note.replace('(docs/', '('+prefix+'docs/'))
    for name in [
        'docs/技术开发/13-支付退款账务与渠道结算.md',
        'docs/技术开发/附录-本轮范围与端侧选型修订.md',
        'docs/技术开发/附录-小时租赁授权与累计计量源码细则.md',
        'docs/技术开发/附录-协议字段映射与兼容档位.md',
        'docs/技术开发/00-技术文档导航与实施基线.md',
        'docs/协议/统一API与本地加密网关需求.md']:
        prefix = '../技术开发/' if name.startswith('docs/协议/') else ''
        section(name, marker, note.replace('(docs/技术开发/', '('+prefix))


if __name__ == '__main__':
    main()
