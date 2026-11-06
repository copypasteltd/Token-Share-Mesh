"""Document-only rental native SOURCE_ONLY inventory. Never build or run tests."""
from __future__ import annotations

import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
ANNEX = 'docs/技术开发/附录-小时租赁授权与累计计量源码细则.md'
REFS = [
    'dev/src/client/protocol/rentalaccess/text.go',
    'dev/src/client/protocol/rentalaccess/text_test.go',
    'dev/src/client/adapters/llamaworker/adapter.go',
    'dev/src/client/adapters/llamaworker/input.go',
    'dev/src/client/adapters/llamaworker/rental.go',
    'dev/src/client/adapters/llamaworker/rental_test.go',
    'dev/src/client/rentalruntime/types.go',
    'dev/src/client/rentalruntime/runtime.go',
    'dev/src/client/rentalruntime/runtime_test.go',
    'dev/src/client/rentalruntime/README.md',
    'dev/src/client/node/rental_native.go',
    'dev/src/client/node/rental_native_test.go',
    'dev/src/client/node/rental_session.go',
    'dev/src/client/node/store.go',
    'dev/src/client/execution/rental.go',
    'dev/src/client/internal/rentaltest/fixture.go', ANNEX,
]
ITEMS = [
    ('023', '原租赁文本认证、本机加密输入与三协议严格预检',
     '独立原grant摘要/固定pin/完整原会话与本机策略，AES-256-GCM仅驻内存封装、随机密文摘要和一次解封；只限STANDARD，不是网络E2E'),
    ('024', '独立租赁LLM适配与原生私有内容/取消管道',
     '明确原包/GPU/模型，无假T1/attempt；原调用提交/查询/取消/清理，提前取消墓碑、真实同步回执及进程/管道join源码；尚未编译/设备验证'),
    ('025', '独立本机截止循环、租内监督器与节点有主关闭',
     '本机计时取消不等慢SQL/页面，当前授权重核、一次提交、串行和有界输出；超时/未知保留owner，持有OS锁/密钥直到实际join；平台释放未接'),
    ('026', 'RN-091—110、14项新增测试源码及完整文档同步',
     '共67项租赁测试定义均未运行；48控制HTTP/9租赁、98规划操作/71平台表不变；56任务/222子项全量代码与验收仍开放'),
]
DETAIL = ('租赁STANDARD文本已有独立原grant认证、本机随机AEAD一次输入、无假T1的真实llama私有管道适配、'
          '不等SQL的本机截止监督、原子调用停止清理与节点所有权/join源码；未编译/运行或物理资格验收。'
          '公网/SDK监听与输出交付、平台独立清理确认/预算释放、可信本人批准UI/ready生产、真实收单/付费续租、'
          '确认计量、Android会话/M1、E2E/LP及全56/222其余代码仍待，全量代码齐备后再统一测试。')
