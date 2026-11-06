"""ER source/document authoring only. No compiler, tests, validator or runtime."""
from collections import Counter
from copy import deepcopy
import hashlib
import importlib.util
import json
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[2]
ANNEX = 'docs/技术开发/附录-账户设备公钥目录与原请求发布源码.md'
CONTRACT = 'docs/技术开发/contracts/account-endpoint-directory-source-v1.json'
TOOL = 'dev/tools/整理R026公钥目录源码.py'
RULES = 'dev/开发轮次执行规则.md'
MIGRATION = '022_account_endpoint_directory.sql'
OPERATIONS = [
 ('registerEndpointBundle', 'POST', '/endpoint-bundles'),
 ('listOwnEndpointBundles', 'GET', '/endpoint-bundles'),
 ('getOwnEndpointBundle', 'GET', '/endpoint-bundles/{recipient_id}'),
 ('rotateEndpointBundle', 'POST', '/endpoint-bundles/{recipient_id}/rotations'),
 ('revokeEndpointBundle', 'POST', '/endpoint-bundles/{recipient_id}/revoke'),
 ('getOrderEndpointBundle', 'GET', '/orders/{order_id}/endpoint-bundles/{recipient_id}')]
SCHEMAS = ['EndpointPublicKey', 'EndpointPublicBundle', 'EndpointPublicRotation', 'EndpointBundleRegistration',
           'EndpointBundleRotationWrite', 'EndpointBundleRevocation', 'EndpointDirectoryRecord', 'EndpointDirectoryPage']
TABLES = {'endpoint_public_bundles': 'DATA-085', 'endpoint_public_key_claims': 'DATA-086'}
TESTS = ['dev/src/shared/endpointwire/bundle_test.go', 'dev/src/shared/endpointdirectory/types_test.go',
         'dev/src/internal/endpointregistry/types_test.go', 'dev/src/tests/integration/endpoint_directory_test.go',
         'dev/src/tests/integration/endpoint_directory_keys_test.go', 'dev/src/api/tests/endpoint_directory_http_test.go',
         'dev/src/client/protocol/devicecontrol/endpoint_directory_test.go',
         'dev/src/client/node/endpoint_directory_native_test.go', 'dev/src/client/cmd/p11-node/endpoint_directory_test.go']
REFS = [ANNEX, CONTRACT, TOOL, RULES, 'scripts/白皮书契约数据.py', 'scripts/技术开发数据.py',
        'dev/src/internal/endpointregistry/README.md', 'dev/src/shared/endpointwire/README.md',
        'docs/白皮书/contracts/openapi-control-v3.0.json', 'dev/planning/编号登记-v1.0.json',
        'docs/技术开发/database/migrations/'+MIGRATION, 'dev/src/internal/persistence/migrations/'+MIGRATION,
        'dev/src/internal/persistence/migrations/source-v22.json', 'dev/src/api/cmd/p11-api/main.go',
        'dev/src/api/internal/http/server.go', 'dev/src/api/internal/http/routes.go', 'dev/src/api/internal/http/routes.json',
        'dev/src/api/internal/http/endpoint_directory.go', 'dev/src/internal/identity/sessions.go',
        'dev/src/internal/dispatch/device/auth.go', 'dev/src/internal/testsupport/endpoint_directory.go',
        'dev/src/internal/testsupport/market.go', 'dev/src/tests/integration/core_test.go',
        'dev/src/client/crypto-gateway/endpoint/public.go', 'dev/src/client/crypto-gateway/endpoint/trust.go',
        'dev/src/client/crypto-gateway/endpoint/vault.go', 'dev/src/client/crypto-gateway/endpoint/vault_test.go',
        'dev/src/client/node/store.go', 'dev/src/client/node/control.go', 'dev/src/client/node/endpoint_directory.go',
        'dev/src/client/protocol/devicecontrol/endpoint_directory.go', 'dev/src/client/cmd/p11-node/main.go',
        'dev/src/client/cmd/p11-node/endpoint_directory.go'] + TESTS
