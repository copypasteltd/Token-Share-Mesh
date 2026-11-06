"""Generate checkout source documentation only. No build/test/validator/runtime."""
from collections import Counter
from copy import deepcopy
import importlib.util
import json
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[2]
ANNEX = 'docs/技术开发/附录-原订单付款挑战与双端恢复源码.md'
TOOL = 'dev/tools/整理R026付款衔接源码.py'
OPERATIONS = [
    ('099', 'listOrderPaymentOptions', 'GET', '/orders/{order_id}/payment-options'),
    ('100', 'getOrderPayment', 'GET', '/orders/{order_id}/payment'),
    ('101', 'getPaymentChallenge', 'GET', '/payments/{payment_id}/challenge'),
    ('102', 'submitMachinePaymentProof', 'POST', '/payments/{payment_id}/proofs'),
    ('103', 'getMachinePaymentRecovery', 'GET', '/payments/{payment_id}/proof-recovery'),
]
TESTS = ['dev/src/tests/integration/payment_checkout_test.go',
         'dev/src/api/tests/payment_checkout_http_test.go',
         'dev/src/frontend/packages/core/tests/payment.test.ts']
REFS = [ANNEX, TOOL, 'scripts/白皮书契约数据.py', 'scripts/技术开发数据.py',
        'dev/planning/编号登记-v1.0.json', 'docs/白皮书/contracts/openapi-control-v3.0.json',
        'dev/src/internal/payments/checkout.go', 'dev/src/internal/payments/a402.go',
        'dev/src/internal/payments/machine.go', 'dev/src/api/internal/http/payment.go',
        'dev/src/api/internal/http/server.go', 'dev/src/api/internal/http/routes.go',
        'dev/src/api/internal/http/routes.json', 'dev/src/frontend/packages/core/src/client.ts',
        'dev/src/frontend/packages/core/src/payment.ts', 'dev/src/frontend/packages/core/src/payment-flow.ts',
        'dev/src/frontend/packages/core/src/model.ts', 'dev/src/frontend/packages/core/src/index.ts',
        'dev/src/miniapp/src/components/payment.tsx', 'dev/src/miniapp/src/pages/order/index.tsx',
        'dev/src/web/src/payment.tsx', 'dev/src/web/src/main.tsx', 'dev/tools/frontend/browser.mjs'] + TESTS
ITEMS = [
    ('076', '原订单付款选项与原付款查找源码', 'CK-001—010：当前原卖家能力/四证、同原订单查询、没有外部调用或替单'),
    ('077', '本人原402挑战与凭据恢复HTTP源码', 'CK-011—020：五个正式控制接口、唯一Proof头/严格空对象、202和当前CSRF/CORS；不视作付款或执行'),
    ('078', '双端原付款恢复与会话隔离源码', 'CK-021—030：React/Taro与React/Vite原订单页/安全元数据journal、单次内存凭据、迟到回调丢弃；实际获准PayLauncher未接入'),
    ('079', '固定INT099—103与四schema源码契约', '103规划/124 schema/75表/55控制HTTP，原001—017/端侧DDL原字节保留，无新SQL或迁移'),
    ('080', 'CK测试源码与全56任务222子项同步', '26项新定义仅源码；全量代码/实际接线/测试源码后统一验证，不是整轮完成'),
]
TASKS = {'DEV-02', 'DEV-03', 'DEV-08', 'DEV-09', 'DEV-10', 'DEV-11', 'DEV-13',
         'DEV-14', 'DEV-15', 'DEV-16', 'DEV-45', 'DEV-46', 'DEV-47', 'DEV-51', 'DEV-52', 'DEV-53', 'DEV-54', 'DEV-56'}
DETAIL = ('CK补入原订单当前可选原卖家付款方式、唯一原付款查找、原402挑战、单Proof头提交和恢复读取五接口，'
          '双端原订单付款页/安全元数据恢复journal/会话代次隔离源码。当前103规划操作/124 schema/75平台表/55控制HTTP。'
          '26项新增测试定义未编译/运行；无SQL变更或迁移。获准真实PayLauncher、小程序实际收取Machine Proof的适配、'
          '标准SDK付款前原买方路由/凭据绑定与数据面重试、跨端/丢失回调恢复、完整费用退款到账佣金对账/付费续租/可信计量接受账务、'
          'GF031旧正向夹具升级及真实渠道/设备仍待。全部56任务222子项开放，完整生产代码/实际依赖接线/测试源码后集中验证；本次不是整轮完成。')


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


