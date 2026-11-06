"""IC source/document authoring only; no compiler/test/validator/runtime/migration."""
from collections import Counter
from copy import deepcopy
import json
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[2]
ANNEX = 'docs/技术开发/附录-原密文托管与不可变补传源码.md'
TOOL = 'dev/tools/整理R026原密文托管源码.py'
RULES = 'dev/开发轮次执行规则.md'
CONTRACT = 'docs/技术开发/contracts/original-cipher-custody-source-v1.json'
REFS = [ANNEX, TOOL, RULES, CONTRACT,
        'dev/src/client/crypto-gateway/README.md',
        'dev/src/client/crypto-gateway/custody.go',
        'dev/src/client/crypto-gateway/custody_native_test.go',
        'dev/src/client/crypto-gateway/endpoint/vault.go',
        'dev/src/client/crypto-gateway/endpoint/custody_scope_test.go',
        'dev/src/client/node/store.go', 'dev/src/client/node/cipher_custody.go',
        'dev/src/client/node/cipher_custody_test.go',
        'dev/src/client/cmd/p11-node/main.go', 'dev/src/client/cmd/p11-node/cipher_custody.go',
        'dev/src/client/cmd/p11-node/cipher_custody_test.go']
REFS += [p.relative_to(ROOT).as_posix() for p in sorted((ROOT/'dev/src/client/crypto-gateway/ciphercustody').rglob('*')) if p.is_file()]
REFS = sorted(set(REFS))
TESTS = [name for name in REFS if name.endswith('_test.go')]
TASKS = {'DEV-29', 'DEV-30', 'DEV-31'}
STEPS = {'DEV-29.01', 'DEV-29.04', 'DEV-30.01', 'DEV-30.02', 'DEV-30.03',
         'DEV-30.04', 'DEV-31.01', 'DEV-31.02', 'DEV-31.03'}
DETAIL = ('IC补入原OS根/单写者锁下的AEAD原密文托管、先持久WRITING及完整配额再生产、'
          'READY只回原清单/中断不重加密、同原签名对象完整无钥检查及原字节范围补传、'
          '无私钥副本的CIPHER_CUSTODY用途/导入及补传撤销、串行过期清理/双数据槽满时仍可取消和实际join、'
          '父节点先同时取消密钥与文件owner再等待、四原生组合API和三元数据CLI源码。'
          '本地密文托管不是LOCAL_SEGMENTED_1、私有FINAL/INPUT预检、业务执行权、签收或可信usage。'
          '平台E2E授权/无钥存储与真实网络恢复、SDK/Host私有执行/Android JNI/媒体、全局4MiB背压重排、'
          '独立计量/真实资格及未知替换残留显式恢复仍待。'
          '115控制规划/150既有schema登记/84表/75控制HTTP及3个SDK入口不变，无SQL或迁移。'
          '同一56任务222子项保留，测试源码未编译/运行，完整生产接线与测试源码后集中验证；不是开发轮完成。')
ITEMS = [
    ('129', '原安装AEAD密文托管与完整配额源码', 'IC001—010：原根/锁/master、先anchor、独立包装用途和原描述符/文件绑定；未验证'),
    ('130', '原写入意图与不可变重试恢复源码', 'IC011—020：先持久WRITING再生产、fsync/签名核验后READY、中断保留、不重加密或重执行'),
    ('131', '原密文范围补传与公钥用途源码', 'IC021—028：完整无钥检查/原偏移边界、无私钥副本、导入和补传撤销；不证明私有END或业务授权'),
    ('132', '期限退役与双owner取消等待源码', 'IC029—036：先RETIRING/取消/实际join/删除/RETIRED、保守恢复、双槽与串行control；未验证'),
    ('133', '原生托管组合与三管理入口源码', 'IC037—043：四node组合API、密钥与文件先同时取消、三元数据CLI/严格policy、未知残留失败关闭'),
    ('134', '托管恢复与真实密码组合测试源码', 'IC044—047：结构假密文/原生CGO分开、提交未知/断流/范围/撤销/真实join等定义；未编译/运行'),
    ('135', 'IC技术约束与双语完整待办登记', 'IC048—050：独立源码契约/双语README/更新说明/全56及222子项保留；不是整轮完成')]
