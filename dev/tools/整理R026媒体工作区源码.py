"""Text inventory only: extend current CH/RN/CE/MW records; never run checks."""
from collections import Counter
from copy import deepcopy
import json
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[2]
ANNEX = 'docs/技术开发/附录-租内媒体工作区与结果托管源码.md'
TOOL = 'dev/tools/整理R026媒体工作区源码.py'
TEST_FILES = [
    'dev/src/client/execution/media_workspace_test.go',
    'dev/src/client/protocol/rentalaccess/media_test.go',
    'dev/src/client/mediaworkspace/owner_test.go',
    'dev/src/client/adapters/comfyworker/rental_media_test.go',
]
REFS = sorted(TEST_FILES + [ANNEX, TOOL,
    'dev/src/client/execution/schema-003-media.sql',
    'dev/src/client/execution/schema-003-media.source.json',
    'dev/src/client/execution/media_migration.go',
    'dev/src/client/execution/media_workspace.go',
    'dev/src/client/execution/store.go',
    'dev/src/client/execution/migration.go',
    'dev/src/client/execution/rental_invocation.go',
    'dev/src/client/protocol/rentalaccess/media.go',
    'dev/src/client/mediaworkspace/owner.go',
    'dev/src/client/mediaworkspace/deadline.go',
    'dev/src/client/mediaworkspace/README.md',
    'dev/src/client/adapters/comfyworker/rental_media.go',
    'dev/src/client/adapters/comfyworker/custody.go',
    'dev/src/client/adapters/comfyworker/process.go',
    'dev/src/client/node/store.go',
    'dev/src/client/node/media_workspaces.go',
    'dev/src/client/node/rental_comfy.go',
    'dev/src/client/internal/rentaltest/fixture.go',
])
ITEMS = [
    ('041', '原租内媒体工作区与native003前向账证源码', 'WS-001—006/012：同原账本身份/容量、一次票据、不可变元数据、重开隔离及清理前置；未运行迁移'),
    ('042', '原节点目录持有、清理与恢复边界源码', 'WS-019—026：锚/有限文件/实际join/退役删除/清理专用恢复引用；缺独立停工对账和真实系统证据'),
    ('043', '认证媒体输入和原租赁专用适配器源码', 'WS-007—016/020：RAM GCM、4素材/64KiB块、唯一帧消费、原inbox和ProviderRental接线；非公开SDK/完整会话监督'),
    ('044', '原媒体结果密钥托管与读取撤销源码', 'WS-017—019/025：一次Receipt、原EXPORT_SELF每读/seek、句柄初始化/关闭join及原进程结束撤钥；非买方ACK/计费资格'),
    ('045', 'WS编号、边界测试源码与双语文档更新', '仅23项新增测试定义/文档整理，未编译、测试、迁移或运行；全56/222代码齐备后统一验证，本检查点非完整开发轮'),
]
STEPS = {
    'DEV-18': {'DEV-18.01', 'DEV-18.03', 'DEV-18.04'},
    'DEV-21': {'DEV-21.03'},
    'DEV-22': {'DEV-22.03', 'DEV-22.04'},
    'DEV-24': {'DEV-24.01'},
    'DEV-33': {'DEV-33.03', 'DEV-33.04'},
    'DEV-35': {'DEV-35.03', 'DEV-35.04'},
    'DEV-36': {'DEV-36.01', 'DEV-36.02', 'DEV-36.03', 'DEV-36.04'},
    'DEV-37': {'DEV-37.01', 'DEV-37.02', 'DEV-37.03', 'DEV-37.04'},
    'DEV-53': {'DEV-53.01', 'DEV-53.03'},
    'DEV-54': {'DEV-54.01', 'DEV-54.02', 'DEV-54.03'},
    'DEV-56': {'DEV-56.01', 'DEV-56.04'},
}
DETAIL = ('新增原账本媒体工作区/容量和native003前向源码、原根目录持有/退役删除/清理专用恢复、'
          '认证RAM媒体输入、专用原租内适配器和结果钥托管/读取撤销源码；未编译或运行。'
          '公开媒体SDK/资产/最终ACK、完整媒体会话监督/独立停工对账/ready/可信小时水位/付费续租/平台释放、'
          '完整实际运行包/OS低权限/E2E/性能仍待；全部56任务222子项开放，代码齐备后统一验证。')


