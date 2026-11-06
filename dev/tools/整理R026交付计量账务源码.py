"""DA source/document authoring. Never compiles, tests, validates or starts runtimes."""
from collections import Counter
from copy import deepcopy
import hashlib
import importlib.util
import json
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[2]
ANNEX = 'docs/技术开发/附录-交付签收独立计量与镜像账源码.md'
TOOL = 'dev/tools/整理R026交付计量账务源码.py'
MIGRATION = '020_native_completion_billing.sql'
OPERATIONS = [
 ('getJob', 'GET', '/jobs/{job_id}'),
 ('cancelJob', 'POST', '/jobs/{job_id}/cancel'),
 ('recordBuyerReceipt', 'POST', '/jobs/{job_id}/receipts'),
 ('getOrderBill', 'GET', '/orders/{order_id}/bill'),
 ('registerAttemptDelivery', 'POST', '/runner/attempts/{attempt_id}/deliveries'),
 ('recordAttemptUsage', 'POST', '/runner/attempts/{attempt_id}/usage')]
SCHEMAS = ['OriginalBillingOutcome', 'OriginalResultMetadata', 'OriginalJobCompletion', 'OriginalOrderBill',
           'OriginalCompletionAck', 'OriginalBuyerDecision', 'OriginalCancel', 'NativeResultDeclaration', 'IndependentTokenStatement']
TABLES = {'native_result_deliveries': 'DATA-079', 'native_usage_verifications': 'DATA-080', 'accepted_usage_settlements': 'DATA-081'}
TESTS = ['dev/src/shared/resultwire/result_test.go', 'dev/src/tests/integration/completion_billing_test.go',
         'dev/src/api/tests/completion_http_test.go', 'dev/src/frontend/packages/core/tests/completion.test.ts',
         'dev/src/client/node/host_result_test.go']
REFS = [ANNEX, TOOL, 'dev/src/shared/resultwire/result.go', 'dev/src/internal/usage/verifier.go',
 'dev/src/internal/settlement/acceptance.go', 'dev/src/internal/invocation/completion.go',
 'dev/src/internal/identity/sessions.go', 'dev/src/api/internal/http/completion.go',
 'dev/src/api/internal/http/routes.go', 'dev/src/api/internal/http/server.go', 'dev/src/api/internal/http/routes.json',
 'dev/src/api/cmd/p11-api/completion.go', 'dev/src/api/cmd/p11-api/main.go',
 'dev/src/client/node/host_completion.go', 'dev/src/client/node/host_agent.go', 'dev/src/client/node/host_commands.go',
 'dev/src/client/node/host_store.go', 'dev/src/client/protocol/devicecontrol/completion.go',
 'dev/src/frontend/packages/core/src/completion.ts', 'dev/src/frontend/packages/core/src/model.ts',
 'dev/src/frontend/packages/core/src/index.ts', 'dev/src/miniapp/src/components/completion.tsx',
 'dev/src/miniapp/src/pages/order/index.tsx', 'dev/src/web/src/completion.tsx', 'dev/src/web/src/main.tsx',
 'dev/src/tests/integration/machine_payment_test.go', 'dev/src/tests/integration/core_test.go',
 'scripts/白皮书契约数据.py', 'scripts/技术开发数据.py', 'docs/白皮书/contracts/openapi-control-v3.0.json',
 'dev/planning/编号登记-v1.0.json', 'docs/技术开发/database/migrations/'+MIGRATION,
 'dev/src/internal/persistence/migrations/'+MIGRATION, 'dev/src/internal/persistence/migrations/source-v20.json'] + TESTS
ITEMS = [
 ('097', '原交付声明与本地结果journal源码', 'DA001—010：完整EOF/增量摘要/原设备签名/原报告恢复、原任务结果归属与物理资源复用分离，未验证'),
 ('098', '原买家唯一签收及取消源码', 'DA011—014：原版本/清单/本地核对摘要、当前角色、唯一接受拒绝取消竞争、终态保护；原窗口不延总期'),
 ('099', '独立计量与原数量口径源码', 'DA015—022：独立pin/资格/角色/签名和固定模型配方、OpenAI子集/Anthropic互斥计数、缺证保留，生产端待'),
 ('100', '原成交资格与累计镜像账源码', 'DA023—027：整单有理费用/佣金差额、原资金/退款门闩、同事务资格/预算/平衡账、暂计水位，完整费用到账待'),
 ('101', '同原生宿主、六HTTP与双端账单接线源码', 'DA028—030：复用六既有操作、当前Claims/DB、可选公开pin、Taro/React状态/安全原命令；020/DATA079—081未迁移'),
 ('102', 'DA测试源码及完整编号待办同步', 'DA030—040：协议/原支付链/签收计量账务竞态/HTTP/本地journal/双端恢复测试定义未编译运行；全部56/222开放')]
