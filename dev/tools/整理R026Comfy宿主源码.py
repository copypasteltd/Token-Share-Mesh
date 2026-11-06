"""Document inventory only; never compile/import/validate/run product code.

Extends the MW/CE/RN checkpoint. Older inventory entry points must not be
replayed over this metadata. Test names are read as plain text, not discovered.
"""
from pathlib import Path
from collections import Counter
import json
import re

ROOT = Path(__file__).resolve().parents[2]
ANNEX = 'docs/技术开发/附录-ComfyUI原环境持有与本机进程接线源码.md'
BASE = 'dev/src/client/adapters/comfyworker'
TEST_FILES = [
    'dev/src/client/execution/rental_preparation_test.go',
    'dev/src/client/comfyenv/use_test.go',
    BASE + '/process_test.go',
    'dev/src/client/kernels/comfy-private/tests/test_control.py',
    'dev/src/client/kernels/comfy-private/tests/test_model_inventory.py',
]
PY_NEW = {
    'test_preparation_can_load_but_cannot_invoke_before_original_authorization',
    'test_expired_preparation_cannot_be_revived_by_a_paid_hard_deadline',
    'test_duplicate_authorization_never_extends_or_replaces_original_paid_bound',
    'test_authorization_ack_precedes_content_permission_on_the_other_pipe',
    'test_child_deadline_cannot_exceed_original_paid_bound',
    'test_model_pins_are_checked_without_importing_inference_packages',
    'test_damaged_models_and_path_aliases_never_gain_runtime_mapping',
}
REFS = sorted(set(TEST_FILES + [ANNEX,
    'dev/tools/整理R026Comfy宿主源码.py',
    'dev/src/client/execution/rental_preparation.go',
    'dev/src/client/comfyenv/use.go', 'dev/src/client/comfyenv/registry.go',
    'dev/src/client/node/kernels.go', 'dev/src/client/node/rental_comfy.go',
    'dev/src/client/node/store.go',
    'dev/src/client/kernels/comfy-private/mesh_comfy/__init__.py',
    'dev/src/client/kernels/comfy-private/mesh_comfy/environment.py',
    'dev/src/client/kernels/comfy-private/mesh_comfy/worker.py',
    'dev/src/client/kernels/comfy-private/mesh_comfy/wire.py',
] + [BASE + '/' + name for name in ['config.go', 'process.go', 'process_native.go',
     'process_windows.go', 'process_linux.go', 'wire.go', 'cipher.go']]))
ITEMS = [
    ('036', 'Comfy原租赁准备门闩与不可变环境运行持有源码', 'CH-001—015：同账本/原owner、已选全树、复制共状态、一次claim、漂移撤权和实际join后释放；未编译/运行'),
    ('037', '固定原生Comfy进程与三管道所有权源码', 'CH-016—022/027—032：固定包/模型集/GPU与最小环境、独立I/O和有界退出；无真实运行包/低权限资格'),
    ('038', 'PIPE_2准备/授权应答及双重截止源码', 'CH-023—029：准备只加载、原JWS/新鲜ALLOW、一次AUTHORIZE/ACK、原控制单调截止；不代替付费续租'),
    ('039', '原ProviderRental和节点关闭链的Comfy准备启动源码', '原节点registry/ledger实际接线，不接公开原始媒体；CH-033—036租内Backend/工作区/公开交付/ready及资金释放仍待'),
    ('040', 'CH编号、跨层边界测试源码及双语文档', '仅测试定义和文档，未执行编译/测试/迁移/模型/设备；原56/222完整代码后统一验证，本检查点不是完整开发轮'),
]
DETAIL = ('新增原Comfy环境运行持有、同账本准备回调、固定原生启动/三管道/实际join、'
          'PIPE_2准备与一次原授权ACK/持续单调截止及ProviderRental源码，未编译或运行。'
          'CH-033—036原租内媒体Backend/认证输入/工作区与实际恢复清理、公开协议/资产交付、'
          '完整运行包/低权限/E2E/性能、独立ready与计量/付费续租/平台释放仍待；全56/222保持开放。')
