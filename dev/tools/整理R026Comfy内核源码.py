"""Document-only source inventory; never build, validate, migrate or run tests."""
from __future__ import annotations

import json
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[2]
ANNEX = 'docs/技术开发/附录-ComfyUI完整环境安装与原节点登记源码.md'
REFS = sorted([
    'dev/src/client/comfyenv/' + name for name in [
        'catalog.go', 'archive.go', 'files.go', 'registry.go', 'README.md',
        'catalog_test.go', 'archive_test.go', 'registry_test.go',
        'activation_test.go', 'upstream.lock.json']
] + [
    'dev/src/client/execution/maintenance.go',
    'dev/src/client/execution/maintenance_test.go',
    'dev/src/client/node/kernels.go', 'dev/src/client/node/kernels_test.go',
    'dev/src/client/node/store.go', 'dev/src/client/cmd/p11-node/main.go',
    'dev/src/client/cmd/p11-node/comfy.go', 'dev/src/client/cmd/p11-node/comfy_test.go',
    'dev/licenses/ComfyUI-2c75dc1b379a59e0880619d029607af5169a62ef-GPL-3.0.txt', ANNEX,
])
ITEMS = [
    ('027', 'ComfyUI完整固定目录、独立签名与本人节点/许可选择',
     'CE-001—015：精确逐文件包、角色隔离、有限节点依赖与独立同意；官方v0.44.0源码固定，尚未制作完整实际运行包，未验证'),
    ('028', '本机ZIP有界安装、完整树复核与失败隔离',
     'CE-016—025：分配前限中央目录/逐记录扫描、64KiB解压、先登记stage、两次完整复核和重启隔离；不执行pip/git/节点或模型'),
    ('029', '原节点加密登记、维护共槽切换和五项本机入口',
     'CE-026—034：原系统钥/OS锁/锚定AEAD、原MAINTENANCE关键提交、显式回滚/先墓碑删除/关闭join；本机选择NOT_GRANTED，未验证'),
    ('030', 'CE-001—036、测试源码和完整文档同步',
     '新边界测试只有定义；不编译/迁移/运行，媒体worker/首写加密/租内接线仍待；原RN-110/67测试与全部56/222开放状态保留'),
]
DETAIL = ('新增完整固定软件目录/独立Ed25519与本人许可节点同意、有界ZIP安装/全树复核、'
          '原系统钥锚定AEAD登记及原维护共槽选择/五项CLI源码；均未编译或执行。'
          '实际完整运行包制作/分发、ComfyUI私有worker/四类媒体/首写加密、'
          '原租内授权/清理、低权限和真实性能仍待，全56/222代码齐备后统一验证。')
STEPS = {
    'DEV-01': {'DEV-01.02'}, 'DEV-07': {'DEV-07.02'},
    'DEV-21': {'DEV-21.01', 'DEV-21.03', 'DEV-21.04'},
    'DEV-22': {'DEV-22.01', 'DEV-22.02'},
    'DEV-24': {'DEV-24.01', 'DEV-24.04'},
    'DEV-53': {'DEV-53.01'}, 'DEV-54': {'DEV-54.01'},
    'DEV-56': {'DEV-56.01'},
}


def load(name):
    return json.loads((ROOT / name).read_text(encoding='utf-8'))


def write(name, value):
    path = ROOT / name
    old = path.read_bytes() if path.exists() else b''
    ending = '\r\n' if b'\r\n' in old else '\n'
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(value.replace('\r\n', '\n').replace('\n', ending).encode('utf-8'))


def dump(name, value):
    write(name, json.dumps(value, ensure_ascii=False, indent=2) + '\n')


def append_section(name, marker, content):
    body = (ROOT / name).read_text(encoding='utf-8')
    if marker in body:
        start = body.index(marker)
        end = body.find('\n## ', start + len(marker))
        body = body[:start].rstrip() + ('\n\n' + body[end + 1:] if end >= 0 else '')
    write(name, body.rstrip() + '\n\n' + marker + '\n\n' + content.strip() + '\n')


def source_tests():
    rows = []
    for name in REFS:
        if not name.endswith('_test.go'):
            continue
        # A document index of authored definitions, not execution/discovery.
        for test in re.findall(r'^func (Test\w+)\(', (ROOT / name).read_text(encoding='utf-8'), re.M):
            rows.append({'id': f'CE-T{len(rows) + 1:03d}', 'name': test,
                         'source': name, 'status': 'SOURCE_ONLY_NOT_EXECUTED'})
    return rows


