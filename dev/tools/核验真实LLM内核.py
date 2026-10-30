"""独立、显式的真实 Windows CUDA 小模型验证；不开放市场节点执行。"""
from __future__ import annotations
import argparse
from datetime import datetime, timezone
import hashlib
import importlib.util
import json
from pathlib import Path
import shutil
import subprocess

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('native_dev', ROOT/'dev/tools/原生开发.py')
native = importlib.util.module_from_spec(spec); spec.loader.exec_module(native)

def sha(path):
    with path.open('rb') as source: return hashlib.file_digest(source, 'sha256').hexdigest()

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--attempt', type=int, required=True)
    args = parser.parse_args()
    if args.attempt < 1 or args.attempt > 99: raise RuntimeError('invalid attempt')
    evidence = ROOT/'dev/tests/evidence'; prefix = f'R024-native-real-attempt{args.attempt}'
    raw = evidence/(prefix+'-tests.jsonl'); target = evidence/(prefix+'-summary.json')
    if raw.exists() or target.exists(): raise RuntimeError('attempt evidence is immutable; use next attempt')
    prepared = ROOT/'runtime/native-llama/preparation.json'
    preparation = json.loads(prepared.read_text(encoding='utf-8'))
    package = Path(preparation['package'])
    if sha(package) != preparation['package_sha256']: raise RuntimeError('prepared package changed')
    binary, env = native.environment(); env['GOPROXY'] = 'off'
    env.update(P11_NATIVE_RUNTIME_ROOT=str(ROOT/'runtime'), P11_REAL_PACKAGE=str(package), P11_REAL_PACKAGE_SHA256=preparation['package_sha256'])
    env['P11_REAL_GPU_UUID']='GPU-c101fc79-ced0-9579-92ab-a20c8dcd8a21'
    env['P11_REAL_GPU_PCI']='0000:01:00.0'
    gpu = shutil.which('nvidia-smi.exe')
    if gpu is None: raise RuntimeError('native NVIDIA hardware query unavailable')
    query = [gpu,'--query-gpu=name,driver_version,memory.total,memory.free,utilization.gpu','--format=csv,noheader,nounits']
    before = subprocess.run(query,capture_output=True,text=True,timeout=10,check=True).stdout.strip()
    started = datetime.now(timezone.utc).isoformat()
    with raw.open('xb') as output:
        try:
            result = subprocess.run([str(binary),'test','-count=1','-timeout=180s','-tags=p11_native_real','-run=^TestNativeReal','-json','./client/adapters/llamaworker'],cwd=ROOT/'dev/src',env=env,stdout=output,stderr=subprocess.STDOUT,timeout=220)
            code = result.returncode
        except subprocess.TimeoutExpired: code = 124
    after = subprocess.run(query,capture_output=True,text=True,timeout=10,check=True).stdout.strip()
    events = []
    for line in raw.read_text(encoding='utf-8',errors='replace').splitlines():
        try: events.append(json.loads(line))
        except json.JSONDecodeError: pass
    cases = {e['Test']:e['Action'] for e in events if e.get('Test') and '/' not in e['Test'] and e['Action'] in ('pass','fail','skip')}
    paths = [p for d in ['dev/src/client/adapters/llamaworker','dev/src/client/kernels/llama-worker','dev/src/client/results','dev/src/client/protocol/textoutput','dev/src/client/supervisor','dev/src/shared/gpuid'] for p in (ROOT/d).glob('*') if p.is_file()]
    paths += [Path(__file__).resolve(),ROOT/'dev/tools/准备真实LLM验证包.py']
    manifest = json.loads(package.read_text(encoding='utf-8'))
    for pin in [manifest['executable'],manifest['model'],manifest['template'],*manifest['libraries']]:
        if sha(package.parent/pin['path']) != pin['sha256']: raise RuntimeError('native package changed during test')
    passed = code == 0 and len(cases) == 3 and all(v == 'pass' for v in cases.values())
    receipt = {'round':'R024','attempt':args.attempt,'recorded_at':started,'finished_at':datetime.now(timezone.utc).isoformat(),'passed':passed,'returncode':code,
      'scope':'Explicit Windows native CUDA0/Qwen 0.5B test on this machine; original owned process, pinned CUDA modules, six JSON/SSE render combinations, native stop/cancel/deadline. Observed UUID/PCI is local metadata, not remote attestation. Not marketplace, LP, E2E, SDK, trusted usage, funds or 30-run performance qualification.',
      'gpu_before':before,'gpu_after':after,'cases':cases,'preparation':preparation,'package_manifest':manifest,
      'raw':raw.relative_to(ROOT).as_posix(),'raw_sha256':sha(raw),'sources':{p.relative_to(ROOT).as_posix():sha(p) for p in paths},'virtualization_used':False}
    target.write_text(json.dumps(receipt,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
    if passed: (evidence/'R024-native-real-summary.json').write_bytes(target.read_bytes())
    print(json.dumps({'passed':passed,'returncode':code,'cases':cases},ensure_ascii=False))
    return 0 if passed else 1

if __name__ == '__main__': raise SystemExit(main())
