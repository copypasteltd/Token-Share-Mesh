"""KE source/document authoring only; never compile/test/check/migrate/run."""
from collections import Counter
from copy import deepcopy
import json
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[2]
ANNEX = 'docs/技术开发/附录-端点密钥与签名二进制密文流源码.md'
TOOL = 'dev/tools/整理R026端点密文流源码.py'
RULES = 'dev/开发轮次执行规则.md'
CONTRACT = 'docs/技术开发/contracts/endpoint-cipher-source-v1.json'
SCHEMA = 'docs/技术开发/contracts/binary-stream-source-v1.schema.json'
DIRECTORIES = ['dev/src/client/crypto-gateway', 'dev/src/client/protocol/cipherstream',
               'dev/src/shared/canonicaljson', 'dev/src/thirdparty/jsoncanonicalizer']
REFS = [ANNEX, TOOL, RULES, CONTRACT, SCHEMA, '.gitattributes', 'dev/tools/准备原生密码依赖.py',
        'dev/src/client/node/store.go', 'dev/src/client/node/crypto_keys.go',
        'dev/src/client/node/crypto_keys_test.go', 'dev/src/client/cmd/p11-node/main.go',
        'dev/src/client/cmd/p11-node/crypto.go', 'dev/src/client/cmd/p11-node/crypto_test.go']
for folder in DIRECTORIES:
    REFS += [p.relative_to(ROOT).as_posix() for p in sorted((ROOT/folder).rglob('*')) if p.is_file()]
REFS = sorted(set(REFS))
TESTS = [path for path in REFS if path.endswith('_test.go')]
TASKS = {'DEV-29', 'DEV-30', 'DEV-31'}
TOUCHED_STEPS = {'DEV-29.01', 'DEV-29.02', 'DEV-29.03', 'DEV-29.04',
                 'DEV-30.01', 'DEV-30.02', 'DEV-30.03', 'DEV-31.01', 'DEV-31.03'}
DETAIL = ('KE补入固定libsodium/JCS原字节依赖、原OS秘密根/单写者AEAD密钥库、分用途钥与八项元数据CLI、'
          '独立首次pin/原轮换回执恢复/撤销及实际join、签名BINARY_STREAM_1与加密END/外层清单/无钥结构检查、'
          '原双方用途组合/完整输入屏障/同库文本预检及独立传输期限源码。'
          '平台E2E授权/盲转持久化与原密文补传、本机SDK/实际Host私有执行/LOCAL_SEGMENTED_1、'
          'Android JNI/媒体/有界重排、原签收/独立计量和真实资格尚未形成完整通路；不得宣称可用或合格E2E。'
          '115控制规划/150既有schema登记/84表/75控制HTTP及3个SDK入口不变；新增独立未发布envelope Schema，无SQL。'
          '全56/222保持待收口；源码测试未编译/运行，依赖工具未调用，新媒体未录。先完整生产接线和测试源码，再集中验证；本次不是开发轮完成。')
ITEMS = [
    ('122', '固定原生密码与JCS依赖源码', 'KE001—008：版本/提交/归档摘要、静态CGO FFI、无库失败关闭、JCS严格包装和原许可证；未构建/执行'),
    ('123', '原安装密钥库与元数据入口源码', 'KE009—017：原OS根/锁、AEAD日志/anchor、用途独立和八命令；无明文或私钥导出'),
    ('124', '独立信任与原轮换恢复源码', 'KE018—025：OUT_OF_BAND pin、旧签新/同回执恢复、历史原用途/撤销取消/关闭join；未验证'),
    ('125', '签名二进制密文与完整结束源码', 'KE026—038：C/sealed/H/AAD、严格记录、END FINAL、仅密文摘要清单和无钥结构检查；非业务授权'),
    ('126', '原双方密钥与输入认证屏障源码', 'KE039—044：精确Binding、输入完整认证/同库预检、结果认证前缀及传输期限；执行与平台接线仍待'),
    ('127', '独立结构契约与原生依赖工具源码', 'KE045—047：独立未发布Schema、受限缓存/归档物化、build阶段门槛；工具未调用/无迁移'),
    ('128', 'KE测试定义与双语完整待办登记', 'KE048—050：源码定义/限制/恢复及全56/222保留；未编译/运行，不作整轮完成')]
