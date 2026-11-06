"""PD source/document authoring only; never compile/test/check/migrate/run."""
from collections import Counter
from copy import deepcopy
import json
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[2]
ANNEX = 'docs/技术开发/附录-原生提供方常驻接单与签名续期源码.md'
TOOL = 'dev/tools/整理R026常驻提供方源码.py'
RULES = 'dev/开发轮次执行规则.md'
CONTRACT = 'docs/技术开发/contracts/native-provider-source-v1.json'
TESTS = ['dev/src/client/execution/command_renewal_test.go',
         'dev/src/client/supervisor/resident_test.go',
         'dev/src/client/node/host_progress_test.go',
         'dev/src/client/node/provider_test.go',
         'dev/src/client/cmd/p11-node/provider_test.go']
REFS = [ANNEX, TOOL, RULES, CONTRACT,
        'dev/src/client/execution/command.go',
        'dev/src/client/supervisor/supervisor.go', 'dev/src/client/supervisor/resident.go',
        'dev/src/client/supervisor/text_output.go', 'dev/src/client/node/host_store.go',
        'dev/src/client/node/host_commands.go', 'dev/src/client/node/host_agent.go',
        'dev/src/client/node/host_kernels.go', 'dev/src/client/node/host_native.go',
        'dev/src/client/node/host_native_test.go', 'dev/src/client/node/host_test.go',
        'dev/src/client/node/host_policy.go', 'dev/src/client/node/control.go',
        'dev/src/client/node/host_progress.go', 'dev/src/client/node/host_retire.go',
        'dev/src/client/node/provider.go', 'dev/src/client/cmd/p11-node/main.go']+TESTS
TASKS = {'DEV-'+str(n).zfill(2) for n in [1, 6, 7, 8, 17, 18, 19, 20, 21, 23, 24, 26, 27, 37, 38, 51, 52, 53, 54, 56]}
DETAIL = ('PD补入原生LLM显式host-run常驻接单、独立本地批准/物理门闩预热、冻结内核与逐原manifest绑定、'
          '单控制owner/真实本地资源状态心跳、固定原claim、原进度持久回执/独立pin签名续期、'
          '初次RECEIVE与当前续期摘要分离、原结果上传/清理独立恢复、READY及原清理ACK后本地缓存退役和实际join源码。'
          '仅PLAINTEXT/STANDARD原生文本；115规划控制操作/150 schema/84平台表/75控制HTTP及3个SDK数据面入口不变，无SQL或迁移。'
          'ComfyUI/LMStudio/LMA对应常驻编排、实时SSE、E2E/媒体/独立计量与真实资格、SDK付款前授权/获准PayLauncher、'
          '原空claim版本恢复/旧进程托管/容量轮换和崩溃缓存回收、同卖家重派/持久签收超时/争议、完整费用冲正/结算对账及CK/GF仍待。'
          '全部56任务222子项保留；新测试定义未编译/运行，新媒体未录；先完整生产源码/实际依赖/测试源码后集中验证，本次不是开发轮完成。')
ITEMS = [
    ('116', '常驻内核与后续原任务绑定源码', 'PD005—015：主人固定包/GPU/全部批准，维护预热、冻结后端/并发有界manifest登记和逐单绑定；未运行'),
    ('117', '原进度回执与签名续期源码', 'PD026—030：原序号/原键先写、回执先存、独立pin/同owner与live guard、STOP和重启不复活；未验证'),
    ('118', '原接收与续期清理身份源码', 'PD031—033：不可变RECEIVE/当前摘要分离、原RENEW回执核对、原清理投影；无DDL变更'),
    ('119', '原生常驻入口与控制协调源码', 'PD001—004/016—025/038—039：严格stdin/host-run、单控制owner、实际账本心跳、固定claim、暂失联暂停及实际join；未启动'),
    ('120', '原结果发布恢复与本地退役源码', 'PD034—040：结果/计算清理分离、同原结果重传、丢钥不重跑、READY和原cleanup ACK后销毁本地缓存；未验证'),
    ('121', 'PD测试定义与完整双语登记', 'PD041—045：生产链/网络未知/续期/清理/配置等源码定义及约束文档同步；全56/222仍未收口')]


def load(name):
    return json.loads((ROOT/name).read_text(encoding='utf-8'))