def register_contracts():
    # Merge only these five new operations/four new schemas. Never regenerate
    # unrelated frozen/source-specialized original payment or rental contracts.
    source = ROOT/'scripts/白皮书契约数据.py'
    spec = importlib.util.spec_from_file_location('checkout_contract_source', source)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    canonical = module.make_openapi()
    name = 'docs/白皮书/contracts/openapi-control-v3.0.json'
    api = load(name)
    for schema in ['PaymentOption', 'PaymentOptions', 'MachineProofRecovery', 'MachinePayChallenge']:
        api['components']['schemas'][schema] = deepcopy(canonical['components']['schemas'][schema])
    registry = load('dev/planning/编号登记-v1.0.json')
    routes = load('dev/src/api/internal/http/routes.json')
    for number, operation, method, path in OPERATIONS:
        expected = 'INT-'+number
        if registry['operations'].get(operation, expected) != expected:
            raise ValueError('Preserve published explicit INT identity')
        registry['operations'][operation] = expected
        api['paths'].setdefault(path, {})[method.lower()] = deepcopy(canonical['paths'][path][method.lower()])
        route = {'operation_id': operation, 'method': method, 'path': path, 'handler': operation, 'anonymous': False}
        original = next((r for r in routes['operations'] if r['operation_id'] == operation), None)
        if original is not None and original != route:
            raise ValueError('Preserve original explicit operation binding')
        if original is None:
            routes['operations'].append(route)
    routes['scope'] = ('55 explicit control operations, including five original-buyer checkout/proof/recovery handlers; '
                       'R026 SOURCE_ONLY, uncompiled/unverified; no approved actual wallet bridge, real payment qualification or public inference admission')
    dump(name, api)
    dump('dev/planning/编号登记-v1.0.json', registry)
    dump('dev/src/api/internal/http/routes.json', routes)
    return {'planned_operations': len(registry['operations']), 'schemas': len(api['components']['schemas']),
            'platform_tables': len(registry['tables']), 'control_http_operations': len(routes['operations'])}


def test_inventory():
    result = []
    for source in TESTS:
        body = (ROOT/source).read_text(encoding='utf-8')
        pattern = r"^test\('([^']+)'" if source.endswith('.ts') else r'^func (Test\w+)\('
        for name in re.findall(pattern, body, re.M):
            result.append({'id': f'CK-T{len(result)+1:03d}', 'name': name, 'source': source, 'status': 'SOURCE_ONLY_NOT_EXECUTED'})
    return result


def main():
    counts, tests = register_contracts(), test_inventory()
    plan, stage, status = [load(name) for name in ['dev/progress/code-first-plan.json',
        'dev/progress/R026-code-stage.json', 'dev/progress/implementation-status.json']]
    integration = {'status': 'SOURCE_ONLY_UNVERIFIED', 'requirements': 'CK-001—040', 'annex': ANNEX,
        'original_order_options_lookup_source_wired': True,
        'owner_challenge_proof_recovery_control_source_wired': True,
        'web_miniapp_original_checkout_source_wired': True,
        'memory_only_proof_original_metadata_journal_source_written': True,
        'session_generation_late_response_guard_source_written': True,
        'actual_approved_paylauncher_wired': False,
        'standard_sdk_prepaid_buyer_route_authority_complete': False,
        'standard_sdk_data_plane_proof_retry_complete': False,
        'cross_device_or_lost_wallet_callback_recovery_complete': False,
        'full_fee_refund_payout_statement_renewal_usage_acceptance_ledger_complete': False,
        'legacy_positive_fixtures_rewritten': False, 'real_channel_or_device_qualification': False,
        'compiled_or_executed': False, 'sql_changed_or_migrated': False,
        'new_screenshots_or_recordings_captured': False,
        'test_definition_count': len(tests), 'full_development_round_complete': False,
        **counts,
        'remaining': ['CK-031：真实获准Machine Pay客户端能力与支付宝小程序适配，不能以普通tradePay伪造Proof',
            'CK-032：标准SDK付款前原买方/路由/凭据授权，不能绕过已付资金资格创建路由或API key',
            'CK-033：原SDK请求/Proof/有界402与恢复，不自动换卖家/订单或重收费',
            'CK-034：获准跨端/原钱包丢失回调恢复，不能凭客户端成功或超时认定已付/未付',
            'CK-035：完整费用/退款/到账/佣金/对账单，原付费续租和可信usage/小时/接受/账务',
            'CK-036：GF031旧正向夹具改为真实资格化原链路，不加旁路/skip或降断言',
            'CK-037：真实个人收单/原服务/原设备/秘密ACL/故障与撤销验证',
            'CK-038—040：全部56/222生产源码/实际接线/测试源码后集中验证与真实媒体重录；DEV55延后、增强无证明关闭']}
    for record in (plan, stage):
        record['original_checkout_source_integration'] = deepcopy(integration)
        for key in ['machine_pay_source_integration', 'machine_payment_transaction_source_integration',
                    'machine_payment_host_source_integration', 'original_order_funding_source_integration']:
            record[key].update({'current_checkout_source_annex': ANNEX,
                'owner_checkout_proof_metadata_control_source_written': True,
                'standard_sdk_and_actual_paylauncher_complete': False})
    for task in plan['tasks']:
        if task['id'] in TASKS:
            task['code_status'] = 'IN_PROGRESS'
            for step in task['steps']:
                step['code_status'] = 'IN_PROGRESS'
                step['implementation_refs'] = sorted(set(step.get('implementation_refs', [])+REFS))
    for task in status['tasks']:
        if task['id'] in TASKS:
            task['implementation_status'] = 'IN_PROGRESS'
            task['original_checkout_source_limit'] = DETAIL
            task['current_validation_status'] = 'NOT_EXECUTED'
            if task['id'] == 'DEV-13':
                task['remaining_detail'] = DETAIL
    status['activity_source'] = '2026-11-06 CODE_FIRST：CK-001—030 / IMP076—080；'+DETAIL
    for number, title, state in ITEMS:
        entry = {'id': 'IMP-R026-'+number, 'title': title, 'status': state}
        stage['implemented_items'] = [old for old in stage['implemented_items'] if old['id'] != entry['id']]+[entry]
    stage['source_files'] = sorted(set(stage.get('source_files', [])+REFS))
    stage['tasks'] = deepcopy(status['tasks'])
    stage['checkout_test_definitions'] = {'tests': tests, 'ids': [t['id'] for t in tests], 'status': 'SOURCE_ONLY_NOT_EXECUTED'}
    for name, value in [('dev/progress/code-first-plan.json', plan), ('dev/progress/R026-code-stage.json', stage),
                        ('dev/progress/implementation-status.json', status)]:
        dump(name, value)
    progress(plan, stage, status, tests, counts)
    documentation(tests, counts)
    print(json.dumps({'source_document_generation': 'CK001–040 / IMP076–080', 'unexecuted_test_definitions': len(tests),
        **counts, 'compiler_test_validator_migration_runtime': 'NOT_RUN', 'all_tasks_open': 56, 'all_steps_open': 222}, ensure_ascii=False))


