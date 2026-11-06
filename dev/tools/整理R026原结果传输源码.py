"""DC source/document authoring only. No compiler/test/check/runtime execution."""
from collections import Counter
from copy import deepcopy
import hashlib
import importlib.util
import json
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[2]
ANNEX = 'docs/技术开发/附录-原结果分块传输与本地接收源码.md'
TOOL = 'dev/tools/整理R026原结果传输源码.py'
MIGRATION = '021_original_result_content.sql'
OPERATIONS = [
 ('beginOriginalResultContent', 'POST', '/runner/attempts/{attempt_id}/content/begin', False),
 ('putOriginalResultChunk', 'POST', '/runner/attempts/{attempt_id}/content/chunks', False),
 ('sealOriginalResultContent', 'POST', '/runner/attempts/{attempt_id}/content/seal', False),
 ('createOriginalResultReadGrant', 'POST', '/jobs/{job_id}/results/{delivery_id}/read-grants', False),
 ('getOriginalResultContent', 'GET', '/result-content/{delivery_id}/manifest', True),
 ('getOriginalResultChunk', 'GET', '/result-content/{delivery_id}/chunks/{chunk_index}', True),
 ('getOriginalResultUploadStatus', 'GET', '/runner/attempts/{attempt_id}/content/{delivery_id}/status', False)]
SCHEMAS = ['OriginalResultContentInfo', 'OriginalResultContentTerminal', 'OriginalResultExecutionManifest',
 'OriginalResultContentHead', 'OriginalResultContentBegin', 'OriginalResultContentChunk', 'OriginalResultContentSeal',
 'OriginalResultContentAck', 'OriginalResultReadRequest', 'OriginalResultReadGrant', 'OriginalResultReadIssued',
 'OriginalResultReadPacket', 'OriginalLocalReview']
TABLES = {'native_result_content': 'DATA-082', 'native_result_content_chunks': 'DATA-083', 'native_result_read_grants': 'DATA-084'}
TESTS = ['dev/src/shared/resultwire/content_test.go', 'dev/src/client/results/import_test.go',
 'dev/src/client/buyerresults/receive_test.go', 'dev/src/client/node/host_content_test.go',
 'dev/src/tests/integration/result_content_test.go', 'dev/src/api/tests/result_content_http_test.go',
 'dev/src/frontend/packages/core/tests/result-content.test.ts']
REFS = [ANNEX, TOOL, 'dev/src/shared/resultwire/content.go', 'dev/src/internal/invocation/result_content.go',
 'dev/src/internal/invocation/result_content_status.go', 'dev/src/internal/invocation/completion.go',
 'dev/src/api/internal/http/result_content.go', 'dev/src/api/internal/http/server.go', 'dev/src/api/internal/http/routes.go',
 'dev/src/api/internal/http/routes.json', 'dev/src/api/cmd/p11-api/main.go', 'dev/src/api/cmd/p11-api/result_content.go',
 'dev/src/client/protocol/devicecontrol/result_content.go', 'dev/src/client/node/host_content.go',
 'dev/src/client/node/host_store.go', 'dev/src/client/node/host_commands.go', 'dev/src/client/node/host_test.go',
 'dev/src/client/results/store.go', 'dev/src/client/results/import.go', 'dev/src/client/buyerresults/receive.go',
 'dev/src/client/cmd/p11-buyer/main.go', 'dev/src/frontend/packages/core/src/completion.ts',
 'dev/src/miniapp/src/components/completion.tsx', 'dev/src/web/src/completion.tsx',
 'dev/src/tests/integration/machine_payment_test.go', 'dev/src/tests/integration/completion_billing_test.go',
 'dev/src/tests/integration/core_test.go', 'scripts/白皮书契约数据.py', 'scripts/技术开发数据.py',
 'docs/白皮书/contracts/openapi-control-v3.0.json', 'dev/planning/编号登记-v1.0.json',
 'docs/技术开发/database/migrations/'+MIGRATION, 'dev/src/internal/persistence/migrations/'+MIGRATION,
 'dev/src/internal/persistence/migrations/source-v21.json'] + TESTS