REQUIREMENTS = [
    ('固定套件', '只接受固定suite/版本，不静默替换算法'),
    ('上游固定', 'libsodium提交/归档SHA256/ISC；固定不等于独立签名核验'),
    ('静态原生FFI', 'Windows/Linux CGO p11_sodium，头/库版本与常量联核'),
    ('无库失败关闭', '未启用或不匹配返回不可用，不替代或明文降级'),
    ('原生状态', '有界secretstream状态，失败/FINAL关闭，无状态序列化'),
    ('固定JCS', '上游源码与许可证原字节不格式化，附完整Apache文本'),
    ('严格元数据', '长度/深度/重复键/Unicode和精确JCS；prompt字节不重编码'),
    ('签名域', 'bundle/轮换/C/H/清单/AAD各独立域，无自引用'),
    ('原秘密根', '复用原OS master及原目录锁，不加第二写者'),
    ('先持久anchor', '原随机anchor保存后创建日志，丢日志不重新初始化'),
    ('AEAD日志', '原binding的AES-GCM日志/原子替换/flush'),
    ('分用途钥', '内容/身份/买方签收独立，auth/update不复用'),
    ('public bundle', '原owner/endpoint/role/version/钥/规范UTC有效期自签'),
    ('角色约束', 'BUYER独立receipt，SELLER匹配原设备且不带receipt'),
    ('有界历史', '本机32/对端128/256KiB，不覆盖历史或自动复用endpoint'),
    ('元数据命令', '八项CLI接入p11-node，16KiB严格stdin和固定诊断'),
    ('无内容oracle', '无私钥导出/任意签名/卖家明文解密CLI或内容监听器'),
    ('独立首次pin', 'OUT_OF_BAND指纹与reference，不自动信任TOFU/平台自签'),
    ('精确peer scope', 'owner/endpoint/role/bundlehash精确查找，撤销不再用'),
    ('旧身份签轮换', '同主体/端点/角色严格版本加一，旧签新bundle'),
    ('原回执恢复', '本机保存完整原transition，同原expires恢复不另造钥'),
    ('对端重复联核', '记录原rotationhash，重复须原内容和旧有效签名'),
    ('原历史用途', 'RETIRED仅显式原bundle，不换主体/隐私/原合同'),
    ('撤销和poison', '先持久再取消关联用途，保存失败毒化并取消新用途'),
    ('实际关闭join', '用途有界计数/独立取消，退出清副本后再释放原根/锁'),
    ('独立二进制档', 'BINARY_STREAM_1不冒充旧JSON channel或已协商互操作'),
    ('完整C', '绑定原双方/合同/job/attempt/fence/环境/policy/profile/对象/epoch/期限'),
    ('事前未知字段', 'INPUT无resource代次/未来delivery，执行权另核双围栏'),
    ('sealed内绑定', '随机流钥+域C摘要+header封给原内容接收公钥'),
    ('H先验签', '外部Expected原公钥/Context先验签再unseal，不信自带根'),
    ('长度顺序AAD', 'uint32长度/uint64序号与unsigned-H域hash共同AAD'),
    ('严格内层记录', 'type/零flags/零reserved/精确payloadlen，有界且顺序连续'),
    ('记录顺序', 'HEADERS唯一首条，DATA不得位于USAGE/ERROR之后'),
    ('加密END', 'END自身TAG_FINAL，含此前计数/字节/私有DATA摘要'),
    ('私有错误控制', '错误详情不出公共层，CANCEL独立流而非旁路执行授权'),
    ('仅密文清单', '原C/H/cipherhash/bytes/key/expiry独立签名，无公开明文摘要'),
    ('完整Finish', 'FINAL+manifest+完整hash/长度+真实EOF才cryptoComplete'),
    ('平台无钥检查', 'InspectOpaque只核签名/结构/密文hash，不能证明AEAD/私有END'),
    ('原双方组合', 'Binding精确双端主体/角色/钥和buyer-result-key，不能自换peer'),
    ('输入完整屏障', 'INPUT完整认证后才能交内部consumer，不提前触发执行'),
    ('同库协议预检', '输入用gateway/core原三文本profile/publicmodel Preflight'),
    ('输出认证前缀', '可内部交认证prefix；END必须完整Finish，不自动签收'),
    ('传输计算分离', '输入不晚于硬截止，输出最多其后30min且双钥覆盖deadline'),
    ('先验limits', '读取输入前限制预算，受原ctx/传输deadline/撤销取消'),
    ('独立结构Schema', '新增外层JSON结构schema，跨字段/二进制/业务校验仍在源码'),
    ('受限依赖物化', '本runtime内固定归档/无链接路径穿越/缓存原字节，不运行安装脚本'),
    ('原生构建门槛', 'build先require_test_phase，不在CODE_FIRST编译/启动库'),
    ('测试源码', '密码/身份/截断/撤销/恢复等定义未编译/运行，不能视为通过'),
    ('明确生产缺口', '平台E2E/SDK/Host/JNI/媒体/补传/签收仍未组成完整通路'),
    ('全量范围保留', '双语README/更新说明及全部56任务222子项；不拆轮、不部署、不虚拟化')]


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
            definitions.append({'id': f'KE-T{len(definitions)+1:03d}', 'name': name, 'source': source,
                                'native_build_tag': raw.startswith('//go:build p11_sodium &&'),
                                'status': 'SOURCE_ONLY_NOT_EXECUTED'})
    return definitions


