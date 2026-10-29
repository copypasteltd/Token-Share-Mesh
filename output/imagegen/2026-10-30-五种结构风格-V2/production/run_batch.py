"""Run only the user-selected Sub2API image skill, with per-family reference images."""
from __future__ import annotations
import argparse
from concurrent.futures import ThreadPoolExecutor,as_completed
from datetime import datetime,timezone
import hashlib
import json
from pathlib import Path
import re
import struct
import subprocess
import sys

BASE=Path(__file__).resolve().parents[1]
ROOT=BASE.parents[2]
SKILL=Path('C:/Users/carzy/.codex/skills/sub2-image-gen/scripts/sub2_image_gen.py')

def image_info(file):
    data=file.read_bytes()
    if not data.startswith(b'\x89PNG\r\n\x1a\n'):raise ValueError('Not a PNG')
    width,height=struct.unpack('>II',data[16:24])
    return {'width':width,'height':height,'bytes':len(data),'sha256':hashlib.sha256(data).hexdigest()}

def safe_error(value):
    value=re.sub(r'Bearer\s+\S+','Bearer [REDACTED]',value,flags=re.I)
    value=re.sub(r'\bsk-[A-Za-z0-9_-]{8,}','[REDACTED_API_KEY]',value)
    return value[-2200:]

def run(job):
    target=BASE/job['output']
    if target.exists():
        return {**job,'status':'EXISTING','image':image_info(target)}
    args=[sys.executable,str(SKILL),'generate' if job['page']=='home' else 'edit','--model',job['model'],'--size',job['size'],'--quality',job['quality'],'--output-format','png','--prompt-file',str(BASE/job['prompt']),'--out',str(target)]
    if job['reference']:
        reference=BASE/job['reference']
        if not reference.is_file():return {**job,'status':'BLOCKED_REFERENCE'}
        args+=['--image',str(reference)]
    try:
        result=subprocess.run(args,cwd=ROOT,capture_output=True,text=True,encoding='utf-8',errors='replace',timeout=240,creationflags=getattr(subprocess,'CREATE_NO_WINDOW',0))
        if result.returncode:return {**job,'status':'FAILED','exit_code':result.returncode,'gateway_error':safe_error(result.stderr+'\n'+result.stdout)}
        return {**job,'status':'GENERATED','image':image_info(target)}
    except Exception as error:
        return {**job,'status':'FAILED','error_type':type(error).__name__,'detail':safe_error(str(error))}

def main():
    parser=argparse.ArgumentParser()
    parser.add_argument('phase',choices=['anchors','variants'])
    parser.add_argument('--workers',type=int,default=3)
    parser.add_argument('--styles',nargs='*')
    args=parser.parse_args()
    alljobs=json.loads((BASE/'production/jobs.json').read_text(encoding='utf-8'))['jobs']
    jobs=[j for j in alljobs if (j['page']=='home')==(args.phase=='anchors') and (not args.styles or j['style'] in args.styles)]
    report=BASE/'production'/('results-'+args.phase+'.json')
    results=[]
    with ThreadPoolExecutor(max_workers=max(1,min(args.workers,3))) as pool:
        futures={pool.submit(run,j):j['id'] for j in jobs}
        for future in as_completed(futures):
            item=future.result();results.append(item)
            report.write_text(json.dumps({'phase':args.phase,'updated_at':datetime.now(timezone.utc).isoformat(),'model':'gpt-image-2.5','results':results},ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
            print(json.dumps({'id':item['id'],'status':item['status'],'output':item['output'],**({'gateway_error':item.get('gateway_error',item.get('detail',''))} if item['status']=='FAILED' else {})},ensure_ascii=False),flush=True)
    return 0 if all(r['status'] in {'GENERATED','EXISTING'} for r in results) and len(results)==len(jobs) else 1

if __name__=='__main__':raise SystemExit(main())
