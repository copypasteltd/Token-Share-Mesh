"""Generate RF source/progress documents only; no compiler/test/validator/runtime."""
from collections import Counter
from copy import deepcopy
import json
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[2]
ANNEX = 'docs/技术开发/附录-原退款协议与渠道恢复源码.md'
TOOL = 'dev/tools/整理R026原退款适配源码.py'
TESTS = ['dev/src/internal/payments/refund_alipay_test.go',
         'dev/src/internal/payments/refund_protocol_test.go',
         'dev/src/internal/payments/refund_receipt_test.go']
REFS = [ANNEX, TOOL, 'dev/src/internal/payments/refund-profile.json',
        'dev/src/internal/payments/alipay.go', 'dev/src/internal/payments/refund_alipay.go',
        'dev/src/internal/payments/refund_protocol.go',
        'dev/src/internal/payments/refund_receipt.go'] + TESTS
ITEMS = [
    ('081', '独立原退款范围与不可变意图源码', 'RF001—007：原卖家/原实付/原退款号/独立决策及额度/原绑定/提交与财务窗口；实际资格及持久意图待'),
    ('082', '原生原号退款与查询适配源码', 'RF008—010、015—022：独立RSA2方法围栏、完整原号归属、累计refund_fee、查询明确成功；默认Machine Pay仍只precreate/verify'),
    ('083', '退款UNKNOWN及查询准备状态源码', 'RF011—014、023—024：提交前UNKNOWN/原付款3秒/查询10秒/generation/成功保持；跨进程持久事务和worker待'),
    ('084', '完整原退款签名回执加密归档源码', 'RF025—028：AES-GCM/AAD原范围/意图/决策/方法/金额/时间、显式留旧钥、共享Close和实际响应join；解密不等于重验签'),
    ('085', 'RF测试源码及双语文档全待办同步', 'RF029—040：新增测试定义未执行；平台资格/事务/host/worker/HTTP/费用账务及全56/222继续开放'),
]
TASKS = {'DEV-03', 'DEV-13', 'DEV-14', 'DEV-15', 'DEV-16',
         'DEV-51', 'DEV-52', 'DEV-53', 'DEV-54', 'DEV-56'}
STEP_REFS = {'DEV-03.03', 'DEV-13.01', 'DEV-13.02', 'DEV-13.04',
             'DEV-14.01', 'DEV-14.02', 'DEV-14.03', 'DEV-15.03', 'DEV-15.04',
             'DEV-16.01', 'DEV-16.02', 'DEV-51.02', 'DEV-52.04',
             'DEV-53.01', 'DEV-53.02', 'DEV-54.01', 'DEV-54.02', 'DEV-56.01'}
DETAIL = ('RF补入独立条件原退款profile/原实付与售后决策意图、原生RSA2退款和原号查询、'
          '原付款3秒提交与10秒查询准备/UNKNOWN/generation/成功保持、完整原签名回执AES-GCM归档源码。'
          'refund_fee是原交易累计成功额，fund_change=Y也不直接确认本次部分退款；原号签名查询完整归属才记成功。'
          '本次不新增SQL/迁移/接口或依赖，103规划/124 schema/75平台表/55控制HTTP不变。'
          '当前没有真实Machine Pay个人卖家退款路由资格、持久退款事务、退款host/worker/HTTP接线。'
          '原累计锁/3秒水位/专用任务/独立策略/完整费用佣金到账对账及可信usage小时接受账务仍待，'
          'CK获准PayLauncher/SDK付款前授权/数据面恢复和GF031旧正向夹具继续开放。'
          '全部56任务222子项未收口；先完整生产代码/实际依赖/测试源码，再集中验证。本次不是开发轮完成。')


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