REQUIREMENTS = [
    ('只存原密文', '文件仅保存原BINARY_STREAM_1，不存prompt/生成明文/内容钥或公开明文摘要'),
    ('原单一owner', '复用node私有root与OS单写者锁，不另建根或第二写者'),
    ('原钥前置', '先有原endpoint anchor，再初始化cipher custody'),
    ('先存anchor', '随机custody anchor先保存，丢日志不重建空库'),
    ('独立包装用途', 'HMAC原master/独立label/原installation与双anchor binding'),
    ('AEAD元数据', 'AES-GCM/AAD/随机nonce/私有临时文件/flush/原子替换/目录同步'),
    ('原描述符', '原合同/双方/环境/policy/方向/用途/stream/epoch/期限/钥及public model'),
    ('独立Expected', '公钥来自独立核验bundle，不接管来包自带根'),
    ('对象文件一一绑定', 'custody id与原随机平面文件名同32位hex，拒绝路径与别名'),
    ('完整预留', '生产前保存完整wire预算；中断对象仍保留计数和配额'),
    ('先WRITING', '任何producer前落盘原意图，提交未知不能继续调用'),
    ('首次生产一次', '同逻辑object只能一个原描述符，不用第二次调用重加密'),
    ('有界写入', '64KiB分写/上下文/长度限额/错误后sink关闭'),
    ('fsync后检查', '完整生产后同步/关闭/重开原文件再核同一签名清单'),
    ('READY后回复', '先持久原manifest/bytes/READY，之后才返回handle'),
    ('READY重放', '返回原manifest与handle，不读取source或再次执行producer'),
    ('崩溃中断', '重开WRITING转INTERRUPTED，原文件大小恢复且不发布'),
    ('状态不猜测', '不序列化或恢复secretstream push，不自动新epoch/重跑模型'),
    ('原导入', 'INBOUND绑定已知原manifest hash，保持原密文字节'),
    ('清单不可换', '原expiry/manifesthash/descriptor不一致不能重试或覆盖'),
    ('先验全部密文', '任何范围输出前核完整原签名/结构/密文hash/长度'),
    ('仅原字节范围', 'offset/length受原完成字节约束，不对secretstream随机解密'),
    ('失败补原字节', '目的端失败保留READY，重开后复制同一对象，不重加密'),
    ('公钥用途', 'CIPHER_CUSTODY只复制public bundle，不复制任何私钥'),
    ('撤销导入/补传', '原本机身份/peer pin撤销取消相关用途，文件取消仍真实join'),
    ('业务授权独立', '网络调用者仍核原当前grant及原offset，库不签发执行或付款权'),
    ('私有输入屏障', 'ConsumeStored仍要求完整私有FINAL/manifest/EOF与共享Preflight'),
    ('不冒充签收', 'opaque READY/发送成功/认证prefix均不是买家接受或可信usage'),
    ('实际到期管理', 'CLI只用实际UTC，拒绝用户ACK或用户指定过期时间'),
    ('先退役后删除', '先存RETIRING，再取消/等待真实owner，后删原文件'),
    ('退役未知保守', '删除或同步未知保留配额，按原RETIRING幂等继续'),
    ('过期墓碑压缩', '原期限过后才压缩RETIRED，不以旧描述符复活'),
    ('双数据槽', '最多2个有界payload owner，第三个payload拒绝'),
    ('串行保留控制槽', '1个维护owner能在数据满槽时取消两者，第二次清理pending'),
    ('父层同时取消', 'key vault与spool先同时BeginClose，再join避免嵌套等待'),
    ('真实退出后清钥', 'context取消/kill/超时不替代实际退出；父root/master/锁最后释放'),
    ('四原生组合', 'SealCipher/ImportCipher/ConsumeCipher/CopyCipherRange实际接线'),
    ('三元数据CLI', 'cipher-init/status/expire接入原p11-node，不开内容listener'),
    ('严格策略', '先解析policy再打开状态；16KiB严格JSON且固定诊断不回显'),
    ('明确容量', '66—512MiB/1—64对象/2MiB日志/64KiB复制均为D预算'),
    ('未知残留保留', '未知cipher对象或cipher-next残留拒绝自动接管删除；显式恢复工具仍待'),
    ('不自动执行', '打开元数据owner不启动模型/GPU/设备或赋予内核批准'),
    ('模式边界', '不把原明文SDK标为E2E，不开放增强保密，不更换卖家/隐私档'),
    ('结构夹具边界', 'opaque测试故意无有效secretstream，只测试托管而非私有END'),
    ('原生组合源码', '另有真实CGO端点/secretstream/预检组合测试定义，尚未运行'),
    ('恢复测试源码', '提交未知/部分写入/重开/输送失败/跨scope/篡改等定义未运行'),
    ('并发测试源码', '满槽清理/撤销/父子关闭真实join定义未编译/运行'),
    ('独立机器说明', '新增IC源码契约，不改原HTTP/schema/数据库登记'),
    ('全量缺口保留', '平台盲转/SDK/Host/JNI/LOCAL_SEGMENTED_1/媒体/4MiB/签收usage仍待'),
    ('同轮完整编码', '双语说明及全部56/222保留，完成全部生产/依赖/测试源码后统一验证')]