def source_schema():
    raw = (ROOT/'dev/src/client/protocol/cipherstream/types.go').read_text(encoding='utf-8')
    raw = raw[raw.index('type Context struct'):raw.index('func Hash')]
    fields = re.findall(r'^\s+\w+\s+string\s+`json:"([^",]+)(?:,omitempty)?"`', raw, re.M)
    properties = {f: {'type': 'string', 'maxLength': 512} for f in fields}
    constants = {'profile_id': 'BINARY_STREAM_1', 'suite_id': 'P11_SODIUM_1_0_22_SEAL_SECRETSTREAM_ED25519_JCS_1',
                 'canonicalization_id': 'RFC8785_JCS_1', 'service_mode': 'E2E', 'privacy_profile': 'PLATFORM_BLIND'}
    for key, value in constants.items():
        properties[key] = {'const': value}
    for key in ['contract_hash', 'buyer_result_key_fingerprint', 'receiver_key_fingerprint', 'kernel_environment_digest']:
        properties[key] = {'type': 'string', 'pattern': '^[0-9a-f]{64}$'}
    for key in ['order_id', 'job_id', 'attempt_id', 'seller_id', 'buyer_id', 'seller_endpoint_id', 'buyer_endpoint_id',
                'buyer_result_key_id', 'sender_endpoint_id', 'sender_signing_key_id', 'receiver_endpoint_id',
                'receiver_encryption_key_id', 'local_policy_id', 'object_id', 'stream_id', 'delivery_id']:
        properties[key] = {'type': 'string', 'pattern': '^[A-Za-z][A-Za-z0-9_-]{2,127}$'}
    for key in ['fencing_token', 'epoch', 'delivery_revision']:
        properties[key] = {'type': 'string', 'pattern': '^[1-9][0-9]*$', 'maxLength': 20 if key == 'epoch' else 19}
    properties['api_profile_id'] = {'enum': ['P11_CHAT_TEXT_NATIVE_1', 'P11_RESPONSES_TEXT_NATIVE_1', 'P11_MESSAGES_TEXT_NATIVE_1']}
    properties['direction'] = {'enum': ['BUYER_TO_SELLER', 'SELLER_TO_BUYER']}
    properties['purpose'] = {'enum': ['INPUT', 'OUTPUT', 'CONTROL']}
    properties['stream_header'] = {'type': 'string', 'pattern': '^[A-Za-z0-9_-]{32}$'}
    for key in ['deadline', 'original_hard_deadline']:
        properties[key] = {'type': 'string', 'format': 'date-time', 'maxLength': 40, 'pattern': 'Z$'}
    context = {'type': 'object', 'additionalProperties': False, 'properties': properties,
               'required': [f for f in fields if f not in {'delivery_id', 'delivery_revision'}],
               'allOf': [
                   {'if': {'properties': {'purpose': {'const': 'OUTPUT'}}},
                    'then': {'required': ['delivery_id', 'delivery_revision'], 'properties': {'direction': {'const': 'SELLER_TO_BUYER'}}},
                    'else': {'not': {'anyOf': [{'required': ['delivery_id']}, {'required': ['delivery_revision']}]}}},
                   {'if': {'properties': {'purpose': {'const': 'INPUT'}}},
                    'then': {'properties': {'direction': {'const': 'BUYER_TO_SELLER'}}}}]}
    dump(SCHEMA, {'$schema': 'https://json-schema.org/draft/2020-12/schema',
                  '$id': 'urn:p11:source:binary-stream-envelope:1',
                  'title': 'Unpublished BINARY_STREAM_1 signed envelope structure',
                  '$comment': 'SOURCE_ONLY. Not a binary channel negotiation or complete semantic/cryptographic/business validator.',
                  'type': 'object', 'additionalProperties': False,
                  'required': ['context', 'sealed_key', 'sealed_key_sha256', 'signature_profile_id', 'signature'],
                  'properties': {'context': context,
                                 'sealed_key': {'type': 'string', 'pattern': '^[A-Za-z0-9_-]+$', 'minLength': 66, 'maxLength': 5462},
                                 'sealed_key_sha256': {'type': 'string', 'pattern': '^[0-9a-f]{64}$'},
                                 'signature_profile_id': {'const': 'ED25519_JCS_1'},
                                 'signature': {'type': 'string', 'pattern': '^[A-Za-z0-9_-]{86}$'}}})


