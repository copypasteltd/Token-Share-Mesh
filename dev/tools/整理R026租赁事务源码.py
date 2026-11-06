"""Generate R026 source-only inventory/docs metadata. Never runs product/tests."""
from __future__ import annotations

import hashlib
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
ANNEX = 'docs/技术开发/附录-小时租赁授权与累计计量源码细则.md'
MIGRATION = '014_rental_authority_context.sql'
SOURCE = 'docs/技术开发/database/migrations/' + MIGRATION
COPY = 'dev/src/internal/persistence/migrations/' + MIGRATION
REFS = [
    'dev/src/internal/rental/service.go', 'dev/src/internal/rental/store.go',
    'dev/src/internal/rental/readiness.go', 'dev/src/internal/rental/access.go',
    'dev/src/internal/rental/service_test.go', 'dev/src/tests/integration/rental_test.go',
    'dev/src/internal/market/catalog.go', 'dev/src/internal/testsupport/market.go',
    'dev/src/internal/identity/sessions.go', 'dev/src/internal/dispatch/device/auth.go',
    'dev/src/api/internal/http/rental.go', 'dev/src/api/internal/http/routes.go',
    'dev/src/api/internal/http/routes.json', 'dev/src/api/internal/http/server.go',
    'dev/src/api/cmd/p11-api/rental.go', 'dev/src/api/cmd/p11-api/rental_test.go',
    'dev/src/api/cmd/p11-api/main.go', SOURCE, COPY,
    'dev/src/internal/persistence/migrations/source-v14.json', ANNEX,
]
OPERATIONS = [
    ('createRentalSession', 'POST', '/rental-sessions'),
    ('getRentalSession', 'GET', '/rental-sessions/{session_id}'),
    ('claimRentalPreparation', 'POST', '/runner/rental-sessions/{session_id}/preparation'),
    ('recordRentalDeviceReady', 'POST', '/rental-sessions/{session_id}/device-ready'),
    ('confirmRentalReady', 'POST', '/rental-sessions/{session_id}/ready'),
    ('stopRentalSession', 'POST', '/rental-sessions/{session_id}/stop'),
    ('createRentalAccessGrant', 'POST', '/rental-sessions/{session_id}/access-grants'),
]
ITEMS = [
    ('011', '原会话实际事务、原始清单/历史与前向014', '原订单/资金/资格/全额预算/原物理owner和安全事件接线源码已写；未迁移/测试，端侧门闩未接通'),
    ('012', '原设备准备、独立证据及双端ready和7项HTTP接线', '新增INT-096；其余沿用原操作；证据生产者及Android消费仍待，未验证'),
    ('013', '一次grant提交后响应、原键安全恢复及显式替换', '原Bearer不落幂等/事件/DB；独立配置签发与公钥pin已接启动源码，未验证'),
    ('014', 'RN-031—050、10项新增测试源码及完整文档同步', '源码已写，测试未执行；56任务/222子项原范围和开放验收全部保留'),
]
DETAIL = ('R026已有原小时合同/SETTLED资金事实重核、占位/准备/双端ready/停止、一次grant及014 SQL/HTTP源码；'
          '尚未编译/迁移/测试。真实渠道处理和独立就绪证据生产、付费续租/控制/计量/清理释放及Android/桌面会话消费者仍待，不能开放交易或标记代码完成。')
REMAINING = [
    '真实支付宝收单验签/原号恢复、追加支付/续租附约及追加容量原子落库',
    '独立连接探测和本机就绪证据生产，不能自动APPROVED设备自报',
    '原公共manifest的端侧消费者、租内媒体子调用inbox和参考素材/分块密文链路',
    '签名持续控制的实际请求/回应、原加密journal和独立水位接线',
    '完整区间/故障独立确认、不可变计量修订和真实财务资格',
    'Android/桌面共享物理槽、停止/导出/实际清理恢复与安全释放',
    '全56任务/222子项其余生产代码及测试源码后统一编译/测试/调试',
]


def read_json(name: str):
    return json.loads((ROOT / name).read_text(encoding='utf-8'))