ITEMS = [
 ('103', '原内容协议与加密分块保存源码', 'DC001—017：原声明/清单、32MiB/32KiB、逐结果钥/分块AEAD、完整UTF8/hash/length封存，未验证'),
 ('104', '原提供方上传及原状态恢复源码', 'DC018—022：同缓存两遍读取、四传输单元、受认证head journal、原状态查询；无重跑/新计算权，守护编排待'),
 ('105', '原买家短时只读授权与撤权源码', 'DC023—029/042—044：原会话/角色/版本，提交后HMAC只读秘密、安全幂等、取消拒绝/过期退役，不代替E2E'),
 ('106', '买家原生加密接收与导出源码', 'DC030—038：固定GET/四块窗口、完整核验后加密缓存、protocol/raw明确导出、失败无成功回执，未运行'),
 ('107', 'React/Taro与Web显式本地核对签收接线源码', 'DC039—041：读取包仅内存、原未知授权重试、会话代次隔离、六字段回执与用户明确接受拒绝，未构建'),
 ('108', '七HTTP与021前向数据契约接线源码', 'DC016/023—029/044：INT109—115、DATA082—084、独立read认证/原宿主/钥环/退役worker；021未迁移'),
 ('109', 'DC测试源码及完整编号待办同步', 'DC045：40项测试定义只写源码，全56任务/222子项继续开放，集中验证门槛未开放')]
TASKS = {'DEV-'+str(n).zfill(2) for n in [1, 2, 3, 11, 19, 20, 21, 24, 26, 27, 37, 45, 46, 47, 51, 52, 53, 54, 56]}
DETAIL = ('DC补入PLAINTEXT/STANDARD原结果32KiB分块/32MiB上限、逐结果AES-GCM、完整UTF8/hash/length封存、'
 '原提供方缓存上传与元数据journal恢复、当前原买家会话绑定5分钟只读授权/安全幂等、原生加密接收/protocol与raw导出、'
 'React/Taro与Web内存读取包和显式本地核对签收、取消/拒绝/固定过期退役及七HTTP源码。'
 '021/DATA082—084仅源码未迁移；SDK/实际提供方守护编排、独立计量生产端与真实资格、同卖家重派/E2E换端/媒体/租赁最终账单/'
 '持久签收超时/争议、完整费用冲正/续租/渠道到账对账、独立历史恢复及CK/GF继续待。'
 '测试定义未编译运行，新媒体未录；全部56任务222子项开放，先完整生产源码/实际依赖/测试源码后集中验证，本次不是整轮完成。')


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
        start = body.index(marker);end = body.find('\n## ', start+len(marker))
        body = body[:start].rstrip()+('\n\n'+body[end+1:] if end >= 0 else '')
    write(name, body.rstrip()+'\n\n'+marker+'\n\n'+content.strip()+'\n')


