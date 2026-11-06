"""Write source/document inventories only; no builds, tests or validators."""
from collections import Counter
from copy import deepcopy
import json
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[2]
ANNEX = 'docs/技术开发/附录-原租赁媒体兼容入口与加密缓存源码.md'
TOOL = 'dev/tools/整理R026媒体入口源码.py'
TEST_FILES = [
    'dev/src/gateway/media/media_test.go',
    'dev/src/client/rentalmedia/status_test.go',
]
REFS = sorted(TEST_FILES + [ANNEX, TOOL,
    'dev/src/gateway/media/README.md',
    'dev/src/gateway/media/mapping.go',
    'dev/src/gateway/media/request.go',
    'dev/src/gateway/media/ingest.go',
    'dev/src/gateway/media/service.go',
    'dev/src/gateway/media/http.go',
    'dev/src/gateway/media/response.go',
    'dev/src/client/rentalmedia/status.go',
    'dev/src/client/node/rental_media_gateway.go',
    'dev/src/client/node/rental_comfy_owned.go',
    'dev/src/client/internal/rentaltest/fixture.go',
    'dev/src/internal/testsupport/rentaltest/fixture.go',
    'dev/src/internal/testsupport/rentaltest/README.md',
])
ITEMS = [
    ('051', '原租赁批准媒体档位与有限映射源码', 'MG-001—016：Images/legacy Videos/WAV speech/Responses资产、原workflow别名/参数槽/秒数或帧数、严格JSON/multipart；未验证实际SDK或模型输出'),
    ('052', '加密输入暂存与原调用幂等准入源码', 'MG-017—021：有界AES-GCM暂存/消费、原明文HMAC与同键冲突、提交前最坏结果容量预留；不创建T1或另收媒体费用'),
    ('053', '完整认证结果缓存与本人有界下载源码', 'MG-022—030：原结果真实状态、完整认证后缓存、固定保留/单Range/base64、逐块导出权限、断流不补成功尾；无最终ACK/usage/计费事实'),
    ('054', '原节点媒体入口持有与独立导出状态源码', 'MG-001/012/022/027/031：实际ComfyRental绑定、EXPORT与QUEUE分离、未知查询保留原结果、HTTP/回调/缓存真实join；仅本机STANDARD源码'),
    ('055', 'MG编号、媒体入口测试源码与双语全量待办', 'MG-032—036：共享测试夹具工程边界、边界测试定义、56任务/222子项及双语说明；全部未编译/未运行，完整代码后统一验证'),
]
STEPS = {
    'DEV-01': {'DEV-01.01', 'DEV-01.04'},
    'DEV-11': {'DEV-11.02', 'DEV-11.03'},
    'DEV-24': {'DEV-24.01', 'DEV-24.02', 'DEV-24.03'},
    'DEV-26': {'DEV-26.01', 'DEV-26.03', 'DEV-26.04'},
    'DEV-27': {'DEV-27.02', 'DEV-27.03', 'DEV-27.04'},
    'DEV-33': {'DEV-33.04'},
    'DEV-35': {'DEV-35.03', 'DEV-35.04'},
    'DEV-36': {'DEV-36.01', 'DEV-36.02', 'DEV-36.03'},
    'DEV-37': {'DEV-37.01', 'DEV-37.02', 'DEV-37.03', 'DEV-37.04'},
    'DEV-53': {'DEV-53.01', 'DEV-53.03'},
    'DEV-54': {'DEV-54.01', 'DEV-54.02'},
    'DEV-56': {'DEV-56.01', 'DEV-56.04'},
}
DETAIL = ('新增原租赁本机STANDARD媒体Handler、四类批准请求/有限映射、'
          '加密输入暂存/原HMAC幂等/提交前结果容量、完整认证结果后加密缓存/本人Range和原节点真实join源码；'
          '全部未编译或运行。公开SDK/原确认路由/监听与端到端传输/最终ACK、独立双方ready/'
          '可信小时计量/付费续租/物理停工对账/平台资金释放、完整实际引擎配方/OS低权限/E2E/'
          '性能/跨系统及其余生产接线仍待。全部56任务222子项未验收；完整生产代码、实际依赖接线'
          '和测试源码齐备后统一验证；本次仅源码检查点，不是完整开发轮。')