TASKS = {'DEV-'+str(n).zfill(2) for n in [1, 2, 3, 10, 11, 12, 15, 17, 18, 19, 20, 21, 23, 24, 26, 27, 37, 38, 39, 45, 46, 47, 48, 51, 52, 53, 54, 56]}
DETAIL = ('DA补入原交付完整声明/本地受认证journal/原报告恢复、当前原买家唯一签收与取消竞争、独立计量签名与固定计数配方、'
          '整单有理累计取分差额、原资金及退款门闩、同事务成交资格/预算/平衡追加镜像账、六既有HTTP/同原生宿主和Taro/React暂计账单源码。'
          '020/DATA079—081仅源码未迁移，计量生产端/真实结果传输及本地签收、同卖家重派/E2E换端、持久超时/争议、完整含费冲正/续租/渠道费/到账结算对账、'
          '独立复核恢复/历史混合来源、GF031其余旧夹具、CK获准付款/SDK和真实渠道设备资格继续待。'
          '本次测试定义均未编译/运行；全部56任务222子项开放，先完整生产源码/实际依赖/测试源码，再集中验证和重录媒体。本次不是整轮完成。')


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
    name = 'scripts/白皮书契约数据.py'
    body = (ROOT/name).read_text(encoding='utf-8')
    if 'return extend_native_completion_contracts(extend_refund_application_contracts({' not in body:
        start = body.index('    return extend_refund_application_contracts({')
        end = body.index('\ndef extend_refund_application_contracts', start)
        chunk = body[start:end].rstrip()
        if not chunk.endswith('})'): raise ValueError('Preserve original canonical contract structure')
        chunk = chunk.replace('return extend_refund_application_contracts({', 'return extend_native_completion_contracts(extend_refund_application_contracts({', 1)
        body = body[:start]+chunk[:-2]+'}))\n\n'+body[end:]
        write(name, body)
    spec = importlib.util.spec_from_file_location('native_completion_contract_source', ROOT/name)
    module = importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
    canonical = module.make_openapi()
    api = load('docs/白皮书/contracts/openapi-control-v3.0.json')
    for schema in SCHEMAS: api['components']['schemas'][schema] = deepcopy(canonical['components']['schemas'][schema])
    routes = load('dev/src/api/internal/http/routes.json')
    registry = load('dev/planning/编号登记-v1.0.json')
    for operation, method, path in OPERATIONS:
        if operation not in registry['operations']: raise ValueError('Reuse original planned operation ID')
        api['paths'].setdefault(path, {})[method.lower()] = deepcopy(canonical['paths'][path][method.lower()])
        route = {'operation_id': operation, 'method': method, 'path': path, 'handler': operation, 'anonymous': False}
        old = next((r for r in routes['operations'] if r['operation_id'] == operation), None)
        if old is not None and old != route: raise ValueError('Preserve original explicit control operation binding')
        if old is None: routes['operations'].append(route)
    for table, code in TABLES.items():
        if registry['tables'].get(table, code) != code: raise ValueError('Preserve original DATA identity')
        registry['tables'][table] = code
    routes['scope'] = '68 explicit control HTTP operations; six existing original plaintext result/receipt/usage/job/bill operations source-wired. Uncompiled/unexecuted; original privacy and funding gates remain.'
    dump('docs/白皮书/contracts/openapi-control-v3.0.json', api)
    dump('dev/src/api/internal/http/routes.json', routes);dump('dev/planning/编号登记-v1.0.json', registry)
    raw = (ROOT/'docs/技术开发/database/migrations'/MIGRATION).read_bytes()
    target = ROOT/'dev/src/internal/persistence/migrations'/MIGRATION
    if target.exists() and target.read_bytes() != raw: raise ValueError('Append a new migration; preserve registered SQL bytes')
    if not target.exists(): target.write_bytes(raw)
    digest = hashlib.sha256(raw).hexdigest()
    migration = {'source': 'docs/技术开发/database/migrations/'+MIGRATION, 'source_sha256': digest,
        'migration': MIGRATION, 'migration_sha256': digest,
        'policy': '020 native completion/independent usage/accepted append-only billing; SOURCE_ONLY, unapplied'}
    if not (ROOT/'dev/src/internal/persistence/migrations/source-v20.json').exists(): dump('dev/src/internal/persistence/migrations/source-v20.json', migration)
    name = 'scripts/技术开发数据.py';body = (ROOT/name).read_text(encoding='utf-8')
    body = body.replace('native_cleanup_recoveries"', 'native_cleanup_recoveries native_result_deliveries"')
    body = body.replace('"usage_evidence meter_statements"', '"usage_evidence meter_statements native_usage_verifications"')
    body = body.replace('settlement_batches batch_items"', 'settlement_batches batch_items accepted_usage_settlements"')
    body = body.replace('19:"registerAttemptDelivery recordBuyerReceipt"', '19:"getJob registerAttemptDelivery recordBuyerReceipt cancelJob"')
    if "'native_completion_billing_source':" not in body:
        addition = ("      'native_completion_billing_source':{'status':'SOURCE_ONLY_UNVERIFIED',\n"
                    f"        'annex':'{ANNEX}','tables':['native_result_deliveries','native_usage_verifications','accepted_usage_settlements'],\n"
                    "        'forward_source':'020_native_completion_billing.sql','migration_executed':False,\n"
                    "        'six_existing_operations_and_same_native_host_source_wired':True,'independent_meter_producer_complete':False,\n"
                    "        'same_seller_retry_e2e_transport_media_and_full_finance_complete':False,'actual_qualified_chain_complete':False,\n"
                    "        'compiled_or_executed':False},\n")
        body = body.replace("      'unproven_capabilities':", addition+"      'unproven_capabilities':", 1)
    write(name, body)
    name = 'dev/src/tests/integration/core_test.go';write(name, (ROOT/name).read_text(encoding='utf-8').replace('tables != 78 {', 'tables != 81 {'))
    return migration, {'planned_operations': len(registry['operations']), 'schemas': len(api['components']['schemas']),
        'platform_tables': len(registry['tables']), 'control_http_operations': len(routes['operations'])}


