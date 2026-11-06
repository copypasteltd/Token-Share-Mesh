"""SI source/document authoring only. No compilation/test/check/runtime action."""
from collections import Counter
from copy import deepcopy
import json
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[2]
ANNEX = 'docs/技术开发/附录-标准SDK原订单文本入口源码.md'
TOOL = 'dev/tools/整理R026标准SDK源码.py'
RULES = 'dev/开发轮次执行规则.md'
TESTS = ['dev/src/gateway/core/preflight_test.go', 'dev/src/internal/invocation/sdk_test.go',
         'dev/src/gateway/platform/sdkhttp/server_test.go', 'dev/src/api/cmd/p11-api/sdk_test.go',
         'dev/src/tests/integration/sdk_native_text_test.go']
REFS = [ANNEX, TOOL, RULES, 'dev/src/gateway/core/preflight.go',
        'dev/src/internal/invocation/sdk.go', 'dev/src/gateway/platform/sdkhttp/server.go',
        'dev/src/gateway/platform/sdkhttp/output.go', 'dev/src/gateway/platform/sdkhttp/README.md',
        'dev/src/api/cmd/p11-api/main.go', 'dev/src/api/cmd/p11-api/sdk.go',
        'docs/技术开发/contracts/native-sdk-source-v1.json'] + TESTS
TASKS = {'DEV-'+str(n).zfill(2) for n in [1, 3, 11, 12, 17, 19, 20, 26, 27, 37, 38, 51, 52, 53, 54, 56]}
DETAIL = ('SI补入三种标准SDK非流式原生JSON入口、稳定原键、严格身份/头/正文、原订单预算/加密输入/首次租约同事务、'
          '原API key/路由/job逐次读取权、完整原结果核验和原生JSON返回、当前撤权/取消复核、有界等待并发及可选宿主/实际join源码。'
          '请求256KiB/结果1MiB/响应8MiB/并发4/等待120秒均为预算；75控制HTTP外新增3个数据面入口，115规划控制操作/150 schema/84平台表不变，无SQL或迁移。'
          '实时SSE/提供方守护编排/付款前SDK授权与获准PayLauncher、E2E/媒体/独立计量生产与真实资格/同卖家重派/持久签收超时/争议、'
          '租赁最终账单/完整费用冲正/续租/渠道到账对账/历史恢复及CK/GF继续待。全部56任务222子项开放；'
          '新测试定义未编译/运行，新媒体未录；先完整生产代码/实际依赖/测试源码后集中验证，本次不是整轮完成。')
ITEMS = [
    ('110', '三协议SDK请求边界源码', 'SI001—011/020：严格原生文本解析、原键和单一SDK身份，拒未映射流式/工具/beta，未验证'),
    ('111', '原订单SDK原子准入源码', 'SI012—019：同库/独立pin/四用途钥，预算/加密输入/首次租约同事务，原请求恢复，未运行'),
    ('112', '原API key只读结果与完整返回源码', 'SI021—034：原buyer/key/route/job逐次复核、逐块/总摘要核验、JSON返回及末次撤权复核，不自动接受或计费'),
    ('113', 'SDK可选原生宿主与实际关闭源码', 'SI035—037：三数据面入口/同宿主分流、独立限流/并发/期限、取消后实际join再销毁钥，未启动'),
    ('114', '全轮开发次序与完整待办约束', 'CF01—05/SI039—040：同一R026保留56/222，全部生产接线及测试源码齐备前禁止编译测试，不按源码保存拆轮'),
    ('115', 'SI测试源码及双语文档登记', 'SI038—040：解析/配置/HTTP/并发退出/原资金链/撤权取消定义未运行；待办、使用边界及双语说明同步')]


def load(name):
    return json.loads((ROOT / name).read_text(encoding='utf-8'))


