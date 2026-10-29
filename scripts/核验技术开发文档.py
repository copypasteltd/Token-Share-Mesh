"""P11设计附件静态核验；不是产品、迁移、密码或渠道测试。"""
from __future__ import annotations

import copy
import json
import re
import shutil
import subprocess
import sys
from collections import Counter
from html.parser import HTMLParser
from pathlib import Path
from urllib.parse import unquote, urlparse

from jsonschema import Draft202012Validator, FormatChecker

from 构建技术开发文档 import ROOT, TECH, CONTRACTS, WHITE, database_catalog, digest

sys.path.insert(0,str(ROOT/'runtime/tooling/pglast'))
import pglast

REPORT=ROOT/'docs/评审/技术开发文档核验-v1.0'


def load(path: Path): return json.loads(path.read_text(encoding='utf-8'))


class Reader(HTMLParser):
    def __init__(self):
        super().__init__(); self.ids=[]; self.links=[]; self.assets=[]
        self.script_kind=None; self.script_parts=[]; self.scripts=[]; self.search_json=None
    def handle_starttag(self,tag,attrs):
        a=dict(attrs)
        if a.get('id'): self.ids.append(a['id'])
        if tag=='a' and a.get('href'): self.links.append(a['href'])
        if tag in ('script','img','iframe','link','audio','video','source'):
            if a.get('src') or a.get('href'): self.assets.append(a.get('src') or a.get('href'))
        if tag=='script':
            self.script_kind='index' if a.get('id')=='search-index' else 'javascript'
            self.script_parts=[]
    def handle_data(self,data):
        if self.script_kind: self.script_parts.append(data)
    def handle_endtag(self,tag):
        if tag=='script' and self.script_kind:
            value=''.join(self.script_parts)
            if self.script_kind=='index': self.search_json=json.loads(value)
            else: self.scripts.append(value)
            self.script_kind=None