def load(name):
    return json.loads((ROOT / name).read_text(encoding='utf-8'))


def write(name, value):
    path = ROOT / name
    prior = path.read_bytes() if path.exists() else b''
    ending = '\r\n' if b'\r\n' in prior else '\n'
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(value.replace('\r\n', '\n').replace('\n', ending).encode('utf-8'))


def dump(name, record):
    write(name, json.dumps(record, ensure_ascii=False, indent=2) + '\n')


def section(name, marker, content):
    path = ROOT / name
    body = path.read_text(encoding='utf-8') if path.exists() else ''
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
        # Text inventory only. This does not parse, compile or execute Go.
        for name in re.findall(r'^func (Test\w+)\(', (ROOT/source).read_text(encoding='utf-8'), re.M):
            tests.append({'id': f'MG-T{len(tests)+1:03d}', 'name': name,
                          'source': source, 'status': 'SOURCE_ONLY_NOT_EXECUTED'})
    remaining = [
        'MG-033：原确认路由/公开SDK/网络监听/端到端媒体传输/最终ACK及独立留存',
        'MG-034：双方ready/可信小时区间/付费续租/真实渠道/物理停工对账/平台资金释放',
        'MG-035：完整实际环境配方/真实输出尺寸时长/OS低权限/E2E/性能/跨系统及Android资格',
        'MG-036：全部56/222生产代码/实际依赖接线/测试源码齐备，再统一编译测试调试与验收',
    ]
    integration = {
        'status': 'SOURCE_ONLY_UNVERIFIED', 'requirements': 'MG-001—036', 'annex': ANNEX,
        'local_standard_media_handler_source_wired': True,
        'node_original_session_binding_source_wired': True,
        'strict_profile_mapping_source_wired': True,
        'local_aes_gcm_staging_and_cache_source_wired': True,
        'original_idempotency_source_wired': True,
        'pre_submit_result_capacity_source_wired': True,
        'complete_authenticated_result_before_range_source_wired': True,
        'scope_separation_source_wired': True,
        'actual_http_callback_and_cache_join_source_wired': True,
        'public_network_listener_wired': False,
        'end_to_end_public_media_sdk_wired': False,
        'final_delivery_ack_wired': False,
        'platform_ready_hour_meter_paid_renewal_release_wired': False,
        'actual_runtime_bundle_present': False,
        'network_e2e_qualified': False, 'low_privilege_qualified': False,
        'compiled_or_executed': False, 'sql_changed_or_migrated': False,
        'physical_cleanup_or_sale_qualified': False,
        'full_development_round_complete': False,
        'test_definition_count': len(tests), 'remaining': remaining,
    }
    for record in (plan, stage):
        record['media_gateway_source_integration'] = deepcopy(integration)
        record['media_session_source_integration'].update({
            'local_standard_media_handler_source_wired': True,
            'media_gateway_annex': ANNEX,
            'end_to_end_public_media_sdk_wired': False,
            'remaining': ['MS-033：本机STANDARD Handler已有源码；公开SDK/远端传输/最终ACK和独立留存仍待']
                         + record['media_session_source_integration']['remaining'][1:]})
    for task in plan['tasks']:
        if task['id'] in STEPS:
            task['code_status'] = 'IN_PROGRESS'
            for step in task['steps']:
                if step['id'] in STEPS[task['id']]:
                    step['code_status'] = 'IN_PROGRESS'
                    step['implementation_refs'] = sorted(set(step.get('implementation_refs', []) + REFS))
    for task in status['tasks']:
        if task['id'] in STEPS:
            task['implementation_status'] = 'IN_PROGRESS'
            task['media_gateway_source_limit'] = DETAIL
            task['current_validation_status'] = 'NOT_EXECUTED'
            if task['id'] in {'DEV-24', 'DEV-27', 'DEV-36', 'DEV-37'}:
                task['remaining_detail'] = DETAIL
    status['activity_source'] = '2026-11-06 CODE_FIRST：MG-001—032 / IMP051—055新增源码；' + DETAIL
    for number, title, state in ITEMS:
        item = {'id': 'IMP-R026-'+number, 'title': title, 'status': state}
        stage['implemented_items'] = [old for old in stage['implemented_items'] if old['id'] != item['id']] + [item]
    stage['source_files'] = sorted(set(stage.get('source_files', []) + REFS))
    stage['tasks'] = deepcopy(status['tasks'])
    stage['media_gateway_test_definitions'] = {
        'tests': tests, 'ids': [t['id'] for t in tests], 'status': 'SOURCE_ONLY_NOT_EXECUTED'}
    for name, record in [('dev/progress/code-first-plan.json', plan),
                         ('dev/progress/R026-code-stage.json', stage),
                         ('dev/progress/implementation-status.json', status)]:
        dump(name, record)
    write_progress(plan, stage, status, tests)
    write_documentation(tests)
    print(f'Document inventory only: IMP051–055 / MG001–036 / {len(tests)} test definitions; all 56/222 open. No compiler/test/validator/migration/runtime invoked.')