def load(name):
    return json.loads((ROOT/name).read_text(encoding='utf-8'))


def write(name, body):
    p = ROOT/name
    ending = '\r\n' if p.exists() and b'\r\n' in p.read_bytes() else '\n'
    p.parent.mkdir(parents=True, exist_ok=True)
    p.write_bytes(body.replace('\r\n', '\n').replace('\n', ending).encode('utf-8'))


def dump(name, value):
    write(name, json.dumps(value, ensure_ascii=False, indent=2)+'\n')


def section(name, marker, body):
    p = ROOT/name
    value = p.read_text(encoding='utf-8') if p.exists() else ''
    if marker in value:
        start = value.index(marker)
        end = value.find('\n## ', start+len(marker))
        value = value[:start].rstrip()+('\n\n'+value[end+1:] if end >= 0 else '')
    write(name, value.rstrip()+'\n\n'+marker+'\n\n'+body.strip()+'\n')


def inventory():
    definitions = []
    for source in TESTS:
        raw = (ROOT/source).read_text(encoding='utf-8')
        for name in re.findall(r'^func (Test\w+)\(', raw, re.M):
            definitions.append({'id': f'IC-T{len(definitions)+1:03d}', 'name': name, 'source': source,
                                'native_build_tag': raw.startswith('//go:build p11_sodium &&'),
                                'status': 'SOURCE_ONLY_NOT_EXECUTED'})
    return definitions