def main() -> int:
    errors=[]
    report_json=REPORT.parent/(REPORT.name+'.json')
    report_md=REPORT.parent/(REPORT.name+'.md')
    if not report_md.exists():
        report_md.write_text('# 技术开发文档专项核验\n\n本次静态核验正在生成。\n',encoding='utf-8')
    def check(ok: bool, message: str):
        if not ok: errors.append(message)
    mod=load(CONTRACTS/'technical-module-catalog-v1.0.json')['modules']
    tasks=load(CONTRACTS/'development-backlog-v1.0.json')['tasks']
    mapping=load(CONTRACTS/'api-implementation-map-v1.0.json')
    trace=load(CONTRACTS/'requirement-technical-map-v1.0.json')['requirements']
    registry=load(ROOT/'docs/评审/需求细化登记表-v3.0.json')['requirements']
    baseline=load(WHITE/'openapi-control-v3.0.json')
    api={v['operationId']:(method.upper(),path,v) for path,item in baseline['paths'].items()
        for method,v in item.items() if method in ('get','post','put','patch','delete')}
    mids={m['id'] for m in mod}; tids={t['id'] for t in tasks}; fids={r['id'] for r in registry}
    check(len(mod)==len(mids)==20,'模块应唯一20项')
    check(len(tasks)==len(tids)==56,'DEV任务应唯一56项')
    check(len(trace)==80 and {r['requirement_id'] for r in trace}==fids,'需求追踪未完整覆盖80项')
    check(mapping['canonical_api_sha256']==digest(baseline),'权威API整体hash漂移，需重新构建')
    check(len(mapping['operations'])==len(api)==89,'控制操作数量不匹配89')
    check({r['operation_id'] for r in mapping['operations']}==set(api),'控制操作集合漂移')
    mod_by_id={m['id']:m for m in mod}
    task_by_id={t['id']:t for t in tasks}
    covered=set()
    for t in tasks:
        check(t['module_id'] in mids,f"{t['id']}模块不存在")
        check(bool(t['requirement_ids']) and set(t['requirement_ids'])<=fids,f"{t['id']}需求引用无效")
        check(t['acceptance_ids']==[r.replace('-F','-T') for r in t['requirement_ids']],f"{t['id']}主验收不同号")
        check(set(t['dependencies'])<=tids and t['id'] not in t['dependencies'],f"{t['id']}任务前置无效")
        check(len(t['implementation'])>=25 and len(t['done_evidence'])>=25,f"{t['id']}缺具体实现/完成证据")
        check(t['development_status']==('DEFERRED' if t['id']=='DEV-55' else 'DESIGN') and t['test_status']=='NOT_EXECUTED' and not t['evidence_refs'],f"{t['id']}不应生成已完成状态")
        check(all(re.fullmatch(r'(E(?:0[1-9]|1[0-5])|POC-(?:0[1-9]|1[0-2]))',g) for g in t['gates']),f"{t['id']}未知验证门槛")
        for doc in t['source_documents']: check((ROOT/doc).is_file(),f"{t['id']}源文档不存在:{doc}")
        covered.update(t['requirement_ids'])
    check(covered==fids,'任务未覆盖全部需求')
    check(task_by_id['DEV-55']['current_phase_scope']=='DEFERRED','部署/可观测性任务未延后')
    check(all(task_by_id[d]['current_phase_scope']!='DEFERRED' for t in tasks
        if t['current_phase_scope']=='CURRENT' for d in t['dependencies']),'当前任务仍依赖延后任务')
    check(next(r for r in trace if r['requirement_id']=='P11-F51')['current_phase_scope']=='DEFERRED','监控需求未标本轮延后')
    check(mod_by_id['MOD-19']['planned_packages']==['dev/src/miniapp/src','dev/src/web/src','dev/src/frontend/packages'],'前端计划路径未同步')
    visiting=set(); visited=set()
    def visit(task_id):
        if task_id in visiting: errors.append(f'任务依赖循环:{task_id}'); return
        if task_id in visited or task_id not in task_by_id: return
        visiting.add(task_id)
        for dep in task_by_id[task_id]['dependencies']: visit(dep)
        visiting.remove(task_id); visited.add(task_id)
    for task_id in sorted(tids): visit(task_id)
    for r in mapping['operations']:
        method,path,op=api[r['operation_id']]
        check((r['method'],r['path'])==(method,path),f"{r['operation_id']}路由漂移")
        check(r['canonical_operation_sha256']==digest(op),f"{r['operation_id']}契约hash漂移")
        check(r['required_scopes']==op['x-required-scopes'] and r['security']==op['security'],f"{r['operation_id']}鉴权漂移")
        check(r['response_codes']==list(op['responses']),f"{r['operation_id']}响应码漂移")
        check(r['module_id'] in mids and r['task_id'] in tids,f"{r['operation_id']}实施归属无效")
        if r['task_id'] in tids:
            check(r['module_id']==task_by_id[r['task_id']]['module_id'],f"{r['operation_id']}任务模块不一致")
        expected=op.get('requestBody',{}).get('content',{}).get('application/json',{}).get('schema')
        check(r['request_schema']==expected,f"{r['operation_id']}请求Schema漂移")
        expected_idem=any(p.get('name')=='Idempotency-Key' and p.get('required') for p in op.get('parameters',[]))
        check(r['idempotency_header_required']==expected_idem,f"{r['operation_id']}幂等规则漂移")
    canonical={r['id']:r for r in registry}
    for r in trace:
        req=canonical[r['requirement_id']]
        check((r['title'],r['priority'],r['acceptance_id'])==(req['title'],req['priority'],req['test_case']),f"{req['id']}基线漂移")
        check(r['primary_module_id'] in mids and set(r['module_ids'])<=mids,f"{req['id']}模块引用无效")
        check(bool(r['task_ids']) and set(r['task_ids'])<=tids,f"{req['id']}无有效开发任务")
        check(set(r['operation_ids'])<=set(api),f"{req['id']}操作引用无效")
        check(r['test_status']=='NOT_EXECUTED' and not r['evidence_refs'],f"{req['id']}状态失真")
        for doc in r['technical_documents']: check((ROOT/doc).is_file(),f'{doc}不存在')
    lease=baseline['components']['schemas']['Lease']
    check({'resource_id','resource_lock_generation','fencing_token','lease_revision','not_before','hard_deadline',
        'execution_manifest_ref','execution_manifest_hash'}<=set(lease['required']),'Lease缺双围栏/签名截止/执行清单字段')
    progress=baseline['paths']['/runner/attempts/{attempt_id}/progress']['post']
    check(progress['responses']['200']['content']['application/json']['schema']['$ref'].endswith('/ProgressAck'),'心跳续权未使用ProgressAck')

    # Parse and inspect SQL structure. Never connect to PostgreSQL or execute it.
    catalog=database_catalog()
    check(catalog==load(CONTRACTS/'database-catalog-v1.0.json'),'DDL与生成目录漂移')
    table_by_name={t['name']:t for t in catalog['tables']}
    check(len(table_by_name)==len(catalog['tables'])==60,'应为60张唯一表')
    columns={n:{c['name'] for c in t['columns']} for n,t in table_by_name.items()}
    uniques={}
    for n,t in table_by_name.items():
        check(len(columns[n])==len(t['columns']),f'{n}重复列')
        allcs=t['constraints']+[c for col in t['columns'] for c in col['constraints']]
        primaries=[c for c in allcs if c['kind']=='CONSTR_PRIMARY']
        check(len(primaries)==1,f'{n}应恰有一个主键')
        uniques[n]=[tuple(c['columns']) for c in allcs if c['kind'] in ('CONSTR_PRIMARY','CONSTR_UNIQUE')]
        for c in allcs:
            check(set(c['columns'])<=columns[n],f'{n}约束引用不存在的列')
    for fk in catalog['foreign_keys']:
        target=fk['target_table']; source=fk['table']; cols=fk['target_columns']
        check(target in columns,f'{source}外键目标表缺失:{target}')
        if target in columns:
            check(set(cols)<=columns[target],f'{source}外键目标列缺失:{target}')
            check(tuple(cols) in uniques[target],f'{source}外键目标非主键/唯一键:{target}.{cols}')
        check(len(cols)==len(fk['columns']),f'{source}外键列数不匹配')
    all_owned={t for m in mod for t in m['authoritative_tables']}
    check(all_owned==set(table_by_name),'模块权威表集合与DDL不一致')
    for r in trace: check(set(r['tables'])<=set(table_by_name),f"{r['requirement_id']}无效表引用")
    sql=(TECH/'database/schema-draft.sql').read_text(encoding='utf-8')
    check(len(catalog['forward_migrations'])==1,'前向002迁移登记缺失')
    for migration in catalog['forward_migrations']:
        path=ROOT/migration['source']
        embedded=ROOT/'dev/src/internal/persistence/migrations'/path.name
        check(path.read_bytes()==embedded.read_bytes(),'权威前向迁移与嵌入副本不一致')
        manifest=load(embedded.with_name('source-v2.json'))
        check(manifest['source_sha256']==digest(path.read_bytes())==manifest['migration_sha256'],'前向迁移来源摘要不一致')
        check(migration['statement_count']==len(pglast.parse_sql(path.read_text(encoding='utf-8'))),'前向迁移语法树统计失配')
    patterns=(TECH/'database/transaction-patterns.sql').read_text(encoding='utf-8')
    query_ast=pglast.parse_sql(patterns)
    check('journal_new_entries_only' in sql and 'DEFERRABLE INITIALLY DEFERRED' in sql,'缺账务追加/平衡设计')
    check('IS DISTINCT FROM pg_current_xact_id()' in sql,'缺历史journal禁止再加分录设计')
    check('SKIP LOCKED' in patterns and 'claim_generation=$2' in patterns,'缺异步认领围栏设计')
    check('FOR UPDATE' in patterns and "state IN ('REQUESTED','PROCESSING','SUCCEEDED','UNKNOWN')" in patterns,'缺原支付锁/UNKNOWN退款累计设计')

    # Positive/negative schema examples test the design shell, not a crypto or engine.
    command_schema=load(CONTRACTS/'engine-adapter-v1.0.schema.json')
    result_schema=load(CONTRACTS/'engine-adapter-result-v1.0.schema.json')
    environment_schema=load(CONTRACTS/'kernel-environment-v1.0.schema.json')
    allowlist_schema=load(CONTRACTS/'node-allowlist-v1.0.schema.json')
    validators={}
    for name,schema in [('command',command_schema),('result',result_schema),
                        ('environment',environment_schema),('allowlist',allowlist_schema)]:
        Draft202012Validator.check_schema(schema)
        validators[name]=Draft202012Validator(schema,format_checker=FormatChecker())
    validators['progress']=Draft202012Validator({'$ref':'#/components/schemas/ProgressAck',
        'components':baseline['components']},format_checker=FormatChecker())
    base={'schema_version':'1.0-draft','command_id':'command_demo_001','operation':'DESCRIBE',
        'execution_domain':'AUTHORIZED_LOCAL_ENDPOINT','engine_kind':'COMFYUI','idempotency_key':'intent_demo_001'}
    execute={**base,'operation':'EXECUTE','authority_payload_ref':'lease_demo_001',
      'protocol_profile_id':'profile_demo_001','recipe_digest':'a'*64,'kernel_environment_digest':'d'*64,'input_asset_refs':['asset_demo_001'],
      'local_protocol_body_ref':'local_demo_001','context':{
       **{k:k+'_demo_001' for k in ('order_id','seller_id','device_id','resource_id','job_id','attempt_id','lease_id')},
       'authority_kind':'JOB_LEASE','fencing_token':'3','resource_lock_generation':'8','contract_hash':'b'*64,'deadline':'2026-10-29T12:00:00Z'}}
    unknown={'schema_version':'1.0-draft','command_id':'command_demo_001','status':'UNKNOWN',
       'execution_state':'UNKNOWN','safe_code':'BACKEND_SUBMIT_UNKNOWN','retryability':'QUERY_ORIGINAL','evidence_refs':[]}
    cases=[('describe','command',base,True),('execute','command',execute,True),
        ('query_original','command',{**execute,'operation':'QUERY'},True),('unknown_result','result',unknown,True)]
    rental=copy.deepcopy(execute)
    for key in ('job_id','attempt_id','lease_id','fencing_token'): del rental['context'][key]
    rental['context'].update({'authority_kind':'RENTAL_SESSION','session_id':'session_demo_001',
        'invocation_id':'invoke_demo_001','access_grant_id':'grant_demo_001','terms_revision':'1'})
    rental['authority_payload_ref']='grant_demo_001'
    mixed=copy.deepcopy(rental); mixed['context']['job_id']='job_demo_001'
    cases += [('rental_invocation_shell','command',rental,True),('mixed_execution_authorities','command',mixed,False)]
    signed_lease={**{k:k+'_demo_001' for k in ('lease_id','job_id','attempt_id','order_id','seller_id','device_id',
          'resource_id','capability_id','execution_manifest_ref','signature_profile_id')},
        'resource_lock_generation':'8','fencing_token':'3','lease_revision':2,
        'issued_at':'2026-10-29T12:00:10Z','not_before':'2026-10-29T12:00:00Z',
        'expires_at':'2026-10-29T12:00:55Z','hard_deadline':'2026-10-29T12:05:00Z',
        'execution_manifest_hash':'c'*64,'kernel_environment_digest':'d'*64,'contract_hash':'b'*64,'signature':'A'*64}
    ack={'id':'ack_demo_001','version':2,'status':'RECORDED','recorded_at':'2026-10-29T12:00:10Z'}
    cases += [('signed_renewal_shell','progress',{'ack':ack,'action':'CONTINUE','lease':signed_lease},True),
        ('stop_without_renewal','progress',{'ack':ack,'action':'STOP'},True),
        ('continue_missing_signed_lease','progress',{'ack':ack,'action':'CONTINUE'},False),
        ('stop_cannot_extend_lease','progress',{'ack':ack,'action':'STOP','lease':signed_lease},False)]
    wrong=copy.deepcopy(execute); del wrong['context']['resource_lock_generation']
    cases += [('missing_resource_generation','command',wrong,False),
      ('arbitrary_shell','command',{**execute,'shell':'arbitrary command'},False),
      ('platform_plain_body','command',{**execute,'execution_domain':'PLATFORM'},False),
      ('wrong_operation','command',{**execute,'operation':'EXEC_SHELL'},False),
      ('invalid_hash','command',{**execute,'recipe_digest':'raw prompt text'},False),
      ('unsafe_unknown_retry','result',{**unknown,'retryability':'RETRY_SAME_COMMAND'},False),
      ('raw_result_body','result',{**unknown,'prompt':'private content'},False)]
    missing_environment=copy.deepcopy(execute); del missing_environment['kernel_environment_digest']
    cases.append(('missing_execution_environment','command',missing_environment,False))
    # Synthetic shape fixtures, deliberately not real downloadable packages or signatures.
    def component(role):
        return {'component_id':'component_demo_'+role,'role':role,'version':'0.0.0-fixture',
            'source_url':'https://example.invalid/not-a-package','sha256':'e'*64,'license_evidence_ref':'license_demo_001'}
    environment={'schema_version':'1.0-draft','environment_id':'environment_demo_001','engine_kind':'LM_STUDIO',
        'platform':'WINDOWS','arch':'X86_64','adapter_contract_version':'1.0-draft','adapter_version':'0.0.0-fixture',
        'components':[component(r) for r in ('LM_CLI','LM_DAEMON','LM_RUNTIME')],
        'model_digests':['a'*64],'recipe_digests':['b'*64],'protocol_profile_ids':['profile_demo_001'],
        'modalities':['TEXT'],'owner_consent_ref':'consent_demo_001','approval_key_id':'approval_demo_001'}
    missing_runtime=copy.deepcopy(environment); missing_runtime['components'].pop()
    latest=copy.deepcopy(environment); latest['components'][0]['version']='latest'
    comfy=copy.deepcopy(environment); comfy.update({'engine_kind':'COMFYUI',
        'components':[component('COMFY_CORE'),component('PYTHON')],'modalities':['IMAGE','VIDEO','AUDIO','THREED'],
        'node_allowlist_revision':'allowlist_demo_001','node_allowlist_digest':'f'*64})
    missing_allowlist=copy.deepcopy(comfy); del missing_allowlist['node_allowlist_digest']
    mobile=copy.deepcopy(environment); mobile.update({'engine_kind':'LMA_LOCAL','platform':'ANDROID','arch':'ARM64',
        'components':[component(r) for r in ('LMA_SOURCE','ANDROID_NATIVE','PLATFORM_BRIDGE')]})
    missing_bridge=copy.deepcopy(mobile); missing_bridge['components'].pop()
    native=copy.deepcopy(environment); native.update({'engine_kind':'LLAMA_CPP_NATIVE',
        'components':[component(r) for r in ('PRIVATE_WORKER','LLAMA_LIBRARY','GGML_BACKEND')]})
    native_missing_backend=copy.deepcopy(native); native_missing_backend['components'].pop()
    native_android=copy.deepcopy(native); native_android.update({'platform':'ANDROID','arch':'ARM64'})
    node={'node_id':'node_demo_001','source_url':'https://example.invalid/not-a-node','exact_revision':'0.0.0-fixture',
        'code_sha256':'a'*64,'class_types':['ExampleFixtureNode'],'dependency_artifacts':[component('PYTHON_DEPENDENCY')],
        'reviewed_install_step_ids':[],'license_evidence_refs':['license_demo_001'],
        'file_scopes':['TASK_WORKSPACE','APPROVED_MODELS_READONLY'],'install_download_hosts':['example.invalid'],
        'execution_network':'NONE','execution_allowed_hosts':[],
        'runtime_code_download_allowed':False,'runtime_package_install_allowed':False}
    allowlist={'schema_version':'1.0-draft','allowlist_revision':'allowlist_demo_001',
        'comfy_environment_ref':'environment_demo_001','approval_key_id':'approval_demo_001',
        'core_class_types':['ExampleCoreNode'],'node_packs':[node],'valid_until':'2026-10-30T12:00:00Z',
        'revoked':False,'unlisted_enabled_node_policy':'REJECT_BEFORE_IMPORT','buyer_install_permission':False}
    runtime_install=copy.deepcopy(allowlist); runtime_install['node_packs'][0]['runtime_package_install_allowed']=True
    unlisted_network=copy.deepcopy(allowlist); unlisted_network['node_packs'][0]['execution_allowed_hosts']=['example.invalid']
    cases += [('lm_three_component_environment','environment',environment,True),
        ('missing_lm_runtime','environment',missing_runtime,False),('unfrozen_latest_component','environment',latest,False),
        ('comfy_complete_environment','environment',comfy,True),('comfy_missing_allowlist','environment',missing_allowlist,False),
        ('lma_derived_environment','environment',mobile,True),('lma_missing_platform_bridge','environment',missing_bridge,False),
        ('native_private_llm_environment','environment',native,True),
        ('native_private_llm_missing_backend','environment',native_missing_backend,False),
        ('desktop_native_llm_mislabeled_android','environment',native_android,False),
        ('finite_node_allowlist','allowlist',allowlist,True),
        ('buyer_node_install','allowlist',{**allowlist,'buyer_install_permission':True},False),
        ('node_runtime_package_install','allowlist',runtime_install,False),
        ('node_undeclared_network','allowlist',unlisted_network,False)]
    fixture_results=[]
    for name,kind,instance,valid in cases:
        actual=not list(validators[kind].iter_errors(instance))
        check(actual==valid,f'Schema夹具:{name}预期{valid}实际{actual}')
        fixture_results.append({'name':name,'schema':kind,'expected_valid':valid,'actual_valid':actual,
            'scope':'schema shell only, not runtime engine/crypto execution'})
    config=load(CONTRACTS/'config.sample-v1.0.json')
    defaults=load(WHITE/'config-catalog-v3.0.json')
    check(config['design_defaults']=={v['key']:v['value'] for v in defaults['defaults'] if v['id'] not in ('CFG21','CFG22','CFG23')},'本轮配置偏离有效需求基线')
    check(config['deferred_defaults']=={v['key']:v['value'] for v in defaults['defaults'] if v['id'] in ('CFG21','CFG22','CFG23')},'历史延后配置映射失真')
    check(all(v is False for v in config['feature_gates'].values()),'设计配置不得默认开放收费/额外能力')
    check(all(v is None for v in config['secrets'].values()),'配置包含秘密或假凭据')
    check(config['channel']['provider'] is None and config['fees']['commission_bps'] is None,'未定渠道/佣金不能补默认')
    check(config['scope']['deployment']==config['scope']['observability']=='DEFERRED' and 'telemetry' not in config,'样例仍含本轮排除规划')
    frontend=load(CONTRACTS/'frontend-scope-v1.0.json')
    forwarding=load(CONTRACTS/'forwarding-profiles-v1.0.json')
    payment=load(CONTRACTS/'payment-product-profile-v1.0.json')
    check(frontend['primary_surface']=='ALIPAY_MINIAPP' and frontend['miniapp']['framework']=='React + Taro'
        and frontend['web']['framework']=='React + Vite','前端选型与用户要求冲突')
    check(frontend['miniapp']['sse_validated'] is False and frontend['miniapp']['frozen_versions'] is None,'小程序未验证能力不可标通过')
    profiles=forwarding['profiles']
    check(len(profiles)==len({p['id'] for p in profiles})==11,'转发profile设计编号不唯一')
    check(all(p['test_status']=='NOT_EXECUTED' and p['sale_enabled'] is False and not p['evidence_refs'] for p in profiles),'转发档位不得假称已验证可售')
    check(forwarding['conformance_cases']==[f'FW{i:02d}' for i in range(1,23)],'转发场景矩阵缺失')
    check(forwarding['data_plane_private_json_fields_required'] is False and forwarding['payment_headers']['proof_forwarded_to_engine'] is False,'标准入口或付费凭据边界失真')
    check(forwarding['retry_policy']['cross_seller'] is False and forwarding['rental_per_invocation_payment'] is False
        and forwarding['e2e_platform_content_access'] is False,'C2C/租赁/加密转发不变量失真')
    check(payment['runtime_enabled'] is False and payment['qualification_status']=='UNVERIFIED_C2C_SELLER_AND_SERVICE','未获证收单能力不得默认启用')
    check(payment['public_api_constraints']['max_amount_fen']==5000 and payment['public_api_constraints']['max_pay_before_seconds_from_now']==43200,'收单公开限额快照失真')
    check(payment['proof_validation']['resource_id_required'] is False and payment['proof_validation']['resource_id_api_status']=='DEPRECATED_OPTIONAL','废弃resource_id仍被当必需')
    for attachment in [frontend,forwarding,payment]:
        check((ROOT/attachment['source_document']).is_file(),'新增设计附件缺源文档')
    kernels=load(CONTRACTS/'provider-kernel-policy-v1.0.json')
    check(kernels['status']=='DESIGN_UNIMPLEMENTED' and kernels['test_status']=='NOT_EXECUTED'
          and kernels['sale_enabled'] is False,'内核设计不可标实现/可售')
    routes={r['platform']+'_'+r['purpose']:r for r in kernels['routes']}
    check(routes['WINDOWS_LLM']['replaceable_layers']==['CLI','DAEMON','INFERENCE_RUNTIME']
          and routes['WINDOWS_LLM']['frozen_versions'] is None,'Windows LLM内核路线失真')
    check(routes['WINDOWS_MEDIA']['modalities']==['IMAGE','VIDEO','AUDIO','THREED'],'完整ComfyUI媒体范围缺失')
    check(all(routes[p+'_PRIVATE_LLM']['engine']=='LLAMA_CPP_NATIVE'
          and routes[p+'_PRIVATE_LLM']['transport']=='INHERITED_PIPE_NO_TCP_CONTENT'
          for p in ('WINDOWS','LINUX')),'隐私LLM路线未与LM Studio兼容路线区分')
    private_plan=load(CONTRACTS/'private-execution-plan-v1.0.json')
    check(private_plan['test_status']=='NOT_EXECUTED' and private_plan['sale_enabled'] is False
          and (ROOT/private_plan['source_document']).is_file(),'低开销方案缺源或假称已验收')
    e2e=load(CONTRACTS/'e2e-protocol-plan-v1.0.json')
    check(e2e['status']=='DESIGN_UNIMPLEMENTED' and e2e['test_status']=='NOT_EXECUTED'
          and not e2e['sale_enabled'] and not e2e['runtime_enabled']
          and (ROOT/e2e['source_document']).is_file(),'E2E设计清单缺源或假称已实现/可售')
    check(not e2e['endpoints']['platform_content_plaintext_access']
          and not e2e['endpoints']['platform_content_private_keys']
          and not e2e['endpoints']['platform_universal_recovery_key'], '平台盲转/无恢复钥边界失真')
    check(not e2e['profiles']['binary_covered_by_legacy_schema']
          and not e2e['profiles']['binary_channel_negotiation_contract_frozen']
          and not e2e['profiles']['binary_wire_schema_frozen']
          and not config['provider_kernels']['binary_stream_enabled'], '未冻结二进制档位被当成已支持')
    w=e2e['wire']; p=private_plan['wire']
    check(w['plaintext_record_max_bytes']==w['inner_header_bytes']+w['data_payload_max_bytes']
          and w['ciphertext_max_bytes']==w['plaintext_record_max_bytes']+w['candidate_secretstream_abytes']
          and w['body_length_max_bytes']==w['sequence_bytes']+w['ciphertext_max_bytes']
          and w['wire_record_max_bytes']==w['record_length_prefix_bytes']+w['body_length_max_bytes']
          and w['full_data_record_overhead_bytes']==w['wire_record_max_bytes']-w['data_payload_max_bytes']
          and w['body_length_min_bytes']==w['sequence_bytes']+w['inner_header_bytes']+w['candidate_secretstream_abytes'],
          '二进制记录设计长度预算自相矛盾')
    for key in ('stream_header_max_bytes','data_payload_max_bytes','ciphertext_max_bytes',
                'body_length_max_bytes','wire_record_max_bytes','shared_stream_buffer_max_bytes',
                'reorder_max_frames','reorder_max_bytes','text_coalesce_max_ms'):
        check(w[key]==p[key], '加密/低开销方案资源预算漂移:'+key)
    check(w['end_tag']==p['end_record_tag']=='TAG_FINAL'
          and w['extra_empty_final_record'] is False and p['extra_empty_final_record'] is False,
          'END与库FINAL记录语义不一致')
    check(e2e['storage']['manifest_max_bytes']==private_plan['storage']['manifest_max_bytes']
          and e2e['limits']['media_plaintext_max_bytes']==private_plan['budgets']['max_media_object_mib']*1024*1024
          and e2e['limits']['media_ciphertext_max_bytes']==private_plan['budgets']['max_cipher_media_object_mib']*1024*1024,
          '加密/存储/媒体限额漂移')
    check(e2e['crypto']['approved_suite_profile_id'] is None
          and not e2e['crypto']['sealed_box_authenticates_sender']
          and not e2e['crypto']['full_forward_secrecy_claimed'], '密码候选/发送者认证/前向保密边界失真')
    check(not e2e['header_binding']['unknown_resource_generation_in_prepared_input_aad']
          and not e2e['header_binding']['input_invents_future_output_delivery_revision']
          and not e2e['header_binding']['signature_or_manifest_hash_self_reference'],
          'PREPARED未知字段或循环签名设计失真')
    check(not e2e['recovery']['compute_release_invalidates_authorized_original_cipher_delivery']
          and not e2e['recovery']['delivery_scope_authorizes_compute']
          and not e2e['recovery']['old_cleanup_can_touch_new_resource_owner'], '计算/交付/旧清理权限边界失真')
    e2e_cases=e2e['conformance_cases']; e2e_source=(ROOT/e2e['source_document']).read_text(encoding='utf-8')
    check([c['id'] for c in e2e_cases]==[f'E2E-{i:02d}' for i in range(1,21)], 'E2E细化场景编号缺失/重复')
    check(all(c['test_status']=='NOT_EXECUTED' and not c['evidence_refs']
          and c['id'] in e2e_source and set(c['task_ids'])<=tids for c in e2e_cases),
          'E2E细化场景文档/任务引用或未执行状态失真')
    check('LLAMA_CPP_NATIVE' in baseline['components']['schemas']['CapabilityWrite']['properties']['engine']['enum'],
          '控制能力登记遗漏新增原生引擎')
    check(routes['ANDROID_LOCAL_INFERENCE']['implementation']=='LMA_SOURCE_DERIVATIVE'
          and routes['ANDROID_LOCAL_INFERENCE']['downloaded_executable_hotload'] is False,'Android未按LMA源码二开/APK内核约束')
    check(kernels['replacement']['replace_active_paid_environment'] is False
          and kernels['replacement']['rollback_to_revoked_environment'] is False
          and kernels['whitelist']['before_import_inventory_check'] is True,'替换/节点导入边界失真')
    check(kernels['conformance_cases']==[f'K{i:02d}' for i in range(1,21)]
          and (ROOT/kernels['source_document']).is_file(),'内核场景或规范缺失')
    check({'kernel_environment_ref','kernel_environment_digest'}<=columns['capabilities'],'能力表缺内核环境绑定')
    check('kernel_bindings' in baseline['components']['schemas']['ContractSnapshot']['required']
          and 'kernel_environment_digest' in baseline['components']['schemas']['Lease']['required']
          and 'kernel_environment_digest' in baseline['components']['schemas']['RentalAccess']['required'],
          '合同/签名执行权缺内核环境绑定')
    payment_columns={c['name'] for t in catalog['tables'] if t['name']=='payments' for c in t['columns']}
    check({'product_profile_revision','channel_binding_snapshot','prepay_reference'}<=payment_columns,'支付表未补全不可变产品/服务绑定')
    for path in config['paths'].values():
        resolved=(ROOT/path).resolve()
        check(resolved.is_relative_to(ROOT.resolve()),f'配置路径越出项目:{path}')
    compat=load(CONTRACTS/'compatibility-matrix-v1.0.json')['combinations']
    check(len(compat)==28,'组合矩阵数量不匹配')
    check(all(c['test_status']=='NOT_EXECUTED' and c['sale_enabled'] is False and not c['evidence_refs'] for c in compat),'兼容矩阵不得假称已测试/可售')
    check(all(c['capability_state']=='NOT_IN_CURRENT_DEVICE_SCOPE' for c in compat if c['platform']=='ANDROID' and
        (c['mode']=='GPU_RENTAL' or c['privacy']=='ATTESTED_CONFIDENTIAL')),'Android组合限制未记录')

    reader_path=TECH/'技术开发文档完整阅读版-v1.0.html'
    parser=Reader(); content=reader_path.read_text(encoding='utf-8'); parser.feed(content)
    check(len(parser.ids)==len(set(parser.ids)),'离线阅读器重复ID')
    check(not parser.assets and not re.search(r'@import|url\(\s*[\"\']?https?://',content),'离线阅读器含外部资源')
    check(bool(parser.search_json) and all(x['id'] in parser.ids for x in parser.search_json),'搜索索引指向不存在ID')
    reader_links=0
    for target in parser.links:
        parsed=urlparse(target)
        if parsed.scheme in ('http','https','mailto'): continue
        if not parsed.path:
            if parsed.fragment: check(unquote(parsed.fragment) in parser.ids,f'HTML锚点不存在:{target}')
        else:
            reader_links+=1
            check((reader_path.parent/unquote(parsed.path)).exists(),f'HTML本地链接不存在:{target}')
    # Node --check parses inline JavaScript without executing browser or network.
    node_command=shutil.which('node')
    node=Path(node_command) if node_command else None
    js_status='NOT_EXECUTED_NODE_UNAVAILABLE'
    if node is not None and node.is_file():
        js_path=ROOT/'runtime/tmp/technical-reader-syntax.js'
        js_path.parent.mkdir(parents=True,exist_ok=True)
        js_path.write_text('\n'.join(parser.scripts),encoding='utf-8')
        result=subprocess.run([str(node),'--check',str(js_path)],cwd=ROOT,capture_output=True,text=True,encoding='utf-8',timeout=30)
        js_status='PASSED_SYNTAX_ONLY' if result.returncode==0 else 'FAILED'
        check(result.returncode==0,'阅读器JS语法错误:'+result.stderr[:400])
    check(len(sorted(TECH.glob('[0-9][0-9]-*.md')))==24,'技术源章节不完整')
    product_source_files=sum(p.is_file() for p in (ROOT/'dev/src').rglob('*'))
    progress_path=ROOT/'dev/progress/implementation-status.json'
    implementation=json.loads(progress_path.read_text(encoding='utf-8')) if progress_path.exists() else None
    check(product_source_files==0 or implementation is not None and len(implementation['tasks'])==56,'源码实施后必须有完整56任务的独立进度登记')
    source_docs=sorted(TECH.rglob('*.md'))
    for path in source_docs:
        text=path.read_text(encoding='utf-8')
        check(text.startswith('# ') and '\ufffd' not in text,f'{path.name}标题/UTF-8错误')
        check(sum(line.startswith('```') for line in text.splitlines())%2==0,f'{path.name}围栏不成对')
    result={'version':'1.0-draft','date':'2026-10-29','passed':not errors,
      'verification_scope':'Design consistency, SQL AST and offline reader only; NO runtime acceptance',
      'source_chapters':24,'technical_markdown_files':len(source_docs),'modules':len(mod),
      'development_tasks':len(tasks),'requirements':len(trace),'acceptance_cases':80,
      'control_operations':len(api),'sql_tables':len(catalog['tables']),
      'sql_columns':sum(len(t['columns']) for t in catalog['tables']),
      'sql_foreign_keys':len(catalog['foreign_keys']),'sql_indexes':len(catalog['indexes']),
      'ddl_statements_parsed':catalog['ddl_statements'],'query_statements_parsed':len(query_ast),
      'sql_parser':'pglast '+pglast.__version__,'sql_runtime_migrations_in_this_check':'NOT_EXECUTED',
      'plpgsql_runtime_validation_in_this_check':'NOT_EXECUTED',
      'separate_native_evidence':[p for p in ['dev/tests/evidence/R001-验证报告.md','dev/tests/evidence/R003-验证报告.md'] if (ROOT/p).is_file()],
      'schema_positive_fixtures':sum(x[3] for x in cases),
      'schema_negative_fixtures':sum(not x[3] for x in cases),'fixture_results':fixture_results,
      'compatibility_combinations':len(compat),'reader_sections':len(parser.search_json or []),
      'e2e_design_cases_not_executed':len(e2e_cases),
      'reader_local_links':reader_links,'reader_external_assets':len(parser.assets),
      'reader_javascript':js_status,'product_source_files':product_source_files,
      'product_acceptance':'ALL_NOT_EXECUTED','priorities':dict(Counter(r['priority'] for r in trace)),
      'errors':errors}
    report_json.write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
    lines=['# 技术开发文档专项核验','','P11｜技术v1.0｜需求v3.0｜2026-10-29。','',
      f"结果：{'通过' if not errors else '未通过'}。范围仅为设计一致性、SQL语法树与离线阅读器。",'',
      f"实际：24章源详规、{len(mod)}模块、{len(tasks)}开发任务、80项F/T追踪、89控制操作；{len(catalog['tables'])}表/{result['sql_columns']}列/{len(catalog['foreign_keys'])}外键/{len(catalog['indexes'])}显式索引。",'',
      f"SQL：pglast {pglast.__version__}解析{catalog['ddl_statements']}条基础DDL/控制语句、{len(catalog['forward_migrations'])}个前向迁移和{len(query_ast)}条查询片段；静态检查列、主键/唯一键、外键目标。**本脚本不运行PostgreSQL、迁移、触发器或PL/pgSQL体。**源码实施另有[R003原生测试证据](../../dev/tests/evidence/R003-验证报告.md)，不由本报告判定运行结果。",'',
      f"内部命令/结果Schema：{result['schema_positive_fixtures']}正向/{result['schema_negative_fixtures']}负向夹具；只证明设计外壳的接受/拒绝，不证明真实引擎或密码。",'',
      f"E2E设计：{len(e2e_cases)}项细化场景均未执行；核对二进制长度/限额、END语义、旧JSON边界、源文档/任务引用和计算/交付分权。没有运行密码或性能实验。",'',
      f"阅读器：{result['reader_sections']}段检索索引、{reader_links}个本地附件链接、0外部资源；JavaScript：{js_status}。未启动浏览器，无新浏览器标签。",'',
      '检查任务依赖无环、全部需求/同号验收/优先级不漂移、接口方法/路径/scope/请求/响应/hash一致、全部收费开关关闭、配置无秘密且运行路径在本项目内。','',
      '**冻结设计基线55项DESIGN、DEV-55 DEFERRED；源码实施及原生测试进展另见dev/progress。**本核验只检查设计；全部产品主验收和物理引擎/手机/真实支付门槛不因此通过。设计组合仍不可售，PoC/许可/资金/密码须独立真实验证。','',
      '[机器报告](技术开发文档核验-v1.0.json)；[技术导航](../技术开发/00-技术文档导航与实施基线.md)；[契约细化说明](技术开发说明与契约变更-v1.0.md)。','']
    if errors: lines+=['## 待修复','']+['- '+e for e in errors]
    report_md.write_text('\n'.join(lines)+'\n',encoding='utf-8')
    print(json.dumps({k:v for k,v in result.items() if k!='fixture_results'},ensure_ascii=False,indent=2))
    return 0 if not errors else 1


if __name__=='__main__': raise SystemExit(main())