def write_progress(plan, stage, status, tests):
    counts = Counter(t['implementation_status'] for t in status['tasks'])
    lines = ['# R026 全量代码阶段进展', '',
        '**进行中，SOURCE_ONLY / 未验证。** 全部生产代码、实际依赖接线和测试源码齐备后统一验证。新增051—055只是源码检查点，不是开发轮完成；历史内容按原范围阅读。', '',
        '| 编号 | 本阶段已写内容 | 实际状态 |', '|---|---|---|']
    lines += [f"| {i['id']} | {i['title']} | {i['status']} |" for i in stage['implemented_items']]
    lines += ['', f'新增[MG-001—036](../../{ANNEX})及{len(tests)}项测试源码定义。原RN67/CE31/MW44/CH29/WS23/MS27亦未编译/运行；本次没有SQL变更或迁移。', '',
        '当前状态：' + '；'.join(f'{k} {v}' for k, v in sorted(counts.items())) + '；全部222子项保持未验收。', '',
        '| 编号 | 任务 | 当前状态 | 全部仍待收口子项 |', '|---|---|---|---|']
    current = {t['id']: t for t in status['tasks']}
    for task in plan['tasks']:
        t = current[task['id']]
        lines.append(f"| {task['id']} | {task['title']} | {t['implementation_status']} | {'、'.join(t['remaining_step_ids'])} |")
    write('dev/progress/R026-全量代码阶段进展.md', '\n'.join(lines)+'\n')
    lines = ['# 全量代码优先实施清单', '', DETAIL, '',
        '先完成全部生产代码、实际依赖接线和测试源码，再批量验证。代码状态与验收分开；全部222子项未验收。', '',
        '| 子项 | 任务 | 未收口需求 | 代码状态 | 验收 |', '|---|---|---|---|---|']
    for task in plan['tasks']:
        for step in task['steps']:
            lines.append(f"| {step['id']} | {task['title']} | {step['code_requirement']} | {step['code_status']} | {step['acceptance_status']} |")
    write('dev/全量代码优先实施清单.md', '\n'.join(lines)+'\n')


