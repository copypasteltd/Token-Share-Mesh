"""Append R026 rental-control source metadata; never builds or executes tests."""
from __future__ import annotations

import hashlib
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
ANNEX = 'docs/技术开发/附录-小时租赁授权与累计计量源码细则.md'
MIGRATION = '015_rental_control_history.sql'
REFS = [
    'dev/src/internal/rental/control.go', 'dev/src/internal/rental/expectation.go',
    'dev/src/client/protocol/rentalaccess/transport.go', 'dev/src/client/protocol/rentalaccess/expectation.go',
    'dev/src/client/protocol/rentalaccess/window.go', 'dev/src/client/protocol/rentalaccess/transport_test.go',
    'dev/src/client/protocol/rentalaccess/expectation_test.go', 'dev/src/client/protocol/devicecontrol/rental.go',
    'dev/src/client/node/rental_journal.go', 'dev/src/client/node/rental_control.go',
    'dev/src/client/node/rental_journal_test.go', 'dev/src/client/node/store.go',
    'dev/src/tests/integration/rental_control_test.go',
    'dev/src/api/internal/http/rental.go', 'dev/src/api/internal/http/routes.go', 'dev/src/api/internal/http/routes.json',
    'docs/技术开发/database/migrations/' + MIGRATION,
    'dev/src/internal/persistence/migrations/' + MIGRATION, ANNEX,
]
ITEMS = [
    ('015', '最长45秒持续控制的实际签名HTTP/事务源码', 'INT-097、原序号/原键原回复、STOP水位、原资金/资格/占用重核、DATA-071/前向015；未编译/迁移/运行'),
    ('016', '原设备独立读取原DB授权摘要与本机批准元组绑定', 'INT-098、只读安全metadata、明确控制源/原设备请求签名、公钥pin、完整原manifest/connection匹配；不从来客Bearer制造可信摘要'),
    ('017', '原节点OS锁/系统密钥下的加密控制journal与传输连接', '独立派生AEAD、原子替换/flush、先存序号再外发、重启无ALLOW、日志未知关闭；原生/Android物理执行门闩仍待'),
    ('018', 'RN-051—070、14项新增测试源码及双语文档同步', '共42项租赁测试定义均未运行；98项规划操作/71表、48项控制HTTP源码；全部56/222仍待收口'),
]
DETAIL = ('原签名设备持续控制/原DB摘要读取、原序号签名回复和加密journal接线源码已写；'
          '本机原批准准备记录消费者、实际共享物理门闩/租内invocation、独立停工watchdog/水位、Android、'
          '真实收单/独立ready证据生产、付费续租、确认计量及真实清理释放仍待，不能开放执行或标记代码完成。')
REMAINING = [
    '真实支付宝收单验签/原号恢复、追加支付/续租附约及追加容量原子落库',
    '独立连接探测和本机就绪证据生产，设备自报不能自动APPROVED',
    '原公共manifest的本机批准记录消费者、租内媒体子调用inbox及参考素材/分块密文',
    '持续控制接入实际共享物理槽、独立停工watchdog、原生及Android租赁执行和停止/导出/清理恢复',
    '完整区间/故障独立确认、不可变计量修订、实际清理释放和真实财务资格',
    '独立防回滚水位、journal容量回收/原grant终止证明；不能删除旧水位后重建权限',
    '全56任务/222子项其余生产代码及测试源码后统一编译/测试/调试',
]


def load(name):
    return json.loads((ROOT / name).read_text(encoding='utf-8'))


def write(name, text):
    p = ROOT / name
    before = p.read_bytes() if p.exists() else b''
    newline = '\r\n' if b'\r\n' in before else '\n'
    p.write_bytes(text.replace('\r\n', '\n').replace('\n', newline).encode('utf-8'))


def dump(name, value):
    write(name, json.dumps(value, ensure_ascii=False, indent=2) + '\n')


