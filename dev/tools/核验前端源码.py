"""汇总已实际执行的双端编译、共享用例、浏览器和媒体摘要，不替代原生真机验收。"""
from __future__ import annotations
import argparse
import hashlib
import json
from pathlib import Path
import re
import shutil
import subprocess

ROOT = Path(__file__).resolve().parents[2]
EVIDENCE = ROOT / 'dev/tests/evidence'
def sha(p): return hashlib.sha256(p.read_bytes()).hexdigest()
def main():
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument("--round",default="R010",choices=["R006","R007","R008","R009","R010"]);round_id=parser.parse_args().round
    EVIDENCE.mkdir(parents=True, exist_ok=True)
    records = []
    for action in ['bootstrap', 'check', 'test', 'build']:
        r = json.loads((ROOT / f'runtime/frontend/{action}-receipt.json').read_text(encoding='utf-8'))
        if r['exit_code'] != 0 or r['node'] != 'v22.14.0' or r['virtualization_used']: raise ValueError('实际原生前端执行记录未通过')
        for f, digest in r['source_sha256'].items():
            if action == 'bootstrap' and not (f.endswith('/package.json') or f.endswith('/package-lock.json')): continue
            if sha(ROOT / f) != digest: raise ValueError('前端源码已变化，需重跑相应检查：' + action + ':' + f)
        for f, digest in r['build_sha256'].items():
            if sha(ROOT / f) != digest: raise ValueError('前端构建输出已变化')
        for i, c in enumerate(r['commands'], 1):
            if c['exit_code'] or sha(ROOT / c['log']) != c['log_sha256']: raise ValueError('实际前端命令/日志不匹配')
            dest = EVIDENCE / f'{round_id}-frontend-{action}-{i}.log'
            shutil.copyfile(ROOT / c['log'], dest)
            c['evidence_log'] = dest.relative_to(ROOT).as_posix()
        records.append(r)
    unit_count = 21 if round_id in {'R009','R010'} else 13
    unit = next(r for r in records if r['action'] == 'test')
    unit_log = (ROOT / unit['commands'][0]['evidence_log']).read_text(encoding='utf-8')
    if not all(t in unit_log for t in [f'# tests {unit_count}', f'# pass {unit_count}', '# fail 0', '# skipped 0']): raise ValueError('实际共享用例未齐全通过')
    browsers = []
    assets = []
    for target in ['web', 'miniapp']:
        f = EVIDENCE / f'{round_id}-{target}-browser.json'
        r = json.loads(f.read_text(encoding='utf-8'))
        if not r['passed'] or not r['browser_closed'] or r['remaining_owned_tabs'] != 0 or r['virtualization_used'] or not r['recording']: raise ValueError('双端浏览器/清理/录制未通过')
        # Captures must use precisely the build receipt being published.
        if r['build_receipt_sha256']['runtime/frontend/build-receipt.json'] != sha(ROOT / 'runtime/frontend/build-receipt.json'): raise ValueError('截图不是当前编译产物')
        if round_id in {'R009','R010'}:
            for action in ['check', 'test']:
                name = f'runtime/frontend/{action}-receipt.json'
                if r['build_receipt_sha256'][name] != sha(ROOT / name): raise ValueError('页面验证与当前检查/用例记录不一致')
            native = json.loads((EVIDENCE / f'{round_id}-native-source-checks.json').read_text(encoding='utf-8'))
            binary = next(a for a in native['artifacts'] if a['path'] == 'runtime/bin/p11-node.exe')
            commands = r.get('provider_native_commands', [])
            recovery = r.get('provider_recovery', {})
            if (r.get('provider_native_binary_sha256') != binary['sha256'] or
                [(c['action'], c['exit_code']) for c in commands] != ([('init', 0), ('enroll', 0)] + ([('execution-init', 0), ('execution-status', 0)] if round_id == 'R010' else []) + [('heartbeat', 0), ('heartbeat', 1)]) or
                not r.get('provider_native_processes_exited') or not r.get('provider_device_id') or
                any(c['safe_status']['execution_enabled'] for c in commands) or
                len({c['safe_status']['key_fingerprint'] for c in commands if 'key_fingerprint' in c['safe_status']}) != 1 or
                not recovery.get('original_pair_preserved') or not recovery.get('original_policy_preserved') or
                recovery.get('pair_requests') != 3 or recovery.get('policy_requests') != 2 or recovery.get('policy_if_match') != '"1"' or
                any(recovery.get(k) is not False for k in ['credentials_persisted', 'credential_rendered_or_recorded', 'native_engine_started'])):
                raise ValueError('实际提供方双端/原生命令/原请求恢复证据未通过')
            if round_id == 'R010' and r.get('provider_execution_metadata') != {'initialized_once':True,'reopened_same_anchor':True,'native_sqlite':True,'no_resources_qualified':True,'content_received':False}: raise ValueError('实际原生执行元数据库初始化/重开证据未通过')
            if len(r['screenshots']) != 7: raise ValueError('提供方共享条件及撤销页面截图未齐全')
        entries = [*r['screenshots'], r['recording']['mp4'], r['recording']['gif']]
        for entry in entries:
            p = ROOT / entry['path']
            if sha(p) != entry['sha256']: raise ValueError('实际媒体摘要不符')
            assets.append(entry)
        mp4 = ROOT / r['recording']['mp4']['path']
        probe = subprocess.run(['ffprobe','-v','error','-show_format','-show_streams','-of','json',str(mp4)], capture_output=True, encoding='utf-8', check=True)
        info = json.loads(probe.stdout)
        if not any(s.get('codec_name') == 'h264' and s.get('width',0)>0 for s in info['streams']) or float(info['format']['duration']) <= 0: raise ValueError('实际视频探测失败')
        subprocess.run(['ffmpeg','-v','error','-i',str(mp4),'-f','null','-'], capture_output=True, check=True)
        browsers.append({'target': target, 'evidence': f.relative_to(ROOT).as_posix(), 'sha256': sha(f), 'video_probe': info})
    if round_id in {'R009','R010'} and len(assets) != 18: raise ValueError('当前14张截图/4项录制文件未齐全')
    lock = json.loads((ROOT / 'dev/src/package-lock.json').read_text(encoding='utf-8'))
    packages = [{'path': name, **{k:v for k,v in value.items() if k in ['version','resolved','integrity','license','link','dev','optional']}} for name,value in sorted(lock['packages'].items())]
    inventory = ROOT / 'dev/licenses/frontend-dependencies.json'
    inventory.write_text(json.dumps({'scope': 'Exact npm lock metadata including optional/build dependencies; declared licenses, not a complete legal approval or full product SBOM', 'lock_sha256': sha(ROOT / 'dev/src/package-lock.json'), 'packages': packages},ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
    report = {'round':round_id,'status':'PASSED_FOR_FRONTEND_CONTROL_SUBSET_ONLY','unit_cases':unit_count,'failed_cases':0,'skipped_cases':0,'build_targets':['alipay','h5','web'],'records':records,'browser_checks':browsers,'dependency_inventory':inventory.relative_to(ROOT).as_posix(),
        'source_sha256':{p.relative_to(ROOT).as_posix():sha(p) for folder in ['dev/src/frontend','dev/src/miniapp','dev/src/web'] for p in sorted((ROOT / folder).rglob('*')) if p.is_file()},
        'virtualization_used':False,'product_acceptance_status':'NOT_EXECUTED','limitations':['Native Alipay compilation only; H5 uses browser cookies, not native authorization','Synthetic OAuth, qualifications and device heartbeats; no real channel, device, inference or payment','Taro H5 entry-size warnings retained; native package size and performance not accepted','Order history, listing editor, real seller review and buyer gateway remain pending; provider owner controls do not qualify hardware']}
    (EVIDENCE / f'{round_id}-frontend-checks.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
    (ROOT / 'dev/media/frontend-manifest.json').write_text(json.dumps({'round':round_id,'kind':'ACTUAL_COMPILED_PRODUCT_UI_WITH_EXPLICIT_SYNTHETIC_FIXTURES','evidence':f'dev/tests/evidence/{round_id}-frontend-checks.json','source_sha256':report['source_sha256'],'assets':assets,'browser_closed':True,'remaining_owned_tabs':0,'virtualization_used':False,'notice':'Web and Taro H5 real HTTP/DB UI; not native Alipay, real sellers/authorization, payment or device execution'},ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
    print(json.dumps({'passed':True,'unit_cases':unit_count,'browser_targets':2,'assets':len(assets),'scope':report['status']},ensure_ascii=False))
if __name__ == '__main__': main()