def inventory():
    result = []
    for source in TESTS:
        pattern = r"^test\('([^']+)'" if source.endswith('.ts') else r'^func (Test\w+)\('
        for name in re.findall(pattern, (ROOT/source).read_text(encoding='utf-8'), re.M):
            result.append({'id': f'DA-T{len(result)+1:03d}', 'name': name, 'source': source,
                           'kind': 'EXTENDED_EXISTING' if source.endswith('host_result_test.go') else 'NEW_DEFINITION',
                           'status': 'SOURCE_ONLY_NOT_EXECUTED'})
    return result


def main():
    migration, counts = register_source();tests = inventory()
    plan, stage, status = [load(name) for name in ['dev/progress/code-first-plan.json', 'dev/progress/R026-code-stage.json', 'dev/progress/implementation-status.json']]
    integration = {'status': 'SOURCE_ONLY_UNVERIFIED', 'requirements': 'DA001—040', 'annex': ANNEX,
        'signed_original_device_delivery_and_authenticated_local_journal_source_wired': True,
        'current_original_buyer_unique_decision_cancel_source_wired': True,
        'independent_meter_verifier_and_current_qualified_pins_source_wired': True,
        'cumulative_rational_price_qualified_budget_balanced_mirror_source_wired': True,
        'six_existing_http_native_host_react_taro_bill_source_wired': True,
        'independent_meter_producer_complete': False, 'actual_result_transport_and_local_buyer_receipt_complete': False,
        'same_seller_retry_e2e_rewrap_and_media_complete': False, 'durable_decision_expiry_worker_complete': False,
        'full_fee_earned_reversal_commission_renewal_payout_reconciliation_complete': False,
        'actual_channel_device_and_meter_qualification': False, 'legacy_positive_fixtures_rewritten': False,
        'compiled_or_executed': False, 'migration_executed': False, 'new_media_captured': False,
        'full_development_round_complete': False, 'forward_migration_source': migration,
        'test_definition_count': len(tests), 'new_test_definition_count': sum(t['kind'] == 'NEW_DEFINITION' for t in tests), **counts,
        'remaining': ['DA031—032：同卖家重派/未知查询/旧上下文围栏与E2E换端/本地签名/密文结果传输',
          'DA033—035：媒体/租赁最终账单、独立计量生产端及真实接收/资格、持久验收超时/争议举证',
          'DA036—038：完整费用/退款差额冲正/续租/渠道费/到账对账、独立恢复/历史导入、GF031其余旧夹具及CK/SDK',
          'DA039—040：全部56/222生产源码/实际依赖/测试源码后集中验证和新版媒体；DEV55延后、增强无证明关闭']}
    for record in (plan, stage): record['native_completion_billing_source_integration'] = deepcopy(integration)
    for task in plan['tasks']:
        if task['id'] in TASKS:
            task['native_completion_billing_source_limit'] = DETAIL
            for step in task['steps']: step['implementation_refs'] = sorted(set(step.get('implementation_refs', [])+REFS))
        if task['id'] == 'DEV-19': task['code_status'] = 'IN_PROGRESS'
    for task in status['tasks']:
        if task['id'] in TASKS:
            task['native_completion_billing_source_limit'] = DETAIL
            task['current_source_refs'] = sorted(set(task.get('current_source_refs', [])+REFS));task['current_validation_status'] = 'NOT_EXECUTED'
        if task['id'] == 'DEV-19': task['implementation_status'] = 'IN_PROGRESS'
        if task['id'] in {'DEV-19', 'DEV-38', 'DEV-39'}: task['remaining_detail'] = DETAIL
    status['activity_source'] = '2026-11-06 CODE_FIRST：DA001—030 / IMP097—102；'+DETAIL
    for number, title, state in ITEMS:
        entry = {'id': 'IMP-R026-'+number, 'title': title, 'status': state}
        stage['implemented_items'] = [old for old in stage['implemented_items'] if old['id'] != entry['id']]+[entry]
    stage.update({'complete': False, 'validation_started_after_user_policy': False, 'tasks': deepcopy(status['tasks']),
        'platform_completion_billing_migration_source': migration,
        'completion_billing_test_definitions': {'tests': tests, 'ids': [t['id'] for t in tests], 'status': 'SOURCE_ONLY_NOT_EXECUTED'}})
    stage['source_files'] = sorted(set(stage.get('source_files', [])+REFS))
    for name, value in [('dev/progress/code-first-plan.json', plan), ('dev/progress/R026-code-stage.json', stage), ('dev/progress/implementation-status.json', status)]: dump(name, value)
    progress(plan, stage, status, tests);documentation(tests, counts)
    print(json.dumps({'source_document_generation': 'DA001–040 / IMP097–102', 'unexecuted_or_extended_test_definitions': len(tests), **counts,
        'forward_source': MIGRATION, 'compiler_test_validator_migration_runtime': 'NOT_RUN',
        'all_tasks_open': len(plan['tasks']), 'all_steps_open': sum(len(t['steps']) for t in plan['tasks'])}, ensure_ascii=False))