def write(name, body):
    p = ROOT / name
    old = p.read_bytes() if p.exists() else b''
    ending = '\r\n' if b'\r\n' in old else '\n'
    p.parent.mkdir(parents=True, exist_ok=True)
    p.write_bytes(body.replace('\r\n', '\n').replace('\n', ending).encode('utf-8'))


def dump(name, value):
    write(name, json.dumps(value, ensure_ascii=False, indent=2)+'\n')


def section(name, marker, body):
    p = ROOT / name
    text = p.read_text(encoding='utf-8') if p.exists() else ''
    if marker in text:
        a = text.index(marker)
        b = text.find('\n## ', a+len(marker))
        text = text[:a].rstrip()+('\n\n'+text[b+1:] if b >= 0 else '')
    write(name, text.rstrip()+'\n\n'+marker+'\n\n'+body.strip()+'\n')


def inventory():
    definitions = []
    for source in TESTS:
        for name in re.findall(r'^func (Test\w+)\(', (ROOT/source).read_text(encoding='utf-8'), re.M):
            definitions.append({'id': f'SI-T{len(definitions)+1:03d}', 'name': name,
                                'source': source, 'kind': 'NEW_DEFINITION', 'status': 'SOURCE_ONLY_NOT_EXECUTED'})
    return definitions


def source_contract(tests):
    contract = {
        'profile_id': 'P11_NATIVE_TEXT_SDK_SOURCE_1', 'requirements': 'SI-001—040',
        'status': 'SOURCE_ONLY_UNVERIFIED', 'compiled_or_executed': False,
        'authority': ANNEX, 'development_policy': RULES,
        'operation_namespace': 'NATIVE_SDK_DATA_PLANE_NOT_CONTROL_OPENAPI',
        'operations': [
            {'id': 'SI-001', 'method': 'POST', 'path': '/v1/chat/completions', 'profile': 'P11_CHAT_TEXT_NATIVE_1'},
            {'id': 'SI-002', 'method': 'POST', 'path': '/v1/responses', 'profile': 'P11_RESPONSES_TEXT_NATIVE_1'},
            {'id': 'SI-003', 'method': 'POST', 'path': '/v1/messages', 'profile': 'P11_MESSAGES_TEXT_NATIVE_1'}],
        'request': {'native_json_unmodified': True, 'maximum_bytes': 262144,
                    'stream': False, 'idempotency_header': 'Idempotency-Key',
                    'key_bytes_minimum': 8, 'key_bytes_maximum': 128,
                    'anthropic_version': '2026-10-21',
                    'independent_route_bound_api_key_required': True,
                    'private_json_fields_required': False, 'browser_cookie_or_payment_proof_allowed': False},
        'original_call': {'funded_confirmed_c2c_route_required': True,
                          'budget_encrypted_input_first_lease_atomic': True,
                          'fallback_seller_or_reexecution_allowed': False,
                          'result_read_uses_same_original_key': True,
                          'result_read_mints_session_delegation': False,
                          'automatic_payment_acceptance_usage_settlement': False},
        'budgets_not_measurements': {'result_text_bytes': 1048576, 'response_json_bytes': 8388608,
                                     'simultaneous_requests': 4, 'maximum_wait_seconds': 120,
                                     'poll_milliseconds': 500, 'ip_calls_per_minute': 120,
                                     'key_calls_per_minute': 60, 'rate_bucket_capacity': 4096},
        'native_host_opt_in': 'P11_NATIVE_TEXT_SDK_FILE', 'source_refs': REFS,
        'new_test_definitions': len(tests), 'sql_changed_or_migrated': False,
        'full_scope_tasks': 56, 'full_scope_steps': 222,
        'test_admission': 'DENIED_UNTIL_ALL_IN_SCOPE_CODE_COMPLETE'}
    dump('docs/技术开发/contracts/native-sdk-source-v1.json', contract)
    name = 'scripts/技术开发数据.py'
    body = (ROOT/name).read_text(encoding='utf-8')
    if "'native_sdk_source':" not in body:
        addition = ("      'native_sdk_source':{'status':'SOURCE_ONLY_UNVERIFIED',\n"
                    f"        'annex':'{ANNEX}','source_contract':'docs/技术开发/contracts/native-sdk-source-v1.json',\n"
                    "        'nonstream_data_plane_operations':3,'automatic_provider_daemon_or_live_sse_complete':False,\n"
                    "        'original_funded_route_admission_and_same_key_result_source_wired':True,\n"
                    "        'sql_changed_or_migrated':False,'compiled_or_executed':False},\n")
        body = body.replace("      'unproven_capabilities':", addition+"      'unproven_capabilities':", 1)
        write(name, body)
    return contract


