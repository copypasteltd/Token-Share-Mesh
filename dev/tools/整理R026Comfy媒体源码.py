"""Document-only R026 inventory, never build/import/validate/run product code.

Continue the latest CE/RN checkpoint without replaying older inventory tools.
Test names are read as text for a documentation index, not executed/discovered.
"""
from pathlib import Path
from collections import Counter
import json
import re

ROOT = Path(__file__).resolve().parents[2]
ANNEX = 'docs/技术开发/附录-ComfyUI私有媒体执行与本地加密源码.md'
BASE = 'dev/src/client/kernels/comfy-private'
GO = 'dev/src/client/adapters/comfyworker'
REFS = sorted([str(p.relative_to(ROOT)).replace('\\', '/')
               for folder in (ROOT / BASE, ROOT / GO) for p in folder.rglob('*')
               if p.is_file() and p.suffix in {'.py', '.go', '.md'}] + [ANNEX,
    'dev/src/client/comfyenv/upstream.lock.json',
    'dev/tools/补充Comfy进度源码审阅.py', 'dev/tools/整理R026Comfy媒体源码.py'])
ITEMS = [
    ('031', 'ComfyUI私有入口、选择性导入和固定显卡源码',
     'MW-001—010/035—040：包内-I/-B/-S、全树/模型复核、UUID/PCI、独立内容/取消和单调截止；实际原宿主启动/低权限/完整包仍待，未运行'),
    ('032', '固定有类型配方与四类私有媒体编码源码',
     'MW-011—021：有限参数、PNG/JPEG输入、PNG/分片MP4/PCM WAV/几何GLB直接写密文；真实模型/节点组合和公开协议未接，未运行'),
    ('033', '原工作区锚、首建前登记和64KiB本地媒体密文源码',
     'MW-022—030：独立随机RAM钥、原scope/AAD、认证END、一次有界reader及原钥关闭；原宿主目录预留/恢复清理仍待，不是网络E2E/LP'),
    ('034', 'Go原scope一次凭据、加密媒体reader和独立私有帧源码',
     'MW-031—042：严格receipt、复制别名共撤权、原os.Root、END/逐块认证和分配前限帧；正式process/join/RENTAL Backend未接，不授资金清理资格'),
    ('035', 'MW-001—044、编解码器/取消/密文测试源码和双语文档',
     '只有测试定义；未编译、导入、运行或迁移。CE31/RN67和原56/222全部保留，完整代码后统一验证；源码检查点不是完整开发轮'),
]
DETAIL = ('新增私有ComfyUI固定包入口/选择性节点导入/UUID与PCI、有限配方、四类流式编码、'
          '原锚/加密预登记/分块GCM和Go一次receipt/私有帧源码，均未编译或运行。'
          '原环境运行持有与可信清单导出、完整实际依赖包/模型配方、正式宿主进程适配/租内媒体、'
          '公开协议/对象交付、低权限/E2E和性能仍待；全部56/222代码与测试源码齐备后统一验证。')
