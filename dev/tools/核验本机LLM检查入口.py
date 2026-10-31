"""用当前 Windows 节点程序检查真实固定包；不启动推理或开放执行。"""
from pathlib import Path
import hashlib
import json
import subprocess

ROOT=Path(__file__).resolve().parents[2]
def sha(path):
    with path.open('rb') as f:return hashlib.file_digest(f,'sha256').hexdigest()
def main():
    native=json.loads((ROOT/'dev/tests/evidence/R025-native-source-checks.json').read_text(encoding='utf-8'))
    binary=next(a for a in native['artifacts'] if a['path']=='runtime/bin/p11-node.exe')
    executable=ROOT/binary['path']
    if native['status']!='PASSED_WITH_LIMITED_SCOPE' or sha(executable)!=binary['sha256']:raise RuntimeError('current native node binary required')
    prepared=json.loads((ROOT/'runtime/native-llama/preparation.json').read_text(encoding='utf-8'))
    selection={'capability':'cap_local_inspection_only','resource':'physical_gpu_0','gpu_device':'CUDA0','gpu_uuid':'GPU-c101fc79-ced0-9579-92ab-a20c8dcd8a21','gpu_pci':'0000:01:00.0','context_tokens':2048,'batch_tokens':128,'threads':2,'gpu_layers':999}
    parameters={'GPUDevice':'CUDA0','GPUUUID':'GPU-c101fc79-ced0-9579-92ab-a20c8dcd8a21','GPUPCI':'0000:01:00.0','ContextTokens':2048,'BatchTokens':128,'Threads':2,'GPULayers':999}
    # Controlled ASCII fixture, matching PackageEnvironment's declared field order.
    environment=hashlib.sha256(b'P11_LLAMA_ENV_2\0'+prepared['package_sha256'].encode()+b'\0'+json.dumps(parameters,separators=(',',':')).encode()).hexdigest()
    request={'package':prepared['package'],'package_sha256':prepared['package_sha256'],'environment':environment,'selection':selection}
    tests=[('valid',request,0),('environment_mismatch',dict(request,environment='0'*64),2),('unknown_input',dict(request,prompt='inspection-must-not-echo-this'),2),('oversize','x'*4097,2)]
    records=[]
    for name,value,expected in tests:
        data=value if isinstance(value,str) else json.dumps(value,ensure_ascii=False)
        result=subprocess.run([str(executable),'kernel-inspect','--runtime-root',str(ROOT/'runtime')],input=data.encode(),capture_output=True,timeout=45)
        output=result.stdout.decode('utf-8'); diagnostic=result.stderr.decode('utf-8')
        passed=result.returncode==expected and 'inspection-must-not-echo-this' not in output+diagnostic
        view=None
        if name=='valid':
            try: view=json.loads(output)
            except json.JSONDecodeError: view=None
            if not isinstance(view,dict):
                records.append({'case':name,'exit_code':result.returncode,'passed':False,'safe_view':None,'diagnostic':diagnostic.strip()});continue
            passed=passed and view['execution_enabled'] is False and view['qualification']=='NOT_GRANTED' and view['environment']==environment and view['selection']==selection
        else: passed=passed and output=='' and diagnostic.strip()=='local kernel inspection rejected'
        records.append({'case':name,'exit_code':result.returncode,'passed':passed,'safe_view':view,'diagnostic':diagnostic.strip()})
    proposal=ROOT/'runtime/native-llama/inspection-proposal.json';proposal.write_text(json.dumps(request,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
    report={'round':'R025','passed':all(r['passed'] for r in records),'binary':binary['path'],'binary_sha256':sha(executable),'cases':records,'package_sha256':prepared['package_sha256'],
      'proposal_is_approval':False,'inference_executed':False,'execution_enabled':False,'virtualization_used':False,'sources':{p.relative_to(ROOT).as_posix():sha(p) for p in [Path(__file__).resolve(),ROOT/'dev/src/client/cmd/p11-node/kernel.go']}}
    (ROOT/'dev/tests/evidence/R025-native-kernel-cli.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
    print(json.dumps({'passed':report['passed'],'cases':len(records),'execution_enabled':False}))
    return 0 if report['passed'] else 1
if __name__=='__main__':raise SystemExit(main())
