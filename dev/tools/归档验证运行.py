"""覆盖回归证据前，按收据逐路径/摘要归档原日志、媒体和源码；拒绝覆盖旧归档。"""
from __future__ import annotations
import argparse
from datetime import datetime, timezone
import hashlib
import json
from pathlib import Path
import re
import shutil
import zipfile

ROOT = Path(__file__).resolve().parents[2]
EVIDENCE = ROOT / 'dev/tests/evidence'

def sha(path):
    with path.open('rb') as source:
        return hashlib.file_digest(source, 'sha256').hexdigest()

def failed_browser_receipts(events, round_id):
    """Recover only actual failed child stdout from this run, never a stale pass."""
    outputs = {'web': [], 'miniapp': []}
    for line in events.read_text(encoding='utf-8').splitlines():
        event = json.loads(line)
        for target in outputs:
            if event.get('Test', '').endswith('/' + target) and event.get('Action') == 'output':
                outputs[target].append(event.get('Output', ''))
    receipts = {}
    for target, chunks in outputs.items():
        text = ''.join(chunks)
        marker = '{"round":"' + round_id + '","target":"' + target + '","passed":false,'
        start = text.find(marker)
        if start < 0: continue
        receipt, end = json.JSONDecoder().raw_decode(text[start:])
        if receipt.get('round') != round_id or receipt.get('target') != target or receipt.get('passed') is not False:
            raise ValueError('failed browser receipt does not match this run')
        receipts[target] = (receipt, text[start:start + end] + '\n')
    return receipts

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--round',required=True)
    parser.add_argument('--attempt',required=True,type=int)
    args=parser.parse_args()
    if not re.fullmatch(r'R[0-9]{3}',args.round) or args.attempt<1:
        raise ValueError('explicit round/positive attempt required')
    destination=EVIDENCE/f'{args.round}-full-attempt{args.attempt}'
    manifest=EVIDENCE/f'{args.round}-full-attempt{args.attempt}-archive.json'
    if destination.exists() or manifest.exists():
        raise FileExistsError('archive identity already exists; never overwrite')
    destination.mkdir()
    originals={}
    for suffix in ['native-summary.json','native-go-tests.jsonl','native-source-checks.json','web-browser.json','miniapp-browser.json']:
        path=EVIDENCE/f'{args.round}-{suffix}'
        if path.is_file():originals[path.relative_to(ROOT).as_posix()]=None
    if f'dev/tests/evidence/{args.round}-native-summary.json' not in originals:
        raise ValueError('native run summary required')
    inactive_browser_receipts = []
    events = ROOT / f'dev/tests/evidence/{args.round}-native-go-tests.jsonl'
    for target, (_, raw) in failed_browser_receipts(events, args.round).items():
        name = f'dev/tests/evidence/{args.round}-{target}-browser-error.json'
        (ROOT / name).write_text(raw, encoding='utf-8')
        originals[name] = None
        inactive_browser_receipts.append(f'dev/tests/evidence/{args.round}-{target}-browser.json')
    def discover(value):
        if isinstance(value,dict):
            path=value.get('path');digest=value.get('sha256')
            if isinstance(path,str) and path.startswith(f'dev/media/{args.round}/') and isinstance(digest,str):
                if path in originals and originals[path] not in (None,digest):raise ValueError('conflicting media digest')
                originals[path]=digest
            for child in value.values():discover(child)
        elif isinstance(value,list):
            for child in value:discover(child)
    for name in list(originals):
        if name.endswith('.json') and name not in inactive_browser_receipts:discover(json.loads((ROOT/name).read_text(encoding='utf-8')))
    artifacts=[];missing=[]
    for name,expected in originals.items():
        source=ROOT/name
        if not source.resolve().is_relative_to(ROOT.resolve()):raise ValueError('archive path outside project')
        if not source.is_file():missing.append({'original_path':name,'expected_sha256':expected,'reason':'MISSING'});continue
        actual=sha(source)
        if expected and expected!=actual:missing.append({'original_path':name,'expected_sha256':expected,'actual_sha256':actual,'reason':'DIGEST_MISMATCH'});continue
        target=destination/name;target.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(source,target)
        if sha(target)!=actual:raise ValueError('archive copy mismatch')
        artifacts.append({'original_path':name,'path':target.relative_to(ROOT).as_posix(),'sha256':actual,'bytes':target.stat().st_size})
    snapshot=destination/'source-snapshot.zip'
    paths=[p for p in (ROOT/'dev/src').rglob('*') if p.is_file() and p.suffix in {'.go','.cpp','.sql','.mod','.sum'}]
    paths += [ROOT/n for n in ['dev/tools/原生数据库验证.py','dev/tools/核验原生源码.py','dev/tools/frontend/browser.mjs','dev/tools/归档验证运行.py']]
    with zipfile.ZipFile(snapshot,'w',compression=zipfile.ZIP_DEFLATED) as archive:
        for source in sorted(set(paths)):archive.write(source,source.relative_to(ROOT).as_posix())
    artifacts.append({'path':snapshot.relative_to(ROOT).as_posix(),'sha256':sha(snapshot),'bytes':snapshot.stat().st_size,'scope':'committed-type product/test sources and verification programs; no runtime secrets, model or binary'})
    summary=json.loads((ROOT/f'dev/tests/evidence/{args.round}-native-summary.json').read_text(encoding='utf-8'))
    report={'round':args.round,'run_id':summary.get('run_id'),'recorded_at':datetime.now(timezone.utc).isoformat(),'status':'ARCHIVED_EXACT_ASSETS' if not missing else 'ARCHIVED_WITH_EXPLICIT_MISSING_ASSETS','artifacts':artifacts,'inactive_browser_receipts':inactive_browser_receipts,'missing_assets':missing,'virtualization_used':False}
    manifest.write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
    print(json.dumps({'archive':manifest.relative_to(ROOT).as_posix(),'files':len(artifacts),'missing':len(missing)}))
    return 0 if not missing else 1

if __name__=='__main__':raise SystemExit(main())
