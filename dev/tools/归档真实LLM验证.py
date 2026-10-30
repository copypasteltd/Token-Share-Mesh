"""按原摘要归档一次真实内核尝试；不改写原证据或重新生成其媒体。"""
from pathlib import Path
import argparse
import hashlib
import json
import zipfile

ROOT = Path(__file__).resolve().parents[2]
def sha(p):
    with p.open('rb') as f: return hashlib.file_digest(f,'sha256').hexdigest()
def main():
    parser=argparse.ArgumentParser(description=__doc__); parser.add_argument('--attempt',type=int,required=True); args=parser.parse_args()
    if args.attempt < 1 or args.attempt > 99: raise RuntimeError('invalid attempt')
    prefix=f'R024-native-real-attempt{args.attempt}'; report=ROOT/f'dev/tests/evidence/{prefix}-summary.json'
    obj=json.loads(report.read_text(encoding='utf-8'))
    files=[(ROOT/p,p) for p in obj['sources']]
    for path,name in files:
        if sha(path)!=obj['sources'][name]: raise RuntimeError('source changed before archive: '+name)
    package=Path(obj['preparation']['package']); worker=package.parent/obj['package_manifest']['executable']['path']
    if sha(package)!=obj['preparation']['package_sha256'] or sha(worker)!=obj['package_manifest']['executable']['sha256']: raise RuntimeError('original native inputs changed')
    files += [(report,report.relative_to(ROOT).as_posix()),(ROOT/obj['raw'],obj['raw']),(package,'runtime-input/package.json'),(worker,'runtime-input/p11-llama-worker.exe')]
    target=ROOT/f'dev/tests/evidence/archive/{prefix}.zip'; target.parent.mkdir(parents=True,exist_ok=True)
    with zipfile.ZipFile(target,'x',zipfile.ZIP_DEFLATED) as z:
        for path,name in files: z.write(path,name)
    receipt={'round':'R024','attempt':args.attempt,'archive':target.relative_to(ROOT).as_posix(),'sha256':sha(target),'files':len(files),'source_hashes_exact':True,'scope':'Exact original source/worker/package metadata; pinned official runtime libraries and model remain private reproducible downloads, not redistributed'}
    (ROOT/f'dev/tests/evidence/{prefix}-archive.json').write_text(json.dumps(receipt,indent=2)+'\n',encoding='utf-8'); print(json.dumps(receipt))
    return 0
if __name__=='__main__': raise SystemExit(main())
