"""下载固定原生工具到本项目；不安装系统服务、不修改系统 PATH。"""
from __future__ import annotations
import hashlib
import json
from pathlib import Path
import urllib.request
import zipfile

ROOT = Path(__file__).resolve().parents[2]
TOOLS = ROOT / 'runtime/tooling/native'
DOWNLOADS = ROOT / 'runtime/downloads'
SPECS = [
    {'name':'go','version':'1.27.3','url':'https://dl.google.com/go/go1.27.3.windows-amd64.zip',
     'sha256':'f26081f438319757f8a1b46d7d0b2c5ec7583d4038e485e0aa725cfde7885fff',
     'publisher':'https://go.dev/dl/?mode=json','license':'BSD-3-Clause','prefix':'go/'},
    {'name':'postgresql','version':'17.11-5','url':'https://get.enterprisedb.com/postgresql/postgresql-17.11-5-windows-x64-binaries.zip',
     'publisher':'https://www.enterprisedb.com/download-postgresql-binaries','license':'PostgreSQL and bundled component licenses',
     'prefix':'pgsql/'},
]

def main():
    DOWNLOADS.mkdir(parents=True,exist_ok=True)
    lock_path=ROOT/'dev/toolchain.lock.json'
    previous=json.loads(lock_path.read_text(encoding='utf-8')) if lock_path.exists() else {'tools':[]}
    records=[]
    for spec in SPECS:
        archive=DOWNLOADS/spec['url'].rsplit('/',1)[1]
        if not archive.exists():
            temporary=archive.with_suffix('.part')
            print('Downloading',spec['name'],spec['version'],flush=True)
            with urllib.request.urlopen(spec['url'],timeout=45) as response,temporary.open('wb') as out:
                while chunk:=response.read(1024*1024): out.write(chunk)
            temporary.replace(archive)
        digest=hashlib.file_digest(archive.open('rb'),'sha256').hexdigest()
        pinned=next((t.get('archive_sha256') for t in previous['tools'] if t['name']==spec['name'] and t['version']==spec['version']),None)
        expected=spec.get('sha256') or pinned
        if expected and digest!=expected: raise ValueError('Archive checksum mismatch: '+spec['name'])
        destination=TOOLS/(spec['name']+'-'+spec['version'])
        marker=destination/'archive.sha256'
        if not marker.exists() or marker.read_text().strip()!=digest:
            destination.mkdir(parents=True,exist_ok=True)
            with zipfile.ZipFile(archive) as z:
                for member in z.infolist():
                    if not member.filename.startswith(spec['prefix']): continue
                    # pgAdmin and optional GUI payloads are not needed for the native DB tests.
                    if spec['name']=='postgresql' and not member.filename.startswith(('pgsql/bin/','pgsql/lib/','pgsql/share/','pgsql/doc/')): continue
                    target=(destination/member.filename).resolve()
                    if not target.is_relative_to(destination.resolve()): raise ValueError('Archive traversal')
                    z.extract(member,destination)
            marker.write_text(digest+'\n',encoding='utf-8')
        records.append({**{k:v for k,v in spec.items() if k not in ('sha256','prefix')},'archive_sha256':digest,
                        'hash_provenance':'publisher SHA-256' if spec.get('sha256') else 'pinned first download over publisher HTTPS; no separately published digest asserted',
                        'directory':destination.relative_to(ROOT).as_posix()})
        print('Ready',spec['name'],spec['version'],digest,flush=True)
    lock_path.write_text(json.dumps({'platform':'windows-amd64','tools':records},ensure_ascii=False,indent=2)+'\n',encoding='utf-8')

if __name__=='__main__': main()