for package in ['shared/endpointwire', 'shared/endpointdirectory', 'internal/endpointregistry']:
    REFS += [p.relative_to(ROOT).as_posix() for p in sorted((ROOT/'dev/src'/package).rglob('*.go'))]
REFS = sorted(set(REFS))
TASKS = {'DEV-02', 'DEV-03', 'DEV-06', 'DEV-29', 'DEV-53', 'DEV-54'}
STEPS = {'DEV-02.01', 'DEV-02.02', 'DEV-03.01', 'DEV-03.02', 'DEV-03.04', 'DEV-06.02',
         'DEV-29.01', 'DEV-29.02', 'DEV-29.03', 'DEV-53.01', 'DEV-53.02', 'DEV-54.02'}
ITEMS = [
 ('136', '公共签名格式与无私钥依赖隔离源码', 'ER001—008：原公钥/轮换字节与验签移入shared，端侧类型别名保持原格式；历史用途复用拒绝，未验证'),
 ('137', '公钥目录及历史用途占用前向源码', 'ER009—017：022/DATA085—086、原recipient投影、不可改历史/延期成对约束、全局key用途与公钥指纹；未迁移'),
 ('138', '当前授权及原请求幂等生命周期源码', 'ER018—028：登记/原签名轮换/版本撤销/本人查询与有界分页、原号只存引用后查当前；未编译运行'),
 ('139', '原C2C订单发现与六HTTP宿主接线源码', 'ER029—036：当前原买家/原卖家与原设备、scope分离、CSRF/设备证明和同DB实际入口；不是E2E执行权'),
 ('140', '客户端原公钥发布及五管理入口源码', 'ER037—043：AEAD原pending先于网络、只取已保存的公钥/轮换证明、未知原字节/键恢复；不更换密钥或启用内核'),
 ('141', '授权隔离及原请求恢复测试源码', 'ER044—047：公共签名/原生DB与HTTP、设备/角色撤销、分叉/原号恢复及独立native组合；未编译或运行'),
 ('142', 'ER技术契约及双语全量待办同步', 'ER048—050：完整50约束/新编号/双语说明/全56任务222子项保留；不是开发轮完成')]
