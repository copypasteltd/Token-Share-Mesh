"""Append native rental SOURCE_ONLY inventory; no build, migration or tests."""
from __future__ import annotations

import hashlib
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
ANNEX = 'docs/技术开发/附录-小时租赁授权与累计计量源码细则.md'
NATIVE = 'dev/src/client/execution/schema-002-rental.sql'
PROVENANCE = 'dev/src/client/execution/schema-002-rental.source.json'
REFS = [
    'dev/src/shared/contractspec/spec.go', 'dev/src/internal/market/catalog.go',
    'dev/src/internal/rental/service.go', 'dev/src/client/protocol/rentalaccess/preparation.go',
    'dev/src/client/protocol/rentalaccess/preparation_test.go',
    'dev/src/client/protocol/rentalaccess/claims.go', 'dev/src/client/protocol/rentalaccess/control.go',
    'dev/src/client/protocol/rentalaccess/window.go', 'dev/src/client/execution/migration.go',
    'dev/src/client/execution/store.go', 'dev/src/client/execution/store_test.go',
    'dev/src/client/execution/gate.go', 'dev/src/client/execution/rental.go',
    'dev/src/client/execution/rental_history.go', 'dev/src/client/execution/rental_invocation.go',
    'dev/src/client/execution/rental_test.go', 'dev/src/client/execution/README.md', NATIVE, PROVENANCE,
    'dev/src/client/node/rental_journal.go', 'dev/src/client/node/rental_preparation.go',
    'dev/src/client/node/rental_session.go', 'dev/src/client/node/rental_session_test.go', ANNEX,
]
ITEMS = [
    ('019', '原生租赁前向版本2与原物理资源共门闩',
     '旧DDL原字节保留，原占用/inbox/双水位复制；新增4张端侧元数据表、session原owner和独立本机代次；未执行迁移'),
    ('020', '原本机批准加密保存与签名控制提交同物理槽',
     '完整原Manifest/物理摘要/connection/issuer，先加密flush再占位；独立原DB摘要/固定pin/JWS/控制同时核对，重开无ALLOW；源码未验证'),
    ('021', '租内串行一次提交、原键恢复及停止清理门槛',
     '原ID仅查/已知backend不换绑、子清理保留父占位、通用清理拒绝RENTAL、原历史不控制下一owner；真实内核证明/平台释放仍待'),
    ('022', 'RN-071—090、11项新增测试源码及完整双语接线文档',
     '租赁共53项定义未编译/运行；48项控制HTTP/9项租赁、98项规划操作/71平台表不变；全部56/222未收口'),
]
DETAIL = ('原本机批准加密消费者、平台独立原摘要/签名控制→同resources物理会话门闩与串行租内inbox已有SOURCE_ONLY源码，'
          '新增端侧前向版本2保留旧占用/双水位，原键一次提交/停止/原事实清理门槛及历史恢复已写；'
          '真实租赁内核适配/认证输入/独立停工watchdog、Android会话、独立ready/真实收单、付费续租、确认计量及平台清理回执/owner/预算释放仍待。'
          '完整56/222代码齐备后统一编译/测试，不标代码完成或已验收。')
REMAINING = [
    '完整可信本人批准UI/配置与独立连接探测/实际就绪证据生产，不能从网络任务自动批准',
    '真实原生租赁引擎/认证输入与参考素材/分块密文、独立停工watchdog及导出恢复',
    'Android原session授权/共手机执行槽/租内媒体执行与清理回执完整接线',
    '真实支付宝收单验签/原号恢复、追加支付/续租附约及追加容量原子落库',
    '完整区间/故障独立确认与不可变计量修订、真实财务资格',
    '真实内核清理证明、平台独立回执及同原owner/预算原子释放，数据库后续只加016+',
    '独立防回滚水位、安全容量回收与原grant终止证明，不能删除旧水位后重建权限',
    '全56任务/222子项其余生产代码及测试源码后统一编译/测试/调试',
]


def load(name):
    return json.loads((ROOT / name).read_text(encoding='utf-8'))


def write(name, value):
    path = ROOT / name
    before = path.read_bytes() if path.exists() else b''
    ending = '\r\n' if b'\r\n' in before else '\n'
    path.write_bytes(value.replace('\r\n', '\n').replace('\n', ending).encode('utf-8'))


def dump(name, value):
    write(name, json.dumps(value, ensure_ascii=False, indent=2) + '\n')