def write(name, body):
    p = ROOT/name
    old = p.read_bytes() if p.exists() else b''
    ending = '\r\n' if b'\r\n' in old else '\n'
    p.parent.mkdir(parents=True, exist_ok=True)
    p.write_bytes(body.replace('\r\n', '\n').replace('\n', ending).encode('utf-8'))


def dump(name, value):
    write(name, json.dumps(value, ensure_ascii=False, indent=2)+'\n')


def section(name, marker, body):
    p = ROOT/name
    text = p.read_text(encoding='utf-8') if p.exists() else ''
    if marker in text:
        start = text.index(marker)
        end = text.find('\n## ', start+len(marker))
        text = text[:start].rstrip()+('\n\n'+text[end+1:] if end >= 0 else '')
    write(name, text.rstrip()+'\n\n'+marker+'\n\n'+body.strip()+'\n')


def inventory():
    definitions = []
    for source in TESTS:
        for name in re.findall(r'^func (Test\w+)\(', (ROOT/source).read_text(encoding='utf-8'), re.M):
            definitions.append({'id': f'PD-T{len(definitions)+1:03d}', 'name': name,
                                'source': source, 'kind': 'NEW_DEFINITION', 'status': 'SOURCE_ONLY_NOT_EXECUTED'})
    return definitions


def source_contract(tests):
    value = {
        'profile_id': 'P11_NATIVE_PROVIDER_SOURCE_1', 'requirements': 'PD-001—045',
        'status': 'SOURCE_ONLY_UNVERIFIED', 'compiled_or_executed': False,
        'authority': ANNEX, 'development_policy': RULES, 'source_refs': REFS,
        'entry': {'command': 'p11-node host-run --state <original-private-directory>',
                  'configuration': 'P11_NATIVE_PROVIDER_1', 'stdin_maximum_bytes': 16384,
                  'automatic_initialization_installation_qualification': False},
        'scope': {'engine': 'LLAMA_CPP_NATIVE', 'mode': 'PLAINTEXT', 'privacy': 'STANDARD',
                  'same_installed_backend_future_original_manifests': True,
                  'public_content_listener': False, 'cpu_or_privacy_fallback': False,
                  'comfy_lmstudio_lma_daemons_complete': False, 'e2e_or_live_sse_complete': False},
        'original_execution': {'durable_claim_before_network': True, 'empty_claim_reuses_key': True,
                               'device_version_automatically_rewritten': False,
                               'progress_receipt_durable_before_renewal': True,
                               'independent_pin_same_owner_live_guard_required': True,
                               'receive_hash_separate_from_current_hash': True,
                               'restart_restores_live_guard_or_replays_input': False,
                               'cleanup_or_timeout_mints_release': False},
        'custody': {'remote_ready_and_original_cleanup_ack_before_local_retirement': True,
                    'output_can_outlive_compute': True, 'lost_keys_reexecute': False,
                    'automatic_buyer_acceptance_trusted_usage_payment_or_settlement': False},
        'shutdown': {'control_publisher_watchdog_and_native_child_actually_joined': True,
                     'late_submit_metadata_counted_before_lifecycle_done': True,
                     'close_timeout_releases_original_ledger_keys_or_os_lock': False},
        'budgets_not_measurements': {'claim_milliseconds': 2000, 'progress_milliseconds': 5000,
                                     'coordinator_request_milliseconds': 5000, 'cleanup_poll_milliseconds': 500,
                                     'manifest_bindings': 4096, 'host_commands': 4096,
                                     'result_adapter_original_records': 1024, 'output_after_hard_deadline_seconds': 120,
                                     'native_close_seconds': 10, 'exit_cleanup_outbox_seconds': 5},
        'control_http_operations': 75, 'planned_control_operations': 115,
        'control_schemas': 150, 'platform_tables': 84, 'native_sdk_data_plane_operations': 3,
        'sql_changed_or_migrated': False, 'new_test_definitions': len(tests),
        'updated_historical_test_definition': 'TestHostNativeHistoricalCommandQueryDoesNotApproveWarmup',
        'full_scope_tasks': 56, 'full_scope_steps': 222,
        'test_admission': 'DENIED_UNTIL_ALL_IN_SCOPE_CODE_COMPLETE'}
    dump(CONTRACT, value)
    name = 'scripts/技术开发数据.py'
    body = (ROOT/name).read_text(encoding='utf-8')
    if "'native_provider_source':" not in body:
        addition = ("      'native_provider_source':{'status':'SOURCE_ONLY_UNVERIFIED',\n"
                    f"        'annex':'{ANNEX}','source_contract':'{CONTRACT}',\n"
                    "        'original_native_text_daemon_and_continuation_source_wired':True,\n"
                    "        'other_engine_daemons_live_sse_e2e_and_actual_qualification_complete':False,\n"
                    "        'original_receive_and_current_renewal_hash_separate':True,\n"
                    "        'remote_ready_and_cleanup_ack_before_local_retirement':True,\n"
                    "        'sql_changed_or_migrated':False,'compiled_or_executed':False},\n")
        body = body.replace("      'unproven_capabilities':", addition+"      'unproven_capabilities':", 1)
        write(name, body)


