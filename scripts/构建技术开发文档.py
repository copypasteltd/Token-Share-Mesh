"""生成P11技术设计附件和离线阅读器；不运行产品、数据库或设备。"""
from __future__ import annotations

import hashlib
import html
import json
import os
import re
import sys
from html.parser import HTMLParser
from pathlib import Path
from urllib.parse import unquote

import markdown
from 供给端内核数据 import kernel_documents
from 端对端加密数据 import e2e_documents

from 技术开发数据 import (
    DATE, STATUS, VERSION, MODULE_ROWS, OPERATION_GROUPS, PRIMARY_MODULES,
    modules, tasks, operation_tx, engine_schema, engine_result_schema, design_supplements,
)

ROOT = Path(__file__).resolve().parents[1]
TECH = ROOT / "docs/技术开发"
CONTRACTS = TECH / "contracts"
WHITE = ROOT / "docs/白皮书/contracts"
sys.path.insert(0, str(ROOT / "runtime/tooling/pglast"))
from pglast import ast, parse_sql
from pglast.stream import RawStream


def dump(path: Path, value) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2)+"\n", encoding="utf-8")


def digest(value) -> str:
    if not isinstance(value, bytes):
        value=json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(',', ':')).encode('utf-8')
    return hashlib.sha256(value).hexdigest()


def relative(path: Path) -> str:
    return path.relative_to(ROOT).as_posix()


def column_names(values) -> list[str]:
    return [v.sval for v in values or ()]


def database_catalog() -> dict:
    sql_path=TECH/'database/schema-draft.sql'
    statements=parse_sql(sql_path.read_text(encoding='utf-8'))
    tables=[]
    foreign_keys=[]
    indexes=[]
    def constraint_record(c, table: str, inline_column: str | None = None) -> dict:
        out={'kind':c.contype.name, 'name':c.conname, 'sql':RawStream()(c),
            'columns':column_names(c.keys) or ([inline_column] if inline_column else []),
            'deferrable':c.deferrable,'initially_deferred':c.initdeferred}
        if c.contype.name=='CONSTR_FOREIGN':
            out.update({'columns':column_names(c.fk_attrs) or [inline_column],
                'target_table':c.pktable.relname,'target_columns':column_names(c.pk_attrs)})
            foreign_keys.append({'table':table,**out})
        return out
    for raw in statements:
        s=raw.stmt
        if isinstance(s, ast.CreateStmt):
            table={'name':s.relation.relname,'schema':s.relation.schemaname,'columns':[], 'constraints':[]}
            for entry in s.tableElts or ():
                if isinstance(entry, ast.ColumnDef):
                    cs=[constraint_record(c,table['name'],entry.colname) for c in entry.constraints or ()]
                    table['columns'].append({'name':entry.colname,'sql':RawStream()(entry),
                        'type':RawStream()(entry.typeName), 'constraints':cs})
                elif isinstance(entry, ast.Constraint):
                    table['constraints'].append(constraint_record(entry,table['name']))
            tables.append(table)
        elif isinstance(s, ast.AlterTableStmt):
            for cmd in s.cmds:
                if isinstance(cmd.def_,ast.Constraint):
                    c=constraint_record(cmd.def_,s.relation.relname)
                    next(t for t in tables if t['name']==s.relation.relname)['constraints'].append(c)
        elif isinstance(s, ast.IndexStmt):
            indexes.append({'name':s.idxname,'table':s.relation.relname,'unique':s.unique,
                'sql':RawStream()(s)})
    forwards=[]
    for path in sorted((TECH/'database/migrations').glob('*.sql')):
        parsed=parse_sql(path.read_text(encoding='utf-8'))
        forwards.append({'source':relative(path),'source_sha256':digest(path.read_bytes()),
            'statement_count':len(parsed),'statement_types':[type(x.stmt).__name__ for x in parsed],
            'runtime_validation_in_this_build':'NOT_EXECUTED'})
    return {'version':VERSION,'date':DATE,'status':STATUS,'dialect':'PostgreSQL 17 design',
        'parser_only':True,'source':relative(sql_path),'source_sha256':digest(sql_path.read_bytes()),
        'ddl_statements':len(statements),'tables':tables,'foreign_keys':foreign_keys,'indexes':indexes,'forward_migrations':forwards,
        'runtime_validation':'NOT_EXECUTED; PL/pgSQL bodies and permissions not executed'}