def register_source():
    name = 'scripts/白皮书契约数据.py';body = (ROOT/name).read_text(encoding='utf-8')
    if 'return extend_original_result_content_contracts(extend_native_completion_contracts(' not in body:
        start = body.index('    return extend_native_completion_contracts(');end = body.index('\ndef extend_refund_application_contracts', start)
        chunk = body[start:end].rstrip()
        if not chunk.endswith('}))'): raise ValueError('Preserve canonical completion/refund contracts')
        chunk = chunk.replace('return extend_native_completion_contracts(', 'return extend_original_result_content_contracts(extend_native_completion_contracts(', 1)
        body = body[:start]+chunk+')\n\n'+body[end:];write(name, body)
    spec = importlib.util.spec_from_file_location('dc_contract_source', ROOT/name)
    module = importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
    canonical = module.make_openapi();api = load('docs/白皮书/contracts/openapi-control-v3.0.json')
    for schema in SCHEMAS+['OriginalResultMetadata']: api['components']['schemas'][schema] = deepcopy(canonical['components']['schemas'][schema])
    api['components']['securitySchemes']['ResultReadOnly'] = deepcopy(canonical['components']['securitySchemes']['ResultReadOnly'])
    routes = load('dev/src/api/internal/http/routes.json');registry = load('dev/planning/编号登记-v1.0.json')
    for index, (operation, method, path, anonymous) in enumerate(OPERATIONS, 109):
        code = f'INT-{index:03d}'
        if registry['operations'].get(operation, code) != code: raise ValueError('Preserve original operation ID')
        registry['operations'][operation] = code
        api['paths'].setdefault(path, {})[method.lower()] = deepcopy(canonical['paths'][path][method.lower()])
        route = {'operation_id': operation, 'method': method, 'path': path, 'handler': operation, 'anonymous': anonymous}
        old = next((r for r in routes['operations'] if r['operation_id'] == operation), None)
        if old is not None and old != route: raise ValueError('Preserve registered HTTP binding')
        if old is None: routes['operations'].append(route)
    for table, code in TABLES.items():
        if registry['tables'].get(table, code) != code: raise ValueError('Preserve original DATA ID')
        registry['tables'][table] = code
    routes['scope'] = '75 explicit control/content HTTP operations; original plaintext content transport only. Native read-only verification is mandatory even on the two independently authenticated routes. SOURCE_ONLY.'
    dump('docs/白皮书/contracts/openapi-control-v3.0.json', api);dump('dev/src/api/internal/http/routes.json', routes);dump('dev/planning/编号登记-v1.0.json', registry)
    raw = (ROOT/'docs/技术开发/database/migrations'/MIGRATION).read_bytes();target = ROOT/'dev/src/internal/persistence/migrations'/MIGRATION
    if target.exists() and target.read_bytes() != raw: raise ValueError('Registered SQL bytes are immutable; append 022+')
    if not target.exists(): target.write_bytes(raw)
    digest = hashlib.sha256(raw).hexdigest();migration = {'source': 'docs/技术开发/database/migrations/'+MIGRATION, 'source_sha256': digest,
        'migration': MIGRATION, 'migration_sha256': digest, 'policy': '021 original result encrypted chunks/read-only grant; SOURCE_ONLY, unapplied'}
    metadata = 'dev/src/internal/persistence/migrations/source-v21.json'
    if (ROOT/metadata).exists() and load(metadata) != migration: raise ValueError('Preserve registered migration metadata')
    if not (ROOT/metadata).exists(): dump(metadata, migration)
    name = 'scripts/技术开发数据.py';body = (ROOT/name).read_text(encoding='utf-8')
    body = body.replace('assets transfer_grants deletion_tasks"', 'assets transfer_grants deletion_tasks native_result_content native_result_content_chunks native_result_read_grants"')
    old = '19:"getJob registerAttemptDelivery recordBuyerReceipt cancelJob"'
    new = '19:"getJob registerAttemptDelivery recordBuyerReceipt cancelJob beginOriginalResultContent putOriginalResultChunk sealOriginalResultContent createOriginalResultReadGrant getOriginalResultContent getOriginalResultChunk getOriginalResultUploadStatus"'
    body = body.replace(old, new)
    if "'original_result_content_source':" not in body:
        addition = ("      'original_result_content_source':{'status':'SOURCE_ONLY_UNVERIFIED',\n"
            f"        'annex':'{ANNEX}','tables':{list(TABLES)!r},\n"
            "        'forward_source':'021_original_result_content.sql','migration_executed':False,\n"
            "        'original_plaintext_content_and_read_only_native_buyer_source_wired':True,'e2e_or_meter_authority':False,\n"
            "        'automatic_provider_daemon_and_sdk_ingress_complete':False,'compiled_or_executed':False},\n")
        body = body.replace("      'unproven_capabilities':", addition+"      'unproven_capabilities':", 1)
    write(name, body)
    name = 'dev/src/tests/integration/core_test.go';write(name, (ROOT/name).read_text(encoding='utf-8').replace('tables != 81 {', 'tables != 84 {'))
    return migration, {'planned_operations': len(registry['operations']), 'schemas': len(api['components']['schemas']), 'platform_tables': len(registry['tables']), 'control_http_operations': len(routes['operations'])}