def progress(plan, stage, status, tests, counts):
    totals = Counter(t['implementation_status'] for t in status['tasks'])
    lines = ['# R026 全量代码阶段进展', '', '**进行中，SOURCE_ONLY / 未验证。** 全部生产代码、实际依赖接线和测试源码后集中验证。新增076—080仅源码保存点，不是整轮完成；旧记录按当时范围阅读。', '',
        '| 编号 | 已写内容 | 实际状态 |', '|---|---|---|']
    lines += [f"| {i['id']} | {i['title']} | {i['status']} |" for i in stage['implemented_items']]
    lines += ['', f'新增[CK-001—040](../../{ANNEX})与{len(tests)}项未执行测试定义。原RN67/CE31/MW44/CH29/WS23/MS27/MG37/MP49/PT16/PH12/GF14亦未编译/运行。', '',
        f"当前{counts['control_http_operations']}控制HTTP/{counts['planned_operations']}规划/{counts['schemas']} schema/{counts['platform_tables']}平台表。本次没有SQL变更或迁移；已登记001—017及全部原端侧DDL/source.json原字节冻结，后续只追加018+。", '',
        '真实PayLauncher/小程序获准适配、SDK付款前原买方授权和数据面恢复、完整费用退款/续租/可信计量接受账务、旧正向夹具与全部实际接线继续开放。', '',
        '当前状态：'+'；'.join(f'{k} {v}' for k, v in sorted(totals.items()))+'；全部222子项未验收。', '',
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


def documentation(tests, counts):
    section(ANNEX, '## CK 测试源码定义索引', '| 编号 | 定义 | 源码 | 状态 |\n|---|---|---|---|\n'+'\n'.join(
        f"| {t['id']} | {t['name']} | {t['source']} | 未编译/未运行 |" for t in tests))
    zh = (f'R026新增[CK-001—040]({ANNEX})：原卖家当前付款选项、原订单唯一付款查找、原402挑战、单Proof头提交/恢复五接口，'
        'React/Taro与React/Vite原订单付款页、安全元数据journal和会话代次隔离源码。'
        f"当前{counts['control_http_operations']}控制HTTP/{counts['planned_operations']}规划/{counts['schemas']} schema/{counts['platform_tables']}平台表；{len(tests)}项新增测试定义未编译/运行，无SQL变更或迁移。"
        '真实获准PayLauncher/小程序付款能力、SDK付款前原买方授权及数据面重试、跨端回调恢复、完整费用退款到账佣金对账/付费续租/可信计量接受账务、旧正向夹具与真实资格仍待。'
        '当前端未安装获准PayLauncher，付款唤起按钮保持关闭。历史截图/GIF不能证明本次新页面；按全量代码后集中验证要求暂未重录。'
        '全部56任务222子项开放，本次不是整轮完成。')
    en = (f'R026 adds [CK-001–040]({ANNEX}): original-seller payment options, unique original-order payment lookup, original 402 challenge, single-header proof submission/recovery, '
        'React/Taro and React/Vite original checkout panels, a metadata-only recovery journal and session-generation guards. '
        f"Current source: {counts['control_http_operations']} control HTTP operations/{counts['planned_operations']} planned operations/{counts['schemas']} schemas/{counts['platform_tables']} platform tables; {len(tests)} new test definitions are uncompiled/unexecuted. No SQL changes or migrations. "
        'The approved actual PayLauncher/miniapp integration, prepayment original-buyer SDK authority, SDK data-plane recovery, cross-client wallet recovery, full fees/refunds/payouts/commission/statements/renewal/trusted metering/acceptance/ledger, legacy positive fixtures and real qualification remain open. '
        'No approved PayLauncher is installed: payment launch stays disabled. Existing screenshots/GIFs predate these source changes; fresh captures wait for full-source batch validation. All 56 tasks/222 steps remain open; this is not a completed round.')
    marker = '## R026 CK-001—040 原付款挑战与双端恢复 / Original checkout challenge and recovery source'
    for name, content in [('dev/repository/README.zh-CN.md.in', zh), ('dev/repository/README.en.md.in', en)]:
        section(name, marker, content)
        body = (ROOT/name).read_text(encoding='utf-8')
        body = re.sub(r'\n<!-- R026-[A-Z-]+-CURRENT -->.*?<!-- /R026-[A-Z-]+-CURRENT -->\n', '\n', body, flags=re.S)
        body = body.replace('以上 GF 说明', '以上 CK 说明').replace('The GF notice above', 'The CK notice above')
        body = re.sub(r'\n{3,}', '\n\n', body)
        title, rest = body.split('\n', 1)
        write(name, title+'\n\n<!-- R026-CK-CURRENT -->\n'+content+'\n<!-- /R026-CK-CURRENT -->\n'+rest)
    for name, content in [('CHANGELOG.zh-CN.md', zh), ('CHANGELOG.md', en)]:
        body = (ROOT/name).read_text(encoding='utf-8')
        body = re.sub(r'(?m)^- R026(?:新增| adds )\[CK-001[—–]040\].*\n', '', body)
        title, rest = body.split('\n', 1)
        write(name, title+'\n\n- '+content+'\n'+rest)
    for name in ['dev/repository/USAGE.md', 'dev/repository/SYSTEM_DESIGN.md']:
        section(name, marker, zh.replace('(docs/', '(../../docs/')+'\n\n'+en.replace('(docs/', '(../../docs/'))
    for name, prefix in [('dev/src/internal/payments/README.md', '../../../../'),
        ('dev/src/internal/paymenthost/README.md', '../../../../'), ('dev/src/frontend/README.md', '../../../'),
        ('dev/progress/README.md', '../../')]:
        section(name, marker, zh.replace('(docs/', '('+prefix+'docs/'))
    for name in ['docs/技术开发/00-技术文档导航与实施基线.md', 'docs/技术开发/13-支付退款账务与渠道结算.md',
        'docs/技术开发/14-前端路由状态与接口集成.md', 'docs/技术开发/附录-MachinePay原生宿主与本人支付接口源码.md',
        'docs/技术开发/附录-原订单资金唯一分配与当前准入源码.md', 'docs/技术开发/附录-本轮范围与端侧选型修订.md']:
        if (ROOT/name).exists():
            section(name, marker, zh.replace('(docs/技术开发/', '('))
    for name in ['AGENTS.md', 'dev/AGENTS.md']:
        section(name, marker, zh.replace('(docs/', '(../docs/' if name.startswith('dev/') else '(docs/')+
            '\n\n当前唯一增量整理器为'+TOOL+'；禁止重跑旧GF/PH/PT/MP/MG/MS/WS/CH/CE/MW/RN整理器覆盖当前CK状态。'
            '001—017平台双份SQL及原端侧DDL/source.json原字节冻结，只追加018+。'
            '当前不编译/测试/核验/迁移或启动DB、内核、模型、设备、浏览器；全量生产代码/实际依赖/测试源码后集中验证。'
            '无本地虚拟化，DEV55延后，DEV32无真实证明关闭；浏览器用后关闭不用页。')
    profile = load('dev/src/internal/payments/channel-profile.json')
    profile.update({'original_checkout_source_annex': ANNEX,
        'owner_checkout_proof_metadata_control_source_wired': True,
        'web_miniapp_checkout_source_wired': True, 'actual_approved_paylauncher_installed': False,
        'sdk_prepayment_original_buyer_authority_complete': False,
        'sdk_data_plane_payment_proof_retry_complete': False, 'compiled_or_executed': False})
    dump('dev/src/internal/payments/channel-profile.json', profile)


if __name__ == '__main__':
    main()