def source_inventory():
    # Document definitions, never import/compile/execute their test bodies.
    tests = []
    for source in TESTS:
        body = (ROOT/source).read_text(encoding='utf-8')
        for name in re.findall(r'^func (Test\w+)\(', body, re.M):
            tests.append({'id': f'RF-T{len(tests)+1:03d}', 'name': name,
                          'source': source, 'status': 'SOURCE_ONLY_NOT_EXECUTED'})
    registry = load('dev/planning/编号登记-v1.0.json')
    api = load('docs/白皮书/contracts/openapi-control-v3.0.json')
    routes = load('dev/src/api/internal/http/routes.json')
    counts = {'planned_operations': len(registry['operations']),
              'schemas': len(api['components']['schemas']),
              'platform_tables': len(registry['tables']),
              'control_http_operations': len(routes['operations'])}
    return tests, counts


def main():
    tests, counts = source_inventory()
    plan, stage, status = [load(name) for name in ['dev/progress/code-first-plan.json',
        'dev/progress/R026-code-stage.json', 'dev/progress/implementation-status.json']]
    integration = {'status': 'SOURCE_ONLY_CONDITIONAL_UNVERIFIED', 'requirements': 'RF-001—040',
        'annex': ANNEX, 'independent_original_refund_profile_source_written': True,
        'native_original_submit_and_query_source_written': True,
        'unknown_pre_dispatch_and_query_generation_source_written': True,
        'encrypted_complete_signed_refund_archive_source_written': True,
        'submit_fee_semantics': 'ORIGINAL_TRADE_CUMULATIVE_NOT_THIS_PARTIAL',
        'success_requires_exact_original_signed_query': True,
        'default_machine_pay_methods_extended': False,
        'actual_machine_pay_personal_seller_refund_qualification': False,
        'platform_refund_transactions_wired': False,
        'independent_current_refund_policy_and_route_decision_wired': False,
        'durable_cross_process_cumulative_and_dispatch_watermark_wired': False,
        'http_worker_host_refund_wired': False,
        'full_financial_chain_complete': False,
        'legacy_positive_fixtures_rewritten': False,
        'actual_approved_paylauncher_or_sdk_chain_complete': False,
        'compiled_or_executed': False, 'sql_changed_or_migrated': False,
        'new_screenshots_or_recordings_captured': False,
        'test_definition_count': len(tests), 'full_development_round_complete': False,
        **counts,
        'remaining': ['RF031：前向018+持久退款意图/原签名归档/调度与冻结；不改001—017',
            'RF032：独立当前原卖家/app/service/环境/原交易退款路线审批和撤销，真实资格仍无证据',
            'RF033：取消/售后/小时清算/续租/含费合同的独立最大退款决策',
            'RF034：原订单→原付款→退款锁、累计预留、3秒水位、幂等及提交前持久UNKNOWN',
            'RF035：专用任务/outbox/认领/原generation ACK/迟到签名事实归档及原号恢复',
            'RF036：受信任host原注册/秘密/留旧钥及原归档RSA重验签',
            'RF037：本人退款控制API/双端交互；不能直接公开原生适配器',
            'RF038：完整费用/佣金/到账/对账/计量接受账务和GF031旧夹具改造',
            'RF039—040：全56/222源码/接线/测试源码后统一测试与真实渠道设备/媒体；DEV55延后、增强无证明关闭']}
    for record in (plan, stage):
        record['original_refund_source_integration'] = deepcopy(integration)
        for key in ['machine_pay_source_integration', 'machine_payment_transaction_source_integration',
                    'machine_payment_host_source_integration', 'original_order_funding_source_integration',
                    'original_checkout_source_integration']:
            record[key].update({'current_original_refund_source_annex': ANNEX,
                                'native_original_refund_foundation_source_written': True,
                                'actual_qualified_platform_refund_chain_complete': False})
    for task in plan['tasks']:
        if task['id'] in TASKS:
            task['original_refund_source_limit'] = DETAIL
        for step in task['steps']:
            if step['id'] in STEP_REFS:
                step['implementation_refs'] = sorted(set(step.get('implementation_refs', [])+REFS))
    for task in status['tasks']:
        if task['id'] in TASKS:
            task['original_refund_source_limit'] = DETAIL
            task['current_source_refs'] = sorted(set(task.get('current_source_refs', [])+REFS))
            task['current_validation_status'] = 'NOT_EXECUTED'
            if task['id'] == 'DEV-14':
                task['remaining_detail'] = DETAIL
    status['activity_source'] = '2026-11-06 CODE_FIRST：RF-001—030 / IMP081—085；'+DETAIL
    for number, title, state in ITEMS:
        entry = {'id': 'IMP-R026-'+number, 'title': title, 'status': state}
        stage['implemented_items'] = [old for old in stage['implemented_items'] if old['id'] != entry['id']]+[entry]
    stage['complete'] = False
    stage['validation_started_after_user_policy'] = False
    stage['source_files'] = sorted(set(stage.get('source_files', [])+REFS))
    stage['tasks'] = deepcopy(status['tasks'])
    stage['original_refund_test_definitions'] = {'tests': tests, 'ids': [t['id'] for t in tests], 'status': 'SOURCE_ONLY_NOT_EXECUTED'}
    for name, value in [('dev/progress/code-first-plan.json', plan),
                        ('dev/progress/R026-code-stage.json', stage),
                        ('dev/progress/implementation-status.json', status)]:
        dump(name, value)
    progress(plan, stage, status, tests, counts)
    documentation(tests, counts)
    print(json.dumps({'source_document_generation': 'RF001–040 / IMP081–085',
        'unexecuted_test_definitions': len(tests), **counts,
        'compiler_test_validator_migration_runtime': 'NOT_RUN',
        'all_tasks_open': len(plan['tasks']),
        'all_steps_open': sum(len(t['steps']) for t in plan['tasks'])}, ensure_ascii=False))