REQUIREMENTS = [
 ('原签名字节', '原P11_ENDPOINT_PUBLIC_1及ROTATION_1字段/JCS/domain保持；仅公开SigningMessage接口'),
 ('平台无私钥依赖', 'endpointwire只有标准Ed25519/JCS公钥核验，不导入vault/sodium/推理内核'),
 ('类型兼容', '原端侧Bundle/PublicKey/Transition采用公共类型别名，不重编码旧签名状态'),
 ('严格公钥格式', '32字节base64url/小写SHA256、用途算法、ID与签名严格检查'),
 ('角色用途', 'BUYER分身份/内容/receipt；SELLER不带buyer receipt；各用途不共用钥'),
 ('固定有效窗', '严格UTC/规范版本、最长90天；新登记/轮换用数据库当前时刻检查'),
 ('无旧用途复用', '轮换拒绝旧各用途的ID/公钥/指纹别名，SQL全历史跨端点占用'),
 ('不冒充信任', '自签名及账户绑定不证明内容私钥持有、独立核验、TEE或执行合格'),
 ('只前向022', '001—021双份SQL/source及端侧登记DDL与JCS许可证原字节冻结'),
 ('原recipient投影', 'content key ID为recipient_id；owner/设备/版本/公钥/有效窗逐项同原bundle'),
 ('目录双表', 'DATA085保存原bundle/轮换字节，DATA086保存唯一用途claim；没有内容私钥列'),
 ('精确字节', 'bundle<=8192/transition<=12000、原hash/JCS验签；JSON结构检查不替代密码验证'),
 ('原设备快照', 'SELLER endpoint=device_id，记录当前逐请求认证钥指纹；BUYER不挂device'),
 ('历史不可替换', 'owner/endpoint/role/版本/字节/期限/前驱不可改，不删历史或claim'),
 ('延迟成对约束', 'recipient/bundle/全部用途claim同事务提交，撤销时间成对一致'),
 ('原连续前驱', '同owner/endpoint/role、版本+1、旧状态RETIRED；前驱至多一个后继'),
 ('有界历史', '每owner每role最多32端点，每端点最多32版本；不驱逐历史绕过容量'),
 ('当前角色复查', 'SQL内当前账户/session/role；API key不能管理目录，旧幂等结果也复查'),
 ('卖方设备签名', '新SELLER登记/轮换只接受原设备证明及APPROVED卖家；公开用途不得复用设备auth公钥，浏览器不伪造设备'),
 ('本人紧急撤销', '当前同owner角色session可撤销其SELLER版本；设备只操作自己endpoint'),
 ('锁序', '用户/凭据/角色/卖家/设备后owner advisory与目录；设备SHARE不再升级'),
 ('初始登记', '只能version1；已有端点不得新v1替换，完全相同bundle可查回原号'),
 ('原轮换证明', '当前有效旧版本及旧身份签名授权新连续版本；不以账户会话代替'),
 ('不分叉恢复', '原证明/相同后继可恢复；其他后继冲突，回滚旧状态和全部claim'),
 ('明确版本撤销', 'expected_bundle_hash精确匹配、单向REVOKED；不隐式全链撤销或重启旧钥'),
 ('幂等只存引用', '安全响应只保存recipient_id，成功后重新按当前授权查询；不回放历史ACTIVE'),
 ('历史签名核验', '最多32层前驱，按各自原登记时刻验证；过期不抹去原有效轮换事实'),
 ('本人有界分页', '每页2/64KiB，仅当前本人角色的最高保留版本；撤销不回落旧版'),
 ('原订单买家', '真实当前BUYER session和原订单buyer，不能用API key或他人订单枚举'),
 ('原C2C合同', '原contract hash/实际seller/双方不相同，E2E PLATFORM_BLIND及原deadline/交易状态'),
 ('原合同设备', '目标设备必须同时在原DeviceIDs和Bindings，且公钥owner/role匹配原卖家'),
 ('当前卖家身份', '复查seller账户/角色/APPROVED及原device auth指纹；撤权/换身份失败关闭'),
 ('发现不是授权', '不签E2E租约、不分配资金、不创建密文通道、不放行私钥或自动执行'),
 ('六独立接口', 'INT116—121使用/endpoint-bundles，旧/recipients JSON设计不冒充本版本'),
 ('宿主同原库', '原生p11-api实际配置EndpointDirectory；配置不同库或未配置失败关闭'),
 ('现有HTTP保护', '严格JSON/Origin/CSRF/设备逐请求签名/nonce及固定安全错误沿用'),
 ('只发布本机原钥', 'node从原OS-root密钥库读取自己SELLER公钥，不接受外部任意bundle替换'),
 ('只取原证明', 'OriginalLocalRotation读取已持久公有证明，不重新签名/生成新钥'),
 ('发送前持久', '原node AEAD pending保存kind/resource/body/key后才HTTP；本地提交未知不发送'),
 ('未知保留原号', 'directory-recover及通用recover按原正文/URI/键发送，只更新证明nonce'),
 ('严格确认', '核reply的profile/签名bundle/hash/双方/设备/原对象；坏成功或未知保留pending'),
 ('不撤本地私钥', '目录撤销只撤指定public版本；本地crypto-revoke/活动使用cancel/join独立处理'),
 ('五元数据CLI', 'publish/revoke/get/list/recover；原单控制owner、先停可变loop，无正文或内容listener'),
 ('公共格式测试源', '篡改/规范化/用途/有效窗/连续签名与历史钥复用拒绝定义；未执行'),
 ('原生DB测试源', '当前session/device、跨账户/全局claim/配额/撤销/并发后继与原订单定义；未执行'),
 ('实际HTTP测试源', 'CSRF/未知字段/有界cursor/owner/设备nonce/缺配置定义；未运行服务'),
 ('独立native测试源', '真实vault组合的断回复/重开原intent/原轮换证明；仅定义，不运行ABI'),
 ('权威契约', '八新schema/六INT/两DATA显式追加，旧schema与编号不改义'),
 ('待办不隐藏', 'E2E grant/平台无钥存储/SDK与Host私有执行/JNI/媒体/独立计量等继续待办'),
 ('同轮全量编码', '全56任务222子项保留未验收，全部生产接线及测试源齐备后才集中验证')]