def progress(plan, stage, status, tests):
    totals = Counter(t['implementation_status'] for t in status['tasks'])
    lines = ['# R026 全量代码阶段进展', '',
             '**进行中，SOURCE_ONLY / 未验证。** 全部生产源码、实际依赖及测试源码齐备后集中验证。116—121只保存源码进展，不是开发轮完成。',
             '', '| 编号 | 已写内容 | 实际状态 |', '|---|---|---|']
    lines += [f"| {i['id']} | {i['title']} | {i['status']} |" for i in stage['implemented_items']]
    lines += ['', DETAIL, '', f'本次 {len(tests)} 项 PD 新测试定义全部未编译/运行；1 项历史测试按新的预热边界改名，夹具扩展不作为新增验收。',
              '', '无SQL变更；001—021平台双份SQL/source元数据和已登记端侧DDL/source元数据原字节冻结，后续仅022+。',
              '', '实施状态：'+'；'.join(f'{k} {v}' for k, v in sorted(totals.items()))+'；全部222子项未验收。',
              '', '| 编号 | 全部任务 | 当前状态 | 全部仍待收口子项 |', '|---|---|---|---|']
    current = {t['id']: t for t in status['tasks']}
    for task in plan['tasks']:
        t = current[task['id']]
        lines.append(f"| {task['id']} | {task['title']} | {t['implementation_status']} | {'、'.join(t['remaining_step_ids'])} |")
    write('dev/progress/R026-全量代码阶段进展.md', '\n'.join(lines)+'\n')
    lines = ['# 全量代码优先实施清单', '', DETAIL,
             '', '执行规则：[CF-01—05](开发轮次执行规则.md)。完整 R026 不按源码保存拆轮；所有未收口子项继续保留。',
             '', '| 子项 | 任务 | 未收口需求 | 代码状态 | 验收 |', '|---|---|---|---|---|']
    for task in plan['tasks']:
        for step in task['steps']:
            lines.append(f"| {step['id']} | {task['title']} | {step['code_requirement']} | {step['code_status']} | {step['acceptance_status']} |")
    write('dev/全量代码优先实施清单.md', '\n'.join(lines)+'\n')