def main():
    plan = load('dev/progress/code-first-plan.json')
    status = load('dev/progress/implementation-status.json')
    stage = load('dev/progress/R026-code-stage.json')
    tests = source_tests()
    integration = {
        'source_status': 'IMMUTABLE_COMFY_CATALOG_INSTALLER_ORIGINAL_NODE_REGISTRY_MAINTENANCE_AND_CLI_SOURCE_ONLY',
        'annex': ANNEX, 'requirement_ids': [f'CE-{n:03d}' for n in range(1, 37)],
        'official_source_commit': '2c75dc1b379a59e0880619d029607af5169a62ef',
        'official_release': 'v0.44.0', 'complete_runtime_archives_shipped': False,
        'finite_node_installer_source_written': True,
        'original_encrypted_node_registry_source_written': True,
        'original_maintenance_gate_source_written': True,
        'local_cli_source_written': True,
        'private_comfy_media_worker_wired': False,
        'rental_media_executor_wired': False,
        'media_first_write_encryption_wired': False,
        'model_downloaded_or_run': False, 'qualification_granted': False,
        'validation_executed': False,
        'remaining': [
            '完整经许可Python/ComfyUI/Torch/工具/节点运行包制作、签发/分发和可信本人批准UI',
            'ComfyUI无内容网络端口私有worker、选择性导入/GPU绑定/取消与原停工事实',
            '图像/视频/音频/3D素材权限、格式/加密首写、结果恢复与原RENTAL_SESSION调用',
            '独立低权限、平台实际资格/计量/清理/资金及真实设备性能证据',
            '全56/222剩余生产代码和测试源码，然后统一编译/测试/调试',
        ],
    }
    plan['comfy_environment_source_integration'] = integration
    stage['comfy_environment_source_integration'] = integration
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
        if task['id'] in STEPS:
            task['implementation_status'] = 'IN_PROGRESS'
            task['current_source_refs'] = sorted(set(task.get('current_source_refs', []) + REFS))
            task['current_code_components'] = sorted(set(task.get('current_code_components', []) + ['IMP-R026-' + n for n, _, _ in ITEMS]))
            task['comfy_source_limit'] = DETAIL
            if task['id'] == 'DEV-22':
                task['remaining_detail'] = DETAIL
            task['current_validation_status'] = 'NOT_EXECUTED'
    status['activity_source'] = ('2026-11-06 CODE_FIRST：新增CE-001—036完整固定软件目录/有限节点安装、'
        '原节点AEAD登记/维护共槽和五项本机CLI源码；' + str(len(tests)) +
        '项新测试定义未编译/运行。RN-001—110/67租赁定义保留，全56/222未收口；R025仅历史证据。')
    for number, title, state in ITEMS:
        stage['implemented_items'] = [i for i in stage['implemented_items'] if i['id'] != 'IMP-R026-' + number]
        stage['implemented_items'].append({'id': 'IMP-R026-' + number, 'title': title, 'status': state})
    stage['source_files'] = sorted(set(stage['source_files'] + REFS))
    stage['tasks'] = status['tasks']
    stage['comfy_environment_test_definitions'] = {
        'ids': [t['id'] for t in tests], 'tests': tests, 'status': 'SOURCE_ONLY_NOT_EXECUTED',
        'scope': 'Authored signature/AEAD/files/ZIP/SQLite and owner-CLI boundary definitions only; tiny synthetic files, no Python/ComfyUI/model/GPU execution or qualification.'}
    for record in [plan, stage]:
        for i, text in enumerate(record.get('rental_source_integration', {}).get('remaining', [])):
            if '安装器及实际性能' in text:
                record['rental_source_integration']['remaining'][i] = '有限节点安装/原登记源码已写未验证；完整运行包/ComfyUI私有worker、低权限与性能仍待，不可把本机AEAD称E2E/TEE'
    dump('dev/progress/code-first-plan.json', plan)
    dump('dev/progress/implementation-status.json', status)
    dump('dev/progress/R026-code-stage.json', stage)
    lines = ['# R026 全量代码阶段进展', '',
        '**进行中，SOURCE_ONLY / 未验证。** 全部生产代码、真实依赖接线和测试源码齐备后统一编译、测试、调试；源码检查点不是开发轮完成。当前新增推进027—030，早期条目保留其历史范围。', '',
        '| 编号 | 本阶段已写内容 | 实际状态 |', '|---|---|---|']
    lines += [f"| {i['id']} | {i['title']} | {i['status']} |" for i in stage['implemented_items']]
    lines += ['', f'新增[CE-001—036](../../{ANNEX})和{len(tests)}项边界测试定义；原RN-001—110/67租赁定义全部未执行。无DDL改动/迁移，平台001—015与原客户端DDL/端侧v2保持原字节。', '',
        '## 全部56任务：完整未收口清单', '', '| 编号 | 任务 | 当前状态 | 全部仍待收口子项 |', '|---|---|---|---|']
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
        for step in task['steps']:
            inventory.append(f"| {step['id']} | {step['title']} | {step['code_requirement'].replace('|', '&#124;')} | {step['code_status']} | {step['acceptance_status']} |")
        inventory.append('')
    write('dev/全量代码优先实施清单.md', '\n'.join(inventory))
    test_table = '| 编号 | 定义 | 源码 | 状态 |\n|---|---|---|---|\n' + '\n'.join(
        f"| {t['id']} | {t['name']} | {t['source']} | 未编译/未运行 |" for t in tests)
    append_section('dev/src/client/comfyenv/README.md', '## R026 测试定义编号索引', test_table)
    note = ('R026新增[CE-001—036](' + ANNEX + ')：独立签名/本人许可节点同意、固定完整目录、'
        '有限节点依赖、有界ZIP安装/全树复核、原节点AEAD登记、原物理维护共槽切换和五项本机管理入口源码。'
        '官方ComfyUI固定v0.44.0/2c75dc1b379a59e0880619d029607af5169a62ef，尚未制作完整实际运行包；'
        f'{len(tests)}项新测试定义全部未编译/运行。没有ComfyUI媒体执行/首写加密/租内接线或资格/性能结论。'
        'RN-110/67租赁测试、全部56任务222子项保持开放，代码齐备后统一验证；无SQL变更/迁移。')
    en = ('R026 adds [CE-001–036](' + ANNEX + '): independently pinned/signed complete catalogs and local license/node consent, '
        'bounded immutable ZIP installation and whole-tree checks, the original node AEAD registry, shared physical maintenance selection '
        'and five metadata-only local CLI operations. Official source is pinned to ComfyUI v0.44.0/2c75dc1b379a59e0880619d029607af5169a62ef; '
        f'no complete actual runtime archive ships yet. All {len(tests)} new test definitions remain uncompiled/unexecuted. '
        'Media worker/first-write encryption/rental integration, actual qualification and performance remain open. '
        'RN-110/67 rental tests and every 56/222 task/step remain open until full-scope code precedes batch validation. No DDL change/migration.')
    for name, text in [('dev/repository/README.zh-CN.md.in', note), ('dev/repository/README.en.md.in', en)]:
        append_section(name, '## R026 ComfyUI source checkpoint / 源码检查点', text)
    for name in ['dev/repository/USAGE.md', 'dev/repository/SYSTEM_DESIGN.md']:
        append_section(name, '## R026 ComfyUI source checkpoint / 源码检查点', note.replace('(docs/', '(../../docs/') + '\n\n' + en.replace('(docs/', '(../../docs/'))
    for name, text in [('CHANGELOG.zh-CN.md', note), ('CHANGELOG.md', en)]:
        body = (ROOT / name).read_text(encoding='utf-8')
        if text not in body:
            title, rest = body.split('\n', 1)
            write(name, title + '\n\n- ' + text + '\n' + rest)
    for name in ['AGENTS.md', 'dev/AGENTS.md']:
        append_section(name, '## R026 CE-001—036 完整环境安装与原节点源码继续',
            note.replace('(docs/', '(../docs/' if name.startswith('dev/') else '(docs/') + '\n\n当前增量整理器为dev/tools/整理R026Comfy内核源码.py；'
            '禁止重跑旧整理器覆盖本次DEV-22/CE和RN-110状态。所有代码/测试源码齐备后统一验证。'
            '禁止本地Docker/WSL/QEMU等虚拟化；DEV-55延后、DEV-32无证明关闭，浏览器后关闭不用页面。')
    append_section('docs/技术开发/附录-供给端内核封装与替换规范.md', '## R026 CE-001—036：完整环境安装源码进展',
        note.replace('(docs/技术开发/', '('))
    append_section('dev/progress/README.md', '## R026 CE-001—036 源码检查点', note.replace('(docs/', '(../../docs/'))
    print(f'Document inventory only: IMP027–030 / CE001–036 / {len(tests)} authored test definitions. No build, validator, migration or test invoked. Full 56/222 remain open.')


if __name__ == '__main__':
    main()
