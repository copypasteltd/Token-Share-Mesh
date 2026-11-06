"""Update source/doc inventories only; never build, validate, test or migrate."""
from collections import Counter
from copy import deepcopy
import json
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[2]
ANNEX = 'docs/技术开发/附录-原租赁媒体会话监督与停止回收源码.md'
TOOL = 'dev/tools/整理R026媒体会话源码.py'
TEST_FILES = [
    'dev/src/client/rentalmedia/runtime_test.go',
    'dev/src/client/adapters/comfyworker/media_session_test.go',
    'dev/src/client/comfyenv/use_join_test.go',
    'dev/src/client/node/rental_comfy_owned_test.go',
    'dev/src/client/execution/rental_outstanding_test.go',
]
REFS = sorted(TEST_FILES + [ANNEX, TOOL,
    'dev/src/client/rentalmedia/README.md',
    'dev/src/client/rentalmedia/types.go',
    'dev/src/client/rentalmedia/runtime.go',
    'dev/src/client/rentalmedia/comfy.go',
    'dev/src/client/adapters/comfyworker/media_session.go',
    'dev/src/client/adapters/comfyworker/process.go',
    'dev/src/client/adapters/comfyworker/rental_media.go',
    'dev/src/client/comfyenv/use.go',
    'dev/src/client/execution/rental_outstanding.go',
    'dev/src/client/node/rental_comfy_owned.go',
    'dev/src/client/node/rental_comfy.go',
    'dev/src/client/node/rental_native.go',
    'dev/src/client/node/store.go',
    'dev/src/client/node/kernels.go',
    'dev/src/client/node/media_workspaces.go',
])
ITEMS = [
    ('046', '原租赁媒体会话监督与真实Comfy叶子接线源码', 'MS-001—018：独立媒体类型、原准备/授权/单调控制、唯一活动子调用/查询重放；尚非公开SDK或平台计费闭环'),
    ('047', '原节点Comfy会话、输入所有权与两类会话关闭源码', 'MS-003—015/027/028/031/032：加载前登记、一次原授权、单份输入、接口join、失败启动/Use真实持有、文本与媒体共关闭；未运行'),
    ('048', '独立原生停工、晚入队拒绝与实际退出等待源码', 'MS-010/019—023/029/031：同步停止闩、私有STOP_SESSION、原生写前重核、有限强停、真实CLOSED/管道/进程/操作join；无物理资格结论'),
    ('049', '原媒体结果生命周期与本机会话清理CAS源码', 'MS-024—030：单读者/副本共关闭、显式释放、保留期、原未清计数与真实叶子证据；不补造ACK/usage/小时水位/平台资金释放'),
    ('050', 'MS编号、停止回收测试源码及双语待办文档', '新增27项边界测试定义及MS-001—036/双语README/56任务222子项表；全部未编译/未运行，全量代码后统一验证，本次仅源码检查点'),
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
DETAIL = ('新增原节点持有的完整本机媒体会话监督/独立单调截止、原Comfy叶子接线、'
          '一次原授权和单份未提交输入、先停工后回收/两类会话共关闭、原输出单读者/保留期/真实join和本机清理CAS源码；'
          '全部未编译或运行。公开媒体SDK/资产/最终ACK、独立物理停工恢复对账、双方ready/可信小时水位/'
          '付费续租/平台资金释放、完整实际运行包/OS低权限/E2E/性能及其余生产接线仍待。'
          '全部56任务222子项开放，完整代码与测试源码后统一验证；源码检查点不是完整开发轮。')


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


def section(name, marker, content):
    body = (ROOT / name).read_text(encoding='utf-8') if (ROOT / name).exists() else ''
    if marker in body:
        start = body.index(marker)
        end = body.find('\n## ', start + len(marker))
        body = body[:start].rstrip() + ('\n\n' + body[end+1:] if end >= 0 else '')
    write(name, body.rstrip() + '\n\n' + marker + '\n\n' + content.strip() + '\n')


def main():
    plan, stage, status = [load(name) for name in [
        'dev/progress/code-first-plan.json', 'dev/progress/R026-code-stage.json',
        'dev/progress/implementation-status.json']]
    tests = []
    for source in TEST_FILES:
        # A text definition inventory only, not Go parsing/compilation or a check.
        for name in re.findall(r'^func (Test\w+)\(', (ROOT/source).read_text(encoding='utf-8'), re.M):
            tests.append({'id': f'MS-T{len(tests)+1:03d}', 'name': name,
                          'source': source, 'status': 'SOURCE_ONLY_NOT_EXECUTED'})
    remaining = [
        'MS-033：公开OpenAI/Anthropic媒体SDK、原本人资产/结果响应、最终ACK和独立留存接线',
        'MS-034：双方ready、可信小时水位、付费续租、原平台停工/原号恢复及资金释放接线',
        'MS-035：独立物理停工恢复对账、完整实际运行包/OS受限身份、E2E/LP/性能/跨系统资格',
        'MS-036：全部56/222生产代码/实际依赖/测试源码齐备，再统一编译测试调试与组合验收',
    ]
    integration = {
        'status': 'SOURCE_ONLY_UNVERIFIED', 'requirements': 'MS-001—036', 'annex': ANNEX,
        'original_media_session_supervisor_source_wired': True,
        'typed_comfy_backend_source_wired': True,
        'node_owned_comfy_rental_source_wired': True,
        'original_input_owner_revoke_and_join_source_wired': True,
        'independent_monotonic_stop_source_wired': True,
        'late_native_frame_admission_latch_source_wired': True,
        'text_and_media_node_close_source_wired': True,
        'result_lifetime_and_actual_join_source_wired': True,
        'original_local_session_cleanup_cas_source_wired': True,
        'public_media_sdk_source_wired': False,
        'independent_physical_stop_reconciliation_wired': False,
        'platform_ready_hour_meter_paid_renewal_release_wired': False,
        'actual_runtime_bundle_present': False,
        'network_e2e_qualified': False, 'low_privilege_qualified': False,
        'compiled_or_executed': False, 'native_migration_changed_or_executed': False,
        'physical_cleanup_or_sale_qualified': False,
        'full_development_round_complete': False,
        'test_definition_count': len(tests), 'remaining': remaining,
    }
    for record in (plan, stage):
        record['media_session_source_integration'] = deepcopy(integration)
        for key in ['media_workspace_source_integration', 'comfy_host_source_integration',
                    'comfy_media_source_integration', 'comfy_environment_source_integration']:
            record[key].update({
                'full_media_session_supervisor_source_wired': True,
                'node_owned_media_session_source_wired': True,
                'full_media_session_supervisor_wired': True,
                'media_session_annex': ANNEX,
                'public_media_admission_wired': False,
                'compiled_or_executed': False,
                'remaining': remaining})
    for task in plan['tasks']:
        if task['id'] not in STEPS:
            continue
        task['code_status'] = 'IN_PROGRESS'
        for step in task['steps']:
            if step['id'] in STEPS[task['id']]:
                step['code_status'] = 'IN_PROGRESS'
                step['implementation_refs'] = sorted(set(step.get('implementation_refs', []) + REFS))
    for task in status['tasks']:
        if task['id'] not in STEPS:
            continue
        task['implementation_status'] = 'IN_PROGRESS'
        task['media_session_source_limit'] = DETAIL
        task['current_validation_status'] = 'NOT_EXECUTED'
        if task['id'] in {'DEV-21', 'DEV-22', 'DEV-33', 'DEV-35', 'DEV-36', 'DEV-37'}:
            task['remaining_detail'] = DETAIL
    status['activity_source'] = '2026-11-06 CODE_FIRST：MS-001—032 / IMP046—050新增源码；' + DETAIL
    for number, title, state in ITEMS:
        item = {'id': 'IMP-R026-'+number, 'title': title, 'status': state}
        stage['implemented_items'] = [old for old in stage['implemented_items'] if old['id'] != item['id']] + [item]
    stage['source_files'] = sorted(set(stage.get('source_files', []) + REFS))
    stage['tasks'] = deepcopy(status['tasks'])
    stage['media_session_test_definitions'] = {
        'tests': tests, 'ids': [t['id'] for t in tests], 'status': 'SOURCE_ONLY_NOT_EXECUTED'}
    for name, record in [('dev/progress/code-first-plan.json', plan),
                         ('dev/progress/R026-code-stage.json', stage),
                         ('dev/progress/implementation-status.json', status)]:
        dump(name, record)
    write_progress(plan, stage, status, tests)
    write_documentation(tests)
    print(f'Document inventory only: IMP046–050 / MS001–036 / {len(tests)} test definitions; all 56/222 open. No compiler/test/validator/migration/runtime invoked.')


def write_progress(plan, stage, status, tests):
    counts = Counter(t['implementation_status'] for t in status['tasks'])
    lines = ['# R026 全量代码阶段进展', '',
        '**进行中，SOURCE_ONLY / 未验证。** 完整生产代码、实际依赖接线和测试源码齐备后统一验证。新增046—050只是源码检查点，不是开发轮完成；历史内容按原范围阅读。', '',
        '| 编号 | 本阶段已写内容 | 实际状态 |', '|---|---|---|']
    lines += [f"| {i['id']} | {i['title']} | {i['status']} |" for i in stage['implemented_items']]
    lines += ['', f'新增[MS-001—036](../../{ANNEX})及{len(tests)}项测试源码定义。原RN67/CE31/MW44/CH29/WS23亦未编译/运行；本次没有SQL变更或迁移。', '',
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


def write_documentation(tests):
    section(ANNEX, '## MS 测试源码定义索引',
        '| 编号 | 测试定义 | 源码 | 状态 |\n|---|---|---|---|\n' + '\n'.join(
            f"| {t['id']} | {t['name']} | {t['source']} | 未编译/未运行 |" for t in tests))
    note = (f'R026新增[MS-001—036]({ANNEX})：原节点持有的本机媒体会话监督/独立单调截止、'
            '真实Comfy叶子、一次原授权/单份输入及实际join、先停工后回收、两类会话共关闭、'
            '原媒体结果保留/撤销和本机会话清理CAS源码。'
            f'{len(tests)}项新增测试定义均未编译/运行；公开媒体SDK/最终ACK、双方ready/可信小时水位/'
            '付费续租/平台资金释放、独立物理停工恢复及完整实际运行包/低权限/E2E/性能仍待。'
            '全部56任务222子项开放；本次无SQL变更或迁移。完整生产代码/实际依赖/测试源码齐备后统一验证，源码检查点不是开发轮完成。')
    english = (f'R026 adds [MS-001–036]({ANNEX}): node-owned original media-session supervision, '
               'independent monotonic withdrawal, the actual typed Comfy leaf, once-only original '
               'authorization/one pending input, actual joins, stop-before-cleanup, combined text/media '
               'node closure, result retention/revocation and local-session cleanup CAS source. '
               f'All {len(tests)} new test definitions are uncompiled/unexecuted. Public media SDK/final ACK, '
               'independent readiness/hour meters/paid renewal/platform fund release, physical crash '
               'reconciliation, complete runtime archives/OS low privilege/E2E/performance remain open. '
               'All 56 tasks/222 steps remain unaccepted. No SQL changes or migrations. Complete all '
               'production source, dependency wiring and test source before batch validation. This '
               'source checkpoint is not a completed development round.')
    marker = '## R026 MS-001—036 原媒体会话监督与停止回收源码 / Original media-session supervision source'
    for name, content in [('dev/repository/README.zh-CN.md.in', note), ('dev/repository/README.en.md.in', english)]:
        section(name, marker, content)
        body = (ROOT/name).read_text(encoding='utf-8')
        body = re.sub(r'\n<!-- R026-[A-Z-]+-CURRENT -->.*?<!-- /R026-[A-Z-]+-CURRENT -->\n', '\n', body, flags=re.S)
        title, rest = body.split('\n', 1)
        write(name, title+'\n\n<!-- R026-MS-CURRENT -->\n'+content+'\n<!-- /R026-MS-CURRENT -->\n'+rest)
    for name in ['dev/repository/USAGE.md', 'dev/repository/SYSTEM_DESIGN.md']:
        section(name, marker, note.replace('(docs/', '(../../docs/')+'\n\n'+english.replace('(docs/', '(../../docs/'))
    for name, content in [('CHANGELOG.zh-CN.md', note), ('CHANGELOG.md', english)]:
        body = (ROOT/name).read_text(encoding='utf-8')
        if content not in body:
            title, rest = body.split('\n', 1)
            write(name, title+'\n\n- '+content+'\n'+rest)
    for name in ['AGENTS.md', 'dev/AGENTS.md']:
        section(name, marker, note.replace('(docs/', '(../docs/' if name.startswith('dev/') else '(docs/')+
            '\n\n当前唯一增量整理器为'+TOOL+'；禁止重跑旧WS/CH/CE/MW/RN整理器覆盖本次MS源码元数据。'
            '平台001—015双份SQL、schema.sql/host-schema.sql/native002/native003及其source.json原字节冻结，后续只加前向版本。'
            '当前不编译、测试、核验、迁移、运行内核/模型/数据库/设备/浏览器；全量代码和测试源码后统一验证。'
            '禁止本地虚拟化，DEV-55延后、DEV-32无证明关闭，浏览器使用后关闭不用页面。')
    for name, prefix in [
        ('dev/src/client/rentalmedia/README.md', '../../../../'),
        ('dev/src/client/comfyenv/README.md', '../../../../'),
        ('dev/src/client/node/README.md', '../../../../'),
        ('dev/src/client/adapters/comfyworker/README.md', '../../../../../'),
        ('dev/src/client/mediaworkspace/README.md', '../../../../'),
        ('dev/progress/README.md', '../../')]:
        section(name, marker, note.replace('(docs/', '('+prefix+'docs/'))
    for name in ['docs/技术开发/附录-租内媒体工作区与结果托管源码.md',
                 'docs/技术开发/附录-ComfyUI原环境持有与本机进程接线源码.md',
                 'docs/技术开发/附录-ComfyUI私有媒体执行与本地加密源码.md',
                 'docs/技术开发/附录-供给端内核封装与替换规范.md',
                 'docs/技术开发/附录-小时租赁授权与累计计量源码细则.md',
                 'docs/技术开发/00-技术文档导航与实施基线.md']:
        section(name, marker, note.replace('(docs/技术开发/', '('))


if __name__ == '__main__':
    main()