def progress(plan, stage, status, tests):
    totals = Counter(t['implementation_status'] for t in status['tasks'])
    lines = ['# R026 全量代码阶段进展', '',
             '**进行中，SOURCE_ONLY / 未验证。** 先全部生产代码、实际依赖和测试源码，再集中验证。110—115仅源码保存点，不是整轮完成。',
             '', '| 编号 | 已写内容 | 实际状态 |', '|---|---|---|']
    lines += [f"| {i['id']} | {i['title']} | {i['status']} |" for i in stage['implemented_items']]
    lines += ['', DETAIL, '', f'本次 {len(tests)} 项 SI 测试定义只有源码；此前全部 R026 源码和测试定义继续未编译/运行。',
              '', '本次无SQL变更；001—021平台双份SQL/source元数据和已登记端侧DDL/source元数据原字节保持，后续只追加022+。',
              '', '当前实施状态：'+'；'.join(f'{k} {v}' for k, v in sorted(totals.items()))+'；全部222子项未验收。',
              '', '| 编号 | 全部任务 | 当前状态 | 全部仍待收口子项 |', '|---|---|---|---|']
    current = {t['id']: t for t in status['tasks']}
    for task in plan['tasks']:
        t = current[task['id']]
        lines.append(f"| {task['id']} | {task['title']} | {t['implementation_status']} | {'、'.join(t['remaining_step_ids'])} |")
    write('dev/progress/R026-全量代码阶段进展.md', '\n'.join(lines)+'\n')
    lines = ['# 全量代码优先实施清单', '', DETAIL,
             '', '执行规则：[CF-01—05](开发轮次执行规则.md)。源码检查点不拆分完整R026轮次，不因局部模块已写完成而提前测试。',
             '', '| 子项 | 任务 | 未收口需求 | 代码状态 | 验收 |', '|---|---|---|---|---|']
    for task in plan['tasks']:
        for step in task['steps']:
            lines.append(f"| {step['id']} | {task['title']} | {step['code_requirement']} | {step['code_status']} | {step['acceptance_status']} |")
    write('dev/全量代码优先实施清单.md', '\n'.join(lines)+'\n')


