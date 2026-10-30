"""生成每轮已实现表和完整待办表，不从源码文件数推导任务完成。"""
from __future__ import annotations
from collections import Counter
import json
import os
from pathlib import Path

ROOT=Path(__file__).resolve().parents[2]
DEV=ROOT/'dev'

def table(headers,rows):
    def cell(v):return str(v).replace('|','&#124;').replace('\n','<br>')
    return '\n'.join(['| '+' | '.join(headers)+' |','| '+' | '.join('---' for _ in headers)+' |']+['| '+' | '.join(map(cell,row))+' |' for row in rows])+'\n'
def relative(target,current,label):
    return '['+label+']('+Path(os.path.relpath(ROOT/target,(ROOT/current).parent)).as_posix()+')'
def main():
    baseline=json.loads((DEV/'contracts/development-plan-v1.0.json').read_text(encoding='utf-8'))
    progress=json.loads((DEV/'progress/implementation-status.json').read_text(encoding='utf-8'))
    goal_status=progress['goal_status']
    if goal_status not in {'ACTIVE','PAUSED','COMPLETE','BLOCKED'}:raise ValueError('未知目标状态')
    source={t['id']:t for t in baseline['tasks']}
    actual={t['id']:t for t in progress['tasks']}
    if set(source)!=set(actual) or len(actual)!=56 or len(progress['tasks'])!=56:raise ValueError('56 项任务必须全部且唯一登记')
    states={'NOT_STARTED','IN_PROGRESS','COMPLETED','CONDITIONAL','DEFERRED'}
    def verify_tasks(tasks):
        if len(tasks)!=56 or {t['id'] for t in tasks}!=set(source):raise ValueError('每轮快照必须覆盖全部56项')
        for t in tasks:
            if t['implementation_status'] not in states:raise ValueError('未知实现状态')
            if len(t['remaining_step_ids'])!=len(set(t['remaining_step_ids'])) or not set(t['remaining_step_ids'])<={s['id'] for s in source[t['id']]['steps']}:raise ValueError('未知或重复开发子项')
            if t['implementation_status']=='COMPLETED' and (t['remaining_step_ids'] or not t['evidence_refs']):raise ValueError('不能无证据清空待办')
    def verify_file(path):
        candidate=(ROOT/path).resolve()
        if not candidate.is_relative_to(ROOT.resolve()) or not candidate.is_file():raise ValueError('产物/证据缺失或越界：'+path)
    verify_tasks(progress['tasks'])
    implemented_ids=set()
    for round_ in progress['rounds']:
        for item in round_['implemented']:
            if item['id'] in implemented_ids:raise ValueError('实现编号重复')
            implemented_ids.add(item['id'])
            if not set(item['task_ids'])<=set(source):raise ValueError('实现关联未知任务')
            for artifact in item['artifacts']:verify_file(artifact)
        for evidence in round_['evidence_refs']:verify_file(evidence)
        if round_['verification_status']!='PENDING' and not round_['evidence_refs']:raise ValueError('验证状态必须关联实际证据')
    for t in progress['tasks']:
        if not set(t['completed_components'])<=implemented_ids:raise ValueError('任务引用未知实现编号')
    for round_ in progress['rounds']:
        tasks=round_.get('task_snapshot',progress['tasks'])
        verify_tasks(tasks)
        path='dev/progress/'+round_['id']+'-本轮实现与完整待办.md'
        text='# '+round_['id']+' 本轮实现与完整待办\n\n'+round_['title']+'。本表保留该轮真实实现及当时待办；当前源码目标状态见进度入口，不能替代全套产品验收。\n\n'
        text+='## 本轮已实现内容编号表\n\n'
        text+=table(['实现编号','对应任务','实现内容','源码/产物','验证状态'],[[i['id'],'、'.join(i['task_ids']) or '全局进度',i['title'],'；'.join(relative(a,path,Path(a).name) for a in i['artifacts']),i['verification']] for i in round_['implemented']])+'\n'
        text+='## 本轮验证证据\n\n'
        text+='核验状态：'+round_['verification_status']+'。'+('；'.join(relative(a,path,Path(a).name) for a in round_['evidence_refs']) if round_['evidence_refs'] else '实际测试证据尚待取得。')+'\n\n'
        text+='## 整体待实现与完善事项编号表\n\n覆盖全部 56 顶层任务。子项尚有任何缺口就保留编号；硬件增强独立条件分支与明确延后项不计入当前完成。\n\n'
        rows=[]
        for t in tasks:
            base=source[t['id']];steps={s['id']:s for s in base['steps']}
            pending='；'.join(sid+' '+steps[sid]['title'] for sid in t['remaining_step_ids']) or '已实现，继续保留适用发布/产品验收门槛'
            if t.get('remaining_detail'):pending=t['remaining_detail']+'<br>待收口编号：'+ '、'.join(t['remaining_step_ids'])
            rows.append([t['id'],base['title'],t['implementation_status'],'、'.join(t['completed_components']) or '无',pending])
        text+=table(['任务编号','任务','当前实现状态','已有实现编号','全部待实现/完善子项'],rows)+'\n'
        text+='## 交付边界\n\n原生数据库/服务测试不证明支付宝 OAuth/支付渠道、GPU/媒体、Android、密码互通/独立评审或完整产品已通过。未取得证据的组合保持关闭。所有后续代码仍在 dev，运行数据在项目 runtime；不使用本地虚拟化。\n'
        (ROOT/path).write_text(text,encoding='utf-8')
    path='dev/progress/README.md'
    text='# 开发实施进度\n\n目标：按全部开发细则交付成体系源码。当前源码目标 '+goal_status+'，源码实施进展与需求/设计工作分别记录。\n\n'
    if progress.get('current_activity')=='REQUIREMENTS_NB_R1':
        text+='当前推进[NB-R1设计配套需求](../../docs/需求推进/NB-1.0/README.md)：48项子需求、96个验收场景、六项契约缺口均已定义；没有新增源码实现或真实运行验收。\n\n'
    elif progress.get('current_activity')=='SOURCE_IMPLEMENTATION_R003':
        text+='R003已落实NB价版、市场资格、报价与建单应用服务；22项原生测试通过，临时数据库已停止。尚无公开HTTP/登录、真实支付/设备或交易页面。六项NB缺口整体仍开放，80项主验收及96个完整子场景未执行。\n\n'
    elif progress.get('current_activity')=='SOURCE_IMPLEMENTATION_R004':
        text+='R004已实现支付宝RSA2授权适配、一次性挑战/会话和13操作原生HTTP子集；34项实际局部测试通过，临时数据库已停止。渠道响应为明确合成签名测试，真实支付宝授权/收款、GPU/Android、前端页面及完整转发仍待完成；93操作、62表沿固定编号继续开发。全部56项/222未收口子项保留，六项NB缺口开放，80项主验收及96完整场景仍未执行。\n\n'
    elif progress.get('current_activity')=='SOURCE_IMPLEMENTATION_R005':
        text+='R005已实现本人卖家申请、同意与未来挂牌控制：23/93操作、64表；43项实际局部测试通过，临时数据库已停止。申请/提交不自批或自动开售，真实审核/渠道/设备/前端和完整执行链仍待完成；全部56任务/222未收口子项保留，80主验收及96完整场景未执行。\n\n'
    elif progress.get('current_activity')=='SOURCE_IMPLEMENTATION_R006':
        text+='R006已实现React/Taro支付宝编译页面、React/Vite Web和共享控制逻辑，实际交易到等待付款，卖家页面只读、配对/付款关闭。45项Go用例（含Web/Taro H5浏览器子场景）及13项TS用例通过，原请求恢复后各一单一事件；浏览器与临时数据库已停止。支付宝原生只编译，真实授权/设备/资金/密码和完整平台仍未验收；23/93操作、64表，全部56任务/222未收口子项、六项NB缺口和80/96未执行状态保留。\n\n'
    elif progress.get('current_activity')=='SOURCE_IMPLEMENTATION_R007':
        text+='R007已实现一次性配对/登记恢复、设备逐请求签名与持久nonce、单调心跳、共享策略/停止撤销和可复用原生控制库；31/93操作、65表。60项Go局部用例及既有13项TS/三目标构建证据、双端实际浏览器回归通过，临时数据库/自建浏览器关闭。完整原生执行端/系统密钥存储/探测/资格/内核/资金仍待完成；停止撤销不释放在途owner/代次，配对与心跳不自授可售。全部56任务/222未收口子项、六项NB缺口和80/96未执行状态保留。\n\n'
    elif progress.get('current_activity')=='SOURCE_IMPLEMENTATION_R008':
        text+='R008已交付p11-node原生前台控制、Windows DPAPI/Linux Secret Service适配、加密单命令日志与OS进程锁，以及跨重启原登记/心跳恢复。72项Go局部、实际Windows三命令CLI及双端浏览器通过；Linux钥库仅线协议夹具/交叉编译。当前31/93操作、65表，001—005原字节不改；只报UNKNOWN、不开执行/监听/自启，不把控制代理当完整RuntimeSupervisor/执行inbox/资源门闩。完整内核/内容加密/真实设备渠道仍待完成；全部56任务/222子项、80主验收/96完整NB未执行和DEV-55延后保留。\n\n'
    elif progress.get('current_activity')=='SOURCE_IMPLEMENTATION_R009':
        text+='R009已将一次性设备配对、本人设备分页、共享条件及停止/撤销接入React/Taro和React/Vite，配对凭据仅页内存，后台/离开/到期/撤权清除，原请求恢复保留方法/键/正文/强ETag。72项Go、21项TS、三目标构建及双端实际浏览器/Windows控制二进制联调通过；H5不冒称原生容器，控制在线不授执行资格。31/93操作、65表及001—005原字节不变；完整执行/内核/内容加密/真实渠道待完成，全部56任务/222子项、80/96未执行和DEV-55延后保留。\n\n'
    text+=table(['轮次','内容与完整待办','验证状态'],[[r['id'],relative('dev/progress/'+r['id']+'-本轮实现与完整待办.md',path,r['title']),r['verification_status']] for r in progress['rounds']])+'\n'
    counts=Counter(t['implementation_status'] for t in progress['tasks'])
    text+='任务现状：'+ '；'.join(k+'='+str(v) for k,v in sorted(counts.items()))+'。\n\n'
    text+=relative('dev/progress/implementation-status.json',path,'人工维护的机器进度')+'；'+relative('dev/开发细则总表.md',path,'完整开发规范')+'。\n'
    (ROOT/path).write_text(text,encoding='utf-8')
    print(json.dumps({'tasks':56,'rounds':len(progress['rounds']),'counts':dict(counts),'goal':goal_status},ensure_ascii=False))

if __name__=='__main__':main()