STEPS = {
    'DEV-18': {'DEV-18.01', 'DEV-18.03'},
    'DEV-21': {'DEV-21.01', 'DEV-21.03', 'DEV-21.04'},
    'DEV-22': {'DEV-22.01', 'DEV-22.03', 'DEV-22.04'},
    'DEV-24': {'DEV-24.01'},
    'DEV-33': {'DEV-33.01', 'DEV-33.02'},
    'DEV-35': {'DEV-35.02', 'DEV-35.03'},
    'DEV-36': {'DEV-36.01', 'DEV-36.03'},
    'DEV-53': {'DEV-53.01', 'DEV-53.03'},
    'DEV-54': {'DEV-54.01', 'DEV-54.03'},
    'DEV-56': {'DEV-56.01'},
}


def load(name):
    return json.loads((ROOT / name).read_text(encoding='utf-8'))


def write(name, text):
    path = ROOT / name
    old = path.read_bytes() if path.exists() else b''
    ending = '\r\n' if b'\r\n' in old else '\n'
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(text.replace('\r\n', '\n').replace('\n', ending).encode('utf-8'))


def dump(name, value):
    write(name, json.dumps(value, ensure_ascii=False, indent=2) + '\n')


def section(name, marker, text):
    body = (ROOT / name).read_text(encoding='utf-8')
    if marker in body:
        start = body.index(marker)
        end = body.find('\n## ', start + len(marker))
        body = body[:start].rstrip() + ('\n\n' + body[end + 1:] if end >= 0 else '')
    write(name, body.rstrip() + '\n\n' + marker + '\n\n' + text.strip() + '\n')


def test_index():
    rows = []
    for source in TEST_FILES:
        pattern = r'^func (Test\w+)\(' if source.endswith('.go') else r'^    def (test_\w+)\('
        for name in re.findall(pattern, (ROOT / source).read_text(encoding='utf-8'), re.M):
            if source.endswith('.py') and name not in PY_NEW:
                continue
            rows.append({'id': f'CH-T{len(rows) + 1:03d}', 'name': name,
                         'source': source, 'status': 'SOURCE_ONLY_NOT_EXECUTED'})
    return rows