def documentation(tests):
    marker = '## R026 SI-001—040 标准SDK原订单入口 / Original native SDK ingress source'
    zh = (f'R026新增[SI-001—040]({ANNEX})：三协议非流式原生JSON入口、原请求幂等、同原订单/预算/加密输入/首次租约事务、原API key只读结果/完整校验、有界等待/并发和可选原生宿主实际join源码。'
          f'{len(tests)}项新测试定义全部未编译/运行。115规划控制操作/150 schema/84平台表/75控制HTTP不变，另有3个数据面入口；无SQL或迁移。'
          '实时SSE/提供方常驻编排、SDK付款前授权/获准PayLauncher、E2E/媒体/独立计量/真实资格、持久签收超时/争议、完整结算对账及CK/GF仍待。'
          '全部56任务222子项保留，按CF-01—05先完整生产源码/实际依赖/测试源码，再集中验证和重录媒体；本次不是整轮完成。')
    en = (f'R026 adds [SI-001–040]({ANNEX}): three non-streaming native text SDK endpoints, original request idempotency, atomic original-order/budget/encrypted-input/first-lease admission, same-key read-only fully verified results, bounded waiting/concurrency and optional native host with actual caller join. '
          f'{len(tests)} new test definitions are uncompiled/unexecuted. The 115 planned control operations/150 schemas/84 tables/75 control HTTP registrations stay unchanged; three data-plane endpoints are separate. No SQL changes or migrations. '
          'Live SSE/provider-daemon orchestration, SDK pre-payment authorization/approved PayLauncher, E2E/media/independent meters/real qualification, durable buyer deadlines/disputes, complete settlement/reconciliation and CK/GF remain open. '
          'All 56 tasks/222 steps stay in the same round. CF-01–05 require complete source/actual wiring/test source before batch validation and new media; this is not a completed round.')
    section(ANNEX, '## SI 测试源码定义索引',
            '| 编号 | 定义 | 源码 | 状态 |\n|---|---|---|---|\n'+
            '\n'.join(f"| {t['id']} | {t['name']} | {t['source']} | 未编译/未运行 |" for t in tests))
    for name, content in [('dev/repository/README.zh-CN.md.in', zh), ('dev/repository/README.en.md.in', en)]:
        section(name, marker, content)
        body = (ROOT/name).read_text(encoding='utf-8')
        body = re.sub(r'\n<!-- R026-[A-Z-]+-CURRENT -->.*?<!-- /R026-[A-Z-]+-CURRENT -->\n', '\n', body, flags=re.S)
        body = body.replace('以上 DC 说明', '以上 SI 说明').replace('The DC notice above', 'The SI notice above')
        title, rest = re.sub(r'\n{3,}', '\n\n', body).split('\n', 1)
        write(name, title+'\n\n<!-- R026-SI-CURRENT -->\n'+content+'\n<!-- /R026-SI-CURRENT -->\n'+rest)
    for name, content in [('CHANGELOG.zh-CN.md', zh), ('CHANGELOG.md', en)]:
        body = (ROOT/name).read_text(encoding='utf-8')
        body = re.sub(r'(?m)^- R026(?:新增| adds )\[SI-001[—–]040\].*\n', '', body)
        title, rest = body.split('\n', 1)
        write(name, title+'\n\n- '+content+'\n'+rest)
    for name in ['dev/repository/USAGE.md', 'dev/repository/SYSTEM_DESIGN.md']:
        section(name, marker, zh.replace('(docs/', '(../../docs/')+'\n\n'+en.replace('(docs/', '(../../docs/'))
    section('dev/progress/README.md', marker, zh.replace('(docs/', '(../../docs/'))
    section('dev/src/internal/invocation/README.md', marker, zh.replace('(docs/', '(../../../../docs/'))
    section('docs/技术开发/00-技术文档导航与实施基线.md', marker, zh.replace('(docs/技术开发/', '('))
    for name in ['AGENTS.md', 'dev/AGENTS.md']:
        section(name, marker, zh.replace('(docs/', '(../docs/' if name.startswith('dev/') else '(docs/')+
                '\n\n最新唯一增量整理器为'+TOOL+'；禁止重跑旧DC/DA/RA/RT/RF/CK/GF/PH/PT/MP/MG/MS/WS/CH/CE/MW/RN整理器覆盖当前SI状态。'
                '执行'+RULES+'的CF-01—05：同一R026的56/222全部生产代码/实际依赖/测试源码齐备前，不编译/测试/核验/迁移或启动DB、模型、设备、浏览器；源码保存不算开发轮完成。'
                '001—021平台双份SQL/source元数据和已登记端侧DDL/source元数据原字节冻结，只追加022+。'
                '禁止本地Docker/WSL/QEMU等虚拟化；DEV55延后、DEV32无真实证明关闭；浏览器使用后关闭不用页。')


