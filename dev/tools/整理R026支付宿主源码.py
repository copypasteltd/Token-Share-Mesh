"""Source/docs generation only. Never build, test, validate, migrate or start IO."""
from collections import Counter
from copy import deepcopy
import json
from pathlib import Path
import re

ROOT=Path(__file__).resolve().parents[2]
ANNEX='docs/技术开发/附录-MachinePay原生宿主与本人支付接口源码.md'
TOOL='dev/tools/整理R026支付宿主源码.py'
TESTS=['dev/src/internal/paymenthost/config_test.go','dev/src/api/cmd/p11-api/payment_test.go','dev/src/api/tests/payment_http_test.go']
REFS=[ANNEX,TOOL,'dev/src/internal/paymenthost/README.md','scripts/技术开发数据.py','dev/src/internal/paymenthost/config.go','dev/src/internal/paymenthost/host.go','dev/src/internal/paymenthost/retirement.go',
 'dev/src/internal/payments/machine.go','dev/src/internal/payments/machine_store.go',
 'dev/src/api/cmd/p11-api/main.go','dev/src/api/cmd/p11-api/payment.go','dev/src/api/internal/http/payment.go',
 'dev/src/api/internal/http/server.go','dev/src/api/internal/http/routes.go','dev/src/api/internal/http/routes.json',
 'dev/src/internal/identity/sessions.go','dev/src/scheduler/cmd/p11-payment-worker/main.go']+TESTS
ITEMS=[
 ('066','原生支付注册及独立秘密加载源码','PH-001—015：可信runtime根/有界严格文件、原注册/环境/双pin、代调用token和独立双钥环，启动无RPC；真实注册/秘密ACL未验收'),
 ('067','实际调用归属与专用恢复宿主源码','PH-016—020/030—031：单循环/原capability恢复、原到期退役、取消后先join再关闭渠道/钥/外部DB；未运行'),
 ('068','本人支付创建查询两个HTTP接线源码','PH-021—027：原合同/当前角色、先事务后一次precreate、原未知重放、本人原DTO/ETag/CSRF/同DB；SDK和PayLauncher仍待'),
 ('069','正式API与专用原生worker入口源码','PH-028—029：独立成套配置或安全关闭、迁移只核已应用、专用worker不创建收费/退款/执行；未构建/启动'),
 ('070','PH编号和12项测试源码及全量文档','PH-032—040：50控制HTTP/98规划/74表、配置/SQL/合成RSA2边界定义、双语文档与全56/222；全未验证'),
]
TASKS={'DEV-01','DEV-03','DEV-13','DEV-15','DEV-16','DEV-51','DEV-52','DEV-53','DEV-54','DEV-56'}
DETAIL=('PH补入原生注册/独立秘密加载、双钥环、专用恢复宿主、实际退出，以及既有本人create/get两个HTTP和正式API/worker入口源码。'
 '当前50控制HTTP/98规划/74表，无新SQL。12项新测试定义未编译/运行。SDK单Proof/A402、实际小程序PayLauncher、'
 '原资金唯一消费/当前退款设备截止准入、完整获准退款到账佣金对账、原付费续租/可信M token或小时接受账务、'
 '真实秘密/个人seller/渠道/设备与全量验收仍待。全部56任务222子项保持开放；完整生产代码、实际依赖接线和测试源码后统一验证；本次仅源码保存点，不是整轮完成。')

def load(name):return json.loads((ROOT/name).read_text(encoding='utf-8'))
def write(name,text):
 p=ROOT/name;old=p.read_bytes() if p.exists() else b'';ending='\r\n' if b'\r\n' in old else '\n'
 p.parent.mkdir(parents=True,exist_ok=True);p.write_bytes(text.replace('\r\n','\n').replace('\n',ending).encode('utf-8'))