def write_database(catalog: dict) -> None:
    lines=['# PostgreSQL表结构目录','','P11｜技术v1.0｜需求v3.0｜2026-10-29｜从SQL语法树生成，未迁移数据库。','',
        '[DDL源文件](schema-draft.sql)；[事务查询](transaction-patterns.sql)；[说明](README.md)。','',
        f"{len(catalog['tables'])}张表，{sum(len(t['columns']) for t in catalog['tables'])}个字段；外键、索引和函数仍须原生PG17实际验证。",'']
    for forward in catalog['forward_migrations']:
        lines += [f"前向迁移：[{Path(forward['source']).name}](migrations/{Path(forward['source']).name})，{forward['statement_count']}条语法树语句；构建不执行函数体。基础表字段目录不重复列这些触发器；原生证据另见实施进度。",'']
    owners={table:f'MOD-{row[0]:02d}' for row in MODULE_ROWS for table in row[5].split()}
    for table in catalog['tables']:
        lines += [f"## {table['name']}",'',f"权威模块：{owners.get(table['name'],'待分配')}。",'', '| 字段 | SQL定义 |', '| --- | --- |']
        for c in table['columns']:
            lines.append(f"| {c['name']} | `{c['sql'].replace('|','&#124;')}` |")
        lines += ['', '表级约束：','']
        constraints=table['constraints']
        lines += [f"- `{c['sql'].replace('|','&#124;')}`" for c in constraints] or ['无额外表级约束；列级约束见上表。']
        indices=[i for i in catalog['indexes'] if i['table']==table['name']]
        if indices:
            lines += ['', '显式索引：','']+[f"- `{i['sql']}`" for i in indices]
        lines.append('')
    lines += ['## 事务和运行限制','','FK/唯一键不替代跨行金额、当前token/资源代次、证据有效性或真实进程停止检查。数组引用由服务验证；分录必须在header创建事务内一次性写齐，延迟平衡触发器及不可变保护仍须数据库实测。','']
    (TECH/'database/表结构目录.md').write_text('\n'.join(lines),encoding='utf-8')
    dump(CONTRACTS/'database-catalog-v1.0.json',catalog)


def write_api_map(api: dict, module_catalog: list[dict], backlog: list[dict]) -> list[dict]:
    task_for_op={op:f'DEV-{n:02d}' for n,ops in OPERATION_GROUPS.items() for op in ops.split()}
    task_by_id={t['id']:t for t in backlog}
    module_by_id={m['id']:m for m in module_catalog}
    records=[]
    for path,item in api['paths'].items():
        for method,op in item.items():
            if method not in ('get','post','put','patch','delete'): continue
            task=task_by_id[task_for_op[op['operationId']]]
            mod=module_by_id[task['module_id']]
            rb=op.get('requestBody',{}).get('content',{}).get('application/json',{}).get('schema')
            keys=[p for p in op.get('parameters',[]) if p.get('name')=='Idempotency-Key']
            records.append({'operation_id':op['operationId'],'method':method.upper(),'path':path,
                'summary':op['summary'],'module_id':mod['id'],'task_id':task['id'],
                'planned_handler':f"dev/src/api/internal/http/{mod['id'].lower().replace('-','_')}/{op['operationId']}.go",
                'required_scopes':op.get('x-required-scopes',[]),'security':op['security'],
                'request_schema':rb,'response_codes':list(op['responses']),
                'idempotency_header_required':bool(keys and keys[0].get('required')),
                'transaction':operation_tx(op['operationId'],method),
                'ownership_checks':['主体scope与资源归属同时验证','seller/device/order/模式及地区证据以权威状态重查'],
                'external_io_rule':'先提交持久意向，事务外调用；UNKNOWN查原业务引用' if method!='get' else '只读投影，不触发收费或推理',
                'requirement_ids':task['requirement_ids'],'acceptance_ids':task['acceptance_ids'],
                'source_documents':task['source_documents'],
                'canonical_operation_sha256':digest(op),'implementation_status':STATUS,'test_status':'NOT_EXECUTED'})
    if set(task_for_op)!={r['operation_id'] for r in records}:
        raise ValueError('API assignment has missing/extra operations')
    dump(CONTRACTS/'api-implementation-map-v1.0.json',{'version':VERSION,'date':DATE,
        'source':relative(WHITE/'openapi-control-v3.0.json'),'canonical_api_sha256':digest(api),
        'status':STATUS,'operations':records})
    lines=['# 控制接口实施映射','','P11｜技术v1.0｜需求v3.0｜2026-10-29｜从权威OpenAPI与人工任务分配生成，接口均未实现。','',
        'AI数据入口遵循厂商兼容档位；本表仅为自有控制面。鉴权包含scope和对象归属，资金/设备副作用在提交意向后执行。','',
        '[机器实施映射](contracts/api-implementation-map-v1.0.json)；[权威OpenAPI](../白皮书/contracts/openapi-control-v3.0.json)；[任务清单](附录-开发任务清单.md)。','',
        '| operationId | 控制路径 | 模块/任务 | scope | 事务 | 幂等键 |', '| --- | --- | --- | --- | --- | --- |']
    for r in records:
        lines.append(f"| {r['operation_id']} | {r['method']} {r['path']} | {r['module_id']} / {r['task_id']} | {', '.join(r['required_scopes'])} | {r['transaction']} | {'必需' if r['idempotency_header_required'] else '不适用/见契约'} |")
    lines += ['', '## 实施规则','','每条操作的request_schema、响应码、scope和权威操作hash在JSON记录；不能由本表新增私有模型端点。handler路径为计划文件，尚不存在。状态/归属/资金/证据/当前尝试的具体校验由对应模块详规与任务完成条件共同约束。','']
    (TECH/'附录-控制接口实施映射.md').write_text('\n'.join(lines),encoding='utf-8')
    return records


