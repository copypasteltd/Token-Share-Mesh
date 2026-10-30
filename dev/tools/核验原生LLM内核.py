"""真实启动本项目原生 worker＋合成 C ABI DLL；不运行真实模型/GPU。"""
from __future__ import annotations
from datetime import datetime, timezone
import hashlib
import json
from pathlib import Path
import subprocess
from 原生开发 import ROOT, environment

def sha(path: Path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()

def main():
    binary, env = environment();env['GOPROXY'] = 'off'
    build = ROOT / 'runtime/native-worker/build'
    env.update(P11_NATIVE_RUNTIME_ROOT=str(ROOT/'runtime'),P11_NATIVE_WORKER_EXE=str(build/'p11-llama-worker.exe'),P11_NATIVE_FIXTURE_DLL=str(build/'fixture/llama-fixture.dll'))
    evidence = ROOT / 'dev/tests/evidence'
    raw = evidence/'R022-native-capi-fixture-tests.jsonl'
    started = datetime.now(timezone.utc).isoformat()
    with raw.open('wb') as output:
        result = subprocess.run([str(binary),'test','-count=1','-timeout=90s','-tags=p11_native_capi_fixture','-run=^TestNativeCAPI','-json','./client/adapters/llamaworker'],cwd=ROOT/'dev/src',env=env,stdout=output,stderr=subprocess.STDOUT,timeout=180)
    events = []
    for line in raw.read_text(encoding='utf-8').splitlines():
        try: events.append(json.loads(line))
        except json.JSONDecodeError: pass
    cases = {e['Test']:e['Action'] for e in events if e.get('Test') and '/' not in e['Test'] and e['Action'] in ('pass','fail','skip')}
    paths = [p for d in ['dev/src/client/adapters/llamaworker','dev/src/client/kernels/llama-worker','dev/tests/native-worker','dev/src/client/results','dev/src/client/protocol/textoutput','dev/src/client/supervisor'] for p in (ROOT/d).glob('*') if p.is_file()]
    paths += [Path(__file__).resolve(),ROOT/'dev/tools/构建原生LLM内核.py',build/'p11-llama-worker.exe',build/'fixture/llama-fixture.dll']
    passed=result.returncode==0 and len(cases)==6 and all(v=='pass' for v in cases.values())
    receipt = {'round':'R022','recorded_at':started,'returncode':result.returncode,'passed':passed,'scope':'actual Windows process/pipe/C ABI call-order using synthetic DLL only; no real llama runtime/model/GPU/LP/E2E qualification','virtualization_used':False,'cases':cases,'raw':raw.relative_to(ROOT).as_posix(),'raw_sha256':sha(raw),'sources':{p.relative_to(ROOT).as_posix():sha(p) for p in paths},'owned_worker_exit_waited_by_adapter':passed}
    target=evidence/'R022-native-capi-fixture-summary.json';target.write_text(json.dumps(receipt,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
    print(json.dumps({k:receipt[k] for k in ('passed','returncode','cases')}))
    return 0 if receipt['passed'] else 1

if __name__=='__main__':raise SystemExit(main())