def load(name):
    return json.loads((ROOT / name).read_text(encoding='utf-8'))


def write(name, text):
    path = ROOT / name
    prior = path.read_bytes() if path.exists() else b''
    ending = '\r\n' if b'\r\n' in prior else '\n'
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(text.replace('\r\n', '\n').replace('\n', ending).encode('utf-8'))


def dump(name, record):
    write(name, json.dumps(record, ensure_ascii=False, indent=2) + '\n')


def section(name, marker, text):
    body = (ROOT / name).read_text(encoding='utf-8') if (ROOT / name).exists() else ''
    if marker in body:
        start = body.index(marker)
        end = body.find('\n## ', start + len(marker))
        body = body[:start].rstrip() + ('\n\n' + body[end + 1:] if end >= 0 else '')
    write(name, body.rstrip() + '\n\n' + marker + '\n\n' + text.strip() + '\n')


def main():
    plan, stage, status = [load(name) for name in [
        'dev/progress/code-first-plan.json', 'dev/progress/R026-code-stage.json',
        'dev/progress/implementation-status.json']]
    tests = []
    for source in TEST_FILES:
        for name in re.findall(r'^func (Test\w+)\(', (ROOT / source).read_text(encoding='utf-8'), re.M):
            tests.append({'id': f'WS-T{len(tests)+1:03d}', 'name': name,
                          'source': source, 'status': 'SOURCE_ONLY_NOT_EXECUTED'})
    remaining = [
        'WS-027：公开OpenAI/Anthropic兼容媒体SDK、本人资产授权/响应映射、最终ACK及独立留存',
        'WS-028：完整媒体会话监督、独立停工恢复对账、双ready、可信小时水位、付费续租与原平台释放',
        'WS-029：完整真实Python/Torch/codec/node/model/recipe包、OS独立受限身份、LP/E2E/性能资格',
        'WS-030：全部56/222剩余生产代码/实际依赖接线/测试源码，再统一编译测试调试和组合验收',
    ]
    integration = {
        'status': 'SOURCE_ONLY_UNVERIFIED', 'requirements': 'WS-001—030', 'annex': ANNEX,
        'native_schema_version_source': 3,
        'native_migration_source': 'dev/src/client/execution/schema-003-media.source.json',
        'original_workspace_reservation_source_written': True,
        'original_node_workspace_owner_source_wired': True,
        'authenticated_local_media_input_source_written': True,
        'typed_rental_media_adapter_native_source_wired': True,
        'result_key_custody_and_finalizer_source_wired': True,
        'cleanup_only_recovery_reference_source_written': True,
        'public_media_admission_wired': False, 'full_media_session_supervisor_wired': False,
        'independent_physical_stop_reconciliation_wired': False,
        'actual_runtime_bundle_present': False, 'network_e2e_qualified': False,
        'low_privilege_qualified': False, 'compiled_or_executed': False,
        'native_migration_executed': False, 'sale_or_cleanup_qualified': False,
        'test_definition_count': len(tests), 'remaining': remaining,
    }
    for record in (plan, stage):
        record['media_workspace_source_integration'] = deepcopy(integration)
        record['native_migration_sources'] = [
            'dev/src/client/execution/schema-002-rental.source.json',
            integration['native_migration_source']]
        for name in ['comfy_host_source_integration', 'comfy_media_source_integration',
                     'comfy_environment_source_integration']:
            record[name].update({
                'typed_rental_media_adapter_native_source_wired': True,
                'original_workspace_owner_source_wired': True,
                'result_key_custody_source_wired': True,
                'public_media_admission_wired': False,
                'media_workspace_source_annex': ANNEX,
                'remaining': remaining})
    for task in plan['tasks']:
        if task['id'] not in STEPS:
            continue
        task['code_status'] = 'IN_PROGRESS'
        for step in task['steps']:
            if step['id'] in STEPS[task['id']]:
                step['implementation_refs'] = sorted(set(step.get('implementation_refs', []) + REFS))
                if step['code_status'] == 'OPEN':
                    step['code_status'] = 'IN_PROGRESS'
                step['current_source_limit'] = DETAIL
    for task in status['tasks']:
        if task['id'] not in STEPS:
            continue
        task['implementation_status'] = 'IN_PROGRESS'
        task['current_source_refs'] = sorted(set(task.get('current_source_refs', []) + REFS))
        task['current_code_components'] = sorted(set(task.get('current_code_components', []) + ['IMP-R026-'+n for n, _, _ in ITEMS]))
        task['media_workspace_source_limit'] = DETAIL
        task['current_validation_status'] = 'NOT_EXECUTED'
        if task['id'] in {'DEV-22', 'DEV-33', 'DEV-36', 'DEV-37'}:
            task['remaining_detail'] = DETAIL
    status['activity_source'] = '2026-11-06 CODE_FIRST：WS-001—026 / IMP041—045新增源码；' + DETAIL
    for number, title, state in ITEMS:
        item = {'id': 'IMP-R026-'+number, 'title': title, 'status': state}
        stage['implemented_items'] = [old for old in stage['implemented_items'] if old['id'] != item['id']] + [item]
    stage['source_files'] = sorted(set(stage.get('source_files', []) + REFS))
    stage['tasks'] = deepcopy(status['tasks'])
    stage['media_workspace_test_definitions'] = {'tests': tests,
        'ids': [t['id'] for t in tests], 'status': 'SOURCE_ONLY_NOT_EXECUTED'}
    for name, record in [('dev/progress/code-first-plan.json', plan),
                         ('dev/progress/R026-code-stage.json', stage),
                         ('dev/progress/implementation-status.json', status)]:
        dump(name, record)
    counts = Counter(t['implementation_status'] for t in status['tasks'])
    lines = ['# R026 全量代码阶段进展', '',
        '**进行中，SOURCE_ONLY / 未验证。** 完整生产代码、实际依赖接线和测试源码齐备后统一验证。新增041—045只是源码检查点，不是开发轮完成；历史内容按原范围阅读。', '',
        '| 编号 | 本阶段已写内容 | 实际状态 |', '|---|---|---|']
    lines += [f"| {i['id']} | {i['title']} | {i['status']} |" for i in stage['implemented_items']]
    lines += ['', f'新增[WS-001—030](../../{ANNEX})及{len(tests)}项测试源码定义。原RN67/CE31/MW44/CH29也未执行；本次只登记native003源，无迁移。', '',
        '当前状态：' + '；'.join(f'{k} {v}' for k, v in sorted(counts.items())) + '；全部222子项保持未验收。', '',
        '| 编号 | 任务 | 当前状态 | 全部仍待收口子项 |', '|---|---|---|---|']
    current = {t['id']: t for t in status['tasks']}
    for task in plan['tasks']:
        t = current[task['id']]
        lines.append(f"| {task['id']} | {task['title']} | {t['implementation_status']} | {'、'.join(t['remaining_step_ids'])} |")
    write('dev/progress/R026-全量代码阶段进展.md', '\n'.join(lines)+'\n')
    lines = ['# 全量代码优先实施清单', '', DETAIL, '',
        '先完成全部生产代码、真实依赖接线和测试源码，再批量验证。代码状态与验收分开；全部222子项未验收。', '',
        '| 子项 | 任务 | 未收口需求 | 代码状态 | 验收 |', '|---|---|---|---|---|']
    for task in plan['tasks']:
        for step in task['steps']:
            lines.append(f"| {step['id']} | {task['title']} | {step['code_requirement']} | {step['code_status']} | {step['acceptance_status']} |")
    write('dev/全量代码优先实施清单.md', '\n'.join(lines)+'\n')
    section(ANNEX, '## WS 测试源码定义索引',
        '| 编号 | 测试定义 | 源码 | 状态 |\n|---|---|---|---|\n' + '\n'.join(
            f"| {t['id']} | {t['name']} | {t['source']} | 未编译/未运行 |" for t in tests))
    note = (f'R026新增[WS-001—030]({ANNEX})：原租赁媒体工作区/native003元数据预留、'
            '原节点目录持有/退役删除/清理专用恢复、认证RAM媒体输入、专用子调用适配器和结果钥托管/读取撤销。'
            f'{len(tests)}项新测试定义均未编译/运行；公开SDK、完整媒体会话监督/独立停工对账/ready/续租/可信水位/平台释放、'
            '完整实际包/低权限/E2E/性能仍待。全56任务/222子项开放，只登记native003源，未迁移/真实执行；'
            '完整代码后统一验证，源码检查点不是开发轮完成。')
    english = (f'R026 adds [WS-001–030]({ANNEX}): original rental media workspace reservations/native '
               'migration 003 source, node-owned directory lifetimes/retirement/cleanup-only recovery, '
               'authenticated RAM media input, typed child adapter and result-key custody/revocation. '
               f'All {len(tests)} new test definitions are uncompiled/unexecuted. Public SDK/media sessions, '
               'independent stopped-work reconciliation/readiness/paid renewal/hour meters/platform release, '
               'complete runtime archives/OS low privilege/E2E/performance remain open. All 56 tasks/222 steps '
               'remain unaccepted. Native 003 is registered as source only; no migration or runtime was executed. '
               'Complete all source before batch validation; this source checkpoint is not a completed development round.')
    marker = '## R026 WS-001—030 原媒体工作区与结果托管源码 / Original media workspace and custody source'
    for name, text in [('dev/repository/README.zh-CN.md.in', note), ('dev/repository/README.en.md.in', english)]:
        section(name, marker, text)
        body = (ROOT / name).read_text(encoding='utf-8')
        body = re.sub(r'\n<!-- R026-WS-CURRENT -->.*?<!-- /R026-WS-CURRENT -->\n', '\n', body, flags=re.S)
        title, rest = body.split('\n', 1)
        write(name, title+'\n\n<!-- R026-WS-CURRENT -->\n'+text+'\n<!-- /R026-WS-CURRENT -->\n'+rest)
    for name in ['dev/repository/USAGE.md', 'dev/repository/SYSTEM_DESIGN.md']:
        section(name, marker, note.replace('(docs/', '(../../docs/')+'\n\n'+english.replace('(docs/', '(../../docs/'))
    for name, text in [('CHANGELOG.zh-CN.md', note), ('CHANGELOG.md', english)]:
        body = (ROOT/name).read_text(encoding='utf-8')
        if text not in body:
            title, rest = body.split('\n', 1)
            write(name, title+'\n\n- '+text+'\n'+rest)
    for name in ['AGENTS.md', 'dev/AGENTS.md']:
        section(name, marker, note.replace('(docs/', '(../docs/' if name.startswith('dev/') else '(docs/')+
            '\n\n当前唯一增量整理器为'+TOOL+'；禁止重跑旧CH/CE/MW/RN整理器覆盖本次WS元数据。'
            '原001—015双份SQL、schema.sql/host-schema.sql/native002原字节保持，已登记native003以后也只加前向版本。'
            '当前不编译、测试、迁移、运行内核/模型/数据库/设备/浏览器；全量代码和测试源码后统一验证。'
            '禁止本地虚拟化，DEV-55延后、DEV-32无证明关闭，浏览器使用后关闭不用页面。')
    for name, prefix in [
        ('dev/src/client/mediaworkspace/README.md', '../../../../'),
        ('dev/src/client/node/README.md', '../../../../'),
        ('dev/src/client/adapters/comfyworker/README.md', '../../../../../'),
        ('dev/src/client/protocol/rentalaccess/README.md', '../../../../../'),
        ('dev/progress/README.md', '../../')]:
        section(name, marker, note.replace('(docs/', '('+prefix+'docs/'))
    for name in ['docs/技术开发/附录-ComfyUI原环境持有与本机进程接线源码.md',
                 'docs/技术开发/附录-ComfyUI私有媒体执行与本地加密源码.md',
                 'docs/技术开发/附录-供给端内核封装与替换规范.md',
                 'docs/技术开发/附录-小时租赁授权与累计计量源码细则.md',
                 'docs/技术开发/00-技术文档导航与实施基线.md']:
        section(name, marker, note.replace('(docs/技术开发/', '('))
    print(f'Document inventory only: IMP041–045 / WS001–030 / {len(tests)} source test definitions; all 56/222 open. No compiler/test/validator/migration/runtime invoked.')


if __name__ == '__main__':
    main()