def documentation(tests):
    marker = '## R026 PD-001—045 原生提供方常驻编排 / Original native provider orchestration source'
    zh = (f'R026新增[PD-001—045]({ANNEX})：原生host-run常驻接单、独立本地批准/GPU维护预热、冻结内核和逐原manifest绑定、单控制owner/实际本地资源心跳、'
          '原进度回执/签名续期、原RECEIVE与当前摘要分离、原结果上传/清理及READY和原清理ACK后本地缓存退役、关闭实际join源码。'
          f'{len(tests)}项新测试定义全部未编译/运行，1项历史定义改名。115规划控制操作/150 schema/84平台表/75控制HTTP及3个SDK数据面入口不变，无SQL或迁移。'
          '当前仅PLAINTEXT/STANDARD原生文本；其他内核守护编排/实时SSE/E2E/媒体/独立计量与真实资格、SDK付款前授权/获准PayLauncher、'
          '原空claim版本恢复/旧进程托管/容量轮换/崩溃密文回收、同卖家重派/持久超时/争议、完整结算对账和CK/GF仍待。'
          '全56任务222子项保留；CF01—05要求全量生产接线及测试源码后集中验证/重录媒体；本次不是开发轮完成。')
    en = (f'R026 adds [PD-001–045]({ANNEX}): explicit native host-run orchestration, independently approved local GPU warm-up, frozen resident kernels and per-original-manifest binding, one control owner with actual local-gate heartbeat, '
          'durable original progress/signed continuation, immutable RECEIVE versus current lease hashes, original output/cleanup recovery, local cache retirement after remote READY and cleanup ACK, and joined shutdown source. '
          f'{len(tests)} new test definitions are uncompiled/unexecuted; one historical definition is renamed. The 115 planned control operations/150 schemas/84 tables/75 control HTTP and three SDK data-plane registrations stay unchanged; no SQL/migration. '
          'Only PLAINTEXT/STANDARD native text is wired. Other engine daemons/live SSE/E2E/media/independent meters/real qualification, SDK pre-payment authorization/approved PayLauncher, empty-claim version recovery/old-process custody/capacity rotation/crash-ciphertext cleanup, same-seller retry/durable deadlines/disputes, full settlement/reconciliation and CK/GF remain open. '
          'All 56 tasks/222 steps stay in this round. Complete production wiring and test source precede batch validation and new media under CF01–05; this is not a completed development round.')
    section(ANNEX, '## PD 测试源码定义索引',
            '| 编号 | 定义 | 源码 | 状态 |\n|---|---|---|---|\n'+
            '\n'.join(f"| {t['id']} | {t['name']} | {t['source']} | 未编译/未运行 |" for t in tests))
    for name, content in [('dev/repository/README.zh-CN.md.in', zh), ('dev/repository/README.en.md.in', en)]:
        section(name, marker, content)
        body = (ROOT/name).read_text(encoding='utf-8')
        body = re.sub(r'\n<!-- R026-[A-Z-]+-CURRENT -->.*?<!-- /R026-[A-Z-]+-CURRENT -->\n', '\n', body, flags=re.S)
        body = body.replace('以上 SI 说明', '以上 PD 说明').replace('The SI notice above', 'The PD notice above')
        title, rest = re.sub(r'\n{3,}', '\n\n', body).split('\n', 1)
        write(name, title+'\n\n<!-- R026-PD-CURRENT -->\n'+content+'\n<!-- /R026-PD-CURRENT -->\n'+rest)
    for name, content in [('CHANGELOG.zh-CN.md', zh), ('CHANGELOG.md', en)]:
        body = (ROOT/name).read_text(encoding='utf-8')
        body = re.sub(r'(?m)^- R026(?:新增| adds )\[PD-001[—–]045\].*\n', '', body)
        title, rest = body.split('\n', 1)
        write(name, title+'\n\n- '+content+'\n'+rest)
    for name in ['dev/repository/USAGE.md', 'dev/repository/SYSTEM_DESIGN.md']:
        section(name, marker, zh.replace('(docs/', '(../../docs/')+'\n\n'+en.replace('(docs/', '(../../docs/'))
    section('dev/progress/README.md', marker, zh.replace('(docs/', '(../../docs/'))
    node_zh = zh.replace('(docs/', '(../../../../docs/')
    node_en = en.replace('(docs/', '(../../../../docs/')
    section('dev/src/client/node/README.md', marker, node_zh+'\n\n'+node_en)
    name = 'dev/src/client/node/README.md'
    body = (ROOT/name).read_text(encoding='utf-8').replace('`OpenNative` is an explicit trusted owner action after preparation.', '`OpenNative` is an explicit trusted owner action under independent local approval. The PD resident branch permits warm-up before its first claim; warm-up supplies no job authority.')
    write(name, body)
    section('docs/技术开发/00-技术文档导航与实施基线.md', marker, zh.replace('(docs/技术开发/', '('))
    for name in ['AGENTS.md', 'dev/AGENTS.md']:
        section(name, marker, zh.replace('(docs/', '(../docs/' if name.startswith('dev/') else '(docs/')+
                '\n\n最新唯一增量整理器为'+TOOL+'；禁止重跑旧SI/DC/DA/RA/RT/RF/CK/GF/PH/PT/MP/MG/MS/WS/CH/CE/MW/RN整理器覆盖当前PD状态。'
                '执行'+RULES+'的CF01—05；同一R026全部56/222生产代码/实际接线/测试源码齐备前，不编译/测试/核验/迁移或启动DB、模型、设备、浏览器；源码保存不拆轮。'
                '001—021平台双份SQL/source元数据与已登记端侧DDL/source元数据原字节冻结，只追加022+。禁止本地Docker/WSL/QEMU等虚拟化，DEV55延后、DEV32无证明关闭，浏览器使用后关闭不用页。')