def source_contract(tests):
    dump(CONTRACT, {
        'profile_id': 'P11_ORIGINAL_CIPHER_CUSTODY_SOURCE_1', 'requirements': 'IC-001—050',
        'authority': ANNEX, 'status': 'SOURCE_ONLY_UNVERIFIED', 'compiled_or_executed': False,
        'activation': False, 'development_policy': RULES, 'source_refs': REFS,
        'custody_profile': 'P11_ORIGINAL_CIPHER_CUSTODY_1', 'wire_profile': 'BINARY_STREAM_1',
        'local_segmented_1_implemented': False, 'global_4mib_flow_and_reorder_complete': False,
        'ownership': {'original_os_root_and_single_writer': True, 'original_endpoint_anchor_required': True,
                      'custody_anchor_before_journal': True, 'missing_anchored_journal_reinitialized': False,
                      'metadata_aead': 'AES_256_GCM', 'wrap_key_purpose': 'P11_CIPHER_CUSTODY_WRAP_KEY_1',
                      'public_only_key_use': 'CIPHER_CUSTODY', 'unknown_files_or_replacement_retained_and_closed': True,
                      'explicit_unknown_replacement_recovery_tool_complete': False},
        'lifecycle': ['WRITING', 'INTERRUPTED', 'READY', 'RETIRING', 'RETIRED'],
        'immutable_original': {'wire_bound_reserved_before_producer': True, 'intent_committed_before_producer': True,
                               'ready_before_reply': True, 'ready_retry_reads_source_or_reencrypts': False,
                               'interrupted_original_restarts_producer': False, 'push_state_restored': False,
                               'model_reexecution_or_new_epoch_automatically_authorized': False,
                               'range_authenticates_complete_opaque_before_copy': True,
                               'range_randomly_decrypts_secretstream': False, 'failed_transport_retains_ready': True},
        'authority_boundaries': {'opaque_ready_proves_private_end_aead_preflight': False,
                                 'input_consumption_requires_complete_private_barrier': True,
                                 'opaque_ready_is_execution_meter_acceptance_payment': False,
                                 'business_network_grant_and_offset_independently_required': True,
                                 'enhanced_confidential_qualified': False,
                                 'full_platform_sdk_host_android_e2e_complete': False},
        'retirement': {'journal_retiring_before_cancel_join_delete': True, 'actual_owner_join_before_key_root_release': True,
                       'cancel_vault_and_spool_before_joining_either': True,
                       'caller_ack_string_can_delete': False, 'expired_tombstone_compaction': True,
                       'single_reserved_cleanup_owner_works_at_payload_capacity': True},
        'budgets_not_measurements': {'capacity_min_bytes': 66 << 20, 'capacity_max_bytes': 512 << 20,
                                     'objects': 64, 'metadata_journal_bytes': 2 << 20, 'copy_buffer_bytes': 65536,
                                     'payload_owners': 2, 'reserved_serial_cleanup_owners': 1,
                                     'cleanup_cancel_seconds_actual_join_still_required': 60},
        'native_apis_source_wired': ['SealCipher', 'ImportCipher', 'ConsumeCipher', 'CopyCipherRange'],
        'metadata_cli_source_wired': ['cipher-init', 'cipher-status', 'cipher-expire'],
        'content_cli_or_listener_opened': False, 'test_definitions': tests, 'test_definition_count': len(tests),
        'test_fixture_boundary': 'Opaque structural fixture is NOT native AEAD; four native-tag definitions are separate and unexecuted.',
        'control_http_operations': 75, 'planned_control_operations': 115, 'registered_control_schemas': 150,
        'platform_tables': 84, 'native_sdk_data_plane_operations': 3, 'sql_changed_or_migrated': False,
        'full_scope_tasks': 56, 'full_scope_steps': 222, 'full_development_round_complete': False,
        'test_admission': 'DENIED_UNTIL_ALL_IN_SCOPE_CODE_COMPLETE'})
    name = 'scripts/技术开发数据.py'
    body = (ROOT/name).read_text(encoding='utf-8')
    if "'original_cipher_custody_source':" not in body:
        addition = ("      'original_cipher_custody_source':{'status':'SOURCE_ONLY_UNVERIFIED',\n"
                    f"        'annex':'{ANNEX}','source_contract':'{CONTRACT}',\n"
                    "        'original_os_root_cipher_custody_and_four_native_apis_source_wired':True,\n"
                    "        'platform_e2e_relay_sdk_host_jni_and_local_segmented_complete':False,\n"
                    "        'opaque_ready_is_private_end_or_execution_or_acceptance':False,\n"
                    "        'sql_changed_or_migrated':False,'compiled_or_executed':False},\n")
        write(name, body.replace("      'unproven_capabilities':", addition+"      'unproven_capabilities':", 1))