def main():
    plan, stage, status = [load(name) for name in [
        'dev/progress/code-first-plan.json', 'dev/progress/R026-code-stage.json',
        'dev/progress/implementation-status.json']]
    tests = test_index()
    remaining = [
        'CH-033：原租内媒体inbox、认证输入、工作区创建前登记/锚/钥/配额与实际恢复清理',
        'CH-034：正式媒体Backend、OpenAI/Anthropic媒体/资产权限/结果交付与最终ACK',
        'CH-035：完整真实Python/Torch/codec/node/model/recipe包、低权限/E2E、向量和性能',
        'CH-036：独立ready证据生产、可信小时水位/付费续租和原平台owner/预算/资金释放',
        '全部56/222剩余完整代码和测试源码，然后统一编译/测试/调试/真实组合验收',
    ]
    integration = {'status': 'SOURCE_ONLY_UNVERIFIED', 'requirements': 'CH-001—036',
        'annex': ANNEX, 'private_profile': 'P11_COMFY_PRIVATE_PIPE_2',
        'private_release': 'comfy-private-2', 'original_runtime_reference_source_written': True,
        'fixed_native_launcher_source_written': True, 'original_node_preparation_source_wired': True,
        'actual_runtime_bundle_present': False, 'public_media_admission_wired': False,
        'rental_media_backend_wired': False, 'low_privilege_qualified': False,
        'compiled_or_executed': False, 'sale_or_cleanup_qualified': False,
        'test_definition_count': len(tests), 'remaining': remaining}
    for record in (plan, stage):
        record['comfy_host_source_integration'] = integration
        record['comfy_environment_source_integration'].update({
            'runtime_reference_source_written': True, 'exact_runtime_inventory_export_source_written': True,
            'native_prepared_launch_source_wired': True, 'runtime_execution_verified': False,
            'remaining': remaining, 'host_source_annex': ANNEX})
        record['comfy_media_source_integration'].update({
            'native_process_source_written': True, 'original_prepared_host_source_wired': True,
            'private_profile': integration['private_profile'], 'private_release': integration['private_release'],
            'remaining': remaining, 'host_source_annex': ANNEX})
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
        task['current_code_components'] = sorted(set(task.get('current_code_components', []) + ['IMP-R026-' + n for n, _, _ in ITEMS]))
        task['comfy_host_source_limit'] = DETAIL
        task['current_validation_status'] = 'NOT_EXECUTED'
        if task['id'] in {'DEV-21', 'DEV-22'}:
            task['remaining_detail'] = DETAIL
    status['activity_source'] = ('2026-11-06 CODE_FIRST：CH-001—032原环境运行持有、原生进程/独立管道、'
        'PIPE_2授权ACK/截止和原ProviderRental准备接线；' + str(len(tests)) + '项新测试定义未编译/运行。'
        'CH-033—036与全56/222仍开放，原RN67/CE31/MW44只是源码，R025证据只作历史。')
    for number, title, state in ITEMS:
        stage['implemented_items'] = [i for i in stage['implemented_items'] if i['id'] != 'IMP-R026-' + number]
        stage['implemented_items'].append({'id': 'IMP-R026-' + number, 'title': title, 'status': state})
    stage['source_files'] = sorted(set(stage['source_files'] + REFS))
    stage['tasks'] = status['tasks']
    stage['comfy_host_test_definitions'] = {'tests': tests, 'ids': [t['id'] for t in tests],
        'status': 'SOURCE_ONLY_NOT_EXECUTED',
        'scope': 'Authored original-ledger/reference/native-pipe/model-set/handshake/deadline source only; no compiler, test, migration, engine, model, GPU, phone or browser run.'}
    for name, record in [('dev/progress/code-first-plan.json', plan),
                         ('dev/progress/R026-code-stage.json', stage),
                         ('dev/progress/implementation-status.json', status)]:
        dump(name, record)
    counts = Counter(t['implementation_status'] for t in status['tasks'])
    lines = ['# R026 全量代码阶段进展', '',
        '**进行中，SOURCE_ONLY / 未验证。** 全部生产代码、实际依赖接线与测试源码齐备后统一编译、测试、调试。新增036—040是源码检查点，非完整开发轮完成；历史条目按其原范围阅读。', '',
        '| 编号 | 本阶段已写内容 | 实际状态 |', '|---|---|---|']
    lines += [f"| {i['id']} | {i['title']} | {i['status']} |" for i in stage['implemented_items']]
    lines += ['', f'新增[CH-001—036](../../{ANNEX})及{len(tests)}项测试定义。CH-033—036仍待；原RN67/CE31/MW44未执行，全部冻结SQL原字节保留，无迁移。', '',
        '## 全部56任务：完整未收口清单', '',
        '状态数量：' + '、'.join(f'{name} {count}' for name, count in sorted(counts.items())) + '；全部222子项保持未验收。', '',
        '| 编号 | 任务 | 当前状态 | 全部仍待收口子项 |', '|---|---|---|---|']
    current = {t['id']: t for t in status['tasks']}
    for task in plan['tasks']:
        p = current[task['id']]
        lines.append(f"| {task['id']} | {task['title']} | {p['implementation_status']} | {'、'.join(p['remaining_step_ids'])} |")
    write('dev/progress/R026-全量代码阶段进展.md', '\n'.join(lines) + '\n')
    inventory = ['# 全量代码优先实施清单', '', DETAIL, '',
        '先完成全部生产代码、真实依赖接线和测试源码，再批量验证。代码状态与验收分开；全部222子项未验收。', '',
        '| 子项 | 任务 | 未收口需求 | 代码状态 | 验收 |', '|---|---|---|---|---|']
    for task in plan['tasks']:
        for step in task['steps']:
            inventory.append(f"| {step['id']} | {task['title']} | {step['code_requirement']} | {step['code_status']} | {step['acceptance_status']} |")
    write('dev/全量代码优先实施清单.md', '\n'.join(inventory) + '\n')
    table = '| 编号 | 测试定义 | 源码 | 状态 |\n|---|---|---|---|\n' + '\n'.join(
        f"| {t['id']} | {t['name']} | {t['source']} | 未编译/未运行 |" for t in tests)
    section(ANNEX, '## CH 测试源码定义索引', table)
    note = (f'R026新增[CH-001—036]({ANNEX})：原Comfy环境运行持有、固定原生进程/三管道、'
        'PIPE_2准备/一次原授权ACK/单调截止和原ProviderRental准备接线。'
        f'{len(tests)}项新测试定义未编译/运行；CH-033—036正式租内媒体Backend、认证工作区/恢复清理、'
        '公开交付、完整包/低权限/E2E、独立ready/计量/付费续租/平台释放仍待。'
        '全56任务/222子项开放，无SQL变更、迁移或真实执行；完整代码后统一验证，源码检查点不是开发轮完成。')
    english = (f'R026 adds [CH-001–036]({ANNEX}): original Comfy environment lifetime references, '
        'fixed native process/three pipes, PIPE_2 preparation/one-shot original grant ACK/monotonic deadlines '
        f'and original ProviderRental preparation wiring. All {len(tests)} new test definitions are uncompiled/unexecuted. '
        'CH-033–036 rental-media Backend, authenticated workspace/recovery cleanup, public delivery, complete '
        'runtime archives/OS low privilege/E2E, independent readiness/metering/paid renewal/platform release remain open. '
        'All 56 tasks/222 steps remain open; no SQL change, migration or engine run. Full-scope source precedes batch validation; a source checkpoint is not a completed development round.')
    marker = '## R026 CH-001—036 原环境与宿主源码继续 / Original environment and host source'
    for name, text in [('dev/repository/README.zh-CN.md.in', note), ('dev/repository/README.en.md.in', english)]:
        section(name, marker, text)
        body = (ROOT / name).read_text(encoding='utf-8')
        block = '<!-- R026-CH-CURRENT -->\n' + text + '\n<!-- /R026-CH-CURRENT -->'
        body = re.sub(r'\n<!-- R026-CH-CURRENT -->.*?<!-- /R026-CH-CURRENT -->\n', '\n', body, flags=re.S)
        title, rest = body.split('\n', 1)
        write(name, title + '\n\n' + block + '\n' + rest)
    for name in ['dev/repository/USAGE.md', 'dev/repository/SYSTEM_DESIGN.md']:
        section(name, marker, note.replace('(docs/', '(../../docs/') + '\n\n' + english.replace('(docs/', '(../../docs/'))
    for name, text in [('CHANGELOG.zh-CN.md', note), ('CHANGELOG.md', english)]:
        body = (ROOT / name).read_text(encoding='utf-8')
        if text not in body:
            title, rest = body.split('\n', 1)
            write(name, title + '\n\n- ' + text + '\n' + rest)
    for name in ['AGENTS.md', 'dev/AGENTS.md']:
        section(name, marker, note.replace('(docs/', '(../docs/' if name.startswith('dev/') else '(docs/') +
            '\n\n当前唯一增量整理器为dev/tools/整理R026Comfy宿主源码.py；禁止重跑旧CE/MW/RN整理器覆盖本次CH状态。'
            '全部生产接线/依赖与测试源码齐备后统一验证；当前不编译、测试、迁移、运行内核/模型/设备/浏览器。'
            '冻结SQL原字节保持，DEV-55延后、DEV-32无证明关闭；禁止本地虚拟化，浏览器使用后关闭不用页面。')
    for name, prefix in [
        ('dev/src/client/comfyenv/README.md', '../../../../'),
        ('dev/src/client/node/README.md', '../../../../'),
        (BASE + '/README.md', '../../../../../'),
        ('dev/src/client/kernels/comfy-private/README.md', '../../../../../'),
        ('dev/progress/README.md', '../../')]:
        section(name, marker, note.replace('(docs/', '(' + prefix + 'docs/'))
    for name in ['docs/技术开发/附录-ComfyUI完整环境安装与原节点登记源码.md',
                 'docs/技术开发/附录-ComfyUI私有媒体执行与本地加密源码.md',
                 'docs/技术开发/附录-供给端内核封装与替换规范.md',
                 'docs/技术开发/附录-小时租赁授权与累计计量源码细则.md']:
        section(name, marker, note.replace('(docs/技术开发/', '('))
    print(f'Document inventory only: IMP036–040 / CH001–036 / {len(tests)} new test definitions; all 56/222 open. No build/test/validator/engine run.')


if __name__ == '__main__':
    main()