STEPS = {
    'DEV-22': {'DEV-22.03', 'DEV-22.04'},
    'DEV-37': {'DEV-37.02', 'DEV-37.03'},
    'DEV-53': {'DEV-53.01', 'DEV-53.02', 'DEV-53.03'},
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


def test_definitions():
    rows = []
    for name in REFS:
        if name.endswith('_test.go'):
            pattern = r'^func (Test\w+)\('
        elif '/tests/test_' in name and name.endswith('.py'):
            pattern = r'^    def (test_\w+)\('
        else:
            continue
        for test in re.findall(pattern, (ROOT / name).read_text(encoding='utf-8'), re.M):
            rows.append({'id': f'MW-T{len(rows) + 1:03d}', 'name': test,
                         'source': name, 'status': 'SOURCE_ONLY_NOT_EXECUTED'})
    return rows


def main():
    plan = load('dev/progress/code-first-plan.json')
    stage = load('dev/progress/R026-code-stage.json')
    status = load('dev/progress/implementation-status.json')
    tests = test_definitions()
    integration = {
        'source_status': 'PRIVATE_COMFY_WORKER_FIXED_RECIPES_MEDIA_CIPHERTEXT_AND_GO_BOUNDARY_SOURCE_ONLY',
        'annex': ANNEX, 'requirement_ids': [f'MW-{n:03d}' for n in range(1, 45)],
        'official_source_commit': '2c75dc1b379a59e0880619d029607af5169a62ef',
        'private_worker_source_written': True, 'private_encoder_source_written': True,
        'local_first_write_encryption_source_connected': True,
        'go_cipher_and_wire_boundary_source_written': True,
        'source_test_definitions': len(tests),
        'actual_native_process_adapter_connected': False,
        'original_rental_media_backend_connected': False,
        'public_model_listener_connected': False,
        'complete_runtime_archives_shipped': False,
        'network_e2e_implemented_by_this_component': False,
        'low_privilege_profile_granted': False,
        'qualification_granted': False, 'validation_executed': False,
        'actual_model_gpu_or_phone_run': False, 'performance_evidence': False,
        'remaining': [
            '原已验证环境候选运行持有/可信清单导出与当前本人批准、撤销、许可复核',
            '完整固定Python/Torch/CUDA/编解码器/密码库/节点工具包和经许可真实模型/四类配方',
            '原OS锁/物理owner/空间目录预留、独立身份与私有句柄、实际process/pipe join的正式启动适配',
            '原RENTAL_SESSION媒体策略/认证输入/inbox/双围栏/结果保管/原清理回执与平台资金释放',
            'OpenAI/Anthropic媒体和资产权限/结果交付/最终ACK、低权限/E2E及真实设备性能',
            '全部56/222剩余完整生产代码和测试源码，然后统一编译/测试/调试',
        ],
    }
    for record in (plan, stage):
        record['comfy_media_source_integration'] = integration
        environment = record.get('comfy_environment_source_integration', {})
        environment['private_comfy_media_worker_source_written'] = True
        environment['media_first_write_encryption_source_written'] = True
        environment['private_comfy_media_worker_wired'] = False
        environment['media_first_write_encryption_wired'] = False
        environment['media_source_annex'] = ANNEX
        environment['remaining'] = integration['remaining']
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
        task['comfy_media_source_limit'] = DETAIL
        if task['id'] in {'DEV-22', 'DEV-37'}:
            task['remaining_detail'] = DETAIL
        task['current_validation_status'] = 'NOT_EXECUTED'
    status['activity_source'] = ('2026-11-06 CODE_FIRST：新增MW-001—044私有Comfy执行/有限配方/四类编码和本地GCM、'
        'Go原scope一次receipt/私有帧源码；' + str(len(tests)) + '项新测试定义全部未编译/运行。'
        '正式原宿主/租内媒体/完整运行包/低权限等仍待；CE31/RN67与全56/222保持开放，R025仅历史证据。')
    for number, title, state in ITEMS:
        stage['implemented_items'] = [i for i in stage['implemented_items'] if i['id'] != 'IMP-R026-' + number]
        stage['implemented_items'].append({'id': 'IMP-R026-' + number, 'title': title, 'status': state})
    stage['source_files'] = sorted(set(stage['source_files'] + REFS))
    stage['tasks'] = status['tasks']
    stage['comfy_media_test_definitions'] = {'ids': [t['id'] for t in tests], 'tests': tests,
        'status': 'SOURCE_ONLY_NOT_EXECUTED',
        'scope': 'Authored Go/Python original-scope crypto/file/frame/recipe/cancellation and real-codec tests on synthetic CPU tensors. No test, compiler, engine, model, GPU or phone was run.'}
    dump('dev/progress/code-first-plan.json', plan)
    dump('dev/progress/implementation-status.json', status)
    dump('dev/progress/R026-code-stage.json', stage)
    counts = Counter(t['implementation_status'] for t in status['tasks'])
    lines = ['# R026 全量代码阶段进展', '',
        '**进行中，SOURCE_ONLY / 未验证。** 全部生产代码、实际依赖接线与测试源码齐备后统一编译、测试、调试；源码检查点不是开发轮完成。当前新增031—035，早期条目保留其历史范围。', '',
        '| 编号 | 本阶段已写内容 | 实际状态 |', '|---|---|---|']
    lines += [f"| {i['id']} | {i['title']} | {i['status']} |" for i in stage['implemented_items']]
    lines += ['', f'新增[MW-001—044](../../{ANNEX})和{len(tests)}项测试定义；既有CE31/RN67均未执行。本次无DDL改变/迁移，全部冻结SQL原字节保留。', '',
        '## 全部56任务：完整未收口清单', '',
        '状态数量：' + '、'.join(f'{name} {count}' for name, count in sorted(counts.items())) + '；全部222子项保持未验收。', '',
        '| 编号 | 任务 | 当前状态 | 全部仍待收口子项 |', '|---|---|---|---|']
    current = {t['id']: t for t in status['tasks']}
    for task in plan['tasks']:
        progress = current[task['id']]
        lines.append(f"| {task['id']} | {task['title']} | {progress['implementation_status']} | {'、'.join(progress['remaining_step_ids'])} |")
    write('dev/progress/R026-全量代码阶段进展.md', '\n'.join(lines) + '\n')
    inventory = ['# 全量代码优先实施清单', '',
        'R026 · CODE_FIRST · 全56任务/222子项；生产代码、真实依赖接线和测试源码齐备后统一编译/测试/调试。所有原子待办保持开放；DEV-55延后，DEV-32无真实证明安全关闭。', '',
        '[当前进展](progress/R026-全量代码阶段进展.md) · [机器可读清单](progress/code-first-plan.json)', '']
    for task in plan['tasks']:
        inventory += [f"## {task['id']} {task['title']}", '', task['requirement'], '',
            '| 编号 | 开发项 | 完整代码要求 | 代码状态 | 验收状态 |', '|---|---|---|---|---|']
        inventory += [f"| {s['id']} | {s['title']} | {s['code_requirement'].replace('|', '&#124;')} | {s['code_status']} | {s['acceptance_status']} |" for s in task['steps']]
        inventory.append('')
    write('dev/全量代码优先实施清单.md', '\n'.join(inventory))
    table = '| 编号 | 定义 | 源码 | 状态 |\n|---|---|---|---|\n' + '\n'.join(
        f"| {t['id']} | {t['name']} | {t['source']} | 未编译/未运行 |" for t in tests)
    section(BASE + '/README.md', '## R026 测试定义编号索引 / Test source index', table)
    section(GO + '/README.md', '## R026 测试定义编号索引 / Test source index',
        '完整MW测试定义列表见[Python组件README](../../kernels/comfy-private/README.md#r026-测试定义编号索引--test-source-index)。全部只是源码，尚未执行。')
    note = ('R026新增[MW-001—044](' + ANNEX + ')：固定原生私有Comfy执行器、GPU UUID/PCI复核、'
        '有限配方、PNG/分片MP4/PCM WAV/几何GLB编码、原锚/加密预登记/分块GCM及Go一次原scope凭据/私有帧源码。'
        f'{len(tests)}项新测试定义未编译/运行；正式原宿主启动器/租内媒体/完整运行包/公开交付/低权限/E2E与性能仍待。'
        'CE31/RN67及全部56任务222子项仍未收口，代码齐备后统一验证；本次无SQL变更。')
    english = ('R026 adds [MW-001–044](' + ANNEX + '): a fixed native private Comfy worker, exact CUDA UUID/PCI, '
        'finite recipes, PNG/fragmented MP4/PCM WAV/geometry GLB encoders, original workspace anchors, encrypted '
        'pre-create reservations, chunked GCM and a Go single-use original-scope receipt/private-frame boundary. '
        f'All {len(tests)} new test definitions remain uncompiled/unexecuted. The actual host launcher, rental-media '
        'Backend, complete runtime archives, public delivery, OS low-privilege/E2E and performance remain open. '
        'CE31/RN67 and all 56 tasks/222 steps remain open until full-scope code precedes batch validation. No SQL change.')
    marker = '## R026 MW-001—044 私有媒体源码继续 / Private media source'
    for name, text in [('dev/repository/README.zh-CN.md.in', note), ('dev/repository/README.en.md.in', english)]:
        section(name, marker, text)
        body = (ROOT / name).read_text(encoding='utf-8')
        # The newest scope/status should be visible before historical rounds.
        block = '<!-- R026-MW-CURRENT -->\n' + text + '\n<!-- /R026-MW-CURRENT -->'
        body = re.sub(r'\n<!-- R026-MW-CURRENT -->.*?<!-- /R026-MW-CURRENT -->\n', '\n', body, flags=re.S)
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
            '\n\n当前增量整理器为dev/tools/整理R026Comfy媒体源码.py；禁止重跑旧整理器覆盖本次MW/DEV-37及CE/RN状态。'
            '所有生产接线与测试源码齐备后统一验证，不启动编译/内核/模型/设备/数据库/浏览器。'
            '冻结SQL原字节保留；无本地虚拟化，DEV-55延后、DEV-32无证明关闭，浏览器用后关闭不用页面。')
    section('dev/src/client/comfyenv/README.md', marker, note.replace('(docs/', '(../../../../docs/'))
    section('docs/技术开发/附录-ComfyUI完整环境安装与原节点登记源码.md', marker,
        note.replace('(docs/技术开发/', '('))
    section('docs/技术开发/附录-供给端内核封装与替换规范.md', marker,
        note.replace('(docs/技术开发/', '('))
    section('dev/progress/README.md', marker, note.replace('(docs/', '(../../docs/'))
    print(f'Document inventory only: IMP031–035 / MW001–044 / {len(tests)} authored test definitions. No build/validator/test/engine run. All 56/222 remain open.')


if __name__ == '__main__':
    main()