def main():
    tests = inventory()
    source_contract(tests)
    plan, stage, status = [load(n) for n in ['dev/progress/code-first-plan.json', 'dev/progress/R026-code-stage.json', 'dev/progress/implementation-status.json']]
    integration = {'status': 'SOURCE_ONLY_UNVERIFIED', 'requirements': 'PD001—045', 'annex': ANNEX,
                   'source_contract': CONTRACT, 'native_provider_entry_and_continuation_source_wired': True,
                   'original_result_retirement_source_wired': True, 'other_engine_daemons_live_sse_e2e_complete': False,
                   'automatic_buyer_acceptance_payment_trusted_usage_or_settlement': False,
                   'actual_channel_device_meter_qualification': False, 'compiled_or_executed': False,
                   'sql_changed_or_migrated': False, 'new_media_captured': False,
                   'full_development_round_complete': False, 'test_definition_count': len(tests),
                   'control_http_operations': 75, 'native_sdk_data_plane_operations': 3,
                   'planned_control_operations': 115, 'control_schemas': 150, 'platform_tables': 84,
                   'remaining': ['其他内核常驻编排/实时SSE/E2E/媒体/独立计量与真实资格',
                                 'SDK付款前授权/获准PayLauncher/持久签收超时/争议/同卖家重派',
                                 '原空claim版本恢复/旧进程托管/容量轮换/崩溃密文回收',
                                 '完整费用冲正/结算对账/历史导入与CK/GF',
                                 '同一56/222完整源码及测试源码后集中验证；DEV55延后/DEV32无证明关闭']}
    for record in (plan, stage):
        record['native_provider_source_integration'] = deepcopy(integration)
        if 'native_sdk_source_integration' in record:
            record['native_sdk_source_integration']['native_provider_source_wired_by'] = 'PD001—045'
    for task in plan['tasks']:
        if task['id'] in TASKS:
            task['native_provider_source_limit'] = DETAIL
            for step in task['steps']:
                step['implementation_refs'] = sorted(set(step.get('implementation_refs', [])+REFS))
    for task in status['tasks']:
        if task['id'] in TASKS:
            task['native_provider_source_limit'] = DETAIL
            task['current_source_refs'] = sorted(set(task.get('current_source_refs', [])+REFS))
            task['current_validation_status'] = 'NOT_EXECUTED'
    status['activity_source'] = '2026-11-07 CODE_FIRST：PD001—045 / IMP116—121；'+DETAIL
    for number, title, state in ITEMS:
        entry = {'id': 'IMP-R026-'+number, 'title': title, 'status': state}
        stage['implemented_items'] = [old for old in stage['implemented_items'] if old['id'] != entry['id']]+[entry]
    stage.update({'complete': False, 'validation_started_after_user_policy': False, 'tasks': deepcopy(status['tasks']),
                  'native_provider_test_definitions': {'tests': tests, 'ids': [t['id'] for t in tests], 'status': 'SOURCE_ONLY_NOT_EXECUTED'}})
    stage['source_files'] = sorted(set(stage.get('source_files', [])+REFS))
    for name, value in [('dev/progress/code-first-plan.json', plan), ('dev/progress/R026-code-stage.json', stage), ('dev/progress/implementation-status.json', status)]:
        dump(name, value)
    progress(plan, stage, status, tests)
    documentation(tests)
    print(json.dumps({'source_document_generation': 'PD001—045 / IMP116—121', 'unexecuted_test_definitions': len(tests),
                      'control_http_operations': 75, 'native_sdk_data_plane_operations': 3,
                      'compiler_test_validator_migration_runtime': 'NOT_RUN', 'all_tasks_open': len(plan['tasks']),
                      'all_steps_open': sum(len(t['steps']) for t in plan['tasks'])}, ensure_ascii=False))


if __name__ == '__main__':
    main()