def load(name):
    return json.loads((ROOT/name).read_text(encoding='utf-8'))


def write(name, body):
    p = ROOT/name
    ending = '\r\n' if p.exists() and b'\r\n' in p.read_bytes() else '\n'
    p.parent.mkdir(parents=True, exist_ok=True)
    p.write_bytes(body.replace('\r\n', '\n').replace('\n', ending).encode('utf-8'))


def dump(name, value):
    write(name, json.dumps(value, ensure_ascii=False, indent=2)+'\n')


def section(name, marker, content):
    p = ROOT/name
    body = p.read_text(encoding='utf-8') if p.exists() else ''
    if marker in body:
        start = body.index(marker); end = body.find('\n## ', start+len(marker))
        body = body[:start].rstrip()+('\n\n'+body[end+1:] if end >= 0 else '')
    write(name, body.rstrip()+'\n\n'+marker+'\n\n'+content.strip()+'\n')


def register_source():
    spec = importlib.util.spec_from_file_location('er_contract_authoring', ROOT/'scripts/白皮书契约数据.py')
    module = importlib.util.module_from_spec(spec); spec.loader.exec_module(module)
    canonical = module.make_openapi(); api = load('docs/白皮书/contracts/openapi-control-v3.0.json')
    for schema in SCHEMAS:
        api['components']['schemas'][schema] = deepcopy(canonical['components']['schemas'][schema])
    routes = load('dev/src/api/internal/http/routes.json'); registry = load('dev/planning/编号登记-v1.0.json')
    for number, (operation, method, path) in enumerate(OPERATIONS, 116):
        code = f'INT-{number:03d}'
        if operation in registry['operations'] and registry['operations'][operation] != code:
            raise ValueError('Do not renumber a registered operation')
        registry['operations'][operation] = code
        api['paths'].setdefault(path, {})[method.lower()] = deepcopy(canonical['paths'][path][method.lower()])
        item = {'operation_id': operation, 'method': method, 'path': path, 'handler': operation, 'anonymous': False}
        old = next((r for r in routes['operations'] if r['operation_id'] == operation), None)
        if old is not None and old != item: raise ValueError('Do not replace a registered HTTP binding')
        if old is None: routes['operations'].append(item)
    for table, code in TABLES.items():
        if table in registry['tables'] and registry['tables'][table] != code: raise ValueError('Preserve original DATA IDs')
        registry['tables'][table] = code
    routes['scope'] = '81 explicit control/content/public-directory source registrations. Directory metadata does not enable E2E grants, blind content or execution. SOURCE_ONLY.'
    for name, value in [('docs/白皮书/contracts/openapi-control-v3.0.json', api), ('dev/src/api/internal/http/routes.json', routes), ('dev/planning/编号登记-v1.0.json', registry)]:
        dump(name, value)
    raw = (ROOT/'docs/技术开发/database/migrations'/MIGRATION).read_bytes()
    target = ROOT/'dev/src/internal/persistence/migrations'/MIGRATION
    if target.exists() and target.read_bytes() != raw: raise ValueError('Registered SQL immutable; append 023+')
    if not target.exists(): target.write_bytes(raw)
    digest = hashlib.sha256(raw).hexdigest()
    migration = {'source': 'docs/技术开发/database/migrations/'+MIGRATION, 'source_sha256': digest,
        'migration': MIGRATION, 'migration_sha256': digest, 'policy': '022 account/device public endpoint directory only; SOURCE_ONLY, unapplied'}
    name = 'dev/src/internal/persistence/migrations/source-v22.json'
    if (ROOT/name).exists() and load(name) != migration: raise ValueError('Preserve registered migration metadata')
    if not (ROOT/name).exists(): dump(name, migration)
    name = 'scripts/技术开发数据.py'; body = (ROOT/name).read_text(encoding='utf-8')
    body = body.replace('recipient_keys encryption_intents envelopes"', 'recipient_keys encryption_intents envelopes endpoint_public_bundles endpoint_public_key_claims"')
    body = body.replace('29:"createEncryptionIntent registerRecipient getRecipient createRecipientRotation revokeRecipient"',
        '29:"createEncryptionIntent registerRecipient getRecipient createRecipientRotation revokeRecipient '+' '.join(op[0] for op in OPERATIONS)+'"')
    if "'account_endpoint_directory_source':" not in body:
        addition = ("      'account_endpoint_directory_source':{'status':'SOURCE_ONLY_UNVERIFIED',\n"
            f"        'annex':'{ANNEX}','source_contract':'{CONTRACT}',\n"
            "        'forward_source':'022_account_endpoint_directory.sql','migration_executed':False,\n"
            "        'account_device_public_binding_and_native_publish_source_wired':True,\n"
            "        'independent_trust_or_content_possession_or_execution_proven':False,\n"
            "        'platform_e2e_grant_blind_content_sdk_private_host_jni_complete':False,'compiled_or_executed':False},\n")
        body = body.replace("      'unproven_capabilities':", addition+"      'unproven_capabilities':", 1)
    write(name, body)
    name = 'dev/src/tests/integration/core_test.go'; write(name, (ROOT/name).read_text(encoding='utf-8').replace('tables != 84 {', 'tables != 86 {'))
    counts = {'planned_control_operations': len(registry['operations']), 'registered_control_schemas': len(api['components']['schemas']),
              'platform_tables': len(registry['tables']), 'control_http_operations': len(routes['operations']), 'native_sdk_data_plane_operations': 3}
    return migration, counts


