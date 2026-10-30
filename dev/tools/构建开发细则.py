"""从项目权威契约和人工拆解源生成开发细则；不运行产品或设备。"""
from __future__ import annotations

import hashlib
import importlib.util
import json
import os
from pathlib import Path
import sys

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[2]
DEV = ROOT / 'dev'
TECH = ROOT / 'docs/技术开发/contracts'
SOURCE = DEV / 'planning/开发细则源数据.py'
PLAN_PATH = 'dev/contracts/development-plan-v1.0.json'
MASTER = 'dev/开发细则总表.md'
DOCUMENTS = [
    ('DOC-00', '开发基线与编号规范', 'dev/docs/00-开发基线与编号规范.md'),
    ('DOC-01', '模块划分与职责边界', 'dev/docs/01-模块划分与职责边界.md'),
    ('DOC-02', '表格化需求约束', 'dev/docs/02-表格化需求约束.md'),
    ('DOC-03', '开发细则总表', MASTER),
    ('DOC-04', '接口数据与需求追踪', 'dev/docs/04-接口数据与需求追踪.md'),
    ('DOC-05', '开发依赖与阶段清单', 'dev/docs/05-开发依赖与阶段清单.md'),
    ('DOC-06', '验收交付与证据规则', 'dev/docs/06-验收交付与证据规则.md'),
    ('DOC-07', '路径迁移与规范来源', 'dev/docs/07-路径迁移与规范来源.md'),
]
PATHS = [
    ('PTH-001', 'dev/', '开发根目录；新代码、测试、工作流及开发工具统一入口'),
    ('PTH-002', 'dev/src/', '产品工程；单 Go 模块，真实源码与验证进展见 dev/progress'),
    ('PTH-003', 'dev/src/api/', '控制面入口和 HTTP 适配，不承载共享领域真相'),
    ('PTH-004', 'dev/src/internal/', '平台应用服务、领域规则及持久化端口'),
    ('PTH-005', 'dev/src/scheduler/', '原生 worker 组合入口，复用共享应用服务'),
    ('PTH-006', 'dev/src/gateway/', '明文平台入口、共用转发内核、平台盲转'),
    ('PTH-007', 'dev/src/client/', 'Windows/Linux 原生供给端、Android LMA、买卖加密网关'),
    ('PTH-008', 'dev/src/miniapp/', 'React + Taro 支付宝小程序'),
    ('PTH-009', 'dev/src/web/', 'React + Vite Web'),
    ('PTH-010', 'dev/src/frontend/packages/', '纯 TypeScript 领域、DTO、验证和端口共享'),
    ('PTH-011', 'dev/src/shared/', 'Go 共享类型、端口、金额、时钟、ID，不放独立业务账本'),
    ('PTH-012', 'dev/tests/', '跨端测试入口、证据和脱敏夹具；Go 测试按单模块规则随 dev/src 包存放'),
    ('PTH-013', 'dev/workflows/', '受审配方、参数 Schema、模型/节点/版本清单'),
    ('PTH-014', 'dev/tools/', '新原生开发工具和细则构建/核验'),
    ('PTH-015', 'dev/planning/', '人工维护约束与任务子项源数据'),
    ('PTH-016', 'dev/docs/', '生成的编号、约束、依赖、追踪、交付规则'),
    ('PTH-017', 'dev/modules/', '生成的 20 模块实施细则'),
    ('PTH-018', 'dev/contracts/', '生成的机器开发清单，不替代原 OpenAPI/SQL'),
    ('PTH-019', 'dev/评审/', '开发清单结构核验；不是产品测试结果'),
    ('PTH-020', 'docs/', '既有需求、技术设计、SQL、OpenAPI 及来源，保持权威'),
    ('PTH-021', 'scripts/', '既有白皮书/技术附件构建和核验，不新增产品服务'),
    ('PTH-022', 'runtime/', '项目共享模型、引擎、密文工作区、数据库、缓存、临时与工具依赖'),
]