def progress(plan, stage, status, tests):
    totals = Counter(t['implementation_status'] for t in status['tasks'])
    lines = ['# R026 全量代码阶段进展', '', '**进行中，SOURCE_ONLY / 未验证。** 先全部生产代码、实际依赖和测试源码，再集中验证。097—102只是源码保存点，不是整轮完成。', '',
             '| 编号 | 已写内容 | 实际状态 |', '|---|---|---|']
    lines += [f"| {i['id']} | {i['title']} | {i['status']} |" for i in stage['implemented_items']]
    lines += ['', DETAIL, '', f'本次涉及{len(tests)}项新增/扩展未执行测试定义。此前RN67/CE31/MW44/CH29/WS23/MS27/MG37/MP49/PT16/PH12/GF14/CK26/RF32/RT23/RA32亦未编译/运行。', '',
        '020仅源码登记未迁移；001—020及原端侧DDL/source元数据登记发布后原字节冻结，后续只追加021+。', '',
        '当前状态：'+'；'.join(f'{k} {v}' for k, v in sorted(totals.items()))+'；全部222子项未验收。', '',
        '| 编号 | 全部任务 | 当前状态 | 全部仍待收口子项 |', '|---|---|---|---|']
    current = {t['id']: t for t in status['tasks']}
    for task in plan['tasks']:
        t = current[task['id']];lines.append(f"| {task['id']} | {task['title']} | {t['implementation_status']} | {'、'.join(t['remaining_step_ids'])} |")
    write('dev/progress/R026-全量代码阶段进展.md', '\n'.join(lines)+'\n')
    lines = ['# 全量代码优先实施清单', '', DETAIL, '', '| 子项 | 任务 | 未收口需求 | 代码状态 | 验收 |', '|---|---|---|---|---|']
    for task in plan['tasks']:
        for step in task['steps']: lines.append(f"| {step['id']} | {task['title']} | {step['code_requirement']} | {step['code_status']} | {step['acceptance_status']} |")
    write('dev/全量代码优先实施清单.md', '\n'.join(lines)+'\n')


