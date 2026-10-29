"""Use the exact authorized Sub2API image skill; preserve earlier candidates."""
from __future__ import annotations
import argparse
from concurrent.futures import ThreadPoolExecutor,as_completed
from datetime import datetime,timezone
import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys

ROOT=Path(__file__).resolve().parents[3]
OUT=ROOT/'output/imagegen/2026-10-30-04-最终设计约束'
SKILL=Path('C:/Users/carzy/.codex/skills/sub2-image-gen/scripts/sub2_image_gen.py')

def clean(value):
    value=re.sub(r'Bearer\s+\S+','Bearer [REDACTED]',value,flags=re.I)
    return re.sub(r'\bsk-[A-Za-z0-9_-]{8,}','[REDACTED]',value)[-1800:]

def run(job):
    target=ROOT/job['output'];reference=ROOT/job['reference']
    if (OUT/'production/STOP').exists():return dict(id=job['id'],status='CANCELLED_BEFORE_REQUEST')
    if target.exists():return dict(id=job['id'],status='EXISTING',output=job['output'],sha256=hashlib.sha256(target.read_bytes()).hexdigest())
    if not reference.is_file():return dict(id=job['id'],status='MISSING_REFERENCE')
    command=[sys.executable,str(SKILL),'edit','--model','gpt-image-2.5','--quality','high','--size',job['size'],'--output-format','png','--image',str(reference),'--prompt-file',str(ROOT/job['prompt']),'--out',str(target)]
    start=datetime.now(timezone.utc).isoformat()
    try:
        result=subprocess.run(command,cwd=ROOT,capture_output=True,text=True,encoding='utf-8',errors='replace',timeout=240,creationflags=getattr(subprocess,'CREATE_NO_WINDOW',0))
        if result.returncode:return dict(id=job['id'],status='FAILED',exit_code=result.returncode,error=clean(result.stderr+'\n'+result.stdout))
        return dict(id=job['id'],status='GENERATED',output=job['output'],started_at=start,finished_at=datetime.now(timezone.utc).isoformat(),sha256=hashlib.sha256(target.read_bytes()).hexdigest(),reference_sha256=hashlib.sha256(reference.read_bytes()).hexdigest())
    except Exception as e:return dict(id=job['id'],status='FAILED',error_type=type(e).__name__,error=clean(str(e)))

def main():
    p=argparse.ArgumentParser();p.add_argument('phase',choices=['anchor','pages']);p.add_argument('--ids',nargs='*');args=p.parse_args()
    jobs=json.loads((OUT/'production/jobs.json').read_text(encoding='utf-8'))['jobs']
    jobs=[j for j in jobs if (j['kind']=='anchor')==(args.phase=='anchor') and (not args.ids or j['id'] in args.ids)]
    records=[];report=OUT/'production'/('generation-'+args.phase+'.json')
    with ThreadPoolExecutor(max_workers=3) as pool:
        futures=[pool.submit(run,j) for j in jobs]
        for future in as_completed(futures):
            record=future.result();records.append(record)
            report.write_text(json.dumps(dict(phase=args.phase,model='gpt-image-2.5',records=records),ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
            print(json.dumps(record,ensure_ascii=False),flush=True)
    return 0 if len(records)==len(jobs) and all(r['status'] in {'GENERATED','EXISTING'} for r in records) else 1

if __name__=='__main__':raise SystemExit(main())