def progress(plan, stage, status, tests):
    lines = ['# R026 全量代码阶段进展', '',
             '**进行中，SOURCE_ONLY / 未验证。** 同一56/222全部生产代码、实际接线及测试源码齐备后集中验证。129—135仅源码保存，不是整轮完成。',
             '', '| 编号 | 已写内容 | 实际状态 |', '|---|---|---|']
    lines += [f"| {i['id']} | {i['title']} | {i['status']} |" for i in stage['implemented_items']]
    lines += ['', DETAIL, '', f'本次 {len(tests)} 项 IC 新测试定义均未编译/运行；含4项独立native build-tag组合定义。',
              '', '无SQL变更；001—021平台双份SQL/source及已登记端侧DDL/source、固定JCS/许可证原字节冻结。',
              '', '实施状态：'+'；'.join(f'{key} {count}' for key, count in sorted(Counter(t['implementation_status'] for t in status['tasks']).items()))+'；全部222子项未验收。',
              '', '| 编号 | 全部任务 | 当前状态 | 全部仍待收口子项 |', '|---|---|---|---|']
    current = {t['id']: t for t in status['tasks']}
    for task in plan['tasks']:
        t = current[task['id']]
        lines.append(f"| {task['id']} | {task['title']} | {t['implementation_status']} | {'、'.join(t['remaining_step_ids'])} |")
    write('dev/progress/R026-全量代码阶段进展.md', '\n'.join(lines)+'\n')
    lines = ['# 全量代码优先实施清单', '', DETAIL,
             '', '执行规则：[CF-01—05](开发轮次执行规则.md)。同一R026不拆轮，所有未收口子项完整保留。',
             '', '| 子项 | 任务 | 未收口需求 | 代码状态 | 验收 |', '|---|---|---|---|---|']
    for task in plan['tasks']:
        for step in task['steps']:
            lines.append(f"| {step['id']} | {task['title']} | {step['code_requirement']} | {step['code_status']} | {step['acceptance_status']} |")
    write('dev/全量代码优先实施清单.md', '\n'.join(lines)+'\n')