def write_backlog(backlog: list[dict]) -> None:
    dump(CONTRACTS/'development-backlog-v1.0.json',{'version':VERSION,'date':DATE,'status':STATUS,'tasks':backlog})
    lines=['# 开发任务清单','','P11｜技术v1.0｜需求v3.0｜2026-10-29｜55任务DESIGN、DEV-55延后DEFERRED；全部测试NOT_EXECUTED，负责人/工期未分配。','',
        '[排期说明](21-开发任务分解与需求追踪.md)；[结构化清单](contracts/development-backlog-v1.0.json)。','']
    for t in backlog:
        lines += [f"## {t['id']} {t['title']}",'',f"模块/角色：{t['module_id']} / {t['owner_role']}。前置：{', '.join(t['dependencies']) or '无'}。",'',
            '当前阶段：'+t['current_phase_scope']+'；开发状态：'+t['development_status']+'。','','实施：'+t['implementation'],'','完成证据：'+t['done_evidence'],'',
            '需求/主验收：'+', '.join(t['requirement_ids'])+'；'+', '.join(t['acceptance_ids'])+'。','',
            '验证/外部门槛：'+', '.join(t['gates'])+'。','']
    (TECH/'附录-开发任务清单.md').write_text('\n'.join(lines),encoding='utf-8')


def write_trace(registry: dict, module_catalog: list[dict], backlog: list[dict], operations: list[dict]) -> None:
    m={x['id']:x for x in module_catalog}
    records=[]
    for i,r in enumerate(registry['requirements']):
        ts=[t for t in backlog if r['id'] in t['requirement_ids']]
        primary=f'MOD-{PRIMARY_MODULES[i]:02d}'
        mids=list(dict.fromkeys([primary]+[t['module_id'] for t in ts]))
        docs=list(dict.fromkeys([m[x]['source_document'] for x in mids]))
        ops=[o['operation_id'] for o in operations if r['id'] in o['requirement_ids']]
        records.append({'requirement_id':r['id'],'title':r['title'],'priority':r['priority'],
            'acceptance_id':r['test_case'],'primary_module_id':primary,'module_ids':mids,
            'task_ids':[t['id'] for t in ts],'operation_ids':ops,
            'tables':list(dict.fromkeys(table for mid in mids for table in m[mid]['authoritative_tables'])),
            'technical_documents':docs,'requirement_dependencies':r.get('dependency_ids',[]),
            'current_phase_scope':r.get('current_phase_scope','CURRENT'),'implementation_status':STATUS,'test_status':'NOT_EXECUTED','evidence_refs':[]})
    dump(CONTRACTS/'requirement-technical-map-v1.0.json',{'version':VERSION,'date':DATE,
        'source':'docs/评审/需求细化登记表-v3.0.json','status':STATUS,'requirements':records})
    lines=['# 80项需求技术追踪','','P11｜技术v1.0｜需求v3.0｜2026-10-29｜全部设计、未执行主验收。','',
        '[结构化完整映射](contracts/requirement-technical-map-v1.0.json)含表/接口/文档/依赖。没有自有控制API的客户端行为由适配器/SDK/真机场景验收，不能为凑覆盖新增API。','',
        '| 需求/验收 | 标题/优先级 | 主模块 | 开发任务 |', '| --- | --- | --- | --- |']
    for r in records:
        lines.append(f"| {r['requirement_id']} / {r['acceptance_id']} | {r['title']} / {r['priority']} | {r['primary_module_id']} | {', '.join(r['task_ids'])} |")
    (TECH/'附录-80项需求技术追踪.md').write_text('\n'.join(lines)+'\n',encoding='utf-8')