def dump(name,value):write(name,json.dumps(value,ensure_ascii=False,indent=2)+'\n')
def section(name,marker,content):
 p=ROOT/name;body=p.read_text(encoding='utf-8') if p.exists() else ''
 if marker in body:
  start=body.index(marker);end=body.find('\n## ',start+len(marker));body=body[:start].rstrip()+('\n\n'+body[end+1:] if end>=0 else '')
 write(name,body.rstrip()+'\n\n'+marker+'\n\n'+content.strip()+'\n')

def main():
 plan,stage,status=[load(n) for n in ['dev/progress/code-first-plan.json','dev/progress/R026-code-stage.json','dev/progress/implementation-status.json']]
 tests=[]
 for source in TESTS:
  for name in re.findall(r'^func (Test\w+)\(', (ROOT/source).read_text(encoding='utf-8'),re.M):
   tests.append({'id':f'PH-T{len(tests)+1:03d}','name':name,'source':source,'status':'SOURCE_ONLY_NOT_EXECUTED'})
 integration={'status':'SOURCE_ONLY_UNVERIFIED','requirements':'PH-001—040','annex':ANNEX,
  'explicit_root_registry_secret_keyring_source_wired':True,'dedicated_worker_and_owned_shutdown_source_wired':True,
  'original_create_get_http_source_wired':True,'api_and_worker_entrypoint_source_wired':True,
  'standard_sdk_proof_a402_paylauncher_wired':False,'uniquely_usable_funds_and_business_admission_complete':False,
  'full_refund_payout_commission_statement_and_paid_renewal_complete':False,
  'real_secret_acl_channel_seller_or_device_qualified':False,'compiled_or_executed':False,'migration_executed':False,
  'source_control_operations':98,'source_catalog_tables':74,'control_http_operations':50,
  'test_definition_count':len(tests),'full_development_round_complete':False,
  'remaining':['PH-033：原标准SDK/单Proof/A402及安全原恢复','PH-034：实际获准PayLauncher与原前后台/取消恢复',
   'PH-035：原资金用途唯一消费及当前合同/主体/退款/设备/硬截止准入','PH-036：完整获准退款/到账/佣金/对账单/资格和异常工作台',
   'PH-037：原付费续租、可信token/小时水位、接受和平衡追加账','PH-038：原生秘密ACL/轮换保留/销毁、跨进程崩溃及撤销与外部签名实证',
   'PH-039：真实个人卖家/服务/代调用/退款费用与支付宝/设备组合','PH-040：全部56/222代码接线及测试源码后统一验证；DEV-55延后/增强无证明关闭']}
 for record in (plan,stage):
  record['machine_payment_host_source_integration']=deepcopy(integration)
  for key in ['machine_pay_source_integration','machine_payment_transaction_source_integration']:
   record[key].update({'native_host_and_create_get_source_annex':ANNEX,'native_host_and_two_http_source_written':True,'full_sdk_paylauncher_and_unique_funds_wired':False})
 for task in plan['tasks']:
  if task['id'] in TASKS:
   task['code_status']='IN_PROGRESS'
   for step in task['steps']:
    step['code_status']='IN_PROGRESS';step['implementation_refs']=sorted(set(step.get('implementation_refs',[])+REFS))
 for task in status['tasks']:
  if task['id'] in TASKS:
   task['implementation_status']='IN_PROGRESS';task['machine_host_source_limit']=DETAIL;task['current_validation_status']='NOT_EXECUTED'
   if task['id'] in {'DEV-13','DEV-15'}:task['remaining_detail']=DETAIL
 status['activity_source']='2026-11-06 CODE_FIRST：PH-001—032 / IMP066—070；'+DETAIL
 for number,title,state in ITEMS:
  entry={'id':'IMP-R026-'+number,'title':title,'status':state}
  stage['implemented_items']=[old for old in stage['implemented_items'] if old['id']!=entry['id']]+[entry]
 stage['source_files']=sorted(set(stage.get('source_files',[])+REFS));stage['tasks']=deepcopy(status['tasks'])
 stage['machine_host_test_definitions']={'tests':tests,'ids':[t['id'] for t in tests],'status':'SOURCE_ONLY_NOT_EXECUTED'}
 for name,value in [('dev/progress/code-first-plan.json',plan),('dev/progress/R026-code-stage.json',stage),('dev/progress/implementation-status.json',status)]:dump(name,value)
 progress(plan,stage,status,tests)
 documentation(tests)
 print(f'Document/source generation only: IMP066–070 / PH001–040 / {len(tests)} new unexecuted test definitions / 50 HTTP / 98 planned / 74 tables. No compiler/test/validator/migration/runtime; all 56/222 open.')