def documentation(tests):
    marker = '## R026 IC-001—050 原密文托管与不可变补传 / Original cipher custody and retry source'
    zh = (f'R026新增[IC-001—050]({ANNEX})：原OS根/单写者AEAD密文托管、完整预留/WRITING先于加密生产、'
          'READY原清单重放/中断不重加密、原签名对象完整检查/原字节范围补传、无私钥副本公钥用途与撤销、'
          '串行过期清理/满双槽仍可取消/实际join、父节点双owner先同时取消、四原生组合API/三元数据CLI源码。'
          f'{len(tests)}项新测试定义未编译/运行，含4项独立native-tag组合。'
          '本地托管不是LOCAL_SEGMENTED_1、私有END/执行/签收/可信usage；平台E2E原授权与无钥持久化/真实网络恢复、'
          'SDK/Host私有执行/Android JNI/媒体/全局4MiB背压重排/独立计量与真实资格、未知替换残留显式恢复仍待。'
          '115控制规划/150既有schema登记/84表/75控制HTTP及3个SDK入口不变，无SQL/迁移。'
          '全56任务222子项保留，先完整生产接线及测试源码再集中验证/重录媒体；本次不是开发轮完成。')
    en = (f'R026 adds [IC-001–050]({ANNEX}): original OS-root/single-writer AEAD ciphertext custody, full reservation/durable WRITING before encryption, '
          'original READY-manifest replay/no re-encryption after interruption, complete signed opaque checks/exact-byte ranges, public-only key custody/revocation, '
          'serial expiry/full-slot cancellation/actual join, both parent owners canceled before either join, four native APIs and three metadata CLI operations. '
          f'{len(tests)} new test definitions are uncompiled/unexecuted, including four separate native-tag compositions. '
          'This is not LOCAL_SEGMENTED_1, private END, execution, acceptance or trusted usage. Platform E2E grants/blind persistence/actual network recovery, '
          'local SDK/private Host/Android JNI/media/global 4 MiB flow and reorder/independent meters/real qualification and explicit unknown-replacement recovery remain open. '
          '115 planned control operations/150 existing registered schemas/84 tables/75 control HTTP and three SDK data-plane registrations stay unchanged; no SQL/migration. '
          'All 56 tasks/222 steps remain in the same CODE_FIRST round; complete production wiring/test source precede batch validation and new media. This is not a completed round.')
    section(ANNEX, '## IC 编号需求约束索引', '| 编号 | 项目 | 源码约束 |\n|---|---|---|\n'+
            '\n'.join(f'| IC-{i:03d} | {title} | {detail} |' for i, (title, detail) in enumerate(REQUIREMENTS, 1)))
    section(ANNEX, '## IC 测试源码定义索引', '| 编号 | 定义 | 源码 | Native tag | 状态 |\n|---|---|---|---|---|\n'+
            '\n'.join(f"| {t['id']} | {t['name']} | {t['source']} | {t['native_build_tag']} | 未编译/未运行 |" for t in tests))
    for name, content in [('dev/repository/README.zh-CN.md.in', zh), ('dev/repository/README.en.md.in', en)]:
        section(name, marker, content)
        body = (ROOT/name).read_text(encoding='utf-8')
        body = re.sub(r'\n<!-- R026-[A-Z-]+-CURRENT -->.*?<!-- /R026-[A-Z-]+-CURRENT -->\n', '\n', body, flags=re.S)
        body = body.replace('以上 KE 说明', '以上 IC 说明').replace('The KE notice above', 'The IC notice above')
        title, rest = re.sub(r'\n{3,}', '\n\n', body).split('\n', 1)
        write(name, title+'\n\n<!-- R026-IC-CURRENT -->\n'+content+'\n<!-- /R026-IC-CURRENT -->\n'+rest)
    for name, content in [('CHANGELOG.zh-CN.md', zh), ('CHANGELOG.md', en)]:
        body = (ROOT/name).read_text(encoding='utf-8')
        body = re.sub(r'(?m)^- R026(?:新增| adds )\[IC-001[—–]050\].*\n', '', body)
        title, rest = body.split('\n', 1)
        write(name, title+'\n\n- '+content+'\n'+rest)
    for name in ['dev/repository/USAGE.md', 'dev/repository/SYSTEM_DESIGN.md']:
        section(name, marker, zh.replace('(docs/', '(../../docs/')+'\n\n'+en.replace('(docs/', '(../../docs/'))
    section('dev/progress/README.md', marker, zh.replace('(docs/', '(../../docs/'))
    for name in ['dev/src/client/node/README.md', 'dev/src/client/README.md', 'dev/src/client/crypto-gateway/README.md']:
        prefix = '(../../../docs/' if name == 'dev/src/client/README.md' else '(../../../../docs/'
        section(name, marker, zh.replace('(docs/', prefix)+'\n\n'+en.replace('(docs/', prefix))
    # Replace only the current remaining-work paragraph, keeping historical KE
    # records intact and clearly stating that network recovery is still open.
    name = 'dev/src/client/crypto-gateway/README.md'
    body = (ROOT/name).read_text(encoding='utf-8')
    body = body.replace('Still open: platform E2E authorization and blind relay storage, immutable ciphertext recovery, local SDK listener,',
                        'Local immutable ciphertext custody/retry now has unverified IC source. Still open: platform E2E authorization and blind relay storage, actual network recovery, local SDK listener,')
    write(name, body)
    for name in ['docs/技术开发/00-技术文档导航与实施基线.md', 'docs/技术开发/08-端到端加密协议与密钥实现设计.md']:
        section(name, marker, zh.replace('(docs/技术开发/', '('))
    for name in ['AGENTS.md', 'dev/AGENTS.md']:
        section(name, marker, zh.replace('(docs/', '(../docs/' if name.startswith('dev/') else '(docs/')+
                '\n\n最新唯一增量整理器为'+TOOL+'；禁止重跑旧KE/PD/SI/DC/DA/RA/RT/RF/CK/GF/PH/PT/MP/MG/MS/WS/CH/CE/MW/RN整理器覆盖当前IC状态。'
                '遵守'+RULES+'：同一R026全56/222生产接线及测试源码齐备前，不编译/测试/核验/迁移或启动DB、密码库、内核、模型、GPU、手机、APK或浏览器；源码保存不拆轮。'
                '001—021平台双份SQL/source、已登记端侧DDL/source、固定第三方JCS源和许可证原字节冻结，平台只追加022+。'
                '不调度Docker/WSL/QEMU/VM/模拟器或子代理；DEV55延后、DEV32无真实证明关闭；浏览器用后关闭不用页。')


