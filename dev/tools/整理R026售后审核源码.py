"""RA source/document authoring only. No compiler/test/validator/DB/runtime."""
from collections import Counter
from copy import deepcopy
import hashlib
import importlib.util
import json
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[2]
ANNEX = 'docs/技术开发/附录-退款申请独立审核与双端售后源码.md'
TOOL = 'dev/tools/整理R026售后审核源码.py'
MIGRATION = '019_refund_applications.sql'
OPERATIONS = [('104', 'listOrderRefunds', 'GET', '/orders/{order_id}/refunds'),
 ('105', 'listRefundReviews', 'GET', '/finance/refund-reviews'),
 ('106', 'getRefundReview', 'GET', '/finance/refund-reviews/{refund_id}'),
 ('107', 'decideRefundReview', 'POST', '/finance/refund-reviews/{refund_id}/decisions'),
 ('108', 'revokeRefundReview', 'POST', '/finance/refund-reviews/{refund_id}/revocations')]
EXISTING = [('createRefund', 'POST', '/orders/{order_id}/refunds'), ('getRefund', 'GET', '/refunds/{refund_id}')]
TESTS = ['dev/src/tests/integration/refund_application_test.go', 'dev/src/api/tests/refund_http_test.go',
         'dev/src/frontend/packages/core/tests/refund.test.ts']
REFS = [ANNEX, TOOL, 'dev/src/internal/payments/refund_approved.go',
 'dev/src/internal/payments/refund_service.go', 'dev/src/internal/payments/refund_application.go',
 'dev/src/internal/payments/refund_application_store.go', 'dev/src/internal/payments/refund_review.go',
 'dev/src/internal/identity/sessions.go', 'dev/src/api/internal/http/refund.go',
 'dev/src/api/internal/http/server.go', 'dev/src/api/internal/http/routes.go', 'dev/src/api/internal/http/routes.json',
 'dev/src/api/cmd/p11-api/main.go', 'dev/src/frontend/packages/core/src/refund.ts',
 'dev/src/frontend/packages/core/src/index.ts', 'dev/src/frontend/packages/core/src/model.ts',
 'dev/src/miniapp/src/components/refund.tsx', 'dev/src/miniapp/src/pages/order/index.tsx',
 'dev/src/web/src/refund.tsx', 'dev/src/web/src/main.tsx', 'dev/src/tests/integration/core_test.go',
 'scripts/白皮书契约数据.py', 'scripts/技术开发数据.py', 'docs/白皮书/contracts/openapi-control-v3.0.json',
 'dev/planning/编号登记-v1.0.json', 'docs/技术开发/database/migrations/'+MIGRATION,
 'dev/src/internal/persistence/migrations/'+MIGRATION, 'dev/src/internal/persistence/migrations/source-v19.json'] + TESTS
ITEMS = [('091', '原买家售后申请与双方查询源码', 'RA001—008：原合同/付款/当前角色、一单一待审、原键/换键恢复及有限证据归属；申请无金融副作用'),
 ('092', '独立金融审核与原退款原子组合源码', 'RA009—020：当前独立角色/三版本/原决策与金融意图同事务、并发、拒绝和协调撤销；未知义务保持'),
 ('093', '七项售后控制接口及真实宿主依赖源码', 'RA021—024：复用create/get并增INT104—108；同Machine退款服务/当前会话/CSRF，无隐式渠道'),
 ('094', 'Taro与React售后及独立审核工作台源码', 'RA025—028：双端状态/金额、Web审核、原安全命令journal/未知复用/会话代次，NB04延续'),
 ('095', 'DATA078与前向019及契约登记', 'RA029：申请不可变/一待审/独立审批与原金融链延迟约束/协调撤销；019未迁移，108/128/78/62'),
 ('096', 'RA测试源码与双语全待办同步', 'RA030—040：新增测试定义未编译/运行；全部56任务222子项开放，完整费用/结算/争议及真实资格待')]
TASKS = {'DEV-01', 'DEV-02', 'DEV-03', 'DEV-10', 'DEV-13', 'DEV-14', 'DEV-15', 'DEV-16',
         'DEV-45', 'DEV-46', 'DEV-47', 'DEV-48', 'DEV-51', 'DEV-52', 'DEV-53', 'DEV-54', 'DEV-56'}