def progress(plan,stage,status,tests):
 counts=Counter(t['implementation_status'] for t in status['tasks'])
 lines=['# R026 全量代码阶段进展','','**进行中，SOURCE_ONLY / 未验证。** 全量生产代码、实际依赖接线和测试源码后统一验证。新增066—070仅源码保存点，不是整轮完成；历史记录按当时范围阅读。','',
  '| 编号 | 已写内容 | 实际状态 |','|---|---|---|']
 lines += [f"| {i['id']} | {i['title']} | {i['status']} |" for i in stage['implemented_items']]
 lines += ['',f'新增[PH-001—040](../../{ANNEX})与{len(tests)}项未执行测试定义。原RN67/CE31/MW44/CH29/WS23/MS27/MG37/MP49/PT16也未编译/运行。当前50控制HTTP/98规划操作/74平台表，无SQL变更；016仍未迁移。','',
  '当前状态：'+'；'.join(f'{k} {v}' for k,v in sorted(counts.items()))+'；全部222子项未验收。','',
  '| 编号 | 全部任务 | 当前状态 | 全部仍待收口子项 |','|---|---|---|---|']
 current={t['id']:t for t in status['tasks']}
 for task in plan['tasks']:
  t=current[task['id']];lines.append(f"| {task['id']} | {task['title']} | {t['implementation_status']} | {'、'.join(t['remaining_step_ids'])} |")
 write('dev/progress/R026-全量代码阶段进展.md','\n'.join(lines)+'\n')
 lines=['# 全量代码优先实施清单','',DETAIL,'','先全部生产代码/实际依赖接线/测试源码，再统一验证。代码状态和验收分开，全部222子项未验收。','',
  '| 子项 | 任务 | 未收口需求 | 代码状态 | 验收 |','|---|---|---|---|---|']
 for task in plan['tasks']:
  for step in task['steps']:lines.append(f"| {step['id']} | {task['title']} | {step['code_requirement']} | {step['code_status']} | {step['acceptance_status']} |")
 write('dev/全量代码优先实施清单.md','\n'.join(lines)+'\n')