def write_configs() -> None:
    baseline=json.loads((WHITE/'config-catalog-v3.0.json').read_text(encoding='utf-8'))
    config={'schema_version':'1.0-draft','status':STATUS,'environment':'DEVELOPMENT',
        'project_root':'.','paths':{'db':'runtime/db','objects':'runtime/objects','logs':'runtime/logs',
          'client_work':'runtime/client-work','temporary':'runtime/tmp','models':'runtime/models'},
        'listeners':{'control':'127.0.0.1:18080','plaintext':'127.0.0.1:18081',
          'cipher':'127.0.0.1:18082','buyer_gateway':'127.0.0.1:18110','local_management':'127.0.0.1:18111'},
        'database':{'host':'127.0.0.1','port':5432,'name':'p11_dev','role_secret_ref':None},
        'secrets':{'channel_credentials_ref':None,'server_signing_key_ref':None,'session_key_ref':None,
          'api_key_pepper_ref':None,'storage_credentials_ref':None},
        'channel':{'provider':None,'product':None,'personal_seller_scope_evidence':None,'environment':'SANDBOX'},
        'fees':{'commission_bps':None,'channel_cost_owner':None,'settlement_cycle':None},
        'feature_gates':{k:False for k in ('real_payments','real_settlements','plaintext_sale','basic_e2e_sale',
          'enhanced_e2e_sale','gpu_rental_sale','android_sale','video_sale','audio_sale','threed_sale','subscription_supply','lan_access')},
        'design_defaults':{r['key']:r['value'] for r in baseline['defaults'] if r['id'] not in ('CFG21','CFG22','CFG23')},
        'deferred_defaults':{r['key']:r['value'] for r in baseline['defaults'] if r['id'] in ('CFG21','CFG22','CFG23')},
        'deferred_defaults_note':'Historical only; excluded from current-phase runtime configuration and acceptance.',
        'protocol_profiles':[],'crypto_profile':None,'sub2api_frozen_commit':None,
        'provider_kernels':{'policy':'provider-kernel-policy-v1.0.json','active_environment_ref':None,
          'active_environment_digest':None,'silent_activation':False,'buyer_node_install':False,
          'private_execution_plan':'private-execution-plan-v1.0.json','private_execution_enabled':False,
          'e2e_protocol_plan':'e2e-protocol-plan-v1.0.json','binary_stream_enabled':False},
        'scope':{'miniapp':'React + Taro / Alipay first','web':'React + Vite','deployment':'DEFERRED','observability':'DEFERRED'},
        'data_disclosure_policy':{'request_body':False,'response_body':False,'private_keys':False,'content_hashes':False,'business_audit_records':True},
        'limits_note':'Defaults are design targets; false flags do not prove implemented controls. No real channel or content keys included.'}
    dump(CONTRACTS/'config.sample-v1.0.json',config)
    platforms=[('WINDOWS','COMFYUI',['OPENAI_IMAGES_APPROVED_SUBSET']),
      ('LINUX','COMFYUI',['OPENAI_IMAGES_APPROVED_SUBSET']),
      ('WINDOWS','LM_STUDIO',['OPENAI_CHAT_TEXT','OPENAI_RESPONSES_TEXT','ANTHROPIC_MESSAGES_TEXT']),
      ('LINUX','LM_STUDIO',['OPENAI_CHAT_TEXT','OPENAI_RESPONSES_TEXT','ANTHROPIC_MESSAGES_TEXT']),
      ('WINDOWS','LLAMA_CPP_NATIVE',['OPENAI_CHAT_TEXT','OPENAI_RESPONSES_TEXT','ANTHROPIC_MESSAGES_TEXT']),
      ('LINUX','LLAMA_CPP_NATIVE',['OPENAI_CHAT_TEXT','OPENAI_RESPONSES_TEXT','ANTHROPIC_MESSAGES_TEXT']),
      ('ANDROID','LMA_LOCAL',['OPENAI_CHAT_TEXT','OPENAI_RESPONSES_TEXT','ANTHROPIC_MESSAGES_TEXT'])]
    rows=[]
    for platform,engine,profiles in platforms:
        for mode,privacy in [('PLAINTEXT','STANDARD'),('E2E','PLATFORM_BLIND'),('E2E','ATTESTED_CONFIDENTIAL'),('GPU_RENTAL','STANDARD')]:
            excluded=platform=='ANDROID' and (mode=='GPU_RENTAL' or privacy=='ATTESTED_CONFIDENTIAL')
            rows.append({'platform':platform,'engine':engine,'mode':mode,'privacy':privacy,
                'kernel_strategy':('LMS_CLI_LLMSTER_RUNTIME_WRAPPER' if engine=='LM_STUDIO' else
                  'NATIVE_CPP_WORKER_EMBEDDED_C_API' if engine=='LLAMA_CPP_NATIVE' else
                  'COMPLETE_NATIVE_ENVIRONMENT_WITH_NODE_ALLOWLIST' if engine=='COMFYUI' else 'LMA_SOURCE_DERIVATIVE'),
                'local_content_protection_state':('COMPATIBILITY_ONLY_LAST_HOP_AND_DIAGNOSTICS_UNRESOLVED' if engine=='LM_STUDIO'
                  else 'DESIGN_UNIMPLEMENTED_NOT_QUALIFIED'),
                'kernel_environment_digest':None,'candidate_modalities':(['IMAGE','VIDEO','AUDIO','THREED'] if engine=='COMFYUI' else ['TEXT']),
                'planned_protocol_profiles':profiles,'capability_state':'NOT_IN_CURRENT_DEVICE_SCOPE' if excluded else 'EVIDENCE_GATED',
                'test_status':'NOT_EXECUTED','sale_enabled':False,'evidence_refs':[],
                'required_evidence':['E01','E02','E04','E07']+(['E03'] if engine=='LM_STUDIO' else [])+
                  (['E08'] if mode=='E2E' else [])+(['E09'] if privacy=='ATTESTED_CONFIDENTIAL' else [])+
                  (['E10','E11'] if platform=='ANDROID' else [])})
    dump(CONTRACTS/'compatibility-matrix-v1.0.json',{'version':VERSION,'date':DATE,'status':STATUS,
        'description':'Planned profiles are not compatibility claims. Video/audio/3D/upstream/rental-enhanced need separate qualified combinations; actual versions unset.',
        'combinations':rows,'sdk_versions':None,'engine_releases':None,'physical_validation':'NOT_EXECUTED'})
    dump(CONTRACTS/'engine-adapter-v1.0.schema.json',engine_schema())
    dump(CONTRACTS/'engine-adapter-result-v1.0.schema.json',engine_result_schema())