def inventory():
    result = []
    for source in TESTS:
        pattern = r"^test\('([^']+)'" if source.endswith('.ts') else r'^func (Test\w+)\('
        for name in re.findall(pattern, (ROOT/source).read_text(encoding='utf-8'), re.M):
            result.append({'id': f'DC-T{len(result)+1:03d}', 'name': name, 'source': source, 'kind': 'NEW_DEFINITION', 'status': 'SOURCE_ONLY_NOT_EXECUTED'})
    return result


def progress(plan, stage, status, tests):
    totals = Counter(t['implementation_status'] for t in status['tasks'])
    lines = ['# R026 全量代码阶段进展', '', '**进行中，SOURCE_ONLY / 未验证。** 先全部生产代码、实际依赖和测试源码，再集中验证。103—109仅源码保存点，不是整轮完成。', '', '| 编号 | 已写内容 | 实际状态 |', '|---|---|---|']
    lines += [f"| {i['id']} | {i['title']} | {i['status']} |" for i in stage['implemented_items']]
    lines += ['', DETAIL, '', f'本次{len(tests)}项DC测试定义未执行；此前所有R026源码和测试定义继续未编译/运行。', '', '021只登记源码未迁移；001—021平台双份SQL/source元数据及原端侧DDL/source元数据发布后原字节冻结，后续追加022+。', '',
        '当前实施状态：'+'；'.join(f'{k} {v}' for k, v in sorted(totals.items()))+'；全部222子项未验收。代码状态与验收状态独立。', '', '| 编号 | 全部任务 | 当前状态 | 全部仍待收口子项 |', '|---|---|---|---|']
    current = {t['id']: t for t in status['tasks']}
    for task in plan['tasks']:
        t = current[task['id']];lines.append(f"| {task['id']} | {task['title']} | {t['implementation_status']} | {'、'.join(t['remaining_step_ids'])} |")
    write('dev/progress/R026-全量代码阶段进展.md', '\n'.join(lines)+'\n')
    lines = ['# 全量代码优先实施清单', '', DETAIL, '', '| 子项 | 任务 | 未收口需求 | 代码状态 | 验收 |', '|---|---|---|---|---|']
    for task in plan['tasks']:
        for step in task['steps']: lines.append(f"| {step['id']} | {task['title']} | {step['code_requirement']} | {step['code_status']} | {step['acceptance_status']} |")
    write('dev/全量代码优先实施清单.md', '\n'.join(lines)+'\n')