def write(name: str, text: str):
    path = ROOT / name
    before = path.read_bytes() if path.exists() else b''
    newline = '\r\n' if b'\r\n' in before else '\n'
    path.write_bytes(text.replace('\r\n', '\n').replace('\n', newline).encode('utf-8'))


def dump(name: str, value):
    write(name, json.dumps(value, ensure_ascii=False, indent=2) + '\n')


def main():
    raw = (ROOT / SOURCE).read_bytes()
    digest = hashlib.sha256(raw).hexdigest()
    provenance = 'dev/src/internal/persistence/migrations/source-v14.json'
    if (ROOT / provenance).exists() and read_json(provenance)['source_sha256'] != digest:
        raise ValueError('已登记014不得重写；新的数据库变更另加前向迁移')
    (ROOT / COPY).write_bytes(raw)
    dump(provenance, {'source': SOURCE, 'source_sha256': digest, 'migration': MIGRATION,
         'migration_sha256': digest, 'policy': '014 forward-only original hourly session context/history/access; unexecuted source, physical release and paid renewal remain closed'})
    numbers = read_json('dev/planning/编号登记-v1.0.json')
    numbers['operations']['claimRentalPreparation'] = 'INT-096'
    numbers['tables'].update(rental_authority_contexts='DATA-069', rental_session_history='DATA-070')
    dump('dev/planning/编号登记-v1.0.json', numbers)
    routes = read_json('dev/src/api/internal/http/routes.json')
    existing = {o['operation_id'] for o in routes['operations']}
    for operation, method, path in OPERATIONS:
        if operation not in existing:
            routes['operations'].append({'operation_id': operation, 'method': method, 'path': path, 'handler': operation, 'anonymous': False})
    routes['scope'] = (f"{len(routes['operations'])} explicit control operations; rental session/preparation/ready/stop and one-time access are R026 source-only, uncompiled/unverified; no native rental execution, payment qualification or public inference admission")
    dump('dev/src/api/internal/http/routes.json', routes)
    integration = {'source_status': 'DOMAIN_JWS_CONTROL_METER_AND_PLATFORM_TRANSACTION_SOURCE_ONLY',
        'library': 'github.com/golang-jwt/jwt/v5 v5.3.1 (MIT)', 'authority_profile': 'P11_RENTAL_ACCESS_EDDSA_1',
        'api_registered': True, 'registered_rental_operations': [o[0] for o in OPERATIONS],
        'storage_transaction_wired': True, 'storage_transaction_status': 'SOURCE_ONLY_NOT_EXECUTED',
        'grant_once_response_source_wired': True, 'database_physical_session_owner_source_wired': True,
        'independent_readiness_producer_wired': False, 'paid_renewal_persistence_wired': False,
        'android_session_executor_wired': False, 'physical_session_gate_wired': False,
        'control_transport_and_encrypted_journal_wired': False, 'cleanup_release_wired': False,
        'sql_migrations_added': [14], 'validation_executed': False, 'remaining': REMAINING}
    plan = read_json('dev/progress/code-first-plan.json')
    status = read_json('dev/progress/implementation-status.json')
    stage = read_json('dev/progress/R026-code-stage.json')
    related = {'DEV-02', 'DEV-03', 'DEV-12', 'DEV-18', 'DEV-33', 'DEV-34', 'DEV-35', 'DEV-36', 'DEV-51', 'DEV-53', 'DEV-56'}
    for task in plan['tasks']:
        if task['id'] in related:
            # Source progress never closes an original acceptance step.
            if task['code_status'] == 'OPEN': task['code_status'] = 'IN_PROGRESS'
            for step in task['steps']:
                step['implementation_refs'] = sorted(set(step.get('implementation_refs', []) + REFS))
                if step['code_status'] == 'OPEN': step['code_status'] = 'IN_PROGRESS'
                step['current_source_limit'] = DETAIL
    for task in status['tasks']:
        if task['id'] in related:
            task['current_source_refs'] = sorted(set(task.get('current_source_refs', []) + REFS))
            task['current_code_components'] = sorted(set(task.get('current_code_components', []) + ['IMP-R026-' + n for n, _, _ in ITEMS]))
            task['remaining_detail'] = DETAIL
            task['current_validation_status'] = 'NOT_EXECUTED'
    status['activity_source'] = ('2026-11-06 CODE_FIRST：新增小时租赁原订单/资金重核、会话/预算/物理owner/history事务、7项HTTP及提交后一次grant源码，前向014；RN-001—050和共28项租赁测试定义未运行。全部56/222未收口，R025仅为历史验证身份。')
    for obj in (plan, stage): obj['rental_source_integration'] = integration
    for n, title, current in ITEMS:
        stage['implemented_items'] = [i for i in stage['implemented_items'] if i['id'] != 'IMP-R026-' + n]
        stage['implemented_items'].append({'id': 'IMP-R026-' + n, 'title': title, 'status': current})
    for item in stage['implemented_items']:
        if item['id'] == 'IMP-R026-007': item['status'] = '领域与原会话/预算/物理owner事务源码已写；付费续租/清理释放和native执行仍待，未验证'
        if item['id'] == 'IMP-R026-008': item['status'] = 'JWS和一次HTTP响应源码已接；持续控制传输/原加密journal/Android和物理门闩未接通'
    stage['source_files'] = sorted(set(stage['source_files'] + REFS))
    stage['tasks'] = status['tasks']
    stage['rental_boundary_test_definitions'] = {'ids': [f'RN-T{n:03d}' for n in range(1, 29)],
        'status': 'SOURCE_ONLY_NOT_EXECUTED', 'registered_http_operations_added': 7,
        'new_contract_operations_added': 1, 'new_sql_migrations_added': 1,
        'scope': 'Go domain/JWS/control/meter/transaction/config test source; not Android, channel or native-session acceptance'}
    dump('dev/progress/code-first-plan.json', plan)
    dump('dev/progress/implementation-status.json', status)
    dump('dev/progress/R026-code-stage.json', stage)
    lines = ['# R026 全量代码阶段进展', '', '**进行中，SOURCE_ONLY / 未验证。** 按用户2026-11-06要求，先完成全部生产代码、真实依赖接线和测试源码，再集中编译、测试、调试。源码检查点不代表本轮完成。', '',
        '| 编号 | 本阶段已写内容 | 实际状态 |', '|---|---|---|']
    lines += [f"| {i['id']} | {i['title']} | {i['status']} |" for i in stage['implemented_items']]
    lines += ['', f'租赁细则：[RN-001—050](../../{ANNEX})。28项租赁测试定义均未运行；014只作为SQL源码保存，未启动数据库。', '',
        '## 全部56任务：完整未收口清单', '', '| 编号 | 任务 | 当前状态 | 全部仍待收口子项 |', '|---|---|---|---|']
    for task, progress in zip(plan['tasks'], status['tasks']):
        lines.append(f"| {task['id']} | {task['title']} | {progress['implementation_status']} | {'、'.join(progress['remaining_step_ids'])} |")
    write('dev/progress/R026-全量代码阶段进展.md', '\n'.join(lines) + '\n')
    inventory = ['# 全量代码优先实施清单', '', 'R026 · CODE_FIRST · 全56任务/222子项；生产代码、真实依赖接线与测试源码完成后再统一编译/测试/调试。代码进展与运行验收分开，全部原子待办保持开放。DEV-55延后，DEV-32无真实证明安全关闭。', '',
        '[当前进展](progress/R026-全量代码阶段进展.md) · [机器可读清单](progress/code-first-plan.json)', '']
    for task in plan['tasks']:
        inventory += [f"## {task['id']} {task['title']}", '', task['requirement'], '', '| 编号 | 开发项 | 完整代码要求 | 代码状态 | 验收状态 |', '|---|---|---|---|---|']
        for step in task['steps']:
            inventory.append(f"| {step['id']} | {step['title']} | {step['code_requirement'].replace('|', '&#124;')} | {step['code_status']} | {step['acceptance_status']} |")
        inventory.append('')
    write('dev/全量代码优先实施清单.md', '\n'.join(inventory))
    print('Generated source-only rental metadata: 14 IMP entries; all 56 tasks/222 steps retained; no product or tests executed.')


if __name__ == '__main__':
    main()