def sha(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def read_json(name: str) -> dict:
    return json.loads((TECH / (name + '-v1.0.json')).read_text(encoding='utf-8'))


def source_data():
    spec = importlib.util.spec_from_file_location('development_plan_source', SOURCE)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def rows(raw: str) -> list[list[str]]:
    return [[v.strip() for v in line.split('|')] for line in raw.strip().splitlines() if line.strip()]


def module_path(m: dict) -> str:
    return f"dev/modules/{m['id']}-{m['name']}.md"


def link(current: str, target: str, label: str, anchor: str | None = None) -> str:
    value = Path(os.path.relpath(ROOT / target, (ROOT / current).parent)).as_posix()
    if anchor:
        value += '#' + anchor
    return f'[{label}]({value})'


def table(headers, records) -> str:
    def cell(v):
        return str(v if v is not None else '—').replace('|', '&#124;').replace('\n', '<br>')
    lines = ['| ' + ' | '.join(map(cell, headers)) + ' |', '| ' + ' | '.join('---' for _ in headers) + ' |']
    lines.extend('| ' + ' | '.join(map(cell, row)) + ' |' for row in records)
    return '\n'.join(lines) + '\n'


def header(title: str, ident: str, date: str) -> str:
    return (f'# {title}\n\nP11｜开发细则 v1.0｜{ident}｜{date}｜需求 v3.0 / 技术 v1.0。\n\n'
            '> 由 dev/planning/开发细则源数据.py 与既有技术契约生成；修改源后重建，不手改本文件。'
            '本套为冻结设计拆解：55 任务 DESIGN、DEV-55 DEFERRED；真实源码进展见 dev/progress/README.md，完整产品/设备/支付/密码/性能验收不能据此记为通过。\n\n')


def topology(tasks: list[dict]) -> list[list[str]]:
    pending = {t['id']: set(t['dependencies']) for t in tasks}
    done, waves = set(), []
    while pending:
        ready = sorted(k for k, deps in pending.items() if deps <= done)
        if not ready:
            raise ValueError('任务依赖缺失或循环：' + ','.join(pending))
        waves.append(ready)
        done.update(ready)
        for k in ready:
            del pending[k]
    return waves


def load_plan() -> dict:
    src = source_data()
    modules = read_json('technical-module-catalog')['modules']
    tasks = read_json('development-backlog')['tasks']
    requirements = read_json('requirement-technical-map')['requirements']
    operations = read_json('api-implementation-map')['operations']
    database = read_json('database-catalog')
    constraints = []
    for number, scope, rule, failure, evidence, source in rows(src.CONSTRAINT_ROWS):
        constraints.append({'id': 'CON-' + number, 'module_ids': [m['id'] for m in modules] if scope == 'ALL' else ['MOD-' + n for n in scope.split()],
                            'rule': rule, 'failure_action': failure, 'required_evidence': evidence, 'source_document': source})
    details = {f'MOD-{int(r[0]):02d}': r[1:] for r in rows(src.MODULE_DETAILS)}
    for m in modules:
        purpose, inputs, outputs, forbidden = details[m['id']]
        m.update({'purpose': purpose, 'inputs': inputs, 'outputs': outputs, 'forbidden': forbidden, 'document': module_path(m),
                  'task_ids': [t['id'] for t in tasks if t['module_id'] == m['id']],
                  'constraint_ids': [c['id'] for c in constraints if m['id'] in c['module_ids']]})
    task_map = {t['id']: t for t in tasks}
    for t in tasks:
        t['steps'] = []
        t['constraint_ids'] = next(m['constraint_ids'] for m in modules if m['id'] == t['module_id'])
        t['operation_ids'] = [o['operation_id'] for o in operations if o['task_id'] == t['id']]
        t['conditional_branch'] = t['id'] == 'DEV-32'
    for number, title, detail, evidence in rows(src.STEP_ROWS):
        task = task_map[f'DEV-{int(number):02d}']
        task['steps'].append({'id': task['id'] + f".{len(task['steps']) + 1:02d}", 'title': title, 'detail': detail,
                              'required_evidence': evidence, 'development_status': task['development_status'],
                              'test_status': 'NOT_EXECUTED', 'evidence_refs': []})
    milestones = []
    for ident, name, numbers, exit_condition in src.MILESTONES:
        ids = [f'DEV-{n:02d}' for n in numbers]
        milestones.append({'id': ident, 'name': name, 'task_ids': ids, 'exit_condition': exit_condition,
                           'conditional': ident == 'MS-07', 'deferred': ident == 'MS-DEFER'})
        for task_id in ids:
            task_map[task_id]['milestone_id'] = ident
    numbering = json.loads((DEV / 'planning/编号登记-v1.0.json').read_text(encoding='utf-8'))
    if set(numbering['operations']) != {o['operation_id'] for o in operations} or set(numbering['tables']) != {t['name'] for t in database['tables']}:
        raise ValueError('权威操作/表发生变化，须先显式维护编号登记，禁止自动重排')
    for operation in operations:
        operation['id'] = numbering['operations'][operation['operation_id']]
    operations.sort(key=lambda o: o['id'])
    tables = []
    for index, t in enumerate(sorted(database['tables'], key=lambda t: t['name']), 1):
        owners = [m['id'] for m in modules if t['name'] in m['authoritative_tables']]
        if len(owners) != 1:
            raise ValueError(f"表 {t['name']} 必须有唯一模块 owner：{owners}")
        tables.append({'id': numbering['tables'][t['name']], 'name': t['name'], 'schema': t['schema'], 'module_id': owners[0],
                       'column_count': len(t['columns']), 'foreign_key_count': sum(f['table'] == t['name'] for f in database['foreign_keys']),
                       'source_document': t['source'], 'runtime_validation': 'NOT_EXECUTED'})
    source_paths = {'dev/AGENTS.md', 'AGENTS.md', 'dev/planning/编号登记-v1.0.json', SOURCE.relative_to(ROOT).as_posix(), Path(__file__).relative_to(ROOT).as_posix(),
                    'scripts/技术开发数据.py', 'scripts/供给端内核数据.py', 'scripts/端对端加密数据.py',
                    'docs/需求推进/NB-1.0/需求增量登记-NB-R1.json',
                    'docs/技术开发/database/schema-draft.sql', 'docs/白皮书/contracts/openapi-control-v3.0.json',
                    'docs/技术开发/02-工程结构开发环境与编码规范.md', 'docs/技术开发/附录-dev开发路径与细则索引.md'}
    source_paths.update(c['source_document'] for c in constraints)
    source_paths.update(m['source_document'] for m in modules)
    source_paths.update(t['source'] for t in database['tables'])
    source_paths.update(m['source'] for m in database['forward_migrations'])
    source_paths.update({'dev/src/api/internal/http/routes.json','docs/技术开发/附录-身份登录与HTTP交易实施约束.md','docs/技术开发/附录-卖家申请与挂牌版本实施约束.md'})
    source_paths.update(p for t in tasks for p in t['source_documents'])
    source_paths.update('docs/技术开发/contracts/' + n + '-v1.0.json' for n in
                        ('technical-module-catalog', 'development-backlog', 'requirement-technical-map', 'api-implementation-map', 'database-catalog'))
    source_manifest = [{'path': p, 'sha256': sha((ROOT / p).read_bytes())} for p in sorted(source_paths)]
    return {'project': 'P11', 'version': src.VERSION, 'date': src.DATE, 'development_root': 'dev/',
            'status': 'DESIGN_UNIMPLEMENTED', 'product_test_status': 'NOT_EXECUTED', 'actual_owner': None, 'estimate': None,
            'documents': [{'id': i, 'title': n, 'path': p} for i, n, p in DOCUMENTS],
            'paths': [{'id': i, 'path': p, 'purpose': d} for i, p, d in PATHS],
            'modules': modules, 'constraints': constraints, 'tasks': tasks, 'operations': operations, 'tables': tables,
            'requirements': requirements, 'milestones': milestones,
            'delivery_checks': [{'id': i, 'name': n, 'rule': d} for i, n, d in src.DELIVERY_CHECKS],
            'topological_waves': topology(tasks), 'source_manifest': source_manifest,
            'summary': {'modules': len(modules), 'tasks': len(tasks), 'steps': sum(len(t['steps']) for t in tasks),
                        'constraints': len(constraints), 'control_operations': len(operations), 'tables': len(tables),
                        'requirements': len(requirements), 'acceptance_cases': len({r['acceptance_id'] for r in requirements}),
                        'current_tasks': sum(t['development_status'] == 'DESIGN' for t in tasks),
                        'deferred_tasks': sum(t['development_status'] == 'DEFERRED' for t in tasks)}}


def render_outputs() -> dict[str, str]:
    p = load_plan()
    nb = json.loads((ROOT / 'docs/需求推进/NB-1.0/需求增量登记-NB-R1.json').read_text(encoding='utf-8'))
    outputs = {}
    mods = {m['id']: m for m in p['modules']}
    tasks = {t['id']: t for t in p['tasks']}
    date = p['date']
    def ml(path, mid):
        return link(path, mods[mid]['document'], mid)
    def tl(path, tid):
        return link(path, MASTER, tid, tid)
    def cls(path, ids):
        return '、'.join(link(path, DOCUMENTS[2][2], i, i) for i in ids)
    def task_content(path, t):
        m = mods[t['module_id']]
        text = f"<a id=\"{t['id']}\"></a>\n\n### {t['id']} {t['title']}\n\n"
        text += table(['字段', '细则'], [
            ['模块/岗位', f"{ml(path, m['id'])} / {t['owner_role']}；实际负责人未分配"],
            ['计划路径', '；'.join('`' + v + '`' for v in m['planned_packages'])],
            ['范围/状态', f"{t['current_phase_scope']} / {t['development_status']} / NOT_EXECUTED" + ('；硬件增强条件分支' if t['conditional_branch'] else '')],
            ['依赖', '、'.join(tl(path, i) for i in t['dependencies']) or '无'],
            ['需求/主验收', '、'.join(t['requirement_ids']) + '；' + '、'.join(t['acceptance_ids'])],
            ['实施目标', t['implementation']], ['原完成证据', t['done_evidence']],
            ['门槛', '、'.join(t['gates']) or '见任务及组合资格；无默认通过'],
            ['控制操作', '、'.join(t['operation_ids']) or '无独立控制操作；通过端口/协议/测试交付'],
            ['需求约束', cls(path, t['constraint_ids'])],
            ['统一交付', '适用 CHK-01—08；不适用项注明理由；证据规则见 DOC-06'],
        ]) + '\n'
        text += table(['子项编号', '开发项', '实现要求与失败边界', '交付/证据'],
                      [[s['id'], s['title'], s['detail'], s['required_evidence']] for s in t['steps']])
        if t.get('branch_gates'):
            text += '\n分支门槛：`' + json.dumps(t['branch_gates'], ensure_ascii=False) + '`。兼容分支不能共用隐私资格。\n'
        related = [r['id'] for r in nb['requirements'] if t['id'] in r['tasks']]
        if related:
            text += '\nNB-R1需求增量：' + '、'.join(related) + '；' + link(path, 'docs/需求推进/NB-1.0/06-需求追踪与验收矩阵.md', '正反向验收及契约缺口') + '。源码实施见进度登记，子场景仍未执行。\n'
        text += '\n规范来源：' + '；'.join(link(path, f, Path(f).name) for f in t['source_documents']) + '。\n\n'
        return text

    # DOC-00: baseline and numbering authority.
    path = DOCUMENTS[0][2]
    text = header('开发基线与编号规范', 'DOC-00', date)
    text += '## 00.1 开发范围\n\n'
    text += ('开发根目录 `E:\\dev\\sandbox\\支付宝黑客松\\算力共享平台\\dev`。所有命令以项目根目录运行，代码/测试/工作流/新工具落在 dev；规范在 docs，运行数据在 runtime。'
             '本文件记录设计基线，源码实现与原生验证进展另在 dev/progress 登记，不用设计状态覆盖真实实现记录。Go 测试遵从单模块结构随 dev/src 包存放，跨端入口及证据仍在 dev/tests。\n\n'
             'C2C 平台撮合真实个人卖家；端对端加密、受控 GPU 租赁、明文三模式保留。小程序是主交易入口，标准模型 SDK 经已确认路由调用；所有者知道组件、资源与收益，但默认不展示买方正文。\n\n')
    text += '## 00.2 技术选型约束\n\n' + table(['项号', '层', '当前路线', '冻结/验证要求'], [
        ['00.2.01', '小程序', 'React + TypeScript + Taro；支付宝优先', '原生支付宝真机支付/生命周期；不借 Web 环境结论'],
        ['00.2.02', 'Web', 'React + TypeScript + Vite', '共享纯 TS；DOM、支付和设备 API 分端'],
        ['00.2.03', '服务端', 'Go 模块化单体；单 dev/src/go.mod；原生 worker', 'HTTP 仅适配，共享规则在 dev/src/internal；版本待锁'],
        ['00.2.04', '持久化', 'PostgreSQL 17 + pgx/sqlc；客户端 SQLite', 'SQL 语法树检查不是 PG 迁移验证；不引入容器'],
        ['00.2.05', '网关', 'Sub2API 类可复用核心 + 平台明文入口/盲转入口', '冻结来源 commit/许可；删除卖家池轮换/独立钱包/正文遥测'],
        ['00.2.06', '桌面 LLM', '隐私：llama.cpp C API/C++ worker；兼容：lms CLI + llmster + runtime', '逐分支冻结、替换、合格验证；LM 不借隐私档证据'],
        ['00.2.07', '桌面媒体', '完整 ComfyUI 原生可替换环境；私有管道、有限白名单', '图/视频/音频/3D 分别资格；依赖与节点导入前验证'],
        ['00.2.08', 'Android', '固定 LMA 派生；Java 基座/Kotlin 互操作、内部 JNI', '固定 commit 5d9e81d59273f2a656763ea3ee3401b2846a18cc；签名 APK 升级/真机'],
        ['00.2.09', '加密', 'libsodium sealed box + secretstream + 独立 Ed25519 签名作为 PoC 路线', '确切版本、suite、跨语言向量及独立评审未冻结'],
        ['00.2.10', '数据协议', 'OpenAI Chat Completions / Responses、Anthropic Messages 的已发布子集', 'JSON_COMPAT、BINARY_STREAM_1、LOCAL_SEGMENTED_1 分层，媒体扩展不冒充厂商原生能力'],
        ['00.2.11', '支付', '按赛事收单材料和实际获准 seller/application/service 接入', '资料不是渠道授权；原单验付/退款/到账均需真实证据'],
        ['00.2.12', '本轮排除', '部署/公网 HTTPS/运维/备份建设/可观测性', 'DEV-55 延后；保留交易账证、取消清理与故障恢复'],
    ]) + '\n'
    text += '## 00.3 编号与稳定性\n\n' + table(['编号', '范围', '规则'], [
        ['DOC-00—07', '8 份细则主文档', 'DOC-03 为 dev/开发细则总表.md；其他按章节文件命名'],
        ['MOD-01—20', '20 模块', '沿用技术目录；一个模块一个细则文件'],
        ['DEV-01—56', '56 任务', '沿用原任务；DEV-55 保留 DEFERRED；DEV-32 为条件分支'],
        ['DEV-xx.yy', '222 开发子项', '原任务 + 两位子序号；继承任务状态/F/T/依赖；不新增顶层任务'],
        ['P11-F01—80 / P11-T01—80', '80 功能 / 80 同号主验收', '保留原优先级和 CURRENT/PARTIAL/DEFERRED，不能用技术项替代主验收'],
        ['CON-001—047', '47 条需求约束', '人工源维护；CON-047承接NB-R1；含失败动作、证据及来源，适用模块显式登记'],
        ['INT-001—093', '93 控制操作的索引', '首次按 operationId 排序登记；以后按人工编号登记读取，operationId 才是 API 标识'],
        ['DATA-001—065', '65 张表的索引', '首次按表名排序登记；以后按人工编号登记读取；表名/DDL 是权威标识；唯一 owner'],
        ['MS-01—08 / MS-DEFER', '阶段/条件分支/延后', '组织工作用，不替代任务依赖，不表示连续工期或承诺日期'],
        ['CHK-01—08', '统一交付检查', '按任务适用，N/A 须说明依据，未执行不能记为通过'],
        ['VFY-nnn', '开发文档结构核验', '核验器按检查项顺序编号，只证明文档结构，不替代 T/LP/PE/E2E 产品验收'],
        ['PTH-001—022', '路径登记', '开发路径及保留的规范/运行路径逐项登记'],
        ['POC / E / SDK / LP / PE / E2E / K / FW', '既有专项/门槛编号', '沿用原文档和机器契约；不是自动生成的成功记录'],
    ]) + '\n'
    text += ('本基线发布后不回收已分配编号；删除项保留废止记录，新项显式登记。INT/DATA 使用 dev/planning/编号登记-v1.0.json 的固定映射，后续新增不能静默重排；扩展时同步编号登记与核验基线。'
             '负责人仅记录岗位，实际人员/工期均待分配；不得把此清单当成已排期承诺。\n\n')
    text += '## 00.4 使用顺序\n\n'
    text += '\n'.join(f'{i}. {link(path, target, ident + " " + name)}' for i, (ident, name, target) in enumerate(DOCUMENTS[1:], 1)) + '\n\n'
    text += '约束冲突处理：遵从当前用户指令与项目 AGENTS；业务以需求源、工程以专项源和契约为准，细则仅作可追踪拆解。发现冲突先记录并修正源，不以新表格覆盖原合同。\n'
    outputs[path] = text

    # DOC-01: all module boundaries.
    path = DOCUMENTS[1][2]
    text = header('模块划分与职责边界', 'DOC-01', date)
    text += '## 01.1 模块责任总表\n\n'+table(['模块/细则', '岗位', '职责', '输入', '输出', '禁止越界'], [
        [link(path, m['document'], m['id'] + ' ' + m['name']), m['owner_role'], m['purpose'], m['inputs'], m['outputs'], m['forbidden']] for m in p['modules']]) + '\n'
    text += '## 01.2 依赖、计划路径与表归属\n\n'+table(['模块', '应用服务/端口', '依赖模块', '计划路径', '权威表', '任务'], [
        [m['id'], '、'.join(m['application_services']), '、'.join(ml(path, v) for v in m['dependencies']) or '无', '；'.join('`'+x+'`' for x in m['planned_packages']), '、'.join(m['authoritative_tables']) or '无服务端权威表；客户端数据见专项', '、'.join(tl(path, t) for t in m['task_ids'])] for m in p['modules']]) + '\n'
    text += ('模块依赖表示业务协作/端口关系，不等于全部任务按模块编号顺序实现。公共应用服务在 dev/src/internal；API 与 worker 通过这些服务交互，禁止跨模块私写权威表。'
             '本地 SQLite/inbox/钥缓存由客户端负责，不能成为平台资金/设备归属的第二权威。无独立控制操作的模块通过端口、标准数据面、原生程序或测试交付。\n')
    outputs[path] = text

    # DOC-02: constraint master.
    path = DOCUMENTS[2][2]
    text = header('表格化需求约束', 'DOC-02', date)
    text += ('每条约束均是开发/评审输入。范围跨模块时，各方都必须满足，不能把责任推给 UI、SDK 或底层网关。失败动作描述应达到的实现行为，不代表现已实现。\n\n')
    text += '## 02.1 全部需求约束\n\n'
    text += table(['约束编号', '适用模块', '硬约束', '不满足时的处理', '验收证据', '规范源'], [
        [f'<a id="{c["id"]}"></a>{c["id"]}', '、'.join(c['module_ids']) if len(c['module_ids']) != 20 else '全部 20 模块', c['rule'], c['failure_action'], c['required_evidence'], link(path,c['source_document'],Path(c['source_document']).name)] for c in p['constraints']])
    text += '\n## 02.2 量化边界与待实测预算\n\n'
    text += ('以下从既有技术08及低开销方案摘取；均是设计上限/验收目标，尚无实测结论。额外内存以同机同版本裸引擎、相同模型/精度/输入/配方为基线，'
             '包含桥接/密码/缓存/序列化，不只统计 Go heap；模型和张量也比较峰值差。\n\n')
    text += table(['项号', '约束/预算', '目标值', '适用 CON / 失败处理'], [
        ['02.2.01','标准 JSON / 媒体','JSON 编码后含 base64 ≤2MiB；媒体明文 ≤64MiB、低开销密文对象 ≤66MiB','CON-033/037；先验大小再分配，配方可更低'],
        ['02.2.02','二进制流','流头 ≤16KiB；DATA 正文 ≤65528B；线记录 ≤65565B；聚合 ≤8ms','CON-025/026/033；独立协商/Schema/向量未完前不开启'],
        ['02.2.03','流缓冲/重排','单流各阶段共用 ≤4MiB；重排同时满足 ≤32 条且 ≤2MiB','CON-033；背压，不允许每阶段另拿一份额度'],
        ['02.2.04','提供方桌面新增内存','后台空闲 ≤64MiB，单 GPU 活动 ≤96MiB','CON-034；无设置/预览页，页面开启另列，不虚报总量'],
        ['02.2.05','Android 新增内存','后台空闲 ≤12MiB，活动 ≤24MiB','CON-031/034；不同真机分别实测 PSS/RSS'],
        ['02.2.06','买方网关新增内存','后台空闲 ≤48MiB，单流活动 ≤80MiB，受控内容缓冲 ≤16MiB','CON-034；SDK 自身全量解析额外开销单列'],
        ['02.2.07','提供方内容总缓冲','桌面单活动资源 ≤16MiB、同进程总池 ≤32MiB；Android ≤8MiB','CON-033/034；多卡不无界叠加'],
        ['02.2.08','本地已完成对象缓存','1MiB 只读段；桌面最多 4 段/Android 2 段 LRU；加密 manifest ≤64KiB','CON-027/034；段预算计入总池，认证前不交字节'],
        ['02.2.09','磁盘 spool','每活动资源默认 256MiB，另受设备总盘预算；先预留输入转换/输出/重传空间','CON-033/034；不足拒准入或暂停新单，不转 RAM'],
        ['02.2.10','暖机稳态性能差','桌面吞吐回退 ≤3%、端到端 p95 增加 ≤5%；Android 端到端 p95 增加 ≤8%','CON-034；不换模型/精度/画质，不降级隐私'],
        ['02.2.11','首包/控制延迟','短文本新增首包 p95：桌面 ≤20ms、Android ≤30ms；服务到 worker 取消/续期分发 p95 ≤50ms','CON-021/022/034；控制分发不等于 GPU 瞬时停算'],
        ['02.2.12','并发/待交付','每 GPU/手机一活动执行；额外待交付对象桌面 ≤2、Android ≤1','CON-019/022/034；实际停算清理后释放，交付不占新执行权'],
        ['02.2.13','验证方式','固定环境先暖机，裸执行/无密码包裹测试基线/完整隐私链路，各分支至少 30 次 A/B','CON-034/043；记 p50/p95、峰值内存/VRAM/CPU/IO/网络'],
        ['02.2.14','零新增 GPU 干扰','加密 GPU 任务 0、额外模型副本 0、全片 RAM 盘 0','CON-007/034；CPU 加密和有界流式；超标先优化或不发布 SKU'],
    ]) + '\n'
    text += '完整定义见'+link(path,'docs/技术开发/08-端到端加密协议与密钥实现设计.md','技术08')+'和'+link(path,'docs/技术开发/附录-低开销隐私执行完整方案.md','低开销方案第8节')+'；所有数字保持 D 预算状态。\n'
    outputs[path] = text

    # DOC-03: one readable master, all custom steps.
    path = MASTER
    text = header('开发细则总表', 'DOC-03', date)
    text += ('开发根目录：`E:\\dev\\sandbox\\支付宝黑客松\\算力共享平台\\dev`。共 20 模块、56 任务、222 子项、47 约束；93 个控制操作、65 张表、80 功能/80 主验收全部关联。'
             '本表覆盖所有现有顶层任务，不实现软件、不分配虚构负责人或工期。\n\n')
    text += table(['文档', '入口'], [[i + ' ' + name, link(path, target, name)] for i, name, target in DOCUMENTS if target != path]) + '\n'
    text += '## 03.1 模块索引\n\n'+table(['模块', '职责与细则', '任务', '子项数'], [
        [m['id'], link(path,m['document'],m['name']), '、'.join(tl(path,t) for t in m['task_ids']), sum(len(tasks[t]['steps']) for t in m['task_ids'])] for m in p['modules']])+'\n'
    text += '## 03.2 任务索引\n\n'+table(['任务', '名称', '模块', '阶段', '直接依赖', '状态/范围'], [
        [tl(path,t['id']), t['title'], t['module_id'], t['milestone_id'], '、'.join(t['dependencies']) or '无', t['development_status']+' / '+t['current_phase_scope']+('；条件分支' if t['conditional_branch'] else '')] for t in p['tasks']])+'\n'
    text += '## 03.3 逐任务开发细则\n\n'
    for t in p['tasks']:
        text += task_content(path,t)
    outputs[path] = text

    # Per-module implementation instructions, interfaces and storage owner.
    for m in p['modules']:
        path = m['document']
        text = header(m['id']+' '+m['name']+'开发细则', m['id'], date)
        text += link(path,MASTER,'返回开发总表')+'；'+link(path,DOCUMENTS[1][2],'模块边界总表')+'。\n\n'
        text += '## 1. 职责与边界\n\n'+table(['字段', '约定'],[
            ['职责',m['purpose']],['输入',m['inputs']],['输出',m['outputs']],['禁止承担',m['forbidden']],
            ['岗位/人员',m['owner_role']+'；实际负责人/工期未分配'],['计划路径','；'.join(m['planned_packages'])],
            ['协作模块','、'.join(ml(path,i) for i in m['dependencies']) or '无'],['应用服务','、'.join(m['application_services'])],
            ['权威表','、'.join(m['authoritative_tables']) or '无服务端权威表；不得另建平台资金/设备归属真相'],
            ['适用约束',cls(path,m['constraint_ids'])],['核心规范',link(path,m['source_document'],Path(m['source_document']).name)]])+'\n'
        text += '## 2. 控制接口归属\n\n'
        ops=[o for o in p['operations'] if o['module_id']==m['id']]
        text += (table(['索引', 'operationId', '方法/路径', '任务', 'scope', '事务/幂等'],[[o['id'],o['operation_id'],o['method']+' '+o['path'],tl(path,o['task_id']),', '.join(o['required_scopes']),o['transaction']+' / '+('必需稳定键' if o['idempotency_header_required'] else '见权威契约')] for o in ops]) if ops else '无独立控制操作。按端口/标准协议/本地命令交付，不据此新增未经设计的公开接口。\n')
        text += '\n'+link(path,DOCUMENTS[4][2],'完整控制面、数据表与需求追踪')+'；请求/响应 Schema 和响应码沿用既有 OpenAPI。\n\n'
        text += '## 3. 开发任务与子项\n\n'
        for tid in m['task_ids']:
            text += task_content(path,tasks[tid])
        reqs=[r for r in p['requirements'] if m['id'] in r['module_ids']]
        text += '## 4. 需求与主验收覆盖\n\n'+table(['需求/主验收', '功能', '优先级/范围', '关联任务'],[[r['requirement_id']+' / '+r['acceptance_id'],r['title'],r['priority']+' / '+r['current_phase_scope'],'、'.join(tl(path,t) for t in r['task_ids'])] for r in reqs])+'\n'
        text += '## 5. 完成规则\n\n适用 CHK-01—08，分别提交实现、失败恢复、权限/幂等和真实证据。模块代码完成不意味着对应组合可售；无证据的引擎、许可、隐私档位、支付资格保持关闭。'+link(path,DOCUMENTS[6][2],'验收交付规则')+'。\n'
        outputs[path]=text

    # DOC-04: all 93 operations, 65 tables and 80 F/T pairs.
    path=DOCUMENTS[4][2]
    text=header('接口数据与需求追踪','DOC-04',date)
    text+='## 04.1 控制接口权威与边界\n\n'
    text+=link(path,'docs/白皮书/contracts/openapi-control-v3.0.json','权威控制面 OpenAPI')+'；'+link(path,'docs/技术开发/contracts/api-implementation-map-v1.0.json','完整实施映射')+'。以下 93 项为控制面，不是 OpenAI/Anthropic 数据面新定义；planned_handler 为计划；实际注册子集另见权威映射的 runtime_registration。请求字段、响应码及 canonical hash 保存在权威映射，本表不复制修改第二份 Schema。\n\n'
    text+=table(['编号', 'operationId', '方法/控制路径', '模块/任务', 'scope', '幂等/事务'],[[o['id'],o['operation_id'],o['method']+' '+o['path'],o['module_id']+' / '+tl(path,o['task_id']),', '.join(o['required_scopes']),('稳定键必需；' if o['idempotency_header_required'] else '按契约；')+o['transaction']] for o in p['operations']])+'\n'
    text+='## 04.2 数据面与内部端口\n\n'+table(['项号', '边界', '约定', '负责模块/任务'],[
        ['04.2.01','标准模型入口','OpenAI/Anthropic 已发布子集；绑定路由/卖家/模式/预算；不强制私有请求体字段','MOD-04/09；DEV-11/25/26/27/28'],
        ['04.2.02','明文平台到提供端','Sub2API 类核心转发、终态/取消/usage；不绕过执行租约','MOD-06/07/08/09'],
        ['04.2.03','买方/卖方本机入口','配对/Host/Origin/本地 ACL；内容在端侧转换、加解密、验收','MOD-07/08/10'],
        ['04.2.04','密文通道','JSON_COMPAT 与新增 BINARY_STREAM_1 不混用；后者需协商/独立 Schema/字节向量','MOD-10/11；DEV-29/30/31'],
        ['04.2.05','原生引擎','llama C API 受控管道；Comfy 私有管道；LMA 内部 JNI；仅控制元数据走开放接口','MOD-07/08/09'],
        ['04.2.06','媒体与租赁','受控配方/文件授权/会话 grant；不开放 shell/RDP；不支持类型执行前拒绝','MOD-12/13；DEV-33—37'],
        ['04.2.07','后台应用端口','业务事务+outbox，外部 I/O 在提交后；重入受幂等/围栏约束','MOD-18；DEV-51'],
    ])+'\n'
    text+='协议逐字段/工具/SSE/媒体错误规则见'+link(path,'docs/技术开发/07-统一协议网关与Sub2API改造.md','技术07')+'、'+link(path,'docs/技术开发/附录-协议字段映射与兼容档位.md','兼容档位')+'、'+link(path,'docs/技术开发/08-端到端加密协议与密钥实现设计.md','技术08')+'。尚未发布的二进制能力不得标已支持。\n\n'
    text+='## 04.3 全部数据表及唯一归属\n\n'
    text+=link(path,'docs/技术开发/database/schema-draft.sql','SQL 权威源')+'；'+link(path,'docs/技术开发/database/表结构目录.md','完整字段/约束')+'；'+link(path,'docs/技术开发/database/transaction-patterns.sql','事务模式')+'。下列为实施索引，DDL 尚未在真实 PG 执行。\n\n'
    text+=table(['编号', '权威表', '唯一模块', '字段数', '外键数', '归属任务'],[[t['id'],'p11.'+t['name'],ml(path,t['module_id']),t['column_count'],t['foreign_key_count'],'、'.join(tl(path,i) for i in mods[t['module_id']]['task_ids'])] for t in p['tables']])+'\n'
    text+='## 04.4 全部功能与主验收追踪\n\n保留原优先级及阶段范围；CURRENT 不代表实现，PARTIAL 不自动补齐延后部分。每个功能有唯一同号主验收，跨模块协作与任务多对多。\n\n'
    text+=table(['功能/主验收', '功能名', '优先级/范围', '主模块/协作模块', '开发任务', '控制操作数/表数', '状态'],[[r['requirement_id']+' / '+r['acceptance_id'],r['title'],r['priority']+' / '+r['current_phase_scope'],r['primary_module_id']+' / '+', '.join(r['module_ids']),'、'.join(tl(path,i) for i in r['task_ids']),str(len(r['operation_ids']))+' / '+str(len(r['tables'])),'未实现 / 未执行'] for r in p['requirements']])+'\n'
    text+='完整 task_ids/operation_ids/tables/requirement_dependencies 见'+link(path,PLAN_PATH,'机器开发清单')+'与'+link(path,'docs/技术开发/contracts/requirement-technical-map-v1.0.json','原需求技术映射')+'；需求验收正文以白皮书和 05 主验收为准。\n'
    outputs[path]=text

    # DOC-05: dependency graph and stage list.
    path=DOCUMENTS[5][2]
    text=header('开发依赖与阶段清单','DOC-05',date)
    text+='## 05.1 阶段组织\n\n阶段是协作分组，不是顺序强屏障，也不是工期承诺。任务必须遵守 05.2 的直接依赖；可先以稳定端口和契约实现，但不能用 mock 代替最终真机/资金证据。\n\n'
    text+=table(['阶段', '目标', '任务', '退出条件', '性质'],[[m['id'],m['name'],'、'.join(tl(path,t) for t in m['task_ids']),m['exit_condition'],'延后' if m['deferred'] else ('独立条件分支' if m['conditional'] else '当前开发组织')] for m in p['milestones']])+'\n'
    text+='## 05.2 全任务直接依赖\n\n'+table(['任务', '直接依赖', '依赖任务对应模块', '阶段/状态'],[[tl(path,t['id']),'、'.join(tl(path,i) for i in t['dependencies']) or '无','、'.join(sorted({tasks[i]['module_id'] for i in t['dependencies']})) or '无',t['milestone_id']+' / '+t['development_status']] for t in p['tasks']])+'\n'
    text+='## 05.3 依赖图计算层次\n\n下表由依赖图求拓扑层，不是要求所有任务同批启动；每个任务的前置都满足后才进入。DEV-32/55 即使出现在拓扑层，也不加入基础链统一通过门槛。\n\n'
    text+=table(['层次', '前置满足后可进入的任务'],[[f'05.3.{i:02d}','、'.join(tl(path,v) for v in wave)] for i,wave in enumerate(p['topological_waves'],1)])+'\n'
    text+='## 05.4 外部证据与分支规则\n\n'+table(['项号', '项目', '进入开发', '允许售卖/完成'],[
        ['05.4.01','基础工程/后端/前端','可按契约实现、原生工具验证','模块完成须必要负向和恢复证据，不等于商品可售'],
        ['05.4.02','Windows/Linux 六桌面组合','各 OS × Comfy/LLAMA_NATIVE/LM_COMPAT 分别建立实机计划','合格的 OS/引擎/模型/节点/隐私/支付组合单独开启'],
        ['05.4.03','Android','固定 LMA 本项目派生，真机测试','至少按原规范的不同真机/ABI/后台/热负载证据逐组合'],
        ['05.4.04','加密','先定义 profile/向量/信任/生命周期，再实现','独立评审及本地 LP、端到端 E2E、性能 PE 未通过保持关闭'],
        ['05.4.05','硬件增强 DEV-32','单独评估真实全路径硬件能力','无证明只拒绝 enhanced SKU，不阻基础档与本地实用隐私'],
        ['05.4.06','个人收单/结算','可实现渠道端口与模拟故障','获准实际 seller/service/application，真实验付/退款/到账'],
        ['05.4.07','上游订阅/高阶媒体','许可、语义与 SDK 能力先核对','不能预设订阅可转售；视频/音频/3D 各自资格'],
        ['05.4.08','DEV-55','当前不实现','不得变成当前 55 项的前置；未来显式扩范围再启动'],
    ])+'\n'
    text+='阶段通过需完成对应适用任务/证据，实际负责人、时间、阻碍原因由实施时登记；本次不编造估时和已通过记录。\n'
    outputs[path]=text

    # DOC-06: acceptance and evidence semantics.
    path=DOCUMENTS[6][2]
    text=header('验收交付与证据规则','DOC-06',date)
    text+='## 06.1 每任务交付检查\n\n'+table(['编号', '检查项', '通过条件'],[[c['id'],c['name'],c['rule']] for c in p['delivery_checks']])+'\n'
    text+='## 06.2 交付记录字段\n\n'+table(['项号','字段','必填规则'],[
        ['06.2.01','任务标识','DEV/子项/MOD/F/T/CON；提交版本、差异、实际负责人和复核人'],
        ['06.2.02','输入与环境','OS、实体设备、引擎/模型/节点/环境摘要、SDK/profile、渠道应用/service；无秘密正文'],
        ['06.2.03','实现产物','dev 内真实代码/测试/配方路径，依赖锁/许可；Schema/SQL 变更链接原权威源'],
        ['06.2.04','验证类型','STATIC、MOCK、NATIVE_INTEGRATION、PHYSICAL_DEVICE、REAL_CHANNEL、INDEPENDENT_REVIEW 分别记录'],
        ['06.2.05','场景与预期','正常、权限/归属、重复/乱序/重启/取消/硬截止、余额/预算及内容出口按适用列出'],
        ['06.2.06','结果与证据','NOT_EXECUTED/PASSED/FAILED/BLOCKED/N_A；命令/时刻/脱敏结果/摘要/复现步骤；N_A 必须理由'],
        ['06.2.07','财务与清理','原业务号/原请求摘要、ledger/退款守恒和资源清理；不得披露完整凭据和买方内容'],
        ['06.2.08','已知限制','未支持字段/模型/设备/隐私/支付范围、失败后关闭策略、返工项与责任'],
        ['06.2.09','交付与可售','代码完成、主验收结果、组合发布资格分字段，禁止一次布尔值统代'],
    ])+'\n'
    text+='未来实现阶段才生成真实记录，建议放 dev/tests/evidence/<DEV>/<日期或运行号>/；含内容/密钥/设备数据库的运行数据放 runtime 并按授权保留。当前不创建假验收结果。\n\n'
    text+='## 06.3 状态与证据等级\n\n'+table(['项号','等级/状态','可以证明','不能证明'],[
        ['06.3.01','文档核验通过','覆盖、编号、依赖、链接、源摘要与生成一致','产品实现、PG 迁移、安全、设备、资金或可售'],
        ['06.3.02','STATIC / MOCK','类型/契约/局部状态和模拟故障逻辑','实际性能/原生引擎/设备或真实渠道'],
        ['06.3.03','NATIVE_INTEGRATION','原生服务、真实本机 PG、跨进程协议的被测部分','未测物理设备/Android/渠道授权'],
        ['06.3.04','PHYSICAL_DEVICE','所记录 OS/硬件/环境/profile 的场景','别的模型、节点、设备或硬件增强'],
        ['06.3.05','REAL_CHANNEL','获准主体和产品的原交易/退款/到账','其他卖家/service、其他结算模式'],
        ['06.3.06','INDEPENDENT_REVIEW','评审覆盖范围的密码/协议/实现判断','未实现分支或未来版本永久安全'],
        ['06.3.07','DESIGN / DEFERRED','设计待实现 / 本轮延后','完成、支付就绪或收费许可'],
        ['06.3.08','BLOCKED / N_A','有原因和待补条件 / 经说明确实不适用','静默算通过或抹掉需求'],
    ])+'\n'
    text+='## 06.4 关键专项验收\n\n'+table(['项号','专项','要求','来源'],[
        ['06.4.01','P11-T01—80','逐项按原主验收；跨模块任务通过不能替代功能验收',link(path,'docs/05-验收测试与实施计划.md','主验收')],
        ['06.4.02','E2E-01—20','密钥/信任/签名/终块/重连/交付/执行分离；全部未执行',link(path,'docs/技术开发/contracts/e2e-protocol-plan-v1.0.json','E2E 设计清单')],
        ['06.4.03','LP-01—14','UI/日志/文件/本地 API/抓包/简单改包门槛；不承诺高级内存防护',link(path,'docs/技术开发/附录-本地内容隐私防护与分级验收.md','LP 规范')],
        ['06.4.04','PE-01—08','同机/模型/精度/任务，分支至少 30 次 A/B；记录 RSS/PSS/VRAM/IO 与时延',link(path,'docs/技术开发/附录-低开销隐私执行完整方案.md','PE 与预算')],
        ['06.4.05','K01—K20','完整内核/节点依赖、排空升级、拒绝漂移、合格回滚',link(path,'docs/技术开发/附录-供给端内核封装与替换规范.md','内核规范')],
        ['06.4.06','FW01—FW22 与 SDK/渠道门槛','协议字段/SSE/媒体子集分别验证；Taro/Web 和支付另按各自真机/渠道用例；保留原编号',link(path,'docs/技术开发/附录-本轮范围与端侧选型修订.md','本轮规范')],
    ])+'\n'
    text+='## 06.5 当前验证限制\n\n当前所有子项和产品验收仍 NOT_EXECUTED；尚无真实负责人、实测性能或卖家渠道证据。文档工具不启动 Docker/WSL/VM/Android 模拟器、GPU 或手机，不调用真实收款。部署/HTTPS/运维/可观测性继续延后。\n'
    outputs[path]=text

    # DOC-07: path plan and authoritative sources.
    path=DOCUMENTS[7][2]
    text=header('路径迁移与规范来源','DOC-07',date)
    text+='## 07.1 路径归属登记\n\n'+table(['编号','项目相对路径','用途'],[[v['id'],'`'+v['path']+'`',v['purpose']] for v in p['paths']])+'\n'
    text+='旧根级 src/tests/workflows 仅保留历史占位，不再写入。没有搬动模型、引擎、输入输出或他人仓库。dev/src 正在按任务实现，目录存在不作为完成证据；真实进展、源码和验证链接在 dev/progress 记录。\n\n'
    text+='## 07.2 人工源、权威契约与生成物\n\n'+table(['项号','内容','人工维护入口','生成/引用关系'],[
        ['07.2.01','需求与主验收','docs/01—07、交易/模式/供给端专项和白皮书源','scripts/构建项目白皮书.py → 需求细化/控制契约/合订版'],
        ['07.2.02','技术模块/任务/映射','scripts/技术开发数据.py + 技术章节','scripts/构建技术开发文档.py → 模块/任务/API/需求技术映射'],
        ['07.2.03','内核/E2E 专项','scripts/供给端内核数据.py、scripts/端对端加密数据.py + 专项章节','技术构建器 → 既有机器策略/矩阵；dev 引用不覆盖'],
        ['07.2.04','数据库','docs/技术开发/database/schema-draft.sql','SQL AST → database-catalog；需未来原生 PG 运行验证'],
        ['07.2.05','开发子项/约束','dev/planning/开发细则源数据.py','dev/tools/构建开发细则.py → 本套 8 文档/20 模块/机器清单'],
        ['07.2.06','实施代码','dev/src、dev/tests、dev/workflows','当前无代码；后续按 DEV/F/T 与原权威契约实现'],
        ['07.2.07','状态/证据','真实实现后的报告和组合资格材料','不得用生成文件数、静态核验或 mock 推导已通过'],
    ])+'\n'
    text+='## 07.3 重建与核验顺序\n\n```powershell\npython -X utf8 scripts/构建项目白皮书.py\npython -X utf8 scripts/构建技术开发文档.py\npython -X utf8 dev/tools/构建开发细则.py\npython -X utf8 scripts/核验需求文档.py\npython -X utf8 scripts/核验项目白皮书.py\npython -X utf8 scripts/核验技术开发文档.py\npython -X utf8 dev/tools/核验开发细则.py\n```\n\n'
    text+='均在项目根目录用原生 Python 执行。当前文档依赖沿用已有 runtime/tooling；开发细则工具只用标准库。先重建被修改的权威源，再重建 dev；生成内容过期或来源摘要不符必须修复，不能绕过核验。\n\n'
    text+='## 07.4 本次生成输入清单\n\n'+table(['项号','来源','SHA-256'],[[f'07.4.{i:02d}',link(path,s['path'],s['path']),'`'+s['sha256']+'`'] for i,s in enumerate(p['source_manifest'],1)])+'\n'
    text+='源摘要同时保存在机器开发清单。摘要用于判断本套细则是否过期，不是运行环境、签名信任或产品验收证明。\n'
    outputs[path]=text

    # dev root navigation.
    path='dev/README.md'
    text=header('算力共享平台开发目录','DEV-ROOT',date)
    # Keep current implementation scope in the generated navigation. Reading
    # the manual registry avoids hand-editing this generated README each round.
    progress=json.loads((ROOT/'dev/progress/implementation-status.json').read_text(encoding='utf-8'))
    text+='## 当前源码阶段：'+progress['round']+'\n\n'+progress['activity_source']+'\n\n'
    text+=link(path,'dev/progress/README.md','全部56任务与每轮证据')+'；'+link(path,'dev/repository/USAGE.md','本机使用与恢复')+'。\n\n'
    text+='开发路径：克隆根目录下的 `dev/`。后续开发均在本目录组织；所有命令以当前克隆的项目根目录为工作目录。\n\n'
    text+=table(['编号','文档','内容'],[[i,link(path,target,name),{'DOC-00':'范围/选型/编号','DOC-01':'20 模块职责及边界','DOC-02':'47 约束、失败处理及证据','DOC-03':'56 任务、222 子项、依赖与交付','DOC-04':'93 控制操作、65 表、80 F/T','DOC-05':'依赖图、阶段和条件分支','DOC-06':'验收条件与真实证据等级','DOC-07':'开发路径、规范源和重建顺序'}[i]] for i,name,target in DOCUMENTS])+'\n'
    text+=link(path,'dev/源码使用说明.md','源码使用说明')+'；'+link(path,PLAN_PATH,'机器开发清单')+'；'+link(path,'dev/评审/开发细则核验-v1.0.md','结构核验报告')+'；'+link(path,'dev/AGENTS.md','开发工作约定')+'。\n\n'
    text+='## 路径\n\n'+table(['编号','路径','用途'],[[v['id'],'`'+v['path']+'`',v['purpose']] for v in p['paths']])+'\n'
    text+='## 使用\n\n先读 DOC-00/01/02，再从 DOC-03 选任务进入对应模块细则，按 DOC-05 依赖实现，最后按 DOC-06 交付。修改人工源/权威契约后先重建相应附件，再重建细则并核验；不得直接编辑生成文档。\n\n'
    text+='```powershell\npython -X utf8 dev/tools/构建开发细则.py\npython -X utf8 dev/tools/核验开发细则.py\n```\n\n'
    text+=link(path,'dev/progress/README.md','源码实施与每轮完整待办')+'；'+link(path,'docs/需求推进/NB-1.0/README.md','NB-R1需求推进')+'。源码阶段由进度登记维护；主验收及真实设备/渠道资格仍需独立证据。不启动本地虚拟化或模拟器，部署/HTTPS/可观测性延后。历史 root/src、tests、workflows 不再接收新开发内容；运行数据保持在项目 runtime。\n'
    outputs[path]=text
    p['generated_files']=sorted([*outputs,PLAN_PATH])
    outputs[PLAN_PATH]=json.dumps(p,ensure_ascii=False,indent=2)+'\n'
    return outputs


def main() -> int:
    outputs=render_outputs()
    for name,content in outputs.items():
        path=ROOT/name
        if not path.resolve().is_relative_to(DEV.resolve()):
            raise ValueError('生成路径超出 dev：'+name)
        path.parent.mkdir(parents=True,exist_ok=True)
        path.write_text(content,encoding='utf-8')
    plan=json.loads(outputs[PLAN_PATH])
    print(json.dumps({'generated_files':len(outputs),**plan['summary'],'implementation_progress':'dev/progress/implementation-status.json','product_tests':'NOT_EXECUTED'},ensure_ascii=False))
    return 0


if __name__=='__main__':
    raise SystemExit(main())