def documentation(tests, counts):
    section(ANNEX, '## DC 测试源码定义索引', '| 编号 | 定义 | 源码 | 状态 |\n|---|---|---|---|\n'+'\n'.join(f"| {t['id']} | {t['name']} | {t['source']} | 未编译/未运行 |" for t in tests))
    zh = (f'R026新增[DC-001—045]({ANNEX})：PLAINTEXT/STANDARD原结果分块AEAD保存/完整核验、原提供方上传与原状态恢复、当前买家session绑定短时只读授权、原生加密接收和明确导出、React/Taro与Web显式本地核对签收、取消拒绝/过期退役及七HTTP源码。'
          f"021/DATA082—084仅源码，当前{counts['planned_operations']}规划/{counts['schemas']} schema/{counts['platform_tables']}平台表/{counts['control_http_operations']}控制HTTP；{len(tests)}项新测试定义未编译/运行，021未迁移。"
          '平台能解密此档内容，不是E2E盲转；SDK/提供方守护编排、独立计量生产/真实资格、E2E/媒体/同卖家重派/持久超时、完整费用冲正/续租/到账对账、历史恢复与CK/GF继续待。'
          '全部56任务222子项开放，先全部生产源码/实际接线/测试源码，再集中验证和重录媒体；本次不是整轮完成。')
    en = (f'R026 adds [DC-001–045]({ANNEX}): original PLAINTEXT/STANDARD chunked AEAD storage/full verification, provider upload/original status recovery, current-session scoped read-only delegation, native encrypted receipt/explicit export, React/Taro and Web local review, cancellation/rejection/expiry retirement and seven HTTP operations. '
          f"021/DATA082–084 are source only; {counts['planned_operations']} planned operations/{counts['schemas']} schemas/{counts['platform_tables']} tables/{counts['control_http_operations']} control HTTP operations. {len(tests)} new test definitions are uncompiled/unexecuted; 021 is unapplied. "
          'The platform can decrypt this profile: it is not E2E. SDK/provider-daemon wiring, the independent meter producer/real qualification, E2E/media/same-seller retry/durable expiry, full fee reversals/renewals/payout/reconciliation, reviewed history and CK/GF remain open. '
          'All 56 tasks/222 steps remain open. Complete source/actual wiring/test source precede batch validation and fresh media; this is not a completed round.')
    marker = '## R026 DC-001—045 原结果传输与本地接收 / Original result transport and local receipt source'
    for name, content in [('dev/repository/README.zh-CN.md.in', zh), ('dev/repository/README.en.md.in', en)]:
        section(name, marker, content);body = (ROOT/name).read_text(encoding='utf-8')
        body = re.sub(r'\n<!-- R026-[A-Z-]+-CURRENT -->.*?<!-- /R026-[A-Z-]+-CURRENT -->\n', '\n', body, flags=re.S)
        body = body.replace('以上 DA 说明', '以上 DC 说明').replace('The DA notice above', 'The DC notice above')
        title, rest = re.sub(r'\n{3,}', '\n\n', body).split('\n', 1);write(name, title+'\n\n<!-- R026-DC-CURRENT -->\n'+content+'\n<!-- /R026-DC-CURRENT -->\n'+rest)
    for name, content in [('CHANGELOG.zh-CN.md', zh), ('CHANGELOG.md', en)]:
        body = (ROOT/name).read_text(encoding='utf-8');body = re.sub(r'(?m)^- R026(?:新增| adds )\[DC-001[—–]045\].*\n', '', body)
        title, rest = body.split('\n', 1);write(name, title+'\n\n- '+content+'\n'+rest)
    for name in ['dev/repository/USAGE.md', 'dev/repository/SYSTEM_DESIGN.md']:
        section(name, marker, zh.replace('(docs/', '(../../docs/')+'\n\n'+en.replace('(docs/', '(../../docs/'))
    for name in ['dev/src/client/buyerresults/README.md', 'dev/src/client/results/README.md', 'dev/src/internal/invocation/README.md']:
        section(name, marker, zh.replace('(docs/', '(../../../../docs/'))
    section('dev/progress/README.md', marker, zh.replace('(docs/', '(../../docs/'))
    for name in ['AGENTS.md', 'dev/AGENTS.md']:
        section(name, marker, zh.replace('(docs/', '(../docs/' if name.startswith('dev/') else '(docs/')+
            '\n\n当前唯一增量整理器为'+TOOL+'；禁止重跑旧DA/RA/RT/RF/CK/GF/PH/PT/MP/MG/MS/WS/CH/CE/MW/RN整理器覆盖DC状态。'
            '001—021平台双份SQL/source元数据和原端侧DDL/source元数据发布登记后原字节冻结，只追加022+。'
            '当前不编译/测试/核验/迁移或启动DB、内核、模型、设备、浏览器；全部生产源码/实际依赖/测试源码后集中验证。'
            '禁止本地虚拟化，DEV55延后、DEV32无真实证明关闭；浏览器用后关闭不用页面。')