def main():
    source = 'docs/技术开发/database/migrations/' + MIGRATION
    raw = (ROOT / source).read_bytes()
    digest = hashlib.sha256(raw).hexdigest()
    provenance = 'dev/src/internal/persistence/migrations/source-v15.json'
    if (ROOT / provenance).exists() and load(provenance)['source_sha256'] != digest:
        raise ValueError('已登记015不得覆盖；后续数据库更改使用016+')
    (ROOT / 'dev/src/internal/persistence/migrations' / MIGRATION).write_bytes(raw)
    dump(provenance, {'source': source, 'source_sha256': digest, 'migration': MIGRATION,
        'migration_sha256': digest, 'policy': '015 append-only signed control metadata; source only, unexecuted, no physical release or paid renewal'})
    numbers = load('dev/planning/编号登记-v1.0.json')
    numbers['operations'].update(confirmRentalControl='INT-097', getRentalGrantExpectation='INT-098')
    numbers['tables']['rental_control_history'] = 'DATA-071'
    dump('dev/planning/编号登记-v1.0.json', numbers)
    routes = load('dev/src/api/internal/http/routes.json')
    existing = {o['operation_id'] for o in routes['operations']}
    operations = [
        ('confirmRentalControl', 'POST', '/runner/rental-sessions/{session_id}/control'),
        ('getRentalGrantExpectation', 'GET', '/runner/rental-sessions/{session_id}/access-grants/{grant_id}'),
    ]
    for operation, method, path in operations:
        if operation not in existing:
            routes['operations'].append({'operation_id': operation, 'method': method, 'path': path,
                'handler': operation, 'anonymous': False})
    routes['scope'] = (f"{len(routes['operations'])} explicit control operations; nine rental handlers and native encrypted-control journal are R026 SOURCE_ONLY, uncompiled/unverified; no native rental execution, payment qualification or public inference admission")
    dump('dev/src/api/internal/http/routes.json', routes)
    plan = load('dev/progress/code-first-plan.json')
    status = load('dev/progress/implementation-status.json')
    stage = load('dev/progress/R026-code-stage.json')
    integration = plan['rental_source_integration']
    integration.update(source_status='DOMAIN_JWS_METER_PLATFORM_CONTROL_AND_ENCRYPTED_JOURNAL_SOURCE_ONLY',
        stored_grant_expectation_source_wired=True, control_transport_and_encrypted_journal_wired=True,
        control_transport_status='SOURCE_ONLY_NOT_COMPILED_OR_EXECUTED', encrypted_journal_status='SOURCE_ONLY_NOT_COMPILED_OR_EXECUTED',
        independently_approved_native_context_consumer_wired=False, independent_rollback_watermark_wired=False,
        native_stop_watchdog_wired=False, android_session_executor_wired=False, physical_session_gate_wired=False,
        cleanup_release_wired=False, sql_migrations_added=[14, 15], validation_executed=False, remaining=REMAINING)
    integration['registered_rental_operations'] = list(dict.fromkeys(integration['registered_rental_operations'] + [o[0] for o in operations]))
    related = {'DEV-02', 'DEV-03', 'DEV-18', 'DEV-24', 'DEV-33', 'DEV-35', 'DEV-36', 'DEV-51', 'DEV-53', 'DEV-54', 'DEV-56'}
    for task in plan['tasks']:
        if task['id'] in related:
            if task['code_status'] == 'OPEN':
                task['code_status'] = 'IN_PROGRESS'
            for step in task['steps']:
                step['implementation_refs'] = sorted(set(step.get('implementation_refs', []) + REFS))
                if step['code_status'] == 'OPEN':
                    step['code_status'] = 'IN_PROGRESS'
                step['current_source_limit'] = DETAIL
    for task in status['tasks']:
        if task['id'] in related:
            task['current_source_refs'] = sorted(set(task.get('current_source_refs', []) + REFS))
            task['current_code_components'] = sorted(set(task.get('current_code_components', []) + ['IMP-R026-' + n for n, _, _ in ITEMS]))
            task['remaining_detail'] = DETAIL
            task['current_validation_status'] = 'NOT_EXECUTED'
    status['activity_source'] = ('2026-11-06 CODE_FIRST：新增持续签名控制/原DB摘要查询2项实际HTTP源码，前向015追加控制历史；'
        '原节点OS锁/系统密钥下的AES-GCM journal和实际控制传输源码；RN-001—070，共42项租赁测试定义未运行。'
        '没有执行编译、测试、迁移、设备/模型或虚拟化。全部56/222未收口，R025仅为历史验证身份。')
    for obj in (plan, stage):
        obj['rental_source_integration'] = integration
    for n, title, current in ITEMS:
        stage['implemented_items'] = [i for i in stage['implemented_items'] if i['id'] != 'IMP-R026-' + n]
        stage['implemented_items'].append({'id': 'IMP-R026-' + n, 'title': title, 'status': current})
    for item in stage['implemented_items']:
        if item['id'] == 'IMP-R026-008':
            item['status'] = '一次JWS、平台持续控制/原摘要读取和原加密journal接线源码已写；Android/物理门闩及运行验证仍待'
    stage['source_files'] = sorted(set(stage['source_files'] + REFS))
    stage['tasks'] = status['tasks']
    stage['rental_boundary_test_definitions'] = {'ids': [f'RN-T{n:03d}' for n in range(1, 43)],
        'status': 'SOURCE_ONLY_NOT_EXECUTED', 'registered_http_operations_added': 9,
        'new_contract_operations_added': 3, 'new_sql_migrations_added': 2,
        'scope': 'Go domain/JWS/control/meter/platform/config/native encrypted-journal/transport test source; no Android, channel or physical session acceptance'}
    dump('dev/progress/code-first-plan.json', plan)
    dump('dev/progress/implementation-status.json', status)
    dump('dev/progress/R026-code-stage.json', stage)
    lines = ['# R026 全量代码阶段进展', '',
        '**进行中，SOURCE_ONLY / 未验证。** 按用户2026-11-06要求，先完成全部生产代码、真实依赖接线和测试源码，再集中编译、测试、调试。源码检查点不代表本轮完成。', '',
        '| 编号 | 本阶段已写内容 | 实际状态 |', '|---|---|---|']
    lines += [f"| {i['id']} | {i['title']} | {i['status']} |" for i in stage['implemented_items']]
    lines += ['', f'租赁细则：[RN-001—070](../../{ANNEX})。42项租赁测试定义均未运行；014/015仅为SQL源码，未启动数据库。', '',
        '## 全部56任务：完整未收口清单', '', '| 编号 | 任务 | 当前状态 | 全部仍待收口子项 |', '|---|---|---|---|']
    for task, progress in zip(plan['tasks'], status['tasks']):
        lines.append(f"| {task['id']} | {task['title']} | {progress['implementation_status']} | {'、'.join(progress['remaining_step_ids'])} |")
    write('dev/progress/R026-全量代码阶段进展.md', '\n'.join(lines) + '\n')
    inventory = ['# 全量代码优先实施清单', '',
        'R026 · CODE_FIRST · 全56任务/222子项；生产代码、真实依赖接线与测试源码完成后再统一编译/测试/调试。代码进展与运行验收分开，全部原子待办保持开放。DEV-55延后，DEV-32无真实证明安全关闭。', '',
        '[当前进展](progress/R026-全量代码阶段进展.md) · [机器可读清单](progress/code-first-plan.json)', '']
    for task in plan['tasks']:
        inventory += [f"## {task['id']} {task['title']}", '', task['requirement'], '',
            '| 编号 | 开发项 | 完整代码要求 | 代码状态 | 验收状态 |', '|---|---|---|---|---|']
        for step in task['steps']:
            inventory.append(f"| {step['id']} | {step['title']} | {step['code_requirement'].replace('|', '&#124;')} | {step['code_status']} | {step['acceptance_status']} |")
        inventory.append('')
    write('dev/全量代码优先实施清单.md', '\n'.join(inventory))
    print('Source metadata written: 18 IMP entries; original 56/222 remain open; no build/test/migration executed.')


if __name__ == '__main__':
    main()