def main():
    tests = inventory()
    source_contract(tests)
    plan, stage, status = [load(n) for n in ['dev/progress/code-first-plan.json', 'dev/progress/R026-code-stage.json', 'dev/progress/implementation-status.json']]
    integration = {'status': 'SOURCE_ONLY_UNVERIFIED', 'requirements': 'IC001—050', 'annex': ANNEX,
                   'source_contract': CONTRACT, 'original_cipher_custody_and_native_apis_source_wired': True,
                   'metadata_cli_source_wired': True, 'local_segmented_and_global_4mib_complete': False,
                   'platform_e2e_relay_sdk_host_jni_complete': False, 'compiled_or_executed': False,
                   'sql_changed_or_migrated': False, 'new_media_captured': False,
                   'full_development_round_complete': False, 'test_definition_count': len(tests),
                   'control_http_operations': 75, 'native_sdk_data_plane_operations': 3,
                   'planned_control_operations': 115, 'registered_control_schemas': 150, 'platform_tables': 84,
                   'remaining': ['平台E2E原上下文/授权/无钥持久化/实际网络补传/背压及有界重排',
                                 '本机SDK/实际Host私有执行/LOCAL_SEGMENTED_1/Android JNI/媒体/全局4MiB门闩',
                                 '原买家receipt/可信usage/资金事务、独立信任UI与真实资格',
                                 '未知替换残留显式恢复工具；固定库构建/向量/LP/PE/性能实测',
                                 '完整56/222其他生产接线及测试源码后集中验证；DEV55延后/DEV32无证明关闭']}
    for record in (plan, stage):
        record['original_cipher_custody_source_integration'] = deepcopy(integration)
    for task in plan['tasks']:
        if task['id'] in TASKS:
            task['code_status'] = 'IN_PROGRESS'
            task['original_cipher_custody_source_limit'] = DETAIL
            for step in task['steps']:
                if step['id'] in STEPS:
                    step['code_status'] = 'IN_PROGRESS'
                    step['implementation_refs'] = sorted(set(step.get('implementation_refs', [])+REFS))
    for task in status['tasks']:
        if task['id'] in TASKS:
            task['implementation_status'] = 'IN_PROGRESS'
            task['original_cipher_custody_source_limit'] = DETAIL
            task['current_source_refs'] = sorted(set(task.get('current_source_refs', [])+REFS))
            task['current_validation_status'] = 'NOT_EXECUTED'
    status['activity_source'] = '2026-11-07 CODE_FIRST：IC001—050 / IMP129—135；'+DETAIL
    for number, title, state in ITEMS:
        entry = {'id': 'IMP-R026-'+number, 'title': title, 'status': state}
        stage['implemented_items'] = [old for old in stage['implemented_items'] if old['id'] != entry['id']]+[entry]
    stage.update({'complete': False, 'validation_started_after_user_policy': False, 'tasks': deepcopy(status['tasks']),
                  'original_cipher_custody_test_definitions': {'tests': tests, 'ids': [t['id'] for t in tests], 'status': 'SOURCE_ONLY_NOT_EXECUTED'}})
    stage['source_files'] = sorted(set(stage.get('source_files', [])+REFS))
    for name, value in [('dev/progress/code-first-plan.json', plan), ('dev/progress/R026-code-stage.json', stage), ('dev/progress/implementation-status.json', status)]:
        dump(name, value)
    progress(plan, stage, status, tests)
    documentation(tests)
    print(json.dumps({'source_document_generation': 'IC001—050 / IMP129—135', 'unexecuted_test_definitions': len(tests),
                      'compiler_test_validator_migration_runtime': 'NOT_RUN',
                      'all_tasks_open': len(plan['tasks']), 'all_steps_open': sum(len(t['steps']) for t in plan['tasks'])}, ensure_ascii=False))


if __name__ == '__main__':
    main()