def source_contract(tests):
    source_schema()
    dump(CONTRACT, {
        'profile_id': 'P11_ENDPOINT_CIPHER_SOURCE_1', 'requirements': 'KE-001—050', 'authority': ANNEX,
        'status': 'SOURCE_ONLY_UNVERIFIED', 'compiled_or_executed': False, 'activation': False,
        'development_policy': RULES, 'source_refs': REFS, 'independent_envelope_schema': SCHEMA,
        'suite_id': 'P11_SODIUM_1_0_22_SEAL_SECRETSTREAM_ED25519_JCS_1', 'canonicalization_id': 'RFC8785_JCS_1',
        'scope': {'platform_blind_only': True, 'enhanced_qualified': False, 'metadata_cli_wired': True,
                  'platform_e2e_authorization_relay_sdk_worker_jni_complete': False,
                  'text_profile_ids': ['P11_CHAT_TEXT_NATIVE_1', 'P11_RESPONSES_TEXT_NATIVE_1', 'P11_MESSAGES_TEXT_NATIVE_1'],
                  'media_profiles_or_reorder_or_segmented_cache_complete': False},
        'keys': {'original_os_master_and_single_writer': True, 'anchor_before_journal': True,
                 'aead': 'AES_256_GCM', 'separate_content_identity_buyer_receipt': True,
                 'initial_trust': 'INDEPENDENT_OUT_OF_BAND', 'lost_state_reinitialized': False,
                 'original_rotation_receipt_recovered': True, 'revocation_cancels_owned_uses': True,
                 'close_joins_owned_uses': True, 'static_receiving_keys_full_forward_secrecy': False},
        'wire': {'profile_id': 'BINARY_STREAM_1', 'old_json_channel_changed': False,
                 'resource_generation_or_future_delivery_in_input': False, 'signed_header_before_unseal': True,
                 'record_aad': ['unsigned_header_domain_hash', 'body_length_u32be', 'sequence_u64be'],
                 'encrypted_end_is_tag_final': True, 'plaintext_digest_public': False,
                 'finish_requires_final_manifest_hash_length_and_eof': True,
                 'opaque_inspection_proves_private_aead_or_end': False,
                 'input_complete_and_preflight_before_consumer': True,
                 'authenticated_output_prefix_is_acceptance_or_trusted_usage': False},
        'budgets_not_measurements': {'header_bytes': 16384, 'inner_record_bytes': 65536, 'payload_bytes': 65528,
                                     'record_body_bytes': 65561, 'record_wire_bytes': 65565, 'records': 131072,
                                     'input_data_bytes': 2 << 20, 'output_data_bytes': 64 << 20, 'cipher_object_bytes': 66 << 20,
                                     'local_key_versions': 32, 'peer_key_versions': 128, 'key_journal_bytes': 256 << 10,
                                     'active_key_uses': 2, 'metadata_stdin_bytes': 16384, 'key_validity_days': 90,
                                     'output_transport_after_compute_maximum_seconds': 1800},
        'test_definitions': tests, 'test_definition_count': len(tests), 'native_dependency_tool_executed': False,
        'control_http_operations': 75, 'planned_control_operations': 115, 'registered_control_schemas': 150,
        'platform_tables': 84, 'native_sdk_data_plane_operations': 3, 'sql_changed_or_migrated': False,
        'full_scope_tasks': 56, 'full_scope_steps': 222, 'full_development_round_complete': False,
        'test_admission': 'DENIED_UNTIL_ALL_IN_SCOPE_CODE_COMPLETE'})
    name = 'scripts/技术开发数据.py'
    body = (ROOT/name).read_text(encoding='utf-8')
    if "'endpoint_cipher_source':" not in body:
        addition = ("      'endpoint_cipher_source':{'status':'SOURCE_ONLY_UNVERIFIED',\n"
                    f"        'annex':'{ANNEX}','source_contract':'{CONTRACT}','independent_envelope_schema':'{SCHEMA}',\n"
                    "        'original_endpoint_metadata_cli_and_signed_cipher_libraries_source_wired':True,\n"
                    "        'platform_e2e_authorization_relay_sdk_worker_jni_complete':False,\n"
                    "        'input_complete_authentication_and_preflight_before_consumer':True,\n"
                    "        'sql_changed_or_migrated':False,'native_dependency_tool_executed':False,'compiled_or_executed':False},\n")
        write(name, body.replace("      'unproven_capabilities':", addition+"      'unproven_capabilities':", 1))