def progress(plan, stage, status, tests, counts):
    totals = Counter(t['implementation_status'] for t in status['tasks'])
    lines = ['# R026 全量代码阶段进展', '',
        '**进行中，SOURCE_ONLY / 未验证。** 先完成全部生产代码、实际依赖接线和测试源码，再集中验证。新增081—085只是源码保存点，不是开发轮完成；此前记录按当时范围阅读。', '',
        '| 编号 | 已写内容 | 实际状态 |', '|---|---|---|']
    lines += [f"| {i['id']} | {i['title']} | {i['status']} |" for i in stage['implemented_items']]
    lines += ['', f'新增[RF-001—040](../../{ANNEX})与{len(tests)}项未执行测试定义。此前RN67/CE31/MW44/CH29/WS23/MS27/MG37/MP49/PT16/PH12/GF14/CK26亦未编译或运行。', '',
        f"当前{counts['control_http_operations']}控制HTTP/{counts['planned_operations']}规划/{counts['schemas']} schema/{counts['platform_tables']}平台表。本次没有SQL变更/迁移或新接口；已登记001—017及全部原端侧DDL/source.json原字节冻结，后续只追加018+。", '',
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
    section(ANNEX, '## RF 测试源码定义索引', '| 编号 | 定义 | 源码 | 状态 |\n|---|---|---|---|\n'+'\n'.join(
        f"| {t['id']} | {t['name']} | {t['source']} | 未编译/未运行 |" for t in tests))
    zh = (f'R026新增[RF-001—040]({ANNEX})：独立条件原退款profile/原意图、原生RSA2退款及原号查询、'
        '提交前UNKNOWN/3秒提交及10秒查询准备/generation/成功保持、完整原签名回执AES-GCM归档源码。'
        'refund_fee按整笔累计额处理，签名提交本身不能确认本次部分退款；原号查询完整归属及明确成功才记金融事实。'
        f"{len(tests)}项新增测试定义未编译/运行，{counts['planned_operations']}规划/{counts['schemas']} schema/{counts['platform_tables']}平台表/{counts['control_http_operations']}控制HTTP不变，无SQL变更/迁移。"
        '默认Machine Pay仍仅precreate/verify；真实原交易退款资格、持久累计锁/提交水位/任务、退款host/worker/HTTP和完整费用/佣金/到账/对账链继续待补。'
        'CK真实PayLauncher与SDK付款前授权/数据面恢复、GF031旧正向夹具及全56任务222子项继续开放。'
        '先完整源码/实际接线/测试源码，再集中验证和重录媒体；本次不是开发轮完成。')
    en = (f'R026 adds [RF-001–040]({ANNEX}): a separate conditional original-refund profile/intent, native RSA2 submit/query, '
        'pre-dispatch UNKNOWN preparation, three-second submit and ten-second query boundaries, generations, monotonic success and encrypted complete signed refund archives. '
        'Submit refund_fee is the original trade cumulative total; only an exact signed original-refund query can confirm this partial refund. '
        f"{len(tests)} new test definitions are uncompiled/unexecuted. Counts stay at {counts['planned_operations']} planned operations/{counts['schemas']} schemas/{counts['platform_tables']} platform tables/{counts['control_http_operations']} control HTTP operations; no SQL changes or migrations. "
        'Default Machine Pay still exposes only precreate/verify. Actual original-transaction refund qualification, durable cumulative locks/dispatch watermarks/jobs, refund host/worker/HTTP and full fees/commission/payout/reconciliation remain open. '
        'The CK approved PayLauncher/prepayment SDK authority/data-plane recovery, GF031 legacy positive fixtures and all 56 tasks/222 steps remain open. '
        'Complete production source, actual dependency wiring and test source precede batch validation and fresh media capture; this is not a completed development round.')
    marker = '## R026 RF-001—040 原退款适配与加密归档 / Original refund adapter and encrypted archive source'
    for name, content in [('dev/repository/README.zh-CN.md.in', zh), ('dev/repository/README.en.md.in', en)]:
        section(name, marker, content)
        body = (ROOT/name).read_text(encoding='utf-8')
        body = re.sub(r'\n<!-- R026-[A-Z-]+-CURRENT -->.*?<!-- /R026-[A-Z-]+-CURRENT -->\n', '\n', body, flags=re.S)
        body = body.replace('以上 CK 说明', '以上 RF 说明').replace('The CK notice above', 'The RF notice above')
        body = re.sub(r'\n{3,}', '\n\n', body)
        title, rest = body.split('\n', 1)
        write(name, title+'\n\n<!-- R026-RF-CURRENT -->\n'+content+'\n<!-- /R026-RF-CURRENT -->\n'+rest)
    for name, content in [('CHANGELOG.zh-CN.md', zh), ('CHANGELOG.md', en)]:
        body = (ROOT/name).read_text(encoding='utf-8')
        body = re.sub(r'(?m)^- R026(?:新增| adds )\[RF-001[—–]040\].*\n', '', body)
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
        'docs/技术开发/附录-原订单付款挑战与双端恢复源码.md',
        'docs/技术开发/附录-本轮范围与端侧选型修订.md']:
        section(name, marker, zh.replace('(docs/技术开发/', '('))
    for name in ['AGENTS.md', 'dev/AGENTS.md']:
        section(name, marker, zh.replace('(docs/', '(../docs/' if name.startswith('dev/') else '(docs/')+
            '\n\n当前唯一增量整理器为'+TOOL+'；禁止重跑旧CK/GF/PH/PT/MP/MG/MS/WS/CH/CE/MW/RN整理器覆盖当前RF状态。'
            '001—017平台双份SQL及原端侧DDL/source.json原字节冻结，只追加018+。'
            '当前不编译/测试/核验/迁移或启动DB、内核、模型、设备、浏览器；全量生产代码/实际依赖/测试源码后集中验证。'
            '无本地虚拟化，DEV55延后，DEV32无真实证明关闭；浏览器用后关闭不用页。')
    profile = load('dev/src/internal/payments/channel-profile.json')
    profile.update({'original_refund_source_annex': ANNEX,
        'independent_conditional_original_refund_source_written': True,
        'original_refund_actual_qualification': False,
        'original_refund_platform_transaction_host_worker_http_wired': False,
        'compiled_or_executed': False})
    dump('dev/src/internal/payments/channel-profile.json', profile)


if __name__ == '__main__':
    main()