def main():
    tests = inventory()
    contract = source_contract(tests)
    plan, stage, status = [load(n) for n in ['dev/progress/code-first-plan.json', 'dev/progress/R026-code-stage.json', 'dev/progress/implementation-status.json']]
    integration = {'status': 'SOURCE_ONLY_UNVERIFIED', 'requirements': 'SI001—040', 'annex': ANNEX,
                   'nonstream_original_sdk_admission_result_and_host_source_wired': True,
                   'live_sse_or_automatic_provider_daemon_complete': False,
                   'automatic_payment_buyer_acceptance_or_trusted_usage': False,
                   'actual_channel_device_and_meter_qualification': False,
                   'compiled_or_executed': False, 'sql_changed_or_migrated': False,
                   'new_media_captured': False, 'full_development_round_complete': False,
                   'control_http_operations': 75, 'native_sdk_data_plane_operations': 3,
                   'planned_control_operations': 115, 'control_schemas': 150, 'platform_tables': 84,
                   'test_definition_count': len(tests), 'source_contract': contract['profile_id'],
                   'remaining': ['实时SSE与提供方真实常驻接单/自动生成交付/恢复编排',
                                 '付款前SDK授权/A402/获准PayLauncher和完整协议SDK版本联调',
                                 'E2E/媒体/同卖家重派/可信计量生产/真实资格/持久签收超时/争议',
                                 '租赁最终账单/完整费用冲正/续租/佣金渠道到账对账/历史恢复及CK/GF',
                                 '全56/222源码及测试源码后集中验证/新媒体；DEV55延后/DEV32无证明关闭']}
    policy = {'authority': '用户要求每一轮全部未定事项/TODO完整开发后再集中测试调试',
              'document': RULES, 'requirements': ['CF-01', 'CF-02', 'CF-03', 'CF-04', 'CF-05'],
              'checkpoints_do_not_split_development_round': True,
              'all_production_wiring_and_test_source_before_tests': True,
              'test_admission': 'DENIED_UNTIL_ALL_IN_SCOPE_CODE_COMPLETE'}
    for record in (plan, stage):
        record['native_sdk_source_integration'] = deepcopy(integration)
        record['full_round_execution_policy'] = deepcopy(policy)
    for task in plan['tasks']:
        if task['id'] in TASKS:
            task['native_sdk_source_limit'] = DETAIL
            for step in task['steps']:
                step['implementation_refs'] = sorted(set(step.get('implementation_refs', [])+REFS))
    for task in status['tasks']:
        if task['id'] in TASKS:
            task['native_sdk_source_limit'] = DETAIL
            task['current_source_refs'] = sorted(set(task.get('current_source_refs', [])+REFS))
            task['current_validation_status'] = 'NOT_EXECUTED'
    status['activity_source'] = '2026-11-07 CODE_FIRST：SI001—040 / IMP110—115；'+DETAIL
    for number, title, state in ITEMS:
        entry = {'id': 'IMP-R026-'+number, 'title': title, 'status': state}
        stage['implemented_items'] = [old for old in stage['implemented_items'] if old['id'] != entry['id']]+[entry]
    stage.update({'complete': False, 'validation_started_after_user_policy': False, 'tasks': deepcopy(status['tasks']),
                  'native_sdk_test_definitions': {'tests': tests, 'ids': [t['id'] for t in tests], 'status': 'SOURCE_ONLY_NOT_EXECUTED'}})
    stage['source_files'] = sorted(set(stage.get('source_files', [])+REFS))
    for name, value in [('dev/progress/code-first-plan.json', plan), ('dev/progress/R026-code-stage.json', stage), ('dev/progress/implementation-status.json', status)]:
        dump(name, value)
    progress(plan, stage, status, tests)
    documentation(tests)
    print(json.dumps({'source_document_generation': 'SI001—040 / IMP110—115', 'unexecuted_test_definitions': len(tests),
                      'control_http_operations': 75, 'native_sdk_data_plane_operations': 3,
                      'compiler_test_validator_migration_runtime': 'NOT_RUN', 'all_tasks_open': len(plan['tasks']),
                      'all_steps_open': sum(len(t['steps']) for t in plan['tasks'])}, ensure_ascii=False))


if __name__ == '__main__':
    main()