REMAINING = [
    '完整可信本人批准UI/配置与独立探测/ready证据生产，不能从网络任务自动批准',
    '公开SDK/模型监听、完整协议输出交付/ACK、内容E2E/分块素材与持久加密结果恢复',
    'Android原session授权/共手机执行槽/租内媒体执行与真实清理回执',
    '真实支付宝收单验签/原号恢复、追加支付/续租附约及追加容量原子落库',
    '完整区间/故障独立确认与不可变计量修订、真实财务资格',
    '平台独立清理回执及同原owner/预算原子释放，后续数据库变更只加前向版本',
    '独立防回滚水位、安全容量回收、原grant终止证明；重开不能恢复ALLOW',
    '独立低权限服务/隐私档完整接线、安装器及实际性能；不可把本机AEAD称为E2E/TEE',
    '全56任务/222子项其余生产代码及测试源码后统一编译/测试/调试',
]
TESTS = [
    ('054', 'TestRentalTextEnvelopeAuthenticatesAllThreeOriginalProtocols', '三协议/原字节不改、随机密文摘要、格式化遮蔽与一次解封'),
    ('055', 'TestRentalTextEnvelopeRejectsSelfApprovedAuthorityAndUnsupportedSemantics', '独立原摘要/卖家/模型/协议、scope、超限和非规范围栏拒绝'),
    ('056', 'TestRentalTextEnvelopeCloseInvalidatesCopiesAndExpiredGrant', '浅拷贝同钥所有权、过期/取消解封拒绝，保留安全查询元数据'),
    ('057', 'TestNativeRentalTypedInputAndSubmitContextDoNotBecomeT1', '合成私有管道、原租内ID/正文、短提交context不误停完整调用、重复拒绝'),
    ('058', 'TestNativeRentalCancelBeforeSubmitCreatesPermanentOriginalTombstone', '提交前取消阻后续进入、原权威ABSENT与跨代次拒绝'),
    ('059', 'TestNativeRentalCancelUsesSeparatePipeAndExitIsNotCleanup', '内容管道阻塞不挡取消；进程退出UNKNOWN不可清理，真实所有权join'),
    ('060', 'TestRentalRuntimeNoInitialAllowNeverEntersBackend', '准备/绑定grant无首次ALLOW不入内核'),
    ('061', 'TestRentalRuntimeChildCleanupRetainsParentAndOriginalQuery', '串行/原查询、子清理保留父槽，下一调用不换原会话'),
    ('062', 'TestRentalRuntimeDisconnectedReaderStopsOnlyOriginalChild', '已断连接也取消原调用、不释放父物理占位'),
    ('063', 'TestRentalRuntimeUnknownStopRetainsOriginalPhysicalOwner', '停工未知和关闭到期不释放原owner'),
    ('064', 'TestRentalRuntimeCloseWaitsForOwnedJoinBeforeRelease', '进程/管道未join不报Done、不释放物理槽'),
    ('065', 'TestRentalRuntimeLocalDeadlineCancelsWhileMetadataCheckIsStuck', '合成卡住元数据检查时独立截止循环仍发原取消；不是GPU时延基准'),
    ('066', 'TestNodeRentalDiscardInputClearsOnlyItsOwnEnvelopeAndCapacity', '丢弃本人未提交封装清钥并返名额，不销毁别的owner输入'),
    ('067', 'TestNodeRentalTimedCloseRetainsOSLockAndMasterUntilActualJoin', '关闭超时保持原OS锁/密钥，实际join后再完成主资源关闭'),
]


def load(name):
    return json.loads((ROOT / name).read_text(encoding='utf-8'))


def write(name, value):
    path = ROOT / name
    before = path.read_bytes() if path.exists() else b''
    ending = '\r\n' if b'\r\n' in before else '\n'
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(value.replace('\r\n', '\n').replace('\n', ending).encode('utf-8'))


def dump(name, value):
    write(name, json.dumps(value, ensure_ascii=False, indent=2) + '\n')


def append_section(name, marker, content):
    text = (ROOT / name).read_text(encoding='utf-8')
    if marker in text:
        text = text[:text.index(marker)].rstrip()
    write(name, text.rstrip() + '\n\n' + marker + '\n\n' + content.strip() + '\n')