def main():
    migration, counts = register_source();tests = inventory()
    plan, stage, status = [load(n) for n in ['dev/progress/code-first-plan.json', 'dev/progress/R026-code-stage.json', 'dev/progress/implementation-status.json']]
    integration = {'status': 'SOURCE_ONLY_UNVERIFIED', 'requirements': 'DC001—045', 'annex': ANNEX,
        'original_plaintext_chunk_encrypted_storage_and_full_verification_source_wired': True,
        'provider_original_upload_journal_and_status_recovery_source_wired': True,
        'current_buyer_session_read_only_delegation_and_retirement_source_wired': True,
        'native_encrypted_receive_protocol_raw_export_local_review_source_wired': True,
        'react_taro_web_memory_only_read_packet_explicit_decision_source_wired': True,
        'automatic_provider_daemon_and_sdk_ingress_complete': False, 'e2e_or_trusted_meter_authority': False,
        'actual_channel_device_and_meter_qualification': False, 'compiled_or_executed': False,
        'migration_executed': False, 'new_media_captured': False, 'full_development_round_complete': False,
        'forward_migration_source': migration, 'test_definition_count': len(tests), **counts,
        'remaining': ['SDK入口/付款前授权/提供方真实常驻接单和自动交付编排', '独立计量生产/真实资格/同卖家重派/持久签收超时/争议',
            'E2E双端信任/盲转/签名回执/换端及媒体/租赁最终账单', '完整费用冲正/续租/佣金/渠道到账对账、独立历史恢复及CK/GF',
            '全部56/222生产源码和测试源码后集中验证与重录媒体；DEV55延后，DEV32无证明关闭']}
    for record in (plan, stage): record['original_result_content_source_integration'] = deepcopy(integration)
    for task in plan['tasks']:
        if task['id'] in TASKS:
            task['original_result_content_source_limit'] = DETAIL
            for step in task['steps']: step['implementation_refs'] = sorted(set(step.get('implementation_refs', [])+REFS))
    for task in status['tasks']:
        if task['id'] in TASKS:
            task['original_result_content_source_limit'] = DETAIL;task['current_source_refs'] = sorted(set(task.get('current_source_refs', [])+REFS));task['current_validation_status'] = 'NOT_EXECUTED'
    status['activity_source'] = '2026-11-07 CODE_FIRST：DC001—045 / IMP103—109；'+DETAIL
    for number, title, state in ITEMS:
        entry = {'id': 'IMP-R026-'+number, 'title': title, 'status': state}
        stage['implemented_items'] = [old for old in stage['implemented_items'] if old['id'] != entry['id']]+[entry]
    stage.update({'complete': False, 'validation_started_after_user_policy': False, 'tasks': deepcopy(status['tasks']),
        'original_result_content_migration_source': migration, 'result_content_test_definitions': {'tests': tests, 'ids': [t['id'] for t in tests], 'status': 'SOURCE_ONLY_NOT_EXECUTED'}})
    stage['source_files'] = sorted(set(stage.get('source_files', [])+REFS))
    for name, value in [('dev/progress/code-first-plan.json', plan), ('dev/progress/R026-code-stage.json', stage), ('dev/progress/implementation-status.json', status)]: dump(name, value)
    progress(plan, stage, status, tests);documentation(tests, counts)
    print(json.dumps({'source_document_generation': 'DC001–045 / IMP103–109', 'unexecuted_test_definitions': len(tests), **counts,
        'forward_source': MIGRATION, 'compiler_test_validator_migration_runtime': 'NOT_RUN', 'all_tasks_open': len(plan['tasks']),
        'all_steps_open': sum(len(t['steps']) for t in plan['tasks'])}, ensure_ascii=False))


if __name__ == '__main__':
    main()