class PlainText(HTMLParser):
    def __init__(self): super().__init__(); self.parts=[]
    def handle_data(self,data): self.parts.append(data)


def plain(value: str) -> str:
    parser=PlainText(); parser.feed(value)
    return ' '.join(' '.join(parser.parts).split())


def build_reader(sources: list[Path]) -> dict:
    anchors={p.resolve():f'chapter-{i:02d}' for i,p in enumerate(sources)}
    merged=['# 算力共享平台成套技术开发文档','','P11｜技术v1.0｜需求v3.0｜2026-10-29｜工程设计，产品/数据库/密码/资金/设备均未验收。','',
        '[原始导航](00-技术文档导航与实施基线.md)；[离线HTML](技术开发文档完整阅读版-v1.0.html)。','','## 目录','']
    titles={p:p.read_text(encoding='utf-8').splitlines()[0].removeprefix('# ') for p in sources}
    merged += [f'- [{titles[p]}](#{anchors[p.resolve()]})' for p in sources]
    sections=[]; nav=[]; index=[]
    for source in sources:
        anchor=anchors[source.resolve()]
        content=source.read_text(encoding='utf-8')
        def rewrite(match):
            label,target=match.group(1),match.group(2).strip('<>')
            if re.match(r'^(https?://|#|mailto:)',target): return match.group(0)
            part,sep,fragment=target.partition('#')
            p=(source.parent/unquote(part)).resolve()
            if p in anchors: return f'[{label}](#{anchors[p]})'
            return f'[{label}]({Path(os.path.relpath(p,TECH)).as_posix()}' + ('#'+fragment if sep else '')+')'
        content=re.sub(r'\[([^\]\n]+)\]\(([^)]+)\)',rewrite,content)
        output=[]; fenced=False
        for line in content.splitlines():
            if line.startswith('```'): fenced=not fenced
            if not fenced and re.match(r'^#{1,5} ',line): line='#'+line
            output.append(line)
        content='\n'.join(output)
        merged += ['',f'<a id="{anchor}"></a>','',content,'']
        def slug(value,sep):
            text=re.sub(r'[^\w\s-]','',html.unescape(value).lower())
            return anchor+'-'+(re.sub(r'[\s-]+',sep,text).strip(sep) or 'section')
        rendered=markdown.markdown(content,extensions=['tables','fenced_code','toc'],
            extension_configs={'toc':{'slugify':slug,'permalink':False}})
        rendered=re.sub(r'<a href="(https?://[^\"]+)"',r'<a rel="noopener noreferrer" href="\1"',rendered)
        headings=list(re.finditer(r'<h([1-6]) id="([^\"]+)">(.*?)</h\1>',rendered,re.S))
        for i,h in enumerate(headings):
            end=headings[i+1].start() if i+1<len(headings) else len(rendered)
            index.append({'id':h.group(2),'title':plain(h.group(3)),'chapter':titles[source],
                'text':plain(rendered[h.end():end])})
        sections.append(f'<section class="chapter" id="{anchor}">{rendered}</section>')
        nav.append(f'<li><a href="#{anchor}">{html.escape(titles[source])}</a></li>')
    (TECH/'技术开发文档完整阅读版-v1.0.md').write_text('\n'.join(merged),encoding='utf-8')
    # Independent offline reader. No network assets or browser automation needed.
    template=r'''<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>算力共享平台 · 技术开发文档 v1.0</title>
<style>
:root{--ink:#18313c;--muted:#526a74;--line:#d7e2e5;--accent:#176861}*{box-sizing:border-box}body{margin:0;background:#eff4f5;color:var(--ink);font:16px/1.85 "Segoe UI","Microsoft YaHei",sans-serif}a{color:var(--accent);text-underline-offset:3px}header{background:#153c3c;color:#fff;padding:30px 38px}h1{font-size:29px;margin:0}header p{margin:8px 0 0;color:#c8dedb}.layout{display:grid;grid-template-columns:310px minmax(0,1fr);max-width:1700px;margin:auto}aside{position:sticky;top:0;align-self:start;max-height:100vh;overflow:auto;padding:24px 22px}aside ul{list-style:none;padding:0}aside li{margin:8px 0;font-size:14px}aside label{display:block;font-weight:600}input{width:100%;padding:10px;border:1px solid #b5cbc5;font:inherit;border-radius:5px}button{margin-top:10px;padding:8px 12px;background:#fff;border:1px solid #b5cbc5;border-radius:5px;cursor:pointer}main{min-width:0;padding:26px 30px 70px}.chapter{padding:30px 36px;background:white;border:1px solid var(--line);border-radius:10px;margin-bottom:24px;overflow-wrap:anywhere}h2{font-size:26px;margin:0 0 22px}h3{font-size:21px;margin-top:30px}h4{font-size:18px}table{border-collapse:collapse;width:100%;font-size:14px;line-height:1.65;margin:20px 0}th,td{border:1px solid var(--line);padding:9px;vertical-align:top;text-align:left}th{background:#eaf3f0}tr:nth-child(even){background:#fafcfc}pre{background:#f0f4f5;overflow:auto;padding:16px;font-size:13px;line-height:1.65}code{font-family:Consolas,monospace;background:#f0f4f5;padding:1px 3px}pre code{padding:0}.note{padding:14px 18px;background:#dfefea;border-left:4px solid var(--accent);margin-bottom:24px}.small{font-size:13px;color:var(--muted)}#search-panel{display:none;background:#fff;padding:24px;margin-bottom:24px;border:1px solid var(--line)}#results{padding:0;list-style:none}#results li{padding:12px 0;border-bottom:1px solid var(--line)}#results p{margin:4px 0;font-size:13px;color:var(--muted)}.top{position:fixed;right:15px;bottom:15px;background:#153c3c;color:white;padding:7px 13px;border-radius:20px}html{scroll-padding-top:18px}@media(max-width:1000px){.layout{display:block}aside{position:relative;max-height:none}aside ul{columns:2}main{padding:0 16px 50px}.chapter{padding:24px 20px}table{display:block;overflow:auto}}@media print{@page{size:A4;margin:18mm}body{background:#fff;font-size:10pt;line-height:1.6}header{background:white;color:black;padding:0}header p{color:#333}.layout{display:block}aside,#search-panel,.note,.top{display:none!important}main{padding:20px 0}.chapter{padding:0;border:0;border-radius:0;break-before:page}h2,h3,h4{break-after:avoid}table{display:table;font-size:8pt}tr{break-inside:avoid}pre{white-space:pre-wrap;overflow-wrap:anywhere}}
</style></head><body id="top"><header><h1>算力共享平台 · 成套技术开发文档</h1><p>技术v1.0 / 需求v3.0 · 2026-10-29 · C2C / 原生多端 / 双协议 / 三模式</p></header><div class="layout"><aside aria-label="技术文档导航"><label for="search">全文检索</label><input id="search" type="search" placeholder="DEV-18、resource、加密…" autocomplete="off"><p class="small">离线阅读与打印。Mermaid以可读源图呈现；JSON/SQL附件由章节链接打开。</p><button type="button" id="print">打印 / 保存PDF</button><ul>__NAV__</ul></aside><main><div class="note">本交付为冻结工程设计依据；源码实施和原生测试见 <a href="../../dev/progress/README.md">实施进度与独立证据</a>。80项完整主验收尚未执行；设计或SQL语法检查不证明SDK、密码、设备或资金已通过。</div><section id="search-panel" aria-label="搜索结果"><h2>检索结果</h2><p id="status" role="status" aria-live="polite"></p><ol id="results"></ol></section>__SECTIONS__</main></div><a class="top" href="#top">返回顶部</a><script id="search-index" type="application/json">__INDEX__</script><script>
(()=>{const data=JSON.parse(document.getElementById('search-index').textContent),input=document.getElementById('search'),panel=document.getElementById('search-panel'),list=document.getElementById('results'),status=document.getElementById('status');let timer;function search(){const q=input.value.trim().toLocaleLowerCase();list.replaceChildren();panel.style.display=q?'block':'none';if(!q)return;const words=q.split(/\s+/),matches=data.filter(x=>words.every(w=>(x.title+' '+x.text).toLocaleLowerCase().includes(w)));status.textContent='共 '+matches.length+' 处匹配'+(matches.length>50?'，显示前50处':'');for(const x of matches.slice(0,50)){const li=document.createElement('li'),a=document.createElement('a'),p=document.createElement('p');a.href='#'+x.id;a.textContent=x.title;const pos=Math.max(0,x.text.toLocaleLowerCase().indexOf(words[0])-40);p.textContent=x.chapter+' · '+x.text.slice(pos,pos+220);li.append(a,p);list.append(li);}}input.addEventListener('input',()=>{clearTimeout(timer);timer=setTimeout(search,130)});input.addEventListener('keydown',e=>{if(e.key==='Escape'){input.value='';search()}});document.getElementById('print').addEventListener('click',()=>window.print());})();
</script></body></html>'''
    data=json.dumps(index,ensure_ascii=False).replace('<','\\u003c').replace('>','\\u003e').replace('&','\\u0026')
    output=template.replace('__NAV__',''.join(nav)).replace('__SECTIONS__','\n'.join(sections)).replace('__INDEX__',data)
    reader=TECH/'技术开发文档完整阅读版-v1.0.html'
    reader.write_text(output,encoding='utf-8')
    return {'reader_sources':len(sources),'reader_sections':len(index),'reader_bytes':reader.stat().st_size}