def repository_notes():
    zh = ('R026继续补齐[小时租赁源码 RN-001—110](docs/技术开发/附录-小时租赁授权与累计计量源码细则.md)：'
          '原本机批准/加密journal/共享物理门闩/串行inbox之外，新增独立原grant认证、三标准文本协议预检、'
          '仅驻内存的随机AES-GCM一次输入、租赁专用llama私有管道适配、不等SQL的本机到期循环与节点有主关闭。'
          '不假造T1 job/attempt，不换卖家/设备/模型/隐私档；真实原同步/清理回执及进程/管道join后才可能释放本机槽。'
          '参见[租赁执行模块](dev/src/client/rentalruntime/README.md)。')
    zh_limit = ('**SOURCE_ONLY / 未验证。** 共67项租赁测试定义均未运行，没有编译/迁移/设备验证。'
                '本机AEAD仅限GPU_RENTAL/STANDARD文本，不是网络E2E或设备所有者不可见；停止超时/回执未知保留原owner，'
                '父节点未实际join时保留OS锁/主密钥。公开SDK/模型入口、完整本人批准UI/独立ready、真实收单/付费续租、'
                '确认计量、Android租内媒体、E2E/LP及平台独立清理回执/原owner和预算释放仍待。'
                '48控制HTTP/9租赁、98规划操作/71平台表不变，已发布DDL原字节保留。全56任务222子项未收口，全部代码齐备后统一测试。')
    en = ('R026 extends [hourly rental source RN-001—110](docs/技术开发/附录-小时租赁授权与累计计量源码细则.md) '
          'with independently authenticated original grants, strict Chat/Responses/Messages text input, one-use random RAM AEAD envelopes, '
          'a rental-only llama private-pipe adapter, a SQL-independent monotonic deadline loop and owned node shutdown. '
          'No fabricated T1 job/attempt or seller/device/model/privacy fallback is used. Original synchronized cleanup receipts and actual '
          'process/pipe joins precede any local slot release. See the [rental runtime](dev/src/client/rentalruntime/README.md).')
    en_limit = ('**SOURCE_ONLY / UNVERIFIED.** All 67 rental test definitions remain unexecuted; no compilation, migration, model/GPU/phone run occurred. '
                'This RAM AEAD covers GPU_RENTAL/STANDARD text and provides neither network E2E nor provider blindness. '
                'Unknown stop/cleanup retains the original owner; unjoined shutdown retains the node lock/master key. Public SDK/model listeners, '
                'owner approval UI/independent readiness, real acquiring/paid renewal, confirmed metering, Android rental media, E2E/LP and platform '
                'cleanup/budget release remain open. Counts stay at 48 control/nine rental HTTP handlers, 98 planned operations and 71 platform tables. '
                'Published DDL bytes are unchanged. All 56 tasks/222 steps remain open until full-scope code precedes batch validation.')
    for name, paragraphs in [
        ('dev/repository/README.zh-CN.md.in', [zh, zh_limit]),
        ('dev/repository/README.en.md.in', [en, en_limit]),
        ('dev/repository/USAGE.md', [zh.replace('(docs/', '(../../docs/').replace('(dev/src/', '(../src/'),
                                      en_limit + ' Internal use: prepare the original physical hold; explicitly OpenNative during PREPARING; attach independently pinned current control; SealTextInput then Invoke. Discard unused envelopes. Query/Cancel/Output reauthenticate current credentials; close and clear owned outputs. These Go methods do not open a public listener.']),
        ('dev/repository/SYSTEM_DESIGN.md', [zh.replace('(docs/', '(../../docs/').replace('(dev/src/', '(../src/'), en_limit]),
    ]:
        parts = (ROOT / name).read_text(encoding='utf-8').split('\n\n')
        parts[1:3] = paragraphs
        write(name, '\n\n'.join(parts))
    module_note = '''RN-091—110追加租赁专用文本接线源码：独立原grant/当前物理门闩、本人TextPolicy与三协议预检，随机AES-256-GCM仅RAM一次输入；不构造T1 job/attempt。独立本机截止循环不等SQL即发原取消，真实同步清理回执及进程/管道join后才可能释放本机槽；未知或超时保留owner，父节点未join则保留OS锁/密钥。输入/输出调用者仍负责本人持有的明文切片。

当前只限GPU_RENTAL/STANDARD文本。没有网络E2E、提供方不可见、TEE或实际性能结论；平台独立清理回执/资金释放、Android租内媒体、公开SDK/模型入口与全56/222其余代码仍待。累计67项租赁测试定义均未编译/运行。本段更新早期源码检查点的“租赁内核/watchdog仍待”状态，不追认历史验收。

Source only, unverified. Typed original rental input and private native pipes reuse encoding, not T1 authority. A separate monotonic deadline loop cancels without SQL; original cleanup facts plus actual joins gate local release. Unknown outcomes retain ownership. Local RAM encryption is not network E2E, provider blindness or a platform funds-release proof. No builds, tests, migration or device execution occurred.'''
    for name in ['dev/src/client/protocol/rentalaccess/README.md',
                 'dev/src/client/adapters/llamaworker/README.md',
                 'dev/src/client/execution/README.md']:
        append_section(name, '## R026 RN-091—110：租赁文本与独立停止源码', module_note)
    for name, note in [
        ('CHANGELOG.zh-CN.md', 'R026新增IMP-R026-023—026：RN-091—110原租赁文本认证/RAM AEAD、三协议预检、无假T1的llama私有管道适配、独立本机截止监督及节点有主关闭；14项新测试源码、累计67项全部未运行。源码检查点不代表本轮完成；全56/222仍待全量代码，随后统一验证。无DDL变更、迁移、模型/设备执行；平台清理/预算释放等仍待。'),
        ('CHANGELOG.md', 'R026 adds IMP-R026-023–026 / RN-091–110: original rental text authentication and RAM AEAD, strict three-profile preflight, a typed llama rental/private-pipe adapter, independent local deadline supervision and owned node shutdown. Fourteen new source test definitions bring the rental total to 67; all remain uncompiled/unexecuted. No DDL/migration/device run or full-round completion. All 56/222 remain open; platform cleanup/budget release and full-scope code still precede batch validation.'),
    ]:
        text = (ROOT / name).read_text(encoding='utf-8')
        if note not in text:
            title, rest = text.split('\n', 1)
            write(name, title + '\n\n- ' + note + '\n' + rest)
    append_section('AGENTS.md', '## R026 RN-091—110 原租赁内核与有主关闭源码继续',
        '当前新增IMP-R026-023—026：原grant认证/本人TextPolicy/RAM AEAD一次输入、独立租赁llama私有管道适配、'
        '不等SQL的本机单调截止循环、串行子调用监督及节点OS锁/主密钥/真实join所有权。只有GPU_RENTAL/STANDARD文本源码，'
        '不是网络E2E/设备所有者不可见/LP/TEE或性能结论；真实物理、财务/ready资格不因内部回执存在而完成。'
        '公开SDK/模型监听、完整本人批准UI、Android租内媒体/M1、真实收单/付费续租/确认计量、平台独立清理回执/owner/预算释放仍待。'
        '67项租赁测试定义只有源码，未编译/测试/迁移/设备运行；完整56/222代码优先门槛不变。'
        '001—015平台双份SQL、原schema.sql/host-schema.sql和已登记schema-002-rental.sql原字节保留，后续只增前向版本。'
        '当前增量整理器是整理R026租赁内核源码.py，禁止重跑旧整理器覆盖RN-110。'
        '禁止Docker/WSL/QEMU等本地虚拟化；DEV-55延后、DEV-32无真实证明关闭，浏览器用后关不用页。')