def documentation(tests, counts):
    section(ANNEX, '## DA 测试源码定义索引', '| 编号 | 定义 | 源码 | 类型 | 状态 |\n|---|---|---|---|---|\n'+'\n'.join(
        f"| {t['id']} | {t['name']} | {t['source']} | {t['kind']} | 未编译/未运行 |" for t in tests))
    zh = (f'R026新增[DA-001—040]({ANNEX})：原交付/本地签名报告恢复、当前原买家唯一签收与取消、独立计量核验、整单有理累计费用/佣金差额、'
          '同事务成交资格/原预算/追加镜像账，六既有控制HTTP/同原生宿主及Taro/React暂计账单源码。'
          f"020/DATA079—081仅源码，当前{counts['planned_operations']}规划/{counts['schemas']} schema/{counts['platform_tables']}平台表/{counts['control_http_operations']}控制HTTP；{len(tests)}项新增/扩展测试定义未编译/运行，020未迁移。"
          '实际结果传输/本地签收程序、独立计量生产端/真实资格、同卖家重派/E2E换端/媒体/持久超时/争议、完整费用冲正/续租/到账对账、历史恢复、CK/GF继续待。'
          '全部56任务222子项开放，先全部生产源码/实际接线/测试源码，再集中验证和重录媒体；本次不是整轮完成。')
    en = (f'R026 adds [DA-001–040]({ANNEX}): original delivery/local signed-report recovery, unique current-buyer acceptance/cancellation, independent metering verification, cumulative rational price/commission deltas, '
          'atomic qualification/original budget/append-only mirrors, six existing control operations/same native host and Taro/React provisional bills. '
          f"020/DATA079–081 are source only; {counts['planned_operations']} planned operations/{counts['schemas']} schemas/{counts['platform_tables']} platform tables/{counts['control_http_operations']} control HTTP operations. {len(tests)} new/extended test definitions remain uncompiled/unexecuted; 020 is unapplied. "
          'Actual result transport/local buyer receipt, the independent meter producer/real qualification, same-seller retry/E2E rewrap/media/durable expiry/disputes, full fee reversals/renewal/payout/reconciliation, reviewed history recovery and CK/GF remain open. '
          'All 56 tasks/222 steps remain open. Complete production source, actual wiring and test source precede batch validation and fresh media; this is not a completed round.')
    marker = '## R026 DA-001—040 交付签收与可信账务 / Original delivery, independent usage and billing source'
    for name, content in [('dev/repository/README.zh-CN.md.in', zh), ('dev/repository/README.en.md.in', en)]:
        section(name, marker, content);body = (ROOT/name).read_text(encoding='utf-8')
        body = re.sub(r'\n<!-- R026-[A-Z-]+-CURRENT -->.*?<!-- /R026-[A-Z-]+-CURRENT -->\n', '\n', body, flags=re.S)
        body = body.replace('以上 RA 说明', '以上 DA 说明').replace('The RA notice above', 'The DA notice above')
        title, rest = re.sub(r'\n{3,}', '\n\n', body).split('\n', 1)
        write(name, title+'\n\n<!-- R026-DA-CURRENT -->\n'+content+'\n<!-- /R026-DA-CURRENT -->\n'+rest)
    for name, content in [('CHANGELOG.zh-CN.md', zh), ('CHANGELOG.md', en)]:
        body = (ROOT/name).read_text(encoding='utf-8');body = re.sub(r'(?m)^- R026(?:新增| adds )\[DA-001[—–]040\].*\n', '', body)
        title, rest = body.split('\n', 1);write(name, title+'\n\n- '+content+'\n'+rest)
    for name in ['dev/repository/USAGE.md', 'dev/repository/SYSTEM_DESIGN.md']:
        section(name, marker, zh.replace('(docs/', '(../../docs/')+'\n\n'+en.replace('(docs/', '(../../docs/'))
    for name in ['dev/src/internal/invocation/README.md', 'dev/src/internal/usage/README.md', 'dev/src/internal/settlement/README.md']:
        section(name, marker, zh.replace('(docs/', '(../../../../docs/'))
    section('dev/progress/README.md', marker, zh.replace('(docs/', '(../../docs/'))
    for name in ['AGENTS.md', 'dev/AGENTS.md']:
        section(name, marker, zh.replace('(docs/', '(../docs/' if name.startswith('dev/') else '(docs/')+
            '\n\n当前唯一增量整理器为'+TOOL+'；禁止重跑旧RA/RT/RF/CK/GF/PH/PT/MP/MG/MS/WS/CH/CE/MW/RN整理器覆盖DA状态。'
            '001—020平台双份SQL/source元数据和原端侧DDL/source元数据登记发布后原字节冻结，只追加021+。'
            '当前不编译/测试/核验/迁移或启动DB、内核、模型、设备、浏览器；全部生产源码/实际依赖/测试源码后集中验证。'
            '无本地虚拟化，DEV55延后、DEV32无真实证明关闭；浏览器使用后关闭不用页面。')


if __name__ == '__main__':
    main()
