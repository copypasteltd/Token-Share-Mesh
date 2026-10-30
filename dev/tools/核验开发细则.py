"""核验开发清单覆盖、编号、源同步、依赖和链接；不运行产品测试。"""
from __future__ import annotations

import ast
from collections import Counter
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import sys
from urllib.parse import unquote

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[2]
DEV = ROOT / 'dev'
REPORT = DEV / '评审/开发细则核验-v1.0.md'
JSON_REPORT = REPORT.with_suffix('.json')


def main() -> int:
    checks, errors = [], []
    def check(condition, title, details=''):
        checks.append({'id': f'VFY-{len(checks)+1:03d}', 'title': title,
                       'status': 'PASSED' if condition else 'FAILED', 'details': details})
        if not condition:
            errors.append(title + ('：' + details if details else ''))
    plan_path = DEV / 'contracts/development-plan-v1.0.json'
    p = json.loads(plan_path.read_text(encoding='utf-8'))
    spec = importlib.util.spec_from_file_location('development_plan_builder', DEV / 'tools/构建开发细则.py')
    builder = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(builder)
    expected = builder.render_outputs()
    modules, tasks = p['modules'], p['tasks']
    module_map, task_map = {m['id']: m for m in modules}, {t['id']: t for t in tasks}
    check(p['development_root']=='dev/', '开发根目录固定为 dev/')
    check(p['status']=='DESIGN_UNIMPLEMENTED' and p['product_test_status']=='NOT_EXECUTED', '设计与真实验证状态无升级')
    check(len(modules)==20 and set(module_map)=={f'MOD-{i:02d}' for i in range(1,21)}, '20 模块连续唯一')
    check(len(tasks)==56 and set(task_map)=={f'DEV-{i:02d}' for i in range(1,57)}, '56 顶层任务连续唯一')
    check(Counter(t['development_status'] for t in tasks)=={'DESIGN':55,'DEFERRED':1} and task_map['DEV-55']['development_status']=='DEFERRED', '55 DESIGN + DEV-55 DEFERRED')
    steps = [s for t in tasks for s in t['steps']]
    check(len(steps)==222 and len({s['id'] for s in steps})==222, '222 子项唯一')
    check(all(len(t['steps'])==(2 if t['id']=='DEV-55' else 4) and [s['id'] for s in t['steps']]==[t['id']+f'.{i:02d}' for i in range(1,len(t['steps'])+1)] for t in tasks), '子项连续并正确绑定父任务')
    check(all(s['title'] and s['detail'] and s['required_evidence'] and s['test_status']=='NOT_EXECUTED' and not s['evidence_refs'] for s in steps), '子项有实现/失败边界和待交付证据，无假结果')
    check(all(t['actual_owner'] is None and t['estimate'] is None and not t['evidence_refs'] for t in tasks), '不伪造人员、工期或证据')
    check(all(t['module_id'] in module_map for t in tasks), '任务模块归属完整')
    check(all(set(m['task_ids'])=={t['id'] for t in tasks if t['module_id']==m['id']} for m in modules), '模块与任务双向对应')
    check(all(m['purpose'] and m['inputs'] and m['outputs'] and m['forbidden'] for m in modules), '全部模块含职责、输入、输出、禁止越界')
    check(all(v.startswith('dev/') for m in modules for v in m['planned_packages']), '模块计划路径全部落 dev/')
    check(all((ROOT/v).is_dir() for m in modules for v in m['planned_packages']), '模块计划目录已建立')
    constraints = p['constraints']
    constraint_ids = {c['id'] for c in constraints}
    check(len(constraints)==47 and constraint_ids=={f'CON-{i:03d}' for i in range(1,48)}, '47 约束连续唯一（NB-R1追加CON-047，既有编号不改）')
    check(all(c['module_ids'] and set(c['module_ids'])<=set(module_map) and c['rule'] and c['failure_action'] and c['required_evidence'] and (ROOT/c['source_document']).is_file() for c in constraints), '约束的模块、规则、失败动作、证据、来源完整')
    check(all(set(m['constraint_ids'])=={c['id'] for c in constraints if m['id'] in c['module_ids']} for m in modules), '约束与模块双向映射')
    check(all(set(t['constraint_ids'])<=constraint_ids and t['constraint_ids']==module_map[t['module_id']]['constraint_ids'] for t in tasks), '任务继承全部模块约束')
    waves=builder.topology(tasks)
    check(waves==p['topological_waves'] and sum(map(len,waves))==56, '任务依赖无缺失/循环，拓扑层完整')
    check(all('DEV-55' not in t['dependencies'] for t in tasks if t['id']!='DEV-55'), '当前任务不依赖延后 DEV-55')
    check(all('DEV-32' not in t['dependencies'] for t in tasks if t['id']!='DEV-32'), '硬件增强不成为基础档统一前置')
    stage_ids=[tid for m in p['milestones'] for tid in m['task_ids']]
    check(Counter(stage_ids)==Counter(task_map.keys()) and all(t['milestone_id'] in {m['id'] for m in p['milestones']} for t in tasks), '56 任务在阶段中恰好出现一次')
    check(next(m for m in p['milestones'] if m['id']=='MS-07')['conditional'] and next(m for m in p['milestones'] if m['id']=='MS-DEFER')['task_ids']==['DEV-55'], '条件分支与延后阶段正确')
    reqs=p['requirements']
    req_ids={r['requirement_id'] for r in reqs}
    check(len(reqs)==80 and req_ids=={f'P11-F{i:02d}' for i in range(1,81)} and all(r['acceptance_id']==r['requirement_id'].replace('-F','-T') for r in reqs), '80 功能与同号主验收连续唯一')
    check(all(r['task_ids'] and set(r['task_ids'])<=set(task_map) and r['primary_module_id'] in r['module_ids'] and set(r['module_ids'])<=set(module_map) for r in reqs), '80 功能全部关联模块与任务')
    check({f for t in tasks for f in t['requirement_ids']}==req_ids, '任务覆盖全部 80 功能，无悬空编号')
    check(all(r['test_status']=='NOT_EXECUTED' and not r['evidence_refs'] for r in reqs), '80 主验收未执行状态保持')
    check(Counter(r['current_phase_scope'] for r in reqs)=={'CURRENT':73,'PARTIAL':6,'DEFERRED':1}, '原需求阶段范围不丢失')
    operations=p['operations']
    check(len(operations)==93 and {o['id'] for o in operations}=={f'INT-{i:03d}' for i in range(1,94)} and len({o['operation_id'] for o in operations})==93, '93 控制接口连续唯一')
    check(all(o['task_id'] in task_map and task_map[o['task_id']]['module_id']==o['module_id'] and o['planned_handler'].startswith('dev/src/api/internal/http/') for o in operations), '控制接口任务/模块/新 handler 路径正确')
    check(all(set(t['operation_ids'])=={o['operation_id'] for o in operations if o['task_id']==t['id']} for t in tasks), '任务与控制接口双向对应')
    tables=p['tables']
    owners=[(name,m['id']) for m in modules for name in m['authoritative_tables']]
    check(len(tables)==65 and {t['id'] for t in tables}=={f'DATA-{i:03d}' for i in range(1,66)} and len({t['name'] for t in tables})==65, '65 数据表连续唯一')
    check(Counter(name for name,_ in owners)==Counter(t['name'] for t in tables) and all((t['name'],t['module_id']) in owners for t in tables), '65 表各有唯一权威模块，无遗漏/重叠')
    numbering=json.loads((DEV/'planning/编号登记-v1.0.json').read_text(encoding='utf-8'))
    check(all(numbering['operations'][o['operation_id']]==o['id'] for o in operations) and all(numbering['tables'][t['name']]==t['id'] for t in tables), 'INT/DATA 使用固定人工登记，不隐式重排')
    for name,key in [('technical-module-catalog','modules'),('development-backlog','tasks'),('requirement-technical-map','requirements'),('api-implementation-map','operations')]:
        canonical=builder.read_json(name)[key]
        ids='requirement_id' if key=='requirements' else 'operation_id' if key=='operations' else 'id'
        current={v[ids]:v for v in p[key]}
        check(len(canonical)==len(current) and all(all(current[v[ids]][k]==value for k,value in v.items()) for v in canonical), '原权威 '+name+' 全字段语义保持')
    check(len(p['documents'])==8 and {d['id'] for d in p['documents']}=={f'DOC-{i:02d}' for i in range(8)}, '8 份主文档编号完整')
    check(len(p['delivery_checks'])==8 and {c['id'] for c in p['delivery_checks']}=={f'CHK-{i:02d}' for i in range(1,9)}, '8 条统一交付检查完整')
    check(len(p['paths'])==22 and len({v['id'] for v in p['paths']})==22 and all((ROOT/v['path']).is_dir() for v in p['paths']), '22 条路径登记有效')
    check(set(p['generated_files'])==set(expected) and all((ROOT/n).resolve().is_relative_to(DEV.resolve()) for n in expected), '全部生成物登记且位于 dev/')
    changed=[n for n,content in expected.items() if not (ROOT/n).exists() or (ROOT/n).read_text(encoding='utf-8')!=content]
    check(not changed, '从源重建与当前文件逐字一致', ', '.join(changed))
    stale=[v['path'] for v in p['source_manifest'] if not (ROOT/v['path']).is_file() or hashlib.sha256((ROOT/v['path']).read_bytes()).hexdigest()!=v['sha256']]
    check(not stale, '全部规范源和机器契约摘要一致', ', '.join(stale))
    # The navigation links to this report on a fresh checkout as well.
    REPORT.parent.mkdir(parents=True,exist_ok=True)
    if not REPORT.exists():
        REPORT.write_text('# 开发细则结构核验\n\n正在核验，尚无通过结论。\n',encoding='utf-8')
    # Validate every generated document's UTF-8, fences, tables, links and explicit anchors.
    markdown=[ROOT/n for n in expected if n.endswith('.md')]+[DEV/'AGENTS.md']
    broken, malformed, link_count=[],[],0
    for file in markdown:
        content=file.read_text(encoding='utf-8')
        if not content.startswith('# ') or '\ufffd' in content or sum(line.startswith('```') for line in content.splitlines())%2:
            malformed.append(str(file.relative_to(ROOT)))
        ids=re.findall(r'<a id="([^"]+)"></a>',content)
        if len(ids)!=len(set(ids)):
            malformed.append(str(file.relative_to(ROOT))+': duplicate anchor')
        width=None
        for line in content.splitlines():
            if line.startswith('|') and line.endswith('|'):
                count=line.count('|')
                if width is None: width=count
                if count!=width: malformed.append(str(file.relative_to(ROOT))+': table width')
            else:
                width=None
        for target in re.findall(r'\[[^\]\n]+\]\(([^)]+)\)',content):
            if re.match(r'^(https?://|mailto:)',target): continue
            link_count+=1
            name,_,anchor=unquote(target.strip('<>')).partition('#')
            destination=(file.parent/name).resolve() if name else file
            if not destination.exists(): broken.append(str(file.relative_to(ROOT))+' -> '+target)
            elif anchor and destination.suffix=='.md' and destination.is_relative_to(DEV) and f'<a id="{anchor}"></a>' not in destination.read_text(encoding='utf-8'):
                broken.append(str(file.relative_to(ROOT))+' -> missing anchor '+target)
    check(not malformed, '全部细则编码、表格、围栏和锚点有效', ', '.join(malformed[:8]))
    check(not broken, '全部细则本地链接和显式锚点有效', ', '.join(broken[:8]))
    for file in [*DEV.glob('tools/*.py'),*DEV.glob('planning/*.py')]:
        ast.parse(file.read_text(encoding='utf-8'),filename=str(file))
    check(True, '文档工具/人工源 Python 语法可解析；未运行产品')
    product_files=sum(f.is_file() for root in (ROOT/'src',DEV/'src') for f in root.rglob('*'))
    progress_path=DEV/'progress/implementation-status.json'
    implementation=json.loads(progress_path.read_text(encoding='utf-8')) if progress_path.exists() else None
    check(product_files==0 or implementation is not None and {t['id'] for t in implementation['tasks']}==set(task_map), '存在源码时必须单独登记完整56项实施进度')
    check(not any(f.is_file() for old in (ROOT/'tests',ROOT/'workflows') for f in old.rglob('*')), '旧根级测试/工作流占位无新增文件')
    summary={'date':p['date'],'kind':'DEVELOPMENT_DOCUMENT_STRUCTURE_ONLY','passed':not errors,
             **p['summary'],'checks':len(checks),'markdown_files':len(markdown),'generated_files':len(expected),
             'local_links':link_count,'product_source_files':product_files,'product_tests':'NOT_EXECUTED',
             'source_hashes_checked':len(p['source_manifest']),'errors':errors,'details':checks}
    REPORT.parent.mkdir(parents=True,exist_ok=True)
    JSON_REPORT.write_text(json.dumps(summary,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
    lines=['# 开发细则结构核验','','P11｜'+p['date']+'｜仅文档结构/覆盖/追踪/源同步；不运行软件、设备或资金。','',
           '结果：'+('通过' if not errors else '未通过')+'。','',
           f"覆盖：20 模块、56 任务、222 子项、47 约束、93 控制操作、65 表、80 功能及同号主验收。核验 {len(checks)} 项、{link_count} 个本地链接。",'',
           '[开发总表](../开发细则总表.md)；[机器报告](开发细则核验-v1.0.json)；[机器开发清单](../contracts/development-plan-v1.0.json)。','',
           '冻结设计基线为55任务DESIGN、DEV-55 DEFERRED；真实源码/原生测试进展另见 ../progress/README.md。本次仅核验文档，不将代码文件数或设计检查提升为产品/密码/性能/真实渠道/设备通过。未调用本地虚拟化，部署/HTTPS/可观测性继续延后。','',
           '执行：项目根目录原生运行 `python -X utf8 dev/tools/核验开发细则.py`。','',
           '| 检查编号 | 内容 | 结果 |','| --- | --- | --- |']
    lines.extend('| '+c['id']+' | '+c['title']+' | '+c['status']+' |' for c in checks)
    if errors:
        lines+=['','问题：','']+['- '+e for e in errors]
    REPORT.write_text('\n'.join(lines)+'\n',encoding='utf-8')
    print(json.dumps({k:v for k,v in summary.items() if k!='details'},ensure_ascii=False,indent=2))
    return int(bool(errors))


if __name__=='__main__':
    raise SystemExit(main())