def documentation(tests):
 section(ANNEX,'## PH 测试源码定义索引','| 编号 | 定义 | 源码 | 状态 |\n|---|---|---|---|\n'+'\n'.join(f"| {t['id']} | {t['name']} | {t['source']} | 未编译/未运行 |" for t in tests))
 zh=f'R026新增[PH-001—040]({ANNEX})：原生显式注册/独立秘密/双钥环、有界文件与原scope、专用恢复和到期原退役宿主、实际join后关闭、既有本人支付create/get两个HTTP及正式API/worker入口源码。当前50控制HTTP/98规划/74平台表，无SQL变更；{len(tests)}项新测试定义及既有全部R026源码均未编译/运行。SDK/Proof/A402/实际PayLauncher、原资金唯一消费/当前退款设备截止准入、完整渠道退款到账佣金对账/续租/可信计量接受账务与真实资格仍待。全部56/222开放，完整生产代码/实际接线/测试源码后统一验证；本次不是开发轮完成。'
 en=f'R026 adds [PH-001–040]({ANNEX}): explicit native registration, independent secrets and proof/receipt keyrings, bounded rooted files, dedicated original recovery/expiry, actual joins before key release, two existing authenticated payment create/get HTTP operations and API/worker entrypoints. Current source: 50 control HTTP operations/98 planned operations/74 platform tables; no SQL changes. All {len(tests)} new test definitions and earlier R026 source remain uncompiled/unexecuted. SDK/Proof/A402/PayLauncher, unique funds/current refund-device-deadline admission, full qualified refunds/payouts/fees/statements/renewal/trusted metering/acceptance/ledger and real qualification remain open. All 56 tasks/222 steps are unaccepted. Complete all production source, actual wiring and test source before batch validation; this is not a completed round.'
 marker='## R026 PH-001—040 原生支付宿主与本人接口 / Native payment host and owner HTTP source'
 for name,content in [('dev/repository/README.zh-CN.md.in',zh),('dev/repository/README.en.md.in',en)]:
  section(name,marker,content);body=(ROOT/name).read_text(encoding='utf-8')
  body=re.sub(r'\n<!-- R026-[A-Z-]+-CURRENT -->.*?<!-- /R026-[A-Z-]+-CURRENT -->\n','\n',body,flags=re.S)
  body=body.replace('以上 PT 说明','以上 PH 说明').replace('The PT notice above','The PH notice above')
  body=re.sub(r'\n{3,}','\n\n',body);title,rest=body.split('\n',1)
  write(name,title+'\n\n<!-- R026-PH-CURRENT -->\n'+content+'\n<!-- /R026-PH-CURRENT -->\n'+rest)
 for name,content in [('CHANGELOG.zh-CN.md',zh),('CHANGELOG.md',en)]:
  body=(ROOT/name).read_text(encoding='utf-8');body=re.sub(r'(?m)^- R026(?:新增| adds )\[PH-001[—–]040\].*\n','',body)
  title,rest=body.split('\n',1);write(name,title+'\n\n- '+content+'\n'+rest)
 for name in ['dev/repository/USAGE.md','dev/repository/SYSTEM_DESIGN.md']:section(name,marker,zh.replace('(docs/','(../../docs/')+'\n\n'+en.replace('(docs/','(../../docs/'))
 for name,prefix in [('dev/src/internal/payments/README.md','../../../../'),('dev/progress/README.md','../../')]:section(name,marker,zh.replace('(docs/','('+prefix+'docs/'))
 for name in ['docs/技术开发/00-技术文档导航与实施基线.md','docs/技术开发/13-支付退款账务与渠道结算.md','docs/技术开发/附录-MachinePay支付事务与原凭据恢复源码.md','docs/技术开发/附录-MachinePay原支付边界与凭证恢复源码.md','docs/技术开发/附录-本轮范围与端侧选型修订.md']:
  section(name,marker,zh.replace('(docs/技术开发/','('))
 for name in ['AGENTS.md','dev/AGENTS.md']:
  section(name,marker,zh.replace('(docs/','(../docs/' if name.startswith('dev/') else '(docs/')+'\n\n当前唯一增量整理器为'+TOOL+'；不得重跑旧PT/MP/MG/MS/WS/CH/CE/MW/RN整理器覆盖PH状态。001—016平台双份SQL和已登记端侧DDL/source.json原字节冻结，只追加前向017+。当前不编译/测试/核验/迁移/运行DB、内核、模型、设备或浏览器；无本地虚拟化，DEV-55延后、DEV-32无证明关闭；浏览器用后关不用页面。')
 profile=load('dev/src/internal/payments/channel-profile.json');profile.update({'native_host_source_annex':ANNEX,'native_host_and_api_worker_entrypoints_source_wired':True,'public_payment_handlers_wired':True,'standard_sdk_paylauncher_wired':False,'real_channel_or_merchant_qualified':False,'compiled_or_executed':False});dump('dev/src/internal/payments/channel-profile.json',profile)

if __name__=='__main__':main()