DETAIL = ('RA补入原买家申请/一单一待审/双方查询、当前独立金融审核/三版本/原决策与原退款同事务、拒绝/协调撤销和金融事实保持，'
          '七项控制HTTP、同原生宿主依赖、Taro/React售后与Web独立审核台、安全原请求恢复及会话代次源码。'
          '019/DATA078仅源码未迁移，108规划/128 schema/78平台表/62控制HTTP；新测试定义均未编译/运行。'
          '完整含费/已赚取价款冲销/佣金/续租/到账结算对账、权威失败复核重试/历史导入、争议选择性证据/运营生命周期、'
          'CK获准PayLauncher/SDK原买方授权和GF031旧正向夹具及真实渠道设备全链仍待。'
          '全部56任务222子项开放；先全部生产源码/实际依赖/测试源码，再集中验证和重录媒体，本次不是开发轮完成。')


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
    raw = (ROOT/'docs/技术开发/database/migrations'/MIGRATION).read_bytes()
    target = ROOT/'dev/src/internal/persistence/migrations'/MIGRATION
    if target.exists() and target.read_bytes() != raw:
        raise ValueError('Preserve registered SQL bytes; append the next version')
    if not target.exists(): target.write_bytes(raw)
    digest = hashlib.sha256(raw).hexdigest()
    migration = {'source': 'docs/技术开发/database/migrations/'+MIGRATION, 'source_sha256': digest,
                 'migration': MIGRATION, 'migration_sha256': digest,
                 'policy': '019 original buyer application/independent review/atomic original financial intent; SOURCE_ONLY, not executed'}
    if not (ROOT/'dev/src/internal/persistence/migrations/source-v19.json').exists():
        dump('dev/src/internal/persistence/migrations/source-v19.json', migration)
    registry = load('dev/planning/编号登记-v1.0.json')
    if registry['tables'].get('refund_applications', 'DATA-078') != 'DATA-078': raise ValueError('Preserve DATA identity')
    registry['tables']['refund_applications'] = 'DATA-078'
    spec = importlib.util.spec_from_file_location('refund_application_contract_source', ROOT/'scripts/白皮书契约数据.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    canonical = module.make_openapi()
    name = 'docs/白皮书/contracts/openapi-control-v3.0.json'
    api = load(name)
    for schema in ['Refund', 'RefundWrite', 'RefundApplicationPage', 'RefundReview', 'RefundReviewInput', 'RefundRevokeInput']:
        api['components']['schemas'][schema] = deepcopy(canonical['components']['schemas'][schema])
    routes = load('dev/src/api/internal/http/routes.json')
    operations = list(EXISTING)
    for number, operation, method, path in OPERATIONS:
        expected = 'INT-'+number
        if registry['operations'].get(operation, expected) != expected: raise ValueError('Preserve INT identity')
        registry['operations'][operation] = expected
        operations.append((operation, method, path))
    for operation, method, path in operations:
        api['paths'].setdefault(path, {})[method.lower()] = deepcopy(canonical['paths'][path][method.lower()])
        route = {'operation_id': operation, 'method': method, 'path': path, 'handler': operation, 'anonymous': False}
        old = next((r for r in routes['operations'] if r['operation_id'] == operation), None)
        if old is not None and old != route: raise ValueError('Preserve explicit original operation binding')
        if old is None: routes['operations'].append(route)
    routes['scope'] = '62 explicit control HTTP operations, including buyer aftersale requests and independent current financial review; SOURCE_ONLY, not compiled or qualified, no customer content or default refund capability'
    dump(name, api);dump('dev/src/api/internal/http/routes.json', routes);dump('dev/planning/编号登记-v1.0.json', registry)
    name = 'scripts/技术开发数据.py'
    body = (ROOT/name).read_text(encoding='utf-8')
    body = body.replace('order_fundings refund_machine_contexts refund_machine_receipts"', 'order_fundings refund_machine_contexts refund_machine_receipts refund_applications"')
    body = body.replace('OriginalPaidArchiveVerifier"', 'OriginalPaidArchiveVerifier RefundApplication IndependentRefundReview"')
    body = body.replace('14:"createRefund getRefund"', '14:"createRefund getRefund listOrderRefunds listRefundReviews getRefundReview decideRefundReview revokeRefundReview"')
    if "'original_refund_application_source':" not in body:
        addition = ("      'original_refund_application_source':{'status':'SOURCE_ONLY_UNVERIFIED',\n"
                    f"        'annex':'{ANNEX}','table':'refund_applications','data_id':'DATA-078',\n"
                    "        'forward_source':'019_refund_applications.sql','migration_executed':False,\n"
                    "        'buyer_request_is_approval':False,'independent_atomic_review_source_wired':True,\n"
                    "        'public_refund_http_and_dual_ui_source_wired':True,'native_host_same_machine_required':True,\n"
                    "        'full_fee_reversal_renewal_payout_chain_complete':False,'actual_channel_qualification':False,\n"
                    "        'compiled_or_executed':False},\n")
        body = body.replace("      'unproven_capabilities':", addition+"      'unproven_capabilities':", 1)
    write(name, body)
    name = 'dev/src/tests/integration/core_test.go'
    write(name, (ROOT/name).read_text(encoding='utf-8').replace('tables != 77 {', 'tables != 78 {'))
    name = ANNEX
    body = (ROOT/name).read_text(encoding='utf-8').replace('原 INT-060', registry['operations']['createRefund']).replace('原既有编号', registry['operations']['getRefund'])
    write(name, body)
    return migration, {'planned_operations': len(registry['operations']), 'schemas': len(api['components']['schemas']),
                       'platform_tables': len(registry['tables']), 'control_http_operations': len(routes['operations'])}


def inventory():
    result = []
    for source in TESTS:
        pattern = r"^test\('([^']+)'" if source.endswith('.ts') else r'^func (Test\w+)\('
        for name in re.findall(pattern, (ROOT/source).read_text(encoding='utf-8'), re.M):
            result.append({'id': f'RA-T{len(result)+1:03d}', 'name': name, 'source': source, 'status': 'SOURCE_ONLY_NOT_EXECUTED'})
    return result


def main():
    migration, counts = register_source()
    tests = inventory()
    plan, stage, status = [load(name) for name in ['dev/progress/code-first-plan.json', 'dev/progress/R026-code-stage.json', 'dev/progress/implementation-status.json']]
    integration = {'status': 'SOURCE_ONLY_UNVERIFIED', 'requirements': 'RA001—040', 'annex': ANNEX,
        'buyer_application_and_both_participants_read_source_wired': True,
        'current_independent_financial_review_and_atomic_original_intent_source_wired': True,
        'coordinated_revocation_preserves_financial_obligations_source_written': True,
        'seven_public_control_operations_source_wired': True, 'same_original_native_host_required': True,
        'react_taro_and_vite_aftersale_web_review_desk_source_wired': True,
        'safe_original_command_recovery_and_session_epoch_source_written': True,
        'actual_channel_qualification': False, 'full_fee_earned_reversal_commission_renewal_payout_reconciliation_complete': False,
        'full_dispute_selective_evidence_and_review_operations_lifecycle_complete': False,
        'definitive_failure_reviewed_retry_historical_import_complete': False,
        'approved_paylauncher_sdk_chain_complete': False, 'legacy_positive_fixtures_rewritten': False,
        'compiled_or_executed': False, 'migration_executed': False, 'new_media_captured': False,
        'full_development_round_complete': False, 'forward_migration_source': migration, 'test_definition_count': len(tests), **counts,
        'remaining': ['RA031：完整含费/已赚取价款冲销/佣金/续租/到账结算对账',
          'RA032：权威失败同号重试/复核恢复/历史金融导入', 'RA033：完整选择性证据/争议申诉/审核运营生命周期',
          'RA034—037：真实原退款资格/获准付款/SDK/GF031/计量接受账务与其他全量端侧源码',
          'RA038—040：全部56/222生产源码/依赖/测试源码后集中验证及新版媒体；DEV55延后/增强无证明关闭']}
    for record in (plan, stage):
        record['original_refund_application_source_integration'] = deepcopy(integration)
        for key in ['machine_pay_source_integration', 'machine_payment_transaction_source_integration', 'machine_payment_host_source_integration', 'original_order_funding_source_integration', 'original_checkout_source_integration', 'original_refund_source_integration', 'original_refund_transaction_source_integration']:
            record[key].update({'current_original_refund_application_source_annex': ANNEX,
                'current_public_refund_application_review_dual_ui_source_wired': True, 'actual_qualified_full_refund_chain_complete': False})
    for task in plan['tasks']:
        if task['id'] in TASKS:
            task['original_refund_application_source_limit'] = DETAIL
            for step in task['steps']:
                step['implementation_refs'] = sorted(set(step.get('implementation_refs', [])+REFS))
    for task in status['tasks']:
        if task['id'] in TASKS:
            task['original_refund_application_source_limit'] = DETAIL
            task['current_source_refs'] = sorted(set(task.get('current_source_refs', [])+REFS))
            task['current_validation_status'] = 'NOT_EXECUTED'
            if task['id'] in {'DEV-14','DEV-48'}: task['remaining_detail'] = DETAIL
        if task['id'] == 'DEV-48': task['implementation_status'] = 'IN_PROGRESS'
    status['activity_source'] = '2026-11-06 CODE_FIRST：RA001—030 / IMP091—096；'+DETAIL
    for number, title, state in ITEMS:
        entry = {'id': 'IMP-R026-'+number, 'title': title, 'status': state}
        stage['implemented_items'] = [old for old in stage['implemented_items'] if old['id'] != entry['id']]+[entry]
    stage.update({'complete': False, 'validation_started_after_user_policy': False, 'tasks': deepcopy(status['tasks']),
        'platform_refund_application_migration_source': migration,
        'refund_application_test_definitions': {'tests': tests, 'ids': [t['id'] for t in tests], 'status': 'SOURCE_ONLY_NOT_EXECUTED'}})
    stage['source_files'] = sorted(set(stage.get('source_files', [])+REFS))
    for name, value in [('dev/progress/code-first-plan.json', plan), ('dev/progress/R026-code-stage.json', stage), ('dev/progress/implementation-status.json', status)]: dump(name, value)
    progress(plan, stage, status, tests)
    documentation(tests, counts)
    print(json.dumps({'source_document_generation': 'RA001–040 / IMP091–096', 'unexecuted_test_definitions': len(tests), **counts,
        'forward_source': MIGRATION, 'compiler_test_validator_migration_runtime': 'NOT_RUN',
        'all_tasks_open': len(plan['tasks']), 'all_steps_open': sum(len(t['steps']) for t in plan['tasks'])}, ensure_ascii=False))


def progress(plan, stage, status, tests):
    totals = Counter(t['implementation_status'] for t in status['tasks'])
    lines = ['# R026 全量代码阶段进展', '', '**进行中，SOURCE_ONLY / 未验证。** 先全部生产代码、实际依赖和测试源码，再集中验证。091—096只是源码保存点，不是整轮完成。', '',
             '| 编号 | 已写内容 | 实际状态 |', '|---|---|---|']
    lines += [f"| {i['id']} | {i['title']} | {i['status']} |" for i in stage['implemented_items']]
    lines += ['', DETAIL, '', f'本次新增{len(tests)}项未执行测试定义。此前RN67/CE31/MW44/CH29/WS23/MS27/MG37/MP49/PT16/PH12/GF14/CK26/RF32/RT23亦未编译/运行。', '',
              '019仅源码登记未迁移；001—019及原端侧DDL/source元数据登记后原字节冻结，后续只追加020+。', '',
              '当前状态：'+'；'.join(f'{k} {v}' for k, v in sorted(totals.items()))+'；全部222子项未验收。', '',
              '| 编号 | 全部任务 | 当前状态 | 全部仍待收口子项 |', '|---|---|---|---|']
    current = {t['id']: t for t in status['tasks']}
    for task in plan['tasks']:
        t = current[task['id']]
        lines.append(f"| {task['id']} | {task['title']} | {t['implementation_status']} | {'、'.join(t['remaining_step_ids'])} |")
    write('dev/progress/R026-全量代码阶段进展.md', '\n'.join(lines)+'\n')
    lines = ['# 全量代码优先实施清单', '', DETAIL, '', '| 子项 | 任务 | 未收口需求 | 代码状态 | 验收 |', '|---|---|---|---|---|']
    for task in plan['tasks']:
        for step in task['steps']:
            lines.append(f"| {step['id']} | {task['title']} | {step['code_requirement']} | {step['code_status']} | {step['acceptance_status']} |")
    write('dev/全量代码优先实施清单.md', '\n'.join(lines)+'\n')


def documentation(tests, counts):
    section(ANNEX, '## RA 测试源码定义索引', '| 编号 | 定义 | 源码 | 状态 |\n|---|---|---|---|\n'+'\n'.join(
        f"| {t['id']} | {t['name']} | {t['source']} | 未编译/未运行 |" for t in tests))
    zh = (f'R026新增[RA-001—040]({ANNEX})：原买家申请/双方查询、当前独立金融审核和原退款同事务批准、拒绝/协调撤销、'
          '七项控制HTTP/同原生宿主、Taro/React售后与Web审核台、安全原请求恢复和会话代次源码。'
          f"019/DATA078仅源码，当前{counts['planned_operations']}规划/{counts['schemas']} schema/{counts['platform_tables']}平台表/{counts['control_http_operations']}控制HTTP；{len(tests)}项新测试定义未编译/运行，019未迁移。"
          '申请不是批准/渠道结果；完整费用冲销/佣金/续租/到账结算对账、独立复核重试/历史导入、完整争议举证/运营生命周期、CK/GF与真实资格仍待。'
          '全部56任务222子项开放，先全部生产源码/实际接线/测试源码，再集中验证和重录媒体；本次不是整轮完成。')
    en = (f'R026 adds [RA-001–040]({ANNEX}): original buyer applications/participant reads, current independent financial review atomically composed with the original refund, rejection/coordinated revocation, '
          'seven control HTTP operations/same native host, Taro/React aftersale views/Web review desk and safe original-command recovery/session epochs. '
          f"019/DATA078 are source only; {counts['planned_operations']} planned operations/{counts['schemas']} schemas/{counts['platform_tables']} platform tables/{counts['control_http_operations']} control HTTP operations. {len(tests)} new test definitions remain uncompiled/unexecuted; 019 is unapplied. "
          'An application is neither approval nor a channel result. Full fee/earned-service reversals, commission/renewal/payout/reconciliation, reviewed retries/historical imports, dispute disclosure/reviewer lifecycle, CK/GF and actual qualification remain open. '
          'All 56 tasks/222 steps remain open. Complete production source, actual wiring and test source precede batch validation and fresh media; this is not a completed round.')
    marker = '## R026 RA-001—040 退款申请与双端审核 / Refund applications and independent review source'
    for name, content in [('dev/repository/README.zh-CN.md.in', zh), ('dev/repository/README.en.md.in', en)]:
        section(name, marker, content)
        body = (ROOT/name).read_text(encoding='utf-8')
        body = re.sub(r'\n<!-- R026-[A-Z-]+-CURRENT -->.*?<!-- /R026-[A-Z-]+-CURRENT -->\n', '\n', body, flags=re.S)
        body = body.replace('以上 RT 说明', '以上 RA 说明').replace('The RT notice above', 'The RA notice above')
        body = re.sub(r'\n{3,}', '\n\n', body)
        title, rest = body.split('\n', 1)
        write(name, title+'\n\n<!-- R026-RA-CURRENT -->\n'+content+'\n<!-- /R026-RA-CURRENT -->\n'+rest)
    for name, content in [('CHANGELOG.zh-CN.md', zh), ('CHANGELOG.md', en)]:
        body = (ROOT/name).read_text(encoding='utf-8')
        body = re.sub(r'(?m)^- R026(?:新增| adds )\[RA-001[—–]040\].*\n', '', body)
        title, rest = body.split('\n', 1);write(name, title+'\n\n- '+content+'\n'+rest)
    for name in ['dev/repository/USAGE.md', 'dev/repository/SYSTEM_DESIGN.md']:
        section(name, marker, zh.replace('(docs/', '(../../docs/')+'\n\n'+en.replace('(docs/', '(../../docs/'))
    for name, prefix in [('dev/src/internal/payments/README.md', '../../../../'), ('dev/src/internal/paymenthost/README.md', '../../../../'), ('dev/progress/README.md', '../../')]:
        section(name, marker, zh.replace('(docs/', '('+prefix+'docs/'))
    for name in ['docs/技术开发/00-技术文档导航与实施基线.md', 'docs/技术开发/13-支付退款账务与渠道结算.md',
        'docs/技术开发/附录-原退款持久事务与宿主恢复源码.md', 'docs/技术开发/附录-原退款协议与渠道恢复源码.md',
        'docs/技术开发/附录-MachinePay原生宿主与本人支付接口源码.md', 'docs/技术开发/附录-本轮范围与端侧选型修订.md']:
        section(name, marker, zh.replace('(docs/技术开发/', '('))
    for name in ['AGENTS.md', 'dev/AGENTS.md']:
        section(name, marker, zh.replace('(docs/', '(../docs/' if name.startswith('dev/') else '(docs/')+
            '\n\n当前唯一增量整理器为'+TOOL+'；禁止重跑旧RT/RF/CK/GF/PH/PT/MP/MG/MS/WS/CH/CE/MW/RN整理器覆盖RA状态。'
            '001—019平台双份SQL/source元数据和原端侧DDL/source元数据登记发布后原字节冻结，只追加020+。'
            '当前不编译/测试/核验/迁移或启动DB、内核、模型、设备、浏览器；全部生产源码/实际依赖/测试源码后集中验证。'
            '无本地虚拟化，DEV55延后、DEV32无真实证明关闭，浏览器使用后关闭不用页面。')
    for name in ['dev/src/internal/payments/channel-profile.json', 'dev/src/internal/payments/refund-profile.json']:
        p = load(name)
        p.update({'original_refund_application_source_annex': ANNEX, 'public_refund_http_source_wired': True,
                  'independent_review_and_dual_aftersale_ui_source_wired': True,
                  'actual_original_refund_qualification': False, 'full_fee_reversal_renewal_payout_chain_complete': False,
                  'compiled_or_executed': False})
        dump(name, p)


if __name__ == '__main__':
    main()