def write_contract_readme() -> None:
    rows = [
        ('technical-module-catalog-v1.0.json', '20模块职责/角色/计划路径/表/依赖'),
        ('development-backlog-v1.0.json', '56任务的实现/完成证据/前置/F/T；55 DESIGN、DEV-55 DEFERRED'),
        ('api-implementation-map-v1.0.json', '89控制操作到模块/事务/scope/请求/响应/权威hash'),
        ('requirement-technical-map-v1.0.json', '80项需求到模块/任务/操作/表/详规/同号验收'),
        ('database-catalog-v1.0.json', 'DDL AST的表/列/约束/FK/索引；不证明实际数据库行为'),
        ('engine-adapter-v1.0.schema.json', '本地可信域受控命令外壳，非厂商AI数据协议'),
        ('engine-adapter-result-v1.0.schema.json', '安全命令结果/后端状态；SUCCEEDED不等于成交'),
        ('config.sample-v1.0.json', '项目内路径、空秘密引用、未验证收费/隐私/二进制开关关闭'),
        ('compatibility-matrix-v1.0.json', '28候选组合，含两OS原生隐私LLM与LM兼容；全部未测试/不可售'),
        ('frontend-scope-v1.0.json', 'Taro小程序/React Web职责、纯TS边界和付款动作'),
        ('forwarding-profiles-v1.0.json', '11个转发设计档、22项联调场景；非厂商Schema'),
        ('payment-product-profile-v1.0.json', 'A402字段/时序、50元/12h快照及C2C未获证能力'),
        ('provider-kernel-policy-v1.0.json', '内核路线/替换/导入前白名单/用户同意及本地防护边界'),
        ('kernel-environment-v1.0.schema.json', '组件/精确版本/hash/许可引用组成的不可变环境'),
        ('node-allowlist-v1.0.schema.json', '有限节点/传递依赖/权限范围；非OS隔离证明'),
        ('private-execution-plan-v1.0.json', '原生LLM/媒体/Android、管道/加密/资源预算，LP/PE未执行'),
        ('e2e-protocol-plan-v1.0.json', '双端/钥/信任/C与H/AAD/三层profile/终态/恢复与20项待执行子场景；不是wire Schema'),
        ('build-summary-v1.0.json', '可重建的文档数量，不表示产品完成比例'),
    ]
    lines = ['# 技术开发机器附件', '',
        'P11｜技术v1.0｜需求v3.0｜2026-10-29｜全部为设计/未实现，由scripts生成，不手改生成物。', '',
        '当前范围为[阶段修订](../附录-本轮范围与端侧选型修订.md)：小程序优先，部署/公网HTTPS/可观测性延后。', '',
        '| 附件 | 用途与边界 |', '| --- | --- |']
    lines += [f'| [{name}]({name}) | {description} |' for name, description in rows]
    lines += ['',
        '控制面/事件/路由/旧JSON密文帧权威源仍在[白皮书contracts](../../白皮书/contracts/README.md)；不复制另一份OpenAPI。', '',
        'EngineAdapter仅用于授权本地执行域。local_protocol_body_ref是本机受控引用，authority_payload_ref引用任务租约/租赁授权；JOB_LEASE与RENTAL_SESSION分开，租内调用不另计任务费。PREPARE需真实执行权，不能混同无执行权的E2E PREPARED。QUERY/CANCEL/CLEANUP可用原上下文查停清理，不赋新执行权；服务另核签名/当前代次/归属/期限，不接shell/任意图/裸路径。', '',
        '计数器用十进制字符串，数据库job token/资源代次受bigint上界约束，不因网络uint64扩大。Schema外壳不证明签名/算法/二进制/许可/真实设备通过。', '',
        'BINARY_STREAM_1、JSON_COMPAT和LOCAL_SEGMENTED_1分层。旧JSON channel/Schema不覆盖新增二进制格式；协商/独立Schema/库及向量冻结后才可启用。完整钥生命周期、签名/AAD、END/回执/恢复见[技术08](../08-端到端加密协议与密钥实现设计.md)，执行/媒体/预算见[低开销方案](../附录-低开销隐私执行完整方案.md)。', '',
        '源为技术章节/SQL、scripts/技术开发数据.py、scripts/供给端内核数据.py和scripts/端对端加密数据.py。构建运行scripts/构建技术开发文档.py，核验运行scripts/核验技术开发文档.py；只写/核设计，不运行产品或真实密码。E2E-01—20、LP-01—14、PE-01—08及80项主验收均NOT_EXECUTED。', '']
    (CONTRACTS/'README.md').write_text('\n'.join(lines), encoding='utf-8')