def inventory():
    tests = []
    for source in TESTS:
        raw = (ROOT/source).read_text(encoding='utf-8')
        for name in re.findall(r'^func (Test\w+)\(', raw, re.M):
            tests.append({'id': f'ER-T{len(tests)+1:03d}', 'name': name, 'source': source,
                          'native_build_tag': raw.startswith('//go:build p11_sodium &&'), 'status': 'SOURCE_ONLY_NOT_EXECUTED'})
    return tests


def detail(counts):
    return ('ER补入公共公钥/轮换验签隔离、原账户设备绑定与历史key用途占用、当前角色和设备证明、'
            '原请求引用重放后查当前、连续无分叉轮换/明确版本撤销、本人有界列表和原C2C订单发现、'
            '六HTTP及同DB原生宿主接线、客户端AEAD原意图/原证明发布和五元数据CLI源码。'
            f"022/DATA085—086仅源；当前{counts['planned_control_operations']}控制规划/{counts['registered_control_schemas']}schema/"
            f"{counts['platform_tables']}平台表/{counts['control_http_operations']}控制HTTP及3个SDK入口。"
            '账户登记不是独立信任、内容私钥持有或执行/资金/密文通道授权；旧明文租约不放开。'
            '平台E2E grant/无钥持久化/SDK与实际Host私有推理/独立信任及本机配对UI/JNI/媒体、'
            '全局4MiB流控/有界重排/LOCAL_SEGMENTED_1/独立计量/原签收与真实资格、'
            '过期或丢钥显式恢复/未知文件残留恢复和全部其余56/222生产接线继续待。'
            '测试源码未编译运行、022未迁移；同一R026全量代码及测试源码齐备后集中验证，不是开发轮完成。')