def write_documentation(tests):
    section(ANNEX, '## MG 测试源码定义索引',
        '| 编号 | 测试定义 | 源码 | 状态 |\n|---|---|---|---|\n' + '\n'.join(
            f"| {t['id']} | {t['name']} | {t['source']} | 未编译/未运行 |" for t in tests))
    note = (f'R026新增[MG-001—036]({ANNEX})：原租赁本机STANDARD媒体入口、'
            'Images/明确旧版Videos/WAV speech/Responses资产映射、严格请求/独立scope、'
            'AES-GCM暂存/原HMAC幂等/提交前缓存配额、完整认证结果后加密缓存/本人单Range和原节点真实join源码。'
            f'{len(tests)}项新增测试定义均未编译/运行。公开SDK/网络监听/E2E媒体传输/最终ACK、'
            '双方ready/可信小时计量/付费续租/物理停工对账/平台资金释放、完整实际配方/低权限/性能仍待。'
            '全部56任务222子项开放；无SQL变更或迁移。完整生产代码/实际依赖/测试源码齐备后统一验证，本次不是开发轮完成。')
    english = (f'R026 adds [MG-001–036]({ANNEX}): an original-rental local STANDARD media handler, '
               'approved Images/explicit legacy Videos/WAV speech/Responses assets, strict requests and '
               'independent scopes, AES-GCM staging, original-id HMAC replay, result capacity reserved '
               'before submission, complete authenticated results before encrypted caching/ranges, and '
               f'actual node-owned HTTP/callback/cache joins. All {len(tests)} new test definitions remain '
               'uncompiled/unexecuted. Public SDK/listeners/E2E media transport/final ACK, independent '
               'readiness/hour meters/paid renewal/physical reconciliation/platform fund release, '
               'complete real recipes/low privilege/performance remain open. All 56 tasks/222 steps '
               'remain unaccepted. No SQL changes or migrations. Complete all production source, '
               'dependency wiring and test source before batch validation. This is a source checkpoint, '
               'not a completed development round.')
    marker = '## R026 MG-001—036 原租赁媒体入口与加密缓存源码 / Original-rental media gateway source'
    for name, content in [('dev/repository/README.zh-CN.md.in', note), ('dev/repository/README.en.md.in', english)]:
        section(name, marker, content)
        body = (ROOT/name).read_text(encoding='utf-8')
        body = re.sub(r'\n<!-- R026-[A-Z-]+-CURRENT -->.*?<!-- /R026-[A-Z-]+-CURRENT -->\n', '\n', body, flags=re.S)
        title, rest = body.split('\n', 1)
        write(name, title+'\n\n<!-- R026-MG-CURRENT -->\n'+content+'\n<!-- /R026-MG-CURRENT -->\n'+rest)
    for name in ['dev/repository/USAGE.md', 'dev/repository/SYSTEM_DESIGN.md']:
        section(name, marker, note.replace('(docs/', '(../../docs/')+'\n\n'+english.replace('(docs/', '(../../docs/'))
    for name, content in [('CHANGELOG.zh-CN.md', note), ('CHANGELOG.md', english)]:
        body = (ROOT/name).read_text(encoding='utf-8')
        body = re.sub(r'(?m)^- R026(?:新增| adds )\[MG-001[—–]036\].*\n', '', body)
        if content not in body:
            title, rest = body.split('\n', 1)
            write(name, title+'\n\n- '+content+'\n'+rest)
    for name in ['AGENTS.md', 'dev/AGENTS.md']:
        section(name, marker, note.replace('(docs/', '(../docs/' if name.startswith('dev/') else '(docs/')+
            '\n\n当前唯一增量整理器为'+TOOL+'；禁止重跑旧MS/WS/CH/CE/MW/RN整理器覆盖本次MG源码元数据。'
            '平台001—015双份SQL、schema.sql/host-schema.sql/native002/native003及source.json原字节冻结。'
            '当前不编译、测试、核验、迁移、运行内核/模型/数据库/设备/浏览器；全量生产代码、实际接线与测试源码后统一验证。'
            '禁止本地虚拟化，DEV-55延后、DEV-32无证明关闭；浏览器使用后关闭不用页面。')
    for name, prefix in [
        ('dev/src/gateway/media/README.md', '../../../../'),
        ('dev/src/client/rentalmedia/README.md', '../../../../'),
        ('dev/src/client/node/README.md', '../../../../'),
        ('dev/src/client/protocol/rentalaccess/README.md', '../../../../../'),
        ('dev/progress/README.md', '../../')]:
        section(name, marker, note.replace('(docs/', '('+prefix+'docs/'))
    for name in [
        'docs/技术开发/附录-原租赁媒体会话监督与停止回收源码.md',
        'docs/技术开发/附录-租内媒体工作区与结果托管源码.md',
        'docs/技术开发/附录-小时租赁授权与累计计量源码细则.md',
        'docs/技术开发/附录-协议字段映射与兼容档位.md',
        'docs/技术开发/07-统一协议网关与Sub2API改造.md',
        'docs/技术开发/00-技术文档导航与实施基线.md',
        'docs/协议/统一API与本地加密网关需求.md']:
        prefix = '../技术开发/' if name.startswith('docs/协议/') else ''
        section(name, marker, note.replace('(docs/技术开发/', '('+prefix))


if __name__ == '__main__':
    main()