def main():
    original = 'dev/src/client/execution/schema.sql'
    digest = hashlib.sha256((ROOT / NATIVE).read_bytes()).hexdigest()
    base_digest = hashlib.sha256((ROOT / original).read_bytes()).hexdigest()
    if (ROOT / PROVENANCE).exists():
        prior = load(PROVENANCE)
        if prior['migration_sha256'] != digest or prior['base_source_sha256'] != base_digest:
            raise ValueError('已登记端侧DDL不可覆盖；后续变更新增前向迁移')
    dump(PROVENANCE, {'base_source': original, 'base_source_sha256': base_digest,
        'migration': NATIVE, 'migration_sha256': digest, 'native_user_version': 2,
        'status': 'SOURCE_ONLY_NOT_EXECUTED',
        'policy': 'Original bytes retained after registration; subsequent changes require native forward migrations. No physical cleanup or payment qualification.'})

    plan = load('dev/progress/code-first-plan.json')
    status = load('dev/progress/implementation-status.json')
    stage = load('dev/progress/R026-code-stage.json')
    integration = plan['rental_source_integration']
    integration.update(
        source_status='PLATFORM_CONTROL_ENCRYPTED_APPROVAL_SHARED_NATIVE_GATE_CHILD_INBOX_SOURCE_ONLY',
        independently_approved_native_context_consumer_wired=True,
        approved_context_consumer_status='SOURCE_ONLY_OWNER_UI_AND_INDEPENDENT_READINESS_PRODUCER_STILL_OPEN',
        physical_session_gate_wired=True,
        physical_session_gate_status='SOURCE_ONLY_SHARED_SQL_ROW_NO_ENGINE_OR_PHYSICAL_VALIDATION',
        serial_invocation_inbox_source_wired=True,
        native_stop_cleanup_gates_source_wired=True,
        actual_native_engine_cleanup_producer_wired=False,
        platform_cleanup_receipt_and_release_wired=False,
        native_stop_watchdog_wired=False, independent_rollback_watermark_wired=False,
        android_session_executor_wired=False, cleanup_release_wired=False,
        native_forward_migrations_added=[2], validation_executed=False, remaining=REMAINING)
    related = {'DEV-02', 'DEV-03', 'DEV-07', 'DEV-10', 'DEV-18', 'DEV-20', 'DEV-21',
               'DEV-24', 'DEV-33', 'DEV-35', 'DEV-36', 'DEV-51', 'DEV-53', 'DEV-54', 'DEV-56'}
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
    status['activity_source'] = ('2026-11-06 CODE_FIRST：原本机批准加密消费者、同resources行的原session占用/签名控制、'
        '租内串行一次提交inbox和原停止清理门槛源码；新端侧前向版本2，平台001—015/旧客户端DDL不改。'
        'RN-001—090、53项租赁测试定义未编译/迁移/运行。完整56/222未收口，R025证据仅为历史身份。')
    plan['rental_source_integration'] = integration
    stage['rental_source_integration'] = integration
    for number, title, state in ITEMS:
        stage['implemented_items'] = [i for i in stage['implemented_items'] if i['id'] != 'IMP-R026-' + number]
        stage['implemented_items'].append({'id': 'IMP-R026-' + number, 'title': title, 'status': state})
    stage['source_files'] = sorted(set(stage['source_files'] + REFS))
    stage['tasks'] = status['tasks']
    tests = stage['rental_boundary_test_definitions']
    tests.update(ids=[f'RN-T{n:03d}' for n in range(1, 54)], status='SOURCE_ONLY_NOT_EXECUTED',
        native_forward_migrations_added=1,
        scope='53 domain/JWS/control/meter/platform/config/native encrypted approval/shared gate/serial inbox/transport test definitions; no compilation, migrations, Android, payment-channel or physical session acceptance')
    stage['native_migration_source'] = PROVENANCE
    dump('dev/progress/code-first-plan.json', plan)
    dump('dev/progress/implementation-status.json', status)
    dump('dev/progress/R026-code-stage.json', stage)

    lines = ['# R026 全量代码阶段进展', '',
        '**进行中，SOURCE_ONLY / 未验证。** 按用户2026-11-06要求，先完成全部生产代码、真实依赖接线和测试源码，再集中编译、测试、调试。源码检查点不代表本轮完成。', '',
        '| 编号 | 本阶段已写内容 | 实际状态 |', '|---|---|---|']
    lines += [f"| {i['id']} | {i['title']} | {i['status']} |" for i in stage['implemented_items']]
    lines += ['', f'租赁细则：[RN-001—090](../../{ANNEX})。53项租赁测试定义均未运行；平台014/015及端侧schema-002仅为SQL源码，未迁移。', '',
        '## 全部56任务：完整未收口清单', '', '| 编号 | 任务 | 当前状态 | 全部仍待收口子项 |', '|---|---|---|---|']
    current = {t['id']: t for t in status['tasks']}
    for task in plan['tasks']:
        progress = current[task['id']]
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
    print('Native rental SOURCE_ONLY inventory written:22 IMP entries,53 rental test definitions; all56/222 open; no build/test/migration.')


if __name__ == '__main__':
    main()