def progress(plan, stage, status, tests):
    lines = ['# R026 全量代码阶段进展', '',
             '**进行中，SOURCE_ONLY / 未验证。** 同一56/222全部生产代码、实际接线及测试源码齐备后集中验证。122—128仅源码保存，不是整轮完成。',
             '', '| 编号 | 已写内容 | 实际状态 |', '|---|---|---|']
    lines += [f"| {i['id']} | {i['title']} | {i['status']} |" for i in stage['implemented_items']]
    lines += ['', DETAIL, '', f'本次 {len(tests)} 项 KE 新测试定义均未编译/运行；固定密码依赖工具没有调用。',
              '', '无SQL变更；001—021平台双份SQL/source及已登记端侧DDL/source冻结；后续平台迁移仅022+。',
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
    marker = '## R026 KE-001—050 端点密钥与签名密文流 / Endpoint keys and signed cipher source'
    zh = (f'R026新增[KE-001—050]({ANNEX})：固定libsodium/JCS依赖、原OS秘密根/单写者AEAD密钥库、分用途钥与八项元数据CLI、'
          '独立pin/原轮换回执恢复/撤销及join、签名BINARY_STREAM_1/加密END/仅密文摘要清单/无钥结构检查、原双方用途组合/完整输入屏障/同库预检/独立传输期限源码。'
          f'{len(tests)}项新测试定义未编译/运行，依赖工具未调用。新增独立未发布envelope Schema，115控制规划/150既有schema登记/84表/75控制HTTP及3个SDK入口不变，无SQL/迁移。'
          '平台E2E授权/盲转持久化及原密文补传、本机SDK/实际Host/LOCAL_SEGMENTED_1、Android JNI/媒体/有界重排、原签收/独立计量及真实资格仍待；'
          '不能称完整可用或合格E2E。全部56任务222子项继续保留，先完整生产接线/测试源码再集中验证和重录媒体；本次不是开发轮完成。')
    en = (f'R026 adds [KE-001–050]({ANNEX}): pinned libsodium/JCS source, original OS-root/single-writer AEAD key vault, separate key purposes and eight metadata CLI operations, '
          'independent pins/original rotation-receipt recovery/revocation/join, signed BINARY_STREAM_1/encrypted END/ciphertext-only manifest/opaque structural inspection, '
          'exact-party composition/full INPUT barrier/shared preflight and separate transport deadlines. '
          f'{len(tests)} new test definitions are uncompiled/unexecuted; the dependency tool has not run. An independent unpublished envelope schema is added; '
          '115 planned control operations/150 existing registered schemas/84 tables/75 control HTTP and three SDK data-plane registrations stay unchanged, with no SQL/migration. '
          'Platform E2E authorization/blind storage/original retransmission, local SDK/actual Host/LOCAL_SEGMENTED_1, Android JNI/media/bounded reorder, '
          'original acceptance/independent meters and real qualification remain open. This is not a usable or qualified complete E2E path. '
          'All 56 tasks/222 steps stay open; complete production wiring/test source precede batch validation and new media. This is not a completed development round.')
    section(ANNEX, '## KE 编号需求约束索引', '| 编号 | 项目 | 源码约束 |\n|---|---|---|\n'+
            '\n'.join(f'| KE-{i:03d} | {title} | {detail} |' for i, (title, detail) in enumerate(REQUIREMENTS, 1)))
    section(ANNEX, '## KE 测试源码定义索引', '| 编号 | 定义 | 源码 | Native tag | 状态 |\n|---|---|---|---|---|\n'+
            '\n'.join(f"| {t['id']} | {t['name']} | {t['source']} | {t['native_build_tag']} | 未编译/未运行 |" for t in tests))
    for name, content in [('dev/repository/README.zh-CN.md.in', zh), ('dev/repository/README.en.md.in', en)]:
        section(name, marker, content)
        body = (ROOT/name).read_text(encoding='utf-8')
        body = re.sub(r'\n<!-- R026-[A-Z-]+-CURRENT -->.*?<!-- /R026-[A-Z-]+-CURRENT -->\n', '\n', body, flags=re.S)
        body = body.replace('以上 PD 说明', '以上 KE 说明').replace('The PD notice above', 'The KE notice above')
        title, rest = re.sub(r'\n{3,}', '\n\n', body).split('\n', 1)
        write(name, title+'\n\n<!-- R026-KE-CURRENT -->\n'+content+'\n<!-- /R026-KE-CURRENT -->\n'+rest)
    for name, content in [('CHANGELOG.zh-CN.md', zh), ('CHANGELOG.md', en)]:
        body = (ROOT/name).read_text(encoding='utf-8')
        body = re.sub(r'(?m)^- R026(?:新增| adds )\[KE-001[—–]050\].*\n', '', body)
        title, rest = body.split('\n', 1)
        write(name, title+'\n\n- '+content+'\n'+rest)
    for name in ['dev/repository/USAGE.md', 'dev/repository/SYSTEM_DESIGN.md']:
        section(name, marker, zh.replace('(docs/', '(../../docs/')+'\n\n'+en.replace('(docs/', '(../../docs/'))
    section('dev/progress/README.md', marker, zh.replace('(docs/', '(../../docs/'))
    for name in ['dev/src/client/node/README.md', 'dev/src/client/README.md']:
        prefix = '(../../../../docs/' if '/node/' in name else '(../../../docs/'
        section(name, marker, zh.replace('(docs/', prefix)+'\n\n'+en.replace('(docs/', prefix))
    for name in ['docs/技术开发/00-技术文档导航与实施基线.md', 'docs/技术开发/08-端到端加密协议与密钥实现设计.md']:
        section(name, marker, zh.replace('(docs/技术开发/', '('))
    for name in ['AGENTS.md', 'dev/AGENTS.md']:
        section(name, marker, zh.replace('(docs/', '(../docs/' if name.startswith('dev/') else '(docs/')+
                '\n\n最新唯一增量整理器为'+TOOL+'；禁止重跑旧PD/SI/DC/DA/RA/RT/RF/CK/GF/PH/PT/MP/MG/MS/WS/CH/CE/MW/RN整理器覆盖当前KE状态。'
                '遵守'+RULES+'：同一R026全56/222生产接线和测试源码齐备前，不编译/测试/核验/迁移或启动DB、密码库、内核、模型、GPU、手机、APK或浏览器；源码保存不拆轮。'
                '001—021平台双份SQL/source和已登记端侧DDL/source以及固定第三方JCS源文件/许可证原字节冻结；平台只追加022+。'
                '不调度Docker/WSL/QEMU/VM/模拟器；不调度子代理；DEV55延后、DEV32无真实证明关闭；浏览器用后关闭不用页。')


def main():
    tests = inventory()
    source_contract(tests)
    plan, stage, status = [load(n) for n in ['dev/progress/code-first-plan.json', 'dev/progress/R026-code-stage.json', 'dev/progress/implementation-status.json']]
    integration = {'status': 'SOURCE_ONLY_UNVERIFIED', 'requirements': 'KE001—050', 'annex': ANNEX,
                   'source_contract': CONTRACT, 'independent_envelope_schema': SCHEMA,
                   'metadata_cli_and_signed_cipher_libraries_source_wired': True,
                   'platform_e2e_authorization_relay_sdk_worker_jni_complete': False,
                   'compiled_or_executed': False, 'native_dependency_tool_executed': False,
                   'sql_changed_or_migrated': False, 'new_media_captured': False, 'full_development_round_complete': False,
                   'test_definition_count': len(tests), 'control_http_operations': 75, 'native_sdk_data_plane_operations': 3,
                   'planned_control_operations': 115, 'registered_control_schemas': 150, 'platform_tables': 84,
                   'remaining': ['平台E2E原上下文/授权/无钥持久化/原密文补传/背压及有界重排',
                                 '本机SDK/实际Host私有执行/LOCAL_SEGMENTED_1/Android JNI/媒体',
                                 '原买家receipt/可信usage/资金事务、独立信任UI与真实资格',
                                 '固定库独立签名核验/native构建/跨端向量/LP/PE/性能实测',
                                 '完整56/222其他生产接线和测试源码后集中验证；DEV55延后/DEV32无证明关闭']}
    for record in (plan, stage):
        record['endpoint_cipher_source_integration'] = deepcopy(integration)
    for task in plan['tasks']:
        if task['id'] in TASKS:
            task['code_status'] = 'IN_PROGRESS'
            task['endpoint_cipher_source_limit'] = DETAIL
            for step in task['steps']:
                if step['id'] in TOUCHED_STEPS:
                    step['code_status'] = 'IN_PROGRESS'
                    step['implementation_refs'] = sorted(set(step.get('implementation_refs', [])+REFS))
    for task in status['tasks']:
        if task['id'] in TASKS:
            task['implementation_status'] = 'IN_PROGRESS'
            task['endpoint_cipher_source_limit'] = DETAIL
            task['current_source_refs'] = sorted(set(task.get('current_source_refs', [])+REFS))
            task['current_validation_status'] = 'NOT_EXECUTED'
    status['activity_source'] = '2026-11-07 CODE_FIRST：KE001—050 / IMP122—128；'+DETAIL
    for number, title, state in ITEMS:
        entry = {'id': 'IMP-R026-'+number, 'title': title, 'status': state}
        stage['implemented_items'] = [old for old in stage['implemented_items'] if old['id'] != entry['id']]+[entry]
    stage.update({'complete': False, 'validation_started_after_user_policy': False, 'tasks': deepcopy(status['tasks']),
                  'endpoint_cipher_test_definitions': {'tests': tests, 'ids': [t['id'] for t in tests], 'status': 'SOURCE_ONLY_NOT_EXECUTED'}})
    stage['source_files'] = sorted(set(stage.get('source_files', [])+REFS))
    for name, value in [('dev/progress/code-first-plan.json', plan), ('dev/progress/R026-code-stage.json', stage), ('dev/progress/implementation-status.json', status)]:
        dump(name, value)
    progress(plan, stage, status, tests)
    documentation(tests)
    print(json.dumps({'source_document_generation': 'KE001—050 / IMP122—128', 'unexecuted_test_definitions': len(tests),
                      'independent_unpublished_envelope_schema': SCHEMA, 'compiler_test_validator_migration_runtime': 'NOT_RUN',
                      'all_tasks_open': len(plan['tasks']), 'all_steps_open': sum(len(t['steps']) for t in plan['tasks'])}, ensure_ascii=False))


if __name__ == '__main__':
    main()