def main():
    plan = load('dev/progress/code-first-plan.json')
    status = load('dev/progress/implementation-status.json')
    stage = load('dev/progress/R026-code-stage.json')
    integration = plan['rental_source_integration']
    integration.update(
        source_status='STANDARD_TEXT_TYPED_INPUT_NATIVE_RENTAL_WATCHDOG_AND_OWNED_LIFECYCLE_SOURCE_ONLY',
        native_stop_watchdog_wired=True,
        native_stop_watchdog_status='SOURCE_ONLY_INDEPENDENT_MONOTONIC_TIMER_NATIVE_CANCEL_AND_JOIN_UNVERIFIED',
        actual_native_engine_cleanup_producer_wired=True,
        native_engine_cleanup_status='SOURCE_ONLY_OWNED_LLAMA_STANDARD_TEXT_ONLY_NO_PLATFORM_ACCEPTANCE',
        authenticated_rental_text_input_wired=True,
        authenticated_text_input_status='SOURCE_ONLY_LOCAL_RAM_AEAD_NOT_NETWORK_E2E',
        native_rental_node_lifecycle_wired=True,
        platform_cleanup_receipt_and_release_wired=False,
        independent_rollback_watermark_wired=False,
        android_session_executor_wired=False, cleanup_release_wired=False,
        validation_executed=False, remaining=REMAINING)
    related = {'DEV-02', 'DEV-03', 'DEV-07', 'DEV-17', 'DEV-18', 'DEV-20', 'DEV-21',
               'DEV-23', 'DEV-24', 'DEV-33', 'DEV-34', 'DEV-35', 'DEV-36', 'DEV-51',
               'DEV-53', 'DEV-54', 'DEV-56'}
    for task in plan['tasks']:
        if task['id'] in related:
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
    status['activity_source'] = ('2026-11-06 CODE_FIRST：RN-001—110，原租赁文本输入认证/本机AEAD、'
        '独立llama租赁适配/私有管道、不等SQL的截止循环、原子调用清理与节点所有权/join源码；'
        '67项租赁测试定义均未编译/运行。全56/222未收口，R025证据仅是历史身份。')
    plan['rental_source_integration'] = integration
    stage['rental_source_integration'] = integration
    for number, title, state in ITEMS:
        stage['implemented_items'] = [i for i in stage['implemented_items'] if i['id'] != 'IMP-R026-' + number]
        stage['implemented_items'].append({'id': 'IMP-R026-' + number, 'title': title, 'status': state})
    updates = {
        'IMP-R026-007': '领域/原会话预算事务与STANDARD文本租赁内核接线源码已写；付费续租/平台清理释放和其余设备仍待，未验证',
        'IMP-R026-008': '一次JWS、平台持续控制/原摘要及原加密journal/共享门闩已有源码；Android与完整验证仍待',
        'IMP-R026-011': '原订单/资金门槛/全额预算/原物理owner、上下文历史与端侧共门闩已有源码；未迁移/测试',
        'IMP-R026-017': '独立AEAD、先flush序号/实际HTTP、重启无ALLOW及原生租赁停止接线源码已写；Android仍待，未验证',
        'IMP-R026-021': '原ID仅查/后端不换绑、子清理保留父槽及租赁LLM原事实生产源码已写；平台独立清理/预算释放仍待',
    }
    for item in stage['implemented_items']:
        if item['id'] in updates:
            item['status'] = updates[item['id']]
    stage['source_files'] = sorted(set(stage['source_files'] + REFS))
    stage['tasks'] = status['tasks']
    stage['rental_boundary_test_definitions'].update(
        ids=[f'RN-T{n:03d}' for n in range(1, 68)], status='SOURCE_ONLY_NOT_EXECUTED',
        scope='67 rental source test definitions; actual crypto/local SQL plus synthetic backend/private pipes only when future batch testing opens. No compilation/migration/model/GPU/Android/payment/physical acceptance executed.')
    dump('dev/progress/code-first-plan.json', plan)
    dump('dev/progress/implementation-status.json', status)
    dump('dev/progress/R026-code-stage.json', stage)
    lines = ['# R026 全量代码阶段进展', '',
        '**进行中，SOURCE_ONLY / 未验证。** 先补齐全部生产代码、真实依赖接线和测试源码，再集中编译、测试、调试；源码检查点不代表本轮完成。早期条目是当时检查点描述，当前新增推进见023—026。', '',
        '| 编号 | 本阶段已写内容 | 实际状态 |', '|---|---|---|']
    lines += [f"| {i['id']} | {i['title']} | {i['status']} |" for i in stage['implemented_items']]
    lines += ['', f'当前租赁细则：[RN-001—110](../../{ANNEX})；共67项测试源码，全部未运行。无SQL改动/迁移，平台001—015及端侧原DDL/版本2保持原字节。', '',
        '## 全部56任务：完整未收口清单', '', '| 编号 | 任务 | 当前状态 | 全部仍待收口子项 |', '|---|---|---|---|']
    current = {t['id']: t for t in status['tasks']}
    for task in plan['tasks']:
        progress = current[task['id']]
        lines.append(f"| {task['id']} | {task['title']} | {progress['implementation_status']} | {'、'.join(progress['remaining_step_ids'])} |")
    write('dev/progress/R026-全量代码阶段进展.md', '\n'.join(lines) + '\n')
    inventory = ['# 全量代码优先实施清单', '',
        'R026 · CODE_FIRST · 全56任务/222子项；生产代码、真实依赖接线与测试源码完整后统一编译/测试/调试。全部原子待办保持开放；DEV-55延后，DEV-32无真实证明安全关闭。', '',
        '[当前进展](progress/R026-全量代码阶段进展.md) · [机器可读清单](progress/code-first-plan.json)', '']
    for task in plan['tasks']:
        inventory += [f"## {task['id']} {task['title']}", '', task['requirement'], '',
            '| 编号 | 开发项 | 完整代码要求 | 代码状态 | 验收状态 |', '|---|---|---|---|---|']
        for step in task['steps']:
            inventory.append(f"| {step['id']} | {step['title']} | {step['code_requirement'].replace('|', '&#124;')} | {step['code_status']} | {step['acceptance_status']} |")
        inventory.append('')
    write('dev/全量代码优先实施清单.md', '\n'.join(inventory))
    repository_notes()
    print('Document inventory written: IMP023-026; RN001-110; 67 test definitions SOURCE_ONLY; all 56/222 open. No build, validator, migration or test invoked.')


if __name__ == '__main__':
    main()