def progress(plan, stage, status, tests, text):
    lines = ['# R026 全量代码阶段进展', '', '**进行中，SOURCE_ONLY / 未验证。** 同一56/222全部生产接线及测试源码后集中验证。136—142仅源码保存，不是整轮完成。',
             '', '| 编号 | 已写内容 | 实际状态 |', '|---|---|---|']
    lines += [f"| {i['id']} | {i['title']} | {i['status']} |" for i in stage['implemented_items']]
    lines += ['', text, '', f'本次 {len(tests)} 项ER新测试定义未编译/运行；含2项独立native组合定义。',
              '', '022仅源码、未迁移。001—022双份SQL/source、已登记端侧DDL/source和固定JCS源/许可证原字节冻结。',
              '', '实施状态：'+'；'.join(f'{k} {v}' for k, v in sorted(Counter(t['implementation_status'] for t in status['tasks']).items()))+'；全部222子项未验收。',
              '', '| 编号 | 全部任务 | 当前状态 | 全部仍待收口子项 |', '|---|---|---|---|']
    current = {t['id']: t for t in status['tasks']}
    for task in plan['tasks']:
        t = current[task['id']]
        lines.append(f"| {task['id']} | {task['title']} | {t['implementation_status']} | {'、'.join(t['remaining_step_ids'])} |")
    write('dev/progress/R026-全量代码阶段进展.md', '\n'.join(lines)+'\n')
    lines = ['# 全量代码优先实施清单', '', text, '', '执行规则：[CF-01—05](开发轮次执行规则.md)。同一R026不拆轮，所有未收口子项完整保留。',
             '', '| 子项 | 任务 | 未收口需求 | 代码状态 | 验收 |', '|---|---|---|---|---|']
    for task in plan['tasks']:
        for step in task['steps']:
            lines.append(f"| {step['id']} | {task['title']} | {step['code_requirement']} | {step['code_status']} | {step['acceptance_status']} |")
    write('dev/全量代码优先实施清单.md', '\n'.join(lines)+'\n')


def documentation(tests, counts, text):
    marker = '## R026 ER-001—050 账户设备公钥目录 / Account and device public endpoint directory source'
    zh = (f'R026新增[ER-001—050]({ANNEX})：'+text+f' 本次{len(tests)}项新测试定义未执行，其中2项独立native组合；无新截图或录屏。')
    en = (f'R026 adds [ER-001–050]({ANNEX}): public-only bundle/rotation verification, current account/device binding, immutable public-key purpose claims, '
          'reference-only idempotency with current rereads, non-forking signed rotation, explicit version revocation, bounded own listings, original C2C order discovery, '
          'six HTTP registrations/the same native DB host, original AEAD client publication intents and five metadata CLI actions. '
          f"022/DATA085–086 are source only: {counts['planned_control_operations']} planned operations/{counts['registered_control_schemas']} schemas/{counts['platform_tables']} tables/{counts['control_http_operations']} control HTTP and three SDK entries. "
          f'{len(tests)} new test definitions including two independent native compositions are uncompiled/unexecuted; 022 is unapplied and no new media is captured. '
          'Account binding proves neither independent endpoint trust nor content-key possession, funds, execution or a cipher-channel grant. Existing plaintext lease guards stay closed to E2E. '
          'Platform E2E grants/blind storage, private SDK/Host execution, trusted pairing UI, JNI/media, global 4MiB flow/reorder, LOCAL_SEGMENTED_1, independent meters/receipts/real qualification, '
          'explicit expired/lost-key and unknown-file recovery and all other production wiring remain open. All 56 tasks/222 steps stay in the same CODE_FIRST round; this is not a completed round.')
    section(ANNEX, '## ER 编号需求约束索引', '| 编号 | 项目 | 源码约束 |\n|---|---|---|\n'+'\n'.join(f'| ER-{i:03d} | {name} | {constraint} |' for i, (name, constraint) in enumerate(REQUIREMENTS, 1)))
    section(ANNEX, '## ER 测试源码定义索引', '| 编号 | 定义 | 源码 | Native tag | 状态 |\n|---|---|---|---|---|\n'+'\n'.join(f"| {t['id']} | {t['name']} | {t['source']} | {t['native_build_tag']} | 未编译/未运行 |" for t in tests))
    for name, content in [('dev/repository/README.zh-CN.md.in', zh), ('dev/repository/README.en.md.in', en)]:
        section(name, marker, content)
        body = (ROOT/name).read_text(encoding='utf-8')
        body = re.sub(r'\n<!-- R026-[A-Z-]+-CURRENT -->.*?<!-- /R026-[A-Z-]+-CURRENT -->\n', '\n', body, flags=re.S)
        body = body.replace('以上 IC 说明', '以上 ER 说明').replace('The IC notice above', 'The ER notice above')
        title, rest = re.sub(r'\n{3,}', '\n\n', body).split('\n', 1)
        write(name, title+'\n\n<!-- R026-ER-CURRENT -->\n'+content+'\n<!-- /R026-ER-CURRENT -->\n'+rest)
    for name, content in [('CHANGELOG.zh-CN.md', zh), ('CHANGELOG.md', en)]:
        body = (ROOT/name).read_text(encoding='utf-8')
        body = re.sub(r'(?m)^- R026(?:新增| adds )\[ER-001[—–]050\].*\n', '', body)
        title, rest = body.split('\n', 1); write(name, title+'\n\n- '+content+'\n'+rest)
    for name in ['dev/repository/USAGE.md', 'dev/repository/SYSTEM_DESIGN.md']:
        section(name, marker, zh.replace('(docs/', '(../../docs/')+'\n\n'+en.replace('(docs/', '(../../docs/'))
    section('dev/progress/README.md', marker, zh.replace('(docs/', '(../../docs/'))
    for name in ['dev/src/client/node/README.md', 'dev/src/client/README.md', 'dev/src/client/crypto-gateway/README.md']:
        prefix = '(../../../docs/' if name == 'dev/src/client/README.md' else '(../../../../docs/'
        section(name, marker, zh.replace('(docs/', prefix)+'\n\n'+en.replace('(docs/', prefix))
    for name in ['docs/技术开发/00-技术文档导航与实施基线.md', 'docs/技术开发/08-端到端加密协议与密钥实现设计.md']:
        section(name, marker, zh.replace('(docs/技术开发/', '('))
    for name in ['AGENTS.md', 'dev/AGENTS.md']:
        section(name, marker, zh.replace('(docs/', '(../docs/' if name.startswith('dev/') else '(docs/')+
            '\n\n最新唯一增量整理器为'+TOOL+'；禁止重跑旧IC/KE/PD/SI/DC/DA/RA/RT/RF/CK/GF/PH/PT/MP/MG/MS/WS/CH/CE/MW/RN整理器覆盖当前ER状态。'
            '遵守'+RULES+'：同一R026全56/222生产接线及测试源码齐备前，不编译/测试/核验/迁移或启动DB、密码库、内核、模型、GPU、手机、APK或浏览器；源码保存不拆轮。'
            '001—022平台双份SQL/source、已登记端侧DDL/source和固定第三方JCS源/许可证原字节冻结，平台只追加023+。'
            '不调度Docker/WSL/QEMU/VM/模拟器或子代理；DEV55延后、DEV32无真实证明关闭；浏览器用后关闭不用页。')