def main() -> int:
    CONTRACTS.mkdir(parents=True,exist_ok=True)
    chapter_paths=sorted(TECH.glob('[0-9][0-9]-*.md'))
    chapters={int(p.name[:2]):relative(p) for p in chapter_paths}
    if set(chapters)!=set(range(24)): raise ValueError('expected chapters 00..23')
    api=json.loads((WHITE/'openapi-control-v3.0.json').read_text(encoding='utf-8'))
    registry=json.loads((ROOT/'docs/评审/需求细化登记表-v3.0.json').read_text(encoding='utf-8'))
    mod=modules(chapters); backlog=tasks(mod,chapters)
    dump(CONTRACTS/'technical-module-catalog-v1.0.json',{'version':VERSION,'date':DATE,'status':STATUS,'modules':mod})
    write_backlog(backlog)
    ops=write_api_map(api,mod,backlog)
    write_trace(registry,mod,backlog,ops)
    catalog=database_catalog(); write_database(catalog); write_configs()
    for name,value in design_supplements().items(): dump(CONTRACTS/name,value)
    for name,value in kernel_documents().items(): dump(CONTRACTS/name,value)
    for name,value in e2e_documents().items(): dump(CONTRACTS/name,value)
    write_contract_readme()
    sources=chapter_paths+sorted(TECH.glob('附录-*.md'))+[TECH/'database/README.md',TECH/'database/表结构目录.md']
    summary={'version':VERSION,'status':STATUS,'chapters':len(chapters),'modules':len(mod),
        'development_tasks':len(backlog),'requirements':len(registry['requirements']),
        'control_operations':len(ops),'sql_tables':len(catalog['tables']),**build_reader(sources)}
    dump(CONTRACTS/'build-summary-v1.0.json',summary)
    print(json.dumps(summary,ensure_ascii=False,indent=2))
    return 0


if __name__=='__main__':
    raise SystemExit(main())
