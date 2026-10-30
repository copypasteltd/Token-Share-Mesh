"""用当前原生节点验证只读显卡探测；不获取资源槽、不加载模型。"""
from pathlib import Path
import hashlib
import json
import shutil
import subprocess

ROOT=Path(__file__).resolve().parents[2]
def sha(path):
    with path.open('rb') as f:return hashlib.file_digest(f,'sha256').hexdigest()
def main():
    source=json.loads((ROOT/'dev/tests/evidence/R024-native-source-checks.json').read_text(encoding='utf-8'))
    artifact=next(a for a in source['artifacts'] if a['path']=='runtime/bin/p11-node.exe')
    binary=ROOT/artifact['path']
    if source['status']!='PASSED_WITH_LIMITED_SCOPE' or sha(binary)!=artifact['sha256']:raise RuntimeError('current node binary required')
    prepared=json.loads((ROOT/'runtime/native-llama/preparation.json').read_text(encoding='utf-8'))
    request={'package':prepared['package'],'package_sha256':prepared['package_sha256']}
    smi=shutil.which('nvidia-smi.exe')
    observation=subprocess.run([smi,'--query-gpu=uuid,pci.bus_id,name','--format=csv,noheader,nounits'],capture_output=True,text=True,check=True,timeout=10).stdout.strip()
    uuid,pci,name=[s.strip() for s in observation.split(',')]
    if pci.startswith('00000000:'):pci=pci[4:]
    cases={}
    for label,payload in [('valid',request),('wrong_pin',{**request,'package_sha256':'0'*64}),('unknown_field',{**request,'execute':True}),('oversize','x'*4097)]:
        body=payload if isinstance(payload,str) else json.dumps(payload)
        result=subprocess.run([str(binary),'gpu-discover','--runtime-root',str(ROOT/'runtime')],input=body,capture_output=True,text=True,encoding='utf-8',timeout=40)
        if label=='valid':
            view=json.loads(result.stdout);items=view.get('gpus',[])
            passed=result.returncode==0 and view.get('profile')=='P11_LLAMA_PIPE_4' and view.get('execution_enabled') is False and view.get('qualification')=='NOT_GRANTED' and len(items)==1 and items[0]['gpu_uuid']==uuid and items[0]['gpu_pci']==pci.lower() and items[0]['name']==name
        else:passed=result.returncode==2 and not result.stdout and result.stderr.strip()=='local GPU discovery rejected'
        cases[label]={'passed':passed,'returncode':result.returncode}
    paths=[p for d in ['dev/src/client/adapters/llamaworker','dev/src/client/kernels/llama-worker','dev/src/shared/gpuid','dev/src/client/cmd/p11-node'] for p in (ROOT/d).glob('*') if p.is_file()]
    receipt={'round':'R024','passed':all(c['passed'] for c in cases.values()),'cases':cases,'binary':artifact,'gpus':view['gpus'],'scope':'Actual current Windows CLI and system-driver metadata only; no model/context load, execution approval, Host registration, remote attestation or marketplace/privacy/performance/funds qualification','execution_enabled':False,'model_loaded':False,'virtualization_used':False,'sources':{p.relative_to(ROOT).as_posix():sha(p) for p in [*paths,Path(__file__)]}}
    (ROOT/'dev/tests/evidence/R024-native-gpu-cli.json').write_text(json.dumps(receipt,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
    print(json.dumps({'passed':receipt['passed'],'cases':cases},ensure_ascii=False));return 0 if receipt['passed'] else 1
if __name__=='__main__':raise SystemExit(main())