def main():
    migration, counts = register_source(); tests = inventory(); text = detail(counts)
    integration = {'status': 'SOURCE_ONLY_UNVERIFIED', 'requirements': 'ER001—050', 'annex': ANNEX, 'source_contract': CONTRACT,
        'public_only_verification_and_account_device_directory_source_wired': True, 'native_original_publication_and_five_metadata_cli_source_wired': True,
        'independent_trust_or_content_possession_or_execution_proven': False, 'platform_e2e_grant_blind_store_private_sdk_host_jni_complete': False,
        'migration_executed': False, 'compiled_or_executed': False, 'new_media_captured': False, 'full_development_round_complete': False,
        'test_definition_count': len(tests), **counts,
        'remaining': ['平台E2E原上下文/授权/无钥持久化/实际网络补传与全局背压重排', '本机SDK/实际Host私有推理/LOCAL_SEGMENTED_1/Android JNI/媒体',
                      '独立信任和本机配对UI/原签收/可信usage/资金与真实资格', '过期或丢钥显式恢复/未知文件残留恢复、实际固定库和物理测试',
                      '全56/222其余生产接线及测试源码齐备后集中验证；DEV55延后/DEV32无证明关闭']}
    dump(CONTRACT, {'profile_id': 'P11_ACCOUNT_ENDPOINT_DIRECTORY_SOURCE_1', 'requirements': 'ER-001—050',
        'authority': ANNEX, 'source_refs': REFS, 'development_policy': RULES, **integration,
        'directory_profile': 'P11_ACCOUNT_ENDPOINT_DIRECTORY_1', 'bundle_profile': 'P11_ENDPOINT_PUBLIC_1',
        'rotation_profile': 'P11_ENDPOINT_ROTATION_1', 'source_migration': migration,
        'public_only': {'content_private_keys_or_native_cipher_dependency': False, 'signed_wire_format_changed': False,
                        'out_of_band_pin_or_content_key_possession_proven': False, 'recipient_trust_evidence_id': None},
        'limits_not_measurements': {'account_endpoints_per_role': 32, 'versions_per_endpoint': 32, 'bundle_bytes': 8192,
                                    'transition_bytes': 12000, 'page_size': 2, 'reply_bytes': 65536, 'maximum_validity_days': 90},
        'states': ['ACTIVE', 'RETIRED', 'REVOKED'], 'time_projection': 'EXPIRED; exact signed nanoseconds checked in Go, SQL projection floored to microseconds',
        'revocation_scope': 'Exactly named version; does not cascade to successors or erase/join local key uses.',
        'recovery': 'Persist original command before HTTP; retain unknown; replay original bytes/key with fresh device proof nonce; no replacement keys or automatic pending abandonment.',
        'explicit_expired_or_lost_key_recovery_complete': False, 'http_operations': [op[0] for op in OPERATIONS],
        'metadata_cli': ['directory-publish', 'directory-revoke', 'directory-get', 'directory-list', 'directory-recover'],
        'test_definitions': tests, 'full_scope_tasks': 56, 'full_scope_steps': 222, 'test_admission': 'DENIED_UNTIL_ALL_IN_SCOPE_CODE_COMPLETE'})
    plan, stage, status = [load(n) for n in ['dev/progress/code-first-plan.json', 'dev/progress/R026-code-stage.json', 'dev/progress/implementation-status.json']]
    for record in (plan, stage): record['account_endpoint_directory_source_integration'] = deepcopy(integration)
    for task in plan['tasks']:
        if task['id'] in TASKS:
            task['code_status'] = 'IN_PROGRESS'; task['account_endpoint_directory_source_limit'] = text
            for step in task['steps']:
                if step['id'] in STEPS:
                    step['code_status'] = 'IN_PROGRESS'; step['implementation_refs'] = sorted(set(step.get('implementation_refs', [])+REFS))
    for task in status['tasks']:
        if task['id'] in TASKS:
            task['implementation_status'] = 'IN_PROGRESS'; task['account_endpoint_directory_source_limit'] = text
            task['current_source_refs'] = sorted(set(task.get('current_source_refs', [])+REFS)); task['current_validation_status'] = 'NOT_EXECUTED'
    status['activity_source'] = '2026-11-07 CODE_FIRST：ER001—050 / IMP136—142；'+text
    for number, title, state in ITEMS:
        entry = {'id': 'IMP-R026-'+number, 'title': title, 'status': state}
        stage['implemented_items'] = [old for old in stage['implemented_items'] if old['id'] != entry['id']]+[entry]
    stage.update({'complete': False, 'validation_started_after_user_policy': False, 'tasks': deepcopy(status['tasks']),
                  'account_endpoint_directory_test_definitions': {'tests': tests, 'ids': [t['id'] for t in tests], 'status': 'SOURCE_ONLY_NOT_EXECUTED'}})
    stage['source_files'] = sorted(set(stage.get('source_files', [])+REFS))
    for name, value in [('dev/progress/code-first-plan.json', plan), ('dev/progress/R026-code-stage.json', stage), ('dev/progress/implementation-status.json', status)]: dump(name, value)
    progress(plan, stage, status, tests, text); documentation(tests, counts, text)
    print(json.dumps({'source_document_generation': 'ER001—050 / IMP136—142', 'unexecuted_test_definitions': len(tests),
        **counts, 'compiler_test_validator_migration_runtime': 'NOT_RUN', 'all_tasks_open': len(plan['tasks']),
        'all_steps_open': sum(len(t['steps']) for t in plan['tasks'])}, ensure_ascii=False))


if __name__ == '__main__':
    main()
